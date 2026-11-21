package addons

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

const (
	maxManifestBytes  = 512 * 1024
	maxNestedJarBytes = 32 * 1024 * 1024
	maxNestedJars     = 32
)

// ParsedManifestInfo describes metadata and dependencies extracted for one loader format.
type ParsedManifestInfo struct {
	ID           string
	DisplayName  string
	Version      string
	Provides     []string
	RequiredDeps []string
	ClientOnly   bool
}

// AddonInspection holds the parsed capabilities and manifests of an installed addon file or Bedrock pack directory.
type AddonInspection struct {
	IsZip          bool
	IsJar          bool
	Fabric         *ParsedManifestInfo
	NeoForge       *ParsedManifestInfo
	BukkitPaper    *ParsedManifestInfo
	IsPaperPlugin  bool
	IsBukkitPlugin bool
	IsNukkitPlugin bool
	HasPackMcmeta  bool
	InvalidMcmeta  bool
	IsBedrockPack  bool
	BedrockUUID    string
	BedrockName    string
	BedrockVersion string
	BedrockDeps    []string
}

// EffectiveMCVersion converts engine-specific version identifiers (notably NeoForge 21.1.209 -> 1.21.1,
// 26.2.0.95 -> 26.2) into the canonical Minecraft version used by Modrinth and pack manifests.
func EffectiveMCVersion(eng models.EngineType, rawVersion string) string {
	trimmed := strings.TrimSpace(rawVersion)
	if eng != models.EngineNeoForge || trimmed == "" || strings.EqualFold(trimmed, "latest") {
		return trimmed
	}
	base, _, _ := strings.Cut(trimmed, "-")
	parts := strings.Split(base, ".")
	if len(parts) < 2 {
		return trimmed
	}
	p0, err0 := strconv.Atoi(parts[0])
	p1, err1 := strconv.Atoi(parts[1])
	if err0 != nil || err1 != nil {
		return trimmed
	}
	if p0 == 1 {
		return base
	}
	if p0 >= 26 {
		if len(parts) >= 3 {
			if p2, err2 := strconv.Atoi(parts[2]); err2 == nil {
				if p2 == 0 {
					return fmt.Sprintf("%d.%d", p0, p1)
				}
				return fmt.Sprintf("%d.%d.%d", p0, p1, p2)
			}
		}
		return fmt.Sprintf("%d.%d", p0, p1)
	}
	if p0 >= 20 {
		if p1 == 0 {
			return fmt.Sprintf("1.%d", p0)
		}
		return fmt.Sprintf("1.%d.%d", p0, p1)
	}
	return base
}

func isFabricAPIModule(depID string) bool {
	lower := strings.ToLower(strings.TrimSpace(depID))
	if lower == "" {
		return false
	}
	nonAPIExact := map[string]bool{
		"fabricloader":        true,
		"fabric-loader":       true,
		"fabric-carpet":       true,
		"fabric-seasons":      true,
		"fabric-waystones":    true,
		"fabric-tree-chopper": true,
		"fabric-exporter":     true,
	}
	if nonAPIExact[lower] {
		return false
	}
	for _, prefix := range []string{"fabric-language-", "fabric-permissions-api"} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	if lower == "fabric" || lower == "fabric-api" || lower == "fabric-api-base" || lower == "fabric-api-lookup-api-v1" {
		return true
	}
	if strings.HasPrefix(lower, "fabric-") {
		for _, suffix := range []string{"-v0", "-v1", "-v2", "-v3", "-v4", "-base"} {
			if strings.HasSuffix(lower, suffix) {
				return true
			}
		}
	}
	return false
}

func isBuiltInFabricDep(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "", "minecraft", "java", "fabricloader", "fabric-loader", "mixinextras", "mixin-extras":
		return true
	}
	return false
}

func isBuiltInNeoForgeDep(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "", "minecraft", "neoforge", "forge", "java", "fml", "javafml", "lowcodefml", "mclanguage":
		return true
	}
	return false
}

func normalizeAddonKey(id string) string {
	lower := strings.ToLower(strings.TrimSpace(id))
	return strings.ReplaceAll(lower, "-", "_")
}

func addNormalizedProvided(set map[string]bool, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	set[strings.ToLower(id)] = true
	set[normalizeAddonKey(id)] = true
}

func isProvided(set map[string]bool, depID string) bool {
	lower := strings.ToLower(strings.TrimSpace(depID))
	if lower == "" {
		return true
	}
	if set[lower] || set[normalizeAddonKey(lower)] {
		return true
	}
	return false
}

func cleanAddonFileName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.TrimSuffix(lower, ".disabled")
}

