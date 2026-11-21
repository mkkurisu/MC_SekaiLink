package addons

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func createTestZipEntries(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func createTestJarWithFabricModJSON(t *testing.T, path, fabricModJSON string) {
	t.Helper()
	createTestZipEntries(t, path, map[string]string{"fabric.mod.json": fabricModJSON})
}

func TestIsFabricAPIModule(t *testing.T) {
	mustTrue := []string{"fabric", "fabric-api", "fabric-api-base", "fabric-resource-loader-v0", "fabric-networking-api-v1", "fabric-lifecycle-events-v1"}
	for _, id := range mustTrue {
		if !isFabricAPIModule(id) {
			t.Errorf("expected isFabricAPIModule(%q) = true", id)
		}
	}
	mustFalse := []string{"fabricloader", "fabric-loader", "fabric-language-kotlin", "fabric-language-scala", "fabric-permissions-api", "fabric-permissions-api-v0", "fabric-carpet"}
	for _, id := range mustFalse {
		if isFabricAPIModule(id) {
			t.Errorf("expected isFabricAPIModule(%q) = false", id)
		}
	}
}

func TestJarRequiresFabricAPI(t *testing.T) {
	dir := t.TempDir()
	veinJar := filepath.Join(dir, "veinminer.jar")
	createTestJarWithFabricModJSON(t, veinJar, `{
		"schemaVersion": 1,
		"id": "mr_vein_miner",
		"depends": {
			"fabricloader": ">=0.16.0",
			"fabric-resource-loader-v0": "*"
		}
	}`)
	if !jarRequiresFabricAPI(veinJar) {
		t.Fatalf("expected jarRequiresFabricAPI=true when depending on fabric-resource-loader-v0")
	}

	kotlinOnlyJar := filepath.Join(dir, "kotlin-only.jar")
	createTestJarWithFabricModJSON(t, kotlinOnlyJar, `{
		"schemaVersion": 1,
		"id": "kotlin_only",
		"depends": {
			"fabricloader": ">=0.16.0",
			"fabric-language-kotlin": "*"
		}
	}`)
	if jarRequiresFabricAPI(kotlinOnlyJar) {
		t.Fatalf("expected jarRequiresFabricAPI=false when depending only on fabric-language-kotlin")
	}

	fabricAPIJar := filepath.Join(dir, "fabric-api-0.161.0+26.2.jar")
	createTestJarWithFabricModJSON(t, fabricAPIJar, `{
		"schemaVersion": 1,
		"id": "fabric-api",
		"depends": {
			"fabricloader": ">=0.16.0"
		}
	}`)
	if jarRequiresFabricAPI(fabricAPIJar) {
		t.Fatalf("expected jarRequiresFabricAPI=false for fabric-api itself")
	}
}

func TestHasFabricAPIInstalled(t *testing.T) {
	modsDir := t.TempDir()
	if hasFabricAPIInstalled(modsDir) {
		t.Fatalf("expected empty modsDir not to have fabric-api")
	}

	// Even a renamed Fabric API JAR should be detected via its fabric.mod.json id.
	renamed := filepath.Join(modsDir, "fapi-bundled.jar")
	createTestJarWithFabricModJSON(t, renamed, `{"schemaVersion":1,"id":"fabric-api"}`)
	if !hasFabricAPIInstalled(modsDir) {
		t.Fatalf("expected renamed fabric-api jar to be detected via fabric.mod.json id")
	}
}

func TestPickCompatibleVersion(t *testing.T) {
	jarFiles := []models.ModrinthFile{{Filename: "mod.jar", Primary: true}}
	zipFiles := []models.ModrinthFile{{Filename: "pack.zip", Primary: true}}

	versions := []models.ModrinthVersion{
		{ID: "v-old", VersionType: "release", GameVersions: []string{"1.20.1"}, Loaders: []string{"fabric"}, Files: jarFiles},
		{ID: "v-beta", VersionType: "beta", GameVersions: []string{"26.2"}, Loaders: []string{"fabric"}, Files: jarFiles},
		{ID: "v-rel", VersionType: "release", GameVersions: []string{"26.2"}, Loaders: []string{"fabric"}, Files: jarFiles},
		{ID: "v-patch", VersionType: "release", GameVersions: []string{"26.2.1"}, Loaders: []string{"fabric"}, Files: jarFiles},
	}

	got, ok := pickCompatibleVersion(versions, "26.2", []string{"fabric"}, "mod")
	if !ok || got.ID != "v-rel" {
		t.Fatalf("expected exact release v-rel for 26.2, got ok=%v id=%q", ok, got.ID)
	}

	seriesOnly := []models.ModrinthVersion{
		{ID: "v-old", VersionType: "release", GameVersions: []string{"1.20.1"}, Loaders: []string{"fabric"}, Files: jarFiles},
		{ID: "v-patch", VersionType: "release", GameVersions: []string{"26.2.1"}, Loaders: []string{"fabric"}, Files: jarFiles},
	}
	gotSeries, okSeries := pickCompatibleVersion(seriesOnly, "26.2", []string{"fabric"}, "mod")
	if !okSeries || gotSeries.ID != "v-patch" {
		t.Fatalf("expected same minor series v-patch for 26.2, got ok=%v id=%q", okSeries, gotSeries.ID)
	}

	incompatible := []models.ModrinthVersion{
		{ID: "v-old", VersionType: "release", GameVersions: []string{"1.20.1"}, Loaders: []string{"fabric"}, Files: jarFiles},
	}
	if _, okBad := pickCompatibleVersion(incompatible, "26.2", []string{"fabric"}, "mod"); okBad {
		t.Fatalf("expected major mismatch 1.20.1 vs 26.2 to be rejected")
	}

	// Datapack target must choose .zip over .jar even if .jar appears first
	dpVersions := []models.ModrinthVersion{
		{ID: "dp-jar", VersionType: "release", GameVersions: []string{"26.2"}, Loaders: []string{"datapack"}, Files: jarFiles},
		{ID: "dp-zip", VersionType: "release", GameVersions: []string{"26.2"}, Loaders: []string{"datapack"}, Files: zipFiles},
	}
	gotDP, okDP := pickCompatibleVersion(dpVersions, "26.2", []string{"datapack"}, "datapack")
	if !okDP || gotDP.ID != "dp-zip" {
		t.Fatalf("expected datapack to pick .zip version dp-zip, got ok=%v id=%q", okDP, gotDP.ID)
	}

	// Paper should prefer paper-tagged build over spigot-only build for the same version
	paperVersions := []models.ModrinthVersion{
		{ID: "v-spigot", VersionType: "release", GameVersions: []string{"26.2"}, Loaders: []string{"spigot"}, Files: jarFiles},
		{ID: "v-paper", VersionType: "release", GameVersions: []string{"26.2"}, Loaders: []string{"paper", "folia"}, Files: jarFiles},
	}
	gotPaper, okPaper := pickCompatibleVersion(paperVersions, "26.2", []string{"paper", "spigot", "bukkit", "purpur"}, "plugin")
	if !okPaper || gotPaper.ID != "v-paper" {
		t.Fatalf("expected Paper to prefer paper-native build v-paper, got ok=%v id=%q", okPaper, gotPaper.ID)
	}
}

func TestExpandLoaderFilterAndEngineLoaders(t *testing.T) {
	if got := LoadersForEngine(models.EnginePaper, "plugin"); !reflect.DeepEqual(got, []string{"paper", "spigot", "bukkit", "purpur"}) {
		t.Fatalf("unexpected Paper loaders: %v", got)
	}
	if got := LoadersForEngine(models.EnginePurpur, "plugin"); !reflect.DeepEqual(got, []string{"purpur", "paper", "spigot", "bukkit"}) {
		t.Fatalf("unexpected Purpur loaders: %v", got)
	}
	if got := ExpandLoaderFilter("paper"); !reflect.DeepEqual(got, []string{"paper", "spigot", "bukkit", "purpur"}) {
		t.Fatalf("unexpected expanded paper filter: %v", got)
	}
}
