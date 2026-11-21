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
	"sort"
	"strconv"
	"strings"
)

type JavaInfo struct {
	Detected bool   `json:"detected"`
	Path     string `json:"path"`
	Version  string `json:"version"`
	Major    int    `json:"major"`
}

var javaVersionRe = regexp.MustCompile(`version "([^"]+)"`)

// ParseJavaMajor extracts the major Java feature version (e.g. "25.0.1" -> 25, "1.8.0_402" -> 8).
func ParseJavaMajor(version string) int {
	v := strings.TrimSpace(version)
	if v == "" {
		return 0
	}
	v = strings.TrimLeft(v, "v")
	if idx := strings.IndexAny(v, "-_+"); idx > 0 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 {
		return 0
	}
	p0, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	if p0 == 1 && len(parts) > 1 {
		if p1, err := strconv.Atoi(parts[1]); err == nil {
			return p1
		}
	}
	return p0
}

// RequiredJavaMajor returns the minimum Java major version required by a Minecraft release.
func RequiredJavaMajor(mcVersion string) int {
	base, _, _ := strings.Cut(strings.TrimSpace(mcVersion), "-")
	if base == "" || strings.EqualFold(base, "latest") {
		return 21
	}
	parts := strings.Split(base, ".")
	p0, _ := strconv.Atoi(parts[0])
	var p1, p2 int
	if len(parts) > 1 {
		p1, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		p2, _ = strconv.Atoi(parts[2])
	}
	if p0 >= 26 {
		return 25
	}
	if p0 == 1 {
		if p1 >= 21 || (p1 == 20 && p2 >= 5) {
			return 21
		}
		if p1 >= 18 {
			return 17
		}
		if p1 == 17 {
			return 16
		}
	}
	return 21
}

// DetectJava returns the highest-version Java runtime available on this machine.
func DetectJava(preferredPath, dataDir string) JavaInfo {
	all := DetectAllJavas(preferredPath, dataDir)
	if len(all) == 0 {
		return JavaInfo{}
	}
	return all[0]
}

// DetectJavaForMCVersion selects a Java runtime that meets the minimum version required by mcVersion.
func DetectJavaForMCVersion(mcVersion, preferredPath, dataDir string) (JavaInfo, int, bool) {
	req := RequiredJavaMajor(mcVersion)
	all := DetectAllJavas(preferredPath, dataDir)
	if len(all) == 0 {
		return JavaInfo{}, req, false
	}
	cleanPref := ""
	if preferredPath != "" {
		cleanPref = filepath.Clean(preferredPath)
	}
	if cleanPref != "" {
		for _, j := range all {
			if strings.EqualFold(j.Path, cleanPref) && j.Major >= req {
				return j, req, true
			}
		}
	}
	for _, j := range all {
		if j.Major >= req {
			return j, req, true
		}
	}
	return all[0], req, false
}