// InspectAddonPath reads loader manifests or pack descriptors from an addon file (.jar/.zip) or directory.
func InspectAddonPath(path string) AddonInspection {
	var insp AddonInspection
	info, err := os.Stat(path)
	if err != nil {
		return insp
	}
	if info.IsDir() {
		manifestPath := filepath.Join(path, "manifest.json")
		if raw, err := os.ReadFile(manifestPath); err == nil {
			if bm, ok := parseBedrockManifestBytes(raw); ok {
				insp.IsBedrockPack = true
				insp.BedrockUUID = bm.UUID
				insp.BedrockName = bm.Name
				insp.BedrockVersion = bm.Version
				insp.BedrockDeps = bm.Deps
			}
		}
		return insp
	}

	cleanLower := cleanAddonFileName(filepath.Base(path))
	if strings.HasSuffix(cleanLower, ".jar") {
		insp.IsJar = true
	} else if strings.HasSuffix(cleanLower, ".zip") {
		insp.IsZip = true
	} else {
		return insp
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		return insp
	}
	defer zr.Close()
	inspectZipEntries(&insp, zr.File, 0)
	return insp
}

func inspectZipEntries(insp *AddonInspection, files []*zip.File, depth int) {
	byName := make(map[string]*zip.File, len(files))
	for _, f := range files {
		byName[f.Name] = f
	}

	if insp.IsZip {
		if f, ok := byName["pack.mcmeta"]; ok {
			if raw, err := readZipFileLimited(f, maxManifestBytes); err == nil {
				var obj map[string]any
				if json.Unmarshal(stripJSONCommentsAndBOM(raw), &obj) == nil {
					insp.HasPackMcmeta = true
				} else {
					insp.InvalidMcmeta = true
				}
			}
		}
	}

	if !insp.IsJar {
		return
	}

	if f, ok := byName["fabric.mod.json"]; ok {
		if raw, err := readZipFileLimited(f, maxManifestBytes); err == nil {
			if fm, ok := parseFabricModJSONBytes(raw); ok {
				insp.Fabric = fm
				if depth == 0 {
					collectFabricNestedJars(files, fm)
				}
			}
		}
	}

	if f, ok := byName["META-INF/neoforge.mods.toml"]; ok {
		if raw, err := readZipFileLimited(f, maxManifestBytes); err == nil {
			if nf, ok := parseModsTOMLBytes(raw, true); ok {
				insp.NeoForge = nf
				if depth == 0 {
					collectNeoForgeJarJar(byName, nf)
				}
			}
		}
	} else if f, ok := byName["META-INF/mods.toml"]; ok {
		if raw, err := readZipFileLimited(f, maxManifestBytes); err == nil {
			if nf, ok := parseModsTOMLBytes(raw, false); ok {
				insp.NeoForge = nf
				if depth == 0 {
					collectNeoForgeJarJar(byName, nf)
				}
			}
		}
	}

	if f, ok := byName["paper-plugin.yml"]; ok {
		if raw, err := readZipFileLimited(f, maxManifestBytes); err == nil {
			if pp := parsePaperPluginYML(raw); pp != nil {
				insp.BukkitPaper = pp
				insp.IsPaperPlugin = true
			}
		}
	}
	if f, ok := byName["plugin.yml"]; ok {
		if raw, err := readZipFileLimited(f, maxManifestBytes); err == nil {
			bp, isNukkit := parsePluginYML(raw)
			if insp.BukkitPaper == nil {
				insp.BukkitPaper = bp
				if isNukkit {
					insp.IsNukkitPlugin = true
				} else {
					insp.IsBukkitPlugin = true
				}
			} else if bp != nil {
				mergePluginInfo(insp.BukkitPaper, bp)
			}
		}
	}
}

func readZipFileLimited(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, limit))
}

func collectFabricNestedJars(files []*zip.File, parent *ParsedManifestInfo) {
	if parent == nil {
		return
	}
	count := 0
	for _, f := range files {
		if f.FileInfo().IsDir() {
			continue
		}
		if !strings.HasPrefix(f.Name, "META-INF/jars/") || !strings.HasSuffix(strings.ToLower(f.Name), ".jar") {
			continue
		}
		count++
		if count > maxNestedJars {
			break
		}
		raw, err := readZipFileLimited(f, maxNestedJarBytes)
		if err != nil {
			continue
		}
		nzr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			continue
		}
		var sub AddonInspection
		sub.IsJar = true
		inspectZipEntries(&sub, nzr.File, 1)
		if sub.Fabric != nil {
			if sub.Fabric.ID != "" {
				parent.Provides = appendUnique(parent.Provides, sub.Fabric.ID)
			}
			for _, p := range sub.Fabric.Provides {
				parent.Provides = appendUnique(parent.Provides, p)
			}
		}
	}
}

