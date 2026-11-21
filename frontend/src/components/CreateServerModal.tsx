import React, { useEffect, useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Check, Cpu, Gamepad2, Globe2, Loader2, Layers, Sparkles, X } from 'lucide-react';
import { apiFetch } from '../lib/api';
import { Lang, translations } from '../lib/i18n';
import { EngineDescriptor, EngineType, JVMPreset, ServerInstance } from '../types';

interface Props {
  open: boolean;
  onClose: () => void;
  lang: Lang;
  isAndroid: boolean;
  onCreated: (inst: ServerInstance) => void;
}

type ModeChoice = 'crossplay' | 'java' | 'bedrock';

export const CreateServerModal: React.FC<Props> = ({ open, onClose, lang, isAndroid, onCreated }) => {
  const t = translations[lang].wizard;
  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [mode, setMode] = useState<ModeChoice>('crossplay');
  const [engines, setEngines] = useState<EngineDescriptor[]>([]);
  const [selectedEngine, setSelectedEngine] = useState<EngineType>('paper');
  const [versions, setVersions] = useState<string[]>([]);
  const [selectedVersion, setSelectedVersion] = useState<string>('');
  const [loadingVersions, setLoadingVersions] = useState(false);

  const [name, setName] = useState('SekaiLink SMP');
  const [memoryMB, setMemoryMB] = useState(2048);
  const [jvmPreset, setJvmPreset] = useState<JVMPreset>('aikar');
  const [javaPort, setJavaPort] = useState(25565);
  const [bedrockPort, setBedrockPort] = useState(19132);
  const [viaVersion, setViaVersion] = useState(true);
  const [offlineMode, setOfflineMode] = useState(true);
  const [eulaAccepted, setEulaAccepted] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!open) return;
    setStep(1);
    setError('');
    apiFetch<EngineDescriptor[]>('/api/catalog/engines')
      .then(setEngines)
      .catch((e) => setError(e.message));
  }, [open]);

  const filteredEngines = engines.filter((e) => {
    if (isAndroid && !e.supportedOnAndroid) return false;
    if (mode === 'crossplay') return e.supportsCrossPlay;
    if (mode === 'java') return e.edition === 'java';
    return e.edition === 'bedrock';
  });

  useEffect(() => {
    if (filteredEngines.length > 0 && !filteredEngines.some((e) => e.id === selectedEngine)) {
      setSelectedEngine(filteredEngines[0].id);
    }
  }, [mode, engines]);

  useEffect(() => {
    if (!open || step !== 2 || !selectedEngine) return;
    setError('');
    setLoadingVersions(true);
    setVersions([]);
    apiFetch<string[]>(`/api/catalog/versions/${selectedEngine}`)
      .then((list) => {
        setVersions(list);
        if (list.length > 0) setSelectedVersion(list[0]);
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoadingVersions(false));
  }, [open, step, selectedEngine]);

  const handleSubmit = async () => {
    if (!name.trim()) {
      setError('Sunucu adi gerekli');
      return;
    }
    if (selectedEngine !== 'bds' && selectedEngine !== 'powernukkitx' && !eulaAccepted) {
      setError('EULA onayi gerekli');
      return;
    }
    setSubmitting(true);
    setError('');
    try {
      const created = await apiFetch<ServerInstance>('/api/servers', {
        method: 'POST',
        body: JSON.stringify({
          name: name.trim(),
          engine: selectedEngine,
          mcVersion: selectedVersion || 'latest',
          crossPlay: {
            enabled: mode === 'crossplay',
            bedrockPort,
            floodgate: true,
            viaVersion: mode === 'crossplay' && viaVersion,
          },
          javaPort,
          bedrockPort,
          memoryMB,
          jvmPreset,
          offlineMode,
          eulaAccepted,
          autoRestart: false,
        }),
      });
      await apiFetch(`/api/servers/${created.id}/install`, { method: 'POST' });
      onCreated(created);
      onClose();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Hata olustu');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <AnimatePresence>
      {open && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4 backdrop-blur-md"
        >
          <motion.div
            initial={{ y: 20, opacity: 0, scale: 0.97 }}
            animate={{ y: 0, opacity: 1, scale: 1 }}
            exit={{ y: 20, opacity: 0, scale: 0.97 }}
            className="glass-panel flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-3xl"
          >
            <div className="flex items-center justify-between border-b border-white/10 px-6 py-4">
              <div>
                <h2 className="text-lg font-bold text-white">{t.title}</h2>
                <div className="mt-1 flex items-center gap-2 text-xs text-slate-400">
                  <span className={step === 1 ? 'text-emerald-400 font-semibold' : ''}>{t.step1}</span>
                  <span>•</span>
                  <span className={step === 2 ? 'text-emerald-400 font-semibold' : ''}>{t.step2}</span>
                  <span>•</span>
                  <span className={step === 3 ? 'text-emerald-400 font-semibold' : ''}>{t.step3}</span>
                </div>
              </div>
              <button onClick={onClose} className="rounded-xl p-2 text-slate-400 hover:bg-white/10 hover:text-white">
                <X className="h-5 w-5" />
              </button>
            </div>

            <div className="flex-1 overflow-y-auto p-6">
              {error && (
                <div className="mb-4 rounded-xl border border-rose-500/30 bg-rose-500/10 px-4 py-2.5 text-sm text-rose-300">
                  {error}
                </div>
              )}

              {step === 1 && (
                <div className="grid gap-3">
                  {[
                    {
                      id: 'crossplay' as ModeChoice,
                      icon: Globe2,
                      badge: 'Onerilen',
                      title: t.modeCrossplay,
                      desc: t.modeCrossplayDesc,
                      accent: 'from-emerald-500/20 to-cyan-500/10 border-emerald-500/40',
                    },
                    {
                      id: 'java' as ModeChoice,
                      icon: Layers,
                      title: t.modeJava,
                      desc: t.modeJavaDesc,
                      accent: 'from-indigo-500/20 to-violet-500/10 border-indigo-500/40',
                    },
                    {
                      id: 'bedrock' as ModeChoice,
                      icon: Gamepad2,
                      title: t.modeBedrock,
                      desc: t.modeBedrockDesc,
                      accent: 'from-cyan-500/20 to-sky-500/10 border-cyan-500/40',
                    },
                  ].map((item) => {
                    const Icon = item.icon;
                    const active = mode === item.id;
                    return (
                      <button
                        key={item.id}
                        onClick={() => setMode(item.id)}
                        className={`flex items-start gap-4 rounded-2xl border p-4 text-left transition-all ${
                          active
                            ? `bg-gradient-to-br ${item.accent} shadow-lg`
                            : 'border-white/10 bg-slate-900/40 hover:border-white/20 hover:bg-slate-800/50'
                        }`}
                      >
                        <div className={`rounded-xl p-3 ${active ? 'bg-emerald-500/20 text-emerald-300' : 'bg-white/5 text-slate-400'}`}>
                          <Icon className="h-6 w-6" />
                        </div>
                        <div className="flex-1">
                          <div className="flex items-center gap-2">
                            <span className="font-semibold text-white">{item.title}</span>
                            {item.badge && (
                              <span className="rounded-full bg-emerald-500/20 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-emerald-300">
                                {item.badge}
                              </span>
                            )}
                          </div>
                          <p className="mt-1 text-xs leading-relaxed text-slate-400">{item.desc}</p>
                        </div>
                        {active && <Check className="mt-1 h-5 w-5 text-emerald-400" />}
                      </button>
                    );
                  })}
                </div>
              )}

              {step === 2 && (
                <div className="space-y-4">
                  <div className="grid gap-2.5 sm:grid-cols-2">
                    {filteredEngines.map((eng) => {
                      const active = eng.id === selectedEngine;
                      return (
                        <button
                          key={eng.id}
                          onClick={() => setSelectedEngine(eng.id)}
                          className={`rounded-2xl border p-3.5 text-left transition-all ${
                            active
                              ? 'border-emerald-500/50 bg-emerald-500/10'
                              : 'border-white/10 bg-slate-900/40 hover:border-white/20'
                          }`}
                        >
                          <div className="flex items-center justify-between">
                            <span className="font-semibold text-white">{eng.name}</span>
                            {active && <Check className="h-4 w-4 text-emerald-400" />}
                          </div>
                          <p className="mt-1.5 text-xs text-slate-400">
                            {lang === 'tr' ? eng.descriptionTr : eng.descriptionEn}
                          </p>
                        </button>
                      );
                    })}
                  </div>

                  <div>
                    <label className="mb-1.5 block text-xs font-medium text-slate-300">Minecraft / Motor Surumu</label>
                    {loadingVersions ? (
                      <div className="flex items-center gap-2 rounded-xl border border-white/10 bg-slate-950/60 px-4 py-3 text-sm text-slate-400">
                        <Loader2 className="h-4 w-4 animate-spin text-emerald-400" />
                        Surum listesi aliniyor...
                      </div>
                    ) : (
                      <select
                        value={selectedVersion}
                        onChange={(e) => setSelectedVersion(e.target.value)}
                        className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-4 py-2.5 text-sm text-white outline-none focus:border-emerald-500"
                      >
                        {versions.map((v) => (
                          <option key={v} value={v}>
                            {v}
                          </option>
                        ))}
                      </select>
                    )}
                  </div>
                </div>
              )}

              {step === 3 && (
                <div className="space-y-4">
                  <div>
                    <label className="mb-1.5 block text-xs font-medium text-slate-300">{t.serverName}</label>
                    <input
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-4 py-2.5 text-sm text-white outline-none focus:border-emerald-500"
                      placeholder="SekaiLink SMP"
                    />
                  </div>

                  {selectedEngine !== 'bds' && (
                    <div>
                      <div className="mb-1.5 flex items-center justify-between text-xs">
                        <span className="font-medium text-slate-300">{t.memory}</span>
                        <span className="font-mono text-emerald-400">{memoryMB} MB ({(memoryMB / 1024).toFixed(1)} GB)</span>
                      </div>
                      <input
                        type="range"
                        min={512}
                        max={16384}
                        step={512}
                        value={memoryMB}
                        onChange={(e) => setMemoryMB(Number(e.target.value))}
                        className="w-full accent-emerald-500"
                      />
                    </div>
                  )}

                  <div className="grid gap-3 sm:grid-cols-3">
                    {selectedEngine !== 'bds' && (
                      <div>
                        <label className="mb-1 block text-xs text-slate-400">{t.jvmPreset}</label>
                        <select
                          value={jvmPreset}
                          onChange={(e) => setJvmPreset(e.target.value as JVMPreset)}
                          className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 text-sm text-white outline-none"
                        >
                          <option value="aikar">Aikar&apos;s Flags (Onerilen)</option>
                          <option value="zgc">Generational ZGC</option>
                          <option value="standard">Standard</option>
                        </select>
                      </div>
                    )}
                    {mode !== 'bedrock' && (
                      <div>
                        <label className="mb-1 block text-xs text-slate-400">{t.javaPort}</label>
                        <input
                          type="number"
                          value={javaPort}
                          onChange={(e) => setJavaPort(Number(e.target.value))}
                          className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 text-sm text-white outline-none"
                        />
                      </div>
                    )}
                    {(mode === 'crossplay' || mode === 'bedrock') && (
                      <div>
                        <label className="mb-1 block text-xs text-slate-400">{t.bedrockPort}</label>
                        <input
                          type="number"
                          value={bedrockPort}
                          onChange={(e) => setBedrockPort(Number(e.target.value))}
                          className="w-full rounded-xl border border-white/10 bg-slate-950/80 px-3 py-2 text-sm text-white outline-none"
                        />
                      </div>
                    )}
                  </div>

                  {mode === 'crossplay' && (selectedEngine === 'paper' || selectedEngine === 'purpur') && (
                    <label className="flex items-center gap-3 rounded-xl border border-white/10 bg-slate-900/50 p-3 text-sm text-slate-200">
                      <input
                        type="checkbox"
                        checked={viaVersion}
                        onChange={(e) => setViaVersion(e.target.checked)}
                        className="h-4 w-4 accent-emerald-500"
                      />
                      <span>{t.viaVersion}</span>
                    </label>
                  )}

                  <div className="space-y-2 rounded-xl border border-amber-500/25 bg-amber-500/5 p-3">
                    <label className="flex items-start gap-3 text-sm text-slate-200">
                      <input
                        type="checkbox"
                        checked={offlineMode}
                        onChange={(e) => setOfflineMode(e.target.checked)}
                        className="mt-0.5 h-4 w-4 accent-amber-400"
                      />
                      <div>
                        <div className="font-medium text-white">Cevrimdisi / Premium Olmayan Giris (Offline Mode)</div>
                        <p className="mt-0.5 text-xs text-slate-400">
                          Orijinal hesabi olmayan (crackli/offline) Java ve Bedrock oyuncularinin girmesine izin verir (online-mode=false, enforce-secure-profile=false, Geyser offline).
                        </p>
                      </div>
                    </label>
                    {offlineMode && (
                      <p className="pl-7 text-[11px] text-amber-300/90">
                        Guvenlik ipucu: Herkese acik tunel kullanirken isim taklidini onlemek icin Whitelist veya Modrinth&apos;ten AuthMe / LoginSecurity kurmaniz onerilir.
                      </p>
                    )}
                  </div>

                  {selectedEngine !== 'bds' && selectedEngine !== 'powernukkitx' && (
                    <label className="flex items-center gap-3 rounded-xl border border-emerald-500/20 bg-emerald-500/5 p-3 text-sm text-slate-200">
                      <input
                        type="checkbox"
                        checked={eulaAccepted}
                        onChange={(e) => setEulaAccepted(e.target.checked)}
                        className="h-4 w-4 accent-emerald-500"
                      />
                      <span>
                        {t.eulaAccept}{' '}
                        <span className="text-xs text-emerald-400">(aka.ms/MinecraftEULA)</span>
                      </span>
                    </label>
                  )}
                </div>
              )}
            </div>

            <div className="flex items-center justify-between border-t border-white/10 bg-slate-950/40 px-6 py-4">
              <button
                onClick={() => (step > 1 ? setStep((step - 1) as 1 | 2) : onClose())}
                className="rounded-xl px-4 py-2 text-sm font-medium text-slate-300 hover:bg-white/5"
              >
                {step === 1 ? translations[lang].actions.cancel : 'Geri'}
              </button>
              {step < 3 ? (
                <button
                  onClick={() => setStep((step + 1) as 2 | 3)}
                  className="flex items-center gap-2 rounded-xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-5 py-2.5 text-sm font-semibold text-slate-950 shadow-lg shadow-emerald-500/20 hover:brightness-110"
                >
                  Devam Et
                </button>
              ) : (
                <button
                  disabled={submitting}
                  onClick={handleSubmit}
                  className="flex items-center gap-2 rounded-xl bg-gradient-to-r from-emerald-500 to-cyan-500 px-5 py-2.5 text-sm font-semibold text-slate-950 shadow-lg shadow-emerald-500/20 hover:brightness-110 disabled:opacity-50"
                >
                  {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Sparkles className="h-4 w-4" />}
                  {t.createAndInstall}
                </button>
              )}
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
};
