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

const fabricAPIProjectID = "P7dR8mSH"

var modrinthClient = &http.Client{Timeout: 30 * time.Second}

// LoadersForEngine returns ordered Modrinth loader identifiers supported by the engine and addon kind.
func LoadersForEngine(eng models.EngineType, targetKind string) []string {
	switch strings.ToLower(strings.TrimSpace(targetKind)) {
	case "datapack":
		return []string{"datapack"}
	case "resourcepack":
		return []string{"minecraft"}
	case "shader":
		return []string{"iris"}
	}
	switch eng {
	case models.EnginePaper:
		return []string{"paper", "spigot", "bukkit", "purpur"}
	case models.EnginePurpur:
		return []string{"purpur", "paper", "spigot", "bukkit"}
	case models.EngineFabric:
		return []string{"fabric"}
	case models.EngineNeoForge:
		return []string{"neoforge"}
	default:
		return nil
	}
}

// LoaderForEngine returns the primary Modrinth loader string for backward-compatible callers.
func LoaderForEngine(eng models.EngineType, targetKind string) string {
	loaders := LoadersForEngine(eng, targetKind)
	if len(loaders) == 0 {
		return ""
	}
	return loaders[0]
}

// ExpandLoaderFilter expands a primary loader or comma-separated loader list into compatible Modrinth loaders.
func ExpandLoaderFilter(loader string) []string {
	raw := strings.TrimSpace(loader)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) == 1 {
		switch strings.ToLower(strings.TrimSpace(parts[0])) {
		case "paper":
			return []string{"paper", "spigot", "bukkit", "purpur"}
		case "purpur":
			return []string{"purpur", "paper", "spigot", "bukkit"}
		}
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		l := strings.ToLower(strings.TrimSpace(p))
		if l != "" {
			out = appendUnique(out, l)
		}
	}
	return out
}