func collectNeoForgeJarJar(byName map[string]*zip.File, parent *ParsedManifestInfo) {
	if parent == nil {
		return
	}
	metaFile, ok := byName["META-INF/jarjar/metadata.json"]
	if !ok {
		return
	}
	rawMeta, err := readZipFileLimited(metaFile, maxManifestBytes)
	if err != nil {
		return
	}
	var doc struct {
		Jars []struct {
			Path       string `json:"path"`
			Identifier struct {
				Artifact string `json:"artifact"`
			} `json:"identifier"`
		} `json:"jars"`
	}
	if json.Unmarshal(rawMeta, &doc) != nil {
		return
	}
	count := 0
	for _, entry := range doc.Jars {
		if count >= maxNestedJars {
			break
		}
		zf, exists := byName[entry.Path]
		if !exists {
			continue
		}
		count++
		rawJar, err := readZipFileLimited(zf, maxNestedJarBytes)
		if err != nil {
			continue
		}
		nzr, err := zip.NewReader(bytes.NewReader(rawJar), int64(len(rawJar)))
		if err != nil {
			continue
		}
		var sub AddonInspection
		sub.IsJar = true
		inspectZipEntries(&sub, nzr.File, 1)
		if sub.NeoForge != nil {
			if sub.NeoForge.ID != "" {
				parent.Provides = appendUnique(parent.Provides, sub.NeoForge.ID)
			}
			for _, p := range sub.NeoForge.Provides {
				parent.Provides = appendUnique(parent.Provides, p)
			}
		}
	}
}

type fabricModJSONDoc struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Environment string         `json:"environment"`
	Provides    []string       `json:"provides"`
	Depends     map[string]any `json:"depends"`
}

func parseFabricModJSONBytes(raw []byte) (*ParsedManifestInfo, bool) {
	var doc fabricModJSONDoc
	if err := json.Unmarshal(bytes.TrimPrefix(raw, []byte("\xEF\xBB\xBF")), &doc); err != nil {
		return nil, false
	}
	if strings.TrimSpace(doc.ID) == "" && strings.TrimSpace(doc.Name) == "" {
		return nil, false
	}
	info := &ParsedManifestInfo{
		ID:          strings.TrimSpace(doc.ID),
		DisplayName: strings.TrimSpace(doc.Name),
		Version:     strings.TrimSpace(doc.Version),
		ClientOnly:  strings.EqualFold(strings.TrimSpace(doc.Environment), "client"),
	}
	if info.DisplayName == "" {
		info.DisplayName = info.ID
	}
	if info.ID != "" {
		info.Provides = appendUnique(info.Provides, info.ID)
	}
	for _, p := range doc.Provides {
		info.Provides = appendUnique(info.Provides, p)
	}
	for depID := range doc.Depends {
		if !isBuiltInFabricDep(depID) && !strings.EqualFold(strings.TrimSpace(depID), info.ID) {
			info.RequiredDeps = appendUnique(info.RequiredDeps, strings.TrimSpace(depID))
		}
	}
	return info, true
}

func parseModsTOMLBytes(raw []byte, isNeoForge bool) (*ParsedManifestInfo, bool) {
	text := strings.TrimPrefix(string(raw), "\xEF\xBB\xBF")
	lines := strings.Split(text, "\n")

	type modBlock struct {
		modID      string
		display    string
		version    string
		clientOnly string
	}
	type depBlock struct {
		modID     string
		depType   string
		mandatory string
		side      string
	}

	var topClientOnly string
	var mods []modBlock
	var deps []depBlock
	sectionKind := "" // "", "mod", "dep", "other"
	curModIdx := -1
	curDepIdx := -1
	multilineDelim := ""

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if multilineDelim != "" {
			if idx := strings.Index(line, multilineDelim); idx >= 0 {
				after := strings.TrimSpace(line[idx+len(multilineDelim):])
				if after == "" || strings.HasPrefix(after, "#") {
					multilineDelim = ""
				}
			}
			continue
		}

		line = stripTOMLComment(line)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			hdr := strings.ToLower(strings.TrimSpace(line[2 : len(line)-2]))
			if hdr == "mods" {
				mods = append(mods, modBlock{})
				curModIdx = len(mods) - 1
				sectionKind = "mod"
			} else if strings.HasPrefix(hdr, "dependencies.") {
				deps = append(deps, depBlock{})
				curDepIdx = len(deps) - 1
				sectionKind = "dep"
			} else {
				sectionKind = "other"
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			hdr := strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if hdr == "mods" {
				mods = append(mods, modBlock{})
				curModIdx = len(mods) - 1
				sectionKind = "mod"
			} else {
				sectionKind = "other"
			}
			continue
		}

		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		valRaw := strings.TrimSpace(v)
		for _, delim := range []string{"'''", `"""`} {
			if strings.HasPrefix(valRaw, delim) {
				rest := valRaw[len(delim):]
				if !strings.Contains(rest, delim) {
					multilineDelim = delim
				}
				valRaw = ""
				break
			}
		}
		if multilineDelim != "" {
			continue
		}
		val := unquoteSimple(valRaw)

		switch sectionKind {
		case "":
			if key == "clientsideonly" {
				topClientOnly = val
			}
		case "mod":
			if curModIdx < 0 {
				continue
			}
			switch key {
			case "modid":
				mods[curModIdx].modID = val
			case "displayname":
				mods[curModIdx].display = val
			case "version":
				mods[curModIdx].version = val
			case "clientsideonly":
				mods[curModIdx].clientOnly = val
			}
		case "dep":
			if curDepIdx < 0 {
				continue
			}
			switch key {
			case "modid":
				deps[curDepIdx].modID = val
			case "type":
				deps[curDepIdx].depType = val
			case "mandatory":
				deps[curDepIdx].mandatory = val
			case "side":
				deps[curDepIdx].side = val
			}
		}
	}

	if len(mods) == 0 && len(deps) == 0 {
		return nil, false
	}

	info := &ParsedManifestInfo{}
	selfIDs := make(map[string]bool)
	allModsClientOnly := len(mods) > 0
	for i, m := range mods {
		if m.modID != "" {
			if info.ID == "" {
				info.ID = m.modID
			}
			if i == 0 {
				info.DisplayName = m.display
				info.Version = m.version
			}
			info.Provides = appendUnique(info.Provides, m.modID)
			selfIDs[strings.ToLower(m.modID)] = true
		}
		isClient := strings.EqualFold(m.clientOnly, "true") || (m.clientOnly == "" && strings.EqualFold(topClientOnly, "true"))
		if !isClient {
			allModsClientOnly = false
		}
	}
	if len(mods) == 0 {
		allModsClientOnly = strings.EqualFold(topClientOnly, "true")
	}
	info.ClientOnly = allModsClientOnly
	if info.DisplayName == "" {
		info.DisplayName = info.ID
	}

	for _, d := range deps {
		if d.modID == "" || isBuiltInNeoForgeDep(d.modID) || selfIDs[strings.ToLower(d.modID)] {
			continue
		}
		if strings.EqualFold(d.side, "client") {
			continue
		}
		required := false
		if isNeoForge {
			depType := strings.ToLower(strings.TrimSpace(d.depType))
			if depType == "required" || (depType == "" && strings.EqualFold(d.mandatory, "true")) {
				required = true
			} else if depType == "" && d.mandatory == "" {
				// NeoForge 1.20.5+ defaults dependency type to required when omitted.
				required = true
			}
		} else {
			if strings.EqualFold(d.mandatory, "true") || strings.EqualFold(d.depType, "required") {
				required = true
			}
		}
		if required {
			info.RequiredDeps = appendUnique(info.RequiredDeps, d.modID)
		}
	}

	return info, info.ID != "" || len(info.RequiredDeps) > 0
}

