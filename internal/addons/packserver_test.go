package addons

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func TestSaveAndApplyPackSHA1(t *testing.T) {
	srvDir := t.TempDir()
	inst := models.ServerInstance{
		ID:     "srv-p1",
		Engine: models.EnginePaper,
		Path:   srvDir,
	}

	pack, err := SavePackFile(inst, "sekai-shaders.zip", bytes.NewReader([]byte("dummy-pack-bytes")))
	if err != nil {
		t.Fatalf("SavePackFile failed: %v", err)
	}
	if len(pack.SHA1) != 40 {
		t.Fatalf("expected 40-char sha1, got %q", pack.SHA1)
	}

	props, err := ApplyResourcePack(inst, "sekai-shaders.zip", "http://127.0.0.1:25580", true)
	if err != nil {
		t.Fatalf("ApplyResourcePack failed: %v", err)
	}
	if props["resource-pack-sha1"] != pack.SHA1 {
		t.Fatalf("sha1 mismatch: %s vs %s", props["resource-pack-sha1"], pack.SHA1)
	}
	if props["require-resource-pack"] != "true" {
		t.Fatalf("expected require-resource-pack=true")
	}

	mrpackOut := filepath.Join(t.TempDir(), "out.mrpack")
	if err := ExportClientMRPack(inst, mrpackOut); err != nil {
		t.Fatalf("ExportClientMRPack failed: %v", err)
	}
	if _, err := os.Stat(mrpackOut); err != nil {
		t.Fatalf("mrpack file missing: %v", err)
	}
}
