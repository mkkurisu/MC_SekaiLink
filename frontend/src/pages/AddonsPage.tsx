import React, { useEffect, useRef, useState } from 'react';
import { AlertTriangle, CheckCircle2, Download, Loader2, Package, Power, RefreshCw, Search, Trash2, Upload } from 'lucide-react';
import { apiFetch, formatBytes } from '../lib/api';
import { Lang, translations } from '../lib/i18n';
import { AddonInstallResult, InstalledAddon, ModrinthHit, ModrinthSearchResult, ServerInstance } from '../types';

interface Props {
  server: ServerInstance | null;
  lang: Lang;
}

interface StatusBannerState {
  type: 'success' | 'error';
  message: string;
  dependencies?: string[];
  missingDependencies?: string[];
  warnings?: string[];
}

export const AddonsPage: React.FC<Props> = ({ server, lang }) => {
  const t = translations[lang].addons;
  const supportsModrinth = !!server && server.engine !== 'bds' && server.engine !== 'powernukkitx';

  const allowedTypes: ('plugin' | 'mod' | 'datapack')[] =
    !server
      ? ['plugin', 'mod', 'datapack']
      : server.engine === 'fabric' || server.engine === 'neoforge'
        ? ['mod', 'datapack']
        : server.engine === 'paper' || server.engine === 'purpur'
          ? ['plugin', 'datapack']
          : ['datapack'];

  const defaultTypeForEngine = (eng?: ServerInstance['engine']): 'plugin' | 'mod' | 'datapack' => {
    if (eng === 'fabric' || eng === 'neoforge') return 'mod';
    if (eng === 'paper' || eng === 'purpur') return 'plugin';
    return 'datapack';
  };

  const [tab, setTab] = useState<'store' | 'installed'>('store');
  const [projectType, setProjectType] = useState<'mod' | 'plugin' | 'datapack'>(() => defaultTypeForEngine(server?.engine));
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<ModrinthHit[]>([]);
  const [searching, setSearching] = useState(false);
  const [installingId, setInstallingId] = useState('');
  const [installed, setInstalled] = useState<InstalledAddon[]>([]);
  const [uploading, setUploading] = useState(false);
  const [statusBanner, setStatusBanner] = useState<StatusBannerState | null>(null);
  const [needsRestart, setNeedsRestart] = useState(false);
  const skipAutoSearchRef = useRef(false);

  useEffect(() => {
    if (!server) return;
    const defType = defaultTypeForEngine(server.engine);
    setProjectType(defType);
    setStatusBanner(null);
    setNeedsRestart(false);
    setQuery('');
    setResults([]);
    if (server.engine === 'bds' || server.engine === 'powernukkitx') {
      setTab('installed');
    } else {
      setTab('store');
    }
    loadInstalled();
  }, [server?.id]);

  useEffect(() => {
    if (!server || !supportsModrinth) return;
    if (skipAutoSearchRef.current) {
      skipAutoSearchRef.current = false;
      return;
    }
    runSearch('');
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

  const runSearch = async (q: string, overrideType?: 'mod' | 'plugin' | 'datapack') => {
    if (!server || !supportsModrinth) return;
    const activeType = overrideType ?? projectType;
    setSearching(true);
    try {
      let loader = '';
      if (activeType === 'plugin' && (server.engine === 'paper' || server.engine === 'purpur')) loader = server.engine;
      if (activeType === 'mod' && (server.engine === 'fabric' || server.engine === 'neoforge')) loader = server.engine;
      if (activeType === 'datapack') loader = 'datapack';

      const params = new URLSearchParams({
        query: q,
        projectType: activeType,
        loader,
        engine: server.engine,
        mcVersion: server.mcVersion === 'latest' ? '' : server.mcVersion,
        limit: '18',
      });
      const res = await apiFetch<ModrinthSearchResult>(`/api/modrinth/search?${params.toString()}`);
      setResults(res.hits || []);
    } catch (e: unknown) {
      setStatusBanner({
        type: 'error',
        message: `${t.searchErr}: ${e instanceof Error ? e.message : t.installFailed}`,
      });
    } finally {
      setSearching(false);
    }
  };

  const searchDependencyInStore = async (depName: string) => {
    if (!server || !supportsModrinth) return;
    const primaryType = defaultTypeForEngine(server.engine);
    setQuery(depName);
    if (projectType !== primaryType) {
      skipAutoSearchRef.current = true;
      setProjectType(primaryType);
    }
    setTab('store');
    await runSearch(depName, primaryType);
  };

  const installFromModrinth = async (hit: ModrinthHit) => {
    if (!server) return;
    setInstallingId(hit.project_id);
    setStatusBanner(null);
    try {
      const res = await apiFetch<AddonInstallResult>(`/api/servers/${server.id}/addons/install-modrinth`, {
        method: 'POST',
        body: JSON.stringify({ projectId: hit.project_id, kind: projectType }),
      });
      setStatusBanner({
        type: 'success',
        message: `${t.installedMsg} ${(res.installed || []).join(', ')}`,
        dependencies: res.dependencies,
        missingDependencies: res.missingDependencies,
        warnings: res.warnings,
      });
      if (server.status === 'online' || server.status === 'starting') {
        setNeedsRestart(true);
      }
      await loadInstalled();
    } catch (e: unknown) {
      setStatusBanner({
        type: 'error',
        message: `${t.errorPrefix} ${e instanceof Error ? e.message : t.installFailed}`,
      });
    } finally {
      setInstallingId('');
    }
  };

  const handleUpload = async (file: File) => {
    if (!server) return;
    setUploading(true);
    setStatusBanner(null);
    try {
      const fd = new FormData();
      fd.append('file', file);
      fd.append('kind', projectType);
      const res = await apiFetch<AddonInstallResult>(`/api/servers/${server.id}/addons/upload`, {
        method: 'POST',
        body: fd,
      });
      setStatusBanner({
        type: 'success',
        message: `${t.uploadedMsg} ${(res.installed || [file.name]).join(', ')}`,
        dependencies: res.dependencies,
        missingDependencies: res.missingDependencies,
        warnings: res.warnings,
      });
      if (server.status === 'online' || server.status === 'starting') {
        setNeedsRestart(true);
      }
      await loadInstalled();
      setTab('installed');
    } catch (e: unknown) {
      setStatusBanner({
        type: 'error',
        message: `${t.uploadErrorPrefix} ${e instanceof Error ? e.message : t.uploadFailed}`,
      });
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
    if (server.status === 'online' || server.status === 'starting') {
      setNeedsRestart(true);
    }
    await loadInstalled();
  };

  const removeAddon = async (path: string) => {
    if (!server) return;
    await apiFetch(`/api/servers/${server.id}/addons?path=${encodeURIComponent(path)}`, { method: 'DELETE' });
    if (server.status === 'online' || server.status === 'starting') {
      setNeedsRestart(true);
    }
    await loadInstalled();
  };

  if (!server) {
    return <div className="glass-card rounded-2xl p-8 text-center text-sm text-slate-400">{t.selectServerFirst}</div>;
  }

  const issueCount = installed.filter(
    (item) => (item.missingDeps && item.missingDeps.length > 0) || (item.warnings && item.warnings.length > 0)
  ).length;

  const uploadAccept =
    server.engine === 'bds'
      ? '.mcpack,.mcaddon'
      : server.engine === 'powernukkitx'
        ? '.jar,.mcpack,.mcaddon'
        : server.engine === 'vanilla'
          ? '.zip'
          : '.jar,.zip,.mcpack,.mcaddon';

  const uploadLabel =
    server.engine === 'bds'
      ? t.uploadBtnBedrock
      : server.engine === 'powernukkitx'
        ? t.uploadBtnPnx
        : server.engine === 'vanilla'
          ? t.uploadBtnVanilla
          : t.uploadBtn;

  return (
    <div className="space-y-4">
      <div className="glass-panel flex flex-wrap items-center justify-between gap-3 rounded-3xl p-5">
        <div className="flex items-center gap-2">
          {supportsModrinth && (
            <button
              onClick={() => setTab('store')}
              className={`rounded-xl px-4 py-2 text-sm font-semibold transition ${tab === 'store' ? 'bg-emerald-500 text-slate-950' : 'bg-white/5 text-slate-300 hover:bg-white/10'}`}
            >
              {t.storeTab}
            </button>
          )}
          <button
            onClick={() => {
              setTab('installed');
              loadInstalled();
            }}
            className={`flex items-center gap-2 rounded-xl px-4 py-2 text-sm font-semibold transition ${tab === 'installed' ? 'bg-emerald-500 text-slate-950' : 'bg-white/5 text-slate-300 hover:bg-white/10'}`}
          >
            <span>
              {t.installedTab} ({installed.length})
            </span>
            {issueCount > 0 && (
              <span className="rounded-md bg-amber-500 px-1.5 py-0.5 text-[10px] font-bold text-slate-950">
                {issueCount} {t.issuesBadge}
              </span>
            )}
          </button>
        </div>

        <label className="flex cursor-pointer items-center gap-2 rounded-xl border border-cyan-500/30 bg-cyan-500/10 px-4 py-2 text-xs font-semibold text-cyan-300 hover:bg-cyan-500/20">
          {uploading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
          <span>{uploadLabel}</span>
          <input
            type="file"
            className="hidden"
            accept={uploadAccept}
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) handleUpload(f);
              e.target.value = '';
            }}
          />
        </label>
      </div>

      {!supportsModrinth && (
        <div className="rounded-2xl border border-cyan-500/30 bg-cyan-500/10 px-4 py-3 text-xs text-cyan-200">
          <div className="font-semibold text-cyan-100">
            {server.engine === 'bds' ? t.bedrockOnlyTitle : t.pnxOnlyTitle}
          </div>
          <div className="mt-0.5 text-cyan-200/85">
            {server.engine === 'bds' ? t.bedrockOnlyDesc : t.pnxOnlyDesc}
          </div>
        </div>
      )}

      {server.engine === 'vanilla' && (
        <div className="rounded-2xl border border-slate-600/40 bg-slate-800/40 px-4 py-2.5 text-xs text-slate-300">
          {t.vanillaNote}
        </div>
      )}

      {needsRestart && (
        <div className="flex items-center gap-2 rounded-2xl border border-amber-500/35 bg-amber-500/10 px-4 py-2.5 text-xs text-amber-200">
          <RefreshCw className="h-4 w-4 shrink-0 text-amber-400" />
          <span>{t.restartHint}</span>
        </div>
      )}

      {statusBanner && (
        <div
          className={`space-y-2 rounded-2xl border px-4 py-3 text-sm ${
            statusBanner.type === 'error'
              ? 'border-rose-500/35 bg-rose-500/10 text-rose-200'
              : 'border-emerald-500/30 bg-emerald-500/10 text-emerald-200'
          }`}
        >
          <div className="flex items-center gap-2">
            {statusBanner.type === 'error' ? (
              <AlertTriangle className="h-4 w-4 shrink-0 text-rose-400" />
            ) : (
              <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400" />
            )}
            <span>{statusBanner.message}</span>
          </div>

          {statusBanner.dependencies && statusBanner.dependencies.length > 0 && (
            <div className="text-xs text-emerald-300/90">
              {t.autoDepsLabel} <span className="font-medium">{statusBanner.dependencies.join(', ')}</span>
            </div>
          )}

          {statusBanner.missingDependencies && statusBanner.missingDependencies.length > 0 && (
            <div className="rounded-xl border border-amber-500/35 bg-amber-500/10 p-2.5 text-xs text-amber-200">
              <div className="mb-1.5 flex items-center gap-1.5 font-semibold text-amber-300">
                <AlertTriangle className="h-3.5 w-3.5" />
                {t.missingDepsTitle}
              </div>
              <div className="flex flex-wrap gap-2">
                {statusBanner.missingDependencies.map((dep) => (
                  <div key={dep} className="flex items-center gap-1.5 rounded-lg bg-slate-950/60 px-2.5 py-1 text-amber-100">
                    <span className="font-mono">{dep}</span>
                    {supportsModrinth && (
                      <button
                        onClick={() => searchDependencyInStore(dep)}
                        className="rounded bg-amber-500/25 px-1.5 py-0.5 text-[11px] font-semibold text-amber-200 hover:bg-amber-500/40"
                      >
                        {t.searchDepBtn}
                      </button>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {statusBanner.warnings && statusBanner.warnings.length > 0 && (
            <div className="rounded-xl border border-amber-500/30 bg-slate-950/40 p-2.5 text-xs text-amber-200">
              <div className="mb-1 font-semibold text-amber-300">{t.warningsTitle}</div>
              <ul className="list-disc space-y-0.5 pl-4">
                {statusBanner.warnings.map((w, idx) => (
                  <li key={idx}>{w}</li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}

      {tab === 'store' && supportsModrinth ? (
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
          ) : results.length === 0 ? (
            <div className="glass-card rounded-2xl p-10 text-center text-sm text-slate-400">{t.noResults}</div>
          ) : (
            <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
              {results.map((hit) => (
                <div key={hit.project_id} className="glass-card flex flex-col justify-between rounded-2xl p-4">
                  <div className="flex items-start gap-3">
                    {hit.icon_url ? (
                      <img src={hit.icon_url} alt="" className="h-12 w-12 rounded-xl bg-slate-800 object-cover" />
                    ) : (
                      <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-white/5 text-slate-400">
                        <Package className="h-6 w-6" />
                      </div>
                    )}
                    <div className="min-w-0 flex-1">
                      <h3 className="truncate font-semibold text-white">{hit.title}</h3>
                      <div className="text-[11px] text-slate-400">
                        {hit.author} • {hit.downloads.toLocaleString()} {t.downloadsLabel}
                      </div>
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
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className={`font-medium ${item.enabled ? 'text-white' : 'text-slate-500 line-through'}`}>{item.name}</span>
                      <span className="rounded-md bg-white/5 px-2 py-0.5 text-[10px] uppercase text-cyan-300">{item.kind}</span>
                      {item.displayName && item.displayName !== item.name && (
                        <span className="text-xs text-slate-400">
                          ({item.displayName}
                          {item.version ? ` v${item.version}` : ''})
                        </span>
                      )}
                    </div>
                    <div className="text-xs text-slate-500">
                      {item.path} {item.sizeBytes > 0 ? `• ${formatBytes(item.sizeBytes)}` : ''}
                    </div>

                    {item.missingDeps && item.missingDeps.length > 0 && (
                      <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
                        <span className="text-[11px] font-semibold text-rose-300">{t.missingDepBadge}:</span>
                        {item.missingDeps.map((dep) => (
                          <span
                            key={dep}
                            className="inline-flex items-center gap-1 rounded-md border border-rose-500/35 bg-rose-500/15 px-2 py-0.5 text-[11px] text-rose-200"
                          >
                            <span className="font-mono">{dep}</span>
                            {supportsModrinth && (
                              <button
                                onClick={() => searchDependencyInStore(dep)}
                                className="ml-1 rounded bg-rose-500/30 px-1.5 py-0.5 text-[10px] font-semibold text-white hover:bg-rose-500/50"
                              >
                                {t.searchDepBtn}
                              </button>
                            )}
                          </span>
                        ))}
                      </div>
                    )}

                    {item.warnings && item.warnings.length > 0 && (
                      <div className="mt-1.5 space-y-0.5">
                        {item.warnings.map((w, idx) => (
                          <div key={idx} className="flex items-start gap-1.5 text-xs text-amber-300">
                            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                            <span>{w}</span>
                          </div>
                        ))}
                      </div>
                    )}
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