func parsePluginYML(raw []byte) (info *ParsedManifestInfo, isNukkit bool) {
	text := strings.TrimPrefix(string(raw), "\xEF\xBB\xBF")
	lines := strings.Split(text, "\n")

	out := &ParsedManifestInfo{}
	hasAPIVersion := false
	hasNukkitAPI := false
	activeListKey := ""
	activeIndent := -1

	for _, rawLine := range lines {
		cleaned := stripYAMLComment(rawLine)
		trimmed := strings.TrimSpace(cleaned)
		if trimmed == "" || trimmed == "---" || trimmed == "..." {
			continue
		}
		indent := countLeadingSpaces(rawLine)

		if activeListKey != "" {
			if indent > activeIndent && strings.HasPrefix(trimmed, "-") {
				item := unquoteSimple(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")))
				if item != "" {
					switch activeListKey {
					case "depend":
						out.RequiredDeps = appendUnique(out.RequiredDeps, item)
					case "provides":
						out.Provides = appendUnique(out.Provides, item)
					}
				}
				continue
			}
			if indent == activeIndent && strings.HasPrefix(trimmed, "-") {
				item := unquoteSimple(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")))
				if item != "" {
					switch activeListKey {
					case "depend":
						out.RequiredDeps = appendUnique(out.RequiredDeps, item)
					case "provides":
						out.Provides = appendUnique(out.Provides, item)
					}
				}
				continue
			}
			activeListKey = ""
			activeIndent = -1
		}

		if indent > 0 {
			continue
		}
		k, v, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)

		switch key {
		case "name":
			out.ID = unquoteSimple(val)
			out.DisplayName = out.ID
			if out.ID != "" {
				out.Provides = appendUnique(out.Provides, out.ID)
			}
		case "version":
			out.Version = unquoteSimple(val)
		case "api-version":
			if val != "" {
				hasAPIVersion = true
			}
		case "api":
			hasNukkitAPI = true
		case "depend":
			if val == "" {
				activeListKey = "depend"
				activeIndent = indent
			} else {
				for _, item := range parseYAMLInlineOrScalarList(val) {
					out.RequiredDeps = appendUnique(out.RequiredDeps, item)
				}
			}
		case "provides":
			if val == "" {
				activeListKey = "provides"
				activeIndent = indent
			} else {
				for _, item := range parseYAMLInlineOrScalarList(val) {
					out.Provides = appendUnique(out.Provides, item)
				}
			}
		}
	}

	if out.ID == "" && len(out.RequiredDeps) == 0 {
		return nil, false
	}
	return out, hasNukkitAPI && !hasAPIVersion
}

func parsePaperPluginYML(raw []byte) *ParsedManifestInfo {
	text := strings.TrimPrefix(string(raw), "\xEF\xBB\xBF")
	lines := strings.Split(text, "\n")

	out := &ParsedManifestInfo{}
	topSection := ""
	providesListActive := false
	providesIndent := -1

	inDepsSection := false
	depsIndent := -1
	inDepGroup := false // dependencies.server or dependencies.bootstrap
	depGroupIndent := -1
	curDepName := ""
	curDepIndent := -1
	curDepRequired := true

	flushCurDep := func() {
		if curDepName != "" && curDepRequired {
			out.RequiredDeps = appendUnique(out.RequiredDeps, curDepName)
		}
		curDepName = ""
		curDepIndent = -1
		curDepRequired = true
	}

	for _, rawLine := range lines {
		cleaned := stripYAMLComment(rawLine)
		trimmed := strings.TrimSpace(cleaned)
		if trimmed == "" || trimmed == "---" || trimmed == "..." {
			continue
		}
		indent := countLeadingSpaces(rawLine)

		if providesListActive {
			if indent >= providesIndent && strings.HasPrefix(trimmed, "-") {
				item := unquoteSimple(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")))
				if item != "" {
					out.Provides = appendUnique(out.Provides, item)
				}
				continue
			}
			providesListActive = false
		}

		if inDepsSection && indent <= depsIndent && !strings.HasPrefix(trimmed, "-") {
			flushCurDep()
			inDepsSection = false
			inDepGroup = false
		}

		if inDepsSection {
			if inDepGroup && indent <= depGroupIndent {
				flushCurDep()
				inDepGroup = false
			}
			if !inDepGroup {
				k, _, ok := strings.Cut(trimmed, ":")
				if ok {
					grp := strings.ToLower(strings.TrimSpace(k))
					if grp == "server" || grp == "bootstrap" {
						inDepGroup = true
						depGroupIndent = indent
					}
				}
				continue
			}
			// Inside dependencies.server or dependencies.bootstrap
			if curDepName != "" && indent <= curDepIndent {
				flushCurDep()
			}
			if curDepName == "" {
				k, v, ok := strings.Cut(trimmed, ":")
				if ok {
					name := unquoteSimple(strings.TrimSpace(k))
					if name != "" {
						curDepName = name
						curDepIndent = indent
						curDepRequired = true
						val := strings.TrimSpace(v)
						if strings.HasPrefix(val, "{") && strings.Contains(strings.ToLower(val), "required: false") {
							curDepRequired = false
						}
					}
				}
				continue
			}
			// Inside a specific dependency entry's properties
			k, v, ok := strings.Cut(trimmed, ":")
			if ok && strings.EqualFold(strings.TrimSpace(k), "required") {
				if strings.EqualFold(unquoteSimple(strings.TrimSpace(v)), "false") {
					curDepRequired = false
				}
			}
			continue
		}

		if indent == 0 {
			topSection = ""
			k, v, ok := strings.Cut(trimmed, ":")
			if !ok {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(k))
			val := strings.TrimSpace(v)
			topSection = key
			switch key {
			case "name":
				out.ID = unquoteSimple(val)
				out.DisplayName = out.ID
				if out.ID != "" {
					out.Provides = appendUnique(out.Provides, out.ID)
				}
			case "version":
				out.Version = unquoteSimple(val)
			case "provides":
				if val == "" {
					providesListActive = true
					providesIndent = indent
				} else {
					for _, item := range parseYAMLInlineOrScalarList(val) {
						out.Provides = appendUnique(out.Provides, item)
					}
				}
			case "dependencies":
				inDepsSection = true
				depsIndent = indent
				inDepGroup = false
			}
			_ = topSection
		}
	}
	flushCurDep()

	if out.ID == "" && len(out.RequiredDeps) == 0 {
		return nil
	}
	return out
}

func mergePluginInfo(dst, src *ParsedManifestInfo) {
	if dst == nil || src == nil {
		return
	}
	if dst.ID == "" {
		dst.ID = src.ID
	}
	if dst.DisplayName == "" {
		dst.DisplayName = src.DisplayName
	}
	if dst.Version == "" {
		dst.Version = src.Version
	}
	for _, p := range src.Provides {
		dst.Provides = appendUnique(dst.Provides, p)
	}
	for _, d := range src.RequiredDeps {
		dst.RequiredDeps = appendUnique(dst.RequiredDeps, d)
	}
}

type bedrockManifestDoc struct {
	Header struct {
		Name    string `json:"name"`
		UUID    string `json:"uuid"`
		Version []int  `json:"version"`
	} `json:"header"`
	Dependencies []struct {
		UUID       string `json:"uuid"`
		ModuleName string `json:"module_name"`
	} `json:"dependencies"`
}

type parsedBedrockMeta struct {
	Name    string
	UUID    string
	Version string
	Deps    []string
}

func parseBedrockManifestBytes(raw []byte) (parsedBedrockMeta, bool) {
	var doc bedrockManifestDoc
	if err := json.Unmarshal(stripJSONCommentsAndBOM(raw), &doc); err != nil {
		return parsedBedrockMeta{}, false
	}
	if strings.TrimSpace(doc.Header.UUID) == "" {
		return parsedBedrockMeta{}, false
	}
	ver := ""
	if len(doc.Header.Version) > 0 {
		parts := make([]string, len(doc.Header.Version))
		for i, n := range doc.Header.Version {
			parts[i] = strconv.Itoa(n)
		}
		ver = strings.Join(parts, ".")
	}
	var deps []string
	for _, d := range doc.Dependencies {
		u := strings.TrimSpace(d.UUID)
		if u != "" && d.ModuleName == "" {
			deps = appendUnique(deps, u)
		}
	}
	return parsedBedrockMeta{
		Name:    strings.TrimSpace(doc.Header.Name),
		UUID:    strings.TrimSpace(doc.Header.UUID),
		Version: ver,
		Deps:    deps,
	}, true
}

// stripJSONCommentsAndBOM removes UTF-8 BOM and // or /* */ comments without modifying string literals.
func stripJSONCommentsAndBOM(raw []byte) []byte {
	raw = bytes.TrimPrefix(raw, []byte("\xEF\xBB\xBF"))
	var out bytes.Buffer
	out.Grow(len(raw))

	inString := false
	escaped := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inLineComment {
			if c == '\n' {
				inLineComment = false
				out.WriteByte(c)
			}
			continue
		}
		if inBlockComment {
			if c == '*' && i+1 < len(raw) && raw[i+1] == '/' {
				inBlockComment = false
				i++
			} else if c == '\n' {
				out.WriteByte('\n')
			}
			continue
		}
		if inString {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(raw) {
			if raw[i+1] == '/' {
				inLineComment = true
				i++
				continue
			}
			if raw[i+1] == '*' {
				inBlockComment = true
				i++
				continue
			}
		}
		out.WriteByte(c)
	}
	return out.Bytes()
}

func stripTOMLComment(s string) string {
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inDouble {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inDouble = false
			}
			continue
		}
		if inSingle {
			if c == '\'' {
				inSingle = false
			}
			continue
		}
		if c == '"' {
			inDouble = true
		} else if c == '\'' {
			inSingle = true
		} else if c == '#' {
			return s[:i]
		}
	}
	return s
}

