package addons

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/engine"
	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
	"github.com/MC_SekaiLink/mc-sekailink/internal/worlds"
)

func TargetDirForAddonKind(inst models.ServerInstance, kind string) string {
	switch kind {
	case "datapack":
		var worldName string
		if _, err := os.Stat(filepath.Join(inst.Path, "server.properties")); err == nil {
			props, _ := engine.ReadProperties(inst.Path)
			worldName = strings.TrimSpace(props["level-name"])
		}
		if worldName == "" {
			worldName = strings.TrimSpace(inst.ActiveWorld)
		}
		if worldName == "" {
			worldName = "world"
		}
		return filepath.Join(inst.Path, worldName, "datapacks")
	case "resourcepack", "shader":
		return filepath.Join(inst.Path, "sekailink_packs")
	case "plugin":
		if inst.Engine == models.EngineFabric || inst.Engine == models.EngineNeoForge {
			return filepath.Join(inst.Path, "mods")
		}
		return filepath.Join(inst.Path, "plugins")
	case "mod":
		if inst.Engine == models.EnginePaper || inst.Engine == models.EnginePurpur || inst.Engine == models.EnginePowerNukkitX {
			return filepath.Join(inst.Path, "plugins")
		}
		return filepath.Join(inst.Path, "mods")
	case "geyser-pack":
		if inst.Engine == models.EngineFabric {
			return filepath.Join(inst.Path, "config", "Geyser-Fabric", "packs")
		}
		if inst.Engine == models.EngineNeoForge {
			return filepath.Join(inst.Path, "config", "Geyser-NeoForge", "packs")
		}
		return filepath.Join(inst.Path, "plugins", "Geyser-Spigot", "packs")
	default:
		if inst.Engine == models.EngineFabric || inst.Engine == models.EngineNeoForge {
			return filepath.Join(inst.Path, "mods")
		}
		return filepath.Join(inst.Path, "plugins")
	}
}

func ListInstalledAddons(inst models.ServerInstance) ([]models.InstalledAddon, error) {
	var list []models.InstalledAddon

	dirs := []struct {
		dir  string
		kind string
	}{
		{filepath.Join(inst.Path, "plugins"), "plugin"},
		{filepath.Join(inst.Path, "mods"), "mod"},
		{TargetDirForAddonKind(inst, "datapack"), "datapack"},
		{TargetDirForAddonKind(inst, "geyser-pack"), "geyser-pack"},
	}

	for _, d := range dirs {
		entries, err := os.ReadDir(d.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			lower := strings.ToLower(name)
			enabled := !strings.HasSuffix(lower, ".disabled")
			cleanLower := strings.TrimSuffix(lower, ".disabled")
			if !strings.HasSuffix(cleanLower, ".jar") &&
				!strings.HasSuffix(cleanLower, ".zip") &&
				!strings.HasSuffix(cleanLower, ".mcpack") {
				continue
			}
			info, _ := e.Info()
			var size int64
			modTime := time.Now()
			if info != nil {
				size = info.Size()
				modTime = info.ModTime()
			}
			rel, _ := filepath.Rel(inst.Path, filepath.Join(d.dir, name))
			list = append(list, models.InstalledAddon{
				Name:      name,
				Kind:      d.kind,
				Enabled:   enabled,
				SizeBytes: size,
				ModTime:   modTime,
				Path:      filepath.ToSlash(rel),
			})
		}
	}

	// Bedrock packs (BDS & PowerNukkitX)
	for _, pair := range []struct {
		dir  string
		kind string
	}{
		{filepath.Join(inst.Path, "resource_packs"), "bedrock-resource"},
		{filepath.Join(inst.Path, "behavior_packs"), "bedrock-behavior"},
	} {
		entries, err := os.ReadDir(pair.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			info, _ := e.Info()
			modTime := time.Now()
			if info != nil {
				modTime = info.ModTime()
			}
			rel, _ := filepath.Rel(inst.Path, filepath.Join(pair.dir, e.Name()))
			uuid := readPackUUID(filepath.Join(pair.dir, e.Name(), "manifest.json"))
			list = append(list, models.InstalledAddon{
				Name:      e.Name(),
				Kind:      pair.kind,
				Enabled:   true,
				SizeBytes: 0,
				ModTime:   modTime,
				Path:      filepath.ToSlash(rel),
				UUID:      uuid,
			})
		}
	}

	if list == nil {
		return []models.InstalledAddon{}, nil
	}
	return AnalyzeInstalledAddons(inst, list), nil
}

func InstallUploadedAddon(inst models.ServerInstance, filename string, src io.Reader, kind string) (string, error) {
	name, _, _, err := InstallUploadedAddonWithPath(inst, filename, src, kind)
	return name, err
}

