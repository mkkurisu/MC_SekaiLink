package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseJavaMajor(t *testing.T) {
	cases := map[string]int{
		"25.0.1":    25,
		"21.0.13.1": 21,
		"17.0.12":   17,
		"1.8.0_402": 8,
		"25-ea":     25,
	}
	for input, want := range cases {
		if got := ParseJavaMajor(input); got != want {
			t.Errorf("ParseJavaMajor(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestRequiredJavaMajor(t *testing.T) {
	cases := map[string]int{
		"26.3":    25,
		"26.2":    25,
		"26.1.2":  25,
		"1.21.11": 21,
		"1.21.4":  21,
		"1.20.6":  21,
		"1.20.4":  17,
		"1.18.2":  17,
		"1.17.1":  16,
		"latest":  21,
	}
	for mcVer, want := range cases {
		if got := RequiredJavaMajor(mcVer); got != want {
			t.Errorf("RequiredJavaMajor(%q) = %d, want %d", mcVer, got, want)
		}
	}
}

func TestJavaEnvForBinary(t *testing.T) {
	t.Setenv("SEKAILINK_PLATFORM", "android")
	jreRoot := filepath.Join(t.TempDir(), "jre25")
	if err := os.MkdirAll(filepath.Join(jreRoot, "lib", "jli"), 0o755); err != nil {
		t.Fatal(err)
	}
	javaBin := filepath.Join(jreRoot, "bin", "java")
	env := javaEnvForBinary(javaBin)
	if len(env) == 0 {
		t.Fatalf("expected non-empty env for Android java binary")
	}
	var ldVal, homeVal string
	for _, kv := range env {
		if strings.HasPrefix(kv, "LD_LIBRARY_PATH=") {
			ldVal = strings.TrimPrefix(kv, "LD_LIBRARY_PATH=")
		} else if strings.HasPrefix(kv, "JAVA_HOME=") {
			homeVal = strings.TrimPrefix(kv, "JAVA_HOME=")
		}
	}
	if homeVal != jreRoot {
		t.Fatalf("expected JAVA_HOME=%q, got %q", jreRoot, homeVal)
	}
	for _, wantSub := range []string{
		filepath.Join(jreRoot, "lib"),
		filepath.Join(jreRoot, "lib", "jli"),
		filepath.Join(jreRoot, "lib", "server"),
	} {
		if !strings.Contains(ldVal, wantSub) {
			t.Fatalf("expected LD_LIBRARY_PATH %q to contain %q", ldVal, wantSub)
		}
	}
}
