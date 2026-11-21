package models

import "time"

type EngineType string

const (
	EnginePaper        EngineType = "paper"
	EnginePurpur       EngineType = "purpur"
	EngineFabric       EngineType = "fabric"
	EngineNeoForge     EngineType = "neoforge"
	EngineVanilla      EngineType = "vanilla"
	EngineBDS          EngineType = "bds"
	EnginePowerNukkitX EngineType = "powernukkitx"
)

type ServerStatus string

const (
	StatusOffline    ServerStatus = "offline"
	StatusInstalling ServerStatus = "installing"
	StatusStarting   ServerStatus = "starting"
	StatusOnline     ServerStatus = "online"
	StatusStopping   ServerStatus = "stopping"
	StatusError      ServerStatus = "error"
)

type JVMPreset string

const (
	JVMStandard JVMPreset = "standard"
	JVMAikar    JVMPreset = "aikar"
	JVMZGC      JVMPreset = "zgc"
)

type CrossPlayConfig struct {
	Enabled     bool `json:"enabled"`
	BedrockPort int  `json:"bedrockPort"`
	Floodgate   bool `json:"floodgate"`
	ViaVersion  bool `json:"viaVersion"`
}

type ServerInstance struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Engine       EngineType      `json:"engine"`
	MCVersion    string          `json:"mcVersion"`
	CrossPlay    CrossPlayConfig `json:"crossPlay"`
	JavaPort     int             `json:"javaPort"`
	BedrockPort  int             `json:"bedrockPort"`
	MemoryMB     int             `json:"memoryMB"`
	JVMPreset    JVMPreset       `json:"jvmPreset"`
	JavaPath     string          `json:"javaPath,omitempty"`
	AutoRestart  bool            `json:"autoRestart"`
	OfflineMode  bool            `json:"offlineMode"`
	EULAAccepted bool            `json:"eulaAccepted"`
	CreatedAt    time.Time       `json:"createdAt"`
	Path         string          `json:"path"`
	Status       ServerStatus    `json:"status"`
	ActiveWorld  string          `json:"activeWorld"`
	LastError    string          `json:"lastError,omitempty"`
}

type AppConfig struct {
	DataDir         string `json:"dataDir"`
	APIPort         int    `json:"apiPort"`
	BindAddress     string `json:"bindAddress"`
	RemoteEnabled   bool   `json:"remoteEnabled"`
	PairingToken    string `json:"pairingToken"`
	Language        string `json:"language"`
	JavaPath        string `json:"javaPath,omitempty"`
	PlayitAutoStart bool   `json:"playitAutoStart"`
}

type ConsoleLine struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Text      string    `json:"text"`
}

type PlayerInfo struct {
	Name      string    `json:"name"`
	Platform  string    `json:"platform"` // "java" or "bedrock"
	JoinedAt  time.Time `json:"joinedAt"`
}

type ServerMetrics struct {
	ServerID    string       `json:"serverId"`
	Status      ServerStatus `json:"status"`
	CPUPercent  float64      `json:"cpuPercent"`
	MemoryUsedMB float64     `json:"memoryUsedMB"`
	MemoryMaxMB int          `json:"memoryMaxMB"`
	UptimeSec   int64        `json:"uptimeSec"`
	TPS         float64      `json:"tps"`
	PlayerCount int          `json:"playerCount"`
	MaxPlayers  int          `json:"maxPlayers"`
	Players     []PlayerInfo `json:"players"`
	PID         int32        `json:"pid"`
}

type SystemOverview struct {
	OS            string  `json:"os"`
	Arch          string  `json:"arch"`
	PlatformMode  string  `json:"platformMode"` // "desktop", "headless", "android"
	CPUModel      string  `json:"cpuModel"`
	CPUCores      int     `json:"cpuCores"`
	CPUPercent    float64 `json:"cpuPercent"`
	RAMTotalMB    uint64  `json:"ramTotalMB"`
	RAMUsedMB     uint64  `json:"ramUsedMB"`
	RAMFreeMB     uint64  `json:"ramFreeMB"`
	DiskTotalGB   float64 `json:"diskTotalGB"`
	DiskFreeGB    float64 `json:"diskFreeGB"`
	JavaDetected  bool    `json:"javaDetected"`
	JavaVersion   string  `json:"javaVersion"`
	JavaPath      string  `json:"javaPath"`
	LocalIP       string  `json:"localIP"`
}

type EngineDescriptor struct {
	ID                 EngineType `json:"id"`
	Name               string     `json:"name"`
	Edition            string     `json:"edition"` // "java", "bedrock", "hybrid"
	SupportsPlugins    bool       `json:"supportsPlugins"`
	SupportsMods       bool       `json:"supportsMods"`
	SupportsCrossPlay  bool       `json:"supportsCrossPlay"`
	SupportedOnAndroid bool       `json:"supportedOnAndroid"`
	DescriptionTR      string     `json:"descriptionTr"`
	DescriptionEN      string     `json:"descriptionEn"`
}