func InstallUploadedAddonWithPath(inst models.ServerInstance, filename string, src io.Reader, kind string) (name string, installedPaths []string, effectiveKind string, err error) {
	safeName := filepath.Base(strings.TrimSpace(filename))
	lower := strings.ToLower(safeName)

	if strings.HasSuffix(lower, ".mcpack") || strings.HasSuffix(lower, ".mcaddon") {
		tmpFile, err := os.CreateTemp("", "sekailink-mcpack-*"+filepath.Ext(safeName))
		if err != nil {
			return "", nil, kind, err
		}
		tmpPath := tmpFile.Name()
		if _, err := io.Copy(tmpFile, src); err != nil {
			tmpFile.Close()
			_ = os.Remove(tmpPath)
			return "", nil, kind, err
		}
		tmpFile.Close()
		defer os.Remove(tmpPath)

		if inst.Engine == models.EngineBDS || inst.Engine == models.EnginePowerNukkitX {
			destDirs, err := installBedrockArchive(inst, safeName, tmpPath, inst.Engine == models.EngineBDS)
			if err != nil {
				return "", nil, "bedrock-resource", err
			}
			return safeName, destDirs, "bedrock-resource", nil
		}
		// Java engines with GeyserMC accept .mcpack directly inside Geyser's packs/ folder
		geyserDir := TargetDirForAddonKind(inst, "geyser-pack")
		if err := os.MkdirAll(geyserDir, 0o755); err != nil {
			return "", nil, "geyser-pack", err
		}
		dest := filepath.Join(geyserDir, safeName)
		in, err := os.Open(tmpPath)
		if err != nil {
			return "", nil, "geyser-pack", err
		}
		defer in.Close()
		out, err := os.Create(dest)
		if err != nil {
			return "", nil, "geyser-pack", err
		}
		defer out.Close()
		if _, err := io.Copy(out, in); err != nil {
			return "", nil, "geyser-pack", err
		}
		return safeName, []string{dest}, "geyser-pack", nil
	}

	if inst.Engine == models.EngineBDS {
		return "", nil, kind, fmt.Errorf("resmi Bedrock (BDS) yalnizca .mcpack veya .mcaddon dosyalarini destekler")
	}
	if inst.Engine == models.EngineVanilla {
		if strings.HasSuffix(lower, ".jar") {
			return "", nil, kind, fmt.Errorf("Vanilla sunucu .jar mod veya eklenti desteklemez; yalnizca datapack (.zip) yukleyebilirsiniz")
		}
		effectiveKind = "datapack"
	} else if strings.HasSuffix(lower, ".jar") && (kind == "" || kind == "datapack") {
		effectiveKind = normalizeTargetKind(inst.Engine, "")
	} else {
		effectiveKind = normalizeTargetKind(inst.Engine, kind)
	}

	destDir := TargetDirForAddonKind(inst, effectiveKind)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", nil, effectiveKind, err
	}
	destPath := filepath.Join(destDir, safeName)
	out, err := os.Create(destPath)
	if err != nil {
		return "", nil, effectiveKind, err
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		return "", nil, effectiveKind, err
	}
	return safeName, []string{destPath}, effectiveKind, nil
}

func ToggleAddon(inst models.ServerInstance, relPath string) (bool, error) {
	target, err := resolveInsideServer(inst.Path, relPath)
	if err != nil {
		return false, err
	}
	if strings.HasSuffix(target, ".disabled") {
		enabledPath := strings.TrimSuffix(target, ".disabled")
		return true, os.Rename(target, enabledPath)
	}
	disabledPath := target + ".disabled"
	return false, os.Rename(target, disabledPath)
}

func DeleteAddon(inst models.ServerInstance, relPath string) error {
	target, err := resolveInsideServer(inst.Path, relPath)
	if err != nil {
		return err
	}
	slashRel := filepath.ToSlash(relPath)
	if strings.HasPrefix(slashRel, "resource_packs/") || strings.HasPrefix(slashRel, "behavior_packs/") {
		if uuid := readPackUUID(filepath.Join(target, "manifest.json")); uuid != "" {
			worldName := resolveBedrockWorldName(inst)
			jsonName := "world_resource_packs.json"
			if strings.HasPrefix(slashRel, "behavior_packs/") {
				jsonName = "world_behavior_packs.json"
			}
			_ = unregisterWorldPackJSON(filepath.Join(inst.Path, "worlds", worldName, jsonName), uuid)
		}
	}
	return os.RemoveAll(target)
}

func resolveInsideServer(serverRoot, relPath string) (string, error) {
	cleanRoot := filepath.Clean(serverRoot)
	full := filepath.Clean(filepath.Join(cleanRoot, filepath.FromSlash(relPath)))
	if !strings.HasPrefix(full, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("gecersiz eklenti yolu")
	}
	return full, nil
}

type bedrockManifest struct {
	Header struct {
		Name    string `json:"name"`
		UUID    string `json:"uuid"`
		Version []int  `json:"version"`
	} `json:"header"`
	Modules []struct {
		Type string `json:"type"`
	} `json:"modules"`
}

type bedrockWorldPackEntry struct {
	PackID  string `json:"pack_id"`
	Version []int  `json:"version"`
}

