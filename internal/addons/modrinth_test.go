package addons

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func createTestJarWithFabricModJSON(t *testing.T, path, fabricModJSON string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("fabric.mod.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(fabricModJSON)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
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

	standaloneJar := filepath.Join(dir, "standalone.jar")
	createTestJarWithFabricModJSON(t, standaloneJar, `{
		"schemaVersion": 1,
		"id": "standalone_mod",
		"depends": {
			"fabricloader": ">=0.16.0",
			"minecraft": "~26.2"
		}
	}`)
	if jarRequiresFabricAPI(standaloneJar) {
		t.Fatalf("expected jarRequiresFabricAPI=false for standalone mod without fabric-* dependency")
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
	versions := []models.ModrinthVersion{
		{ID: "v-old", VersionType: "release", GameVersions: []string{"1.20.1"}},
		{ID: "v-beta", VersionType: "beta", GameVersions: []string{"26.2"}},
		{ID: "v-rel", VersionType: "release", GameVersions: []string{"26.2"}},
		{ID: "v-patch", VersionType: "release", GameVersions: []string{"26.2.1"}},
	}

	got, ok := pickCompatibleVersion(versions, "26.2")
	if !ok || got.ID != "v-rel" {
		t.Fatalf("expected exact release v-rel for 26.2, got ok=%v id=%q", ok, got.ID)
	}

	seriesOnly := []models.ModrinthVersion{
		{ID: "v-old", VersionType: "release", GameVersions: []string{"1.20.1"}},
		{ID: "v-patch", VersionType: "release", GameVersions: []string{"26.2.1"}},
	}
	gotSeries, okSeries := pickCompatibleVersion(seriesOnly, "26.2")
	if !okSeries || gotSeries.ID != "v-patch" {
		t.Fatalf("expected same minor series v-patch for 26.2, got ok=%v id=%q", okSeries, gotSeries.ID)
	}

	incompatible := []models.ModrinthVersion{
		{ID: "v-old", VersionType: "release", GameVersions: []string{"1.20.1"}},
	}
	if _, okBad := pickCompatibleVersion(incompatible, "26.2"); okBad {
		t.Fatalf("expected major mismatch 1.20.1 vs 26.2 to be rejected")
	}
}
