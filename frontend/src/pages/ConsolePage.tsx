import React, { useEffect, useRef, useState } from 'react';
import { ArrowDown, Download, Search, Send, Terminal, Trash2 } from 'lucide-react';
import { buildWsUrl } from '../lib/api';
import { ConsoleLine, ServerInstance } from '../types';

interface Props {
  server: ServerInstance | null;
}

const QUICK_COMMANDS = ['list', 'tps', 'save-all', 'say Sunucu acik!', 'time set day', 'weather clear'];

export const ConsolePage: React.FC<Props> = ({ server }) => {
  const [lines, setLines] = useState<ConsoleLine[]>([]);
  const [input, setInput] = useState('');
  const [filter, setFilter] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);
  const [connected, setConnected] = useState(false);
  const [history, setHistory] = useState<string[]>([]);
  const [histIdx, setHistIdx] = useState(-1);

  const wsRef = useRef<WebSocket | null>(null);
  const endRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!server) {
      setLines([]);
      return;
    }
    const url = buildWsUrl(`/api/ws/console/${server.id}`);
    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data);
        if (msg.type === 'history' && Array.isArray(msg.data)) {
          setLines(msg.data);
        } else if (msg.type === 'console' && msg.data) {
          setLines((prev) => [...prev.slice(-999), msg.data]);
        } else if (msg.type === 'install_progress' && typeof msg.data === 'string') {
          setLines((prev) => [
            ...prev.slice(-999),
            { timestamp: new Date().toISOString(), level: 'INFO', text: `[Kurulum] ${msg.data}` },
          ]);
        }
      } catch {
        // ignore malformed packets
      }
    };

    return () => {
      ws.close();
      wsRef.current = null;
    };
  }, [server?.id]);

  useEffect(() => {
    if (autoScroll) {
      endRef.current?.scrollIntoView({ behavior: 'auto' });
    }
  }, [lines, autoScroll]);

  const sendCmd = (cmdText: string) => {
    const trimmed = cmdText.trim();
    if (!trimmed || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ type: 'command', command: trimmed }));
    setHistory((prev) => [trimmed, ...prev.slice(0, 49)]);
    setHistIdx(-1);
    setInput('');
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      sendCmd(input);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      const next = Math.min(histIdx + 1, history.length - 1);
      if (history[next] !== undefined) {
        setHistIdx(next);
        setInput(history[next]);
      }
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      const next = histIdx - 1;
      if (next < 0) {
        setHistIdx(-1);
        setInput('');
      } else if (history[next] !== undefined) {
        setHistIdx(next);
        setInput(history[next]);
      }
    }
  };

  const downloadLog = () => {
    const content = lines.map((l) => `[${new Date(l.timestamp).toLocaleTimeString()}] [${l.level}] ${l.text}`).join('\n');
    const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `${server?.name || 'server'}-console.log`;
    a.click();
  };

  if (!server) {
    return <div className="glass-card rounded-2xl p-8 text-center text-sm text-slate-400">Once bir sunucu secin veya olusturun.</div>;
  }

  const visibleLines = filter.trim()
    ? lines.filter((l) => l.text.toLowerCase().includes(filter.toLowerCase()))
    : lines;

  return (
    <div className="glass-panel flex h-[calc(100vh-11rem)] min-h-[480px] flex-col overflow-hidden rounded-3xl">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-white/10 px-5 py-3.5">
        <div className="flex items-center gap-2.5">
          <Terminal className="h-5 w-5 text-emerald-400" />
          <h2 className="font-semibold text-white">Canli Konsol — {server.name}</h2>
          <span className={`rounded-full px-2 py-0.5 text-[10px] font-semibold ${connected ? 'bg-emerald-500/20 text-emerald-300' : 'bg-slate-700 text-slate-400'}`}>
            {connected ? 'WS BAGLI' : 'BAGLANTI YOK'}
          </span>
        </div>

        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="absolute left-2.5 top-2 h-3.5 w-3.5 text-slate-500" />
            <input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Log filtrele..."
              className="w-40 rounded-xl border border-white/10 bg-slate-950/70 py-1.5 pl-8 pr-3 text-xs text-white outline-none focus:border-emerald-500"
            />
          </div>
          <button
            onClick={() => setAutoScroll(!autoScroll)}
            className={`rounded-xl border px-2.5 py-1.5 text-xs ${autoScroll ? 'border-emerald-500/40 bg-emerald-500/15 text-emerald-300' : 'border-white/10 text-slate-400'}`}
            title="Otomatik kaydir"
          >
            <ArrowDown className="h-3.5 w-3.5" />
          </button>
          <button onClick={downloadLog} className="rounded-xl border border-white/10 p-1.5 text-slate-400 hover:text-white" title="Log Indir">
            <Download className="h-3.5 w-3.5" />
          </button>
          <button onClick={() => setLines([])} className="rounded-xl border border-white/10 p-1.5 text-slate-400 hover:text-rose-300" title="Temizle">
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {/* Quick command chips */}
      <div className="flex flex-wrap gap-1.5 border-b border-white/5 bg-slate-950/40 px-5 py-2">
        {QUICK_COMMANDS.map((cmd) => (
          <button
            key={cmd}
            onClick={() => sendCmd(cmd)}
            className="rounded-lg border border-white/10 bg-white/5 px-2.5 py-1 font-mono text-[11px] text-slate-300 hover:border-emerald-500/40 hover:text-emerald-300"
          >
            /{cmd}
          </button>
        ))}
      </div>

      {/* Terminal Output */}
      <div className="flex-1 overflow-y-auto bg-[#050811] p-4 font-mono text-xs leading-relaxed">
        {visibleLines.length === 0 ? (
          <div className="py-12 text-center text-slate-600">Sunucu ciktisi burada canli akacak...</div>
        ) : (
          visibleLines.map((l, i) => {
            const color =
              l.level === 'ERROR'
                ? 'text-rose-400'
                : l.level === 'WARN'
                ? 'text-amber-300'
                : l.level === 'CMD'
                ? 'text-cyan-300 font-semibold'
                : 'text-slate-300';
            return (
              <div key={i} className="whitespace-pre-wrap break-all py-0.5">
                <span className="mr-2 text-slate-600">[{new Date(l.timestamp).toLocaleTimeString([], { hour12: false })}]</span>
                <span className={color}>{l.text}</span>
              </div>
            );
          })
        )}
        <div ref={endRef} />
      </div>

      {/* Command Input */}
      <div className="flex items-center gap-2 border-t border-white/10 bg-slate-950/80 px-4 py-3">
        <span className="font-mono text-sm font-bold text-emerald-400">&gt;</span>
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Sunucu komutu yazin (orn: op KullaniciAdi, say Merhaba)..."
          className="flex-1 bg-transparent font-mono text-sm text-white outline-none placeholder:text-slate-600"
        />
        <button
          onClick={() => sendCmd(input)}
          className="flex items-center gap-1.5 rounded-xl bg-emerald-500 px-4 py-2 text-xs font-semibold text-slate-950 hover:brightness-110"
        >
          <Send className="h-3.5 w-3.5" />
          Gonder
        </button>
      </div>
    </div>
  );
};
