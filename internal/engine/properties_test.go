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

func TestOptimizeLowLagPropertiesAndTurkishTPS(t *testing.T) {
	javaDir := t.TempDir()
	initial := DefaultJavaProperties()
	initial["view-distance"] = "20"
	initial["sync-chunk-writes"] = "true"
	if err := WriteProperties(javaDir, initial); err != nil {
		t.Fatal(err)
	}

	props, err := OptimizeLowLagProperties(models.ServerInstance{
		Engine: models.EnginePaper,
		Path:   javaDir,
	})
	if err != nil {
		t.Fatalf("OptimizeLowLagProperties(Java) failed: %v", err)
	}
	if props["view-distance"] != "8" || props["simulation-distance"] != "6" || props["sync-chunk-writes"] != "false" {
		t.Fatalf("expected optimized java properties, got %+v", props)
	}

	bdsDir := t.TempDir()
	bdsProps, err := OptimizeLowLagProperties(models.ServerInstance{
		Engine: models.EngineBDS,
		Path:   bdsDir,
	})
	if err != nil {
		t.Fatalf("OptimizeLowLagProperties(BDS) failed: %v", err)
	}
	if bdsProps["view-distance"] != "10" || bdsProps["tick-distance"] != "4" {
		t.Fatalf("expected optimized bedrock properties, got %+v", bdsProps)
	}

	// Verify Turkish/EU decimal comma TPS parsing
	m := &Manager{
		consoles: make(map[string]*consoleState),
	}
	m.processLineEvents("srv-test", "[21:44:30] [Server thread/INFO]: TPS from last 1m, 5m, 15m: 19,9, 20,0, 19,9")
	cs := m.getConsole("srv-test")
	if cs.tps != 19.9 {
		t.Fatalf("expected TPS 19.9 from comma decimal log, got %v", cs.tps)
	}

	// Verify low-RAM Aikar args omit AlwaysPreTouch and use 4M regions
	lowArgs := strings.Join(buildJVMArgs(2048, models.JVMAikar, 25), " ")
	if strings.Contains(lowArgs, "AlwaysPreTouch") {
		t.Fatalf("expected AlwaysPreTouch omitted for 2048MB heap, got %s", lowArgs)
	}
	if !strings.Contains(lowArgs, "G1HeapRegionSize=4M") || !strings.Contains(lowArgs, "MaxGCPauseMillis=100") {
		t.Fatalf("expected tuned low-RAM G1 flags, got %s", lowArgs)
	}
}

func TestBuildLivePropertyCommands(t *testing.T) {
	prev := map[string]string{
		"gamemode":   "survival",
		"difficulty": "normal",
		"white-list": "false",
		"pvp":        "true",
	}
	next := map[string]string{
		"gamemode":   "creative",
		"difficulty": "hard",
		"white-list": "true",
		"pvp":        "false",
	}

	cmds, applied := buildLivePropertyCommands(models.EnginePaper, prev, next)
	wantCmds := []string{"defaultgamemode creative", "gamemode creative @a", "difficulty hard", "whitelist on"}
	if len(cmds) != len(wantCmds) {
		t.Fatalf("unexpected Java live cmds %v, want %v", cmds, wantCmds)
	}
	for i := range wantCmds {
		if cmds[i] != wantCmds[i] {
			t.Fatalf("cmd[%d] = %q, want %q", i, cmds[i], wantCmds[i])
		}
	}
	if len(applied) != 3 {
		t.Fatalf("expected 3 Java applied keys, got %v", applied)
	}

	bdsCmds, bdsApplied := buildLivePropertyCommands(models.EngineBDS, prev, next)
	wantBDS := []string{"defaultgamemode creative", "gamemode creative @a", "difficulty hard", "allowlist reload", "gamerule pvp false"}
	if len(bdsCmds) != len(wantBDS) || len(bdsApplied) != 4 {
		t.Fatalf("unexpected BDS live cmds=%v applied=%v", bdsCmds, bdsApplied)
	}

	noCmds, noApplied := buildLivePropertyCommands(models.EnginePaper, prev, prev)
	if len(noCmds) != 0 || len(noApplied) != 0 {
		t.Fatalf("expected no-op for unchanged properties, got cmds=%v applied=%v", noCmds, noApplied)
	}
}

func TestExtractCrashSummaryFabric(t *testing.T) {
	cs := newConsoleState()
	lines := []struct{ lvl, txt string }{
		{"INFO", "[00:03:22] [main/INFO]: Loading Minecraft 26.2 with Fabric Loader 0.20.1"},
		{"ERROR", "[00:03:23] [main/ERROR]: Incompatible mods found!"},
		{"ERROR", "net.fabricmc.loader.impl.FormattedException: Some of your mods are incompatible with the game or each other!"},
		{"INFO", "\t - Mod 'Vein Miner' (mr_vein_miner) 26.x requires any version of fabric-resource-loader-v0, which is missing!"},
		{"INFO", "\tat net.fabricmc.loader.impl.FormattedException.ofLocalized(FormattedException.java:51)"},
		{"INFO", "\tat net.fabricmc.loader.impl.FabricLoaderImpl.load(FabricLoaderImpl.java:207)"},
		{"INFO", "\tat net.fabricmc.loader.impl.launch.knot.Knot.init(Knot.java:142)"},
	}
	for _, l := range lines {
		cs.lines = append(cs.lines, models.ConsoleLine{Level: l.lvl, Text: l.txt})
	}
	got := extractCrashSummary(cs)
	if !strings.Contains(got, "requires any version of fabric-resource-loader-v0") {
		t.Fatalf("expected Fabric missing dependency line, got %q", got)
	}
}
