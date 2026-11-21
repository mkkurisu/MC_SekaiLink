import React, { useState } from 'react';
import { QRCodeSVG } from 'qrcode.react';
import { Check, Copy, QrCode, X } from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';
import { copyTextSafe } from '../lib/api';
import { Lang, translations } from '../lib/i18n';

interface Props {
  open: boolean;
  onClose: () => void;
  title: string;
  subtitle?: string;
  value: string;
  lang?: Lang;
}

export const QrShareModal: React.FC<Props> = ({ open, onClose, title, subtitle, value, lang = 'tr' }) => {
  const [copied, setCopied] = useState(false);
  const t = translations[lang].qrModal;

  const handleCopy = async () => {
    await copyTextSafe(value);
    setCopied(true);
    setTimeout(() => setCopied(false), 1800);
  };

  return (
    <AnimatePresence>
      {open && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4 backdrop-blur-sm"
          onClick={onClose}
        >
          <motion.div
            initial={{ scale: 0.94, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            exit={{ scale: 0.94, opacity: 0 }}
            onClick={(e) => e.stopPropagation()}
            className="glass-panel w-full max-w-sm rounded-2xl p-6 text-center"
          >
            <div className="mb-4 flex items-center justify-between">
              <div className="flex items-center gap-2 text-emerald-400">
                <QrCode className="h-5 w-5" />
                <h3 className="font-semibold text-white">{title}</h3>
              </div>
              <button onClick={onClose} className="rounded-lg p-1.5 text-slate-400 hover:bg-white/10 hover:text-white">
                <X className="h-4 w-4" />
              </button>
            </div>
            {subtitle && <p className="mb-4 text-xs text-slate-400">{subtitle}</p>}
            <div className="mx-auto inline-block rounded-2xl bg-white p-4 shadow-xl">
              <QRCodeSVG value={value || 'MC SekaiLink'} size={196} level="M" />
            </div>
            <div className="mt-4 flex items-center justify-between gap-2 rounded-xl border border-white/10 bg-slate-950/70 px-3 py-2 text-left">
              <code className="truncate font-mono text-xs text-cyan-300">{value}</code>
              <button
                onClick={handleCopy}
                className="flex shrink-0 items-center gap-1 rounded-lg bg-emerald-500/20 px-2.5 py-1 text-xs font-medium text-emerald-300 hover:bg-emerald-500/30"
              >
                {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                {copied ? t.copied : t.copy}
              </button>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
};
