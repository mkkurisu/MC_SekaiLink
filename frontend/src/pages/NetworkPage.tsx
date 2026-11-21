import React, { useEffect, useState } from 'react';
import {
  Activity,
  Check,
  Copy,
  Download,
  ExternalLink,
  Globe,
  Loader2,
  Play,
  QrCode,
  RefreshCw,
  Router,
  ShieldCheck,
  Smartphone,
  Square,
} from 'lucide-react';
import { apiFetch } from '../lib/api';
import { AppConfig, NetworkDiagnostics, ServerInstance, SystemOverview, TunnelState } from '../types';
import { QrShareModal } from '../components/QrShareModal';

interface Props {
  server: ServerInstance | null;
  system: SystemOverview | null;
  config: AppConfig | null;
  tunnel: TunnelState | null;
  onRefreshTunnel: () => void;
  onRefreshConfig: () => void;
}

export const NetworkPage: React.FC<Props> = ({
  server,
  system,
  config,
  tunnel,
  onRefreshTunnel,
  onRefreshConfig,
}) => {
  const [diag, setDiag] = useState<NetworkDiagnostics | null>(null);
  const [loadingDiag, setLoadingDiag] = useState(false);
  const [busyPlayit, setBusyPlayit] = useState(false);
  const [busyUpnp, setBusyUpnp] = useState(false);
  const [msg, setMsg] = useState('');
  const [copied, setCopied] = useState(false);
  const [showPairingQr, setShowPairingQr] = useState(false);

  useEffect(() => {
    runDiag();
  }, []);

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
      setMsg(e instanceof Error ? e.message : 'playit hatasi');
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
      if (server.engine !== 'bds' && server.engine !== 'powernukkitx') {
        await apiFetch('/api/network/upnp/map', {
          method: 'POST',
          body: JSON.stringify({ port: server.javaPort, protocol: 'TCP', desc: 'MC SekaiLink Java' }),
        });
      }
      if (server.crossPlay?.enabled || server.engine === 'bds' || server.engine === 'powernukkitx') {
        const bPort = server.crossPlay?.enabled ? server.crossPlay.bedrockPort : server.bedrockPort;
        await apiFetch('/api/network/upnp/map', {
          method: 'POST',
          body: JSON.stringify({ port: bPort, protocol: 'UDP', desc: 'MC SekaiLink Bedrock' }),
        });
      }
      setMsg('UPnP port yonlendirme modemde acildi.');
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : 'UPnP port acilamadi');
    } finally {
      setBusyUpnp(false);
    }
  };

  const toggleRemoteAccess = async () => {
    if (!config) return;
    await apiFetch('/api/config', {
      method: 'PUT',
      body: JSON.stringify({ remoteEnabled: !config.remoteEnabled }),
    });
    onRefreshConfig();
    setMsg(!config.remoteEnabled ? 'Uzak erisim aktif edildi (0.0.0.0). Telefonundan baglanabilirsin.' : 'Uzak erisim kapatildi (127.0.0.1).');
  };

  const pairingUrl = `http://${diag?.localIP || system?.localIP || '127.0.0.1'}:${config?.apiPort || 25580}`;
  const pairingPayload = JSON.stringify({ url: pairingUrl, token: config?.pairingToken || '' });

  return (
    <div className="space-y-5">
      {msg && (
        <div className="rounded-2xl border border-cyan-500/30 bg-cyan-500/10 px-4 py-2.5 text-sm text-cyan-200">{msg}</div>
      )}

      {/* CGNAT Diagnostics */}
      <div className="glass-panel rounded-3xl p-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <Activity className="h-5 w-5 text-cyan-400" />
            <h2 className="text-lg font-bold text-white">Ag &amp; CGNAT Tanilamasi</h2>
          </div>
          <button
            disabled={loadingDiag}
            onClick={runDiag}
            className="flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/5 px-3 py-1.5 text-xs font-medium text-slate-300 hover:bg-white/10"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loadingDiag ? 'animate-spin' : ''}`} />
            Yeniden Test Et
          </button>
        </div>

        <div className="mt-4 grid gap-3 sm:grid-cols-4">
          <InfoBox label="Yerel Ag IP" value={diag?.localIP || system?.localIP || '...'} />
          <InfoBox label="Dis (Public) IP" value={diag?.publicIP || 'Bilinmiyor'} />
          <InfoBox label="Modem UPnP" value={diag?.upnpAvailable ? 'Destekleniyor' : 'Yanit Yok'} />
          <InfoBox
            label="CGNAT Durumu"
            value={diag?.behindCGNAT ? 'CGNAT Var' : diag?.status === 'direct_ok' ? 'Dogrudan Acik' : 'Bilinmiyor'}
            highlight={diag?.behindCGNAT ? 'warn' : 'ok'}
          />
        </div>

        {diag?.recommendation && (
          <p className="mt-3 rounded-xl border border-white/10 bg-slate-950/60 px-4 py-2.5 text-xs text-slate-300">
            {diag.recommendation}
          </p>
        )}
      </div>

      {/* playit.gg Tunnel Controller */}
      <div className="glass-panel rounded-3xl p-6">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div>
            <h3 className="flex items-center gap-2 text-lg font-bold text-white">
              <Globe className="h-5 w-5 text-emerald-400" />
              playit.gg Dis Baglanti Tuneli (VDS&apos;siz &amp; CGNAT Asan)
            </h3>
            <p className="mt-1 text-xs text-slate-400">
              Arkadaslarinin VPN kurmadan Java (TCP) ve Bedrock (UDP) uzerinden disaridan baglanmasi icin ucretsiz genel adres olusturur.
            </p>
          </div>

          <div className="flex items-center gap-2">
            {!tunnel?.running ? (
              <button
                disabled={busyPlayit}
                onClick={installAndStartPlayit}
                className="flex items-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-5 py-2.5 text-sm font-semibold text-slate-950 shadow-lg shadow-emerald-500/20 hover:brightness-110 disabled:opacity-50"
              >
                {busyPlayit ? <Loader2 className="h-4 w-4 animate-spin" /> : tunnel?.installed ? <Play className="h-4 w-4 fill-current" /> : <Download className="h-4 w-4" />}
                {tunnel?.installed ? 'Tuneli Baslat' : 'Kur ve Baslat'}
              </button>
            ) : (
              <button
                disabled={busyPlayit}
                onClick={stopPlayit}
                className="flex items-center gap-2 rounded-2xl border border-rose-500/40 bg-rose-500/20 px-5 py-2.5 text-sm font-semibold text-rose-200 hover:bg-rose-500/30"
              >
                <Square className="h-4 w-4 fill-current" />
                Tuneli Durdur
              </button>
            )}
          </div>
        </div>

        {tunnel?.claimUrl && (
          <div className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-amber-500/30 bg-amber-500/10 p-4">
            <div className="text-xs text-amber-200">
              <strong>Ilk Kurulum:</strong> playit.gg hesabina bu cihazi baglamak ve Java (25565) / Bedrock (19132) tuneli eklemek icin linke tikla:
            </div>
            <a
              href={tunnel.claimUrl}
              target="_blank"
              rel="noreferrer"
              className="flex items-center gap-1.5 rounded-xl bg-amber-400 px-3.5 py-2 text-xs font-bold text-slate-950 hover:brightness-110"
            >
              <span>Claim Linkini Ac</span>
              <ExternalLink className="h-3.5 w-3.5" />
            </a>
          </div>
        )}

        {(tunnel?.javaAddress || tunnel?.bedrockAddress || (tunnel?.addresses && tunnel.addresses.length > 0)) && (
          <div className="mt-4 grid gap-3 sm:grid-cols-2">
            {tunnel.javaAddress && <InfoBox label="Java Genel Adresi" value={tunnel.javaAddress} highlight="ok" />}
            {tunnel.bedrockAddress && <InfoBox label="Bedrock Genel Adresi" value={tunnel.bedrockAddress} highlight="ok" />}
          </div>
        )}

        {tunnel?.logs && tunnel.logs.length > 0 && (
          <div className="mt-4 max-h-40 overflow-y-auto rounded-2xl border border-white/10 bg-[#050811] p-3 font-mono text-[11px] text-slate-400">
            {tunnel.logs.slice(-25).map((l, idx) => (
              <div key={idx}>{l}</div>
            ))}
          </div>
        )}
      </div>

      {/* UPnP & Remote Companion */}
      <div className="grid gap-4 md:grid-cols-2">
        <div className="glass-card rounded-2xl p-5">
          <h3 className="flex items-center gap-2 font-semibold text-white">
            <Router className="h-4 w-4 text-cyan-400" />
            Modem Otomatik Port Acma (UPnP)
          </h3>
          <p className="mt-1.5 text-xs text-slate-400">
            CGNAT arkasinda degilsen ve modeminde UPnP aciksa, aktif sunucunun portlarini tek tikla disa acar.
          </p>
          <button
            disabled={busyUpnp || !server}
            onClick={mapPortsUpnp}
            className="mt-4 flex items-center gap-2 rounded-xl border border-white/15 bg-white/5 px-4 py-2 text-xs font-semibold text-white hover:bg-white/10 disabled:opacity-40"
          >
            {busyUpnp && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            Portlari UPnP ile Ac
          </button>
        </div>

        <div className="glass-card rounded-2xl p-5">
          <div className="flex items-center justify-between">
            <h3 className="flex items-center gap-2 font-semibold text-white">
              <Smartphone className="h-4 w-4 text-violet-400" />
              Android / Uzaktan Kontrol (Companion)
            </h3>
            <button
              onClick={toggleRemoteAccess}
              className={`rounded-xl px-3 py-1 text-xs font-semibold ${config?.remoteEnabled ? 'bg-emerald-500/20 text-emerald-300' : 'bg-slate-700/50 text-slate-300'}`}
            >
              {config?.remoteEnabled ? 'Acik' : 'Kapali'}
            </button>
          </div>
          <p className="mt-1.5 text-xs text-slate-400">
            Telefonundaki MC SekaiLink uygulamasindan bu bilgisayardaki sunucuyu guvenli sekilde yonet.
          </p>
          <div className="mt-3 space-y-2 rounded-xl border border-white/10 bg-slate-950/70 p-3 text-xs">
            <div className="flex justify-between">
              <span className="text-slate-400">Panel URL:</span>
              <code className="text-cyan-300">{pairingUrl}</code>
            </div>
            <div className="flex items-center justify-between gap-2">
              <span className="text-slate-400">Eslestirme Token:</span>
              <div className="flex items-center gap-1.5">
                <code className="font-mono text-emerald-300">{config?.pairingToken || '...'}</code>
                <button
                  onClick={async () => {
                    if (!config?.pairingToken) return;
                    await navigator.clipboard.writeText(config.pairingToken);
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1500);
                  }}
                  className="rounded p-1 hover:bg-white/10"
                >
                  {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5 text-slate-400" />}
                </button>
              </div>
            </div>
          </div>
          <div className="mt-3 flex gap-2">
            <button
              onClick={() => setShowPairingQr(true)}
              className="flex items-center gap-1.5 rounded-xl bg-violet-500/20 px-3 py-1.5 text-xs font-semibold text-violet-300 hover:bg-violet-500/30"
            >
              <QrCode className="h-3.5 w-3.5" />
              QR ile Eslestir
            </button>
            <button
              onClick={async () => {
                await apiFetch('/api/config/regenerate-token', { method: 'POST' });
                onRefreshConfig();
              }}
              className="flex items-center gap-1.5 rounded-xl border border-white/10 px-3 py-1.5 text-xs text-slate-300 hover:bg-white/5"
            >
              <ShieldCheck className="h-3.5 w-3.5" />
              Token Yenile
            </button>
          </div>
        </div>
      </div>

      <QrShareModal
        open={showPairingQr}
        onClose={() => setShowPairingQr(false)}
        title="Mobil Uzak Baglanti QR"
        subtitle="Android MC SekaiLink Uzak Baglanti sekmesinde bu bilgileri kullan"
        value={pairingPayload}
      />
    </div>
  );
};

const InfoBox: React.FC<{ label: string; value: string; highlight?: 'ok' | 'warn' }> = ({ label, value, highlight }) => (
  <div className={`rounded-2xl border p-3.5 ${highlight === 'ok' ? 'border-emerald-500/30 bg-emerald-500/10' : highlight === 'warn' ? 'border-amber-500/30 bg-amber-500/10' : 'border-white/10 bg-slate-950/60'}`}>
    <div className="text-[10px] uppercase tracking-wider text-slate-400">{label}</div>
    <div className="mt-1 truncate font-mono text-xs font-semibold text-white">{value}</div>
  </div>
);
