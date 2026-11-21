export type EngineType =
  | 'paper'
  | 'purpur'
  | 'fabric'
  | 'neoforge'
  | 'vanilla'
  | 'bds'
  | 'powernukkitx';

export type ServerStatus =
  | 'offline'
  | 'installing'
  | 'starting'
  | 'online'
  | 'stopping'
  | 'error';

export type JVMPreset = 'standard' | 'aikar' | 'zgc';

export interface CrossPlayConfig {
  enabled: boolean;
  bedrockPort: number;
  floodgate: boolean;
  viaVersion: boolean;
}

export interface ServerInstance {
  id: string;
  name: string;
  engine: EngineType;
  mcVersion: string;
  crossPlay: CrossPlayConfig;
  javaPort: number;
  bedrockPort: number;
  memoryMB: number;
  jvmPreset: JVMPreset;
  javaPath?: string;
  autoRestart: boolean;
  offlineMode: boolean;
  eulaAccepted: boolean;
  createdAt: string;
  path: string;
  status: ServerStatus;
  activeWorld: string;
  lastError?: string;
}

export interface AppConfig {
  dataDir: string;
  apiPort: number;
  bindAddress: string;
  remoteEnabled: boolean;
  pairingToken: string;
  language: 'tr' | 'en';
  javaPath?: string;
  playitAutoStart: boolean;
}

export interface ConsoleLine {
  timestamp: string;
  level: string;
  text: string;
}

export interface PlayerInfo {
  name: string;
  platform: 'java' | 'bedrock' | string;
  joinedAt: string;
}

export interface ServerMetrics {
  serverId: string;
  status: ServerStatus;
  cpuPercent: number;
  memoryUsedMB: number;
  memoryMaxMB: number;
  uptimeSec: number;
  tps: number;
  playerCount: number;
  maxPlayers: number;
  players: PlayerInfo[];
  pid: number;
}

export interface SystemOverview {
  os: string;
  arch: string;
  platformMode: 'desktop' | 'headless' | 'android' | string;
  cpuModel: string;
  cpuCores: number;
  cpuPercent: number;
  ramTotalMB: number;
  ramUsedMB: number;
  ramFreeMB: number;
  diskTotalGB: number;
  diskFreeGB: number;
  javaDetected: boolean;
  javaVersion: string;
  javaPath: string;
  localIP: string;
}

export interface EngineDescriptor {
  id: EngineType;
  name: string;
  edition: 'java' | 'bedrock' | 'hybrid' | string;
  supportsPlugins: boolean;
  supportsMods: boolean;
  supportsCrossPlay: boolean;
  supportedOnAndroid: boolean;
  descriptionTr: string;
  descriptionEn: string;
}

export interface ModrinthHit {
  project_id: string;
  slug: string;
  title: string;
  description: string;
  categories: string[];
  project_type: string;
  downloads: number;
  icon_url: string;
  author: string;
  versions: string[];
}

export interface ModrinthSearchResult {
  hits: ModrinthHit[];
  offset: number;
  limit: number;
  total_hits: number;
}

export interface InstalledAddon {
  name: string;
  kind: string;
  enabled: boolean;
  sizeBytes: number;
  modTime: string;
  path: string;
  uuid?: string;
}

export interface PackInfo {
  filename: string;
  kind: string;
  sha1: string;
  sizeBytes: number;
  urlPath: string;
  applied: boolean;
  modTime: string;
}

export interface WorldInfo {
  name: string;
  edition: string;
  active: boolean;
  sizeBytes: number;
  hasNether: boolean;
  hasEnd: boolean;
  modTime: string;
}

export interface BackupInfo {
  filename: string;
  worldName: string;
  sizeBytes: number;
  createdAt: string;
}

export interface NetworkDiagnostics {
  localIP: string;
  publicIP: string;
  routerExternalIP: string;
  upnpAvailable: boolean;
  behindCGNAT: boolean;
  status: string;
  recommendation: string;
}

export interface TunnelState {
  installed: boolean;
  running: boolean;
  binaryPath: string;
  claimUrl: string;
  javaAddress: string;
  bedrockAddress: string;
  addresses: string[];
  logs: string[];
  lastError?: string;
}

export interface BoreState {
  running: boolean;
  serverHost: string;
  localPort: number;
  remotePort: number;
  address: string;
  lastError?: string;
}

export interface RelayState {
  running: boolean;
  relayAddr: string;
  localJavaPort: number;
  localBedrockPort: number;
  javaAddress: string;
  bedrockAddress: string;
  lastError?: string;
}