func SearchModrinth(ctx context.Context, query, projectType, loader, mcVersion string, limit int) (models.ModrinthSearchResult, error) {
	if limit <= 0 || limit > 40 {
		limit = 20
	}
	pt := strings.ToLower(strings.TrimSpace(projectType))
	var facets [][]string
	if pt != "" {
		facets = append(facets, []string{"project_type:" + pt})
	}
	if pt == "mod" {
		// Exclude pure client-side mods (e.g. client hotkey companions) from dedicated server mod search.
		facets = append(facets, []string{"server_side:required", "server_side:optional"})
	}
	loaders := ExpandLoaderFilter(loader)
	if len(loaders) > 0 {
		catFacet := make([]string, 0, len(loaders))
		for _, l := range loaders {
			catFacet = append(catFacet, "categories:"+l)
		}
		facets = append(facets, catFacet)
	}
	effectiveVer := normalizeMCVersionForLoaders(mcVersion, loaders)
	if effectiveVer != "" && !strings.EqualFold(effectiveVer, "latest") {
		facets = append(facets, []string{"versions:" + effectiveVer})
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

func normalizeMCVersionForLoaders(mcVersion string, loaders []string) string {
	ver := strings.TrimSpace(mcVersion)
	if ver == "" || strings.EqualFold(ver, "latest") {
		return ver
	}
	for _, l := range loaders {
		if strings.EqualFold(l, "neoforge") {
			return EffectiveMCVersion(models.EngineNeoForge, ver)
		}
	}
	return ver
}

func ListProjectVersions(ctx context.Context, projectID string, loaders []string, mcVersion string) ([]models.ModrinthVersion, error) {
	params := url.Values{}
	if len(loaders) > 0 {
		b, _ := json.Marshal(loaders)
		params.Set("loaders", string(b))
	}
	effectiveVer := normalizeMCVersionForLoaders(mcVersion, loaders)
	if effectiveVer != "" && !strings.EqualFold(effectiveVer, "latest") {
		b, _ := json.Marshal([]string{effectiveVer})
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

func InstallModrinthProject(ctx context.Context, inst models.ServerInstance, projectID, versionID, targetKind string) (models.AddonInstallResult, error) {
	var res models.AddonInstallResult

	switch inst.Engine {
	case models.EngineBDS:
		return res, fmt.Errorf("resmi Bedrock (BDS) Modrinth Java magazasini desteklemez; .mcpack / .mcaddon dosyasi yukleyin")
	case models.EnginePowerNukkitX:
		return res, fmt.Errorf("PowerNukkitX Java Modrinth eklentilerini calistirmaz; PNX eklentisi (.jar) veya .mcpack yukleyin")
	case models.EngineVanilla:
		if strings.ToLower(strings.TrimSpace(targetKind)) != "datapack" {
			return res, fmt.Errorf("Vanilla sunucular Modrinth'ten yalnizca Datapack (.zip) kurulumunu destekler")
		}
	}

	normalizedKind := normalizeTargetKind(inst.Engine, targetKind)
	loaders := LoadersForEngine(inst.Engine, normalizedKind)
	mcVer := EffectiveMCVersion(inst.Engine, inst.MCVersion)
	visited := make(map[string]bool)

	installedFiles, installedPaths, warnings, missingDeps, err := installModrinthRecursive(
		ctx, inst, projectID, versionID, normalizedKind, loaders, mcVer, visited, 0,
	)
	if err != nil {
		return res, err
	}
	if len(installedFiles) > 0 {
		res.Installed = []string{installedFiles[0]}
		if len(installedFiles) > 1 {
			res.Dependencies = append(res.Dependencies, installedFiles[1:]...)
		}
	}
	res.MissingDependencies = append(res.MissingDependencies, missingDeps...)
	res.Warnings = append(res.Warnings, warnings...)

	resolveManifestDependencies(ctx, inst, installedPaths, normalizedKind, loaders, mcVer, visited, &res)
	collectPostInstallHealth(inst, installedPaths, &res)
	return res, nil
}

// ResolveUploadedAddonDependencies inspects an uploaded addon archive, attempts to auto-install any missing
// manifest-declared dependencies via Modrinth, and returns unresolved dependencies and engine warnings.
func ResolveUploadedAddonDependencies(ctx context.Context, inst models.ServerInstance, destPath, kind string) models.AddonInstallResult {
	var res models.AddonInstallResult
	if destPath == "" {
		return res
	}
	res.Installed = []string{filepath.Base(destPath)}
	normalizedKind := normalizeTargetKind(inst.Engine, kind)
	if normalizedKind == "datapack" || normalizedKind == "geyser-pack" {
		collectPostInstallHealth(inst, []string{destPath}, &res)
		return res
	}
	loaders := LoadersForEngine(inst.Engine, normalizedKind)
	mcVer := EffectiveMCVersion(inst.Engine, inst.MCVersion)
	visited := make(map[string]bool)

	installedPaths := []string{destPath}
	if len(loaders) > 0 && inst.Engine != models.EngineVanilla && inst.Engine != models.EngineBDS && inst.Engine != models.EnginePowerNukkitX {
		resolveManifestDependencies(ctx, inst, installedPaths, normalizedKind, loaders, mcVer, visited, &res)
	}
	collectPostInstallHealth(inst, installedPaths, &res)
	return res
}

// EnsureFabricAPIForJar checks if an uploaded Fabric JAR declares fabric-* module dependencies
// and installs matching Fabric API into mods/ if missing.
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
	res := ResolveUploadedAddonDependencies(ctx, inst, jarPath, "mod")
	return res.Dependencies, nil
}

func normalizeTargetKind(eng models.EngineType, kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch k {
	case "datapack", "resourcepack", "shader", "geyser-pack":
		return k
	}
	switch eng {
	case models.EnginePaper, models.EnginePurpur, models.EnginePowerNukkitX:
		return "plugin"
	case models.EngineFabric, models.EngineNeoForge:
		return "mod"
	case models.EngineVanilla:
		return "datapack"
	default:
		return k
	}
}

func installModrinthRecursive(
	ctx context.Context,
	inst models.ServerInstance,
	projectID string,
	versionID string,
	targetKind string,
	loaders []string,
	mcVer string,
	visited map[string]bool,
	depth int,
) (installed []string, installedPaths []string, warnings []string, missingDeps []string, err error) {
	if depth > 4 {
		return nil, nil, nil, nil, nil
	}
	key := projectID + ":" + versionID
	if visited[key] || (projectID != "" && visited[projectID]) {
		return nil, nil, nil, nil, nil
	}
	visited[key] = true
	if projectID != "" {
		visited[projectID] = true
	}

	var chosen models.ModrinthVersion
	if versionID != "" {
		u := fmt.Sprintf("https://api.modrinth.com/v2/version/%s", url.PathEscape(versionID))
		if err := getModrinthJSON(ctx, u, &chosen); err != nil {
			return nil, nil, nil, nil, err
		}
	} else {
		versions, err := ListProjectVersions(ctx, projectID, loaders, mcVer)
		var ok bool
		if err == nil && len(versions) > 0 {
			chosen, ok = pickCompatibleVersion(versions, mcVer, loaders, targetKind)
		}
		if !ok && mcVer != "" && !strings.EqualFold(mcVer, "latest") {
			allVersions, allErr := ListProjectVersions(ctx, projectID, loaders, "")
			if allErr != nil && err == nil {
				err = allErr
			}
			if allErr == nil && len(allVersions) > 0 {
				chosen, ok = pickCompatibleVersion(allVersions, mcVer, loaders, targetKind)
			}
		}
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if !ok {
			loaderDesc := strings.Join(loaders, "/")
			return nil, nil, nil, nil, fmt.Errorf("bu mod/eklenti Minecraft %s (%s) surumuyle uyumlu degil", mcVer, loaderDesc)
		}
	}

	primaryFile := pickVersionFile(chosen, targetKind)
	if primaryFile == nil {
		if targetKind == "datapack" {
			return nil, nil, nil, nil, fmt.Errorf("indirilecek uygun .zip datapack dosyasi bulunamadi")
		}
		return nil, nil, nil, nil, fmt.Errorf("indirilecek dosya bulunamadi")
	}

	destDir := TargetDirForAddonKind(inst, targetKind)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, nil, nil, nil, err
	}
	fileName := filepath.Base(primaryFile.Filename)
	destPath := filepath.Join(destDir, fileName)
	if err := downloadModrinthFile(ctx, primaryFile.URL, destPath); err != nil {
		return nil, nil, nil, nil, err
	}
	installed = append(installed, fileName)
	installedPaths = append(installedPaths, destPath)

	for _, dep := range chosen.Dependencies {
		if dep.DependencyType != "required" || dep.ProjectID == "" {
			continue
		}
		if visited[dep.ProjectID] {
			continue
		}
		if inst.Engine == models.EngineFabric && dep.ProjectID == fabricAPIProjectID && hasFabricAPIInstalled(destDir) {
			visited[fabricAPIProjectID] = true
			continue
		}
		sub, subPaths, subWarn, subMissing, depErr := installModrinthRecursive(
			ctx, inst, dep.ProjectID, dep.VersionID, targetKind, loaders, mcVer, visited, depth+1,
		)
		if depErr != nil {
			label := fetchModrinthProjectLabel(ctx, dep.ProjectID)
			missingDeps = appendUnique(missingDeps, label)
			warnings = appendUnique(warnings, fmt.Sprintf("Gerekli bagimlilik otomatik kurulamadi (%s): %v", label, depErr))
			continue
		}
		installed = append(installed, sub...)
		installedPaths = append(installedPaths, subPaths...)
		for _, w := range subWarn {
			warnings = appendUnique(warnings, w)
		}
		for _, md := range subMissing {
			missingDeps = appendUnique(missingDeps, md)
		}
	}

	return installed, installedPaths, warnings, missingDeps, nil
}

func resolveManifestDependencies(
	ctx context.Context,
	inst models.ServerInstance,
	installedPaths []string,
	targetKind string,
	loaders []string,
	mcVer string,
	visited map[string]bool,
	res *models.AddonInstallResult,
) {
	if len(installedPaths) == 0 || len(loaders) == 0 {
		return
	}
	tracked := make(map[string]bool)
	for _, p := range installedPaths {
		tracked[filepath.Clean(p)] = true
	}

	for pass := 0; pass < 3; pass++ {
		addons, err := ListInstalledAddons(inst)
		if err != nil {
			break
		}
		anyInstalledThisPass := false
		for _, a := range addons {
			full := filepath.Clean(filepath.Join(inst.Path, filepath.FromSlash(a.Path)))
			if !tracked[full] || len(a.MissingDeps) == 0 {
				continue
			}
			for _, depID := range a.MissingDeps {
				depProjID := knownDepModrinthID(inst.Engine, depID)
				if depProjID == "" {
					depProjID = tryResolveModrinthSlug(ctx, depID, loaders, targetKind)
				}
				if depProjID == "" || visited[depProjID] {
					continue
				}
				sub, subPaths, subWarn, _, depErr := installModrinthRecursive(
					ctx, inst, depProjID, "", targetKind, loaders, mcVer, visited, 1,
				)
				if depErr != nil {
					res.Warnings = appendUnique(res.Warnings, fmt.Sprintf("Gerekli bagimlilik otomatik kurulamadi (%s): %v", depID, depErr))
					continue
				}
				for _, fn := range sub {
					res.Dependencies = appendUnique(res.Dependencies, fn)
				}
				for _, sp := range subPaths {
					tracked[filepath.Clean(sp)] = true
				}
				for _, w := range subWarn {
					res.Warnings = appendUnique(res.Warnings, w)
				}
				anyInstalledThisPass = true
			}
		}
		if !anyInstalledThisPass {
			break
		}
	}
}

func collectPostInstallHealth(inst models.ServerInstance, installedPaths []string, res *models.AddonInstallResult) {
	if res == nil || len(installedPaths) == 0 {
		return
	}
	tracked := make(map[string]bool, len(installedPaths))
	for _, p := range installedPaths {
		tracked[filepath.Clean(p)] = true
	}
	addons, err := ListInstalledAddons(inst)
	if err != nil {
		return
	}
	for _, a := range addons {
		full := filepath.Clean(filepath.Join(inst.Path, filepath.FromSlash(a.Path)))
		if !tracked[full] {
			continue
		}
		for _, md := range a.MissingDeps {
			res.MissingDependencies = appendUnique(res.MissingDependencies, md)
		}
		for _, w := range a.Warnings {
			res.Warnings = appendUnique(res.Warnings, w)
		}
	}
}

func knownDepModrinthID(eng models.EngineType, depID string) string {
	lower := strings.ToLower(strings.TrimSpace(depID))
	norm := normalizeAddonKey(depID)

	if eng == models.EngineFabric {
		if isFabricAPIModule(lower) {
			return fabricAPIProjectID
		}
		switch norm {
		case "fabric", "fabric_api":
			return fabricAPIProjectID
		case "fabric_language_kotlin":
			return "Ha28R6CL"
		case "cloth_config", "cloth_config2":
			return "9s6osm5g"
		case "architectury", "architectury_api":
			return "lhGA9TYQ"
		case "geckolib", "geckolib3":
			return "8BmcQJ2H"
		case "owo", "owo_lib":
			return "ccKDOlHs"
		case "collective":
			return "e0M1UDsY"
		case "balm":
			return "MBAkmtvl"
		case "puzzleslib", "puzzles_lib":
			return "QAGBst4M"
		case "yacl", "yet_another_config_lib_v3", "yet_another_config_lib":
			return "1eAoo2KR"
		case "veinminer":
			return "OhduvhIc"
		case "luckperms":
			return "Vebnzrzj"
		case "worldedit":
			return "1u6JkXh5"
		case "packetevents":
			return "HYKaKraK"
		}
		return ""
	}

	if eng == models.EngineNeoForge {
		switch norm {
		case "kotlinforforge", "kffmod", "kotlin_for_forge":
			return "ordsPcFz"
		case "kotlinlangforge", "kotlin_lang_forge":
			return "1vrSzlao"
		case "cloth_config", "cloth_config2":
			return "9s6osm5g"
		case "architectury", "architectury_api":
			return "lhGA9TYQ"
		case "geckolib", "geckolib3":
			return "8BmcQJ2H"
		case "owo", "owo_lib":
			return "ccKDOlHs"
		case "collective":
			return "e0M1UDsY"
		case "balm":
			return "MBAkmtvl"
		case "puzzleslib", "puzzles_lib":
			return "QAGBst4M"
		case "yacl", "yet_another_config_lib_v3", "yet_another_config_lib":
			return "1eAoo2KR"
		case "veinminer":
			return "OhduvhIc"
		case "luckperms":
			return "Vebnzrzj"
		case "worldedit":
			return "1u6JkXh5"
		case "packetevents":
			return "HYKaKraK"
		}
		return ""
	}

	if eng == models.EnginePaper || eng == models.EnginePurpur {
		switch norm {
		case "veinminer":
			return "OhduvhIc"
		case "luckperms":
			return "Vebnzrzj"
		case "placeholderapi":
			return "lKEzGugV"
		case "packetevents":
			return "HYKaKraK"
		case "vault", "vaultunlocked":
			return "ayRaM8J7"
		case "worldedit", "fastasyncworldedit":
			return "1u6JkXh5"
		}
	}
	return ""
}

func tryResolveModrinthSlug(ctx context.Context, depID string, loaders []string, targetKind string) string {
	clean := strings.ToLower(strings.TrimSpace(depID))
	if clean == "" {
		return ""
	}
	candidates := []string{clean}
	if hyphen := strings.ReplaceAll(clean, "_", "-"); hyphen != clean {
		candidates = append(candidates, hyphen)
	}
	for _, slug := range candidates {
		u := fmt.Sprintf("https://api.modrinth.com/v2/project/%s", url.PathEscape(slug))
		var proj struct {
			ID         string   `json:"id"`
			ServerSide string   `json:"server_side"`
			Loaders    []string `json:"loaders"`
		}
		if err := getModrinthJSON(ctx, u, &proj); err != nil || proj.ID == "" {
			continue
		}
		if targetKind == "mod" && strings.EqualFold(proj.ServerSide, "unsupported") {
			continue
		}
		if len(loaders) > 0 && !sharesAnyCaseInsensitive(proj.Loaders, loaders) {
			continue
		}
		return proj.ID
	}
	return ""
}

func fetchModrinthProjectLabel(ctx context.Context, projectID string) string {
	u := fmt.Sprintf("https://api.modrinth.com/v2/project/%s", url.PathEscape(projectID))
	var proj struct {
		Title string `json:"title"`
		Slug  string `json:"slug"`
	}
	if err := getModrinthJSON(ctx, u, &proj); err == nil {
		if strings.TrimSpace(proj.Title) != "" {
			return strings.TrimSpace(proj.Title)
		}
		if strings.TrimSpace(proj.Slug) != "" {
			return strings.TrimSpace(proj.Slug)
		}
	}
	return projectID
}

func sharesAnyCaseInsensitive(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(y)) {
				return true
			}
		}
	}
	return false
}

