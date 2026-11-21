import React, { useEffect, useState } from 'react';
import { CheckCircle2, Download, Info, Loader2, Sparkles, Upload } from 'lucide-react';
import { apiFetch, formatBytes, getApiBaseUrl } from '../lib/api';
import { Lang, translations } from '../lib/i18n';
import { PackInfo, ServerInstance } from '../types';

interface Props {
  server: ServerInstance | null;
  lang: Lang;
}

export const PacksShadersPage: React.FC<Props> = ({ server, lang }) => {
  const t = translations[lang].packs;
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
      setMsg(`${t.uploadedMsgPrefix} ${file.name}`);
      await loadPacks();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.uploadErr);
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
      setMsg(`${filename} ${t.appliedMsgSuffix}`);
      await loadPacks();
    } catch (e: unknown) {
      setMsg(e instanceof Error ? e.message : t.applyErr);
    } finally {
      setApplying('');
    }
  };

  if (!server) {
    return <div className="glass-card rounded-2xl p-8 text-center text-sm text-slate-400">{t.selectServerFirst}</div>;
  }

  return (
    <div className="space-y-4">
      <div className="glass-panel flex flex-wrap items-center justify-between gap-3 rounded-3xl p-5">
        <div>
          <h2 className="flex items-center gap-2 text-lg font-bold text-white">
            <Sparkles className="h-5 w-5 text-cyan-400" />
            {t.title}
          </h2>
          <p className="mt-1 text-xs text-slate-400">{t.subtitle}</p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <a
            href={`${getApiBaseUrl()}/api/servers/${server.id}/packs/export-mrpack`}
            className="flex items-center gap-2 rounded-xl border border-white/10 bg-white/5 px-3.5 py-2 text-xs font-semibold text-slate-200 hover:bg-white/10"
          >
            <Download className="h-4 w-4 text-emerald-400" />
            {t.exportMrpack}
          </a>
          <label className="flex cursor-pointer items-center gap-2 rounded-xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-4 py-2 text-xs font-semibold text-slate-950 hover:brightness-110">
            {uploading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
            <span>{t.uploadBtn}</span>
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
            <strong>{t.javaShaderTitle}</strong> {t.javaShaderDesc}
          </p>
          <p>
            <strong>{t.serverPackTitle}</strong> {t.serverPackDesc}
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
          {t.requireLabel} (<code>require-resource-pack=true</code>)
        </label>

        {packs.length === 0 ? (
          <div className="py-10 text-center text-sm text-slate-500">{t.empty}</div>
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
                        <CheckCircle2 className="h-3 w-3" /> {t.activeBadge}
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
                  {applying === p.filename ? t.applyingBtn : t.applyBtn}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};
