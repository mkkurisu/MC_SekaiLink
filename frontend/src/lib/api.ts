export interface RemoteConnectionSettings {
  enabled: boolean;
  baseUrl: string;
  token: string;
}

const REMOTE_KEY = 'sekailink_remote_conn';

export function getRemoteConnection(): RemoteConnectionSettings {
  try {
    const raw = localStorage.getItem(REMOTE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      return {
        enabled: Boolean(parsed.enabled),
        baseUrl: String(parsed.baseUrl || '').replace(/\/+$/, ''),
        token: String(parsed.token || ''),
      };
    }
  } catch {
    // ignore storage errors
  }
  return { enabled: false, baseUrl: '', token: '' };
}

export function setRemoteConnection(cfg: RemoteConnectionSettings): void {
  localStorage.setItem(
    REMOTE_KEY,
    JSON.stringify({
      enabled: cfg.enabled,
      baseUrl: cfg.baseUrl.replace(/\/+$/, ''),
      token: cfg.token.trim(),
    }),
  );
}

export function getApiBaseUrl(): string {
  const remote = getRemoteConnection();
  if (remote.enabled && remote.baseUrl) {
    return remote.baseUrl;
  }
  const proto = window.location.protocol;
  const host = window.location.hostname;
  if ((proto === 'http:' || proto === 'https:') && host !== 'wails.localhost' && host !== 'wails') {
    return window.location.origin;
  }
  return 'http://127.0.0.1:25580';
}

export function buildWsUrl(path: string): string {
  const base = getApiBaseUrl();
  const wsBase = base.replace(/^https:\/\//i, 'wss://').replace(/^http:\/\//i, 'ws://');
  const remote = getRemoteConnection();
  if (remote.enabled && remote.token) {
    const sep = path.includes('?') ? '&' : '?';
    return `${wsBase}${path}${sep}token=${encodeURIComponent(remote.token)}`;
  }
  return `${wsBase}${path}`;
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const base = getApiBaseUrl();
  const remote = getRemoteConnection();
  const headers = new Headers(init?.headers || {});
  if (!(init?.body instanceof FormData) && !headers.has('Content-Type') && init?.body) {
    headers.set('Content-Type', 'application/json');
  }
  if (remote.enabled && remote.token) {
    headers.set('X-Sekai-Token', remote.token);
  }
  const res = await fetch(`${base}${path}`, { ...init, headers });
  if (!res.ok) {
    let errMsg = `HTTP ${res.status}`;
    try {
      const data = await res.json();
      if (data?.error) errMsg = data.error;
    } catch {
      // ignore
    }
    throw new Error(errMsg);
  }
  const text = await res.text();
  if (!text) return undefined as T;
  return JSON.parse(text) as T;
}

export function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatUptime(seconds: number): string {
  if (!seconds || seconds <= 0) return '0s';
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  if (h > 0) return `${h}h ${m}m ${s}s`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

export interface AndroidBridge {
  getPlatform(): string;
  isJreInstalled(): boolean;
  installArm64Jre(): void;
  importArm64JreFile?: () => void;
  stopLocalService(): void;
  requestBatteryUnrestricted(): void;
}

export function getAndroidBridge(): AndroidBridge | null {
  const w = window as unknown as { SekaiAndroid?: AndroidBridge };
  return w.SekaiAndroid || null;
}
