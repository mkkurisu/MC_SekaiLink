import React, { useEffect, useState } from 'react';
import { Cpu, Download, Loader2, Save, Settings, Trash2 } from 'lucide-react';
import { apiFetch, getAndroidBridge } from '../lib/api';
import { JVMPreset, ServerInstance, SystemOverview } from '../types';

interface Props {
  server: ServerInstance | null;
  system: SystemOverview | null;
  onRefresh: () => void;
  onDeleted: () => void;
}

const PROPERTY_FIELDS = [
  { key: 'motd', label: 'Sunucu Aciklamasi (MOTD)', type: 'text' },
  { key: 'gamemode', label: 'Oyun Modu', type: 'select', options: ['survival', 'creative', 'adventure', 'spectator'] },
  { key: 'difficulty', label: 'Zorluk', type: 'select', options: ['peaceful', 'easy', 'normal', 'hard'] },
  { key: 'max-players', label: 'Maksimum Oyuncu', type: 'number' },
  { key: 'pvp', label: 'PvP Acik', type: 'bool' },
  { key: 'online-mode', label: 'Premium Dogrulama (online-mode)', type: 'bool' },
  { key: 'white-list', label: 'Whitelist', type: 'bool' },
  { key: 'allow-flight', label: 'Ucus Izni (allow-flight)', type: 'bool' },
  { key: 'view-distance', label: 'Gorus Mesafesi (Chunks)', type: 'number' },
  { key: 'simulation-distance', label: 'Simulasyon Mesafesi', type: 'number' },
  { key: 'level-seed', label: 'Dunya Tohumu (Seed)', type: 'text' },
  { key: 'enable-command-block', label: 'Komut Bloklari', type: 'bool' },
];

