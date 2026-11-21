package addons

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func createNestedJarBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
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
	return buf.Bytes()
}

func TestEffectiveMCVersion(t *testing.T) {
	cases := []struct {
		eng  models.EngineType
		raw  string
		want string
	}{
		{models.EngineNeoForge, "21.1.209", "1.21.1"},
		{models.EngineNeoForge, "21.0.167", "1.21"},
		{models.EngineNeoForge, "20.4.237", "1.20.4"},
		{models.EngineNeoForge, "26.2.0.95", "26.2"},
		{models.EngineNeoForge, "26.1.2.128", "26.1.2"},
		{models.EngineNeoForge, "1.21.1", "1.21.1"},
		{models.EnginePaper, "26.2", "26.2"},
		{models.EngineFabric, "1.21.4", "1.21.4"},
	}
	for _, tc := range cases {
		if got := EffectiveMCVersion(tc.eng, tc.raw); got != tc.want {
			t.Errorf("EffectiveMCVersion(%s, %q) = %q, want %q", tc.eng, tc.raw, got, tc.want)
		}
	}
}

func TestAnalyzeFabricAddons(t *testing.T) {
	dir := t.TempDir()
	inst := models.ServerInstance{
		ID:        "srv-fab",
		Engine:    models.EngineFabric,
		MCVersion: "26.2",
		Path:      dir,
	}

	// Mod needing fabric-api + fabric-language-kotlin + cloth-config
	createTestJarWithFabricModJSON(t, filepath.Join(dir, "mods", "example-mod.jar"), `{
		"schemaVersion": 1,
		"id": "example_mod",
		"name": "Example Mod",
		"version": "1.0.0",
		"environment": "*",
		"depends": {
			"fabricloader": ">=0.16.0",
			"minecraft": "~26.2",
			"fabric-resource-loader-v0": "*",
			"fabric-language-kotlin": "*",
			"cloth-config": "*"
		}
	}`)

	// Client-only mod
	createTestJarWithFabricModJSON(t, filepath.Join(dir, "mods", "client-hud.jar"), `{
		"schemaVersion": 1,
		"id": "client_hud",
		"environment": "client"
	}`)

	// Mod bundling its dependency via JiJ in META-INF/jars/
	nestedJar := createNestedJarBytes(t, map[string]string{
		"fabric.mod.json": `{"schemaVersion": 1, "id": "bundled_lib", "provides": ["bundled_alias"]}`,
	})
	jijPath := filepath.Join(dir, "mods", "jij-parent.jar")
	if err := os.MkdirAll(filepath.Dir(jijPath), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(jijPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w1, _ := zw.Create("fabric.mod.json")
	_, _ = w1.Write([]byte(`{"schemaVersion":1,"id":"jij_parent","depends":{"bundled_alias":"*"}}`))
	w2, _ := zw.Create("META-INF/jars/bundled_lib.jar")
	_, _ = w2.Write(nestedJar)
	_ = zw.Close()
	_ = f.Close()

	// Wrong Paper plugin dropped into Fabric mods/
	createTestZipEntries(t, filepath.Join(dir, "mods", "paper-only.jar"), map[string]string{
		"paper-plugin.yml": "name: PaperOnly\nversion: 1.0\napi-version: \"1.21\"\n",
	})

	list, err := ListInstalledAddons(inst)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]models.InstalledAddon)
	for _, a := range list {
		byName[a.Name] = a
	}

	ex := byName["example-mod.jar"]
	for _, wantDep := range []string{"fabric-api", "fabric-language-kotlin", "cloth-config"} {
		if !slices.Contains(ex.MissingDeps, wantDep) {
			t.Errorf("expected example-mod.jar MissingDeps to contain %q, got %v", wantDep, ex.MissingDeps)
		}
	}
	if len(byName["client-hud.jar"].Warnings) == 0 {
		t.Errorf("expected client-only warning on client-hud.jar")
	}
	if len(byName["jij-parent.jar"].MissingDeps) != 0 {
		t.Errorf("expected JiJ bundled dep to satisfy dependency, got missingDeps=%v", byName["jij-parent.jar"].MissingDeps)
	}
	if len(byName["paper-only.jar"].Warnings) == 0 {
		t.Errorf("expected loader mismatch warning on paper-only.jar in Fabric mods/")
	}
}