func stripYAMLComment(s string) string {
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inDouble {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inDouble = false
			}
			continue
		}
		if inSingle {
			if c == '\'' {
				inSingle = false
			}
			continue
		}
		if c == '"' {
			inDouble = true
		} else if c == '\'' {
			inSingle = true
		} else if c == '#' && (i == 0 || unicode.IsSpace(rune(s[i-1]))) {
			return s[:i]
		}
	}
	return s
}

func unquoteSimple(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func countLeadingSpaces(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			n++
		} else if s[i] == '\t' {
			n += 2
		} else {
			break
		}
	}
	return n
}

func parseYAMLInlineOrScalarList(val string) []string {
	val = strings.TrimSpace(val)
	if val == "" || val == "[]" {
		return nil
	}
	if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
		inner := strings.TrimSpace(val[1 : len(val)-1])
		if inner == "" {
			return nil
		}
		parts := strings.Split(inner, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			item := unquoteSimple(strings.TrimSpace(p))
			if item != "" {
				out = appendUnique(out, item)
			}
		}
		return out
	}
	item := unquoteSimple(val)
	if item == "" {
		return nil
	}
	return []string{item}
}

func appendUnique(slice []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return slice
	}
	for _, existing := range slice {
		if strings.EqualFold(existing, item) {
			return slice
		}
	}
	return append(slice, item)
}

