import React, { useEffect, useState } from 'react';
import {
  Activity,
  Copy,
  Check,
  Cpu,
  Download,
  Globe,
  HardDrive,
  Play,
  Plus,
  QrCode,
  RefreshCw,
  Server,
  ShieldAlert,
  Square,
  Users,
  Wifi,
  Zap,
} from 'lucide-react';
import { Area, AreaChart, ResponsiveContainer, Tooltip } from 'recharts';
import { apiFetch, formatUptime } from '../lib/api';
import { Lang, translations } from '../lib/i18n';
import { ServerInstance, ServerMetrics, SystemOverview, TunnelState } from '../types';
import { QrShareModal } from '../components/QrShareModal';

interface Props {
  server: ServerInstance | null;
  system: SystemOverview | null;
  tunnel: TunnelState | null;
  lang: Lang;
  onRefresh: () => void;
  onOpenCreate: () => void;
  onNavigate: (tab: string) => void;
}

interface MetricPoint {
  t: string;
  cpu: number;
  ram: number;
}

export const DashboardPage: React.FC<Props> = ({
  server,
  system,
  tunnel,
  lang,
  onRefresh,
  onOpenCreate,
  onNavigate,
}) => {
  const t = translations[lang];
  const [metrics, setMetrics] = useState<ServerMetrics | null>(null);
  const [history, setHistory] = useState<MetricPoint[]>([]);
  const [busy, setBusy] = useState(false);
  const [copiedKey, setCopiedKey] = useState<string>('');
  const [qrValue, setQrValue] = useState<{ title: string; value: string } | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!server) {
      setMetrics(null);
      setHistory([]);
      return;
    }
    let active = true;
    const poll = async () => {
      try {
        const m = await apiFetch<ServerMetrics>(`/api/servers/${server.id}/metrics`);
        if (!active) return;
        setMetrics(m);
        setHistory((prev) => [
          ...prev.slice(-24),
          {
            t: new Date().toLocaleTimeString([], { hour12: false, minute: '2-digit', second: '2-digit' }),
            cpu: Math.round(m.cpuPercent * 10) / 10,
            ram: Math.round(m.memoryUsedMB),
          },
        ]);
      } catch {
        // ignore transient poll errors
      }
    };
    poll();
    const timer = setInterval(poll, 2500);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [server?.id]);

  const copyAddr = async (key: string, val: string) => {
    await navigator.clipboard.writeText(val);
    setCopiedKey(key);
    setTimeout(() => setCopiedKey(''), 1800);
  };

  const runAction = async (action: 'start' | 'stop' | 'restart' | 'install') => {
    if (!server) return;
    setBusy(true);
    setError('');
    try {
      await apiFetch(`/api/servers/${server.id}/${action}`, { method: 'POST' });
      onRefresh();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Islem basarisiz');
    } finally {
      setBusy(false);
    }
  };

  const quickPlayerCmd = async (cmd: string) => {
    if (!server) return;
    await apiFetch(`/api/servers/${server.id}/command`, {
      method: 'POST',
      body: JSON.stringify({ command: cmd }),
    });
  };

  if (!server) {
    return (
      <div className="glass-panel flex min-h-[420px] flex-col items-center justify-center rounded-3xl p-8 text-center">
        <div className="mb-4 rounded-2xl bg-gradient-to-br from-emerald-500/20 to-cyan-500/20 p-5 text-emerald-400">
          <Server className="h-12 w-12" />
        </div>
        <h2 className="text-2xl font-bold text-white">{t.dashboard.noServerTitle}</h2>
        <p className="mt-2 max-w-md text-sm text-slate-400">{t.dashboard.noServerDesc}</p>
        <button
          onClick={onOpenCreate}
          className="mt-6 flex items-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-6 py-3 font-semibold text-slate-950 shadow-lg shadow-emerald-500/25 hover:brightness-110"
        >
          <Plus className="h-5 w-5" />
          {t.actions.createServer}
        </button>
      </div>
    );
  }

  const status = metrics?.status || server.status;
  const isOnline = status === 'online';
  const isBusyState = status === 'starting' || status === 'stopping' || status === 'installing';
  const localIP = system?.localIP || '127.0.0.1';
  const javaLocal = `${localIP}:${server.javaPort}`;
  const bedrockLocal = `${localIP}:${server.crossPlay?.enabled ? server.crossPlay.bedrockPort : server.bedrockPort}`;
  const showJava = server.engine !== 'bds' && server.engine !== 'powernukkitx';
  const showBedrock = server.crossPlay?.enabled || server.engine === 'bds' || server.engine === 'powernukkitx';

  const statusColors: Record<string, string> = {
    online: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/40',
    starting: 'bg-amber-500/20 text-amber-300 border-amber-500/40',
    installing: 'bg-cyan-500/20 text-cyan-300 border-cyan-500/40',
    stopping: 'bg-orange-500/20 text-orange-300 border-orange-500/40',
    error: 'bg-rose-500/20 text-rose-300 border-rose-500/40',
    offline: 'bg-slate-700/40 text-slate-300 border-white/10',
  };

  return (
    <div className="space-y-5">
      {error && (
        <div className="flex items-center gap-2 rounded-2xl border border-rose-500/30 bg-rose-500/10 px-4 py-3 text-sm text-rose-300">
          <ShieldAlert className="h-4 w-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* Hero Server Bar */}
      <div className="glass-panel relative overflow-hidden rounded-3xl p-6">
        <div className="flex flex-col justify-between gap-5 lg:flex-row lg:items-center">
          <div>
            <div className="flex flex-wrap items-center gap-2.5">
              <h1 className="text-2xl font-bold text-white">{server.name}</h1>
              <span className={`inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-semibold ${statusColors[status] || statusColors.offline}`}>
                <span className={`h-2 w-2 rounded-full ${isOnline ? 'bg-emerald-400 animate-pulse' : 'bg-current'}`} />
                {t.status[status as keyof typeof t.status] || status}
              </span>
              {server.crossPlay?.enabled && (
                <span className="rounded-full border border-cyan-500/30 bg-cyan-500/15 px-3 py-1 text-xs font-medium text-cyan-300">
                  Cross-Play (Java + Bedrock)
                </span>
              )}
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-4 text-xs text-slate-400">
              <span>Motor: <strong className="text-slate-200 uppercase">{server.engine}</strong></span>
              <span>Surum: <strong className="text-slate-200">{server.mcVersion}</strong></span>
              <span>Dunya: <strong className="text-slate-200">{server.activeWorld}</strong></span>
              {isOnline && metrics && (
                <span>Calisma Suresi: <strong className="text-emerald-300">{formatUptime(metrics.uptimeSec)}</strong></span>
              )}
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2.5">
            {status === 'offline' || status === 'error' ? (
              <button
                disabled={busy}
                onClick={() => runAction('start')}
                className="flex items-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-5 py-2.5 text-sm font-semibold text-slate-950 shadow-lg shadow-emerald-500/20 hover:brightness-110 disabled:opacity-50"
              >
                <Play className="h-4 w-4 fill-current" />
                {t.actions.start}
              </button>
            ) : (
              <button
                disabled={busy || status === 'installing'}
                onClick={() => runAction('stop')}
                className="flex items-center gap-2 rounded-2xl bg-rose-500/20 border border-rose-500/40 px-5 py-2.5 text-sm font-semibold text-rose-200 hover:bg-rose-500/30 disabled:opacity-50"
              >
                <Square className="h-4 w-4 fill-current" />
                {t.actions.stop}
              </button>
            )}
            <button
              disabled={busy || isBusyState}
              onClick={() => runAction('restart')}
              className="flex items-center gap-2 rounded-2xl border border-white/10 bg-white/5 px-4 py-2.5 text-sm font-medium text-slate-200 hover:bg-white/10 disabled:opacity-40"
            >
              <RefreshCw className="h-4 w-4" />
              {t.actions.restart}
            </button>
            <button
              disabled={busy || isOnline || isBusyState}
              onClick={() => runAction('install')}
              className="flex items-center gap-2 rounded-2xl border border-white/10 bg-white/5 px-4 py-2.5 text-sm font-medium text-slate-200 hover:bg-white/10 disabled:opacity-40"
            >
              <Download className="h-4 w-4" />
              {t.actions.install}
            </button>
          </div>
        </div>

        {server.lastError && (
          <div className="mt-4 rounded-xl border border-rose-500/30 bg-rose-500/10 px-4 py-2 text-xs text-rose-300">
            Son Hata: {server.lastError}
          </div>
        )}
      </div>

      {/* Connection Cards */}
      <div className="grid gap-4 md:grid-cols-2">
        {showJava && (
          <div className="glass-card rounded-2xl p-5">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-sm font-semibold text-emerald-300">
                <Wifi className="h-4 w-4" />
                {t.dashboard.javaAddr}
              </div>
              <span className="rounded-lg bg-emerald-500/15 px-2 py-0.5 font-mono text-xs text-emerald-300">TCP {server.javaPort}</span>
            </div>
            <div className="mt-3 space-y-2">
              <AddressRow
                label={t.dashboard.localLan}
                value={javaLocal}
                copied={copiedKey === 'jlan'}
                onCopy={() => copyAddr('jlan', javaLocal)}
                onQr={() => setQrValue({ title: 'Java LAN Adresi', value: javaLocal })}
              />
              {tunnel?.javaAddress ? (
                <AddressRow
                  label={t.dashboard.publicTunnel}
                  value={tunnel.javaAddress}
                  highlight
                  copied={copiedKey === 'jpub'}
                  onCopy={() => copyAddr('jpub', tunnel.javaAddress)}
                  onQr={() => setQrValue({ title: 'Java Dis Baglanti', value: tunnel.javaAddress })}
                />
              ) : (
                <button
                  onClick={() => onNavigate('network')}
                  className="flex w-full items-center justify-between rounded-xl border border-dashed border-white/15 px-3 py-2 text-xs text-slate-400 hover:border-cyan-500/40 hover:text-cyan-300"
                >
                  <span>{t.dashboard.tunnelNotActive}</span>
                  <Globe className="h-3.5 w-3.5" />
                </button>
              )}
            </div>
          </div>
        )}

        {showBedrock && (
          <div className="glass-card rounded-2xl p-5">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-sm font-semibold text-cyan-300">
                <Globe className="h-4 w-4" />
                {t.dashboard.bedrockAddr}
              </div>
              <span className="rounded-lg bg-cyan-500/15 px-2 py-0.5 font-mono text-xs text-cyan-300">
                UDP {server.crossPlay?.enabled ? server.crossPlay.bedrockPort : server.bedrockPort}
              </span>
            </div>
            <div className="mt-3 space-y-2">
              <AddressRow
                label={t.dashboard.localLan}
                value={bedrockLocal}
                copied={copiedKey === 'blan'}
                onCopy={() => copyAddr('blan', bedrockLocal)}
                onQr={() => setQrValue({ title: 'Bedrock LAN Adresi', value: bedrockLocal })}
              />
              {tunnel?.bedrockAddress ? (
                <AddressRow
                  label={t.dashboard.publicTunnel}
                  value={tunnel.bedrockAddress}
                  highlight
                  copied={copiedKey === 'bpub'}
                  onCopy={() => copyAddr('bpub', tunnel.bedrockAddress)}
                  onQr={() => setQrValue({ title: 'Bedrock Dis Baglanti', value: tunnel.bedrockAddress })}
                />
              ) : (
                <button
                  onClick={() => onNavigate('network')}
                  className="flex w-full items-center justify-between rounded-xl border border-dashed border-white/15 px-3 py-2 text-xs text-slate-400 hover:border-cyan-500/40 hover:text-cyan-300"
                >
                  <span>{t.dashboard.tunnelNotActive}</span>
                  <Globe className="h-3.5 w-3.5" />
                </button>
              )}
            </div>
          </div>
        )}
      </div>

      {/* Metrics & Sparklines */}
      <div className="grid gap-4 md:grid-cols-3">
        <div className="glass-card rounded-2xl p-5">
          <div className="flex items-center justify-between text-xs text-slate-400">
            <span className="flex items-center gap-1.5"><Cpu className="h-4 w-4 text-emerald-400" /> {t.dashboard.cpuUsage}</span>
            <span className="font-mono text-base font-bold text-white">{metrics ? `${metrics.cpuPercent.toFixed(1)}%` : '0%'}</span>
          </div>
          <div className="mt-3 h-20">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={history}>
                <defs>
                  <linearGradient id="cpuGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#10b981" stopOpacity={0.5} />
                    <stop offset="95%" stopColor="#10b981" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <Tooltip contentStyle={{ background: '#0f172a', border: '1px solid rgba(255,255,255,0.1)', borderRadius: 10, fontSize: 12 }} />
                <Area type="monotone" dataKey="cpu" stroke="#10b981" strokeWidth={2} fill="url(#cpuGrad)" isAnimationActive={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>

        <div className="glass-card rounded-2xl p-5">
          <div className="flex items-center justify-between text-xs text-slate-400">
            <span className="flex items-center gap-1.5"><HardDrive className="h-4 w-4 text-cyan-400" /> {t.dashboard.ramUsage}</span>
            <span className="font-mono text-base font-bold text-white">
              {metrics ? `${Math.round(metrics.memoryUsedMB)} / ${metrics.memoryMaxMB} MB` : `0 / ${server.memoryMB} MB`}
            </span>
          </div>
          <div className="mt-3 h-20">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={history}>
                <defs>
                  <linearGradient id="ramGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#06b6d4" stopOpacity={0.5} />
                    <stop offset="95%" stopColor="#06b6d4" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <Tooltip contentStyle={{ background: '#0f172a', border: '1px solid rgba(255,255,255,0.1)', borderRadius: 10, fontSize: 12 }} />
                <Area type="monotone" dataKey="ram" stroke="#06b6d4" strokeWidth={2} fill="url(#ramGrad)" isAnimationActive={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>

        <div className="glass-card flex flex-col justify-between rounded-2xl p-5">
          <div className="flex items-center justify-between text-xs text-slate-400">
            <span className="flex items-center gap-1.5"><Zap className="h-4 w-4 text-violet-400" /> {t.dashboard.tpsTitle}</span>
            <span className="font-mono text-base font-bold text-violet-300">{isOnline && metrics ? metrics.tps.toFixed(1) : '--'}</span>
          </div>
          <div className="my-3 space-y-2 text-xs text-slate-400">
            <div className="flex justify-between">
              <span>{t.dashboard.playersTitle}</span>
              <span className="font-semibold text-white">{metrics?.playerCount ?? 0} / {metrics?.maxPlayers ?? 20}</span>
            </div>
            <div className="flex justify-between">
              <span>{t.dashboard.sysHost}</span>
              <span className="text-slate-200">{system ? `${system.os}/${system.arch} (${system.ramFreeMB} MB bos)` : '...'}</span>
            </div>
            <div className="flex justify-between">
              <span>Java</span>
              <span className={system?.javaDetected ? 'text-emerald-300' : 'text-amber-300'}>
                {system?.javaDetected ? `v${system.javaVersion}` : 'Kurulu degil'}
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* Players Panel */}
      <div className="glass-card rounded-2xl p-5">
        <div className="mb-3 flex items-center justify-between">
          <div className="flex items-center gap-2 text-sm font-semibold text-white">
            <Users className="h-4 w-4 text-emerald-400" />
            {t.dashboard.playersTitle}
          </div>
          <span className="text-xs text-slate-400">{metrics?.playerCount ?? 0} aktif</span>
        </div>
        {!metrics?.players || metrics.players.length === 0 ? (
          <p className="py-4 text-center text-xs text-slate-500">{t.dashboard.noPlayers}</p>
        ) : (
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {metrics.players.map((p) => (
              <div key={p.name} className="flex items-center justify-between rounded-xl border border-white/10 bg-slate-950/60 px-3 py-2">
                <div>
                  <div className="text-sm font-medium text-white">{p.name}</div>
                  <div className="text-[10px] uppercase tracking-wider text-cyan-400">{p.platform}</div>
                </div>
                <div className="flex gap-1">
                  <button
                    onClick={() => quickPlayerCmd(`op ${p.name}`)}
                    className="rounded-lg bg-emerald-500/15 px-2 py-1 text-[11px] text-emerald-300 hover:bg-emerald-500/25"
                  >
                    OP
                  </button>
                  <button
                    onClick={() => quickPlayerCmd(`kick ${p.name}`)}
                    className="rounded-lg bg-rose-500/15 px-2 py-1 text-[11px] text-rose-300 hover:bg-rose-500/25"
                  >
                    Kick
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <QrShareModal
        open={Boolean(qrValue)}
        onClose={() => setQrValue(null)}
        title={qrValue?.title || ''}
        subtitle="Arkadaslarinla paylas veya mobilden kopyala"
        value={qrValue?.value || ''}
      />
    </div>
  );
};

const AddressRow: React.FC<{
  label: string;
  value: string;
  highlight?: boolean;
  copied: boolean;
  onCopy: () => void;
  onQr: () => void;
}> = ({ label, value, highlight, copied, onCopy, onQr }) => (
  <div className={`flex items-center justify-between gap-2 rounded-xl border px-3 py-2 ${highlight ? 'border-emerald-500/30 bg-emerald-500/10' : 'border-white/10 bg-slate-950/60'}`}>
    <div className="min-w-0">
      <div className="text-[10px] uppercase tracking-wider text-slate-400">{label}</div>
      <div className="truncate font-mono text-xs text-white">{value}</div>
    </div>
    <div className="flex shrink-0 items-center gap-1">
      <button onClick={onQr} className="rounded-lg p-1.5 text-slate-400 hover:bg-white/10 hover:text-white" title="QR Kod">
        <QrCode className="h-3.5 w-3.5" />
      </button>
      <button onClick={onCopy} className="flex items-center gap-1 rounded-lg bg-white/10 px-2.5 py-1 text-xs font-medium text-white hover:bg-white/20">
        {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
    </div>
  </div>
);
