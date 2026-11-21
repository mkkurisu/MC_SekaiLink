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
	claimURLRe    = regexp.MustCompile(`https://playit\.gg/claim/[A-Za-z0-9\-_]+`)
	tunnelAddrRe  = regexp.MustCompile(`([a-zA-Z0-9.\-]+\.(?:joinmc\.link|playit\.gg|ply\.gg|at\.ply\.gg)(?::\d+)?)`)
)

type PlayitManager struct {
	dataDir string
	binDir  string

	mu    sync.Mutex
	cmd   *exec.Cmd
	state models.TunnelState
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
	if nativeDir := strings.TrimSpace(os.Getenv("SEKAILINK_NATIVE_LIB_DIR")); nativeDir != "" {
		cand := filepath.Join(nativeDir, "libplayit_agent.so")
		if _, err := os.Stat(cand); err == nil {
			pm.state.BinaryPath = cand
			pm.state.Installed = true
			return
		}
	}
	exeName := "playit"
	if runtime.GOOS == "windows" {
		exeName = "playit.exe"
	}
	localBin := filepath.Join(pm.binDir, exeName)
	if _, err := os.Stat(localBin); err == nil {
		pm.state.BinaryPath = localBin
		pm.state.Installed = true
		return
	}
	if found, err := exec.LookPath(exeName); err == nil {
		pm.state.BinaryPath = found
		pm.state.Installed = true
		return
	}
	pm.state.BinaryPath = localBin
	pm.state.Installed = false
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
	destPath := pm.state.BinaryPath
	pm.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return pm.Status(), err
	}

	assetMatch := matchPlayitAssetName()
	if assetMatch == "" {
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
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return pm.Status(), err
	}

	var dlURL string
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, assetMatch) {
			dlURL = a.BrowserDownloadURL
			break
		}
	}
	if dlURL == "" {
		for _, a := range rel.Assets {
			if strings.Contains(strings.ToLower(a.Name), strings.ToLower(assetMatch)) {
				dlURL = a.BrowserDownloadURL
				break
			}
		}
	}
	if dlURL == "" {
		return pm.Status(), fmt.Errorf("playit release icinde %s dosyasi bulunamadi", assetMatch)
	}

	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return pm.Status(), err
	}
	dlReq.Header.Set("User-Agent", "MC-SekaiLink/0.1.0")
	dlResp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(dlReq)
	if err != nil {
		return pm.Status(), err
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		return pm.Status(), fmt.Errorf("playit indirme HTTP %d", dlResp.StatusCode)
	}

	tmp := destPath + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return pm.Status(), err
	}
	if _, err := io.Copy(out, dlResp.Body); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return pm.Status(), err
	}
	out.Close()
	_ = os.Chmod(tmp, 0o755)
	if err := os.Rename(tmp, destPath); err != nil {
		_ = os.Remove(destPath)
		if err2 := os.Rename(tmp, destPath); err2 != nil {
			return pm.Status(), err2
		}
	}

	pm.mu.Lock()
	pm.state.Installed = true
	pm.state.BinaryPath = destPath
	pm.state.LastError = ""
	pm.mu.Unlock()
	return pm.Status(), nil
}

func matchPlayitAssetName() string {
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH == "amd64" {
			return "playit-windows-x86_64.exe"
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "playit-linux-amd64"
		case "arm64":
			return "playit-linux-aarch64"
		}
	}
	return ""
}

func (pm *PlayitManager) Start() error {
	pm.mu.Lock()
	if pm.cmd != nil {
		pm.mu.Unlock()
		return nil
	}
	pm.refreshBinaryPathLocked()
	if !pm.state.Installed {
		pm.mu.Unlock()
		return fmt.Errorf("playit ajani kurulu degil, once yukleyin")
	}
	secretPath := filepath.Join(pm.dataDir, "playit.toml")
	cmd := exec.Command(pm.state.BinaryPath, "--secret_path", secretPath)
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
	pm.state.LastError = ""
	pm.mu.Unlock()

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
		err := cmd.Wait()
		pm.mu.Lock()
		pm.cmd = nil
		pm.state.Running = false
		if err != nil {
			pm.state.LastError = err.Error()
		}
		pm.mu.Unlock()
	}()

	return nil
}

func (pm *PlayitManager) Stop() error {
	pm.mu.Lock()
	cmd := pm.cmd
	pm.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Kill()
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
		if strings.Contains(addr, "playit.gg/claim") {
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
		} else if strings.Contains(lowerLine, "java") || strings.Contains(addr, "joinmc.link") || state.JavaAddress == "" {
			state.JavaAddress = addr
		}
	}
}
