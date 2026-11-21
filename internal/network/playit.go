package network

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

var (
	claimURLRe   = regexp.MustCompile(`https://playit\.gg/claim/[A-Za-z0-9\-_]+`)
	tunnelAddrRe = regexp.MustCompile(`([a-zA-Z0-9.\-]+\.(?:joinmc\.link|playit\.gg|ply\.gg|at\.ply\.gg)(?::\d+)?)`)
)

type playitReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type PlayitManager struct {
	dataDir string
	binDir  string

	mu         sync.Mutex
	cliPath    string
	daemonPath string
	socketPath string
	cmd        *exec.Cmd
	daemonCmd  *exec.Cmd
	state      models.TunnelState
}

func NewPlayitManager(dataDir, binDir string) *PlayitManager {
	pm := &PlayitManager{
		dataDir: dataDir,
		binDir:  binDir,
	}
	pm.refreshBinaryPathLocked()
	return pm
}

func (pm *PlayitManager) refreshBinaryPathLocked() {
	if runtime.GOOS == "windows" {
		localBin := filepath.Join(pm.binDir, "playit.exe")
		if _, err := os.Stat(localBin); err == nil {
			pm.cliPath = localBin
			pm.state.BinaryPath = localBin
			pm.state.Installed = true
			return
		}
		if found, err := exec.LookPath("playit.exe"); err == nil {
			pm.cliPath = found
			pm.state.BinaryPath = found
			pm.state.Installed = true
			return
		}
		pm.cliPath = localBin
		pm.state.BinaryPath = localBin
		pm.state.Installed = false
		return
	}

	// Linux & Android require both playitd (daemon) and playit-cli (IPC frontend for claim URL + stdout tunnels).
	if nativeDir := strings.TrimSpace(os.Getenv("SEKAILINK_NATIVE_LIB_DIR")); nativeDir != "" {
		daemonCand := filepath.Join(nativeDir, "libplayit_agent.so")
		cliCand := filepath.Join(nativeDir, "libplayit_cli.so")
		_, dErr := os.Stat(daemonCand)
		_, cErr := os.Stat(cliCand)
		if dErr == nil && cErr == nil {
			pm.daemonPath = daemonCand
			pm.cliPath = cliCand
			pm.state.BinaryPath = daemonCand
			pm.state.Installed = true
			return
		}
	}

	localDaemon := filepath.Join(pm.binDir, "playitd")
	localCLI := filepath.Join(pm.binDir, "playit-cli")
	if _, err := os.Stat(localDaemon); err != nil {
		if found, err2 := exec.LookPath("playitd"); err2 == nil {
			localDaemon = found
		}
	}
	if _, err := os.Stat(localCLI); err != nil {
		if found, err2 := exec.LookPath("playit-cli"); err2 == nil {
			localCLI = found
		}
	}

	_, dErr := os.Stat(localDaemon)
	_, cErr := os.Stat(localCLI)
	pm.daemonPath = localDaemon
	pm.cliPath = localCLI
	pm.state.BinaryPath = localDaemon
	pm.state.Installed = (dErr == nil && cErr == nil)
}

func (pm *PlayitManager) Status() models.TunnelState {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if !pm.state.Installed {
		pm.refreshBinaryPathLocked()
	}
	cp := pm.state
	cp.Addresses = append([]string{}, pm.state.Addresses...)
	cp.Logs = append([]string{}, pm.state.Logs...)
	return cp
}

