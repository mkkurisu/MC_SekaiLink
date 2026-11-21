package engine

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
	"github.com/MC_SekaiLink/mc-sekailink/internal/storage"
)

const maxConsoleBuffer = 1000

var (
	playerJoinRe    = regexp.MustCompile(`(?i):\s+([A-Za-z0-9_.\- ]{1,32})\s*(?:\[/[^\]]+\])?\s+logged in with entity id|Player connected:\s*([^,]+)`)
	playerLeaveRe   = regexp.MustCompile(`(?i):\s+([A-Za-z0-9_.\- ]{1,32})\s+lost connection|Player disconnected:\s*([^,]+)`)
	serverReadyRe   = regexp.MustCompile(`Done \([^)]+\)! For help, type "help"|Server started\.|Listening on /`)
	tpsLineRe       = regexp.MustCompile(`TPS from last 1m, 5m, 15m:\s*\*?([0-9]+(?:[.,][0-9]+)?)`)
)

type runningProc struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	startedAt time.Time
	stopCh    chan struct{}
	stopping  bool
}

type consoleState struct {
	mu      sync.RWMutex
	lines   []models.ConsoleLine
	players map[string]models.PlayerInfo
	tps     float64
	subs    map[chan models.WSMessage]struct{}
}

func newConsoleState() *consoleState {
	return &consoleState{
		lines:   make([]models.ConsoleLine, 0, 256),
		players: make(map[string]models.PlayerInfo),
		tps:     20.0,
		subs:    make(map[chan models.WSMessage]struct{}),
	}
}

type Manager struct {
	store *storage.Store

	mu      sync.Mutex
	procs   map[string]*runningProc
	consoles map[string]*consoleState
}

func NewManager(store *storage.Store) *Manager {
	return &Manager{
		store:    store,
		procs:    make(map[string]*runningProc),
		consoles: make(map[string]*consoleState),
	}
}

func (m *Manager) getConsole(id string) *consoleState {
	m.mu.Lock()
	defer m.mu.Unlock()
	cs, ok := m.consoles[id]
	if !ok {
		cs = newConsoleState()
		m.consoles[id] = cs
	}
	return cs
}

func (m *Manager) Subscribe(id string) (chan models.WSMessage, []models.ConsoleLine, func()) {
	cs := m.getConsole(id)
	ch := make(chan models.WSMessage, 128)
	cs.mu.Lock()
	cs.subs[ch] = struct{}{}
	history := make([]models.ConsoleLine, len(cs.lines))
	copy(history, cs.lines)
	cs.mu.Unlock()

	unsub := func() {
		cs.mu.Lock()
		delete(cs.subs, ch)
		cs.mu.Unlock()
	}
	return ch, history, unsub
}

func (m *Manager) broadcast(id string, msg models.WSMessage) {
	cs := m.getConsole(id)
	cs.mu.RLock()
	for ch := range cs.subs {
		select {
		case ch <- msg:
		default:
		}
	}
	cs.mu.RUnlock()
}

func (m *Manager) appendConsole(id, level, text string) {
	line := models.ConsoleLine{
		Timestamp: time.Now(),
		Level:     level,
		Text:      text,
	}
	cs := m.getConsole(id)
	cs.mu.Lock()
	if len(cs.lines) >= maxConsoleBuffer {
		copy(cs.lines, cs.lines[1:])
		cs.lines[len(cs.lines)-1] = line
	} else {
		cs.lines = append(cs.lines, line)
	}
	cs.mu.Unlock()
	m.broadcast(id, models.WSMessage{Type: "console", Data: line})
}

