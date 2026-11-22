import React, { useEffect, useState } from 'react';
import { Cpu, Download, Loader2, Save, Settings, Trash2, Zap } from 'lucide-react';
import { apiFetch, getAndroidBridge } from '../lib/api';
import { Lang, translations } from '../lib/i18n';
import { JVMPreset, ServerInstance, SystemOverview } from '../types';

interface Props {
  server: ServerInstance | null;
  system: SystemOverview | null;
  lang: Lang;
  onRefresh: () => void;
  onDeleted: () => void;
}

export const SettingsPage: React.FC<Props> = ({ server, system, lang, onRefresh, onDeleted }) => {
  const t = translations[lang].settings;
  const [name, setName] = useState('');
  const [memoryMB, setMemoryMB] = useState(2048);
  const [jvmPreset, setJvmPreset] = useState<JVMPreset>('aikar');
  const [javaPort, setJavaPort] = useState(25565);
  const [bedrockPort, setBedrockPort] = useState(19132);
  const [autoRestart, setAutoRestart] = useState(false);
  const [offlineMode, setOfflineMode] = useState(false);
  const [props, setProps] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [optimizing, setOptimizing] = useState(false);
  const [installingJava, setInstallingJava] = useState(false);
  const [msg, setMsg] = useState('');

  const PROPERTY_FIELDS = [
    { key: 'motd', label: t.propLabels.motd, type: 'text' },
    { key: 'gamemode', label: t.propLabels.gamemode, type: 'select', options: ['survival', 'creative', 'adventure', 'spectator'] },
    { key: 'difficulty', label: t.propLabels.difficulty, type: 'select', options: ['peaceful', 'easy', 'normal', 'hard'] },
    { key: 'max-players', label: t.propLabels.maxPlayers, type: 'number' },
    { key: 'pvp', label: t.propLabels.pvp, type: 'bool' },
    { key: 'online-mode', label: t.propLabels.onlineMode, type: 'bool' },
    { key: 'white-list', label: t.propLabels.whiteList, type: 'bool' },
    { key: 'allow-flight', label: t.propLabels.allowFlight, type: 'bool' },
    { key: 'view-distance', label: t.propLabels.viewDistance, type: 'number' },
    { key: 'simulation-distance', label: t.propLabels.simulationDistance, type: 'number' },
    { key: 'level-seed', label: t.propLabels.levelSeed, type: 'text' },
    { key: 'enable-command-block', label: t.propLabels.enableCommandBlock, type: 'bool' },
  ];

  useEffect(() => {
    const onJreStatus = (ev: Event) => {
      const detail = (ev as CustomEvent<{ message?: string; done?: boolean; installed?: boolean }>).detail;
      if (!detail) return;
      if (detail.message) {
        setMsg(detail.message);
      }
      if (typeof detail.done === 'boolean') {
        setInstallingJava(!detail.done);
      }
      if (detail.done && detail.installed) {
        onRefresh();
      }
    };
    window.addEventListener('sekai-jre-status', onJreStatus);
    return () => window.removeEventListener('sekai-jre-status', onJreStatus);
  }, [onRefresh]);

  useEffect(() => {
    if (!server) return;
    setName(server.name);
    setMemoryMB(server.memoryMB);
    setJvmPreset(server.jvmPreset);
    setJavaPort(server.javaPort);
    setBedrockPort(server.crossPlay?.enabled ? server.crossPlay.bedrockPort : server.bedrockPort);
    setAutoRestart(server.autoRestart);
    setOfflineMode(Boolean(server.offlineMode));
    apiFetch<Record<string, string>>(`/api/servers/${server.id}/properties`)
      .then(setProps)
      .catch(() => {});
  }, [server?.id]);

  const handleSave = async () => {
    if (!server) return;
    setSaving(true);
    setMsg('');
    try {
      const nextProps: Record<string, string> = {
        ...props,
        'server-port': String(server.engine === 'bds' ? bedrockPort : javaPort),
      };
      if (server.engine === 'powernukkitx') {
        nextProps['xbox-auth'] = offlineMode ? 'off' : 'on';
      } else {
        nextProps['online-mode'] = offlineMode ? 'false' : 'true';
        if (server.engine !== 'bds') {
          nextProps['enforce-secure-profile'] = offlineMode ? 'false' : 'true';
        }
      }
      const res = await apiFetch<{ properties: Record<string, string>; liveApplied: string[] }>(
        `/api/servers/${server.id}/properties?withMeta=true`,
        {
          method: 'PUT',
          body: JSON.stringify(nextProps),
        },
      );
      await apiFetch(`/api/servers/${server.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          name,
          memoryMB,
          jvmPreset,
          javaPort,
          bedrockPort,
          autoRestart,
          offlineMode,
          crossPlay: { ...server.crossPlay, bedrockPort },
        }),
      });
      setProps(res?.properties || nextProps);
      const liveList = res?.liveApplied || [];
      if (liveList.length > 0) {
        const keyMap: Record<string, string> = {
          gamemode: t.propLabels.gamemode,
          difficulty: t.propLabels.difficulty,
          'white-list': t.propLabels.whiteList,
          pvp: t.propLabels.pvp,
        };
        const names = liveList.map((k) => keyMap[k] || k).join(', ');
        setMsg(`${t.savedLivePrefix} ${names}. ${t.savedRestartRest}`);
      } else {
        setMsg(t.savedDefault);
      }
      onRefresh();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.saveErr);
    } finally {
      setSaving(false);
    }
  };

  const handleOptimizeLag = async () => {
    if (!server) return;
    setOptimizing(true);
    setMsg('');
    try {
      const updated = await apiFetch<Record<string, string>>(`/api/servers/${server.id}/optimize-lag`, {
        method: 'POST',
      });
      setProps(updated);
      setMsg(t.lagSuccess);
      onRefresh();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.lagFail);
    } finally {
      setOptimizing(false);
    }
  };

  const handleInstallJava = async () => {
    const bridge = getAndroidBridge();
    if (bridge) {
      setInstallingJava(true);
      setMsg(t.androidJavaStarted);
      bridge.installArm64Jre();
      return;
    }
    setInstallingJava(true);
    setMsg('');
    try {
      await apiFetch('/api/system/install-java', { method: 'POST' });
      setMsg(t.javaInstallSuccess);
      onRefresh();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.javaInstallFail);
    } finally {
      setInstallingJava(false);
    }
  };

  const handleDelete = async () => {
    if (!server) return;
    if (!window.confirm(`${t.deleteConfirmPrefix}${server.name}${t.deleteConfirmSuffix}`)) return;
    await apiFetch(`/api/servers/${server.id}`, { method: 'DELETE' });
    onDeleted();
  };

  if (!server) {
    return <div className="glass-card rounded-2xl p-8 text-center text-sm text-slate-400">{t.selectServerFirst}</div>;
  }

  return (
    <div className="space-y-5">
      {msg && (
        <div className="rounded-2xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-2.5 text-sm text-emerald-200">{msg}</div>
      )}

      {/* Runtime & Java Card */}
      <div className="glass-panel rounded-3xl p-6">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-2.5">
            <Cpu className="h-5 w-5 text-emerald-400" />
            <div>
              <h2 className="text-lg font-bold text-white">{t.runtimeTitle}</h2>
              <p className="text-xs text-slate-400">
                {t.javaStatusLabel}{' '}
                <strong className={system?.javaDetected ? 'text-emerald-300' : 'text-amber-300'}>
                  {system?.javaDetected ? `${t.javaReadyPrefix} (${system.javaVersion})` : t.javaNotFound}
                </strong>
              </p>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <button
              disabled={installingJava}
              onClick={handleInstallJava}
              className="flex items-center gap-1.5 rounded-xl border border-cyan-500/30 bg-cyan-500/15 px-3.5 py-2 text-xs font-semibold text-cyan-300 hover:bg-cyan-500/25"
            >
              {installingJava ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
              {t.installJavaBtn}
            </button>
            {getAndroidBridge()?.importArm64JreFile && (
              <button
                onClick={() => getAndroidBridge()?.importArm64JreFile?.()}
                className="flex items-center gap-1.5 rounded-xl border border-violet-500/30 bg-violet-500/15 px-3.5 py-2 text-xs font-semibold text-violet-300 hover:bg-violet-500/25"
              >
                {t.importAndroidJreBtn}
              </button>
            )}
            <button
              disabled={saving}
              onClick={handleSave}
              className="flex items-center gap-2 rounded-xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-5 py-2 text-xs font-semibold text-slate-950 hover:brightness-110"
            >
              {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
              {t.saveBtn}
            </button>
          </div>
        </div>

        <div className="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <div>
            <label className="mb-1 block text-xs text-slate-400">{t.serverName}</label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 text-sm text-white outline-none"
            />
          </div>
          <div>
            <label className="mb-1 block text-xs text-slate-400">{t.ramLabel} ({memoryMB} MB)</label>
            <input
              type="range"
              min={512}
              max={16384}
              step={512}
              value={memoryMB}
              onChange={(e) => setMemoryMB(Number(e.target.value))}
              className="mt-2 w-full accent-emerald-500"
            />
          </div>
          <div>
            <label className="mb-1 block text-xs text-slate-400">{t.jvmProfile}</label>
            <select
              value={jvmPreset}
              onChange={(e) => setJvmPreset(e.target.value as JVMPreset)}
              className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 text-sm text-white outline-none"
            >
              <option value="aikar">{t.jvmAikar}</option>
              <option value="zgc">{t.jvmZgc}</option>
              <option value="standard">{t.jvmStandard}</option>
            </select>
          </div>
          <div>
            <label className="mb-1 block text-xs text-slate-400">{t.javaPort}</label>
            <input
              type="number"
              value={javaPort}
              onChange={(e) => setJavaPort(Number(e.target.value))}
              className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 text-sm text-white outline-none"
            />
          </div>
          <div>
            <label className="mb-1 block text-xs text-slate-400">{t.bedrockPort}</label>
            <input
              type="number"
              value={bedrockPort}
              onChange={(e) => setBedrockPort(Number(e.target.value))}
              className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 text-sm text-white outline-none"
            />
          </div>
          <label className="flex items-center gap-2.5 self-end rounded-xl border border-white/10 bg-slate-950/60 px-3.5 py-2.5 text-xs text-slate-200">
            <input
              type="checkbox"
              checked={autoRestart}
              onChange={(e) => setAutoRestart(e.target.checked)}
              className="h-4 w-4 accent-emerald-500"
            />
            {t.autoRestartLabel}
          </label>
        </div>

        <div className="mt-4 space-y-2 rounded-2xl border border-amber-500/25 bg-amber-500/5 p-4">
          <label className="flex items-start gap-3 text-sm text-slate-200">
            <input
              type="checkbox"
              checked={offlineMode}
              onChange={(e) => {
                const next = e.target.checked;
                setOfflineMode(next);
                setProps((prev) => ({
                  ...prev,
                  'online-mode': next ? 'false' : 'true',
                  ...(server.engine !== 'bds' && server.engine !== 'powernukkitx'
                    ? { 'enforce-secure-profile': next ? 'false' : 'true' }
                    : {}),
                }));
              }}
              className="mt-0.5 h-4 w-4 accent-amber-400"
            />
            <div>
              <div className="font-semibold text-white">{t.offlineTitle}</div>
              <p className="mt-0.5 text-xs text-slate-400">{t.offlineDesc}</p>
            </div>
          </label>
          {offlineMode && (
            <p className="pl-7 text-[11px] text-amber-300/90">{t.offlineTip}</p>
          )}
        </div>
      </div>

      {/* Low-Lag Optimization Card */}
      {(() => {
        const viewDist = Number(props['view-distance'] || 0);
        const highViewDist = viewDist > 12;
        return (
          <div className={`rounded-2xl border p-5 ${highViewDist ? 'border-amber-500/40 bg-amber-500/10' : 'border-emerald-500/25 bg-emerald-500/5'}`}>
            <div className="flex flex-wrap items-center justify-between gap-4">
              <div>
                <h3 className="flex items-center gap-2 text-sm font-bold text-white">
                  <Zap className={`h-4 w-4 ${highViewDist ? 'text-amber-400' : 'text-emerald-400'}`} />
                  {t.lagTitle}
                </h3>
                <p className="mt-1 text-xs text-slate-300">
                  {highViewDist
                    ? `${t.lagHighWarnPrefix}${viewDist}${t.lagHighWarnSuffix}`
                    : t.lagDefaultDesc}
                </p>
              </div>
              <button
                disabled={optimizing}
                onClick={handleOptimizeLag}
                className="flex items-center gap-2 rounded-xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-4 py-2 text-xs font-bold text-slate-950 hover:brightness-110 disabled:opacity-50"
              >
                {optimizing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Zap className="h-3.5 w-3.5" />}
                {t.lagBtn}
              </button>
            </div>
          </div>
        );
      })()}

      {/* server.properties Editor */}
      <div className="glass-card rounded-2xl p-6">
        <h3 className="mb-4 flex items-center gap-2 text-base font-bold text-white">
          <Settings className="h-4 w-4 text-cyan-400" />
          {t.propsTitle}
        </h3>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {PROPERTY_FIELDS.map((f) => {
            const val = props[f.key] ?? '';
            return (
              <div key={f.key} className="rounded-xl border border-white/10 bg-slate-950/50 p-3">
                <label className="mb-1.5 block text-xs font-medium text-slate-300">{f.label}</label>
                {f.type === 'bool' ? (
                  <button
                    onClick={() => {
                      const nextVal = val === 'true' ? 'false' : 'true';
                      setProps({ ...props, [f.key]: nextVal });
                      if (f.key === 'online-mode') {
                        setOfflineMode(nextVal === 'false');
                      }
                    }}
                    className={`rounded-lg px-3 py-1.5 text-xs font-semibold ${val === 'true' ? 'bg-emerald-500/20 text-emerald-300' : 'bg-slate-800 text-slate-400'}`}
                  >
                    {val === 'true' ? t.boolOn : t.boolOff}
                  </button>
                ) : f.type === 'select' ? (
                  <select
                    value={val}
                    onChange={(e) => setProps({ ...props, [f.key]: e.target.value })}
                    className="w-full rounded-lg border border-white/10 bg-slate-900 px-2.5 py-1.5 text-xs text-white outline-none"
                  >
                    {f.options?.map((o) => (
                      <option key={o} value={o}>
                        {o}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    type={f.type}
                    value={val}
                    onChange={(e) => setProps({ ...props, [f.key]: e.target.value })}
                    className="w-full rounded-lg border border-white/10 bg-slate-900 px-2.5 py-1.5 text-xs text-white outline-none"
                  />
                )}
              </div>
            );
          })}
        </div>
      </div>

      {/* Danger Zone */}
      <div className="flex items-center justify-between rounded-2xl border border-rose-500/25 bg-rose-500/5 p-5">
        <div>
          <h4 className="text-sm font-semibold text-rose-300">{t.dangerTitle}</h4>
          <p className="text-xs text-slate-400">{t.dangerDesc}</p>
        </div>
        <button
          onClick={handleDelete}
          className="flex items-center gap-1.5 rounded-xl border border-rose-500/40 bg-rose-500/20 px-4 py-2 text-xs font-semibold text-rose-200 hover:bg-rose-500/30"
        >
          <Trash2 className="h-3.5 w-3.5" />
          {t.deleteBtn}
        </button>
      </div>
    </div>
  );
};
