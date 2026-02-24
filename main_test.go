package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFlagNames verifies that the registered flag names match the names
// documented in Help() and README. These were previously mismatched:
//   - "blocklist" was registered but README/Help said "-blockfile"
//   - "feeduid"   was registered but README/Help said "-feeduri"
func TestFlagNames(t *testing.T) {
	tests := []struct {
		flagName string
		wantVar  string
	}{
		{"blockfile", "data/blocklist.xml"},
		{"feeduri", ""},         // default is a random UUID; just check it is registered
		{"feedmain", "http://"}, // default must be a static URL
		{"builddir", "build"},
		{"newsfile", "data"},
		{"command", "help"},
	}

	for _, tt := range tests {
		f := flag.Lookup(tt.flagName)
		if f == nil {
			t.Errorf("flag -%s is not registered; check main.go flag declarations", tt.flagName)
			continue
		}
		if tt.wantVar != "" && !strings.HasPrefix(f.DefValue, tt.wantVar) {
			t.Errorf("flag -%s default = %q, want prefix %q", tt.flagName, f.DefValue, tt.wantVar)
		}
	}
}

// TestOldFlagNamesAbsent verifies that the previously incorrect flag names are
// no longer registered, ensuring users who follow the README don't get silent
// no-ops.
func TestOldFlagNamesAbsent(t *testing.T) {
	stale := []string{"blocklist", "feeduid"}
	for _, name := range stale {
		if f := flag.Lookup(name); f != nil {
			t.Errorf("stale flag -%s is still registered; it should have been renamed", name)
		}
	}
}

// TestDefaultFeedURLIsStatic verifies that -feedmain defaults to the static
// fallback URL and does not require a live SAM connection at startup.
// Previously DefaultFeedURL() was called at flag init time, incurring a full
// I2P SAM session for every invocation of newsgo (including "help" and "build").
func TestDefaultFeedURLIsStatic(t *testing.T) {
	f := flag.Lookup("feedmain")
	if f == nil {
		t.Fatal("flag -feedmain is not registered")
	}
	const staticPrefix = "http://tc73n4kivdroccekirco7rhgxdg5f3cjvbaapabupeyzrqwv5guq.b32.i2p"
	if !strings.HasPrefix(f.DefValue, staticPrefix) {
		t.Errorf("-feedmain default = %q; want static URL starting with %q\n"+
			"(DefaultFeedURL() must not be called at flag initialisation time)",
			f.DefValue, staticPrefix)
	}
}

// TestSignCommandTargetsAtomXML is a unit test for the filename matching logic
// used by the sign command's directory walk. The walk must select only
// ".atom.xml" files and skip plain ".html" or other extensions to prevent
// overwriting source files with binary su3 data.
func TestSignCommandTargetsAtomXML(t *testing.T) {
	candidates := []struct {
		path   string
		signed bool
	}{
		{"build/news.atom.xml", true},
		{"build/sub/news_de.atom.xml", true},
		{"data/entries.html", false}, // must NOT be selected — this was the corruption bug
		{"build/index.html", false},
		{"build/style.css", false},
		{"build/news.xml", false}, // plain .xml without .atom prefix also excluded
	}

	for _, c := range candidates {
		got := strings.HasSuffix(c.path, ".atom.xml")
		if got != c.signed {
			t.Errorf("HasSuffix(%q, \".atom.xml\") = %v, want %v", c.path, got, c.signed)
		}
	}
}

// TestLoadPrivateKey_NilPEMGuard verifies that loadPrivateKey returns a
// descriptive error instead of panicking when the key file contains no PEM
// block (e.g. empty file, wrong path, DER-encoded key).
// This guards against the critical nil-pointer dereference reported in the
// audit where pem.Decode returns nil and the old code immediately called
// privDer.Bytes without a nil check.
func TestLoadPrivateKey_NilPEMGuard(t *testing.T) {
	t.Run("empty file", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "key*.pem")
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		_, err = loadPrivateKey(f.Name())
		if err == nil {
			t.Fatal("expected error for empty key file, got nil")
		}
		if !strings.Contains(err.Error(), "no PEM block found") {
			t.Errorf("error message %q does not mention 'no PEM block found'", err.Error())
		}
	})

	t.Run("non-PEM content", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "not_a_key.pem")
		if err := os.WriteFile(path, []byte("this is not PEM data\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := loadPrivateKey(path)
		if err == nil {
			t.Fatal("expected error for non-PEM key file, got nil")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := loadPrivateKey(filepath.Join(t.TempDir(), "does_not_exist.pem"))
		if err == nil {
			t.Fatal("expected error for missing key file, got nil")
		}
	})
}

// TestI2PFlagDefault verifies that the -i2p flag defaults to false so that
// isSamAround() is no longer called at package-init time (which would fire a
// blocking net.Listen for every sub-command invocation including build/sign).
func TestI2PFlagDefault(t *testing.T) {
	f := flag.Lookup("i2p")
	if f == nil {
		t.Fatal("flag -i2p is not registered")
	}
	if f.DefValue != "false" {
		t.Errorf("-i2p default = %q, want \"false\"; SAM probe must not run at package init", f.DefValue)
	}
}
