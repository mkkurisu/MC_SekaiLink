package storage

import (
	"testing"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func TestStoreConfigAndInstances(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	cfg := st.GetConfig()
	if cfg.APIPort != 25580 || cfg.PairingToken == "" {
		t.Fatalf("unexpected default config: %+v", cfg)
	}

	updated, err := st.UpdateConfig(func(c *models.AppConfig) {
		c.RemoteEnabled = true
	})
	if err != nil || updated.BindAddress != "0.0.0.0" {
		t.Fatalf("expected bind 0.0.0.0 when remote enabled, got %q (err=%v)", updated.BindAddress, err)
	}

	inst := models.ServerInstance{
		ID:        "srv-test1",
		Name:      "Test SMP",
		Engine:    models.EnginePaper,
		MCVersion: "1.21.4",
		Status:    models.StatusOnline,
	}
	if err := st.SaveInstance(inst); err != nil {
		t.Fatalf("SaveInstance failed: %v", err)
	}

	// Re-open store and ensure transient online status resets to offline
	st2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reopen NewStore failed: %v", err)
	}
	got, err := st2.GetInstance("srv-test1")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if got.Status != models.StatusOffline {
		t.Fatalf("expected status offline after restart, got %s", got.Status)
	}
}