func (m *Manager) CreateServer(req models.ServerInstance) (models.ServerInstance, error) {
	if strings.TrimSpace(req.Name) == "" {
		return req, fmt.Errorf("sunucu adi bos olamaz")
	}
	if req.ID == "" {
		buf := make([]byte, 4)
		_, _ = rand.Read(buf)
		req.ID = "srv-" + hex.EncodeToString(buf)
	}
	if req.JavaPort <= 0 {
		req.JavaPort = 25565
	}
	if req.BedrockPort <= 0 {
		req.BedrockPort = 19132
	}
	if req.CrossPlay.BedrockPort <= 0 {
		req.CrossPlay.BedrockPort = req.BedrockPort
	}
	if req.MemoryMB <= 0 {
		req.MemoryMB = 2048
	}
	if req.JVMPreset == "" {
		req.JVMPreset = models.JVMAikar
	}
	if req.ActiveWorld == "" {
		if req.Engine == models.EngineBDS {
			req.ActiveWorld = "Bedrock level"
		} else {
			req.ActiveWorld = "world"
		}
	}
	req.Path = filepath.Join(m.store.ServersDir(), req.ID)
	req.CreatedAt = time.Now()
	req.Status = models.StatusOffline

	if err := os.MkdirAll(req.Path, 0o755); err != nil {
		return req, err
	}
	if err := m.store.SaveInstance(req); err != nil {
		return req, err
	}
	return req, nil
}

func (m *Manager) InstallServer(ctx context.Context, id string) error {
	inst, err := m.store.GetInstance(id)
	if err != nil {
		return err
	}
	if m.IsRunning(id) {
		return fmt.Errorf("calisan sunucu uzerinde kurulum yapilamaz, once durdurun")
	}

	inst.Status = models.StatusInstalling
	inst.LastError = ""
	_ = m.store.SaveInstance(inst)
	m.broadcast(id, models.WSMessage{Type: "status", Data: inst.Status})

	cfg := m.store.GetConfig()
	javaInfo := DetectJava(firstNonEmpty(inst.JavaPath, cfg.JavaPath), m.store.DataDir())

	progress := func(msg string) {
		m.appendConsole(id, "INFO", "[Kurulum] "+msg)
		m.broadcast(id, models.WSMessage{Type: "install_progress", Data: msg})
	}

	if err := InstallEngineArtifacts(ctx, inst, javaInfo.Path, progress); err != nil {
		inst.Status = models.StatusError
		inst.LastError = err.Error()
		_ = m.store.SaveInstance(inst)
		m.appendConsole(id, "ERROR", "[Kurulum Hatasi] "+err.Error())
		m.broadcast(id, models.WSMessage{Type: "status", Data: inst.Status})
		return err
	}

	inst.Status = models.StatusOffline
	inst.LastError = ""
	_ = m.store.SaveInstance(inst)
	m.broadcast(id, models.WSMessage{Type: "status", Data: inst.Status})
	return nil
}

func (m *Manager) IsRunning(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.procs[id]
	return ok
}

func (m *Manager) Start(id string) error {
	inst, err := m.store.GetInstance(id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if _, running := m.procs[id]; running {
		m.mu.Unlock()
		return fmt.Errorf("sunucu zaten calisiyor")
	}
	m.mu.Unlock()

	if inst.Engine != models.EngineBDS && inst.Engine != models.EnginePowerNukkitX && !inst.EULAAccepted {
		return fmt.Errorf("Minecraft EULA onaylanmadan sunucu baslatilamaz")
	}

	// Ensure eula.txt and offline/online authentication settings stay synced
	if inst.Engine != models.EngineBDS && inst.Engine != models.EnginePowerNukkitX && inst.EULAAccepted {
		_ = os.WriteFile(filepath.Join(inst.Path, "eula.txt"), []byte("# MC SekaiLink\neula=true\n"), 0o644)
	}
	_ = SyncOfflineMode(inst)

	cmd, err := m.buildStartCommand(inst)
	if err != nil {
		return err
	}
	cmd.Dir = inst.Path
	configureCmdWindow(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("sunucu sureci baslatilamadi: %w", err)
	}

	proc := &runningProc{
		cmd:       cmd,
		stdin:     stdin,
		startedAt: time.Now(),
		stopCh:    make(chan struct{}),
	}

	m.mu.Lock()
	m.procs[id] = proc
	m.mu.Unlock()

	cs := m.getConsole(id)
	cs.mu.Lock()
	cs.players = make(map[string]models.PlayerInfo)
	cs.tps = 20.0
	cs.mu.Unlock()

	inst.Status = models.StatusStarting
	inst.LastError = ""
	_ = m.store.SaveInstance(inst)
	m.broadcast(id, models.WSMessage{Type: "status", Data: inst.Status})
	m.appendConsole(id, "INFO", fmt.Sprintf("[MC SekaiLink] Sunucu baslatiliyor (%s %s)...", inst.Engine, inst.MCVersion))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		m.readStream(id, stdout, "INFO")
	}()
	go func() {
		defer wg.Done()
		m.readStream(id, stderr, "WARN")
	}()

	go func() {
		wg.Wait()
		waitErr := cmd.Wait()
		close(proc.stopCh)

		m.mu.Lock()
		delete(m.procs, id)
		wasStopping := proc.stopping
		m.mu.Unlock()

		cs.mu.Lock()
		cs.players = make(map[string]models.PlayerInfo)
		cs.mu.Unlock()

		latest, err := m.store.GetInstance(id)
		if err != nil {
			return
		}
		if waitErr != nil && !wasStopping {
			latest.Status = models.StatusError
			errReason := extractCrashSummary(cs)
			if errReason != "" {
				latest.LastError = fmt.Sprintf("%s (%v)", errReason, waitErr)
			} else {
				latest.LastError = waitErr.Error()
			}
			m.appendConsole(id, "ERROR", fmt.Sprintf("[MC SekaiLink] Sunucu beklenmedik sekilde kapandi: %s", latest.LastError))
		} else {
			latest.Status = models.StatusOffline
			m.appendConsole(id, "INFO", "[MC SekaiLink] Sunucu durduruldu.")
		}
		_ = m.store.SaveInstance(latest)
		m.broadcast(id, models.WSMessage{Type: "status", Data: latest.Status})

		if latest.AutoRestart && !wasStopping && waitErr != nil {
			m.appendConsole(id, "INFO", "[MC SekaiLink] Auto-Restart aktif, 3 saniye icinde yeniden baslatiliyor...")
			time.Sleep(3 * time.Second)
			_ = m.Start(id)
		}
	}()

	return nil
}

