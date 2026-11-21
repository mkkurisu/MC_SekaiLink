import React, { useState } from 'react';
import { CheckCircle2, Laptop, Link2, Smartphone, Wifi } from 'lucide-react';
import { getRemoteConnection, setRemoteConnection } from '../lib/api';

interface Props {
  onReconnect: () => void;
}

export const RemoteConnectPage: React.FC<Props> = ({ onReconnect }) => {
  const initial = getRemoteConnection();
  const [enabled, setEnabled] = useState(initial.enabled);
  const [baseUrl, setBaseUrl] = useState(initial.baseUrl || 'http://192.168.1.100:25580');
  const [token, setToken] = useState(initial.token);
  const [testing, setTesting] = useState(false);
  const [status, setStatus] = useState('');

  const saveAndSwitch = async (useRemote: boolean) => {
    setTesting(true);
    setStatus('');
    try {
      if (useRemote) {
        const cleanUrl = baseUrl.replace(/\/+$/, '');
        const res = await fetch(`${cleanUrl}/api/servers`, {
          headers: { 'X-Sekai-Token': token.trim() },
        });
        if (!res.ok) {
          throw new Error(`Baglanti reddedildi (HTTP ${res.status}). URL ve Token'i kontrol edin.`);
        }
        setRemoteConnection({ enabled: true, baseUrl: cleanUrl, token: token.trim() });
        setEnabled(true);
        setStatus('Uzak PC/Linux MC SekaiLink sunucusuna baglanildi!');
      } else {
        setRemoteConnection({ enabled: false, baseUrl, token });
        setEnabled(false);
        setStatus('Yerel cihaz moduna gecildi.');
      }
      onReconnect();
    } catch (e: unknown) {
      setStatus(e instanceof Error ? e.message : 'Baglanti hatasi');
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="mx-auto max-w-xl space-y-5">
      <div className="glass-panel rounded-3xl p-6">
        <div className="flex items-center gap-3">
          <div className="rounded-2xl bg-violet-500/20 p-3 text-violet-300">
            <Link2 className="h-6 w-6" />
          </div>
          <div>
            <h2 className="text-lg font-bold text-white">Mod Secimi: Yerel Cihaz &harr; Uzak Sunucu</h2>
            <p className="text-xs text-slate-400">
              Ister bu cihazda sunucu barindir, ister baska bir PC / Linux makinedeki MC SekaiLink&apos;i kontrol et.
            </p>
          </div>
        </div>

        <div className="mt-5 grid grid-cols-2 gap-3">
          <button
            onClick={() => saveAndSwitch(false)}
            className={`flex flex-col items-center gap-2 rounded-2xl border p-4 text-center transition ${!enabled ? 'border-emerald-500/50 bg-emerald-500/15 text-white' : 'border-white/10 bg-slate-900/50 text-slate-400'}`}
          >
            <Smartphone className="h-6 w-6 text-emerald-400" />
            <span className="text-xs font-semibold">Bu Cihaz (Yerel)</span>
          </button>
          <button
            onClick={() => setEnabled(true)}
            className={`flex flex-col items-center gap-2 rounded-2xl border p-4 text-center transition ${enabled ? 'border-violet-500/50 bg-violet-500/15 text-white' : 'border-white/10 bg-slate-900/50 text-slate-400'}`}
          >
            <Laptop className="h-6 w-6 text-violet-400" />
            <span className="text-xs font-semibold">Uzak PC / Linux</span>
          </button>
        </div>

        <div className="mt-5 space-y-3">
          <div>
            <label className="mb-1 block text-xs text-slate-400">Uzak MC SekaiLink Adresi</label>
            <input
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              placeholder="http://192.168.1.100:25580"
              className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-3.5 py-2.5 font-mono text-sm text-white outline-none focus:border-violet-500"
            />
          </div>
          <div>
            <label className="mb-1 block text-xs text-slate-400">Eslestirme Tokeni (Pairing Token)</label>
            <input
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="SL-..."
              className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-3.5 py-2.5 font-mono text-sm text-white outline-none focus:border-violet-500"
            />
          </div>

          <button
            disabled={testing}
            onClick={() => saveAndSwitch(true)}
            className="flex w-full items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-violet-500 to-cyan-500 py-3 text-sm font-semibold text-slate-950 hover:brightness-110 disabled:opacity-50"
          >
            <Wifi className="h-4 w-4" />
            {testing ? 'Baglanti Test Ediliyor...' : 'Uzak Sunucuya Baglan'}
          </button>
        </div>

        {status && (
          <div className="mt-4 flex items-center gap-2 rounded-xl border border-white/10 bg-slate-950/70 px-4 py-2.5 text-xs text-cyan-200">
            <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400" />
            <span>{status}</span>
          </div>
        )}
      </div>
    </div>
  );
};
