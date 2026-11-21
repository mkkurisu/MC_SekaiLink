package addons

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/engine"
	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func ComputeFileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ListPacks(inst models.ServerInstance) ([]models.PackInfo, error) {
	packsDir := filepath.Join(inst.Path, "sekailink_packs")
	entries, err := os.ReadDir(packsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.PackInfo{}, nil
		}
		return nil, err
	}
	props, _ := engine.ReadProperties(inst.Path)
	activeSHA1 := strings.ToLower(strings.TrimSpace(props["resource-pack-sha1"]))
	activeURL := strings.TrimSpace(props["resource-pack"])

	out := make([]models.PackInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".zip") && !strings.HasSuffix(lower, ".mcpack") {
			continue
		}
		full := filepath.Join(packsDir, name)
		sum, _ := ComputeFileSHA1(full)
		info, _ := e.Info()
		size := int64(0)
		modTime := time.Now()
		if info != nil {
			size = info.Size()
			modTime = info.ModTime()
		}
		kind := "java-resource"
		if strings.HasSuffix(lower, ".mcpack") {
			kind = "bedrock-pack"
		} else if strings.Contains(lower, "shader") {
			kind = "shader-client"
		}
		urlPath := fmt.Sprintf("/packs/%s/%s", url.PathEscape(inst.ID), url.PathEscape(name))
		applied := (activeSHA1 != "" && strings.EqualFold(activeSHA1, sum)) || (activeURL != "" && strings.HasSuffix(activeURL, urlPath))

		out = append(out, models.PackInfo{
			Filename:  name,
			Kind:      kind,
			SHA1:      sum,
			SizeBytes: size,
			URLPath:   urlPath,
			Applied:   applied,
			ModTime:   modTime,
		})
	}
	return out, nil
}

func SavePackFile(inst models.ServerInstance, filename string, src io.Reader) (models.PackInfo, error) {
	safeName := filepath.Base(strings.TrimSpace(filename))
	packsDir := filepath.Join(inst.Path, "sekailink_packs")
	if err := os.MkdirAll(packsDir, 0o755); err != nil {
		return models.PackInfo{}, err
	}
	dest := filepath.Join(packsDir, safeName)
	out, err := os.Create(dest)
	if err != nil {
		return models.PackInfo{}, err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return models.PackInfo{}, err
	}
	out.Close()

	sum, err := ComputeFileSHA1(dest)
	if err != nil {
		return models.PackInfo{}, err
	}
	info, _ := os.Stat(dest)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	return models.PackInfo{
		Filename:  safeName,
		Kind:      "java-resource",
		SHA1:      sum,
		SizeBytes: size,
		URLPath:   fmt.Sprintf("/packs/%s/%s", url.PathEscape(inst.ID), url.PathEscape(safeName)),
		ModTime:   time.Now(),
	}, nil
}

func ApplyResourcePack(inst models.ServerInstance, filename, baseURL string, required bool) (map[string]string, error) {
	safeName := filepath.Base(strings.TrimSpace(filename))
	full := filepath.Join(inst.Path, "sekailink_packs", safeName)
	if _, err := os.Stat(full); err != nil {
		return nil, fmt.Errorf("paket bulunamadi: %s", safeName)
	}
	sum, err := ComputeFileSHA1(full)
	if err != nil {
		return nil, err
	}
	baseURL = strings.TrimRight(baseURL, "/")
	packURL := fmt.Sprintf("%s/packs/%s/%s", baseURL, url.PathEscape(inst.ID), url.PathEscape(safeName))

	props, err := engine.ReadProperties(inst.Path)
	if err != nil {
		return nil, err
	}
	props["resource-pack"] = packURL
	props["resource-pack-sha1"] = sum
	if required {
		props["require-resource-pack"] = "true"
	} else {
		props["require-resource-pack"] = "false"
	}
	if err := engine.WriteProperties(inst.Path, props); err != nil {
		return nil, err
	}
	return props, nil
}

// ExportClientMRPack writes a lightweight Modrinth .mrpack manifest listing client-compatible mods & shader packs.
func ExportClientMRPack(inst models.ServerInstance, destZip string) error {
	if err := os.MkdirAll(filepath.Dir(destZip), 0o755); err != nil {
		return err
	}
	f, err := os.Create(destZip)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()

	deps := map[string]string{
		"minecraft": EffectiveMCVersion(inst.Engine, inst.MCVersion),
	}
	if inst.Engine == models.EngineFabric {
		deps["fabric-loader"] = "0.16.9"
	} else if inst.Engine == models.EngineNeoForge {
		deps["neoforge"] = inst.MCVersion
	}

	manifest := map[string]any{
		"formatVersion": 1,
		"game":          "minecraft",
		"versionId":     "1.0.0",
		"name":          inst.Name + " Client Pack",
		"summary":       "MC SekaiLink tarafindan olusturulan istemci paketi",
		"files":         []any{},
		"dependencies":  deps,
	}
	raw, _ := json.MarshalIndent(manifest, "", "  ")
	w, err := zw.Create("modrinth.index.json")
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}

	// Include shader/resource packs in overrides/
	packsDir := filepath.Join(inst.Path, "sekailink_packs")
	if entries, err := os.ReadDir(packsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			lower := strings.ToLower(e.Name())
			sub := "overrides/resourcepacks/"
			if strings.Contains(lower, "shader") {
				sub = "overrides/shaderpacks/"
			}
			fw, err := zw.Create(sub + e.Name())
			if err != nil {
				continue
			}
			in, err := os.Open(filepath.Join(packsDir, e.Name()))
			if err != nil {
				continue
			}
			_, _ = io.Copy(fw, in)
			in.Close()
		}
	}
	return nil
}
