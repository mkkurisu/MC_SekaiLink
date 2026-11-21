package addons

import (
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

var modrinthClient = &http.Client{Timeout: 30 * time.Second}

func SearchModrinth(ctx context.Context, query, projectType, loader, mcVersion string, limit int) (models.ModrinthSearchResult, error) {
	if limit <= 0 || limit > 40 {
		limit = 20
	}
	var facets [][]string
	if projectType != "" {
		facets = append(facets, []string{"project_type:" + projectType})
	}
	if loader != "" {
		facets = append(facets, []string{"categories:" + loader})
	}
	if mcVersion != "" {
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
	if mcVersion != "" {
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
	loader := LoaderForEngine(inst.Engine, targetKind)
	visited := make(map[string]bool)
	return installModrinthRecursive(ctx, inst, projectID, versionID, targetKind, loader, visited, 0)
}

func installModrinthRecursive(ctx context.Context, inst models.ServerInstance, projectID, versionID, targetKind, loader string, visited map[string]bool, depth int) ([]string, error) {
	if depth > 4 {
		return nil, nil
	}
	key := projectID + ":" + versionID
	if visited[key] {
		return nil, nil
	}
	visited[key] = true

	var chosen models.ModrinthVersion
	if versionID != "" {
		u := fmt.Sprintf("https://api.modrinth.com/v2/version/%s", url.PathEscape(versionID))
		if err := getModrinthJSON(ctx, u, &chosen); err != nil {
			return nil, err
		}
	} else {
		versions, err := ListProjectVersions(ctx, projectID, loader, inst.MCVersion)
		if err != nil || len(versions) == 0 {
			// Fallback without exact MC version filter
			versions, err = ListProjectVersions(ctx, projectID, loader, "")
		}
		if err != nil {
			return nil, err
		}
		if len(versions) == 0 {
			return nil, fmt.Errorf("bu motor (%s) ve surum (%s) icin uyumlu dosya bulunamadi", loader, inst.MCVersion)
		}
		chosen = versions[0]
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
			sub, err := installModrinthRecursive(ctx, inst, dep.ProjectID, dep.VersionID, targetKind, loader, visited, depth+1)
			if err == nil {
				installed = append(installed, sub...)
			}
		}
	}
	return installed, nil
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