export const SettingsPage: React.FC<Props> = ({ server, system, onRefresh, onDeleted }) => {
  const [name, setName] = useState('');
  const [memoryMB, setMemoryMB] = useState(2048);
  const [jvmPreset, setJvmPreset] = useState<JVMPreset>('aikar');
  const [javaPort, setJavaPort] = useState(25565);
  const [bedrockPort, setBedrockPort] = useState(19132);
  const [autoRestart, setAutoRestart] = useState(false);
  const [props, setProps] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [installingJava, setInstallingJava] = useState(false);
  const [msg, setMsg] = useState('');

  useEffect(() => {
    if (!server) return;
    setName(server.name);
    setMemoryMB(server.memoryMB);
    setJvmPreset(server.jvmPreset);
    setJavaPort(server.javaPort);
    setBedrockPort(server.crossPlay?.enabled ? server.crossPlay.bedrockPort : server.bedrockPort);
    setAutoRestart(server.autoRestart);
    apiFetch<Record<string, string>>(`/api/servers/${server.id}/properties`)
      .then(setProps)
      .catch(() => {});
  }, [server?.id]);

  const handleSave = async () => {
    if (!server) return;
    setSaving(true);
    setMsg('');
    try {
      await apiFetch(`/api/servers/${server.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          name,
          memoryMB,
          jvmPreset,
          javaPort,
          bedrockPort,
          autoRestart,
          crossPlay: { ...server.crossPlay, bedrockPort },
        }),
      });
      await apiFetch(`/api/servers/${server.id}/properties`, {
        method: 'PUT',
        body: JSON.stringify({ ...props, 'server-port': String(server.engine === 'bds' ? bedrockPort : javaPort) }),
      });
      setMsg('Ayarlar kaydedildi. Port/RAM degisiklikleri sonraki baslatmada uygulanir.');
      onRefresh();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : 'Kaydetme hatasi');
    } finally {
      setSaving(false);
    }
  };

  const handleInstallJava = async () => {
    const bridge = getAndroidBridge();
    if (bridge) {
      bridge.installArm64Jre();
      setMsg('Android ARM64 Java 21 kurulumu baslatildi...');
      return;
    }
    setInstallingJava(true);
    setMsg('');
    try {
      await apiFetch('/api/system/install-java', { method: 'POST' });
      setMsg('Eclipse Temurin Java 21 JRE basariyla kuruldu!');
      onRefresh();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : 'Java kurulumu basarisiz');
    } finally {
      setInstallingJava(false);
    }
  };

  const handleDelete = async () => {
    if (!server) return;
    if (!window.confirm(`"${server.name}" sunucusu ve tum dosyalari silinsin mi?`)) return;
    await apiFetch(`/api/servers/${server.id}`, { method: 'DELETE' });
    onDeleted();
  };

  if (!server) {
    return <div className="glass-card rounded-2xl p-8 text-center text-sm text-slate-400">Once bir sunucu secin.</div>;
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
              <h2 className="text-lg font-bold text-white">Performans &amp; Java Calisma Ortami</h2>
              <p className="text-xs text-slate-400">
                Java Durumu:{' '}
                <strong className={system?.javaDetected ? 'text-emerald-300' : 'text-amber-300'}>
                  {system?.javaDetected ? `Hazir (${system.javaVersion})` : 'Bulunamadi'}
                </strong>
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <button
              disabled={installingJava}
              onClick={handleInstallJava}
              className="flex items-center gap-1.5 rounded-xl border border-cyan-500/30 bg-cyan-500/15 px-3.5 py-2 text-xs font-semibold text-cyan-300 hover:bg-cyan-500/25"
            >
              {installingJava ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
              Otomatik Java 21 Kur
            </button>
            <button
              disabled={saving}
              onClick={handleSave}
              className="flex items-center gap-2 rounded-xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-5 py-2 text-xs font-semibold text-slate-950 hover:brightness-110"
            >
              {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
              Kaydet
            </button>
          </div>
        </div>

        <div className="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <div>
            <label className="mb-1 block text-xs text-slate-400">Sunucu Adi</label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 text-sm text-white outline-none"
            />
          </div>
          <div>
            <label className="mb-1 block text-xs text-slate-400">RAM ({memoryMB} MB)</label>
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
            <label className="mb-1 block text-xs text-slate-400">JVM GC Profili</label>
            <select
              value={jvmPreset}
              onChange={(e) => setJvmPreset(e.target.value as JVMPreset)}
              className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 text-sm text-white outline-none"
            >
              <option value="aikar">Aikar&apos;s Flags (Onerilen)</option>
              <option value="zgc">Generational ZGC</option>
              <option value="standard">Standard</option>
            </select>
          </div>
          <div>
            <label className="mb-1 block text-xs text-slate-400">Java Port (TCP)</label>
            <input
              type="number"
              value={javaPort}
              onChange={(e) => setJavaPort(Number(e.target.value))}
              className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 text-sm text-white outline-none"
            />
          </div>
          <div>
            <label className="mb-1 block text-xs text-slate-400">Bedrock Port (UDP)</label>
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
            Cokme durumunda otomatik yeniden baslat
          </label>
        </div>
      </div>

      {/* server.properties Editor */}
      <div className="glass-card rounded-2xl p-6">
        <h3 className="mb-4 flex items-center gap-2 text-base font-bold text-white">
          <Settings className="h-4 w-4 text-cyan-400" />
          Gorsel server.properties Duzenleyici
        </h3>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {PROPERTY_FIELDS.map((f) => {
            const val = props[f.key] ?? '';
            return (
              <div key={f.key} className="rounded-xl border border-white/10 bg-slate-950/50 p-3">
                <label className="mb-1.5 block text-xs font-medium text-slate-300">{f.label}</label>
                {f.type === 'bool' ? (
                  <button
                    onClick={() => setProps({ ...props, [f.key]: val === 'true' ? 'false' : 'true' })}
                    className={`rounded-lg px-3 py-1.5 text-xs font-semibold ${val === 'true' ? 'bg-emerald-500/20 text-emerald-300' : 'bg-slate-800 text-slate-400'}`}
                  >
                    {val === 'true' ? 'Acik (true)' : 'Kapali (false)'}
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
          <h4 className="text-sm font-semibold text-rose-300">Sunucuyu Kalici Olarak Sil</h4>
          <p className="text-xs text-slate-400">Bu islem sunucuya ait dunyalari, modlari ve yedekleri kaldirir.</p>
        </div>
        <button
          onClick={handleDelete}
          className="flex items-center gap-1.5 rounded-xl border border-rose-500/40 bg-rose-500/20 px-4 py-2 text-xs font-semibold text-rose-200 hover:bg-rose-500/30"
        >
          <Trash2 className="h-3.5 w-3.5" />
          Sunucuyu Sil
        </button>
      </div>
    </div>
  );
};
