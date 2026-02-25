// Package newsbuilder — platform and release-status enumeration helpers.
package newsbuilder

import "path/filepath"

// KnownPlatforms returns the canonical set of non-default platform keys in a
// deterministic order.  The Linux / default tree is represented by the empty
// string ("") and is always added as a separate first entry by the build loop;
// including "linux" here would cause an additional build pass that maps to the
// identical top-level data directory, overwriting the default output with no
// change in content.
func KnownPlatforms() []string {
	return []string{"mac", "mac-arm64", "win", "android", "ios"}
}

// KnownStatuses returns the canonical set of supported release-status keys in
// a deterministic order.
func KnownStatuses() []string {
	return []string{"stable", "beta", "rc", "alpha"}
}

// PlatformDataDir returns the data sub-directory for (dataRoot, platform,
// status).  When platform is empty or "linux" the top-level dataRoot is
// returned unchanged so that the default (Linux) feed path is unaffected.
func PlatformDataDir(dataRoot, platform, status string) string {
	if platform == "" || platform == "linux" {
		return dataRoot
	}
	return filepath.Join(dataRoot, platform, status)
}
