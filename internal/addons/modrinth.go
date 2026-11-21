package addons

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

const fabricAPIProjectID = "P7dR8mSH"

var modrinthClient = &http.Client{Timeout: 30 * time.Second}

func SearchModrinth(ctx context.Context, query, projectType, loader, mcVersion string, limit int) (models.ModrinthSearchResult, error) {
	if limit <= 0 || limit > 40 {
		limit = 20
	}
	var facets [][]string
	if projectType != "" {
		facets = append(facets, []string{"project_type:" + projectType})
	}
	if projectType == "mod" || projectType == "" {
		// Exclude pure client-side mods (e.g. client hotkey companions) from dedicated server search.
		facets = append(facets, []string{"server_side:required", "server_side:optional"})
	}
	if loader != "" {
		facets = append(facets, []string{"categories:" + loader})
	}
	if mcVersion != "" && mcVersion != "latest" {
		facets = append(facets, []string{"versions:" + mcVersion})
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", fmt.Sprintf("%d", limit))
	if len(facets) > 0 {
		b, _ := json.Marshal(facets)
		params.Set("facets", string(b))
	}

	u := "https://api.modrinth.com/v2/search?" + params.Encode()
	var res models.ModrinthSearchResult
	if err := getModrinthJSON(ctx, u, &res); err != nil {
		return res, err
	}
	return res, nil
}

func ListProjectVersions(ctx context.Context, projectID, loader, mcVersion string) ([]models.ModrinthVersion, error) {
	params := url.Values{}
	if loader != "" {
		b, _ := json.Marshal([]string{loader})
		params.Set("loaders", string(b))
	}
	if mcVersion != "" && mcVersion != "latest" {
		b, _ := json.Marshal([]string{mcVersion})
		params.Set("game_versions", string(b))
	}
	u := fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version", url.PathEscape(projectID))
	if encoded := params.Encode(); encoded != "" {
		u += "?" + encoded
	}
	var versions []models.ModrinthVersion
	if err := getModrinthJSON(ctx, u, &versions); err != nil {
		return nil, err
	}
	return versions, nil
}

func InstallModrinthProject(ctx context.Context, inst models.ServerInstance, projectID, versionID, targetKind string) ([]string, error) {
	if targetKind == "plugin" && (inst.Engine == models.EngineFabric || inst.Engine == models.EngineNeoForge) {
		targetKind = "mod"
	}
	loader := LoaderForEngine(inst.Engine, targetKind)
	visited := make(map[string]bool)
	return installModrinthRecursive(ctx, inst, projectID, versionID, targetKind, loader, visited, 0)
}

func installModrinthRecursive(ctx context.Context, inst models.ServerInstance, projectID, versionID, targetKind, loader string, visited map[string]bool, depth int) ([]string, error) {
	if depth > 4 {
		return nil, nil
	}
	key := projectID + ":" + versionID
	if visited[key] || (projectID != "" && visited[projectID]) {
		return nil, nil
	}
	visited[key] = true
	if projectID != "" {
		visited[projectID] = true
	}

	var chosen models.ModrinthVersion
	if versionID != "" {
		u := fmt.Sprintf("https://api.modrinth.com/v2/version/%s", url.PathEscape(versionID))
		if err := getModrinthJSON(ctx, u, &chosen); err != nil {
			return nil, err
		}
	} else {
		versions, err := ListProjectVersions(ctx, projectID, loader, inst.MCVersion)
		var ok bool
		if err == nil && len(versions) > 0 {
			chosen, ok = pickCompatibleVersion(versions, inst.MCVersion)
		}
		if !ok && inst.MCVersion != "" && inst.MCVersion != "latest" {
			allVersions, allErr := ListProjectVersions(ctx, projectID, loader, "")
			if allErr != nil && err == nil {
				err = allErr
			}
			if allErr == nil && len(allVersions) > 0 {
				chosen, ok = pickCompatibleVersion(allVersions, inst.MCVersion)
			}
		}
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("bu mod/eklenti Minecraft %s (%s) surumuyle uyumlu degil", inst.MCVersion, loader)
		}
	}

	var primaryFile *models.ModrinthFile
	for i := range chosen.Files {
		if chosen.Files[i].Primary {
			primaryFile = &chosen.Files[i]
			break
		}
	}
	if primaryFile == nil && len(chosen.Files) > 0 {
		primaryFile = &chosen.Files[0]
	}
	if primaryFile == nil {
		return nil, fmt.Errorf("indirilecek dosya bulunamadi")
	}

	destDir := TargetDirForAddonKind(inst, targetKind)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	destPath := filepath.Join(destDir, filepath.Base(primaryFile.Filename))
	if err := downloadModrinthFile(ctx, primaryFile.URL, destPath); err != nil {
		return nil, err
	}
	installed := []string{filepath.Base(primaryFile.Filename)}

	for _, dep := range chosen.Dependencies {
		if dep.DependencyType == "required" && dep.ProjectID != "" {
			sub, depErr := installModrinthRecursive(ctx, inst, dep.ProjectID, dep.VersionID, targetKind, loader, visited, depth+1)
			if depErr == nil {
				installed = append(installed, sub...)
			}
		}
	}

	// Many Fabric mods (and datapack-wrapped Fabric JARs) declare fabric-* module dependencies
	// in fabric.mod.json without listing fabric-api in Modrinth's API dependency graph.
	if inst.Engine == models.EngineFabric &&
		(targetKind == "mod" || loader == "fabric") &&
		projectID != fabricAPIProjectID &&
		jarRequiresFabricAPI(destPath) &&
		!hasFabricAPIInstalled(destDir) {
		if sub, depErr := installModrinthRecursive(ctx, inst, fabricAPIProjectID, "", "mod", "fabric", visited, depth+1); depErr == nil {
			installed = append(installed, sub...)
		}
	}

	return installed, nil
}

