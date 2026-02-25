package newsbuilder

import (
	"path/filepath"
	"testing"
)

// TestKnownPlatforms verifies that KnownPlatforms returns the full canonical
// set in a deterministic order with no duplicates, and that "linux" is NOT
// included because it maps to the same data directory as the default ("") entry
// and would produce redundant, overwriting build iterations.
func TestKnownPlatforms(t *testing.T) {
	got := KnownPlatforms()
	want := []string{"mac", "mac-arm64", "win", "android", "ios"}
	if len(got) != len(want) {
		t.Fatalf("KnownPlatforms() returned %d items; want %d: %v", len(got), len(want), got)
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("KnownPlatforms()[%d] = %q; want %q", i, got[i], p)
		}
	}
	// Explicitly confirm "linux" is absent — it is covered by the default ("") tree.
	for _, p := range got {
		if p == "linux" {
			t.Error("KnownPlatforms() must not include \"linux\": it is an alias for the default tree and would cause duplicate builds")
		}
	}
}

// TestKnownStatuses verifies that KnownStatuses returns the full canonical set
// in a deterministic order with no duplicates.
func TestKnownStatuses(t *testing.T) {
	got := KnownStatuses()
	want := []string{"stable", "beta", "rc", "alpha"}
	if len(got) != len(want) {
		t.Fatalf("KnownStatuses() returned %d items; want %d: %v", len(got), len(want), got)
	}
	for i, s := range want {
		if got[i] != s {
			t.Errorf("KnownStatuses()[%d] = %q; want %q", i, got[i], s)
		}
	}
}

// TestPlatformDataDir covers the three routing cases: empty platform, "linux"
// (alias for the default tree), and a named non-Linux platform.
func TestPlatformDataDir(t *testing.T) {
	tests := []struct {
		name     string
		dataRoot string
		platform string
		status   string
		want     string
	}{
		{
			name:     "empty platform returns dataRoot unchanged",
			dataRoot: "data",
			platform: "",
			status:   "stable",
			want:     "data",
		},
		{
			// PlatformDataDir still accepts "linux" as a caller-supplied value for
			// backward compatibility; it maps to the same top-level dataRoot so
			// that explicit callers are not broken.  KnownPlatforms() no longer
			// yields "linux" so the build loop never generates this pair itself.
			name:     "linux maps to dataRoot for backward compat",
			dataRoot: "data",
			platform: "linux",
			status:   "stable",
			want:     "data",
		},
		{
			name:     "mac/stable sub-directory",
			dataRoot: "data",
			platform: "mac",
			status:   "stable",
			want:     filepath.Join("data", "mac", "stable"),
		},
		{
			name:     "mac-arm64/beta sub-directory",
			dataRoot: "data",
			platform: "mac-arm64",
			status:   "beta",
			want:     filepath.Join("data", "mac-arm64", "beta"),
		},
		{
			name:     "win/beta sub-directory",
			dataRoot: "data",
			platform: "win",
			status:   "beta",
			want:     filepath.Join("data", "win", "beta"),
		},
		{
			name:     "android/stable sub-directory (future platform)",
			dataRoot: "data",
			platform: "android",
			status:   "stable",
			want:     filepath.Join("data", "android", "stable"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlatformDataDir(tt.dataRoot, tt.platform, tt.status)
			if got != tt.want {
				t.Errorf("PlatformDataDir(%q, %q, %q) = %q; want %q",
					tt.dataRoot, tt.platform, tt.status, got, tt.want)
			}
		})
	}
}