func TestAnalyzeNeoForgeAddons(t *testing.T) {
	dir := t.TempDir()
	inst := models.ServerInstance{
		ID:        "srv-nf",
		Engine:    models.EngineNeoForge,
		MCVersion: "26.2.0.95",
		Path:      dir,
	}

	nfToml := `
modLoader="javafml"
loaderVersion="[1,)"
license="MIT"

[[mods]]
modId="veinminer_enchantment"
version="2.14.0"
displayName="VeinMiner Enchantment"
description='''
Multi-line description mentioning [[mods]] safely
'''

[[dependencies.veinminer_enchantment]]
    modId="neoforge"
    type="required"
    side="BOTH"

[[dependencies.veinminer_enchantment]]
    modId="veinminer"
    type="required"
    side="BOTH"

[[dependencies.veinminer_enchantment]]
    modId="client_only_hud"
    type="required"
    side="CLIENT"

[[dependencies.veinminer_enchantment]]
    modId="jei"
    type="optional"
    side="BOTH"
`
	createTestZipEntries(t, filepath.Join(dir, "mods", "veinminer-enchant.jar"), map[string]string{
		"META-INF/neoforge.mods.toml": nfToml,
	})

	// NeoForge JAR with JarJar bundled dep
	nestedJar := createNestedJarBytes(t, map[string]string{
		"META-INF/neoforge.mods.toml": "[[mods]]\nmodId=\"inner_lib\"\nversion=\"1.0\"\n",
	})
	jjJarPath := filepath.Join(dir, "mods", "jarjar-mod.jar")
	jf, err := os.Create(jjJarPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(jf)
	wm, _ := zw.Create("META-INF/neoforge.mods.toml")
	_, _ = wm.Write([]byte("[[mods]]\nmodId=\"jj_parent\"\n[[dependencies.jj_parent]]\nmodId=\"inner_lib\"\ntype=\"required\"\n"))
	wj, _ := zw.Create("META-INF/jarjar/metadata.json")
	_, _ = wj.Write([]byte(`{"jars":[{"path":"META-INF/jarjar/inner_lib.jar"}]}`))
	wn, _ := zw.Create("META-INF/jarjar/inner_lib.jar")
	_, _ = wn.Write(nestedJar)
	_ = zw.Close()
	_ = jf.Close()

	list, err := ListInstalledAddons(inst)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]models.InstalledAddon)
	for _, a := range list {
		byName[a.Name] = a
	}

	ve := byName["veinminer-enchant.jar"]
	if !reflect.DeepEqual(ve.MissingDeps, []string{"veinminer"}) {
		t.Fatalf("expected MissingDeps=[veinminer] (ignoring optional & CLIENT), got %v", ve.MissingDeps)
	}
	if len(byName["jarjar-mod.jar"].MissingDeps) != 0 {
		t.Fatalf("expected JarJar inner_lib to satisfy dep, got %v", byName["jarjar-mod.jar"].MissingDeps)
	}
}