func (m *Manager) buildStartCommand(inst models.ServerInstance) (*exec.Cmd, error) {
	if inst.Engine == models.EngineBDS {
		exeName := "bedrock_server"
		if runtime.GOOS == "windows" {
			exeName = "bedrock_server.exe"
		}
		bin := filepath.Join(inst.Path, exeName)
		if _, err := os.Stat(bin); err != nil {
			return nil, fmt.Errorf("bedrock_server bulunamadi, once kurulumu calistirin")
		}
		cmd := exec.Command(bin)
		if runtime.GOOS == "linux" {
			cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+inst.Path)
		}
		return cmd, nil
	}

	cfg := m.store.GetConfig()
	javaInfo, reqMajor, sufficient := DetectJavaForMCVersion(inst.MCVersion, firstNonEmpty(inst.JavaPath, cfg.JavaPath), m.store.DataDir())
	if !javaInfo.Detected {
		return nil, fmt.Errorf("Java bulunamadi! Ayarlardan 'Otomatik Java 25 Kur' butonuna tiklayin")
	}
	if !sufficient {
		return nil, fmt.Errorf("Minecraft %s icin Java %d+ gerekli (bulunan: Java %d). Ayarlardan 'Otomatik Java 25 Kur' butonuna tiklayin", inst.MCVersion, reqMajor, javaInfo.Major)
	}

	args := buildJVMArgs(inst.MemoryMB, inst.JVMPreset, javaInfo.Major)

	if inst.Engine == models.EngineNeoForge {
		argsFile := findNeoForgeArgsFile(inst.Path)
		if argsFile != "" {
			args = append(args, "@"+argsFile, "nogui")
			cmd := exec.Command(javaInfo.Path, args...)
			if env := javaEnvForBinary(javaInfo.Path); len(env) > 0 {
				cmd.Env = env
			}
			return cmd, nil
		}
	}

	jarPath := filepath.Join(inst.Path, "server.jar")
	if _, err := os.Stat(jarPath); err != nil {
		return nil, fmt.Errorf("server.jar bulunamadi, once motor kurulumunu calistirin")
	}
	args = append(args, "-jar", jarPath)
	if inst.Engine != models.EnginePowerNukkitX {
		args = append(args, "nogui")
	}
	cmd := exec.Command(javaInfo.Path, args...)
	if env := javaEnvForBinary(javaInfo.Path); len(env) > 0 {
		cmd.Env = env
	}
	return cmd, nil
}

