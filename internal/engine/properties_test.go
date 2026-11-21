package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func TestReadWriteProperties(t *testing.T) {
	dir := t.TempDir()
	props := DefaultJavaProperties()
	props["motd"] = "SekaiLink Test"
	props["max-players"] = "42"

	if err := WriteProperties(dir, props); err != nil {
		t.Fatalf("WriteProperties failed: %v", err)
	}
	loaded, err := ReadProperties(dir)
	if err != nil {
		t.Fatalf("ReadProperties failed: %v", err)
	}
	if loaded["motd"] != "SekaiLink Test" || loaded["max-players"] != "42" {
		t.Fatalf("unexpected loaded properties: %+v", loaded)
	}
}

func TestSyncOfflineMode(t *testing.T) {
	// 1. Java + Geyser Cross-Play
	javaDir := t.TempDir()
	geyserCfg := filepath.Join(javaDir, "plugins", "Geyser-Spigot", "config.yml")
	if err := os.MkdirAll(filepath.Dir(geyserCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(geyserCfg, []byte("remote:\n  address: auto\n  port: 25565\n  auth-type: floodgate\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := models.ServerInstance{
		Engine:      models.EnginePaper,
		Path:        javaDir,
		OfflineMode: true,
		CrossPlay:   models.CrossPlayConfig{Enabled: true, Floodgate: true},
	}
	if err := SyncOfflineMode(inst); err != nil {
		t.Fatalf("SyncOfflineMode(true) failed: %v", err)
	}
	props, _ := ReadProperties(javaDir)
	if props["online-mode"] != "false" || props["enforce-secure-profile"] != "false" {
		t.Fatalf("expected offline java props, got online-mode=%q enforce-secure-profile=%q", props["online-mode"], props["enforce-secure-profile"])
	}
	rawYml, _ := os.ReadFile(geyserCfg)
	if !strings.Contains(string(rawYml), "auth-type: offline") {
		t.Fatalf("expected Geyser auth-type: offline, got:\n%s", string(rawYml))
	}

	// Switch back to Premium + Floodgate
	inst.OfflineMode = false
	if err := SyncOfflineMode(inst); err != nil {
		t.Fatalf("SyncOfflineMode(false) failed: %v", err)
	}
	props, _ = ReadProperties(javaDir)
	if props["online-mode"] != "true" || props["enforce-secure-profile"] != "true" {
		t.Fatalf("expected online java props, got online-mode=%q enforce-secure-profile=%q", props["online-mode"], props["enforce-secure-profile"])
	}
	rawYml, _ = os.ReadFile(geyserCfg)
	if !strings.Contains(string(rawYml), "auth-type: floodgate") {
		t.Fatalf("expected Geyser auth-type: floodgate, got:\n%s", string(rawYml))
	}

	// 2. BDS
	bdsDir := t.TempDir()
	bdsInst := models.ServerInstance{Engine: models.EngineBDS, Path: bdsDir, OfflineMode: true}
	if err := SyncOfflineMode(bdsInst); err != nil {
		t.Fatalf("BDS SyncOfflineMode failed: %v", err)
	}
	bdsProps, _ := ReadProperties(bdsDir)
	if bdsProps["online-mode"] != "false" {
		t.Fatalf("expected BDS online-mode=false, got %q", bdsProps["online-mode"])
	}

	// 3. PowerNukkitX
	pnxDir := t.TempDir()
	pnxInst := models.ServerInstance{Engine: models.EnginePowerNukkitX, Path: pnxDir, OfflineMode: true}
	if err := SyncOfflineMode(pnxInst); err != nil {
		t.Fatalf("PNX SyncOfflineMode failed: %v", err)
	}
	pnxProps, _ := ReadProperties(pnxDir)
	if pnxProps["xbox-auth"] != "off" {
		t.Fatalf("expected PNX xbox-auth=off, got %q", pnxProps["xbox-auth"])
	}
}