// manifestForEngine picks the loader manifest corresponding to the server engine.
func manifestForEngine(insp AddonInspection, eng models.EngineType) *ParsedManifestInfo {
	switch eng {
	case models.EngineFabric:
		return insp.Fabric
	case models.EngineNeoForge:
		return insp.NeoForge
	case models.EnginePaper, models.EnginePurpur, models.EnginePowerNukkitX:
		return insp.BukkitPaper
	default:
		if insp.Fabric != nil {
			return insp.Fabric
		}
		if insp.NeoForge != nil {
			return insp.NeoForge
		}
		return insp.BukkitPaper
	}
}

func expectedActiveKind(eng models.EngineType) string {
	switch eng {
	case models.EngineFabric, models.EngineNeoForge:
		return "mod"
	case models.EnginePaper, models.EnginePurpur, models.EnginePowerNukkitX:
		return "plugin"
	default:
		return ""
	}
}

// AnalyzeInstalledAddons enriches installed addon entries with manifest metadata, missing dependency diagnostics,
// and loader/folder/environment compatibility warnings across all engines.
func AnalyzeInstalledAddons(inst models.ServerInstance, items []models.InstalledAddon) []models.InstalledAddon {
	if len(items) == 0 {
		return items
	}

	inspections := make([]AddonInspection, len(items))
	enabledProvided := make(map[string]bool)
	hasFabricAPI := false
	activeKind := expectedActiveKind(inst.Engine)

	for i, item := range items {
		fullPath := filepath.Join(inst.Path, filepath.FromSlash(item.Path))
		insp := InspectAddonPath(fullPath)
		inspections[i] = insp

		if !item.Enabled {
			continue
		}

		if inst.Engine == models.EngineBDS {
			if insp.IsBedrockPack && insp.BedrockUUID != "" {
				addNormalizedProvided(enabledProvided, insp.BedrockUUID)
			} else if item.UUID != "" {
				addNormalizedProvided(enabledProvided, item.UUID)
			}
			continue
		}

		if activeKind != "" && item.Kind == activeKind {
			if m := manifestForEngine(insp, inst.Engine); m != nil {
				addNormalizedProvided(enabledProvided, m.ID)
				for _, p := range m.Provides {
					addNormalizedProvided(enabledProvided, p)
				}
				if inst.Engine == models.EngineFabric &&
					(strings.EqualFold(m.ID, "fabric-api") || strings.EqualFold(m.ID, "fabric")) {
					hasFabricAPI = true
				}
			}
			if inst.Engine == models.EngineFabric && strings.HasPrefix(strings.ToLower(item.Name), "fabric-api") {
				hasFabricAPI = true
				addNormalizedProvided(enabledProvided, "fabric-api")
				addNormalizedProvided(enabledProvided, "fabric")
			}
		}
	}

	out := make([]models.InstalledAddon, len(items))
	for i, item := range items {
		out[i] = item
		insp := inspections[i]

		if insp.IsBedrockPack {
			if insp.BedrockName != "" {
				out[i].DisplayName = insp.BedrockName
			}
			if insp.BedrockVersion != "" {
				out[i].Version = insp.BedrockVersion
			}
			if insp.BedrockUUID != "" && out[i].UUID == "" {
				out[i].UUID = insp.BedrockUUID
			}
		} else {
			m := manifestForEngine(insp, inst.Engine)
			if m == nil {
				m = manifestForEngine(insp, "")
			}
			if m != nil {
				out[i].AddonID = m.ID
				out[i].DisplayName = m.DisplayName
				out[i].Version = m.Version
			}
		}

		out[i].MissingDeps, out[i].Warnings = evaluateItemHealth(inst, item, insp, enabledProvided, hasFabricAPI)
	}
	return out
}

