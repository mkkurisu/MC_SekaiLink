package engine

import "testing"

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