func TestAnalyzePaperAndPurpurAddons(t *testing.T) {
	dir := t.TempDir()
	inst := models.ServerInstance{
		ID:        "srv-paper",
		Engine:    models.EnginePaper,
		MCVersion: "26.2",
		Path:      dir,
	}

	// Universal multi-platform plugin requiring Veinminer via paper-plugin.yml AND fabric-language-kotlin via fabric.mod.json.
	// On Paper, only Veinminer must be checked!
	createTestZipEntries(t, filepath.Join(dir, "plugins", "veinminer-enchant.jar"), map[string]string{
		"fabric.mod.json": `{"schemaVersion":1,"id":"veinminer_enchantment","depends":{"fabric-language-kotlin":"*","veinminer":"*"}}`,
		"paper-plugin.yml": `
api-version: "1.21"
name: Veinminer_Enchantment
version: 2.14.0
dependencies:
  server:
    Veinminer:
      load: BEFORE
      required: true
    OptionalHelper:
      required: false
`,
	})

	// Bukkit plugin needing Vault and ProtocolLib (block + inline depend, softdepend ignored)
	createTestZipEntries(t, filepath.Join(dir, "plugins", "shop.jar"), map[string]string{
		"plugin.yml": "name: ShopPlugin\nversion: 2.0\napi-version: \"1.21\"\ndepend: [Vault, ProtocolLib]\nsoftdepend: [PlaceholderAPI]\n",
	})

	// VaultUnlocked provides Vault
	createTestZipEntries(t, filepath.Join(dir, "plugins", "vaultunlocked.jar"), map[string]string{
		"plugin.yml": "name: VaultUnlocked\nversion: 2.9\napi-version: \"1.21\"\nprovides:\n  - Vault\n",
	})

	// Disabled plugin placed in mods/ (wrong folder on Paper)
	createTestZipEntries(t, filepath.Join(dir, "mods", "lost.jar"), map[string]string{
		"plugin.yml": "name: Lost\nversion: 1.0\n",
	})

	list, err := ListInstalledAddons(inst)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]models.InstalledAddon)
	for _, a := range list {
		byName[a.Name] = a
	}

	if !reflect.DeepEqual(byName["veinminer-enchant.jar"].MissingDeps, []string{"Veinminer"}) {
		t.Fatalf("expected Paper veinminer-enchant MissingDeps=[Veinminer] (not fabric-language-kotlin!), got %v", byName["veinminer-enchant.jar"].MissingDeps)
	}
	if !reflect.DeepEqual(byName["shop.jar"].MissingDeps, []string{"ProtocolLib"}) {
		t.Fatalf("expected VaultUnlocked (provides: Vault) to satisfy Vault leaving only ProtocolLib missing, got %v", byName["shop.jar"].MissingDeps)
	}
	if len(byName["lost.jar"].Warnings) == 0 {
		t.Fatalf("expected warning for JAR placed in mods/ on Paper")
	}

	// Now install Veinminer plugin and ensure missing dep clears
	createTestZipEntries(t, filepath.Join(dir, "plugins", "veinminer-paper.jar"), map[string]string{
		"paper-plugin.yml": "api-version: \"1.21\"\nname: Veinminer\nversion: 2.14.0\n",
	})
	list2, err := ListInstalledAddons(inst)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list2 {
		if a.Name == "veinminer-enchant.jar" && len(a.MissingDeps) != 0 {
			t.Fatalf("expected veinminer-enchant MissingDeps to clear once veinminer-paper.jar is present, got %v", a.MissingDeps)
		}
	}
}

func TestAnalyzeVanillaAndDatapacks(t *testing.T) {
	dir := t.TempDir()
	inst := models.ServerInstance{
		ID:          "srv-van",
		Engine:      models.EngineVanilla,
		MCVersion:   "26.2",
		ActiveWorld: "world",
		Path:        dir,
	}

	createTestZipEntries(t, filepath.Join(dir, "world", "datapacks", "good.zip"), map[string]string{
		"pack.mcmeta": "\xEF\xBB\xBF" + `{"pack":{"pack_format":61,"description":"ok"}}`,
	})
	createTestZipEntries(t, filepath.Join(dir, "world", "datapacks", "bad.zip"), map[string]string{
		"nested/pack.mcmeta": `{"pack":{"pack_format":61}}`,
	})
	createTestJarWithFabricModJSON(t, filepath.Join(dir, "world", "datapacks", "wrong.jar"), `{"schemaVersion":1,"id":"wrong"}`)

	list, err := ListInstalledAddons(inst)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]models.InstalledAddon)
	for _, a := range list {
		byName[a.Name] = a
	}

	if len(byName["good.zip"].Warnings) != 0 {
		t.Errorf("expected zero warnings on good.zip, got %v", byName["good.zip"].Warnings)
	}
	if len(byName["bad.zip"].Warnings) == 0 {
		t.Errorf("expected warning on bad.zip without root pack.mcmeta")
	}
	if len(byName["wrong.jar"].Warnings) == 0 {
		t.Errorf("expected warning on wrong.jar inside datapacks/")
	}

	if _, err := InstallUploadedAddon(inst, "mod.jar", strings.NewReader("fake"), "mod"); err == nil {
		t.Fatalf("expected Vanilla to reject .jar addon upload")
	}
}

