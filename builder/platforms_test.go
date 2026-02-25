package newsbuilder

import (
	"path/filepath"
	"testing"
)

// TestKnownPlatforms verifies that KnownPlatforms returns the full canonical
// set in a deterministic order with no duplicates.
func TestKnownPlatforms(t *testing.T) {
	got := KnownPlatforms()
	want := []string{"linux", "mac", "mac-arm64", "win", "android", "ios"}
	if len(got) != len(want) {
		t.Fatalf("KnownPlatforms() returned %d items; want %d: %v", len(got), len(want), got)
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("KnownPlatforms()[%d] = %q; want %q", i, got[i], p)
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
			name:     "linux is an alias for the default tree",
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