func resolveBedrockWorldName(inst models.ServerInstance) string {
	if _, err := os.Stat(filepath.Join(inst.Path, "server.properties")); err == nil {
		if props, err := engine.ReadProperties(inst.Path); err == nil {
			if worldName := strings.TrimSpace(props["level-name"]); worldName != "" {
				return worldName
			}
		}
	}
	if w := strings.TrimSpace(inst.ActiveWorld); w != "" {
		return w
	}
	return "Bedrock level"
}

func trimBedrockArchiveExt(filename string) string {
	lower := strings.ToLower(filename)
	if strings.HasSuffix(lower, ".mcaddon") {
		return filename[:len(filename)-len(".mcaddon")]
	}
	if strings.HasSuffix(lower, ".mcpack") {
		return filename[:len(filename)-len(".mcpack")]
	}
	if strings.HasSuffix(lower, ".zip") {
		return filename[:len(filename)-len(".zip")]
	}
	return filename
}

func installBedrockArchive(inst models.ServerInstance, filename, archivePath string, registerWorldJSON bool) ([]string, error) {
	tmpDir, err := os.MkdirTemp("", "sekailink-bpack-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	if err := worlds.ExtractZipSafe(archivePath, tmpDir); err != nil {
		return nil, err
	}

	// Many .mcaddon bundles contain nested .mcpack or .zip archives instead of unzipped folders.
	var nestedArchives []string
	_ = filepath.WalkDir(tmpDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		lower := strings.ToLower(d.Name())
		if strings.HasSuffix(lower, ".mcpack") || strings.HasSuffix(lower, ".zip") {
			nestedArchives = append(nestedArchives, path)
		}
		return nil
	})
	for idx, nested := range nestedArchives {
		subDir := filepath.Join(tmpDir, fmt.Sprintf("__nested_%d", idx))
		if err := os.MkdirAll(subDir, 0o755); err == nil {
			_ = worlds.ExtractZipSafe(nested, subDir)
		}
	}

	var manifests []string
	_ = filepath.WalkDir(tmpDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(d.Name(), "manifest.json") {
			manifests = append(manifests, path)
		}
		return nil
	})
	if len(manifests) == 0 {
		return nil, fmt.Errorf("Bedrock manifest.json bulunamadi")
	}

	worldName := resolveBedrockWorldName(inst)
	var destDirs []string

	for idx, mPath := range manifests {
		raw, err := os.ReadFile(mPath)
		if err != nil {
			continue
		}
		var mf bedrockManifest
		if err := json.Unmarshal(stripJSONCommentsAndBOM(raw), &mf); err != nil || mf.Header.UUID == "" {
			continue
		}
		packKind := "resource"
		for _, mod := range mf.Modules {
			if mod.Type == "data" || mod.Type == "script" {
				packKind = "behavior"
				break
			}
		}
		packRoot := filepath.Dir(mPath)
		folderBase := trimBedrockArchiveExt(filename)
		if len(manifests) > 1 {
			folderBase = fmt.Sprintf("%s_%d", folderBase, idx+1)
		}
		subDir := "resource_packs"
		jsonName := "world_resource_packs.json"
		if packKind == "behavior" {
			subDir = "behavior_packs"
			jsonName = "world_behavior_packs.json"
		}
		destDir := filepath.Join(inst.Path, subDir, folderBase)
		_ = os.RemoveAll(destDir)
		if err := os.MkdirAll(filepath.Dir(destDir), 0o755); err != nil {
			return nil, err
		}
		if err := copyPackDir(packRoot, destDir); err != nil {
			return nil, err
		}
		destDirs = append(destDirs, destDir)
		if registerWorldJSON {
			_ = registerWorldPackJSON(filepath.Join(inst.Path, "worlds", worldName, jsonName), mf.Header.UUID, mf.Header.Version)
		}
	}
	if len(destDirs) == 0 {
		return nil, fmt.Errorf("gecerli Bedrock paketi (header.uuid) bulunamadi")
	}
	return destDirs, nil
}

func registerWorldPackJSON(jsonPath, uuid string, version []int) error {
	if len(version) == 0 {
		version = []int{1, 0, 0}
	}
	var entries []bedrockWorldPackEntry
	if raw, err := os.ReadFile(jsonPath); err == nil {
		_ = json.Unmarshal(raw, &entries)
	}
	for _, e := range entries {
		if strings.EqualFold(e.PackID, uuid) {
			return nil
		}
	}
	entries = append(entries, bedrockWorldPackEntry{
		PackID:  uuid,
		Version: version,
	})
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(jsonPath, out, 0o644)
}

func unregisterWorldPackJSON(jsonPath, uuid string) error {
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil
	}
	var entries []bedrockWorldPackEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return err
	}
	filtered := make([]bedrockWorldPackEntry, 0, len(entries))
	for _, e := range entries {
		if !strings.EqualFold(e.PackID, uuid) {
			filtered = append(filtered, e)
		}
	}
	out, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(jsonPath, out, 0o644)
}

func readPackUUID(manifestPath string) string {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return ""
	}
	var mf bedrockManifest
	if err := json.Unmarshal(stripJSONCommentsAndBOM(raw), &mf); err != nil {
		return ""
	}
	return mf.Header.UUID
}

func copyPackDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