func TestBedrockMcaddonNestedDeleteAndPNX(t *testing.T) {
	dir := t.TempDir()
	bdsInst := models.ServerInstance{
		ID:          "srv-bds",
		Engine:      models.EngineBDS,
		MCVersion:   "latest",
		ActiveWorld: "Bedrock level",
		Path:        dir,
	}

	rpUUID := "11111111-1111-1111-1111-111111111111"
	bpUUID := "22222222-2222-2222-2222-222222222222"
	rpBytes := createNestedJarBytes(t, map[string]string{
		"manifest.json": "\xEF\xBB\xBF// resource pack comment\n" + `{
			"format_version": 2,
			"header": {"name": "RP Pack", "uuid": "` + rpUUID + `", "version": [1, 0, 0]},
			"modules": [{"type": "resources", "uuid": "33333333-3333-3333-3333-333333333333", "version": [1,0,0]}]
		}`,
	})
	bpBytes := createNestedJarBytes(t, map[string]string{
		"manifest.json": `{
			"format_version": 2,
			"header": {"name": "BP Pack", "uuid": "` + bpUUID + `", "version": [1, 0, 0]},
			"modules": [{"type": "data", "uuid": "44444444-4444-4444-4444-444444444444", "version": [1,0,0]}],
			"dependencies": [{"uuid": "` + rpUUID + `", "version": [1, 0, 0]}]
		}`,
	})

	mcaddonPath := filepath.Join(t.TempDir(), "bundle.mcaddon")
	mf, err := os.Create(mcaddonPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(mf)
	wr, _ := zw.Create("pack_rp.mcpack")
	_, _ = wr.Write(rpBytes)
	wb, _ := zw.Create("pack_bp.mcpack")
	_, _ = wb.Write(bpBytes)
	_ = zw.Close()
	_ = mf.Close()

	in, err := os.Open(mcaddonPath)
	if err != nil {
		t.Fatal(err)
	}
	_, destDirs, _, err := InstallUploadedAddonWithPath(bdsInst, "Bundle.MCADDON", in, "")
	in.Close()
	if err != nil {
		t.Fatalf("InstallUploadedAddonWithPath failed: %v", err)
	}
	if len(destDirs) != 2 {
		t.Fatalf("expected 2 extracted packs from nested .mcaddon, got %d (%v)", len(destDirs), destDirs)
	}

	list, err := ListInstalledAddons(bdsInst)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 installed Bedrock packs, got %d", len(list))
	}
	var rpRel string
	for _, a := range list {
		if len(a.MissingDeps) != 0 {
			t.Errorf("expected satisfied Bedrock dependencies when both RP+BP present, got missingDeps=%v", a.MissingDeps)
		}
		if a.Kind == "bedrock-resource" {
			rpRel = a.Path
		}
	}

	// Deleting resource pack must remove it from world_resource_packs.json AND cause BP to report missing dep UUID
	if err := DeleteAddon(bdsInst, rpRel); err != nil {
		t.Fatal(err)
	}
	rawJSON, err := os.ReadFile(filepath.Join(dir, "worlds", "Bedrock level", "world_resource_packs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []bedrockWorldPackEntry
	if err := json.Unmarshal(rawJSON, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected DeleteAddon to unregister UUID from world_resource_packs.json, got %v", entries)
	}

	listAfter, _ := ListInstalledAddons(bdsInst)
	if len(listAfter) != 1 || !slices.Contains(listAfter[0].MissingDeps, rpUUID) {
		t.Fatalf("expected BP to report missing RP UUID %s after RP deletion, got %+v", rpUUID, listAfter)
	}

	// PowerNukkitX: .mcpack goes to resource_packs/, and Paper plugin in plugins/ triggers warning
	pnxDir := t.TempDir()
	pnxInst := models.ServerInstance{ID: "srv-pnx", Engine: models.EnginePowerNukkitX, MCVersion: "latest", Path: pnxDir}
	if _, _, _, err := InstallUploadedAddonWithPath(pnxInst, "rp.mcpack", bytes.NewReader(rpBytes), ""); err != nil {
		t.Fatal(err)
	}
	createTestZipEntries(t, filepath.Join(pnxDir, "plugins", "paper-plugin.jar"), map[string]string{
		"plugin.yml": "name: WrongJavaPlugin\nversion: 1.0\napi-version: \"1.21\"\n",
	})
	pnxAddons, err := ListInstalledAddons(pnxInst)
	if err != nil {
		t.Fatal(err)
	}
	var sawRP, sawPluginWarn bool
	for _, a := range pnxAddons {
		if a.Kind == "bedrock-resource" {
			sawRP = true
		}
		if a.Name == "paper-plugin.jar" && len(a.Warnings) > 0 {
			sawPluginWarn = true
		}
	}
	if !sawRP || !sawPluginWarn {
		t.Fatalf("expected PNX .mcpack in resource_packs (sawRP=%v) and Bukkit plugin warning (sawPluginWarn=%v)", sawRP, sawPluginWarn)
	}
}