func pickVersionFile(v models.ModrinthVersion, targetKind string) *models.ModrinthFile {
	if len(v.Files) == 0 {
		return nil
	}
	if targetKind == "datapack" {
		for i := range v.Files {
			if v.Files[i].Primary && strings.HasSuffix(strings.ToLower(v.Files[i].Filename), ".zip") {
				return &v.Files[i]
			}
		}
		for i := range v.Files {
			if strings.HasSuffix(strings.ToLower(v.Files[i].Filename), ".zip") {
				return &v.Files[i]
			}
		}
		return nil
	}
	for i := range v.Files {
		if v.Files[i].Primary {
			return &v.Files[i]
		}
	}
	for i := range v.Files {
		if strings.HasSuffix(strings.ToLower(v.Files[i].Filename), ".jar") {
			return &v.Files[i]
		}
	}
	return &v.Files[0]
}

func loaderRankForVersion(vLoaders, preferredLoaders []string) int {
	if len(preferredLoaders) == 0 {
		return 0
	}
	best := len(preferredLoaders)
	for _, vl := range vLoaders {
		for idx, pl := range preferredLoaders {
			if strings.EqualFold(strings.TrimSpace(vl), strings.TrimSpace(pl)) && idx < best {
				best = idx
			}
		}
	}
	return best
}

