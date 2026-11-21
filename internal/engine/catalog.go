package engine

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

var catalogClient = &http.Client{Timeout: 45 * time.Second}
var downloadClient = &http.Client{Timeout: 10 * time.Minute}

func ListEngineDescriptors() []models.EngineDescriptor {
	return []models.EngineDescriptor{
		{
			ID:                 models.EnginePaper,
			Name:               "Paper (Java + Cross-Play)",
			Edition:            "java",
			SupportsPlugins:    true,
			SupportsCrossPlay:  true,
			SupportedOnAndroid: true,
			DescriptionTR:      "Yuksek performansli Spigot/Bukkit eklenti motoru. GeyserMC ile Bedrock (Telefon/Konsol) Cross-Play icin en ideal secimdir.",
			DescriptionEN:      "High-performance plugin server. Best choice for Java + Bedrock Cross-Play via GeyserMC.",
		},
		{
			ID:                 models.EnginePurpur,
			Name:               "Purpur (Java + Cross-Play)",
			Edition:            "java",
			SupportsPlugins:    true,
			SupportsCrossPlay:  true,
			SupportedOnAndroid: true,
			DescriptionTR:      "Paper tabanli, ekstra oyun ozellestirmeleri ve yuksek optimizasyon sunan eklenti motoru.",
			DescriptionEN:      "Paper fork with extra gameplay customization and performance tuning.",
		},
		{
			ID:                 models.EngineFabric,
			Name:               "Fabric (Modern Modlu Java)",
			Edition:            "java",
			SupportsMods:       true,
			SupportsCrossPlay:  true,
			SupportedOnAndroid: true,
			DescriptionTR:      "Hafif ve hizli mod yukleyici. Performans modlari, icerik modlari ve Geyser-Fabric destegi sunar.",
			DescriptionEN:      "Lightweight modern mod loader with Fabric Geyser Cross-Play support.",
		},
		{
			ID:                 models.EngineNeoForge,
			Name:               "NeoForge (Agir Modlu Java)",
			Edition:            "java",
			SupportsMods:       true,
			SupportsCrossPlay:  true,
			SupportedOnAndroid: true,
			DescriptionTR:      "Buyuk mod paketleri ve klasik Forge ekosistemi icin modern mod yukleyici.",
			DescriptionEN:      "Modern Forge continuation for large modpacks.",
		},
		{
			ID:                 models.EngineVanilla,
			Name:               "Vanilla (Resmi Java)",
			Edition:            "java",
			SupportedOnAndroid: true,
			DescriptionTR:      "Mojang tarafindan sunulan saf, degistirilmemis resmi Minecraft Java sunucusu.",
			DescriptionEN:      "Official unmodified Minecraft Java Edition server from Mojang.",
		},
		{
			ID:                 models.EngineBDS,
			Name:               "Bedrock Dedicated Server (Resmi BDS)",
			Edition:            "bedrock",
			SupportedOnAndroid: false,
			DescriptionTR:      "Windows ve Linux (x64) icin resmi Mojang Bedrock motoru. .mcpack / .mcaddon destekler.",
			DescriptionEN:      "Official Mojang Bedrock server for Windows & Linux x64 with native addon support.",
		},
		{
			ID:                 models.EnginePowerNukkitX,
			Name:               "PowerNukkitX (Bedrock Cross-Platform)",
			Edition:            "bedrock",
			SupportsPlugins:    true,
			SupportedOnAndroid: true,
			DescriptionTR:      "Java uzerinde calisan Bedrock sunucusu. Android ARM64 telefonlarda saf Bedrock sunucusu acmak icin desteklenir.",
			DescriptionEN:      "Java-powered Bedrock server engine that runs smoothly on Windows, Linux, and Android ARM64.",
		},
	}
}

