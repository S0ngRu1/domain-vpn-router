package app

import "testing"

func TestNormalizeMode(t *testing.T) {
	tests := map[string]Mode{
		"":                ModeAuto,
		"auto":            ModeAuto,
		"tyty":            ModeTyty,
		"globalprotect":   ModeGlobalProtect,
		"gp":              ModeGlobalProtect,
		"direct":          ModeDirect,
		"unexpected-mode": ModeAuto,
	}
	for input, want := range tests {
		if got := NormalizeMode(input); got != want {
			t.Fatalf("NormalizeMode(%q)=%s, want %s", input, got, want)
		}
	}
}