func (pm *PlayitManager) Install(ctx context.Context) (models.TunnelState, error) {
	pm.mu.Lock()
	pm.refreshBinaryPathLocked()
	if pm.state.Installed {
		st := pm.state
		pm.mu.Unlock()
		return st, nil
	}
	cliDest := pm.cliPath
	daemonDest := pm.daemonPath
	pm.mu.Unlock()

	if err := os.MkdirAll(pm.binDir, 0o755); err != nil {
		return pm.Status(), err
	}

	cliAsset, daemonAsset := matchPlayitAssetNames()
	if cliAsset == "" {
		return pm.Status(), fmt.Errorf("bu isletim sistemi/mimari icin playit otomatik indirme desteklenmiyor (%s/%s)", runtime.GOOS, runtime.GOARCH)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/playit-cloud/playit-agent/releases/latest", nil)
	if err != nil {
		return pm.Status(), err
	}
	req.Header.Set("User-Agent", "MC-SekaiLink/0.1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return pm.Status(), err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return pm.Status(), fmt.Errorf("GitHub playit release API HTTP %d", resp.StatusCode)
	}

	var rel struct {
		Assets []playitReleaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return pm.Status(), err
	}

	if daemonAsset != "" {
		if err := downloadPlayitAsset(ctx, rel.Assets, daemonAsset, daemonDest); err != nil {
			return pm.Status(), err
		}
	}
	if err := downloadPlayitAsset(ctx, rel.Assets, cliAsset, cliDest); err != nil {
		return pm.Status(), err
	}

	pm.mu.Lock()
	pm.refreshBinaryPathLocked()
	pm.state.LastError = ""
	pm.mu.Unlock()
	return pm.Status(), nil
}

func downloadPlayitAsset(ctx context.Context, assets []playitReleaseAsset, assetName, destPath string) error {
	var dlURL string
	for _, a := range assets {
		if strings.EqualFold(a.Name, assetName) {
			dlURL = a.BrowserDownloadURL
			break
		}
	}
	if dlURL == "" {
		return fmt.Errorf("playit release icinde %s dosyasi bulunamadi", assetName)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MC-SekaiLink/0.1.0")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("playit indirme HTTP %d", resp.StatusCode)
	}
	tmp := destPath + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	out.Close()
	_ = os.Chmod(tmp, 0o755)
	if err := os.Rename(tmp, destPath); err != nil {
		_ = os.Remove(destPath)
		return os.Rename(tmp, destPath)
	}
	return nil
}

func matchPlayitAssetNames() (cliAsset string, daemonAsset string) {
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH == "amd64" {
			return "playit-windows-x86_64.exe", ""
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "playit-cli-linux-amd64", "playit-linux-amd64"
		case "arm64":
			return "playit-cli-linux-aarch64", "playit-linux-aarch64"
		}
	}
	return "", ""
}

func (pm *PlayitManager) Start() error {
	pm.mu.Lock()
	if pm.cmd != nil || pm.daemonCmd != nil {
		pm.mu.Unlock()
		return nil
	}
	pm.refreshBinaryPathLocked()
	if !pm.state.Installed {
		pm.mu.Unlock()
		return fmt.Errorf("playit ajani kurulu degil, once yukleyin")
	}

	pm.state.Logs = nil
	pm.state.ClaimURL = ""
	pm.state.LastError = ""

	if runtime.GOOS == "windows" {
		cmd := exec.Command(pm.cliPath, "--stdout")
		cmd.Dir = pm.dataDir
		configurePlayitWindow(cmd)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			pm.mu.Unlock()
			return err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			pm.mu.Unlock()
			return err
		}
		if err := cmd.Start(); err != nil {
			pm.mu.Unlock()
			return err
		}
		pm.cmd = cmd
		pm.state.Running = true
		pm.mu.Unlock()

		pm.watchProcess(cmd, stdout, stderr, true)
		return nil
	}

	// Linux / Android: launch playitd with --secret-path and --socket-path, then attach playit-cli --stdout.
	sockPath := filepath.Join(pm.dataDir, "playit.sock")
	if len(sockPath) > 96 {
		sockPath = filepath.Join(os.TempDir(), "sekailink-playit.sock")
	}
	_ = os.Remove(sockPath)
	secretPath := filepath.Join(pm.dataDir, "playit.toml")

	dCmd := exec.Command(pm.daemonPath, "--secret-path", secretPath, "--socket-path", sockPath)
	dCmd.Dir = pm.dataDir
	configurePlayitWindow(dCmd)
	dOut, err := dCmd.StdoutPipe()
	if err != nil {
		pm.mu.Unlock()
		return err
	}
	dErr, err := dCmd.StderrPipe()
	if err != nil {
		pm.mu.Unlock()
		return err
	}
	if err := dCmd.Start(); err != nil {
		pm.mu.Unlock()
		return err
	}
	pm.daemonCmd = dCmd
	pm.socketPath = sockPath
	pm.state.Running = true
	pm.mu.Unlock()

	pm.watchProcess(dCmd, dOut, dErr, false)

	// Wait briefly for Unix IPC socket creation before starting playit-cli
	for i := 0; i < 40; i++ {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	pm.mu.Lock()
	if pm.daemonCmd == nil {
		pm.mu.Unlock()
		return fmt.Errorf("playitd baslatilamadi")
	}
	cCmd := exec.Command(pm.cliPath, "--socket-path", sockPath, "--stdout")
	cCmd.Dir = pm.dataDir
	configurePlayitWindow(cCmd)
	cOut, err := cCmd.StdoutPipe()
	if err != nil {
		pm.mu.Unlock()
		return err
	}
	cErr, err := cCmd.StderrPipe()
	if err != nil {
		pm.mu.Unlock()
		return err
	}
	if err := cCmd.Start(); err != nil {
		pm.mu.Unlock()
		return err
	}
	pm.cmd = cCmd
	pm.mu.Unlock()

	pm.watchProcess(cCmd, cOut, cErr, true)
	return nil
}

