import React, { useEffect, useState } from 'react';
import {
  ClipboardPaste,
  Copy,
  Check,
  Cpu,
  Download,
  Globe,
  HardDrive,
  Loader2,
  Play,
  Plus,
  QrCode,
  Radio,
  RefreshCw,
  Server,
  ShieldAlert,
  Square,
  Users,
  Wifi,
  Zap,
} from 'lucide-react';
import { Area, AreaChart, ResponsiveContainer, Tooltip } from 'recharts';
import { apiFetch, copyTextSafe, formatUptime, pasteTextSafe } from '../lib/api';
import { Lang, translations } from '../lib/i18n';
import { AutoTunnelResult, BoreState, E4mcState, P2PState, PinggyState, ServerInstance, ServerMetrics, SystemOverview, TunnelState } from '../types';
import { QrShareModal } from '../components/QrShareModal';

interface Props {
  server: ServerInstance | null;
  system: SystemOverview | null;
  tunnel: TunnelState | null;
  bore: BoreState | null;
  e4mc: E4mcState | null;
  pinggy: PinggyState | null;
  p2p: P2PState | null;
  lang: Lang;
  onRefresh: () => void;
  onRefreshTunnel: () => void;
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
  bore,
  e4mc,
  pinggy,
  p2p,
  lang,
  onRefresh,
  onRefreshTunnel,
  onOpenCreate,
  onNavigate,
}) => {
  const t = translations[lang];
  const td = t.dashboard;
  const tn = t.network;
  const [metrics, setMetrics] = useState<ServerMetrics | null>(null);
  const [history, setHistory] = useState<MetricPoint[]>([]);
  const [busy, setBusy] = useState(false);
  const [quickOpeningTunnel, setQuickOpeningTunnel] = useState(false);
  const [joinCode, setJoinCode] = useState('');
  const [joiningP2P, setJoiningP2P] = useState(false);
  const [p2pErr, setP2PErr] = useState('');
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
    await copyTextSafe(val);
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
      setError(e instanceof Error ? e.message : td.actionFailed);
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

  const quickJoinP2P = async () => {
    const code = joinCode.trim();
    if (!code) return;
    setJoiningP2P(true);
    setP2PErr('');
    try {
      await apiFetch<P2PState>('/api/network/p2p/join', {
        method: 'POST',
        body: JSON.stringify({ roomCode: code, preferredJavaPort: 25565, preferredBedrockPort: 19132 }),
      });
      onRefreshTunnel();
    } catch (e: unknown) {
      setP2PErr(e instanceof Error ? e.message : tn.p2pErr);
    } finally {
      setJoiningP2P(false);
    }
  };

  const stopP2P = async () => {
    setJoiningP2P(true);
    setP2PErr('');
    try {
      await apiFetch('/api/network/p2p/stop', { method: 'POST' });
      onRefreshTunnel();
    } finally {
      setJoiningP2P(false);
    }
  };

  if (!server) {
    return (
      <div className="grid gap-4 lg:grid-cols-2">
        <div className="glass-panel flex min-h-[380px] flex-col items-center justify-center rounded-3xl p-8 text-center">
          <div className="mb-4 rounded-2xl bg-gradient-to-br from-emerald-500/20 to-cyan-500/20 p-5 text-emerald-400">
            <Server className="h-12 w-12" />
          </div>
          <h2 className="text-2xl font-bold text-white">{td.noServerTitle}</h2>
          <p className="mt-2 max-w-md text-sm text-slate-400">{td.noServerDesc}</p>
          <button
            onClick={onOpenCreate}
            className="mt-6 flex items-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-6 py-3 font-semibold text-slate-950 shadow-lg shadow-emerald-500/25 hover:brightness-110"
          >
            <Plus className="h-5 w-5" />
            {t.actions.createServer}
          </button>
        </div>

        {/* Join Friend P2P without needing to create any local server */}
        <div className="glass-panel flex min-h-[380px] flex-col justify-center rounded-3xl border border-violet-500/30 p-7">
          <div className="flex items-center gap-3">
            <div className="rounded-2xl bg-violet-500/20 p-3 text-violet-300">
              <Radio className="h-7 w-7" />
            </div>
            <div>
              <h2 className="text-xl font-bold text-white">{td.p2pNoServerTitle}</h2>
              <p className="mt-0.5 text-xs text-slate-400">{td.p2pNoServerDesc}</p>
            </div>
          </div>

          {p2p?.running && p2p.mode === 'join' ? (
            <div className="mt-5 space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2 rounded-2xl border border-emerald-500/40 bg-emerald-500/15 px-4 py-2.5 text-xs">
                <span className="font-bold text-emerald-200">
                  ● {tn.p2pConnectedBanner} ({p2p.roomCode})
                </span>
                <span className="font-mono font-bold text-emerald-300">{p2p.latencyMs || 1} ms</span>
              </div>
              {p2p.localJavaAddr && (
                <AddressRow
                  label={tn.p2pLocalJavaLabel}
                  value={p2p.localJavaAddr}
                  highlight
                  copied={copiedKey === 'empty-p2p-j'}
                  onCopy={() => copyAddr('empty-p2p-j', p2p.localJavaAddr!)}
                  onQr={() => setQrValue({ title: tn.p2pLocalJavaLabel, value: p2p.localJavaAddr! })}
                />
              )}
              {p2p.localBedrockAddr && (
                <AddressRow
                  label={tn.p2pLocalBedrockLabel}
                  value={p2p.localBedrockAddr}
                  highlight
                  copied={copiedKey === 'empty-p2p-b'}
                  onCopy={() => copyAddr('empty-p2p-b', p2p.localBedrockAddr!)}
                  onQr={() => setQrValue({ title: tn.p2pLocalBedrockLabel, value: p2p.localBedrockAddr! })}
                />
              )}
              <p className="text-[11px] text-slate-300">{tn.p2pJoinHowTo}</p>
              <button
                disabled={joiningP2P}
                onClick={stopP2P}
                className="flex w-full items-center justify-center gap-2 rounded-2xl border border-rose-500/40 bg-rose-500/20 px-4 py-2.5 text-sm font-semibold text-rose-200 hover:bg-rose-500/30"
              >
                <Square className="h-4 w-4 fill-current" />
                {tn.p2pDisconnectBtn}
              </button>
            </div>
          ) : (
            <div className="mt-5 space-y-3">
              <div className="flex gap-2">
                <input
                  value={joinCode}
                  onChange={(e) => setJoinCode(e.target.value.toUpperCase())}
                  placeholder={tn.p2pJoinPlaceholder}
                  className="flex-1 rounded-2xl border border-white/15 bg-slate-950/90 px-4 py-3 text-center font-mono text-base font-bold tracking-widest text-white outline-none focus:border-violet-400"
                />
                <button
                  type="button"
                  onClick={async () => {
                    const clip = await pasteTextSafe();
                    if (clip) setJoinCode(clip.trim().toUpperCase());
                  }}
                  className="flex items-center gap-1.5 rounded-2xl border border-white/15 bg-white/5 px-3.5 py-3 text-xs font-semibold text-slate-200 hover:bg-white/10"
                >
                  <ClipboardPaste className="h-4 w-4" />
                  {tn.p2pPasteBtn}
                </button>
              </div>
              <button
                disabled={joiningP2P || !joinCode.trim()}
                onClick={quickJoinP2P}
                className="flex w-full items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-violet-500 to-cyan-500 px-5 py-3 text-sm font-bold text-slate-950 shadow-lg shadow-violet-500/20 hover:brightness-110 disabled:opacity-50"
              >
                {joiningP2P ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4 fill-current" />}
                {joiningP2P ? tn.p2pConnectingBtn : tn.p2pJoinBtn}
              </button>
              {(p2pErr || p2p?.lastError) && (
                <div className="rounded-xl border border-rose-500/30 bg-rose-500/10 px-3.5 py-2 text-xs text-rose-300">
                  {p2pErr || p2p?.lastError}
                </div>
              )}
            </div>
          )}
        </div>

        <QrShareModal
          open={Boolean(qrValue)}
          onClose={() => setQrValue(null)}
          title={qrValue?.title || ''}
          subtitle={td.qrShareSubtitle}
          value={qrValue?.value || ''}
          lang={lang}
        />
      </div>
    );
  }

  const status = metrics?.status || server.status;
  const isOnline = status === 'online';
  const isBusyState = status === 'starting' || status === 'stopping' || status === 'installing';
  const localIP = system?.localIP || '127.0.0.1';
  const javaHostLocal = `localhost:${server.javaPort}`;
  const javaLocal = `${localIP}:${server.javaPort}`;
  const bedrockLocal = `${localIP}:${server.crossPlay?.enabled ? server.crossPlay.bedrockPort : server.bedrockPort}`;
  const showJava = server.engine !== 'bds' && server.engine !== 'powernukkitx';
  const showBedrock = server.crossPlay?.enabled || server.engine === 'bds' || server.engine === 'powernukkitx';

  const e4mcActive = Boolean(e4mc?.running && e4mc.domain);
  const pinggyActive = Boolean(pinggy?.running && pinggy.address);
  const playitJavaActive = Boolean(tunnel?.running && tunnel.javaAddress);
  const boreActive = Boolean(bore?.running && bore.address);

  const publicJava =
    (e4mcActive && e4mc!.domain) ||
    (pinggyActive && pinggy!.address) ||
    (playitJavaActive && tunnel!.javaAddress) ||
    (boreActive && bore!.address) ||
    '';

  let publicJavaLabel: string = td.publicTunnel;
  if (e4mcActive) {
    const pingText = e4mc!.latencyMs > 0 ? ` • ${e4mc!.latencyMs} ms` : '';
    publicJavaLabel = `${td.friendsLabelPrefix} (e4mc • ${e4mc!.region || 'EU'}${pingText})`;
  } else if (pinggyActive) {
    const pingText = pinggy!.latencyMs ? ` • ${pinggy!.latencyMs} ms` : '';
    publicJavaLabel = `${td.friendsLabelPrefix} (pinggy.io • ${pinggy!.region || 'EU'}${pingText})`;
  } else if (playitJavaActive) {
    publicJavaLabel = `${td.friendsLabelPrefix} (playit.gg)`;
  } else if (boreActive) {
    const pingText = bore!.latencyMs ? ` • ${bore!.latencyMs} ms` : '';
    publicJavaLabel = `${td.friendsLabelPrefix} (bore.pub${pingText})`;
  }

  const publicBedrock = (tunnel?.running && tunnel.bedrockAddress) || '';
  const publicBedrockLabel: string = td.publicTunnel;

  const startQuickE4mc = async () => {
    setQuickOpeningTunnel(true);
    setError('');
    try {
      const localBedrockPort = server.crossPlay?.enabled ? server.crossPlay.bedrockPort : server.bedrockPort;
      await apiFetch<AutoTunnelResult>('/api/network/tunnel/auto-start', {
        method: 'POST',
        body: JSON.stringify({
          localJavaPort: server.javaPort,
          localBedrockPort,
          wantJava: showJava,
          wantBedrock: showBedrock,
        }),
      });
      onRefresh();
    } catch (e: unknown) {
      const reason = e instanceof Error ? e.message : td.actionFailed;
      setError(`${reason} ${td.quickE4mcErrSuffix}`);
    } finally {
      setQuickOpeningTunnel(false);
    }
  };

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
                  {td.crossplayBadge}
                </span>
              )}
              <span
                className={`rounded-full border px-3 py-1 text-xs font-medium ${
                  server.offlineMode
                    ? 'border-amber-500/35 bg-amber-500/15 text-amber-300'
                    : 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                }`}
              >
                {server.offlineMode ? td.offlineBadge : td.premiumBadge}
              </span>
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-4 text-xs text-slate-400">
              <span>{td.engineLabel} <strong className="text-slate-200 uppercase">{server.engine}</strong></span>
              <span>{td.versionLabel} <strong className="text-slate-200">{server.mcVersion}</strong></span>
              <span>{td.worldLabel} <strong className="text-slate-200">{server.activeWorld}</strong></span>
              {isOnline && metrics && (
                <span>{td.uptimeLabel} <strong className="text-emerald-300">{formatUptime(metrics.uptimeSec)}</strong></span>
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
            {td.lastErrorLabel} {server.lastError}
          </div>
        )}
      </div>

      {/* Active Direct P2P Status Banner */}
      {p2p?.running && (
        <div className="glass-panel rounded-2xl border border-violet-500/35 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-2.5">
              <Radio className="h-4 w-4 text-violet-400" />
              <span className="text-sm font-bold text-white">
                {p2p.mode === 'host' ? td.p2pHostBannerTitle : td.p2pJoinBannerTitle}
              </span>
              {p2p.roomCode && (
                <span className="rounded-lg border border-violet-500/40 bg-violet-500/20 px-2.5 py-0.5 font-mono text-xs font-bold tracking-wider text-violet-200">
                  {p2p.roomCode}
                </span>
              )}
              {p2p.mode === 'host' ? (
                <span className="rounded-full bg-emerald-500/15 px-2.5 py-0.5 text-[11px] font-semibold text-emerald-300">
                  {p2p.peerCount} {td.activeSuffix}
                </span>
              ) : (
                <span className="rounded-full bg-emerald-500/15 px-2.5 py-0.5 font-mono text-[11px] font-semibold text-emerald-300">
                  {p2p.latencyMs || 1} ms
                </span>
              )}
            </div>
            <div className="flex items-center gap-2">
              {p2p.mode === 'host' && p2p.roomCode && (
                <button
                  onClick={() => copyAddr('p2p-code', p2p.roomCode!)}
                  className="flex items-center gap-1 rounded-xl bg-violet-500 px-3 py-1.5 text-xs font-bold text-slate-950 hover:brightness-110"
                >
                  {copiedKey === 'p2p-code' ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                  {copiedKey === 'p2p-code' ? t.actions.copied : t.actions.copy}
                </button>
              )}
              {p2p.mode === 'join' && p2p.localJavaAddr && (
                <button
                  onClick={() => copyAddr('p2p-jaddr', p2p.localJavaAddr!)}
                  className="flex items-center gap-1 rounded-xl bg-emerald-500 px-3 py-1.5 text-xs font-bold text-slate-950 hover:brightness-110"
                >
                  {copiedKey === 'p2p-jaddr' ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                  {p2p.localJavaAddr}
                </button>
              )}
              {p2p.mode === 'join' && p2p.localBedrockAddr && (
                <button
                  onClick={() => copyAddr('p2p-baddr', p2p.localBedrockAddr!)}
                  className="flex items-center gap-1 rounded-xl bg-cyan-500 px-3 py-1.5 text-xs font-bold text-slate-950 hover:brightness-110"
                >
                  {copiedKey === 'p2p-baddr' ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                  {p2p.localBedrockAddr}
                </button>
              )}
              <button
                onClick={() => onNavigate('network')}
                className="rounded-xl border border-white/15 bg-white/5 px-3 py-1.5 text-xs font-medium text-slate-200 hover:bg-white/10"
              >
                {td.p2pManageBtn}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Connection Cards */}
      <div className="grid gap-4 md:grid-cols-2">
        {showJava && (
          <div className="glass-card rounded-2xl p-5">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 text-sm font-semibold text-emerald-300">
                <Wifi className="h-4 w-4" />
                {td.javaAddr}
              </div>
              <span className="rounded-lg bg-emerald-500/15 px-2 py-0.5 font-mono text-xs text-emerald-300">TCP {server.javaPort}</span>
            </div>
            <div className="mt-3 space-y-2">
              <AddressRow
                label={td.hostLocalLabel}
                value={javaHostLocal}
                badge={td.hostLocalBadge}
                copied={copiedKey === 'jhost'}
                onCopy={() => copyAddr('jhost', javaHostLocal)}
                onQr={() => setQrValue({ title: td.qrTitleHost, value: javaHostLocal })}
              />
              <AddressRow
                label={td.lanAddrLabel}
                value={javaLocal}
                copied={copiedKey === 'jlan'}
                onCopy={() => copyAddr('jlan', javaLocal)}
                onQr={() => setQrValue({ title: td.qrTitleJavaLan, value: javaLocal })}
              />
              {publicJava ? (
                <>
                  <AddressRow
                    label={publicJavaLabel}
                    value={publicJava}
                    highlight
                    copied={copiedKey === 'jpub'}
                    onCopy={() => copyAddr('jpub', publicJava)}
                    onQr={() => setQrValue({ title: td.qrTitleJavaPub, value: publicJava })}
                  />
                  <p className="px-1 text-[11px] text-cyan-200/80">
                    {td.hostTip1} <code className="font-mono text-emerald-300">{javaHostLocal}</code> {td.hostTip2}
                  </p>
                </>
              ) : (
                <button
                  disabled={quickOpeningTunnel}
                  onClick={startQuickE4mc}
                  className="flex w-full items-center justify-between rounded-xl border border-emerald-500/35 bg-emerald-500/10 px-3 py-2 text-xs font-semibold text-emerald-300 hover:bg-emerald-500/20 disabled:opacity-50"
                >
                  <span>{quickOpeningTunnel ? td.quickE4mcOpening : td.quickE4mcBtn}</span>
                  <Zap className="h-3.5 w-3.5 shrink-0" />
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
                {td.bedrockAddr}
              </div>
              <span className="rounded-lg bg-cyan-500/15 px-2 py-0.5 font-mono text-xs text-cyan-300">
                UDP {server.crossPlay?.enabled ? server.crossPlay.bedrockPort : server.bedrockPort}
              </span>
            </div>
            <div className="mt-3 space-y-2">
              <AddressRow
                label={td.localLan}
                value={bedrockLocal}
                copied={copiedKey === 'blan'}
                onCopy={() => copyAddr('blan', bedrockLocal)}
                onQr={() => setQrValue({ title: td.qrTitleBedrockLan, value: bedrockLocal })}
              />
              {publicBedrock ? (
                <AddressRow
                  label={publicBedrockLabel}
                  value={publicBedrock}
                  highlight
                  copied={copiedKey === 'bpub'}
                  onCopy={() => copyAddr('bpub', publicBedrock)}
                  onQr={() => setQrValue({ title: td.qrTitleBedrockPub, value: publicBedrock })}
                />
              ) : (
                <button
                  onClick={() => onNavigate('network')}
                  className="flex w-full items-center justify-between rounded-xl border border-dashed border-white/15 px-3 py-2 text-xs text-slate-400 hover:border-cyan-500/40 hover:text-cyan-300"
                >
                  <span>{td.tunnelNotActive}</span>
                  <Globe className="h-3.5 w-3.5 shrink-0" />
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
            <span className="flex items-center gap-1.5"><Cpu className="h-4 w-4 text-emerald-400" /> {td.cpuUsage}</span>
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
            <span className="flex items-center gap-1.5"><HardDrive className="h-4 w-4 text-cyan-400" /> {td.ramUsage}</span>
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
            <span className="flex items-center gap-1.5"><Zap className="h-4 w-4 text-violet-400" /> {td.tpsTitle}</span>
            <span className="font-mono text-base font-bold text-violet-300">{isOnline && metrics ? metrics.tps.toFixed(1) : '--'}</span>
          </div>
          <div className="my-3 space-y-2 text-xs text-slate-400">
            <div className="flex justify-between">
              <span>{td.playersTitle}</span>
              <span className="font-semibold text-white">{metrics?.playerCount ?? 0} / {metrics?.maxPlayers ?? 20}</span>
            </div>
            <div className="flex justify-between">
              <span>{td.sysHost}</span>
              <span className="text-slate-200">{system ? `${system.os}/${system.arch} (${system.ramFreeMB} MB ${td.freeSuffix})` : '...'}</span>
            </div>
            <div className="flex justify-between">
              <span>Java</span>
              <span className={system?.javaDetected ? 'text-emerald-300' : 'text-amber-300'}>
                {system?.javaDetected ? `${td.javaReadyPrefix}${system.javaVersion}` : td.javaNotInstalled}
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
            {td.playersTitle}
          </div>
          <span className="text-xs text-slate-400">{metrics?.playerCount ?? 0} {td.activeSuffix}</span>
        </div>
        {!metrics?.players || metrics.players.length === 0 ? (
          <p className="py-4 text-center text-xs text-slate-500">{td.noPlayers}</p>
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
                    {td.opBtn}
                  </button>
                  <button
                    onClick={() => quickPlayerCmd(`kick ${p.name}`)}
                    className="rounded-lg bg-rose-500/15 px-2 py-1 text-[11px] text-rose-300 hover:bg-rose-500/25"
                  >
                    {td.kickBtn}
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
        subtitle={td.qrShareSubtitle}
        value={qrValue?.value || ''}
        lang={lang}
      />
    </div>
  );
};

const AddressRow: React.FC<{
  label: string;
  value: string;
  badge?: string;
  highlight?: boolean;
  copied: boolean;
  onCopy: () => void;
  onQr: () => void;
}> = ({ label, value, badge, highlight, copied, onCopy, onQr }) => (
  <div className={`flex items-center justify-between gap-2 rounded-xl border px-3 py-2 ${highlight ? 'border-emerald-500/30 bg-emerald-500/10' : 'border-white/10 bg-slate-950/60'}`}>
    <div className="min-w-0">
      <div className="flex items-center gap-1.5 text-[10px] uppercase tracking-wider text-slate-400">
        <span>{label}</span>
        {badge && (
          <span className="rounded bg-emerald-500/20 px-1.5 py-0.2 font-mono text-[9px] font-bold text-emerald-300">{badge}</span>
        )}
      </div>
      <div className="truncate font-mono text-xs text-white">{value}</div>
    </div>
    <div className="flex shrink-0 items-center gap-1">
      <button onClick={onQr} className="rounded-lg p-1.5 text-slate-400 hover:bg-white/10 hover:text-white" title="QR">
        <QrCode className="h-3.5 w-3.5" />
      </button>
      <button onClick={onCopy} className="flex items-center gap-1 rounded-lg bg-white/10 px-2.5 py-1 text-xs font-medium text-white hover:bg-white/20">
        {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
    </div>
  </div>
);
