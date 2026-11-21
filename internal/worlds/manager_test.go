package worlds

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func TestExtractZipSlipBlocked(t *testing.T) {
	dir := t.TempDir()
	malZip := filepath.Join(dir, "evil.zip")
	f, err := os.Create(malZip)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("pwned"))
	_ = zw.Close()
	_ = f.Close()

	target := filepath.Join(dir, "extract")
	if err := ExtractZipSafe(malZip, target); err == nil {
		t.Fatalf("expected zip-slip error, got nil")
	}
}

func TestWorldImportBackupRestore(t *testing.T) {
	root := t.TempDir()
	inst := models.ServerInstance{
		ID:          "srv-w1",
		Engine:      models.EnginePaper,
		Path:        filepath.Join(root, "srv"),
		ActiveWorld: "world",
	}
	_ = os.MkdirAll(inst.Path, 0o755)

	// Create valid sample world zip
	sampleZip := filepath.Join(root, "sample.zip")
	f, _ := os.Create(sampleZip)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("my_adventure/level.dat")
	_, _ = w.Write([]byte("NBT_DUMMY"))
	_ = zw.Close()
	_ = f.Close()

	name, err := ImportWorld(inst, sampleZip, "adventure")
	if err != nil {
		t.Fatalf("ImportWorld failed: %v", err)
	}
	if name != "adventure" {
		t.Fatalf("expected world name adventure, got %s", name)
	}

	bkp, err := CreateBackup(inst, "adventure")
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}
	if bkp.Filename == "" {
		t.Fatalf("expected backup filename")
	}

	_ = os.RemoveAll(filepath.Join(inst.Path, "adventure"))
	if err := RestoreBackup(inst, bkp.Filename); err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(inst.Path, "adventure", "level.dat")); err != nil {
		t.Fatalf("expected restored level.dat: %v", err)
	}
}