func (pm *PlayitManager) watchProcess(proc *exec.Cmd, stdout, stderr io.ReadCloser, isPrimary bool) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		pm.readLogs(stdout)
	}()
	go func() {
		defer wg.Done()
		pm.readLogs(stderr)
	}()
	go func() {
		wg.Wait()
		err := proc.Wait()
		pm.mu.Lock()
		if isPrimary {
			pm.cmd = nil
			if pm.daemonCmd == nil {
				pm.state.Running = false
			}
		} else {
			pm.daemonCmd = nil
			if pm.cmd == nil {
				pm.state.Running = false
			}
			if pm.socketPath != "" {
				_ = os.Remove(pm.socketPath)
			}
		}
		if err != nil && pm.state.LastError == "" {
			pm.state.LastError = err.Error()
		}
		pm.mu.Unlock()
	}()
}

func (pm *PlayitManager) Stop() error {
	pm.mu.Lock()
	cCmd := pm.cmd
	dCmd := pm.daemonCmd
	sock := pm.socketPath
	pm.cmd = nil
	pm.daemonCmd = nil
	pm.state.Running = false
	pm.mu.Unlock()

	if cCmd != nil && cCmd.Process != nil {
		_ = cCmd.Process.Kill()
	}
	if dCmd != nil && dCmd.Process != nil {
		_ = dCmd.Process.Kill()
	}
	if sock != "" {
		_ = os.Remove(sock)
	}
	return nil
}

func (pm *PlayitManager) readLogs(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		pm.mu.Lock()
		if len(pm.state.Logs) >= 120 {
			pm.state.Logs = append(pm.state.Logs[1:], line)
		} else {
			pm.state.Logs = append(pm.state.Logs, line)
		}
		ParsePlayitOutputLine(line, &pm.state)
		pm.mu.Unlock()
	}
}

// ParsePlayitOutputLine extracts claim URLs and allocated Java/Bedrock tunnel addresses from agent stdout.
func ParsePlayitOutputLine(line string, state *models.TunnelState) {
	if m := claimURLRe.FindString(line); m != "" {
		state.ClaimURL = m
	}
	matches := tunnelAddrRe.FindAllString(line, -1)
	for _, addr := range matches {
		lowerAddr := strings.ToLower(addr)
		if lowerAddr == "playit.gg" ||
			lowerAddr == "api.playit.gg" ||
			lowerAddr == "www.playit.gg" ||
			lowerAddr == "docs.playit.gg" ||
			strings.Contains(line, "playit.gg/claim") ||
			strings.Contains(line, "playit.gg/account") {
			continue
		}
		found := false
		for _, existing := range state.Addresses {
			if existing == addr {
				found = true
				break
			}
		}
		if !found {
			state.Addresses = append(state.Addresses, addr)
		}
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "bedrock") || strings.Contains(lowerLine, "udp") || strings.Contains(lowerLine, "19132") {
			state.BedrockAddress = addr
		} else if strings.Contains(lowerLine, "java") || strings.Contains(lowerAddr, "joinmc.link") || state.JavaAddress == "" {
			state.JavaAddress = addr
		}
	}
}
