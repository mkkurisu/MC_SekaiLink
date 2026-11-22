package engine

import (
	"reflect"
	"testing"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func TestEffectiveCrossPlayMCVersion(t *testing.T) {
	if got := effectiveCrossPlayMCVersion(models.EngineNeoForge, "21.4.124-beta"); got != "1.21.4" {
		t.Fatalf("expected 1.21.4, got %q", got)
	}
	if got := effectiveCrossPlayMCVersion(models.EngineNeoForge, "21.0.167-beta"); got != "1.21" {
		t.Fatalf("expected 1.21, got %q", got)
	}
	if got := effectiveCrossPlayMCVersion(models.EngineFabric, "26.2"); got != "26.2" {
		t.Fatalf("expected 26.2, got %q", got)
	}
	if got := effectiveCrossPlayMCVersion(models.EnginePaper, "1.21.4"); got != "1.21.4" {
		t.Fatalf("expected 1.21.4, got %q", got)
	}
}

func TestExtractSortedPaperVersions(t *testing.T) {
	groups := map[string][]string{
		"1.20": {"1.20.4", "1.20.6"},
		"1.21": {"1.21", "1.21.4", "1.21.11-rc3", "1.21.11", "1.21.9-pre4"},
		"26.1": {"26.1.1", "26.1.2"},
		"26.2": {"26.2-rc-2", "26.2"},
		"26.3": {"26.3-rc-3", "26.3"},
	}

	got := extractSortedPaperVersions(groups)
	want := []string{
		"26.3",
		"26.2",
		"26.1.2",
		"26.1.1",
		"1.21.11",
		"1.21.4",
		"1.21",
		"1.20.6",
		"1.20.4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected sorted versions:\n got: %v\nwant: %v", got, want)
	}
}