func buildJVMArgs(memoryMB int, preset models.JVMPreset, javaMajor int) []string {
	if memoryMB < 512 {
		memoryMB = 512
	}
	xms := memoryMB / 2
	if memoryMB <= 3072 && xms > 1024 {
		xms = 1024
	}
	if xms < 512 {
		xms = 512
	}
	base := []string{
		fmt.Sprintf("-Xms%dM", xms),
		fmt.Sprintf("-Xmx%dM", memoryMB),
		"-Dfile.encoding=UTF-8",
	}
	switch preset {
	case models.JVMAikar:
		g1New := "30"
		g1MaxNew := "40"
		g1Region := "8M"
		if memoryMB <= 4096 {
			g1New = "25"
			g1MaxNew = "35"
			g1Region = "4M"
		} else if memoryMB >= 12288 {
			g1New = "40"
			g1MaxNew = "50"
			g1Region = "16M"
		}
		args := []string{
			"-XX:+UseG1GC",
			"-XX:+ParallelRefProcEnabled",
			"-XX:MaxGCPauseMillis=100",
			"-XX:+UnlockExperimentalVMOptions",
			"-XX:+DisableExplicitGC",
			"-XX:G1NewSizePercent=" + g1New,
			"-XX:G1MaxNewSizePercent=" + g1MaxNew,
			"-XX:G1HeapRegionSize=" + g1Region,
			"-XX:G1ReservePercent=20",
			"-XX:G1HeapWastePercent=5",
			"-XX:G1MixedGCCountTarget=4",
			"-XX:InitiatingHeapOccupancyPercent=15",
			"-XX:G1MixedGCLiveThresholdPercent=90",
			"-XX:G1RSetUpdatingPauseTimePercent=5",
			"-XX:SurvivorRatio=32",
			"-XX:+PerfDisableSharedMem",
			"-XX:MaxTenuringThreshold=1",
		}
		if memoryMB > 3072 {
			args = append(args, "-XX:+AlwaysPreTouch")
		}
		return append(base, args...)
	case models.JVMZGC:
		zgcArgs := []string{"-XX:+UseZGC"}
		if javaMajor > 0 && javaMajor < 24 {
			zgcArgs = append(zgcArgs, "-XX:+ZGenerational")
		}
		if memoryMB > 3072 {
			zgcArgs = append(zgcArgs, "-XX:+AlwaysPreTouch")
		}
		return append(base, zgcArgs...)
	default:
		return base
	}
}

func extractCrashSummary(cs *consoleState) string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	var fallback string
	for i := len(cs.lines) - 1; i >= 0; i-- {
		txt := strings.TrimSpace(cs.lines[i].Text)
		if txt == "" || strings.HasPrefix(txt, "[MC SekaiLink]") {
			continue
		}
		if fallback == "" {
			fallback = txt
		}
		if cs.lines[i].Level == "ERROR" || strings.Contains(strings.ToLower(txt), "error") || strings.Contains(strings.ToLower(txt), "requires") {
			if len(txt) > 220 {
				return txt[:220] + "..."
			}
			return txt
		}
	}
	if len(fallback) > 220 {
		return fallback[:220] + "..."
	}
	return fallback
}

func findNeoForgeArgsFile(serverDir string) string {
	osArgs := "unix_args.txt"
	if runtime.GOOS == "windows" {
		osArgs = "win_args.txt"
	}
	var match string
	_ = filepath.WalkDir(filepath.Join(serverDir, "libraries", "net", "neoforged", "neoforge"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Name() == osArgs {
			rel, relErr := filepath.Rel(serverDir, path)
			if relErr == nil {
				match = rel
				return filepath.SkipAll
			}
		}
		return nil
	})
	return match
}

func (m *Manager) readStream(id string, r io.Reader, defaultLevel string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		level := defaultLevel
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "ERROR") || strings.Contains(upper, "EXCEPTION") || strings.Contains(upper, "FATAL") {
			level = "ERROR"
		} else if strings.Contains(upper, "WARN") {
			level = "WARN"
		}
		m.appendConsole(id, level, line)
		m.processLineEvents(id, line)
	}
}

