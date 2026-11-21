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
  Play,
  QrCode,
  Radio,
  RefreshCw,
  Router,
  ShieldCheck,
  Smartphone,
  Square,
  Zap,
} from 'lucide-react';
import { apiFetch } from '../lib/api';
import { AppConfig, BoreState, NetworkDiagnostics, RelayState, ServerInstance, SystemOverview, TunnelState } from '../types';
import { QrShareModal } from '../components/QrShareModal';

interface Props {
  server: ServerInstance | null;
  system: SystemOverview | null;
  config: AppConfig | null;
  tunnel: TunnelState | null;
  bore: BoreState | null;
  relay: RelayState | null;
  onRefreshTunnel: () => void;
  onRefreshConfig: () => void;
}

export const NetworkPage: React.FC<Props> = ({
  server,
  system,
  config,
  tunnel,
  bore,
  relay,
  onRefreshTunnel,
  onRefreshConfig,
}) => {
  const [diag, setDiag] = useState<NetworkDiagnostics | null>(null);
  const [loadingDiag, setLoadingDiag] = useState(false);
  const [busyPlayit, setBusyPlayit] = useState(false);
  const [busyBore, setBusyBore] = useState(false);
  const [busyRelay, setBusyRelay] = useState(false);
  const [busyUpnp, setBusyUpnp] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);

  const [boreHost, setBoreHost] = useState('bore.pub');
  const [relayAddr, setRelayAddr] = useState(() => localStorage.getItem('sekailink_relay_addr') || '');
  const [relaySecret, setRelaySecret] = useState('');

  const [msg, setMsg] = useState('');
  const [copiedKey, setCopiedKey] = useState('');
  const [qrModal, setQrModal] = useState<{ title: string; subtitle: string; value: string } | null>(null);

  useEffect(() => {
    runDiag();
  }, []);

  const isPureJava = Boolean(
    server && server.engine !== 'bds' && server.engine !== 'powernukkitx' && !server.crossPlay?.enabled,
  );
  const isCrossPlay = Boolean(server && server.crossPlay?.enabled);
  const needsBedrock = Boolean(
    !server || server.crossPlay?.enabled || server.engine === 'bds' || server.engine === 'powernukkitx',
  );

  const copyText = async (key: string, val: string) => {
    await navigator.clipboard.writeText(val);
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

  const startBore = async () => {
    setBusyBore(true);
    setMsg('');
    try {
      const localPort = server?.javaPort || 25565;
      const st = await apiFetch<BoreState>('/api/network/bore/start', {
        method: 'POST',
        body: JSON.stringify({ serverHost: boreHost.trim() || 'bore.pub', localPort }),
      });
      setMsg(`Hizli tunel acildi! Java adresin: ${st.address}`);
      onRefreshTunnel();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : 'bore.pub tuneli baslatilamadi');
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

  const startRelay = async () => {
    setBusyRelay(true);
    setMsg('');
    try {
      localStorage.setItem('sekailink_relay_addr', relayAddr.trim());
      const wantJava = !server || (server.engine !== 'bds' && server.engine !== 'powernukkitx');
      const wantBedrock = Boolean(!server || server.crossPlay?.enabled || server.engine === 'bds' || server.engine === 'powernukkitx');
      const localBedrockPort = server?.crossPlay?.enabled ? server.crossPlay.bedrockPort : server?.bedrockPort || 19132;
      await apiFetch<RelayState>('/api/network/relay/start', {
        method: 'POST',
        body: JSON.stringify({
          relayAddr: relayAddr.trim(),
          secret: relaySecret,
          localJavaPort: server?.javaPort || 25565,
          localBedrockPort,
          wantJava,
          wantBedrock,
        }),
      });
      setMsg('SekaiLink Relay baglantisi kuruldu!');
      onRefreshTunnel();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : 'Relay baglantisi basarisiz');
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
    setMsg(
      !config.remoteEnabled
        ? 'Uzak erisim aktif edildi (0.0.0.0). Telefonundan baglanabilirsin.'
        : 'Uzak erisim kapatildi (127.0.0.1).',
    );
  };

  const pairingUrl = `http://${diag?.localIP || system?.localIP || '127.0.0.1'}:${config?.apiPort || 25580}`;
  const pairingPayload = JSON.stringify({ url: pairingUrl, token: config?.pairingToken || '' });

  const renderBoreCard = (compact?: boolean) => (
    <div className="glass-panel rounded-3xl p-5">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <Zap className="h-5 w-5 text-emerald-400" />
            <h3 className="text-base font-bold text-white">
              Hizli Java Tuneli (bore.pub)
            </h3>
            <span className="rounded-full bg-emerald-500/20 px-2.5 py-0.5 text-[10px] font-bold uppercase text-emerald-300">
              Hesapsiz • Uygulama Ici
            </span>
          </div>
          <p className="mt-1 text-xs text-slate-400">
            Tarayici veya hesap acmadan tek tikla dogrudan Java (TCP) adresi olusturur. Windows, Linux ve Android&apos;de harici dosya olmadan calisir.
          </p>
        </div>

        <div className="flex items-center gap-2">
          {!bore?.running ? (
            <button
              disabled={busyBore}
              onClick={startBore}
              className="flex items-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-5 py-2.5 text-sm font-semibold text-slate-950 shadow-lg shadow-emerald-500/20 hover:brightness-110 disabled:opacity-50"
            >
              {busyBore ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4 fill-current" />}
              Tek Tikla Baslat
            </button>
          ) : (
            <button
              disabled={busyBore}
              onClick={stopBore}
              className="flex items-center gap-2 rounded-2xl border border-rose-500/40 bg-rose-500/20 px-5 py-2.5 text-sm font-semibold text-rose-200 hover:bg-rose-500/30"
            >
              <Square className="h-4 w-4 fill-current" />
              Tuneli Kapat
            </button>
          )}
        </div>
      </div>

      {!compact && (
        <div className="mt-3 flex flex-wrap items-center gap-2 text-xs">
          <span className="text-slate-400">Tunel Sunucusu:</span>
          <input
            value={boreHost}
            onChange={(e) => setBoreHost(e.target.value)}
            disabled={Boolean(bore?.running)}
            className="w-48 rounded-xl border border-white/10 bg-slate-950/80 px-3 py-1.5 font-mono text-xs text-white outline-none focus:border-emerald-500 disabled:opacity-60"
          />
          <span className="text-slate-500">Yerel Port: {server?.javaPort || 25565}</span>
        </div>
      )}

      {bore?.running && bore.address && (
        <div className="mt-4">
          <CopyBox
            label="Arkadaslarin Icin Java Baglanti Adresi"
            value={bore.address}
            copied={copiedKey === 'bore'}
            onCopy={() => copyText('bore', bore.address)}
            onQr={() => setQrModal({ title: 'bore.pub Java Adresi', subtitle: 'Minecraft Java > Dogrudan Baglanti', value: bore.address })}
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
            <h3 className="text-base font-bold text-white">playit.gg (Bedrock UDP + Sabit Alan Adi)</h3>
          </div>
          <p className="mt-1 text-xs text-slate-400">
            Bedrock (UDP) ve Cross-Play sunuculari icin ucretsiz genel adres ve sabit alan adi saglar.
          </p>
        </div>

        <div className="flex items-center gap-2">
          {!tunnel?.running ? (
            <button
              disabled={busyPlayit}
              onClick={installAndStartPlayit}
              className="flex items-center gap-2 rounded-2xl bg-gradient-to-r from-cyan-500 to-indigo-500 px-5 py-2.5 text-sm font-semibold text-slate-950 shadow-lg shadow-cyan-500/20 hover:brightness-110 disabled:opacity-50"
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

      <div className="mt-3 flex items-start gap-2 rounded-xl border border-cyan-500/20 bg-cyan-500/5 px-3.5 py-2 text-[11px] text-cyan-200">
        <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-cyan-400" />
        <span>
          <strong>Ipucu:</strong> Hesap acmak istemiyorsan cikan Claim baglantisinda <strong>&quot;Continue as Guest&quot; (Misafir Olarak Devam Et)</strong> secenegini kullanabilirsin.
        </span>
      </div>

      {tunnel?.claimUrl && (
        <div className="mt-3 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-amber-500/30 bg-amber-500/10 p-3.5">
          <div className="text-xs text-amber-200">
            <strong>Ilk Kurulum:</strong> Tunelleri aktif etmek icin Claim linkini ac:
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

      {(tunnel?.javaAddress || tunnel?.bedrockAddress) && (
        <div className="mt-4 grid gap-3 sm:grid-cols-2">
          {tunnel.javaAddress && (
            <CopyBox
              label="Java Genel Adresi"
              value={tunnel.javaAddress}
              copied={copiedKey === 'pjava'}
              onCopy={() => copyText('pjava', tunnel.javaAddress)}
              onQr={() => setQrModal({ title: 'playit.gg Java Adresi', subtitle: 'Dogrudan Baglanti', value: tunnel.javaAddress })}
            />
          )}
          {tunnel.bedrockAddress && (
            <CopyBox
              label="Bedrock Genel Adresi"
              value={tunnel.bedrockAddress}
              copied={copiedKey === 'pbed'}
              onCopy={() => copyText('pbed', tunnel.bedrockAddress)}
              onQr={() => setQrModal({ title: 'playit.gg Bedrock Adresi', subtitle: 'Bedrock Sunucu Ekle', value: tunnel.bedrockAddress })}
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

  const renderRelayCard = () => (
    <div className="glass-panel rounded-3xl p-5">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <Radio className="h-5 w-5 text-violet-400" />
            <h3 className="text-base font-bold text-white">SekaiLink Relay (Kendi Acik Kaynak TCP + UDP Aktaricimiz)</h3>
            <span className="rounded-full bg-violet-500/20 px-2.5 py-0.5 text-[10px] font-bold uppercase text-violet-300">
              Hesapsiz • TCP + UDP
            </span>
          </div>
          <p className="mt-1 text-xs text-slate-400">
            Ucretsiz bir sunucuda (orn. Oracle Cloud Free Tier) <code>sekailink-relay</code> calistirarak tamamen kendi altyapinla hem Java hem Bedrock ac.
          </p>
        </div>

        <div className="flex items-center gap-2">
          {!relay?.running ? (
            <button
              disabled={busyRelay || !relayAddr.trim()}
              onClick={startRelay}
              className="flex items-center gap-2 rounded-2xl bg-gradient-to-r from-violet-500 to-cyan-500 px-5 py-2.5 text-sm font-semibold text-slate-950 hover:brightness-110 disabled:opacity-50"
            >
              {busyRelay ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4 fill-current" />}
              Relay&apos;e Baglan
            </button>
          ) : (
            <button
              disabled={busyRelay}
              onClick={stopRelay}
              className="flex items-center gap-2 rounded-2xl border border-rose-500/40 bg-rose-500/20 px-5 py-2.5 text-sm font-semibold text-rose-200 hover:bg-rose-500/30"
            >
              <Square className="h-4 w-4 fill-current" />
              Baglantiyi Kes
            </button>
          )}
        </div>
      </div>

      <div className="mt-3 grid gap-2.5 sm:grid-cols-2">
        <input
          value={relayAddr}
          onChange={(e) => setRelayAddr(e.target.value)}
          disabled={Boolean(relay?.running)}
          placeholder="Relay Adresi (orn: relay.alanadin.com:25590)"
          className="rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 font-mono text-xs text-white outline-none focus:border-violet-500 disabled:opacity-60"
        />
        <input
          value={relaySecret}
          onChange={(e) => setRelaySecret(e.target.value)}
          disabled={Boolean(relay?.running)}
          type="password"
          placeholder="Gizli Anahtar (istege bagli)"
          className="rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 font-mono text-xs text-white outline-none focus:border-violet-500 disabled:opacity-60"
        />
      </div>

      <div className="mt-2.5 rounded-xl border border-white/10 bg-slate-950/60 px-3 py-2 font-mono text-[11px] text-slate-400">
        Sunucuda baslatma: <span className="text-violet-300">sekailink-relay --listen :25590 --public-host SUNUCU_IP_VEYA_DOMAIN</span>
      </div>

      {relay?.running && (relay.javaAddress || relay.bedrockAddress) && (
        <div className="mt-4 grid gap-3 sm:grid-cols-2">
          {relay.javaAddress && (
            <CopyBox
              label="Relay Java Adresi (TCP)"
              value={relay.javaAddress}
              copied={copiedKey === 'rjava'}
              onCopy={() => copyText('rjava', relay.javaAddress)}
              onQr={() => setQrModal({ title: 'Relay Java Adresi', subtitle: 'Dogrudan Baglanti', value: relay.javaAddress })}
            />
          )}
          {relay.bedrockAddress && (
            <CopyBox
              label="Relay Bedrock Adresi (UDP)"
              value={relay.bedrockAddress}
              copied={copiedKey === 'rbed'}
              onCopy={() => copyText('rbed', relay.bedrockAddress)}
              onQr={() => setQrModal({ title: 'Relay Bedrock Adresi', subtitle: 'Bedrock Sunucu Ekle', value: relay.bedrockAddress })}
            />
          )}
        </div>
      )}
    </div>
  );

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
            <h2 className="text-base font-bold text-white">Ag &amp; CGNAT Tanilamasi</h2>
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

        <div className="mt-3 grid gap-2.5 sm:grid-cols-4">
          <InfoBox label="Yerel Ag IP" value={diag?.localIP || system?.localIP || '...'} />
          <InfoBox label="Dis (Public) IP" value={diag?.publicIP || 'Bilinmiyor'} />
          <InfoBox label="Modem UPnP" value={diag?.upnpAvailable ? 'Destekleniyor' : 'Yanit Yok'} />
          <InfoBox
            label="CGNAT Durumu"
            value={diag?.behindCGNAT ? 'CGNAT Var' : diag?.status === 'direct_ok' ? 'Dogrudan Acik' : 'Bilinmiyor'}
            highlight={diag?.behindCGNAT ? 'warn' : 'ok'}
          />
        </div>
      </div>

      {/* Server-Aware Tunnel Section */}
      {isPureJava ? (
        <>
          {renderBoreCard()}
          <div className="glass-card rounded-2xl">
            <button
              onClick={() => setShowAdvanced(!showAdvanced)}
              className="flex w-full items-center justify-between px-5 py-3.5 text-left text-xs font-semibold text-slate-300 hover:text-white"
            >
              <span>Sabit Alan Adi / Gelismis Tuneller (SekaiLink Relay &amp; playit.gg)</span>
              {showAdvanced ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
            </button>
            {showAdvanced && (
              <div className="space-y-4 border-t border-white/10 p-4">
                {renderRelayCard()}
                {renderPlayitCard()}
              </div>
            )}
          </div>
        </>
      ) : (
        <>
          {needsBedrock && (
            <div className="flex items-center gap-2.5 rounded-2xl border border-emerald-500/25 bg-emerald-500/10 px-4 py-2.5 text-xs text-emerald-200">
              <Info className="h-4 w-4 shrink-0 text-emerald-400" />
              <span>
                Secili sunucu <strong>Bedrock (UDP)</strong> trafigi kullandigi icin <strong>playit.gg</strong> ve <strong>SekaiLink Relay</strong> secenekleri one cikarildi.
              </span>
            </div>
          )}
          {renderPlayitCard()}
          {renderRelayCard()}
          {isCrossPlay && renderBoreCard(true)}
        </>
      )}

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
                  title: 'Mobil Uzak Baglanti QR',
                  subtitle: 'Android MC SekaiLink Uzak Baglanti sekmesinde kullan',
                  value: pairingPayload,
                })
              }
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
        open={Boolean(qrModal)}
        onClose={() => setQrModal(null)}
        title={qrModal?.title || ''}
        subtitle={qrModal?.subtitle || ''}
        value={qrModal?.value || ''}
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

const CopyBox: React.FC<{
  label: string;
  value: string;
  copied: boolean;
  onCopy: () => void;
  onQr: () => void;
}> = ({ label, value, copied, onCopy, onQr }) => (
  <div className="flex items-center justify-between gap-2 rounded-2xl border border-emerald-500/35 bg-emerald-500/10 px-4 py-3">
    <div className="min-w-0">
      <div className="text-[10px] uppercase tracking-wider text-emerald-300">{label}</div>
      <div className="mt-0.5 truncate font-mono text-sm font-bold text-white">{value}</div>
    </div>
    <div className="flex shrink-0 items-center gap-1.5">
      <button onClick={onQr} className="rounded-xl bg-white/10 p-2 text-slate-200 hover:bg-white/20" title="QR Kod">
        <QrCode className="h-4 w-4" />
      </button>
      <button onClick={onCopy} className="flex items-center gap-1 rounded-xl bg-emerald-500 px-3 py-2 text-xs font-bold text-slate-950 hover:brightness-110">
        {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        {copied ? 'Kopyalandi' : 'Kopyala'}
      </button>
    </div>
  </div>
);
