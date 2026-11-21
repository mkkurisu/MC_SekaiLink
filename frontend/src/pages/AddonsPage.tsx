import React, { useEffect, useState } from 'react';
import { Download, Loader2, Package, Power, Search, Trash2, Upload } from 'lucide-react';
import { apiFetch, formatBytes } from '../lib/api';
import { Lang, translations } from '../lib/i18n';
import { InstalledAddon, ModrinthHit, ModrinthSearchResult, ServerInstance } from '../types';

interface Props {
  server: ServerInstance | null;
  lang: Lang;
}

export const AddonsPage: React.FC<Props> = ({ server, lang }) => {
  const t = translations[lang].addons;
  const [tab, setTab] = useState<'store' | 'installed'>('store');
  const allowedTypes: ('plugin' | 'mod' | 'datapack')[] =
    !server
      ? ['plugin', 'mod', 'datapack']
      : server.engine === 'fabric' || server.engine === 'neoforge'
        ? ['mod', 'datapack']
        : server.engine === 'paper' || server.engine === 'purpur'
          ? ['plugin', 'datapack']
          : server.engine === 'vanilla'
            ? ['datapack']
            : ['plugin', 'datapack'];

  const [projectType, setProjectType] = useState<'mod' | 'plugin' | 'datapack'>(allowedTypes[0]);
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<ModrinthHit[]>([]);
  const [searching, setSearching] = useState(false);
  const [installingId, setInstallingId] = useState('');
  const [installed, setInstalled] = useState<InstalledAddon[]>([]);
  const [uploading, setUploading] = useState(false);
  const [banner, setBanner] = useState('');

  useEffect(() => {
    if (!server) return;
    if (server.engine === 'fabric' || server.engine === 'neoforge') {
      setProjectType('mod');
    } else if (server.engine === 'paper' || server.engine === 'purpur') {
      setProjectType('plugin');
    } else {
      setProjectType('datapack');
    }
    loadInstalled();
  }, [server?.id]);

  useEffect(() => {
    if (server && tab === 'store') {
      runSearch('');
    }
  }, [server?.id, projectType]);

  const loadInstalled = async () => {
    if (!server) return;
    try {
      const list = await apiFetch<InstalledAddon[]>(`/api/servers/${server.id}/addons`);
      setInstalled(list || []);
    } catch {
      // ignore
    }
  };

  const runSearch = async (q: string) => {
    if (!server) return;
    setSearching(true);
    try {
      let loader = '';
      if (projectType === 'plugin' && (server.engine === 'paper' || server.engine === 'purpur')) loader = 'paper';
      if (projectType === 'mod' && (server.engine === 'fabric' || server.engine === 'neoforge')) loader = server.engine;
      if (projectType === 'datapack') loader = 'datapack';

      const params = new URLSearchParams({
        query: q,
        projectType: projectType === 'datapack' ? 'datapack' : 'mod',
        loader,
        mcVersion: server.mcVersion === 'latest' ? '' : server.mcVersion,
        limit: '18',
      });
      const res = await apiFetch<ModrinthSearchResult>(`/api/modrinth/search?${params.toString()}`);
      setResults(res.hits || []);
    } catch (e: unknown) {
      setBanner(e instanceof Error ? e.message : t.searchErr);
    } finally {
      setSearching(false);
    }
  };

  const installFromModrinth = async (hit: ModrinthHit) => {
    if (!server) return;
    setInstallingId(hit.project_id);
    setBanner('');
    try {
      const res = await apiFetch<{ installed: string[] }>(`/api/servers/${server.id}/addons/install-modrinth`, {
        method: 'POST',
        body: JSON.stringify({ projectId: hit.project_id, kind: projectType }),
      });
      setBanner(`${t.installedMsg} ${(res.installed || []).join(', ')}`);
      await loadInstalled();
    } catch (e: unknown) {
      setBanner(`${t.errorPrefix} ${e instanceof Error ? e.message : t.installFailed}`);
    } finally {
      setInstallingId('');
    }
  };

  const handleUpload = async (file: File) => {
    if (!server) return;
    setUploading(true);
    setBanner('');
    try {
      const fd = new FormData();
      fd.append('file', file);
      fd.append('kind', projectType);
      const res = await apiFetch<{ installed: string }>(`/api/servers/${server.id}/addons/upload`, {
        method: 'POST',
        body: fd,
      });
      setBanner(`${t.uploadedMsg} ${res.installed}`);
      await loadInstalled();
      setTab('installed');
    } catch (e: unknown) {
      setBanner(`${t.uploadErrorPrefix} ${e instanceof Error ? e.message : t.uploadFailed}`);
    } finally {
      setUploading(false);
    }
  };

  const toggleAddon = async (path: string) => {
    if (!server) return;
    await apiFetch(`/api/servers/${server.id}/addons/toggle`, {
      method: 'POST',
      body: JSON.stringify({ path }),
    });
    await loadInstalled();
  };

  const removeAddon = async (path: string) => {
    if (!server) return;
    await apiFetch(`/api/servers/${server.id}/addons?path=${encodeURIComponent(path)}`, { method: 'DELETE' });
    await loadInstalled();
  };

  if (!server) {
    return <div className="glass-card rounded-2xl p-8 text-center text-sm text-slate-400">{t.selectServerFirst}</div>;
  }

  return (
    <div className="space-y-4">
      <div className="glass-panel flex flex-wrap items-center justify-between gap-3 rounded-3xl p-5">
        <div className="flex items-center gap-2">
          <button
            onClick={() => setTab('store')}
            className={`rounded-xl px-4 py-2 text-sm font-semibold transition ${tab === 'store' ? 'bg-emerald-500 text-slate-950' : 'bg-white/5 text-slate-300 hover:bg-white/10'}`}
          >
            {t.storeTab}
          </button>
          <button
            onClick={() => {
              setTab('installed');
              loadInstalled();
            }}
            className={`rounded-xl px-4 py-2 text-sm font-semibold transition ${tab === 'installed' ? 'bg-emerald-500 text-slate-950' : 'bg-white/5 text-slate-300 hover:bg-white/10'}`}
          >
            {t.installedTab} ({installed.length})
          </button>
        </div>

        <label className="flex cursor-pointer items-center gap-2 rounded-xl border border-cyan-500/30 bg-cyan-500/10 px-4 py-2 text-xs font-semibold text-cyan-300 hover:bg-cyan-500/20">
          {uploading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
          <span>{t.uploadBtn}</span>
          <input
            type="file"
            className="hidden"
            accept=".jar,.zip,.mcpack,.mcaddon"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) handleUpload(f);
            }}
          />
        </label>
      </div>

      {banner && (
        <div className="rounded-2xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-2.5 text-sm text-emerald-200">
          {banner}
        </div>
      )}

      {tab === 'store' ? (
        <div className="space-y-4">
          <div className="glass-card flex flex-wrap items-center gap-3 rounded-2xl p-4">
            <div className="flex rounded-xl bg-slate-950/70 p-1">
              {allowedTypes.map((k) => (
                <button
                  key={k}
                  onClick={() => setProjectType(k)}
                  className={`rounded-lg px-3 py-1.5 text-xs font-semibold ${projectType === k ? 'bg-emerald-500/20 text-emerald-300' : 'text-slate-400 hover:text-white'}`}
                >
                  {k === 'plugin' ? t.typePlugin : k === 'mod' ? t.typeMod : t.typeDatapack}
                </button>
              ))}
            </div>
            <div className="flex flex-1 items-center gap-2">
              <div className="relative flex-1">
                <Search className="absolute left-3 top-2.5 h-4 w-4 text-slate-500" />
                <input
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && runSearch(query)}
                  placeholder={t.searchPlaceholder}
                  className="w-full rounded-xl border border-white/10 bg-slate-950/80 py-2 pl-9 pr-4 text-sm text-white outline-none focus:border-emerald-500"
                />
              </div>
              <button
                onClick={() => runSearch(query)}
                className="rounded-xl bg-emerald-500 px-4 py-2 text-sm font-semibold text-slate-950 hover:brightness-110"
              >
                {t.searchBtn}
              </button>
            </div>
          </div>

          {searching ? (
            <div className="flex items-center justify-center py-16 text-slate-400">
              <Loader2 className="mr-2 h-5 w-5 animate-spin text-emerald-400" />
              {t.searching}
            </div>
          ) : (
            <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
              {results.map((hit) => (
                <div key={hit.project_id} className="glass-card flex flex-col justify-between rounded-2xl p-4">
                  <div className="flex items-start gap-3">
                    {hit.icon_url ? (
                      <img src={hit.icon_url} alt="" className="h-12 w-12 rounded-xl object-cover bg-slate-800" />
                    ) : (
                      <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-white/5 text-slate-400">
                        <Package className="h-6 w-6" />
                      </div>
                    )}
                    <div className="min-w-0 flex-1">
                      <h3 className="truncate font-semibold text-white">{hit.title}</h3>
                      <div className="text-[11px] text-slate-400">{hit.author} • {hit.downloads.toLocaleString()} {t.downloadsLabel}</div>
                      <p className="mt-1.5 line-clamp-2 text-xs text-slate-400">{hit.description}</p>
                    </div>
                  </div>
                  <button
                    disabled={installingId === hit.project_id}
                    onClick={() => installFromModrinth(hit)}
                    className="mt-4 flex w-full items-center justify-center gap-1.5 rounded-xl bg-emerald-500/20 py-2 text-xs font-semibold text-emerald-300 hover:bg-emerald-500/30 disabled:opacity-50"
                  >
                    {installingId === hit.project_id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
                    {t.installBtn}
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      ) : (
        <div className="glass-card overflow-hidden rounded-2xl">
          {installed.length === 0 ? (
            <div className="p-10 text-center text-sm text-slate-400">{t.emptyInstalled}</div>
          ) : (
            <div className="divide-y divide-white/5">
              {installed.map((item) => (
                <div key={item.path} className="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5">
                  <div>
                    <div className="flex items-center gap-2">
                      <span className={`font-medium ${item.enabled ? 'text-white' : 'text-slate-500 line-through'}`}>{item.name}</span>
                      <span className="rounded-md bg-white/5 px-2 py-0.5 text-[10px] uppercase text-cyan-300">{item.kind}</span>
                    </div>
                    <div className="text-xs text-slate-500">{item.path} {item.sizeBytes > 0 ? `• ${formatBytes(item.sizeBytes)}` : ''}</div>
                  </div>
                  <div className="flex items-center gap-2">
                    {!item.kind.startsWith('bedrock') && (
                      <button
                        onClick={() => toggleAddon(item.path)}
                        className={`flex items-center gap-1 rounded-xl px-3 py-1.5 text-xs font-medium ${item.enabled ? 'bg-emerald-500/15 text-emerald-300' : 'bg-slate-700/50 text-slate-300'}`}
                      >
                        <Power className="h-3.5 w-3.5" />
                        {item.enabled ? t.active : t.disabled}
                      </button>
                    )}
                    <button
                      onClick={() => removeAddon(item.path)}
                      className="rounded-xl bg-rose-500/15 p-2 text-rose-300 hover:bg-rose-500/25"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
};