func (m *Manager) processLineEvents(id, line string) {
	if serverReadyRe.MatchString(line) {
		if inst, err := m.store.GetInstance(id); err == nil && inst.Status != models.StatusOnline {
			inst.Status = models.StatusOnline
			_ = m.store.SaveInstance(inst)
			m.broadcast(id, models.WSMessage{Type: "status", Data: inst.Status})
		}
	}

	if match := playerJoinRe.FindStringSubmatch(line); len(match) >= 3 {
		name := strings.TrimSpace(firstNonEmpty(match[1], match[2]))
		if name != "" {
			platform := "java"
			if strings.HasPrefix(name, ".") || strings.Contains(line, "Player connected:") {
				platform = "bedrock"
			}
			cs := m.getConsole(id)
			cs.mu.Lock()
			cs.players[name] = models.PlayerInfo{
				Name:     name,
				Platform: platform,
				JoinedAt: time.Now(),
			}
			cs.mu.Unlock()
		}
	}

	if match := playerLeaveRe.FindStringSubmatch(line); len(match) >= 3 {
		name := strings.TrimSpace(firstNonEmpty(match[1], match[2]))
		if name != "" {
			cs := m.getConsole(id)
			cs.mu.Lock()
			delete(cs.players, name)
			cs.mu.Unlock()
		}
	}

	if match := tpsLineRe.FindStringSubmatch(line); len(match) >= 2 {
		norm := strings.ReplaceAll(match[1], ",", ".")
		if tps, err := strconv.ParseFloat(norm, 64); err == nil {
			cs := m.getConsole(id)
			cs.mu.Lock()
			cs.tps = tps
			cs.mu.Unlock()
		}
	}
}

func (m *Manager) SendCommand(id, command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	m.mu.Lock()
	proc, ok := m.procs[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("sunucu calismiyor")
	}
	m.appendConsole(id, "CMD", "> "+command)
	_, err := io.WriteString(proc.stdin, command+"\n")
	return err
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	proc, ok := m.procs[id]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	proc.stopping = true
	m.mu.Unlock()

	if inst, err := m.store.GetInstance(id); err == nil {
		inst.Status = models.StatusStopping
		_ = m.store.SaveInstance(inst)
		m.broadcast(id, models.WSMessage{Type: "status", Data: inst.Status})
	}

	_, _ = io.WriteString(proc.stdin, "stop\n")

	select {
	case <-proc.stopCh:
		return nil
	case <-time.After(20 * time.Second):
		if proc.cmd.Process != nil {
			_ = proc.cmd.Process.Kill()
		}
		<-proc.stopCh
		return nil
	}
}

func (m *Manager) Restart(id string) error {
	if m.IsRunning(id) {
		if err := m.Stop(id); err != nil {
			return err
		}
	}
	return m.Start(id)
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.procs))
	for id := range m.procs {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(serverID string) {
			defer wg.Done()
			_ = m.Stop(serverID)
		}(id)
	}
	wg.Wait()
}

func (m *Manager) GetMetrics(id string) (models.ServerMetrics, error) {
	inst, err := m.store.GetInstance(id)
	if err != nil {
		return models.ServerMetrics{}, err
	}
	props, _ := ReadProperties(inst.Path)
	maxPlayers, _ := strconv.Atoi(props["max-players"])
	if maxPlayers <= 0 {
		maxPlayers = 20
	}

	metrics := models.ServerMetrics{
		ServerID:    id,
		Status:      inst.Status,
		MemoryMaxMB: inst.MemoryMB,
		MaxPlayers:  maxPlayers,
		TPS:         20.0,
		Players:     []models.PlayerInfo{},
	}

	cs := m.getConsole(id)
	cs.mu.RLock()
	metrics.TPS = cs.tps
	for _, p := range cs.players {
		metrics.Players = append(metrics.Players, p)
	}
	metrics.PlayerCount = len(metrics.Players)
	cs.mu.RUnlock()

	m.mu.Lock()
	proc, ok := m.procs[id]
	m.mu.Unlock()
	if ok && proc.cmd.Process != nil {
		metrics.PID = int32(proc.cmd.Process.Pid)
		metrics.UptimeSec = int64(time.Since(proc.startedAt).Seconds())
		cpuPct, memMB := ProbeProcessMetrics(metrics.PID)
		metrics.CPUPercent = cpuPct
		metrics.MemoryUsedMB = memMB
	}
	return metrics, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