func pickCompatibleVersion(versions []models.ModrinthVersion, mcVersion string, preferredLoaders []string, targetKind string) (models.ModrinthVersion, bool) {
	if len(versions) == 0 {
		return models.ModrinthVersion{}, false
	}
	target := strings.TrimSpace(mcVersion)

	type candidate struct {
		ver  models.ModrinthVersion
		tier int // 0: exact+release, 1: exact+prerelease, 2: series+release, 3: series+prerelease
		rank int
	}

	series := mcVersionSeriesPrefix(target)
	exactAnyMC := target == "" || strings.EqualFold(target, "latest")

	var best *candidate
	for i := range versions {
		v := versions[i]
		if pickVersionFile(v, targetKind) == nil {
			continue
		}
		isRelease := strings.EqualFold(v.VersionType, "release")
		tier := -1
		if exactAnyMC {
			if isRelease {
				tier = 0
			} else {
				tier = 1
			}
		} else {
			matchedExact := false
			matchedSeries := false
			for _, gv := range v.GameVersions {
				trimmedGV := strings.TrimSpace(gv)
				if strings.EqualFold(trimmedGV, target) {
					matchedExact = true
					break
				}
				if series != "" && sameMCSeries(trimmedGV, target, series) {
					matchedSeries = true
				}
			}
			if matchedExact {
				if isRelease {
					tier = 0
				} else {
					tier = 1
				}
			} else if matchedSeries {
				if isRelease {
					tier = 2
				} else {
					tier = 3
				}
			}
		}
		if tier < 0 {
			continue
		}
		rank := loaderRankForVersion(v.Loaders, preferredLoaders)
		cand := candidate{ver: v, tier: tier, rank: rank}
		if best == nil || cand.tier < best.tier || (cand.tier == best.tier && cand.rank < best.rank) {
			best = &cand
		}
	}

	if best == nil {
		return models.ModrinthVersion{}, false
	}
	return best.ver, true
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

func jarRequiresFabricAPI(jarPath string) bool {
	insp := InspectAddonPath(jarPath)
	if insp.Fabric == nil {
		return false
	}
	if strings.EqualFold(insp.Fabric.ID, "fabric-api") || strings.EqualFold(insp.Fabric.ID, "fabric") {
		return false
	}
	for _, depID := range insp.Fabric.RequiredDeps {
		if isFabricAPIModule(depID) {
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
		lower := strings.ToLower(e.Name())
		if !strings.HasSuffix(lower, ".jar") {
			continue
		}
		if strings.HasPrefix(lower, "fabric-api") {
			return true
		}
		insp := InspectAddonPath(filepath.Join(modsDir, e.Name()))
		if insp.Fabric != nil {
			id := strings.ToLower(strings.TrimSpace(insp.Fabric.ID))
			if id == "fabric-api" || id == "fabric" {
				return true
			}
		}
	}
	return false
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
