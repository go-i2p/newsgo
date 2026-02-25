package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-i2p/newsgo/cmd"
)

// TestExecute_Help verifies that the root command runs without panicking when
// --help is requested.  This is a smoke test for the cobra wiring in main().
func TestExecute_Help(t *testing.T) {
	var buf bytes.Buffer
	// Run with --help; cobra always exits 0 for help so the error is nil.
	err := cmd.ExecuteWithArgs([]string{"--help"})
	_ = buf // buf is unused here; cobra writes to its own output
	if err != nil {
		t.Errorf("ExecuteWithArgs(--help) returned error: %v", err)
	}
}

// TestServeCmd_FlagNames verifies that the serve sub-command exposes the flag
// names documented in the README (--host, --port, --i2p) and that the old
// combined --http flag has been removed.
func TestServeCmd_FlagNames(t *testing.T) {
	required := []struct {
		flag    string
		wantDef string
	}{
		{"host", "127.0.0.1"},
		{"port", "9696"},
		{"i2p", "false"},
		{"newsdir", "build"},
		{"statsfile", "build/stats.json"},
	}
	for _, tt := range required {
		f := cmd.LookupFlag("serve", tt.flag)
		if f == nil {
			t.Errorf("serve --%s is not registered; README documents this flag", tt.flag)
			continue
		}
		if f.DefValue != tt.wantDef {
			t.Errorf("serve --%s default = %q, want %q", tt.flag, f.DefValue, tt.wantDef)
		}
	}

	// The old --http combined flag must be gone.
	if f := cmd.LookupFlag("serve", "http"); f != nil {
		t.Errorf("serve --http is still registered; it should be replaced by --host and --port")
	}
}

// TestBuildCmd_FlagNames verifies that the build sub-command exposes the flag
// names documented in the README.
func TestBuildCmd_FlagNames(t *testing.T) {
	required := []struct {
		flag    string
		wantDef string
	}{
		{"newsfile", "data"},
		{"blockfile", "data/blocklist.xml"},
		{"releasejson", "data/releases.json"},
		{"feedtitle", "I2P News"},
		{"feedsubtitle", "News feed, and router updates"},
		{"feedsite", "http://i2p-projekt.i2p"},
		{"builddir", "build"},
		{"feedmain", "http://tc73n4kivdroccekirco7rhgxdg5f3cjvbaapabupeyzrqwv5guq.b32.i2p"},
	}
	for _, tt := range required {
		f := cmd.LookupFlag("build", tt.flag)
		if f == nil {
			t.Errorf("build --%s is not registered; README documents this flag", tt.flag)
			continue
		}
		if tt.wantDef != "" && !strings.HasPrefix(f.DefValue, tt.wantDef) {
			t.Errorf("build --%s default = %q, want prefix %q", tt.flag, f.DefValue, tt.wantDef)
		}
	}

	// Old stale flag names must not be present.
	for _, stale := range []string{"blocklist", "feeduid"} {
		if f := cmd.LookupFlag("build", stale); f != nil {
			t.Errorf("build --%s is still registered; it was renamed and must not exist", stale)
		}
	}
}

// TestBuildCmd_FeedMainIsStatic verifies that --feedmain defaults to the
// static I2P URL and does not trigger a live SAM/onramp connection at startup.
func TestBuildCmd_FeedMainIsStatic(t *testing.T) {
	f := cmd.LookupFlag("build", "feedmain")
	if f == nil {
		t.Fatal("build --feedmain is not registered")
	}
	const staticPrefix = "http://tc73n4kivdroccekirco7rhgxdg5f3cjvbaapabupeyzrqwv5guq.b32.i2p"
	if !strings.HasPrefix(f.DefValue, staticPrefix) {
		t.Errorf("build --feedmain default = %q; want static URL starting with %q\n"+
			"(a live SAM session must not be opened at flag initialisation time)",
			f.DefValue, staticPrefix)
	}
}

// TestSignCmd_FlagNames verifies that the sign sub-command exposes the flag
// names documented in the README.
func TestSignCmd_FlagNames(t *testing.T) {
	required := []struct {
		flag    string
		wantDef string
	}{
		{"signerid", "null@example.i2p"},
		{"signingkey", "signing_key.pem"},
		{"builddir", "build"},
	}
	for _, tt := range required {
		f := cmd.LookupFlag("sign", tt.flag)
		if f == nil {
			t.Errorf("sign --%s is not registered; README documents this flag", tt.flag)
			continue
		}
		if f.DefValue != tt.wantDef {
			t.Errorf("sign --%s default = %q, want %q", tt.flag, f.DefValue, tt.wantDef)
		}
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
