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
		props, _ := engine.ReadProperties(inst.Path)
		worldName := props["level-name"]
		if worldName == "" {
			worldName = inst.ActiveWorld
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

	// Bedrock BDS packs
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
		list = []models.InstalledAddon{}
	}
	return list, nil
}

func InstallUploadedAddon(inst models.ServerInstance, filename string, src io.Reader, kind string) (string, error) {
	safeName := filepath.Base(strings.TrimSpace(filename))
	lower := strings.ToLower(safeName)

	if strings.HasSuffix(lower, ".mcpack") || strings.HasSuffix(lower, ".mcaddon") {
		tmpFile, err := os.CreateTemp("", "sekailink-mcpack-*"+filepath.Ext(safeName))
		if err != nil {
			return "", err
		}
		tmpPath := tmpFile.Name()
		if _, err := io.Copy(tmpFile, src); err != nil {
			tmpFile.Close()
			_ = os.Remove(tmpPath)
			return "", err
		}
		tmpFile.Close()
		defer os.Remove(tmpPath)

		if inst.Engine == models.EngineBDS {
			if err := installBedrockArchive(inst, safeName, tmpPath); err != nil {
				return "", err
			}
			return safeName, nil
		}
		// GeyserMC accepts .mcpack directly inside its packs/ folder
		geyserDir := TargetDirForAddonKind(inst, "geyser-pack")
		if err := os.MkdirAll(geyserDir, 0o755); err != nil {
			return "", err
		}
		dest := filepath.Join(geyserDir, safeName)
		in, err := os.Open(tmpPath)
		if err != nil {
			return "", err
		}
		defer in.Close()
		out, err := os.Create(dest)
		if err != nil {
			return "", err
		}
		defer out.Close()
		if _, err := io.Copy(out, in); err != nil {
			return "", err
		}
		return safeName, nil
	}

	destDir := TargetDirForAddonKind(inst, kind)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	destPath := filepath.Join(destDir, safeName)
	out, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		return "", err
	}
	return safeName, nil
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

func installBedrockArchive(inst models.ServerInstance, filename, archivePath string) error {
	tmpDir, err := os.MkdirTemp("", "sekailink-bpack-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := worlds.ExtractZipSafe(archivePath, tmpDir); err != nil {
		return err
	}

	var manifests []string
	_ = filepath.WalkDir(tmpDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(d.Name(), "manifest.json") {
			manifests = append(manifests, path)
		}
		return nil
	})
	if len(manifests) == 0 {
		return fmt.Errorf("Bedrock manifest.json bulunamadi")
	}

	props, _ := engine.ReadProperties(inst.Path)
	worldName := props["level-name"]
	if worldName == "" {
		worldName = "Bedrock level"
	}

	for idx, mPath := range manifests {
		raw, err := os.ReadFile(mPath)
		if err != nil {
			continue
		}
		var mf bedrockManifest
		if err := json.Unmarshal(raw, &mf); err != nil || mf.Header.UUID == "" {
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
		folderBase := strings.TrimSuffix(strings.TrimSuffix(filename, ".mcaddon"), ".mcpack")
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
			return err
		}
		if err := copyPackDir(packRoot, destDir); err != nil {
			return err
		}
		_ = registerWorldPackJSON(filepath.Join(inst.Path, "worlds", worldName, jsonName), mf.Header.UUID, mf.Header.Version)
	}
	return nil
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
		if e.PackID == uuid {
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

func readPackUUID(manifestPath string) string {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return ""
	}
	var mf bedrockManifest
	if err := json.Unmarshal(raw, &mf); err != nil {
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