type ModrinthHit struct {
	ProjectID    string   `json:"project_id"`
	Slug         string   `json:"slug"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Categories   []string `json:"categories"`
	ProjectType  string   `json:"project_type"`
	Downloads    int      `json:"downloads"`
	IconURL      string   `json:"icon_url"`
	Author       string   `json:"author"`
	Versions     []string `json:"versions"`
}

type ModrinthSearchResult struct {
	Hits      []ModrinthHit `json:"hits"`
	Offset    int           `json:"offset"`
	Limit     int           `json:"limit"`
	TotalHits int           `json:"total_hits"`
}

type ModrinthDependency struct {
	VersionID      string `json:"version_id"`
	ProjectID      string `json:"project_id"`
	DependencyType string `json:"dependency_type"` // "required", "optional", "incompatible", "embedded"
}

type ModrinthFile struct {
	Hashes   map[string]string `json:"hashes"`
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Size     int64             `json:"size"`
}

type ModrinthVersion struct {
	ID            string               `json:"id"`
	ProjectID     string               `json:"project_id"`
	Name          string               `json:"name"`
	VersionNumber string               `json:"version_number"`
	GameVersions  []string             `json:"game_versions"`
	Loaders       []string             `json:"loaders"`
	Files         []ModrinthFile       `json:"files"`
	Dependencies  []ModrinthDependency `json:"dependencies"`
}

type InstalledAddon struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind"` // "mod", "plugin", "datapack", "bedrock-resource", "bedrock-behavior", "geyser-pack"
	Enabled   bool      `json:"enabled"`
	SizeBytes int64     `json:"sizeBytes"`
	ModTime   time.Time `json:"modTime"`
	Path      string    `json:"path"`
	UUID      string    `json:"uuid,omitempty"`
}

type PackInfo struct {
	Filename  string    `json:"filename"`
	Kind      string    `json:"kind"` // "java-resource", "bedrock-pack", "shader-client"
	SHA1      string    `json:"sha1"`
	SizeBytes int64     `json:"sizeBytes"`
	URLPath   string    `json:"urlPath"`
	Applied   bool      `json:"applied"`
	ModTime   time.Time `json:"modTime"`
}

type WorldInfo struct {
	Name      string    `json:"name"`
	Edition   string    `json:"edition"` // "java", "bedrock"
	Active    bool      `json:"active"`
	SizeBytes int64     `json:"sizeBytes"`
	HasNether bool      `json:"hasNether"`
	HasEnd    bool      `json:"hasEnd"`
	ModTime   time.Time `json:"modTime"`
}

type BackupInfo struct {
	Filename  string    `json:"filename"`
	WorldName string    `json:"worldName"`
	SizeBytes int64     `json:"sizeBytes"`
	CreatedAt time.Time `json:"createdAt"`
}

type NetworkDiagnostics struct {
	LocalIP          string `json:"localIP"`
	PublicIP         string `json:"publicIP"`
	RouterExternalIP string `json:"routerExternalIP"`
	UPnPAvailable    bool   `json:"upnpAvailable"`
	BehindCGNAT      bool   `json:"behindCGNAT"`
	Status           string `json:"status"` // "direct_ok", "cgnat_detected", "unknown"
	Recommendation   string `json:"recommendation"`
	E4mcRegion       string `json:"e4mcRegion,omitempty"`
	E4mcPingMs       int64  `json:"e4mcPingMs,omitempty"`
	PlayitPingMs     int64  `json:"playitPingMs,omitempty"`
	BorePingMs       int64  `json:"borePingMs,omitempty"`
}

type TunnelState struct {
	Installed      bool     `json:"installed"`
	Running        bool     `json:"running"`
	BinaryPath     string   `json:"binaryPath"`
	ClaimURL       string   `json:"claimUrl"`
	JavaAddress    string   `json:"javaAddress"`
	BedrockAddress string   `json:"bedrockAddress"`
	Addresses      []string `json:"addresses"`
	Logs           []string `json:"logs"`
	LastError      string   `json:"lastError,omitempty"`
}

type BoreState struct {
	Running    bool   `json:"running"`
	ServerHost string `json:"serverHost"`
	LocalPort  int    `json:"localPort"`
	RemotePort int    `json:"remotePort"`
	Address    string `json:"address"`
	LatencyMs  int64  `json:"latencyMs,omitempty"`
	LastError  string `json:"lastError,omitempty"`
}

type E4mcState struct {
	Running   bool   `json:"running"`
	Region    string `json:"region"`
	RelayHost string `json:"relayHost"`
	LocalPort int    `json:"localPort"`
	Domain    string `json:"domain"`
	LatencyMs int64  `json:"latencyMs"`
	Notice    string `json:"notice,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

type RelayState struct {
	Running          bool   `json:"running"`
	RelayAddr        string `json:"relayAddr"`
	LocalJavaPort    int    `json:"localJavaPort"`
	LocalBedrockPort int    `json:"localBedrockPort"`
	JavaAddress      string `json:"javaAddress"`
	BedrockAddress   string `json:"bedrockAddress"`
	LatencyMs        int64  `json:"latencyMs,omitempty"`
	LastError        string `json:"lastError,omitempty"`
}

type WSMessage struct {
	Type string `json:"type"` // "console", "history", "status", "metrics", "install_progress", "error"
	Data any    `json:"data"`
}
