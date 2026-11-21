import React, { useEffect, useState } from 'react';
import { Archive, Check, Download, Globe2, Loader2, RotateCcw, Trash2, Upload } from 'lucide-react';
import { apiFetch, formatBytes, getApiBaseUrl } from '../lib/api';
import { BackupInfo, ServerInstance, WorldInfo } from '../types';

interface Props {
  server: ServerInstance | null;
  onRefresh: () => void;
}

export const WorldsPage: React.FC<Props> = ({ server, onRefresh }) => {
  const [worlds, setWorlds] = useState<WorldInfo[]>([]);
  const [backups, setBackups] = useState<BackupInfo[]>([]);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState('');

  useEffect(() => {
    if (server) loadAll();
  }, [server?.id]);

  const loadAll = async () => {
    if (!server) return;
    const [w, b] = await Promise.all([
      apiFetch<WorldInfo[]>(`/api/servers/${server.id}/worlds`),
      apiFetch<BackupInfo[]>(`/api/servers/${server.id}/backups`),
    ]);
    setWorlds(w || []);
    setBackups(b || []);
  };

  const handleImport = async (file: File) => {
    if (!server) return;
    setBusy(true);
    setMsg('');
    try {
      const fd = new FormData();
      fd.append('file', file);
      fd.append('name', file.name.replace(/\.(zip|mcworld)$/i, ''));
      const res = await apiFetch<{ name: string }>(`/api/servers/${server.id}/worlds/import`, {
        method: 'POST',
        body: fd,
      });
      setMsg(`Dunya iceri aktarildi: ${res.name}`);
      await loadAll();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : 'Iceri aktarma hatasi');
    } finally {
      setBusy(false);
    }
  };

  const switchWorld = async (name: string) => {
    if (!server) return;
    await apiFetch(`/api/servers/${server.id}/worlds/switch`, {
      method: 'POST',
      body: JSON.stringify({ name }),
    });
    setMsg(`Aktif dunya "${name}" olarak degistirildi.`);
    await loadAll();
    onRefresh();
  };

  const createBackup = async (worldName?: string) => {
    if (!server) return;
    setBusy(true);
    try {
      const b = await apiFetch<BackupInfo>(`/api/servers/${server.id}/backups`, {
        method: 'POST',
        body: JSON.stringify({ worldName: worldName || server.activeWorld }),
      });
      setMsg(`Yedek alindi: ${b.filename}`);
      await loadAll();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : 'Yedekleme hatasi');
    } finally {
      setBusy(false);
    }
  };

  const restoreBackup = async (filename: string) => {
    if (!server) return;
    setBusy(true);
    try {
      await apiFetch(`/api/servers/${server.id}/backups/${encodeURIComponent(filename)}/restore`, { method: 'POST' });
      setMsg(`Yedekten geri yuklendi: ${filename}`);
      await loadAll();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : 'Geri yukleme hatasi');
    } finally {
      setBusy(false);
    }
  };

  if (!server) {
    return <div className="glass-card rounded-2xl p-8 text-center text-sm text-slate-400">Once bir sunucu secin.</div>;
  }

  return (
    <div className="space-y-5">
      <div className="glass-panel flex flex-wrap items-center justify-between gap-3 rounded-3xl p-5">
        <div>
          <h2 className="flex items-center gap-2 text-lg font-bold text-white">
            <Globe2 className="h-5 w-5 text-emerald-400" />
            Dunya &amp; Yedekleme Yoneticisi
          </h2>
          <p className="mt-1 text-xs text-slate-400">Hazir harita (.zip / .mcworld) yukle, dunyalar arasi gecis yap ve tek tikla yedek al.</p>
        </div>

        <div className="flex flex-wrap gap-2">
          <button
            disabled={busy}
            onClick={() => createBackup()}
            className="flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/15 px-4 py-2 text-xs font-semibold text-emerald-300 hover:bg-emerald-500/25"
          >
            {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Archive className="h-4 w-4" />}
            Hizli Yedek Al
          </button>
          <label className="flex cursor-pointer items-center gap-2 rounded-xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-4 py-2 text-xs font-semibold text-slate-950 hover:brightness-110">
            <Upload className="h-4 w-4" />
            <span>Dunya Yukle (.zip / .mcworld)</span>
            <input
              type="file"
              className="hidden"
              accept=".zip,.mcworld"
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) handleImport(f);
              }}
            />
          </label>
        </div>
      </div>

      {msg && (
        <div className="rounded-2xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-2.5 text-sm text-emerald-200">{msg}</div>
      )}

      <div className="grid gap-4 md:grid-cols-2">
        <div className="glass-card rounded-2xl p-5">
          <h3 className="mb-3 text-sm font-semibold text-white">Yuklu Dunyalar</h3>
          {worlds.length === 0 ? (
            <p className="py-6 text-center text-xs text-slate-500">Sunucu en az bir kez baslatildiginda dunya klasoru burada gorunur veya harita yukleyebilirsiniz.</p>
          ) : (
            <div className="space-y-2.5">
              {worlds.map((w) => (
                <div key={w.name} className="flex items-center justify-between gap-3 rounded-xl border border-white/10 bg-slate-950/60 p-3.5">
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="font-semibold text-white">{w.name}</span>
                      {w.active && (
                        <span className="rounded-full bg-emerald-500/20 px-2 py-0.5 text-[10px] font-semibold text-emerald-300">Aktif</span>
                      )}
                    </div>
                    <div className="mt-0.5 text-xs text-slate-400">{formatBytes(w.sizeBytes)} • {w.edition}</div>
                  </div>
                  <div className="flex items-center gap-1.5">
                    {!w.active && (
                      <button
                        onClick={() => switchWorld(w.name)}
                        className="flex items-center gap-1 rounded-lg bg-emerald-500/20 px-2.5 py-1.5 text-xs font-medium text-emerald-300 hover:bg-emerald-500/30"
                      >
                        <Check className="h-3.5 w-3.5" /> Sec
                      </button>
                    )}
                    <a
                      href={`${getApiBaseUrl()}/api/servers/${server.id}/worlds/${encodeURIComponent(w.name)}/export`}
                      className="rounded-lg bg-white/5 p-1.5 text-slate-300 hover:bg-white/10"
                      title="ZIP Indir"
                    >
                      <Download className="h-3.5 w-3.5" />
                    </a>
                    {!w.active && (
                      <button
                        onClick={async () => {
                          await apiFetch(`/api/servers/${server.id}/worlds/${encodeURIComponent(w.name)}`, { method: 'DELETE' });
                          loadAll();
                        }}
                        className="rounded-lg bg-rose-500/15 p-1.5 text-rose-300 hover:bg-rose-500/25"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="glass-card rounded-2xl p-5">
          <h3 className="mb-3 text-sm font-semibold text-white">Yedekler ({backups.length})</h3>
          {backups.length === 0 ? (
            <p className="py-6 text-center text-xs text-slate-500">Henuz alinmis bir yedek yok.</p>
          ) : (
            <div className="space-y-2.5">
              {backups.map((b) => (
                <div key={b.filename} className="flex items-center justify-between gap-3 rounded-xl border border-white/10 bg-slate-950/60 p-3.5">
                  <div className="min-w-0">
                    <div className="truncate text-sm font-medium text-white">{b.filename}</div>
                    <div className="text-xs text-slate-400">
                      {formatBytes(b.sizeBytes)} • {new Date(b.createdAt).toLocaleString()}
                    </div>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <button
                      onClick={() => restoreBackup(b.filename)}
                      className="flex items-center gap-1 rounded-lg bg-cyan-500/20 px-2.5 py-1.5 text-xs font-medium text-cyan-300 hover:bg-cyan-500/30"
                    >
                      <RotateCcw className="h-3.5 w-3.5" /> Geri Yukle
                    </button>
                    <button
                      onClick={async () => {
                        await apiFetch(`/api/servers/${server.id}/backups/${encodeURIComponent(b.filename)}`, { method: 'DELETE' });
                        loadAll();
                      }}
                      className="rounded-lg bg-rose-500/15 p-1.5 text-rose-300 hover:bg-rose-500/25"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