// EnsureFabricAPIForJar checks if an uploaded Fabric JAR declares fabric-* dependencies
// in fabric.mod.json and installs matching Fabric API into mods/ if missing.
func EnsureFabricAPIForJar(ctx context.Context, inst models.ServerInstance, jarPath string) ([]string, error) {
	if inst.Engine != models.EngineFabric {
		return nil, nil
	}
	if !strings.HasSuffix(strings.ToLower(jarPath), ".jar") || !jarRequiresFabricAPI(jarPath) {
		return nil, nil
	}
	modsDir := TargetDirForAddonKind(inst, "mod")
	if hasFabricAPIInstalled(modsDir) {
		return nil, nil
	}
	visited := make(map[string]bool)
	return installModrinthRecursive(ctx, inst, fabricAPIProjectID, "", "mod", "fabric", visited, 0)
}

func pickCompatibleVersion(versions []models.ModrinthVersion, mcVersion string) (models.ModrinthVersion, bool) {
	if len(versions) == 0 {
		return models.ModrinthVersion{}, false
	}
	target := strings.TrimSpace(mcVersion)
	if target == "" || strings.EqualFold(target, "latest") {
		for _, v := range versions {
			if strings.EqualFold(v.VersionType, "release") {
				return v, true
			}
		}
		return versions[0], true
	}

	// 1. Exact game_version match (prefer release over beta/alpha).
	var exactFallback *models.ModrinthVersion
	for i := range versions {
		v := &versions[i]
		for _, gv := range v.GameVersions {
			if strings.EqualFold(strings.TrimSpace(gv), target) {
				if strings.EqualFold(v.VersionType, "release") {
					return *v, true
				}
				if exactFallback == nil {
					exactFallback = v
				}
				break
			}
		}
	}
	if exactFallback != nil {
		return *exactFallback, true
	}

	// 2. Same minor series match (e.g. "26.2" matches "26.2.1"; "1.21.4" matches "1.21.11").
	series := mcVersionSeriesPrefix(target)
	if series == "" {
		return models.ModrinthVersion{}, false
	}
	var seriesFallback *models.ModrinthVersion
	for i := range versions {
		v := &versions[i]
		for _, gv := range v.GameVersions {
			if sameMCSeries(gv, target, series) {
				if strings.EqualFold(v.VersionType, "release") {
					return *v, true
				}
				if seriesFallback == nil {
					seriesFallback = v
				}
				break
			}
		}
	}
	if seriesFallback != nil {
		return *seriesFallback, true
	}
	return models.ModrinthVersion{}, false
}

func mcVersionSeriesPrefix(ver string) string {
	base, _, _ := strings.Cut(strings.TrimSpace(ver), "-")
	parts := strings.Split(base, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}

func sameMCSeries(candidate, target, targetSeries string) bool {
	cBase, _, _ := strings.Cut(strings.TrimSpace(candidate), "-")
	if strings.EqualFold(cBase, target) {
		return true
	}
	if cBase == targetSeries || strings.HasPrefix(cBase, targetSeries+".") {
		return true
	}
	return false
}

type fabricModMetadata struct {
	ID      string         `json:"id"`
	Depends map[string]any `json:"depends"`
}

func readFabricModMetadata(jarPath string) (*fabricModMetadata, bool) {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil, false
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == "fabric.mod.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, false
			}
			raw, readErr := io.ReadAll(io.LimitReader(rc, 512*1024))
			rc.Close()
			if readErr != nil {
				return nil, false
			}
			var meta fabricModMetadata
			if err := json.Unmarshal(raw, &meta); err != nil {
				return nil, false
			}
			return &meta, true
		}
	}
	return nil, false
}

func jarRequiresFabricAPI(jarPath string) bool {
	meta, ok := readFabricModMetadata(jarPath)
	if !ok {
		return false
	}
	if strings.EqualFold(meta.ID, "fabric-api") || strings.EqualFold(meta.ID, "fabric") {
		return false
	}
	for depID := range meta.Depends {
		lower := strings.ToLower(strings.TrimSpace(depID))
		if lower == "fabric" || lower == "fabric-api" || strings.HasPrefix(lower, "fabric-") {
			return true
		}
	}
	return false
}

func hasFabricAPIInstalled(modsDir string) bool {
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if !strings.HasSuffix(name, ".jar") {
			continue
		}
		if strings.HasPrefix(name, "fabric-api") {
			return true
		}
		if meta, ok := readFabricModMetadata(filepath.Join(modsDir, e.Name())); ok {
			id := strings.ToLower(strings.TrimSpace(meta.ID))
			if id == "fabric-api" || id == "fabric" {
				return true
			}
		}
	}
	return false
}

func LoaderForEngine(eng models.EngineType, targetKind string) string {
	switch targetKind {
	case "datapack":
		return "datapack"
	case "resourcepack":
		return "minecraft"
	case "shader":
		return "iris"
	}
	switch eng {
	case models.EnginePaper, models.EnginePurpur:
		return "paper"
	case models.EngineFabric:
		return "fabric"
	case models.EngineNeoForge:
		return "neoforge"
	default:
		return ""
	}
}

func getModrinthJSON(ctx context.Context, u string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MC-SekaiLink/0.1.0 (https://github.com/MC_SekaiLink)")
	resp, err := modrinthClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("Modrinth API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func downloadModrinthFile(ctx context.Context, u, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MC-SekaiLink/0.1.0 (https://github.com/MC_SekaiLink)")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed HTTP %d", resp.StatusCode)
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
	if err := os.Rename(tmp, destPath); err != nil {
		_ = os.Remove(destPath)
		return os.Rename(tmp, destPath)
	}
	return nil
}
