import React, { useEffect, useState } from 'react';
import {
  Activity,
  Check,
  ChevronDown,
  ChevronUp,
  ClipboardPaste,
  Copy,
  Download,
  ExternalLink,
  Globe,
  Info,
  Loader2,
  Play,
  QrCode,
  Radio,
  RefreshCw,
  Router,
  ShieldCheck,
  Smartphone,
  Square,
  Users,
  Zap,
} from 'lucide-react';
import { apiFetch, copyTextSafe, pasteTextSafe } from '../lib/api';
import { Lang, translations } from '../lib/i18n';
import {
  AppConfig,
  BoreState,
  E4mcState,
  NetworkDiagnostics,
  P2PState,
  PinggyState,
  ServerInstance,
  SystemOverview,
  TunnelState,
} from '../types';
import { QrShareModal } from '../components/QrShareModal';

interface Props {
  server: ServerInstance | null;
  system: SystemOverview | null;
  config: AppConfig | null;
  tunnel: TunnelState | null;
  bore: BoreState | null;
  e4mc: E4mcState | null;
  pinggy: PinggyState | null;
  p2p: P2PState | null;
  lang: Lang;
  onRefreshTunnel: () => void;
  onRefreshConfig: () => void;
}

export const NetworkPage: React.FC<Props> = ({
  server,
  system,
  config,
  tunnel,
  bore,
  e4mc,
  pinggy,
  p2p,
  lang,
  onRefreshTunnel,
  onRefreshConfig,
}) => {
  const t = translations[lang].network;
  const [diag, setDiag] = useState<NetworkDiagnostics | null>(null);
  const [loadingDiag, setLoadingDiag] = useState(false);

  const [busyP2P, setBusyP2P] = useState(false);
  const [busyPinggy, setBusyPinggy] = useState(false);
  const [busyPlayit, setBusyPlayit] = useState(false);
  const [busyE4mc, setBusyE4mc] = useState(false);
  const [busyBore, setBusyBore] = useState(false);
  const [busyUpnp, setBusyUpnp] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);

  const [p2pTab, setP2PTab] = useState<'host' | 'join'>(() => {
    if (p2p?.running && p2p.mode === 'join') return 'join';
    if (p2p?.running && p2p.mode === 'host') return 'host';
    return server ? 'host' : 'join';
  });
  const [p2pCodeInput, setP2PCodeInput] = useState('');
  const [p2pNickInput, setP2PNickInput] = useState('');

  const [pinggyRegion, setPinggyRegion] = useState('eu');
  const [pinggyToken, setPinggyToken] = useState('');
  const [boreHost, setBoreHost] = useState('bore.pub');

  const [msg, setMsg] = useState('');
  const [copiedKey, setCopiedKey] = useState('');
  const [qrModal, setQrModal] = useState<{ title: string; subtitle: string; value: string } | null>(null);

  useEffect(() => {
    runDiag();
  }, []);

  useEffect(() => {
    if (p2p?.running && (p2p.mode === 'host' || p2p.mode === 'join')) {
      setP2PTab(p2p.mode);
    }
  }, [p2p?.running, p2p?.mode]);

  const isPureJava = Boolean(
    server && server.engine !== 'bds' && server.engine !== 'powernukkitx' && !server.crossPlay?.enabled,
  );
  const isCrossPlay = Boolean(server && server.crossPlay?.enabled);
  const isPureBedrock = Boolean(server && (server.engine === 'bds' || server.engine === 'powernukkitx'));

  const wantJavaForServer = !server || (server.engine !== 'bds' && server.engine !== 'powernukkitx');
  const wantBedrockForServer = Boolean(!server || server.crossPlay?.enabled || server.engine === 'bds' || server.engine === 'powernukkitx');
  const localJavaPort = server?.javaPort || 25565;
  const localBedrockPort = server?.crossPlay?.enabled ? server.crossPlay.bedrockPort : server?.bedrockPort || 19132;

  const copyText = async (key: string, val: string) => {
    await copyTextSafe(val);
    setCopiedKey(key);
    setTimeout(() => setCopiedKey(''), 1600);
  };

  const runDiag = async () => {
    setLoadingDiag(true);
    try {
      const d = await apiFetch<NetworkDiagnostics>('/api/network/diagnostics');
      setDiag(d);
    } catch {
      // ignore
    } finally {
      setLoadingDiag(false);
    }
  };

  const startP2PHost = async () => {
    setBusyP2P(true);
    setMsg('');
    try {
      const st = await apiFetch<P2PState>('/api/network/p2p/host', {
        method: 'POST',
        body: JSON.stringify({
          localJavaPort,
          localBedrockPort,
          wantJava: wantJavaForServer,
          wantBedrock: wantBedrockForServer,
        }),
      });
      setMsg(`${t.p2pOpenedMsg} ${st.roomCode || ''}`);
      onRefreshTunnel();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.p2pErr);
    } finally {
      setBusyP2P(false);
    }
  };

  const startP2PJoin = async () => {
    if (!p2pCodeInput.trim()) return;
    setBusyP2P(true);
    setMsg('');
    try {
      await apiFetch<P2PState>('/api/network/p2p/join', {
        method: 'POST',
        body: JSON.stringify({
          roomCode: p2pCodeInput.trim(),
          peerName: p2pNickInput.trim(),
          preferredJavaPort: 25565,
          preferredBedrockPort: 19132,
        }),
      });
      setMsg(t.p2pJoinedMsg);
      onRefreshTunnel();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.p2pErr);
    } finally {
      setBusyP2P(false);
    }
  };

  const stopP2P = async () => {
    setBusyP2P(true);
    try {
      await apiFetch('/api/network/p2p/stop', { method: 'POST' });
      onRefreshTunnel();
    } finally {
      setBusyP2P(false);
    }
  };

  const startE4mc = async () => {
    setBusyE4mc(true);
    setMsg('');
    try {
      const st = await apiFetch<E4mcState>('/api/network/e4mc/start', {
        method: 'POST',
        body: JSON.stringify({ localPort: localJavaPort }),
      });
      setMsg(`${t.e4mcOpenedMsg} ${st.domain} (${st.region} • ${st.latencyMs} ms)`);
      onRefreshTunnel();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.e4mcErr);
    } finally {
      setBusyE4mc(false);
    }
  };

  const stopE4mc = async () => {
    setBusyE4mc(true);
    try {
      await apiFetch('/api/network/e4mc/stop', { method: 'POST' });
      onRefreshTunnel();
    } finally {
      setBusyE4mc(false);
    }
  };

  const startPinggy = async () => {
    setBusyPinggy(true);
    setMsg('');
    try {
      const st = await apiFetch<PinggyState>('/api/network/pinggy/start', {
        method: 'POST',
        body: JSON.stringify({
          region: pinggyRegion,
          token: pinggyToken.trim(),
          localPort: localJavaPort,
        }),
      });
      setMsg(`${t.pinggyOpenedMsg} ${st.address}`);
      onRefreshTunnel();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.pinggyErr);
    } finally {
      setBusyPinggy(false);
    }
  };

  const stopPinggy = async () => {
    setBusyPinggy(true);
    try {
      await apiFetch('/api/network/pinggy/stop', { method: 'POST' });
      onRefreshTunnel();
    } finally {
      setBusyPinggy(false);
    }
  };

  const startBore = async () => {
    setBusyBore(true);
    setMsg('');
    try {
      const st = await apiFetch<BoreState>('/api/network/bore/start', {
        method: 'POST',
        body: JSON.stringify({ serverHost: boreHost.trim() || 'bore.pub', localPort: localJavaPort }),
      });
      setMsg(`${t.boreOpenedMsg} ${st.address}`);
      onRefreshTunnel();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.boreErr);
    } finally {
      setBusyBore(false);
    }
  };

  const stopBore = async () => {
    setBusyBore(true);
    try {
      await apiFetch('/api/network/bore/stop', { method: 'POST' });
      onRefreshTunnel();
    } finally {
      setBusyBore(false);
    }
  };

  const installAndStartPlayit = async () => {
    setBusyPlayit(true);
    setMsg('');
    try {
      if (!tunnel?.installed) {
        await apiFetch('/api/network/playit/install', { method: 'POST' });
      }
      await apiFetch('/api/network/playit/start', { method: 'POST' });
      onRefreshTunnel();
      setTimeout(onRefreshTunnel, 1500);
      setTimeout(onRefreshTunnel, 3500);
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.playitErr);
    } finally {
      setBusyPlayit(false);
    }
  };

  const stopPlayit = async () => {
    setBusyPlayit(true);
    try {
      await apiFetch('/api/network/playit/stop', { method: 'POST' });
      onRefreshTunnel();
    } finally {
      setBusyPlayit(false);
    }
  };

  const mapPortsUpnp = async () => {
    if (!server) return;
    setBusyUpnp(true);
    setMsg('');
    try {
      if (wantJavaForServer) {
        await apiFetch('/api/network/upnp/map', {
          method: 'POST',
          body: JSON.stringify({ port: server.javaPort, protocol: 'TCP', desc: 'MC SekaiLink Java' }),
        });
      }
      if (wantBedrockForServer) {
        await apiFetch('/api/network/upnp/map', {
          method: 'POST',
          body: JSON.stringify({ port: localBedrockPort, protocol: 'UDP', desc: 'MC SekaiLink Bedrock' }),
        });
      }
      setMsg(t.upnpSuccessMsg);
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.upnpErr);
    } finally {
      setBusyUpnp(false);
    }
  };

  const pairingUrl = `http://${diag?.localIP || system?.localIP || '127.0.0.1'}:${config?.apiPort || 25580}`;
  const pairingPayload = JSON.stringify({ url: pairingUrl, token: config?.pairingToken || '' });

  const remainingPinggyMin =
    pinggy?.running && pinggy.expiresAt
      ? Math.max(0, Math.ceil((pinggy.expiresAt - Math.floor(Date.now() / 1000)) / 60))
      : null;

  const renderNatPill = (nat?: string) => {
    if (nat === 'cone') {
      return (
        <span className="rounded-full border border-emerald-500/35 bg-emerald-500/15 px-2.5 py-0.5 text-[10px] font-semibold text-emerald-200">
          {t.p2pNatCone}
        </span>
      );
    }
    if (nat === 'symmetric') {
      return (
        <span className="rounded-full border border-amber-500/35 bg-amber-500/15 px-2.5 py-0.5 text-[10px] font-semibold text-amber-200">
          {t.p2pNatSymmetric}
        </span>
      );
    }
    return (
      <span className="rounded-full border border-white/15 bg-white/5 px-2.5 py-0.5 text-[10px] font-semibold text-slate-300">
        {t.p2pNatUnknown}
      </span>
    );
  };

  const renderP2PCard = () => (
    <div className="glass-panel rounded-3xl border border-violet-500/35 p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <Radio className="h-5 w-5 text-violet-400" />
            <h3 className="text-base font-bold text-white">{t.p2pTitle}</h3>
            <span className="rounded-full bg-violet-500/20 px-2.5 py-0.5 text-[10px] font-bold uppercase text-violet-300">
              {t.p2pBadge}
            </span>
            {p2p?.running && renderNatPill(p2p.natType)}
          </div>
          <p className="mt-1 text-xs text-slate-400">{t.p2pDesc}</p>
        </div>

        {/* Mode Segmented Toggle */}
        <div className="flex rounded-2xl border border-white/15 bg-slate-950/70 p-1">
          <button
            type="button"
            disabled={p2p?.running && p2p.mode === 'join'}
            onClick={() => setP2PTab('host')}
            className={`rounded-xl px-3.5 py-1.5 text-xs font-semibold transition ${
              p2pTab === 'host'
                ? 'bg-violet-500 text-slate-950 shadow'
                : 'text-slate-300 hover:text-white disabled:opacity-40'
            }`}
          >
            {t.p2pTabHost}
          </button>
          <button
            type="button"
            disabled={p2p?.running && p2p.mode === 'host'}
            onClick={() => setP2PTab('join')}
            className={`rounded-xl px-3.5 py-1.5 text-xs font-semibold transition ${
              p2pTab === 'join'
                ? 'bg-cyan-500 text-slate-950 shadow'
                : 'text-slate-300 hover:text-white disabled:opacity-40'
            }`}
          >
            {t.p2pTabJoin}
          </button>
        </div>
      </div>

      {p2pTab === 'host' ? (
        <div className="mt-4 space-y-3">
          {!p2p?.running || p2p.mode !== 'host' ? (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-white/10 bg-slate-950/50 p-4">
              <div className="text-xs text-slate-300">
                {wantJavaForServer && <span className="mr-3 font-mono text-emerald-300">Java TCP :{localJavaPort}</span>}
                {wantBedrockForServer && <span className="font-mono text-cyan-300">Bedrock UDP :{localBedrockPort}</span>}
              </div>
              <button
                disabled={busyP2P}
                onClick={startP2PHost}
                className="flex items-center gap-2 rounded-2xl bg-gradient-to-r from-violet-500 to-cyan-500 px-5 py-2.5 text-sm font-bold text-slate-950 shadow-lg shadow-violet-500/20 hover:brightness-110 disabled:opacity-50"
              >
                {busyP2P ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4 fill-current" />}
                {t.p2pOpenRoomBtn}
              </button>
            </div>
          ) : (
            <div className="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-emerald-500/35 bg-emerald-500/10 px-4 py-3.5">
                <div>
                  <div className="text-[10px] font-semibold uppercase tracking-wider text-emerald-300">
                    {t.p2pRoomCodeLabel}
                  </div>
                  <div className="mt-0.5 font-mono text-2xl font-black tracking-[0.18em] text-white">
                    {p2p.roomCode}
                  </div>
                  <div className="mt-1 text-[11px] text-slate-300">{t.p2pShareHint}</div>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  <button
                    onClick={() => p2p.roomCode && setQrModal({ title: t.p2pRoomCodeLabel, subtitle: t.p2pShareHint, value: p2p.roomCode })}
                    className="rounded-xl bg-white/10 p-2.5 text-slate-200 hover:bg-white/20"
                    title={t.qrCodeTitle}
                  >
                    <QrCode className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => p2p.roomCode && copyText('p2p-room', p2p.roomCode)}
                    className="flex items-center gap-1.5 rounded-xl bg-emerald-500 px-4 py-2.5 text-xs font-bold text-slate-950 hover:brightness-110"
                  >
                    {copiedKey === 'p2p-room' ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                    {copiedKey === 'p2p-room' ? t.copied : t.copy}
                  </button>
                  <button
                    disabled={busyP2P}
                    onClick={stopP2P}
                    className="flex items-center gap-1.5 rounded-xl border border-rose-500/40 bg-rose-500/20 px-4 py-2.5 text-xs font-semibold text-rose-200 hover:bg-rose-500/30"
                  >
                    <Square className="h-3.5 w-3.5 fill-current" />
                    {t.p2pCloseRoomBtn}
                  </button>
                </div>
              </div>

              {/* Connected Peers List */}
              <div className="rounded-2xl border border-white/10 bg-slate-950/60 p-3.5">
                <div className="mb-2 flex items-center justify-between text-xs">
                  <span className="flex items-center gap-1.5 font-semibold text-slate-200">
                    <Users className="h-3.5 w-3.5 text-violet-400" />
                    {t.p2pPeersTitle} ({p2p.peerCount})
                  </span>
                  <span className="text-[11px] text-slate-400">
                    {p2p.phase === 'connected' ? t.p2pPhaseConnected : t.p2pPhaseWaiting}
                  </span>
                </div>
                {!p2p.peers || p2p.peers.length === 0 ? (
                  <p className="text-xs text-slate-500">{t.p2pNoPeers}</p>
                ) : (
                  <div className="flex flex-wrap gap-2">
                    {p2p.peers.map((peer) => (
                      <div key={peer.id} className="flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-3 py-1.5 text-xs">
                        <span className="font-semibold text-white">{peer.name}</span>
                        <span className="font-mono text-[11px] text-emerald-300">
                          {peer.latencyMs > 0 ? `${peer.latencyMs} ms` : '...'}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      ) : (
        <div className="mt-4 space-y-3">
          {!p2p?.running || p2p.mode !== 'join' ? (
            <div className="grid gap-2.5 sm:grid-cols-12">
              <input
                value={p2pCodeInput}
                onChange={(e) => setP2PCodeInput(e.target.value.toUpperCase())}
                placeholder={t.p2pJoinPlaceholder}
                className="rounded-2xl border border-white/15 bg-slate-950/85 px-4 py-2.5 text-center font-mono text-sm font-bold tracking-widest text-white outline-none focus:border-cyan-400 sm:col-span-5"
              />
              <input
                value={p2pNickInput}
                onChange={(e) => setP2PNickInput(e.target.value)}
                placeholder={t.p2pNickPlaceholder}
                className="rounded-2xl border border-white/10 bg-slate-950/80 px-3.5 py-2.5 text-xs text-white outline-none focus:border-cyan-400 sm:col-span-3"
              />
              <button
                type="button"
                onClick={async () => {
                  const clip = await pasteTextSafe();
                  if (clip) setP2PCodeInput(clip.trim().toUpperCase());
                }}
                className="flex items-center justify-center gap-1.5 rounded-2xl border border-white/15 bg-white/5 px-3 py-2.5 text-xs font-semibold text-slate-200 hover:bg-white/10 sm:col-span-2"
              >
                <ClipboardPaste className="h-3.5 w-3.5" />
                {t.p2pPasteBtn}
              </button>
              <button
                disabled={busyP2P || !p2pCodeInput.trim()}
                onClick={startP2PJoin}
                className="flex items-center justify-center gap-1.5 rounded-2xl bg-gradient-to-r from-cyan-500 to-emerald-500 px-4 py-2.5 text-xs font-bold text-slate-950 hover:brightness-110 disabled:opacity-50 sm:col-span-2"
              >
                {busyP2P ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-3.5 w-3.5 fill-current" />}
                {busyP2P ? t.p2pConnectingBtn : t.p2pJoinBtn}
              </button>
            </div>
          ) : (
            <div className="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2 rounded-2xl border border-emerald-500/40 bg-emerald-500/15 px-4 py-3">
                <div className="flex flex-wrap items-center gap-2 text-xs">
                  <span className="font-bold text-emerald-200">
                    ● {t.p2pConnectedBanner} ({p2p.roomCode})
                  </span>
                  <span className="rounded-full bg-emerald-500/20 px-2.5 py-0.5 font-mono font-bold text-emerald-300">
                    {p2p.latencyMs || 1} ms
                  </span>
                </div>
                <button
                  disabled={busyP2P}
                  onClick={stopP2P}
                  className="flex items-center gap-1.5 rounded-xl border border-rose-500/40 bg-rose-500/20 px-3.5 py-1.5 text-xs font-semibold text-rose-200 hover:bg-rose-500/30"
                >
                  <Square className="h-3.5 w-3.5 fill-current" />
                  {t.p2pDisconnectBtn}
                </button>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                {p2p.localJavaAddr && (
                  <CopyBox
                    label={t.p2pLocalJavaLabel}
                    value={p2p.localJavaAddr}
                    copied={copiedKey === 'p2p-join-j'}
                    copyLabel={t.copy}
                    copiedLabel={t.copied}
                    qrTitle={t.qrCodeTitle}
                    onCopy={() => copyText('p2p-join-j', p2p.localJavaAddr!)}
                    onQr={() => setQrModal({ title: t.p2pLocalJavaLabel, subtitle: t.p2pJoinHowTo, value: p2p.localJavaAddr! })}
                  />
                )}
                {p2p.localBedrockAddr && (
                  <CopyBox
                    label={t.p2pLocalBedrockLabel}
                    value={p2p.localBedrockAddr}
                    copied={copiedKey === 'p2p-join-b'}
                    copyLabel={t.copy}
                    copiedLabel={t.copied}
                    qrTitle={t.qrCodeTitle}
                    onCopy={() => copyText('p2p-join-b', p2p.localBedrockAddr!)}
                    onQr={() => setQrModal({ title: t.p2pLocalBedrockLabel, subtitle: t.p2pJoinHowTo, value: p2p.localBedrockAddr! })}
                  />
                )}
              </div>
              <p className="text-xs text-slate-300">{t.p2pJoinHowTo}</p>
            </div>
          )}
        </div>
      )}

      {p2p?.lastError && !p2p.running && (
        <div className="mt-3 rounded-xl border border-rose-500/30 bg-rose-500/10 px-3.5 py-2 text-xs text-rose-300">
          {p2p.lastError}
        </div>
      )}
    </div>
  );

  const renderE4mcCard = () => (
    <div className="glass-panel rounded-3xl border border-emerald-500/25 p-5">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <Zap className="h-5 w-5 text-emerald-400" />
            <h3 className="text-base font-bold text-white">{t.e4mcTitle}</h3>
            <span className="rounded-full bg-emerald-500/20 px-2.5 py-0.5 text-[10px] font-bold uppercase text-emerald-300">
              {t.e4mcBadge}
            </span>
            {e4mc?.running && (
              <span className="rounded-full border border-emerald-500/35 bg-emerald-500/15 px-2.5 py-0.5 font-mono text-[10px] font-bold text-emerald-200">
                {e4mc.region || 'EU'} • {e4mc.latencyMs} ms
              </span>
            )}
          </div>
          <p className="mt-1 text-xs text-slate-400">{t.e4mcDesc}</p>
        </div>

        <div className="flex items-center gap-2">
          {!e4mc?.running ? (
            <button
              disabled={busyE4mc}
              onClick={startE4mc}
              className="flex items-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-5 py-2.5 text-sm font-semibold text-slate-950 shadow-lg shadow-emerald-500/20 hover:brightness-110 disabled:opacity-50"
            >
              {busyE4mc ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4 fill-current" />}
              {t.e4mcStartBtn}
            </button>
          ) : (
            <button
              disabled={busyE4mc}
              onClick={stopE4mc}
              className="flex items-center gap-2 rounded-2xl border border-rose-500/40 bg-rose-500/20 px-5 py-2.5 text-sm font-semibold text-rose-200 hover:bg-rose-500/30"
            >
              <Square className="h-4 w-4 fill-current" />
              {t.e4mcCloseBtn}
            </button>
          )}
        </div>
      </div>

      <div className="mt-3 flex items-start gap-2 rounded-xl border border-emerald-500/20 bg-emerald-500/5 px-3.5 py-2 text-[11px] text-emerald-200">
        <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-400" />
        <span>
          {t.e4mcHostTipPrefix} <code className="font-mono text-white">localhost:{localJavaPort}</code> {t.e4mcHostTipSuffix}
        </span>
      </div>

      {e4mc?.running && e4mc.domain && (
        <div className="mt-4">
          <CopyBox
            label={`${t.e4mcCopyLabel} (${e4mc.region || 'EU'} • ${e4mc.latencyMs} ms)`}
            value={e4mc.domain}
            copied={copiedKey === 'e4mc'}
            copyLabel={t.copy}
            copiedLabel={t.copied}
            qrTitle={t.qrCodeTitle}
            onCopy={() => copyText('e4mc', e4mc.domain)}
            onQr={() => setQrModal({ title: t.e4mcQrTitle, subtitle: t.e4mcQrSubtitle, value: e4mc.domain })}
          />
        </div>
      )}

      {e4mc?.lastError && !e4mc.running && (
        <div className="mt-3 rounded-xl border border-rose-500/30 bg-rose-500/10 px-3.5 py-2 text-xs text-rose-300">
          {e4mc.lastError}
        </div>
      )}
    </div>
  );

  const renderPinggyCard = () => (
    <div className="glass-panel rounded-3xl border border-sky-500/25 p-5">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <Globe className="h-5 w-5 text-sky-400" />
            <h3 className="text-base font-bold text-white">{t.pinggyTitle}</h3>
            <span className="rounded-full bg-sky-500/20 px-2.5 py-0.5 text-[10px] font-bold uppercase text-sky-300">
              {t.pinggyBadge}
            </span>
            {pinggy?.running && pinggy.latencyMs ? (
              <span className="rounded-full border border-sky-500/35 bg-sky-500/15 px-2.5 py-0.5 font-mono text-[10px] font-bold text-sky-200">
                {pinggy.region || 'EU'} • {pinggy.latencyMs} ms
              </span>
            ) : null}
            {remainingPinggyMin !== null && (
              <span className="rounded-full border border-amber-500/35 bg-amber-500/15 px-2.5 py-0.5 font-mono text-[10px] font-semibold text-amber-200">
                {t.pinggyRemainingPrefix} {remainingPinggyMin} {t.pinggyMinSuffix}
              </span>
            )}
          </div>
          <p className="mt-1 text-xs text-slate-400">{t.pinggyDesc}</p>
        </div>

        <div className="flex items-center gap-2">
          {!pinggy?.running ? (
            <button
              disabled={busyPinggy}
              onClick={startPinggy}
              className="flex items-center gap-2 rounded-2xl bg-gradient-to-r from-sky-500 to-cyan-500 px-5 py-2.5 text-sm font-semibold text-slate-950 shadow-lg shadow-sky-500/20 hover:brightness-110 disabled:opacity-50"
            >
              {busyPinggy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4 fill-current" />}
              {t.pinggyStartBtn}
            </button>
          ) : (
            <button
              disabled={busyPinggy}
              onClick={stopPinggy}
              className="flex items-center gap-2 rounded-2xl border border-rose-500/40 bg-rose-500/20 px-5 py-2.5 text-sm font-semibold text-rose-200 hover:bg-rose-500/30"
            >
              <Square className="h-4 w-4 fill-current" />
              {t.pinggyStopBtn}
            </button>
          )}
        </div>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2 text-xs">
        <span className="text-slate-400">{t.pinggyRegionLabel}</span>
        <select
          value={pinggyRegion}
          disabled={Boolean(pinggy?.running)}
          onChange={(e) => setPinggyRegion(e.target.value)}
          className="rounded-xl border border-white/10 bg-slate-950/80 px-3 py-1.5 text-xs text-white outline-none focus:border-sky-500 disabled:opacity-60"
        >
          <option value="eu">{t.pinggyRegionAuto}</option>
          <option value="us">{t.pinggyRegionUS}</option>
          <option value="ap">{t.pinggyRegionAP}</option>
        </select>
        <input
          value={pinggyToken}
          disabled={Boolean(pinggy?.running)}
          onChange={(e) => setPinggyToken(e.target.value)}
          placeholder={t.pinggyTokenPlaceholder}
          className="min-w-[220px] flex-1 rounded-xl border border-white/10 bg-slate-950/80 px-3 py-1.5 font-mono text-xs text-white outline-none focus:border-sky-500 disabled:opacity-60"
        />
      </div>

      <p className="mt-2 text-[11px] text-slate-400">{t.pinggyFreeLimitHint}</p>

      {pinggy?.running && pinggy.address && (
        <div className="mt-4">
          <CopyBox
            label={`${t.pinggyCopyLabel} (${pinggy.region || 'EU'} • ${pinggy.latencyMs || 0} ms)`}
            value={pinggy.address}
            copied={copiedKey === 'pinggy'}
            copyLabel={t.copy}
            copiedLabel={t.copied}
            qrTitle={t.qrCodeTitle}
            onCopy={() => copyText('pinggy', pinggy.address)}
            onQr={() => setQrModal({ title: t.pinggyQrTitle, subtitle: t.pinggyQrSub, value: pinggy.address })}
          />
        </div>
      )}

      {pinggy?.lastError && !pinggy.running && (
        <div className="mt-3 rounded-xl border border-rose-500/30 bg-rose-500/10 px-3.5 py-2 text-xs text-rose-300">
          {pinggy.lastError}
        </div>
      )}
    </div>
  );

  const renderBoreCard = (compact?: boolean) => (
    <div className="glass-panel rounded-3xl p-5">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <Zap className="h-5 w-5 text-amber-400" />
            <h3 className="text-base font-bold text-white">{t.boreTitle}</h3>
            <span className="rounded-full bg-amber-500/20 px-2.5 py-0.5 text-[10px] font-bold uppercase text-amber-300">
              {t.boreBadge}
            </span>
            {bore?.running && bore.latencyMs ? (
              <span className="rounded-full border border-amber-500/30 bg-amber-500/15 px-2.5 py-0.5 font-mono text-[10px] font-bold text-amber-200">
                {bore.latencyMs} ms
              </span>
            ) : null}
          </div>
          <p className="mt-1 text-xs text-slate-400">{t.boreDesc}</p>
        </div>

        <div className="flex items-center gap-2">
          {!bore?.running ? (
            <button
              disabled={busyBore}
              onClick={startBore}
              className="flex items-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-5 py-2.5 text-sm font-semibold text-slate-950 shadow-lg shadow-emerald-500/20 hover:brightness-110 disabled:opacity-50"
            >
              {busyBore ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4 fill-current" />}
              {t.boreStartBtn}
            </button>
          ) : (
            <button
              disabled={busyBore}
              onClick={stopBore}
              className="flex items-center gap-2 rounded-2xl border border-rose-500/40 bg-rose-500/20 px-5 py-2.5 text-sm font-semibold text-rose-200 hover:bg-rose-500/30"
            >
              <Square className="h-4 w-4 fill-current" />
              {t.boreStopBtn}
            </button>
          )}
        </div>
      </div>

      {!compact && (
        <div className="mt-3 flex flex-wrap items-center gap-2 text-xs">
          <span className="text-slate-400">{t.boreServerLabel}</span>
          <input
            value={boreHost}
            onChange={(e) => setBoreHost(e.target.value)}
            disabled={Boolean(bore?.running)}
            className="w-48 rounded-xl border border-white/10 bg-slate-950/80 px-3 py-1.5 font-mono text-xs text-white outline-none focus:border-emerald-500 disabled:opacity-60"
          />
          <span className="text-slate-500">{t.boreLocalPortLabel} {localJavaPort}</span>
        </div>
      )}

      {bore?.running && bore.address && (
        <div className="mt-4">
          <CopyBox
            label={t.boreCopyLabel}
            value={bore.address}
            copied={copiedKey === 'bore'}
            copyLabel={t.copy}
            copiedLabel={t.copied}
            qrTitle={t.qrCodeTitle}
            onCopy={() => copyText('bore', bore.address)}
            onQr={() => setQrModal({ title: t.boreQrTitle, subtitle: t.boreQrSub, value: bore.address })}
          />
        </div>
      )}

      {bore?.lastError && !bore.running && (
        <div className="mt-3 rounded-xl border border-rose-500/30 bg-rose-500/10 px-3.5 py-2 text-xs text-rose-300">
          {bore.lastError}
        </div>
      )}
    </div>
  );

  const renderPlayitCard = () => (
    <div className="glass-panel rounded-3xl p-5">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <Globe className="h-5 w-5 text-cyan-400" />
            <h3 className="text-base font-bold text-white">{t.playitTitle}</h3>
          </div>
          <p className="mt-1 text-xs text-slate-400">{t.playitDesc}</p>
        </div>

        <div className="flex items-center gap-2">
          {!tunnel?.running ? (
            <button
              disabled={busyPlayit}
              onClick={installAndStartPlayit}
              className="flex items-center gap-2 rounded-2xl bg-gradient-to-r from-cyan-500 to-indigo-500 px-5 py-2.5 text-sm font-semibold text-slate-950 shadow-lg shadow-cyan-500/20 hover:brightness-110 disabled:opacity-50"
            >
              {busyPlayit ? <Loader2 className="h-4 w-4 animate-spin" /> : tunnel?.installed ? <Play className="h-4 w-4 fill-current" /> : <Download className="h-4 w-4" />}
              {tunnel?.installed ? t.playitStartBtn : t.playitInstallStartBtn}
            </button>
          ) : (
            <button
              disabled={busyPlayit}
              onClick={stopPlayit}
              className="flex items-center gap-2 rounded-2xl border border-rose-500/40 bg-rose-500/20 px-5 py-2.5 text-sm font-semibold text-rose-200 hover:bg-rose-500/30"
            >
              <Square className="h-4 w-4 fill-current" />
              {t.playitStopBtn}
            </button>
          )}
        </div>
      </div>

      <div className="mt-3 flex items-start gap-2 rounded-xl border border-cyan-500/20 bg-cyan-500/5 px-3.5 py-2 text-[11px] text-cyan-200">
        <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-cyan-400" />
        <span>
          <strong>{t.playitGuestTipPrefix}</strong> {t.playitGuestTipBody}
        </span>
      </div>

      {tunnel?.claimUrl && (
        <div className="mt-3 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-amber-500/30 bg-amber-500/10 p-3.5">
          <div className="text-xs text-amber-200">
            <strong>{t.playitClaimLabel}</strong> {t.playitClaimDesc}
          </div>
          <a
            href={tunnel.claimUrl}
            target="_blank"
            rel="noreferrer"
            className="flex items-center gap-1.5 rounded-xl bg-amber-400 px-3.5 py-2 text-xs font-bold text-slate-950 hover:brightness-110"
          >
            <span>{t.playitClaimBtn}</span>
            <ExternalLink className="h-3.5 w-3.5" />
          </a>
        </div>
      )}

      {(tunnel?.javaAddress || tunnel?.bedrockAddress) && (
        <div className="mt-4 grid gap-3 sm:grid-cols-2">
          {tunnel.javaAddress && (
            <CopyBox
              label={t.playitJavaLabel}
              value={tunnel.javaAddress}
              copied={copiedKey === 'pjava'}
              copyLabel={t.copy}
              copiedLabel={t.copied}
              qrTitle={t.qrCodeTitle}
              onCopy={() => copyText('pjava', tunnel.javaAddress)}
              onQr={() => setQrModal({ title: t.playitJavaQrTitle, subtitle: t.playitQrJavaSub, value: tunnel.javaAddress })}
            />
          )}
          {tunnel.bedrockAddress && (
            <CopyBox
              label={t.playitBedrockLabel}
              value={tunnel.bedrockAddress}
              copied={copiedKey === 'pbed'}
              copyLabel={t.copy}
              copiedLabel={t.copied}
              qrTitle={t.qrCodeTitle}
              onCopy={() => copyText('pbed', tunnel.bedrockAddress)}
              onQr={() => setQrModal({ title: t.playitBedrockQrTitle, subtitle: t.playitQrBedrockSub, value: tunnel.bedrockAddress })}
            />
          )}
        </div>
      )}

      {tunnel?.logs && tunnel.logs.length > 0 && (
        <div className="mt-3 max-h-36 overflow-y-auto rounded-2xl border border-white/10 bg-[#050811] p-3 font-mono text-[11px] text-slate-400">
          {tunnel.logs.slice(-20).map((l, idx) => (
            <div key={idx}>{l}</div>
          ))}
        </div>
      )}
    </div>
  );

  const toggleRemoteAccess = async () => {
    if (!config) return;
    await apiFetch('/api/config', {
      method: 'PUT',
      body: JSON.stringify({ remoteEnabled: !config.remoteEnabled }),
    });
    onRefreshConfig();
    setMsg(!config.remoteEnabled ? t.remoteOnMsg : t.remoteOffMsg);
  };

  return (
    <div className="space-y-4">
      {msg && (
        <div className="rounded-2xl border border-cyan-500/30 bg-cyan-500/10 px-4 py-2.5 text-sm text-cyan-200">{msg}</div>
      )}

      {/* CGNAT Diagnostics */}
      <div className="glass-panel rounded-3xl p-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <Activity className="h-5 w-5 text-cyan-400" />
            <h2 className="text-base font-bold text-white">{t.diagTitle}</h2>
            {diag?.ipv6Address && (
              <span className="rounded-full border border-emerald-500/35 bg-emerald-500/15 px-2.5 py-0.5 text-[10px] font-bold text-emerald-200">
                {t.ipv6ReadyBadge}
              </span>
            )}
          </div>
          <button
            disabled={loadingDiag}
            onClick={runDiag}
            className="flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/5 px-3 py-1.5 text-xs font-medium text-slate-300 hover:bg-white/10"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loadingDiag ? 'animate-spin' : ''}`} />
            {t.retestBtn}
          </button>
        </div>

        <div className="mt-3 grid gap-2.5 sm:grid-cols-4">
          <InfoBox label={t.localIp} value={diag?.localIP || system?.localIP || '...'} />
          <InfoBox label={t.publicIp} value={diag?.publicIP || t.unknown} />
          <InfoBox label={t.upnpLabel} value={diag?.upnpAvailable ? t.upnpSupported : t.upnpNoResponse} />
          <InfoBox
            label={t.cgnatLabel}
            value={diag?.behindCGNAT ? t.cgnatYes : diag?.status === 'direct_ok' ? t.cgnatDirect : t.unknown}
            highlight={diag?.behindCGNAT ? 'warn' : 'ok'}
          />
        </div>

        {(diag?.e4mcPingMs || diag?.pinggyPingMs || diag?.playitPingMs || diag?.borePingMs) ? (
          <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-white/10 pt-3 text-xs">
            <span className="text-slate-400">{t.livePingLabel}</span>
            {diag.e4mcPingMs ? <PingPill label={`e4mc (${diag.e4mcRegion || 'EU'})`} ms={diag.e4mcPingMs} /> : null}
            {diag.pinggyPingMs ? <PingPill label="pinggy.io (EU)" ms={diag.pinggyPingMs} /> : null}
            {diag.playitPingMs ? <PingPill label="playit.gg (Anycast)" ms={diag.playitPingMs} /> : null}
            {diag.borePingMs ? <PingPill label="bore.pub (US)" ms={diag.borePingMs} /> : null}
          </div>
        ) : null}

        {diag?.ipv6Address && (
          <div className="mt-3 space-y-1.5 border-t border-white/10 pt-3">
            <CopyBox
              label={t.ipv6CopyLabel}
              value={`[${diag.ipv6Address}]:${wantJavaForServer ? localJavaPort : localBedrockPort}`}
              copied={copiedKey === 'ipv6'}
              copyLabel={t.copy}
              copiedLabel={t.copied}
              qrTitle={t.qrCodeTitle}
              onCopy={() => copyText('ipv6', `[${diag.ipv6Address}]:${wantJavaForServer ? localJavaPort : localBedrockPort}`)}
              onQr={() =>
                setQrModal({
                  title: t.ipv6CopyLabel,
                  subtitle: t.ipv6Tip,
                  value: `[${diag.ipv6Address}]:${wantJavaForServer ? localJavaPort : localBedrockPort}`,
                })
              }
            />
            <p className="px-1 text-[11px] text-emerald-200/80">{t.ipv6Tip}</p>
          </div>
        )}
      </div>

      {/* Primary Direct P2P (Host Room / Join Friend) */}
      {renderP2PCard()}

      {/* Client-Free Tunnels (friends do NOT need MC SekaiLink installed) */}
      {isPureJava ? (
        <>
          {renderE4mcCard()}
          {renderPinggyCard()}
          <div className="glass-card rounded-2xl">
            <button
              onClick={() => setShowAdvanced(!showAdvanced)}
              className="flex w-full items-center justify-between px-5 py-3.5 text-left text-xs font-semibold text-slate-300 hover:text-white"
            >
              <span>{t.advancedToggle}</span>
              {showAdvanced ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
            </button>
            {showAdvanced && (
              <div className="space-y-4 border-t border-white/10 p-4">
                {renderPlayitCard()}
                {renderBoreCard()}
              </div>
            )}
          </div>
        </>
      ) : (
        <>
          <div className="flex items-start gap-2.5 rounded-2xl border border-emerald-500/25 bg-emerald-500/10 px-4 py-3 text-xs text-emerald-200">
            <Info className="mt-0.5 h-4 w-4 shrink-0 text-emerald-400" />
            <span>{isCrossPlay ? t.crossplayNotice : isPureBedrock ? t.bedrockOnlyNotice : t.crossplayNotice}</span>
          </div>
          {renderPlayitCard()}
          {isCrossPlay && renderE4mcCard()}
          {isCrossPlay && renderPinggyCard()}
          {isCrossPlay && (
            <div className="glass-card rounded-2xl">
              <button
                onClick={() => setShowAdvanced(!showAdvanced)}
                className="flex w-full items-center justify-between px-5 py-3.5 text-left text-xs font-semibold text-slate-300 hover:text-white"
              >
                <span>{t.boreTitle}</span>
                {showAdvanced ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
              </button>
              {showAdvanced && (
                <div className="border-t border-white/10 p-4">
                  {renderBoreCard(true)}
                </div>
              )}
            </div>
          )}
        </>
      )}

      {/* UPnP & Remote Companion */}
      <div className="grid gap-4 md:grid-cols-2">
        <div className="glass-card rounded-2xl p-5">
          <h3 className="flex items-center gap-2 font-semibold text-white">
            <Router className="h-4 w-4 text-cyan-400" />
            {t.upnpTitle}
          </h3>
          <p className="mt-1.5 text-xs text-slate-400">{t.upnpDesc}</p>
          <button
            disabled={busyUpnp || !server}
            onClick={mapPortsUpnp}
            className="mt-4 flex items-center gap-2 rounded-xl border border-white/15 bg-white/5 px-4 py-2 text-xs font-semibold text-white hover:bg-white/10 disabled:opacity-40"
          >
            {busyUpnp && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            {t.upnpMapBtn}
          </button>
        </div>

        <div className="glass-card rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <h3 className="flex items-center gap-2 font-semibold text-white">
              <Smartphone className="h-4 w-4 text-violet-400" />
              {t.companionTitle}
            </h3>
            <button
              onClick={toggleRemoteAccess}
              className={`rounded-xl px-3 py-1 text-xs font-semibold ${config?.remoteEnabled ? 'bg-emerald-500/20 text-emerald-300' : 'bg-slate-700/50 text-slate-300'}`}
            >
              {config?.remoteEnabled ? t.companionOn : t.companionOff}
            </button>
          </div>
          <p className="mt-1.5 text-xs text-slate-400">{t.companionDesc}</p>
          <div className="mt-3 space-y-2 rounded-xl border border-white/10 bg-slate-950/70 p-3 text-xs">
            <div className="flex justify-between">
              <span className="text-slate-400">{t.panelUrlLabel}</span>
              <code className="text-cyan-300">{pairingUrl}</code>
            </div>
            <div className="flex items-center justify-between gap-2">
              <span className="text-slate-400">{t.pairingTokenLabel}</span>
              <div className="flex items-center gap-1.5">
                <code className="font-mono text-emerald-300">{config?.pairingToken || '...'}</code>
                <button
                  onClick={() => config?.pairingToken && copyText('token', config.pairingToken)}
                  className="rounded p-1 hover:bg-white/10"
                >
                  {copiedKey === 'token' ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5 text-slate-400" />}
                </button>
              </div>
            </div>
          </div>
          <div className="mt-3 flex gap-2">
            <button
              onClick={() =>
                setQrModal({
                  title: t.qrPairTitle,
                  subtitle: t.qrPairSub,
                  value: pairingPayload,
                })
              }
              className="flex items-center gap-1.5 rounded-xl bg-violet-500/20 px-3 py-1.5 text-xs font-semibold text-violet-300 hover:bg-violet-500/30"
            >
              <QrCode className="h-3.5 w-3.5" />
              {t.qrPairBtn}
            </button>
            <button
              onClick={async () => {
                await apiFetch('/api/config/regenerate-token', { method: 'POST' });
                onRefreshConfig();
              }}
              className="flex items-center gap-1.5 rounded-xl border border-white/10 px-3 py-1.5 text-xs text-slate-300 hover:bg-white/5"
            >
              <ShieldCheck className="h-3.5 w-3.5" />
              {t.regenTokenBtn}
            </button>
          </div>
        </div>
      </div>

      <QrShareModal
        open={Boolean(qrModal)}
        onClose={() => setQrModal(null)}
        title={qrModal?.title || ''}
        subtitle={qrModal?.subtitle || ''}
        value={qrModal?.value || ''}
        lang={lang}
      />
    </div>
  );
};

const InfoBox: React.FC<{ label: string; value: string; highlight?: 'ok' | 'warn' }> = ({ label, value, highlight }) => (
  <div className={`rounded-2xl border p-3 ${highlight === 'ok' ? 'border-emerald-500/30 bg-emerald-500/10' : highlight === 'warn' ? 'border-amber-500/30 bg-amber-500/10' : 'border-white/10 bg-slate-950/60'}`}>
    <div className="text-[10px] uppercase tracking-wider text-slate-400">{label}</div>
    <div className="mt-1 truncate font-mono text-xs font-semibold text-white">{value}</div>
  </div>
);

const PingPill: React.FC<{ label: string; ms: number }> = ({ label, ms }) => {
  const cls =
    ms <= 110
      ? 'border-emerald-500/35 bg-emerald-500/15 text-emerald-200'
      : ms <= 150
        ? 'border-amber-500/35 bg-amber-500/15 text-amber-200'
        : 'border-rose-500/35 bg-rose-500/15 text-rose-200';
  return (
    <span className={`rounded-full border px-2.5 py-0.5 font-mono text-[11px] font-semibold ${cls}`}>
      {label}: {ms} ms
    </span>
  );
};

const CopyBox: React.FC<{
  label: string;
  value: string;
  copied: boolean;
  copyLabel: string;
  copiedLabel: string;
  qrTitle: string;
  onCopy: () => void;
  onQr: () => void;
}> = ({ label, value, copied, copyLabel, copiedLabel, qrTitle, onCopy, onQr }) => (
  <div className="flex items-center justify-between gap-2 rounded-2xl border border-emerald-500/35 bg-emerald-500/10 px-4 py-3">
    <div className="min-w-0">
      <div className="text-[10px] uppercase tracking-wider text-emerald-300">{label}</div>
      <div className="mt-0.5 truncate font-mono text-sm font-bold text-white">{value}</div>
    </div>
    <div className="flex shrink-0 items-center gap-1.5">
      <button onClick={onQr} className="rounded-xl bg-white/10 p-2 text-slate-200 hover:bg-white/20" title={qrTitle}>
        <QrCode className="h-4 w-4" />
      </button>
      <button onClick={onCopy} className="flex items-center gap-1 rounded-xl bg-emerald-500 px-3 py-2 text-xs font-bold text-slate-950 hover:brightness-110">
        {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        {copied ? copiedLabel : copyLabel}
      </button>
    </div>
  </div>
);