func FetchVersions(ctx context.Context, eng models.EngineType) ([]string, error) {
	switch eng {
	case models.EnginePaper:
		var res struct {
			Versions map[string][]string `json:"versions"`
		}
		if err := getJSON(ctx, "https://fill.papermc.io/v3/projects/paper", &res); err != nil {
			return nil, err
		}
		return limitStrings(extractSortedPaperVersions(res.Versions), 40), nil

	case models.EnginePurpur:
		var res struct {
			Versions []string `json:"versions"`
		}
		if err := getJSON(ctx, "https://api.purpurmc.org/v2/purpur", &res); err != nil {
			return nil, err
		}
		reverseStrings(res.Versions)
		return limitStrings(res.Versions, 40), nil

	case models.EngineFabric:
		var res []struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		}
		if err := getJSON(ctx, "https://meta.fabricmc.net/v2/versions/game", &res); err != nil {
			return nil, err
		}
		out := make([]string, 0, 40)
		for _, v := range res {
			if v.Stable {
				out = append(out, v.Version)
			}
			if len(out) >= 40 {
				break
			}
		}
		return out, nil

	case models.EngineVanilla:
		var res struct {
			Versions []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"versions"`
		}
		if err := getJSON(ctx, "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &res); err != nil {
			return nil, err
		}
		out := make([]string, 0, 40)
		for _, v := range res.Versions {
			if v.Type == "release" {
				out = append(out, v.ID)
			}
			if len(out) >= 40 {
				break
			}
		}
		return out, nil

	case models.EngineNeoForge:
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "MC-SekaiLink/0.1.0")
		resp, err := catalogClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("neoforge metadata HTTP %d", resp.StatusCode)
		}
		var meta struct {
			Versioning struct {
				Versions []string `xml:"versions>version"`
			} `xml:"versioning"`
		}
		if err := xml.NewDecoder(resp.Body).Decode(&meta); err != nil {
			return nil, err
		}
		out := make([]string, 0, 40)
		for i := len(meta.Versioning.Versions) - 1; i >= 0 && len(out) < 40; i-- {
			v := meta.Versioning.Versions[i]
			if !strings.Contains(v, "alpha") {
				out = append(out, v)
			}
		}
		return out, nil

	case models.EngineBDS:
		return []string{"latest"}, nil

	case models.EnginePowerNukkitX:
		var rels []struct {
			TagName string `json:"tag_name"`
		}
		if err := getJSON(ctx, "https://api.github.com/repos/PowerNukkitX/PowerNukkitX/releases?per_page=10", &rels); err == nil && len(rels) > 0 {
			out := make([]string, 0, len(rels))
			for _, r := range rels {
				if r.TagName != "" {
					out = append(out, r.TagName)
				}
			}
			if len(out) > 0 {
				return out, nil
			}
		}
		return []string{"latest"}, nil

	default:
		return nil, fmt.Errorf("unknown engine: %s", eng)
	}
}

func InstallEngineArtifacts(ctx context.Context, inst models.ServerInstance, javaPath string, progress func(string)) error {
	if err := os.MkdirAll(inst.Path, 0o755); err != nil {
		return err
	}
	if progress == nil {
		progress = func(string) {}
	}

	switch inst.Engine {
	case models.EnginePaper:
		if err := installPaper(ctx, inst.MCVersion, inst.Path, progress); err != nil {
			return err
		}
	case models.EnginePurpur:
		url := fmt.Sprintf("https://api.purpurmc.org/v2/purpur/%s/latest/download", inst.MCVersion)
		progress(fmt.Sprintf("Purpur %s indiriliyor...", inst.MCVersion))
		if err := downloadToFile(ctx, url, filepath.Join(inst.Path, "server.jar")); err != nil {
			return err
		}
	case models.EngineFabric:
		if err := installFabric(ctx, inst.MCVersion, inst.Path, progress); err != nil {
			return err
		}
	case models.EngineVanilla:
		if err := installVanilla(ctx, inst.MCVersion, inst.Path, progress); err != nil {
			return err
		}
	case models.EngineNeoForge:
		if err := installNeoForge(ctx, inst.MCVersion, inst.Path, javaPath, progress); err != nil {
			return err
		}
	case models.EngineBDS:
		if err := installBDS(ctx, inst.Path, progress); err != nil {
			return err
		}
	case models.EnginePowerNukkitX:
		if err := installPowerNukkitX(ctx, inst.MCVersion, inst.Path, progress); err != nil {
			return err
		}
	default:
		return fmt.Errorf("desteklenmeyen motor: %s", inst.Engine)
	}

	// Write EULA & default server.properties
	if inst.Engine != models.EngineBDS && inst.Engine != models.EnginePowerNukkitX {
		eulaVal := "false"
		if inst.EULAAccepted {
			eulaVal = "true"
		}
		if err := os.WriteFile(filepath.Join(inst.Path, "eula.txt"), []byte("# MC SekaiLink\neula="+eulaVal+"\n"), 0o644); err != nil {
			return err
		}
		props, _ := ReadProperties(inst.Path)
		if inst.JavaPort > 0 {
			props["server-port"] = fmt.Sprintf("%d", inst.JavaPort)
		}
		if inst.Name != "" {
			props["motd"] = inst.Name + " | MC SekaiLink"
		}
		if inst.ActiveWorld != "" {
			props["level-name"] = inst.ActiveWorld
		}
		if err := WriteProperties(inst.Path, props); err != nil {
			return err
		}
	} else {
		propsPath := filepath.Join(inst.Path, "server.properties")
		if _, err := os.Stat(propsPath); os.IsNotExist(err) {
			props := DefaultBedrockProperties()
			if inst.BedrockPort > 0 {
				props["server-port"] = fmt.Sprintf("%d", inst.BedrockPort)
			}
			if inst.Name != "" {
				props["server-name"] = inst.Name
			}
			if err := WriteProperties(inst.Path, props); err != nil {
				return err
			}
		}
	}

	if inst.CrossPlay.Enabled && inst.Engine != models.EngineBDS && inst.Engine != models.EnginePowerNukkitX && inst.Engine != models.EngineVanilla {
		if err := installCrossPlayPlugins(ctx, inst, progress); err != nil {
			return fmt.Errorf("cross-play kurulumu basarisiz: %w", err)
		}
	}

	progress("Kurulum tamamlandi.")
	return nil
}

func installPaper(ctx context.Context, mcVersion, serverDir string, progress func(string)) error {
	progress(fmt.Sprintf("Paper %s surum bilgisi aliniyor...", mcVersion))
	var latest struct {
		ID        int `json:"id"`
		Downloads map[string]struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"downloads"`
	}
	u := fmt.Sprintf("https://fill.papermc.io/v3/projects/paper/versions/%s/builds/latest", mcVersion)
	if err := getJSON(ctx, u, &latest); err != nil {
		return err
	}
	dl := latest.Downloads["server:default"]
	if dl.URL == "" {
		for _, entry := range latest.Downloads {
			if entry.URL != "" {
				dl = entry
				break
			}
		}
	}
	if dl.URL == "" {
		return fmt.Errorf("Paper %s icin indirilebilir build bulunamadi", mcVersion)
	}
	progress(fmt.Sprintf("Paper #%d indiriliyor...", latest.ID))
	return downloadToFile(ctx, dl.URL, filepath.Join(serverDir, "server.jar"))
}

