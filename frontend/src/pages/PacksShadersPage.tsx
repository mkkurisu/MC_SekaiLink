import React, { useEffect, useState } from 'react';
import { CheckCircle2, Download, Info, Loader2, Sparkles, Upload } from 'lucide-react';
import { apiFetch, formatBytes, getApiBaseUrl } from '../lib/api';
import { PackInfo, ServerInstance } from '../types';

interface Props {
  server: ServerInstance | null;
}

export const PacksShadersPage: React.FC<Props> = ({ server }) => {
  const [packs, setPacks] = useState<PackInfo[]>([]);
  const [required, setRequired] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [applying, setApplying] = useState('');
  const [msg, setMsg] = useState('');

  useEffect(() => {
    if (server) loadPacks();
  }, [server?.id]);

  const loadPacks = async () => {
    if (!server) return;
    const list = await apiFetch<PackInfo[]>(`/api/servers/${server.id}/packs`);
    setPacks(list || []);
  };

  const handleUpload = async (file: File) => {
    if (!server) return;
    setUploading(true);
    setMsg('');
    try {
      const fd = new FormData();
      fd.append('file', file);
      await apiFetch<PackInfo>(`/api/servers/${server.id}/packs/upload`, { method: 'POST', body: fd });
      setMsg(`Yuklendi ve SHA-1 hesaplandi: ${file.name}`);
      await loadPacks();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : 'Yukleme hatasi');
    } finally {
      setUploading(false);
    }
  };

  const applyToServer = async (filename: string) => {
    if (!server) return;
    setApplying(filename);
    setMsg('');
    try {
      await apiFetch(`/api/servers/${server.id}/packs/apply`, {
        method: 'POST',
        body: JSON.stringify({ filename, required }),
      });
      setMsg(`${filename} sunucuya baglandi! Oyuncular giriste otomatik indirecek.`);
      await loadPacks();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : 'Uygulama hatasi');
    } finally {
      setApplying('');
    }
  };

  if (!server) {
    return <div className="glass-card rounded-2xl p-8 text-center text-sm text-slate-400">Once bir sunucu secin.</div>;
  }

  return (
    <div className="space-y-4">
      <div className="glass-panel flex flex-wrap items-center justify-between gap-3 rounded-3xl p-5">
        <div>
          <h2 className="flex items-center gap-2 text-lg font-bold text-white">
            <Sparkles className="h-5 w-5 text-cyan-400" />
            Shader, Resource Pack & Istemci Paketi Merkezi
          </h2>
          <p className="mt-1 text-xs text-slate-400">
            Vanilla Core Shader / Kaynak Paketi (.zip) yukle, otomatik SHA-1 uret ve sunucuya tek tikla bagla.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <a
            href={`${getApiBaseUrl()}/api/servers/${server.id}/packs/export-mrpack`}
            className="flex items-center gap-2 rounded-xl border border-white/10 bg-white/5 px-3.5 py-2 text-xs font-semibold text-slate-200 hover:bg-white/10"
          >
            <Download className="h-4 w-4 text-emerald-400" />
            Arkadaslar Icin .mrpack Indir
          </a>
          <label className="flex cursor-pointer items-center gap-2 rounded-xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-4 py-2 text-xs font-semibold text-slate-950 hover:brightness-110">
            {uploading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
            <span>Paket / Shader Yukle (.zip / .mcpack)</span>
            <input
              type="file"
              className="hidden"
              accept=".zip,.mcpack"
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) handleUpload(f);
              }}
            />
          </label>
        </div>
      </div>

      <div className="flex items-start gap-3 rounded-2xl border border-cyan-500/25 bg-cyan-500/10 p-4 text-xs text-cyan-100">
        <Info className="mt-0.5 h-4 w-4 shrink-0 text-cyan-400" />
        <div className="space-y-1">
          <p>
            <strong>Java Shader Notu:</strong> Iris / OptiFine shader&apos;lari (BSL, Complementary vb.) oyuncunun GPU&apos;sunda calisir; arkadaslarina dagitmak icin yukaridaki <code>.mrpack</code> disa aktarimini kullanabilirsin.
          </p>
          <p>
            <strong>Sunucu Zorunlu Paket &amp; Vanilla Core Shaders:</strong> Buraya yuklenen <code>.zip</code> kaynak/core-shader paketleri dahili HTTP sunucumuzdan otomatik SHA-1 ile tum oyunculara indirilir.
          </p>
        </div>
      </div>

      {msg && (
        <div className="rounded-2xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-2.5 text-sm text-emerald-200">{msg}</div>
      )}

      <div className="glass-card rounded-2xl p-4">
        <label className="mb-3 flex items-center gap-2 text-xs text-slate-300">
          <input
            type="checkbox"
            checked={required}
            onChange={(e) => setRequired(e.target.checked)}
            className="h-4 w-4 accent-emerald-500"
          />
          Paketi oyuncular icin zorunlu kil (<code>require-resource-pack=true</code>)
        </label>

        {packs.length === 0 ? (
          <div className="py-10 text-center text-sm text-slate-500">Henuz yuklenmis kaynak paketi veya shader paketi yok.</div>
        ) : (
          <div className="space-y-2.5">
            {packs.map((p) => (
              <div key={p.filename} className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-white/10 bg-slate-950/60 p-4">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="font-semibold text-white">{p.filename}</span>
                    <span className="rounded bg-white/10 px-2 py-0.5 text-[10px] uppercase text-cyan-300">{p.kind}</span>
                    {p.applied && (
                      <span className="flex items-center gap-1 rounded-full bg-emerald-500/20 px-2.5 py-0.5 text-[10px] font-semibold text-emerald-300">
                        <CheckCircle2 className="h-3 w-3" /> Sunucuda Aktif
                      </span>
                    )}
                  </div>
                  <div className="mt-1 font-mono text-[11px] text-slate-500">
                    {formatBytes(p.sizeBytes)} • SHA-1: {p.sha1}
                  </div>
                </div>
                <button
                  disabled={applying === p.filename}
                  onClick={() => applyToServer(p.filename)}
                  className="rounded-xl bg-emerald-500/20 px-4 py-2 text-xs font-semibold text-emerald-300 hover:bg-emerald-500/30 disabled:opacity-50"
                >
                  {applying === p.filename ? 'Uygulaniyor...' : 'Sunucuya Uygula'}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};
