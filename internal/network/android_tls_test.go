package network

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitTLSRootCAsWithPaths(t *testing.T) {
	tmp := t.TempDir()
	androidDir := filepath.Join(tmp, "apex-cacerts")
	emptyDir := filepath.Join(tmp, "empty-cacerts")
	if err := os.MkdirAll(androidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(androidDir, "root.0"), []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. No standard Linux cert file + existing Android CA dir -> sets SSL_CERT_DIR
	t.Setenv("SSL_CERT_FILE", "")
	t.Setenv("SSL_CERT_DIR", "")
	initTLSRootCAsWithPaths("linux", []string{filepath.Join(tmp, "missing.crt")}, []string{emptyDir, androidDir})
	if got := os.Getenv("SSL_CERT_DIR"); got != androidDir {
		t.Fatalf("expected SSL_CERT_DIR=%q, got %q", androidDir, got)
	}

	// 2. Existing standard Linux cert file -> leaves SSL_CERT_DIR empty
	linuxBundle := filepath.Join(tmp, "ca-certificates.crt")
	if err := os.WriteFile(linuxBundle, []byte("bundle"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", "")
	t.Setenv("SSL_CERT_DIR", "")
	initTLSRootCAsWithPaths("linux", []string{linuxBundle}, []string{androidDir})
	if got := os.Getenv("SSL_CERT_DIR"); got != "" {
		t.Fatalf("expected SSL_CERT_DIR empty when standard bundle exists, got %q", got)
	}

	// 3. SSL_CERT_FILE already set -> leaves SSL_CERT_DIR untouched
	t.Setenv("SSL_CERT_FILE", "/custom/ca.pem")
	t.Setenv("SSL_CERT_DIR", "")
	initTLSRootCAsWithPaths("linux", []string{filepath.Join(tmp, "missing.crt")}, []string{androidDir})
	if got := os.Getenv("SSL_CERT_DIR"); got != "" {
		t.Fatalf("expected SSL_CERT_DIR empty when SSL_CERT_FILE is preset, got %q", got)
	}
}