// DetectAllJavas discovers all installed Java runtimes sorted from highest major version to lowest.
func DetectAllJavas(preferredPath, dataDir string) []JavaInfo {
	candidates := []string{}
	if preferredPath != "" {
		candidates = append(candidates, preferredPath)
	}
	if env := strings.TrimSpace(os.Getenv("SEKAILINK_JAVA_HOME")); env != "" {
		candidates = append(candidates, javaBinaryInHome(env))
	}
	if dataDir != "" {
		candidates = append(candidates, javaBinaryInHome(filepath.Join(dataDir, "jre25")))
		candidates = append(candidates, javaBinaryInHome(filepath.Join(dataDir, "jre21")))
		candidates = append(candidates, javaBinaryInHome(filepath.Join(dataDir, "jre")))
		candidates = append(candidates, javaBinaryInHome(filepath.Join(dataDir, "runtime", "jre")))
		if parent := filepath.Dir(filepath.Clean(dataDir)); parent != "" && parent != dataDir {
			candidates = append(candidates, javaBinaryInHome(filepath.Join(parent, "jre25")))
			candidates = append(candidates, javaBinaryInHome(filepath.Join(parent, "jre21")))
			candidates = append(candidates, javaBinaryInHome(filepath.Join(parent, "jre")))
		}
	}
	if env := strings.TrimSpace(os.Getenv("JAVA_HOME")); env != "" {
		candidates = append(candidates, javaBinaryInHome(env))
	}
	if found, err := exec.LookPath(javaExeName()); err == nil {
		candidates = append(candidates, found)
	}
	if runtime.GOOS == "windows" {
		for _, root := range []string{
			`C:\Users\Kurisu\Tools\jre25`,
			`C:\Users\Kurisu\Tools\jdk25`,
			`C:\Users\Kurisu\Tools\jdk21`,
			filepath.Join(os.Getenv("USERPROFILE"), "Tools", "jre25"),
			filepath.Join(os.Getenv("USERPROFILE"), "Tools", "jdk25"),
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
		roots := []string{"/usr/lib/jvm", "/opt/java", "/usr/local/java"}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			roots = append(roots,
				filepath.Join(home, ".jdks"),
				filepath.Join(home, ".sdkman", "candidates", "java"),
			)
		}
		for _, root := range roots {
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
	}

	seen := make(map[string]bool)
	var foundList []JavaInfo
	for _, c := range candidates {
		c = filepath.Clean(c)
		if c == "" || c == "." || seen[c] {
			continue
		}
		seen[c] = true
		if info, err := os.Stat(c); err != nil || info.IsDir() {
			continue
		}
		ver := probeJavaVersion(c)
		if ver != "" {
			foundList = append(foundList, JavaInfo{
				Detected: true,
				Path:     c,
				Version:  ver,
				Major:    ParseJavaMajor(ver),
			})
		}
	}
	sort.SliceStable(foundList, func(i, j int) bool {
		return foundList[i].Major > foundList[j].Major
	})
	return foundList
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

func javaEnvForBinary(javaPath string) []string {
	if javaPath == "" {
		return nil
	}
	jreRoot := filepath.Dir(filepath.Dir(filepath.Clean(javaPath)))
	_, hasJLI := os.Stat(filepath.Join(jreRoot, "lib", "jli"))
	if DetectPlatformMode() != "android" && (runtime.GOOS != "linux" || hasJLI != nil) {
		return nil
	}
	libPaths := []string{
		filepath.Join(jreRoot, "lib"),
		filepath.Join(jreRoot, "lib", "jli"),
		filepath.Join(jreRoot, "lib", "server"),
	}
	if existing := strings.TrimSpace(os.Getenv("LD_LIBRARY_PATH")); existing != "" {
		libPaths = append(libPaths, existing)
	}
	ldPath := strings.Join(libPaths, ":")

	baseEnv := os.Environ()
	out := make([]string, 0, len(baseEnv)+2)
	for _, entry := range baseEnv {
		if strings.HasPrefix(entry, "LD_LIBRARY_PATH=") || strings.HasPrefix(entry, "JAVA_HOME=") {
			continue
		}
		out = append(out, entry)
	}
	out = append(out, "JAVA_HOME="+jreRoot, "LD_LIBRARY_PATH="+ldPath)
	return out
}

func probeJavaVersion(javaPath string) string {
	cmd := exec.Command(javaPath, "-version")
	configureCmdWindow(cmd)
	if env := javaEnvForBinary(javaPath); len(env) > 0 {
		cmd.Env = env
	}
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

// InstallManagedJRE downloads Eclipse Temurin JRE (default Java 25) into dataDir/jre<major> for Desktop/Linux hosts.
func InstallManagedJRE(ctx context.Context, dataDir string, majorVersion int, progress func(string)) (JavaInfo, error) {
	if majorVersion <= 0 {
		majorVersion = 25
	}
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

	url := fmt.Sprintf("https://api.adoptium.net/v3/binary/latest/%d/ga/%s/%s/jre/hotspot/normal/eclipse?project=jdk", majorVersion, osPart, archPart)
	if progress != nil {
		progress(fmt.Sprintf("Java %d JRE indiriliyor...", majorVersion))
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

	jreRoot := filepath.Join(dataDir, fmt.Sprintf("jre%d", majorVersion))
	_ = os.RemoveAll(jreRoot)
	extractTmp := filepath.Join(dataDir, "jre_tmp")
	_ = os.RemoveAll(extractTmp)
	if err := os.MkdirAll(extractTmp, 0o755); err != nil {
		return JavaInfo{}, err
	}
	defer os.RemoveAll(extractTmp)

	if progress != nil {
		progress(fmt.Sprintf("Java %d JRE arsivden cikariliyor...", majorVersion))
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

	if runtime.GOOS != "windows" {
		binDir := filepath.Join(jreRoot, "bin")
		if binEntries, err := os.ReadDir(binDir); err == nil {
			for _, be := range binEntries {
				if !be.IsDir() {
					_ = os.Chmod(filepath.Join(binDir, be.Name()), 0o755)
				}
			}
		}
	}

	binPath := javaBinaryInHome(jreRoot)
	ver := probeJavaVersion(binPath)
	if ver == "" {
		return JavaInfo{}, fmt.Errorf("JRE extracted but java binary failed to run")
	}
	return JavaInfo{
		Detected: true,
		Path:     binPath,
		Version:  ver,
		Major:    ParseJavaMajor(ver),
	}, nil
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