func evaluateItemHealth(
	inst models.ServerInstance,
	item models.InstalledAddon,
	insp AddonInspection,
	enabledProvided map[string]bool,
	hasFabricAPI bool,
) (missingDeps []string, warnings []string) {
	if item.Kind == "geyser-pack" {
		if !inst.CrossPlay.Enabled {
			warnings = appendUnique(warnings, "Cross-Play (GeyserMC) kapali oldugu icin Geyser paketi yuklenmez")
		}
		return nil, warnings
	}

	if item.Kind == "datapack" {
		if insp.IsJar {
			warnings = appendUnique(warnings, "Datapack klasorune .jar konulamaz; .zip datapack gerekli")
		} else if insp.IsZip && insp.InvalidMcmeta {
			warnings = appendUnique(warnings, "pack.mcmeta JSON dosyasi bozuk; Minecraft datapack'i yuklemeyebilir")
		} else if insp.IsZip && !insp.HasPackMcmeta {
			warnings = appendUnique(warnings, "Arsiv kokunde pack.mcmeta bulunamadi; gecerli bir datapack .zip olmayabilir")
		}
		return nil, warnings
	}

	if item.Kind == "bedrock-resource" || item.Kind == "bedrock-behavior" {
		if !insp.IsBedrockPack {
			warnings = appendUnique(warnings, "manifest.json bulunamadi veya gecersiz")
			return nil, warnings
		}
		if item.Enabled && inst.Engine == models.EngineBDS {
			for _, depUUID := range insp.BedrockDeps {
				if !isProvided(enabledProvided, depUUID) {
					missingDeps = appendUnique(missingDeps, depUUID)
				}
			}
		}
		return missingDeps, warnings
	}

	// Engine vs folder checks for mods/plugins
	switch inst.Engine {
	case models.EnginePaper, models.EnginePurpur:
		if item.Kind == "mod" {
			warnings = appendUnique(warnings, fmt.Sprintf("%s motoru mods/ klasorunu yuklemez; eklentiler plugins/ klasorunde olmali", inst.Engine))
			return nil, warnings
		}
		if item.Kind == "plugin" {
			if insp.IsJar && insp.BukkitPaper == nil {
				if insp.Fabric != nil || insp.NeoForge != nil {
					warnings = appendUnique(warnings, fmt.Sprintf("Bu JAR yalnizca %s modudur; %s eklentisi (Paper/Spigot/Bukkit) icermiyor", describeDetectedLoaders(insp), inst.Engine))
				} else {
					warnings = appendUnique(warnings, "Gecerli plugin.yml / paper-plugin.yml bulunamadi")
				}
				return nil, warnings
			}
			if insp.IsNukkitPlugin && !insp.IsPaperPlugin && !insp.IsBukkitPlugin {
				warnings = appendUnique(warnings, "Bu eklenti PowerNukkitX/Nukkit icindir; Paper/Purpur ile uyumlu olmayabilir")
			}
		}

	case models.EngineFabric:
		if item.Kind == "plugin" {
			warnings = appendUnique(warnings, "Fabric sunucular plugins/ klasorunu yuklemez; modlar mods/ klasorunde olmali")
			return nil, warnings
		}
		if item.Kind == "mod" && insp.IsJar {
			if insp.Fabric == nil {
				if insp.NeoForge != nil || insp.BukkitPaper != nil {
					warnings = appendUnique(warnings, fmt.Sprintf("Bu JAR %s icindir; Fabric (fabric.mod.json) icermiyor", describeDetectedLoaders(insp)))
				} else {
					warnings = appendUnique(warnings, "Gecerli fabric.mod.json bulunamadi")
				}
				return nil, warnings
			}
			if insp.Fabric.ClientOnly {
				warnings = appendUnique(warnings, "Istemci tarafli (client-only) Fabric modu; dedicated sunucuda cokmeye neden olabilir")
			}
		}

	case models.EngineNeoForge:
		if item.Kind == "plugin" {
			warnings = appendUnique(warnings, "NeoForge sunucular plugins/ klasorunu yuklemez; modlar mods/ klasorunde olmali")
			return nil, warnings
		}
		if item.Kind == "mod" && insp.IsJar {
			if insp.NeoForge == nil {
				if insp.Fabric != nil || insp.BukkitPaper != nil {
					warnings = appendUnique(warnings, fmt.Sprintf("Bu JAR %s icindir; NeoForge (neoforge.mods.toml) icermiyor", describeDetectedLoaders(insp)))
				} else {
					warnings = appendUnique(warnings, "Gecerli NeoForge/Forge mods.toml bulunamadi")
				}
				return nil, warnings
			}
			if insp.NeoForge.ClientOnly {
				warnings = appendUnique(warnings, "Istemci tarafli (client-only) NeoForge modu; dedicated sunucuda cokmeye neden olabilir")
			}
		}

	case models.EnginePowerNukkitX:
		if item.Kind == "mod" {
			warnings = appendUnique(warnings, "PowerNukkitX Java mods/ klasorunu yuklemez; PNX eklentileri plugins/ klasorunde olmali")
			return nil, warnings
		}
		if item.Kind == "plugin" && insp.IsJar {
			if insp.IsPaperPlugin || (insp.IsBukkitPlugin && !insp.IsNukkitPlugin) {
				warnings = appendUnique(warnings, "Bu JAR Bukkit/Paper Java eklentisidir; PowerNukkitX (Bedrock) ile uyumlu degildir")
			} else if insp.BukkitPaper == nil && (insp.Fabric != nil || insp.NeoForge != nil) {
				warnings = appendUnique(warnings, fmt.Sprintf("Bu JAR %s modudur; PowerNukkitX ile calismaz", describeDetectedLoaders(insp)))
			}
		}

	case models.EngineVanilla:
		if item.Kind == "mod" || item.Kind == "plugin" {
			warnings = appendUnique(warnings, "Vanilla sunucu .jar mod veya eklenti yuklemez; yalnizca world/datapacks/ (.zip) destekler")
			return nil, warnings
		}

	case models.EngineBDS:
		if item.Kind == "mod" || item.Kind == "plugin" {
			warnings = appendUnique(warnings, "Resmi Bedrock (BDS) .jar mod veya eklenti yuklemez; yalnizca .mcpack / .mcaddon destekler")
			return nil, warnings
		}
	}

	if !item.Enabled || item.Kind != expectedActiveKind(inst.Engine) {
		return nil, warnings
	}

	m := manifestForEngine(insp, inst.Engine)
	if m == nil {
		return nil, warnings
	}
	for _, depID := range m.RequiredDeps {
		if isProvided(enabledProvided, depID) {
			continue
		}
		if inst.Engine == models.EngineFabric && isFabricAPIModule(depID) {
			if hasFabricAPI {
				continue
			}
			missingDeps = appendUnique(missingDeps, "fabric-api")
			continue
		}
		missingDeps = appendUnique(missingDeps, depID)
	}
	return missingDeps, warnings
}

func describeDetectedLoaders(insp AddonInspection) string {
	var list []string
	if insp.Fabric != nil {
		list = append(list, "Fabric")
	}
	if insp.NeoForge != nil {
		list = append(list, "NeoForge")
	}
	if insp.IsPaperPlugin || insp.IsBukkitPlugin {
		list = append(list, "Paper/Bukkit")
	}
	if insp.IsNukkitPlugin {
		list = append(list, "PowerNukkitX")
	}
	if len(list) == 0 {
		return "Bilinmeyen"
	}
	return strings.Join(list, "/")
}
