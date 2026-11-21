package engine

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type JavaInfo struct {
	Detected bool   `json:"detected"`
	Path     string `json:"path"`
	Version  string `json:"version"`
}

var javaVersionRe = regexp.MustCompile(`version "([^"]+)"`)

// DetectJava inspects common environment variables, bundled runtimes, and system directories for Java.
func DetectJava(preferredPath, dataDir string) JavaInfo {
	candidates := []string{}
	if preferredPath != "" {
		candidates = append(candidates, preferredPath)
	}
	if env := strings.TrimSpace(os.Getenv("SEKAILINK_JAVA_HOME")); env != "" {
		candidates = append(candidates, javaBinaryInHome(env))
	}
	if dataDir != "" {
		candidates = append(candidates, javaBinaryInHome(filepath.Join(dataDir, "jre")))
		candidates = append(candidates, javaBinaryInHome(filepath.Join(dataDir, "runtime", "jre")))
	}
	if env := strings.TrimSpace(os.Getenv("JAVA_HOME")); env != "" {
		candidates = append(candidates, javaBinaryInHome(env))
	}
	if found, err := exec.LookPath(javaExeName()); err == nil {
		candidates = append(candidates, found)
	}
	if runtime.GOOS == "windows" {
		for _, root := range []string{
			`C:\Users\Kurisu\Tools\jdk21`,
			filepath.Join(os.Getenv("USERPROFILE"), "Tools", "jdk21"),
			`C:\Program Files\Eclipse Adoptium`,
			`C:\Program Files\Java`,
			`C:\Program Files\Microsoft`,
		} {
			if entries, err := os.ReadDir(root); err == nil {
				if hasJavaExe(root) {
					candidates = append(candidates, javaBinaryInHome(root))
				}
				for _, e := range entries {
					if e.IsDir() {
						candidates = append(candidates, javaBinaryInHome(filepath.Join(root, e.Name())))
					}
				}
			}
		}
	} else {
		for _, root := range []string{"/usr/lib/jvm", "/opt/java"} {
			if entries, err := os.ReadDir(root); err == nil {
				for _, e := range entries {
					if e.IsDir() {
						candidates = append(candidates, javaBinaryInHome(filepath.Join(root, e.Name())))
					}
				}
			}
		}
	}

	seen := make(map[string]bool)
	for _, c := range candidates {
		c = filepath.Clean(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if info, err := os.Stat(c); err != nil || info.IsDir() {
			continue
		}
		ver := probeJavaVersion(c)
		if ver != "" {
			return JavaInfo{Detected: true, Path: c, Version: ver}
		}
	}
	return JavaInfo{}
}

func javaExeName() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

func javaBinaryInHome(home string) string {
	return filepath.Join(home, "bin", javaExeName())
}

func hasJavaExe(home string) bool {
	_, err := os.Stat(javaBinaryInHome(home))
	return err == nil
}

func probeJavaVersion(javaPath string) string {
	cmd := exec.Command(javaPath, "-version")
	configureCmdWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	text := string(out)
	if m := javaVersionRe.FindStringSubmatch(text); len(m) == 2 {
		return m[1]
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return "unknown"
}

// InstallManagedJRE downloads Eclipse Temurin 21 JRE into dataDir/jre for Desktop/Linux hosts.
func InstallManagedJRE(ctx context.Context, dataDir string, progress func(string)) (JavaInfo, error) {
	goos := runtime.GOOS
	arch := runtime.GOARCH
	var osPart, archPart string
	switch goos {
	case "windows":
		osPart = "windows"
	case "linux":
		osPart = "linux"
	default:
		return JavaInfo{}, fmt.Errorf("automatic JRE download not supported on %s", goos)
	}
	switch arch {
	case "amd64":
		archPart = "x64"
	case "arm64":
		archPart = "aarch64"
	default:
		return JavaInfo{}, fmt.Errorf("unsupported CPU architecture for JRE: %s", arch)
	}

	url := fmt.Sprintf("https://api.adoptium.net/v3/binary/latest/21/ga/%s/%s/jre/hotspot/normal/eclipse?project=jdk", osPart, archPart)
	if progress != nil {
		progress("Java 21 JRE indiriliyor...")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return JavaInfo{}, err
	}
	req.Header.Set("User-Agent", "MC-SekaiLink/0.1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return JavaInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return JavaInfo{}, fmt.Errorf("adoptium download status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "sekailink-jre-*")
	if err != nil {
		return JavaInfo{}, err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return JavaInfo{}, err
	}
	tmpFile.Close()

	jreRoot := filepath.Join(dataDir, "jre")
	_ = os.RemoveAll(jreRoot)
	extractTmp := filepath.Join(dataDir, "jre_tmp")
	_ = os.RemoveAll(extractTmp)
	if err := os.MkdirAll(extractTmp, 0o755); err != nil {
		return JavaInfo{}, err
	}
	defer os.RemoveAll(extractTmp)

	if progress != nil {
		progress("Java 21 JRE arsivden cikariliyor...")
	}

	if goos == "windows" {
		if err := extractZipSafe(tmpPath, extractTmp); err != nil {
			return JavaInfo{}, err
		}
	} else {
		if err := extractTarGzSafe(tmpPath, extractTmp); err != nil {
			return JavaInfo{}, err
		}
	}

	entries, err := os.ReadDir(extractTmp)
	if err != nil || len(entries) == 0 {
		return JavaInfo{}, fmt.Errorf("extracted JRE directory empty")
	}
	inner := filepath.Join(extractTmp, entries[0].Name())
	if !entries[0].IsDir() {
		inner = extractTmp
	}
	if err := os.Rename(inner, jreRoot); err != nil {
		return JavaInfo{}, err
	}

	info := DetectJava(javaBinaryInHome(jreRoot), dataDir)
	if !info.Detected {
		return info, fmt.Errorf("JRE extracted but java binary failed to run")
	}
	return info, nil
}

func extractZipSafe(zipPath, targetDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	cleanBase := filepath.Clean(targetDir) + string(os.PathSeparator)

	for _, f := range r.File {
		dest := filepath.Join(targetDir, f.Name)
		cleanDest := filepath.Clean(dest)
		if !strings.HasPrefix(cleanDest, cleanBase) && cleanDest != filepath.Clean(targetDir) {
			return fmt.Errorf("zip-slip blocked: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanDest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cleanDest), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(cleanDest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func extractTarGzSafe(tarGzPath, targetDir string) error {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	cleanBase := filepath.Clean(targetDir) + string(os.PathSeparator)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		dest := filepath.Join(targetDir, hdr.Name)
		cleanDest := filepath.Clean(dest)
		if !strings.HasPrefix(cleanDest, cleanBase) && cleanDest != filepath.Clean(targetDir) {
			return fmt.Errorf("tar-slip blocked: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(cleanDest, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(cleanDest), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(cleanDest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			_ = os.MkdirAll(filepath.Dir(cleanDest), 0o755)
			_ = os.Remove(cleanDest)
			_ = os.Symlink(hdr.Linkname, cleanDest)
		}
	}
	return nil
}
