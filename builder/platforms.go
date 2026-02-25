// Package newsbuilder — platform and release-status enumeration helpers.
package newsbuilder

import "path/filepath"

// KnownPlatforms returns the canonical set of supported platform keys in a
// deterministic order.  The empty string ("") represents the top-level
// default (Linux) tree and is handled separately by callers.
func KnownPlatforms() []string {
	return []string{"linux", "mac", "mac-arm64", "win", "android", "ios"}
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
