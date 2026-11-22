import React, { useEffect, useState } from 'react';
import {
  Activity,
  Check,
  ChevronDown,
  ChevronUp,
  Copy,
  Download,
  ExternalLink,
  Globe,
  Info,
  Loader2,
  Lock,
  Play,
  Plus,
  QrCode,
  Radio,
  RefreshCw,
  Router,
  ShieldCheck,
  Smartphone,
  Square,
  Trash2,
  Zap,
} from 'lucide-react';
import { apiFetch, copyTextSafe } from '../lib/api';
import { Lang, translations } from '../lib/i18n';
import {
  AppConfig,
  AutoTunnelResult,
  BoreState,
  E4mcState,
  NetworkDiagnostics,
  RelayNode,
  RelayNodeProbe,
  RelayState,
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
  relay: RelayState | null;
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
  relay,
  lang,
  onRefreshTunnel,
  onRefreshConfig,
}) => {
  const t = translations[lang].network;
  const [diag, setDiag] = useState<NetworkDiagnostics | null>(null);
  const [loadingDiag, setLoadingDiag] = useState(false);
  const [busyPlayit, setBusyPlayit] = useState(false);
  const [busyE4mc, setBusyE4mc] = useState(false);
  const [busyBore, setBusyBore] = useState(false);
  const [busyRelay, setBusyRelay] = useState(false);
  const [probingRelay, setProbingRelay] = useState(false);
  const [busyUpnp, setBusyUpnp] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);

  const [boreHost, setBoreHost] = useState('bore.pub');
  const [relayNodes, setRelayNodes] = useState<RelayNode[]>(() => config?.relayNodes || []);
  const [relayProbes, setRelayProbes] = useState<Record<string, RelayNodeProbe>>({});

  const [newNodeId, setNewNodeId] = useState('SV-Oracle');
  const [newNodeAddr, setNewNodeAddr] = useState(() => localStorage.getItem('sekailink_relay_addr') || '');
  const [newNodeSecret, setNewNodeSecret] = useState('');
  const [newNodePin, setNewNodePin] = useState('');
  const [newNodeTLS, setNewNodeTLS] = useState(true);

  const [msg, setMsg] = useState('');
  const [copiedKey, setCopiedKey] = useState('');
  const [qrModal, setQrModal] = useState<{ title: string; subtitle: string; value: string } | null>(null);

  useEffect(() => {
    runDiag();
  }, []);

  useEffect(() => {
    if (config?.relayNodes) {
      setRelayNodes(config.relayNodes);
    }
  }, [config?.relayNodes]);

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

  const probeRelayNodes = async () => {
    setProbingRelay(true);
    try {
      const list = await apiFetch<RelayNodeProbe[]>('/api/network/relay/probe');
      const map: Record<string, RelayNodeProbe> = {};
      for (const p of list || []) {
        map[p.address] = p;
      }
      setRelayProbes(map);
    } catch {
      // ignore
    } finally {
      setProbingRelay(false);
    }
  };

  const saveRelayNodes = async (next: RelayNode[]) => {
    const saved = await apiFetch<RelayNode[]>('/api/network/relay/nodes', {
      method: 'PUT',
      body: JSON.stringify({ nodes: next }),
    });
    setRelayNodes(saved || []);
    onRefreshConfig();
    return saved || [];
  };

  const addRelayNode = async () => {
    const addr = newNodeAddr.trim();
    if (!addr) return;
    setBusyRelay(true);
    setMsg('');
    try {
      localStorage.setItem('sekailink_relay_addr', addr);
      const next: RelayNode[] = [
        ...relayNodes,
        {
          id: newNodeId.trim() || addr,
          address: addr,
          secret: newNodeSecret.trim(),
          certPin: newNodePin.trim(),
          tls: newNodeTLS,
          enabled: true,
        },
      ];
      await saveRelayNodes(next);
      setNewNodeAddr('');
      setNewNodeSecret('');
      setNewNodePin('');
      await probeRelayNodes();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.relayErr);
    } finally {
      setBusyRelay(false);
    }
  };

  const toggleNodeEnabled = async (idx: number) => {
    const next = relayNodes.map((n, i) => (i === idx ? { ...n, enabled: !n.enabled } : n));
    await saveRelayNodes(next);
  };

  const deleteRelayNode = async (idx: number) => {
    const next = relayNodes.filter((_, i) => i !== idx);
    await saveRelayNodes(next);
  };

  const autoConnectFastestBridge = async () => {
    setBusyRelay(true);
    setMsg('');
    try {
      const res = await apiFetch<AutoTunnelResult>('/api/network/tunnel/auto-start', {
        method: 'POST',
        body: JSON.stringify({
          localJavaPort,
          localBedrockPort,
          wantJava: wantJavaForServer,
          wantBedrock: wantBedrockForServer,
        }),
      });
      if (res.provider === 'relay') {
        setMsg(`${t.relayConnectedBridge} ${res.nodeId || 'SV-Relay'} (${res.latencyMs || 0} ms${res.encrypted ? ' • TLS 1.3' : ''})`);
      } else {
        const fallbackNote = res.fallbackReason ? `${t.relayFallbackNoticePrefix} ${res.fallbackReason} → ` : '';
        setMsg(`${fallbackNote}${res.nodeId || res.provider}: ${res.javaAddress || ''}`);
      }
      onRefreshTunnel();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.relayErr);
    } finally {
      setBusyRelay(false);
    }
  };

  const connectSingleNode = async (node: RelayNode) => {
    setBusyRelay(true);
    setMsg('');
    try {
      const st = await apiFetch<RelayState>('/api/network/relay/start', {
        method: 'POST',
        body: JSON.stringify({
          relayAddr: node.address,
          secret: node.secret || '',
          tls: node.tls,
          certPin: node.certPin || '',
          localJavaPort,
          localBedrockPort,
          wantJava: wantJavaForServer,
          wantBedrock: wantBedrockForServer,
        }),
      });
      setMsg(`${t.relayConnectedBridge} ${st.nodeId || node.id} (${st.latencyMs || 0} ms)`);
      onRefreshTunnel();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.relayErr);
    } finally {
      setBusyRelay(false);
    }
  };

  const stopRelay = async () => {
    setBusyRelay(true);
    try {
      await apiFetch('/api/network/relay/stop', { method: 'POST' });
      onRefreshTunnel();
    } finally {
      setBusyRelay(false);
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

  const renderRelayCard = () => (
    <div className="glass-panel rounded-3xl border border-violet-500/30 p-5">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <Radio className="h-5 w-5 text-violet-400" />
            <h3 className="text-base font-bold text-white">{t.relayTitle}</h3>
            <span className="rounded-full bg-violet-500/20 px-2.5 py-0.5 text-[10px] font-bold uppercase text-violet-300">
              {t.relayBadge}
            </span>
            {relay?.running && (
              <span className="inline-flex items-center gap-1 rounded-full border border-emerald-500/40 bg-emerald-500/15 px-2.5 py-0.5 font-mono text-[11px] font-bold text-emerald-200">
                {t.relayConnectedBridge} {relay.nodeId || 'SV-Relay'} • {relay.latencyMs || 0} ms
                {relay.encrypted ? ' • TLS' : ''}
              </span>
            )}
          </div>
          <p className="mt-1 text-xs text-slate-400">{t.relayDesc}</p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <button
            disabled={probingRelay || relayNodes.length === 0}
            onClick={probeRelayNodes}
            className="flex items-center gap-1.5 rounded-2xl border border-white/15 bg-white/5 px-3.5 py-2.5 text-xs font-semibold text-slate-200 hover:bg-white/10 disabled:opacity-40"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${probingRelay ? 'animate-spin' : ''}`} />
            {t.relayProbeBtn}
          </button>

          {!relay?.running ? (
            <button
              disabled={busyRelay}
              onClick={autoConnectFastestBridge}
              className="flex items-center gap-2 rounded-2xl bg-gradient-to-r from-violet-500 to-cyan-500 px-5 py-2.5 text-sm font-semibold text-slate-950 hover:brightness-110 disabled:opacity-50"
            >
              {busyRelay ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4 fill-current" />}
              {t.relayAutoBtn}
            </button>
          ) : (
            <button
              disabled={busyRelay}
              onClick={stopRelay}
              className="flex items-center gap-2 rounded-2xl border border-rose-500/40 bg-rose-500/20 px-5 py-2.5 text-sm font-semibold text-rose-200 hover:bg-rose-500/30"
            >
              <Square className="h-4 w-4 fill-current" />
              {t.relayDisconnectBtn}
            </button>
          )}
        </div>
      </div>

      {/* Add VDS node form */}
      <div className="mt-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-12">
        <input
          value={newNodeId}
          onChange={(e) => setNewNodeId(e.target.value)}
          placeholder={t.relayNodeIdPlaceholder}
          className="rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 font-mono text-xs text-white outline-none focus:border-violet-500 lg:col-span-2"
        />
        <input
          value={newNodeAddr}
          onChange={(e) => setNewNodeAddr(e.target.value)}
          placeholder={t.relayNodeAddrPlaceholder}
          className="rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 font-mono text-xs text-white outline-none focus:border-violet-500 lg:col-span-3"
        />
        <input
          value={newNodeSecret}
          onChange={(e) => setNewNodeSecret(e.target.value)}
          type="password"
          placeholder={t.relayNodeSecretPlaceholder}
          className="rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 font-mono text-xs text-white outline-none focus:border-violet-500 lg:col-span-2"
        />
        <input
          value={newNodePin}
          onChange={(e) => setNewNodePin(e.target.value)}
          placeholder={t.relayNodePinPlaceholder}
          className="rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 font-mono text-xs text-white outline-none focus:border-violet-500 lg:col-span-2"
        />
        <label className="flex items-center justify-center gap-1.5 rounded-xl border border-white/10 bg-slate-950/70 px-2.5 py-2 text-xs text-slate-300 lg:col-span-1">
          <input
            type="checkbox"
            checked={newNodeTLS}
            onChange={(e) => setNewNodeTLS(e.target.checked)}
            className="h-3.5 w-3.5 accent-violet-500"
          />
          <span>TLS</span>
        </label>
        <button
          disabled={busyRelay || !newNodeAddr.trim()}
          onClick={addRelayNode}
          className="flex items-center justify-center gap-1.5 rounded-xl bg-violet-500/25 px-3 py-2 text-xs font-semibold text-violet-200 hover:bg-violet-500/35 disabled:opacity-40 lg:col-span-2"
        >
          <Plus className="h-3.5 w-3.5" />
          {t.relayAddNodeBtn}
        </button>
      </div>

      {/* Saved VDS Node List */}
      {relayNodes.length === 0 ? (
        <div className="mt-3 rounded-xl border border-dashed border-white/15 bg-slate-950/40 px-3.5 py-2.5 text-xs text-slate-400">
          {t.relayNoNodesHint}
        </div>
      ) : (
        <div className="mt-3 space-y-2">
          {relayNodes.map((node, idx) => {
            const addrKey = node.address.includes(':') ? node.address : `${node.address}:25590`;
            const probe = relayProbes[addrKey] || relayProbes[node.address];
            const isConnectedThis = Boolean(relay?.running && relay.relayAddr === addrKey);
            return (
              <div
                key={`${node.id}-${node.address}-${idx}`}
                className={`flex flex-wrap items-center justify-between gap-2 rounded-2xl border px-3.5 py-2.5 text-xs ${
                  isConnectedThis
                    ? 'border-emerald-500/40 bg-emerald-500/10'
                    : 'border-white/10 bg-slate-950/60'
                }`}
              >
                <div className="flex flex-wrap items-center gap-2">
                  <input
                    type="checkbox"
                    checked={node.enabled}
                    onChange={() => toggleNodeEnabled(idx)}
                    className="h-3.5 w-3.5 accent-emerald-500"
                  />
                  <span className={`font-mono font-bold ${node.enabled ? 'text-white' : 'text-slate-500 line-through'}`}>
                    {probe?.nodeId || node.id}
                  </span>
                  <span className="font-mono text-slate-400">{node.address}</span>
                  <span className="inline-flex items-center gap-1 rounded-md bg-white/5 px-2 py-0.5 text-[10px] text-slate-300">
                    <Lock className="h-2.5 w-2.5 text-violet-300" />
                    {node.tls ? t.relayEncryptedBadge : t.relayPlainBadge}
                  </span>
                  {probe && (
                    probe.online ? (
                      <PingPill label={probe.nodeId || node.id} ms={probe.latencyMs || 1} />
                    ) : (
                      <span className="rounded-full border border-rose-500/35 bg-rose-500/15 px-2 py-0.5 text-[10px] text-rose-300">
                        {t.relayOfflineBadge}
                      </span>
                    )
                  )}
                </div>

                <div className="flex items-center gap-1.5">
                  <button
                    disabled={busyRelay || isConnectedThis}
                    onClick={() => connectSingleNode(node)}
                    className="rounded-xl bg-violet-500/20 px-3 py-1 text-xs font-semibold text-violet-200 hover:bg-violet-500/30 disabled:opacity-40"
                  >
                    {t.relayConnectBtn}
                  </button>
                  <button
                    onClick={() => deleteRelayNode(idx)}
                    title={t.relayDeleteNodeTitle}
                    className="rounded-xl bg-rose-500/15 p-1.5 text-rose-300 hover:bg-rose-500/25"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      <div className="mt-2.5 rounded-xl border border-white/10 bg-slate-950/60 px-3 py-2 font-mono text-[11px] text-slate-400">
        {t.relayCmdHint} <span className="text-violet-300">sekailink-relay --node-id SV-Oracle --public-host VDS_IP --secret GIZLI_ANAHTAR</span>
      </div>

      {relay?.running && (relay.javaAddress || relay.bedrockAddress) && (
        <div className="mt-4 grid gap-3 sm:grid-cols-2">
          {relay.javaAddress && (
            <CopyBox
              label={`${t.relayJavaCopy} (${relay.nodeId || 'SV-Relay'} • ${relay.latencyMs || 0} ms)`}
              value={relay.javaAddress}
              copied={copiedKey === 'rjava'}
              copyLabel={t.copy}
              copiedLabel={t.copied}
              qrTitle={t.qrCodeTitle}
              onCopy={() => copyText('rjava', relay.javaAddress)}
              onQr={() => setQrModal({ title: t.relayJavaQrTitle, subtitle: t.playitQrJavaSub, value: relay.javaAddress })}
            />
          )}
          {relay.bedrockAddress && (
            <CopyBox
              label={`${t.relayBedrockCopy} (${relay.nodeId || 'SV-Relay'} • ${relay.latencyMs || 0} ms)`}
              value={relay.bedrockAddress}
              copied={copiedKey === 'rbed'}
              copyLabel={t.copy}
              copiedLabel={t.copied}
              qrTitle={t.qrCodeTitle}
              onCopy={() => copyText('rbed', relay.bedrockAddress)}
              onQr={() => setQrModal({ title: t.relayBedrockQrTitle, subtitle: t.playitQrBedrockSub, value: relay.bedrockAddress })}
            />
          )}
        </div>
      )}

      {relay?.lastError && !relay.running && (
        <div className="mt-3 rounded-xl border border-rose-500/30 bg-rose-500/10 px-3.5 py-2 text-xs text-rose-300">
          {relay.lastError}
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

        {(diag?.e4mcPingMs || diag?.playitPingMs || diag?.borePingMs) ? (
          <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-white/10 pt-3 text-xs">
            <span className="text-slate-400">{t.livePingLabel}</span>
            {diag.e4mcPingMs ? <PingPill label={`e4mc (${diag.e4mcRegion || 'EU'})`} ms={diag.e4mcPingMs} /> : null}
            {diag.playitPingMs ? <PingPill label="playit.gg (Anycast)" ms={diag.playitPingMs} /> : null}
            {diag.borePingMs ? <PingPill label="bore.pub (US)" ms={diag.borePingMs} /> : null}
          </div>
        ) : null}
      </div>

      {/* Primary VDS Bridge Pool & Server-Aware Tunnels */}
      {renderRelayCard()}

      {isPureJava ? (
        <>
          {renderE4mcCard()}
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