func installFabric(ctx context.Context, mcVersion, serverDir string, progress func(string)) error {
	progress("Fabric loader ve installer surumleri aliniyor...")
	var loaders []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := getJSON(ctx, "https://meta.fabricmc.net/v2/versions/loader", &loaders); err != nil {
		return err
	}
	var installers []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := getJSON(ctx, "https://meta.fabricmc.net/v2/versions/installer", &installers); err != nil {
		return err
	}
	if len(loaders) == 0 || len(installers) == 0 {
		return fmt.Errorf("Fabric surum bilgisi bos")
	}
	loaderVer := loaders[0].Version
	for _, l := range loaders {
		if l.Stable {
			loaderVer = l.Version
			break
		}
	}
	instVer := installers[0].Version
	for _, i := range installers {
		if i.Stable {
			instVer = i.Version
			break
		}
	}
	dl := fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s/%s/%s/server/jar", mcVersion, loaderVer, instVer)
	progress(fmt.Sprintf("Fabric %s (loader %s) indiriliyor...", mcVersion, loaderVer))
	return downloadToFile(ctx, dl, filepath.Join(serverDir, "server.jar"))
}

func installVanilla(ctx context.Context, mcVersion, serverDir string, progress func(string)) error {
	progress("Mojang surum manifesti aliniyor...")
	var manifest struct {
		Versions []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"versions"`
	}
	if err := getJSON(ctx, "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &manifest); err != nil {
		return err
	}
	var verURL string
	for _, v := range manifest.Versions {
		if v.ID == mcVersion {
			verURL = v.URL
			break
		}
	}
	if verURL == "" {
		return fmt.Errorf("Vanilla %s bulunamadi", mcVersion)
	}
	var detail struct {
		Downloads struct {
			Server struct {
				URL string `json:"url"`
			} `json:"server"`
		} `json:"downloads"`
	}
	if err := getJSON(ctx, verURL, &detail); err != nil {
		return err
	}
	if detail.Downloads.Server.URL == "" {
		return fmt.Errorf("Vanilla %s server jar URL bulunamadi", mcVersion)
	}
	progress(fmt.Sprintf("Vanilla %s indiriliyor...", mcVersion))
	return downloadToFile(ctx, detail.Downloads.Server.URL, filepath.Join(serverDir, "server.jar"))
}

func installNeoForge(ctx context.Context, neoVersion, serverDir, javaPath string, progress func(string)) error {
	if javaPath == "" {
		return fmt.Errorf("NeoForge kurulumu icin Java 21 gerekli")
	}
	installerPath := filepath.Join(serverDir, "neoforge-installer.jar")
	dl := fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", neoVersion, neoVersion)
	progress(fmt.Sprintf("NeoForge %s yukleyici indiriliyor...", neoVersion))
	if err := downloadToFile(ctx, dl, installerPath); err != nil {
		return err
	}
	defer os.Remove(installerPath)

	progress("NeoForge sunucu kutuphaneleri kuruluyor (biraz surebilir)...")
	cmd := exec.CommandContext(ctx, javaPath, "-jar", installerPath, "--installServer")
	cmd.Dir = serverDir
	configureCmdWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("NeoForge installer hatasi: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func installBDS(ctx context.Context, serverDir string, progress func(string)) error {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return fmt.Errorf("resmi BDS yalnizca Windows ve Linux x64 destekler")
	}
	if runtime.GOARCH != "amd64" {
		return fmt.Errorf("resmi BDS yalnizca x86_64/amd64 islemcilerde calisir (Android/ARM64 icin Paper+Geyser veya PowerNukkitX secin)")
	}
	progress("Resmi Bedrock Dedicated Server indirme linki aliniyor...")
	var links struct {
		Result struct {
			Links []struct {
				DownloadType string `json:"downloadType"`
				DownloadURL  string `json:"downloadUrl"`
			} `json:"links"`
		} `json:"result"`
	}
	if err := getJSON(ctx, "https://net-secondary.web.minecraft-services.net/api/v1.0/download/links", &links); err != nil {
		return err
	}
	targetType := "serverBedrockWindows"
	if runtime.GOOS == "linux" {
		targetType = "serverBedrockLinux"
	}
	var dl string
	for _, l := range links.Result.Links {
		if l.DownloadType == targetType {
			dl = l.DownloadURL
			break
		}
	}
	if dl == "" {
		return fmt.Errorf("BDS indirme baglantisi alinamadi (%s)", targetType)
	}
	tmpZip := filepath.Join(serverDir, "bds-download.zip")
	progress("Bedrock Dedicated Server arsive indiriliyor...")
	if err := downloadToFile(ctx, dl, tmpZip); err != nil {
		return err
	}
	defer os.Remove(tmpZip)

	progress("Bedrock Dedicated Server cikartiliyor...")
	if err := extractZipSafe(tmpZip, serverDir); err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		_ = os.Chmod(filepath.Join(serverDir, "bedrock_server"), 0o755)
	}
	return nil
}

func installPowerNukkitX(ctx context.Context, tag, serverDir string, progress func(string)) error {
	progress("PowerNukkitX surumu araniyor...")
	apiURL := "https://api.github.com/repos/PowerNukkitX/PowerNukkitX/releases/latest"
	if tag != "" && tag != "latest" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/PowerNukkitX/PowerNukkitX/releases/tags/%s", tag)
	}
	var rel struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := getJSON(ctx, apiURL, &rel); err != nil {
		return err
	}
	var dl string
	for _, a := range rel.Assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".jar") && !strings.Contains(lower, "sources") && !strings.Contains(lower, "javadoc") {
			dl = a.BrowserDownloadURL
			if strings.Contains(lower, "shaded") {
				break
			}
		}
	}
	if dl == "" {
		return fmt.Errorf("PowerNukkitX jar varligi bulunamadi")
	}
	progress("PowerNukkitX indiriliyor...")
	return downloadToFile(ctx, dl, filepath.Join(serverDir, "server.jar"))
}

func installCrossPlayPlugins(ctx context.Context, inst models.ServerInstance, progress func(string)) error {
	var platform, destDir, configSubDir string
	switch inst.Engine {
	case models.EnginePaper, models.EnginePurpur:
		platform = "spigot"
		destDir = filepath.Join(inst.Path, "plugins")
		configSubDir = filepath.Join(inst.Path, "plugins", "Geyser-Spigot")
	case models.EngineFabric:
		platform = "fabric"
		destDir = filepath.Join(inst.Path, "mods")
		configSubDir = filepath.Join(inst.Path, "config", "Geyser-Fabric")
	case models.EngineNeoForge:
		platform = "neoforge"
		destDir = filepath.Join(inst.Path, "mods")
		configSubDir = filepath.Join(inst.Path, "config", "Geyser-NeoForge")
	default:
		return nil
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	progress("GeyserMC Cross-Play modulu indiriliyor...")
	geyserURL := fmt.Sprintf("https://download.geysermc.org/v2/projects/geyser/versions/latest/builds/latest/downloads/%s", platform)
	if err := downloadToFile(ctx, geyserURL, filepath.Join(destDir, "Geyser-"+platform+".jar")); err != nil {
		return err
	}

	if inst.CrossPlay.Floodgate {
		progress("Floodgate (Bedrock giris destegi) indiriliyor...")
		floodgateURL := fmt.Sprintf("https://download.geysermc.org/v2/projects/floodgate/versions/latest/builds/latest/downloads/%s", platform)
		if err := downloadToFile(ctx, floodgateURL, filepath.Join(destDir, "floodgate-"+platform+".jar")); err != nil {
			return err
		}
	}

	if inst.CrossPlay.ViaVersion && (inst.Engine == models.EnginePaper || inst.Engine == models.EnginePurpur) {
		progress("ViaVersion (coklu surum destegi) indiriliyor...")
		_ = installModrinthLatest(ctx, "viaversion", "paper", destDir)
	}

	// Pre-configure Geyser port & Floodgate auth
	if err := os.MkdirAll(configSubDir, 0o755); err == nil {
		cfgPath := filepath.Join(configSubDir, "config.yml")
		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			bedrockPort := inst.CrossPlay.BedrockPort
			if bedrockPort <= 0 {
				bedrockPort = 19132
			}
			authType := "online"
			if inst.CrossPlay.Floodgate {
				authType = "floodgate"
			}
			yml := fmt.Sprintf(`bedrock:
  address: 0.0.0.0
  port: %d
  clone-remote-port: false
  motd1: "%s"
  motd2: "MC SekaiLink Cross-Play"
remote:
  address: auto
  port: %d
  auth-type: %s
floodgate-key-file: key.pem
`, bedrockPort, inst.Name, inst.JavaPort, authType)
			_ = os.WriteFile(cfgPath, []byte(yml), 0o644)
		}
	}
	return nil
}

func installModrinthLatest(ctx context.Context, slug, loader, destDir string) error {
	var versions []models.ModrinthVersion
	u := fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version?loaders=%%5B%%22%s%%22%%5D", slug, loader)
	if err := getJSON(ctx, u, &versions); err != nil || len(versions) == 0 {
		return err
	}
	for _, f := range versions[0].Files {
		if f.Primary || strings.HasSuffix(strings.ToLower(f.Filename), ".jar") {
			return downloadToFile(ctx, f.URL, filepath.Join(destDir, f.Filename))
		}
	}
	return nil
}

func getJSON(ctx context.Context, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MC-SekaiLink/0.1.0 (https://github.com/MC_SekaiLink)")
	resp, err := catalogClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func downloadToFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MC-SekaiLink/0.1.0 (https://github.com/MC_SekaiLink)")
	resp, err := downloadClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download HTTP %d for %s", resp.StatusCode, url)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
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
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, destPath); err != nil {
		_ = os.Remove(destPath)
		return os.Rename(tmp, destPath)
	}
	return nil
}

func reverseStrings(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func limitStrings(s []string, max int) []string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func extractSortedPaperVersions(groups map[string][]string) []string {
	seen := make(map[string]bool)
	var stable []string
	var all []string
	for _, list := range groups {
		for _, v := range list {
			v = strings.TrimSpace(v)
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			all = append(all, v)
			lower := strings.ToLower(v)
			if !strings.Contains(lower, "-rc") &&
				!strings.Contains(lower, "-pre") &&
				!strings.Contains(lower, "-snapshot") &&
				!strings.Contains(lower, "-alpha") &&
				!strings.Contains(lower, "-beta") {
				stable = append(stable, v)
			}
		}
	}
	target := stable
	if len(target) == 0 {
		target = all
	}
	sort.Slice(target, func(i, j int) bool {
		return compareMCVersion(target[i], target[j]) > 0
	})
	return target
}

func compareMCVersion(a, b string) int {
	aBase, aSuffix, aHasSuffix := strings.Cut(a, "-")
	bBase, bSuffix, bHasSuffix := strings.Cut(b, "-")
	aParts := strings.Split(aBase, ".")
	bParts := strings.Split(bBase, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := 0; i < maxLen; i++ {
		var ai, bi int
		if i < len(aParts) {
			ai, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bi, _ = strconv.Atoi(bParts[i])
		}
		if ai != bi {
			if ai > bi {
				return 1
			}
			return -1
		}
	}
	if aHasSuffix != bHasSuffix {
		if !aHasSuffix {
			return 1
		}
		return -1
	}
	return strings.Compare(aSuffix, bSuffix)
}
