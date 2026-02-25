// Package newsbuilder — platform and release-status enumeration helpers.
package newsbuilder

import "path/filepath"

// KnownPlatforms returns the canonical set of non-default platform keys in a
// deterministic order.  The unnamed default tree (the top-level data directory)
// is represented by the empty string ("") and is added as a separate first
// entry by the build loop.  "linux" is treated as a first-class platform with
// its own data sub-directory (data/linux/<status>/) so that per-status Linux
// feeds are distinct from both each other and the default tree.
func KnownPlatforms() []string {
	return []string{"linux", "mac", "mac-arm64", "win", "android", "ios"}
}

// KnownStatuses returns the canonical set of supported release-status keys in
// a deterministic order.
func KnownStatuses() []string {
	return []string{"stable", "beta", "rc", "alpha"}
}

// PlatformDataDir returns the data sub-directory for (dataRoot, platform,
// status).  When platform is empty the top-level dataRoot is returned
// unchanged, preserving the default (unnamed) feed path.  All named
// platforms — including "linux" — map to dataRoot/<platform>/<status>/,
// allowing per-status Linux feeds to be distinct from each other and from
// the default tree.
func PlatformDataDir(dataRoot, platform, status string) string {
	if platform == "" {
		return dataRoot
	}
	return filepath.Join(dataRoot, platform, status)
}
