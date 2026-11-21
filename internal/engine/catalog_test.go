package engine

import (
	"reflect"
	"testing"
)

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
