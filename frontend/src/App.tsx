import React, { useEffect, useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import {
  Blocks,
  Box,
  Globe,
  Globe2,
  Languages,
  LayoutDashboard,
  Link2,
  Plus,
  Settings,
  Sparkles,
  Terminal,
} from 'lucide-react';
import { apiFetch, getRemoteConnection } from './lib/api';
import { Lang, translations } from './lib/i18n';
import { AppConfig, BoreState, RelayState, ServerInstance, SystemOverview, TunnelState } from './types';
import { CreateServerModal } from './components/CreateServerModal';
import { DashboardPage } from './pages/DashboardPage';
import { ConsolePage } from './pages/ConsolePage';
import { AddonsPage } from './pages/AddonsPage';
import { PacksShadersPage } from './pages/PacksShadersPage';
import { WorldsPage } from './pages/WorldsPage';
import { NetworkPage } from './pages/NetworkPage';
import { SettingsPage } from './pages/SettingsPage';
import { RemoteConnectPage } from './pages/RemoteConnectPage';

export function App() {
  const [lang, setLang] = useState<Lang>('tr');
  const [tab, setTab] = useState('dashboard');
  const [servers, setServers] = useState<ServerInstance[]>([]);
  const [selectedId, setSelectedId] = useState<string>('');
  const [system, setSystem] = useState<SystemOverview | null>(null);
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [tunnel, setTunnel] = useState<TunnelState | null>(null);
  const [bore, setBore] = useState<BoreState | null>(null);
  const [relay, setRelay] = useState<RelayState | null>(null);
  const [createOpen, setCreateOpen] = useState(false);

  const refreshTunnels = () => {
    apiFetch<TunnelState>('/api/network/playit/status').then(setTunnel).catch(() => {});
    apiFetch<BoreState>('/api/network/bore/status').then(setBore).catch(() => {});
    apiFetch<RelayState>('/api/network/relay/status').then(setRelay).catch(() => {});
  };

  const loadAll = async () => {
    try {
      const [srvs, sys, cfg, tun, br, rl] = await Promise.all([
        apiFetch<ServerInstance[]>('/api/servers'),
        apiFetch<SystemOverview>('/api/system'),
        apiFetch<AppConfig>('/api/config'),
        apiFetch<TunnelState>('/api/network/playit/status'),
        apiFetch<BoreState>('/api/network/bore/status'),
        apiFetch<RelayState>('/api/network/relay/status'),
      ]);
      setServers(srvs || []);
      setSystem(sys);
      setConfig(cfg);
      setTunnel(tun);
      setBore(br);
      setRelay(rl);
      if (cfg?.language === 'en' || cfg?.language === 'tr') {
        setLang(cfg.language);
      }
      if (srvs && srvs.length > 0 && (!selectedId || !srvs.some((s) => s.id === selectedId))) {
        setSelectedId(srvs[0].id);
      } else if (!srvs || srvs.length === 0) {
        setSelectedId('');
      }
    } catch {
      // ignore initial connect errors when switching remote mode
    }
  };

  useEffect(() => {
    loadAll();
    const timer = setInterval(() => {
      apiFetch<ServerInstance[]>('/api/servers').then((s) => setServers(s || [])).catch(() => {});
      refreshTunnels();
    }, 3500);
    return () => clearInterval(timer);
  }, []);

  const t = translations[lang];
  const activeServer = servers.find((s) => s.id === selectedId) || null;
  const isAndroid = system?.platformMode === 'android';
  const isRemote = getRemoteConnection().enabled;

  const navItems = [
    { id: 'dashboard', label: t.nav.dashboard, icon: LayoutDashboard },
    { id: 'console', label: t.nav.console, icon: Terminal },
    { id: 'addons', label: t.nav.addons, icon: Blocks },
    { id: 'packs', label: t.nav.packs, icon: Sparkles },
    { id: 'worlds', label: t.nav.worlds, icon: Globe2 },
    { id: 'network', label: t.nav.network, icon: Globe },
    { id: 'settings', label: t.nav.settings, icon: Settings },
    { id: 'remote', label: t.nav.remote, icon: Link2 },
  ];

  const toggleLang = async () => {
    const next: Lang = lang === 'tr' ? 'en' : 'tr';
    setLang(next);
    try {
      await apiFetch('/api/config', { method: 'PUT', body: JSON.stringify({ language: next }) });
    } catch {
      // ignore
    }
  };

  return (
    <div className="portal-bg-glow flex h-screen w-full overflow-hidden text-slate-100">
      {/* Desktop Sidebar */}
      <aside className="hidden w-64 shrink-0 flex-col border-r border-white/10 bg-[#080d18]/90 backdrop-blur-xl md:flex">
        <div className="flex items-center gap-3 px-5 py-5">
          <div className="flex h-10 w-10 items-center justify-center rounded-2xl bg-gradient-to-br from-emerald-400 via-cyan-400 to-indigo-500 text-slate-950 shadow-lg shadow-emerald-500/20">
            <Box className="h-5 w-5 stroke-[2.5]" />
          </div>
          <div>
            <div className="portal-gradient-text text-base font-extrabold tracking-tight">{t.appName}</div>
            <div className="text-[10px] text-slate-400">{isRemote ? 'Remote Companion' : t.tagline}</div>
          </div>
        </div>

        {/* Server Switcher */}
        <div className="px-4 pb-3">
          <div className="flex items-center gap-1.5">
            <select
              value={selectedId}
              onChange={(e) => setSelectedId(e.target.value)}
              className="flex-1 truncate rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 text-xs font-medium text-white outline-none focus:border-emerald-500"
            >
              {servers.length === 0 ? (
                <option value="">Sunucu yok</option>
              ) : (
                servers.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.status === 'online' ? '● ' : '○ '}
                    {s.name}
                  </option>
                ))
              )}
            </select>
            <button
              onClick={() => setCreateOpen(true)}
              className="rounded-xl bg-emerald-500/20 p-2 text-emerald-300 hover:bg-emerald-500/30"
              title={t.actions.createServer}
            >
              <Plus className="h-4 w-4" />
            </button>
          </div>
        </div>

        <nav className="flex-1 space-y-1 overflow-y-auto px-3 py-2">
          {navItems.map((item) => {
            const Icon = item.icon;
            const active = tab === item.id;
            return (
              <button
                key={item.id}
                onClick={() => setTab(item.id)}
                className={`flex w-full items-center gap-3 rounded-xl px-3.5 py-2.5 text-sm font-medium transition-all ${
                  active
                    ? 'bg-gradient-to-r from-emerald-500/20 to-cyan-500/10 text-emerald-300 border border-emerald-500/30 shadow-sm'
                    : 'text-slate-400 hover:bg-white/5 hover:text-slate-200 border border-transparent'
                }`}
              >
                <Icon className="h-4 w-4 shrink-0" />
                <span>{item.label}</span>
              </button>
            );
          })}
        </nav>

        <div className="border-t border-white/10 p-4">
          <button
            onClick={toggleLang}
            className="flex w-full items-center justify-between rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-xs text-slate-300 hover:bg-white/10"
          >
            <span className="flex items-center gap-2">
              <Languages className="h-3.5 w-3.5 text-cyan-400" />
              Dil / Language
            </span>
            <span className="font-mono font-bold uppercase text-emerald-300">{lang}</span>
          </button>
        </div>
      </aside>

      {/* Main Content */}
      <div className="flex flex-1 flex-col overflow-hidden">
        {/* Mobile Top Bar */}
        <header className="flex items-center justify-between border-b border-white/10 bg-[#080d18]/90 px-4 py-3 md:hidden">
          <div className="flex items-center gap-2">
            <Box className="h-5 w-5 text-emerald-400" />
            <span className="portal-gradient-text font-bold">MC SekaiLink</span>
          </div>
          <div className="flex items-center gap-2">
            <select
              value={selectedId}
              onChange={(e) => setSelectedId(e.target.value)}
              className="max-w-[140px] rounded-lg border border-white/10 bg-slate-950 px-2 py-1 text-xs text-white"
            >
              {servers.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
            <button onClick={() => setCreateOpen(true)} className="rounded-lg bg-emerald-500 p-1.5 text-slate-950">
              <Plus className="h-4 w-4" />
            </button>
          </div>
        </header>

        <main className="flex-1 overflow-y-auto p-4 pb-24 md:p-6 md:pb-6">
          <AnimatePresence mode="wait">
            <motion.div
              key={tab}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -8 }}
              transition={{ duration: 0.16 }}
              className="mx-auto max-w-6xl"
            >
              {tab === 'dashboard' && (
                <DashboardPage
                  server={activeServer}
                  system={system}
                  tunnel={tunnel}
                  bore={bore}
                  relay={relay}
                  lang={lang}
                  onRefresh={loadAll}
                  onOpenCreate={() => setCreateOpen(true)}
                  onNavigate={setTab}
                />
              )}
              {tab === 'console' && <ConsolePage server={activeServer} />}
              {tab === 'addons' && <AddonsPage server={activeServer} />}
              {tab === 'packs' && <PacksShadersPage server={activeServer} />}
              {tab === 'worlds' && <WorldsPage server={activeServer} onRefresh={loadAll} />}
              {tab === 'network' && (
                <NetworkPage
                  server={activeServer}
                  system={system}
                  config={config}
                  tunnel={tunnel}
                  bore={bore}
                  relay={relay}
                  onRefreshTunnel={refreshTunnels}
                  onRefreshConfig={() => apiFetch<AppConfig>('/api/config').then(setConfig).catch(() => {})}
                />
              )}
              {tab === 'settings' && (
                <SettingsPage server={activeServer} system={system} onRefresh={loadAll} onDeleted={loadAll} />
              )}
              {tab === 'remote' && <RemoteConnectPage onReconnect={loadAll} />}
            </motion.div>
          </AnimatePresence>
        </main>

        {/* Mobile Bottom Bar */}
        <nav className="fixed bottom-0 left-0 right-0 z-40 flex items-center justify-around border-t border-white/10 bg-[#080d18]/95 px-1 py-2 backdrop-blur-xl md:hidden">
          {navItems.slice(0, 6).map((item) => {
            const Icon = item.icon;
            const active = tab === item.id;
            return (
              <button
                key={item.id}
                onClick={() => setTab(item.id)}
                className={`flex flex-col items-center gap-0.5 rounded-xl px-2 py-1 text-[10px] ${active ? 'text-emerald-400' : 'text-slate-400'}`}
              >
                <Icon className="h-4 w-4" />
                <span className="truncate max-w-[54px]">{item.label.split(' ')[0]}</span>
              </button>
            );
          })}
          <button
            onClick={() => setTab(tab === 'settings' ? 'remote' : 'settings')}
            className={`flex flex-col items-center gap-0.5 rounded-xl px-2 py-1 text-[10px] ${tab === 'settings' || tab === 'remote' ? 'text-emerald-400' : 'text-slate-400'}`}
          >
            <Settings className="h-4 w-4" />
            <span>Diger</span>
          </button>
        </nav>
      </div>

      <CreateServerModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        lang={lang}
        isAndroid={isAndroid}
        onCreated={(created) => {
          setSelectedId(created.id);
          setTab('console');
          loadAll();
        }}
      />
    </div>
  );
}

export default App;
