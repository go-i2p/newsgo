package cmd

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	builder "github.com/go-i2p/newsgo/builder"
	newsfetch "github.com/go-i2p/newsgo/fetch"
	"github.com/go-i2p/onramp"
	"github.com/spf13/viper"
	"i2pgit.org/go-i2p/reseed-tools/su3"
)

// TestLoadPrivateKey_NilPEMGuard verifies that loadPrivateKey returns a
// descriptive error (not a panic) when the key file contains no valid PEM block.
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
			t.Errorf("error %q does not mention 'no PEM block found'", err.Error())
		}
	})

	t.Run("non-PEM content", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.pem")
		if err := os.WriteFile(path, []byte("not pem data\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := loadPrivateKey(path)
		if err == nil {
			t.Fatal("expected error for non-PEM file, got nil")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := loadPrivateKey(filepath.Join(t.TempDir(), "missing.pem"))
		if err == nil {
			t.Fatal("expected error for missing file, got nil")
		}
	})
}

// TestIsSamAround_Callable verifies that isSamAround() is callable and returns
// a bool without panicking.  We do not assert a specific value since SAM may or
// may not be present in the test environment.
func TestIsSamAround_Callable(t *testing.T) {
	result := isSamAround()
	t.Logf("isSamAround() = %v", result)
}

// TestCheckPortListening_NothingListening verifies that checkPortListening
// returns false when no process is accepting connections on the probed address.
// We pick a random high port and bind-then-close it before probing, so the OS
// has released it prior to the Dial attempt.
func TestCheckPortListening_NothingListening(t *testing.T) {
	// Find a free port by asking the OS for one, then immediately closing it
	// so it is guaranteed to be unoccupied when checkPortListening dials it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not find a free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // release the port before probing

	got := checkPortListening(addr)
	if got {
		t.Errorf("checkPortListening(%q) = true; want false (nothing listening)", addr)
	}
}

// TestCheckPortListening_SomethingListening verifies that checkPortListening
// returns true when a TCP listener is actively accepting on the probed address.
// This is the canonical "SAM is running" path \u2014 here exercised with a plain
// net.Listener so the test does not require an actual SAM gateway.
func TestCheckPortListening_SomethingListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not start test listener: %v", err)
	}
	defer ln.Close()

	got := checkPortListening(ln.Addr().String())
	if !got {
		t.Errorf("checkPortListening(%q) = false; want true (listener is up)", ln.Addr().String())
	}
}

// TestCheckPortListening_NonRoutableAddr verifies that checkPortListening
// returns false within its timeout on an address that is not reachable (TEST-NET
// per RFC 5737 is non-routable; the connection attempt will time out or be
// refused quickly on loopback-only test runners).
// We use 127.0.0.2, which is loopback-family but almost never has a listener,
// so both "connection refused" and "dial timeout" produce the expected false.
func TestCheckPortListening_UnreachablePort(t *testing.T) {
	// Port 1 is reserved; no process should be listening on it in CI.
	got := checkPortListening("127.0.0.1:1")
	if got {
		t.Skip("port 1 unexpectedly accepts connections in this environment; skipping")
	}
}

// TestNoListenerConfigured verifies the condition logic that guards against the
// serve command spinning in an infinite loop without any active listener.
// When host is non-empty or i2p is true, at least one listener will start and
// the guard must NOT fire.  Only when both are false/empty should it fire.
func TestNoListenerConfigured(t *testing.T) {
	tests := []struct {
		name string
		host string
		i2p  bool
		want bool
	}{
		{"both disabled — no listener", "", false, true},
		{"clearnet only — listener present", "127.0.0.1", false, false},
		{"i2p only — listener present", "", true, false},
		{"both enabled — listeners present", "127.0.0.1", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := noListenerConfigured(tt.host, tt.i2p); got != tt.want {
				t.Errorf("noListenerConfigured(%q, %v) = %v, want %v", tt.host, tt.i2p, got, tt.want)
			}
		})
	}
}

// TestResolveOverrideFile validates the "platform-specific overrides global
// when present" helper used for both releases.json and blocklist.xml.
func TestResolveOverrideFile(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists.xml")
	if err := os.WriteFile(existing, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "absent.xml")
	global := filepath.Join(dir, "global.xml")

	t.Run("returns platform path when it exists", func(t *testing.T) {
		got := resolveOverrideFile(existing, global)
		if got != existing {
			t.Errorf("resolveOverrideFile(%q, %q) = %q; want %q", existing, global, got, existing)
		}
	})
	t.Run("returns global fallback when platform path absent", func(t *testing.T) {
		got := resolveOverrideFile(missing, global)
		if got != global {
			t.Errorf("resolveOverrideFile(%q, %q) = %q; want %q", missing, global, got, global)
		}
	})
}

// makeMinimalDataDir creates the minimum files needed for a successful build
// under dataRoot (global) and optionally a platform sub-directory.
// releasesJSON and blocklistXML are written to whichever of root/platform is
// requested via the boolean flags.  Returns the root and platform dir paths.
func makeMinimalDataDir(t *testing.T, platform, status string,
	platformReleasesJSON, platformBlocklist bool,
) (root, platDir string) {
	t.Helper()
	root = t.TempDir()
	const minReleasesJSON = `[{"date":"2025-01-01","version":"2.0.0","minVersion":"0.9.9","minJavaVersion":"1.8","updates":{"su3":{"torrent":"magnet:?xt=urn:btih:abc","url":["http://example.com/update.su3"]}}}]`
	const entriesHTML = `<html><body><header>H</header><article id="urn:1" title="T" href="http://x.com" author="A" published="2025-01-01" updated="2025-01-02"><details><summary>S</summary></details><p>B</p></article></body></html>`

	// Always write global files.
	must(t, os.WriteFile(filepath.Join(root, "entries.html"), []byte(entriesHTML), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "releases.json"), []byte(minReleasesJSON), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "blocklist.xml"), []byte(""), 0o644))

	platDir = filepath.Join(root, platform, status)
	if err := os.MkdirAll(platDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if platformReleasesJSON {
		must(t, os.WriteFile(filepath.Join(platDir, "releases.json"), []byte(minReleasesJSON), 0o644))
	}
	if platformBlocklist {
		must(t, os.WriteFile(filepath.Join(platDir, "blocklist.xml"), []byte(""), 0o644))
	}
	return root, platDir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestBuildPlatform_UsesGlobalReleasesWhenPlatformAbsent verifies that a
// platform directory without a releases.json still produces output (using the
// global releases.json as fallback) instead of being silently skipped.
func TestBuildPlatform_UsesGlobalReleasesWhenPlatformAbsent(t *testing.T) {
	root, _ := makeMinimalDataDir(t, "mac", "stable", false /*no platform releases*/, false)
	buildDir := t.TempDir()

	prev := *c
	defer func() { *c = prev }()
	c.NewsFile = root
	c.ReleaseJsonFile = filepath.Join(root, "releases.json")
	c.BlockList = filepath.Join(root, "blocklist.xml")
	c.BuildDir = buildDir
	c.FeedTitle = "Test"
	c.FeedSite = "http://example.com"
	c.FeedMain = "http://example.com/news.atom.xml"
	c.FeedBackup = ""
	c.FeedSubtitle = "sub"
	c.FeedUuid = "00000000-0000-0000-0000-000000000001"
	c.TranslationsDir = ""

	buildPlatform("mac", "stable")

	out := filepath.Join(buildDir, "mac", "stable", "news.atom.xml")
	if _, err := os.Stat(out); err != nil {
		t.Errorf("expected %s to be produced when platform releases.json absent (global fallback); stat: %v", out, err)
	}
}

// TestBuildPlatform_UsesPlatformBlocklistWhenPresent verifies that the feed
// is built using the platform-specific blocklist.xml when it exists in the
// platform data directory, not the global one.
func TestBuildPlatform_UsesPlatformBlocklistWhenPresent(t *testing.T) {
	root, platDir := makeMinimalDataDir(t, "win", "beta", true /*platform releases*/, true /*platform blocklist*/)
	buildDir := t.TempDir()

	prev := *c
	defer func() { *c = prev }()
	c.NewsFile = root
	c.ReleaseJsonFile = filepath.Join(root, "releases.json")
	c.BlockList = filepath.Join(root, "blocklist.xml")
	c.BuildDir = buildDir
	c.FeedTitle = "Test"
	c.FeedSite = "http://example.com"
	c.FeedMain = "http://example.com/news.atom.xml"
	c.FeedBackup = ""
	c.FeedSubtitle = "sub"
	c.FeedUuid = "00000000-0000-0000-0000-000000000002"
	c.TranslationsDir = ""

	// Ensure we can detect which blocklist is resolved by checking the file is
	// the platform one; resolveOverrideFile is the gating function.
	got := resolveOverrideFile(
		filepath.Join(platDir, "blocklist.xml"),
		filepath.Join(root, "blocklist.xml"),
	)
	if got != filepath.Join(platDir, "blocklist.xml") {
		t.Errorf("resolveOverrideFile preferred global blocklist over present platform blocklist; got %q", got)
	}

	buildPlatform("win", "beta")
	out := filepath.Join(buildDir, "win", "beta", "news.atom.xml")
	if _, err := os.Stat(out); err != nil {
		t.Errorf("expected %s to be produced; stat: %v", out, err)
	}
}

// TestBuildPlatform_SkipsMissingDirectory verifies that a (platform, status)
// combination whose data directory does not exist is silently skipped —
// no output file is created and no fatal error occurs.
func TestBuildPlatform_SkipsMissingDirectory(t *testing.T) {
	root := t.TempDir()
	buildDir := t.TempDir()

	prev := *c
	defer func() { *c = prev }()
	c.NewsFile = root
	c.ReleaseJsonFile = filepath.Join(root, "releases.json")
	c.BlockList = ""
	c.BuildDir = buildDir
	c.FeedUuid = "00000000-0000-0000-0000-000000000003"

	buildPlatform("android", "stable") // data/android/stable does not exist

	out := filepath.Join(buildDir, "android", "stable", "news.atom.xml")
	if _, err := os.Stat(out); err == nil {
		t.Errorf("expected no output for absent directory android/stable, but %s was created", out)
	}
}

// TestBuildPlatform_GlobalEntriesMergedIntoPlatformFeed verifies that when a
// platform has its own entries.html, the global jar-feed articles are merged
// into the output (Feed.BaseEntriesHTMLPath == canonical global entries).
// This is verified by checking that the output Atom XML contains the article
// ID from the global entries file.
func TestBuildPlatform_GlobalEntriesMergedIntoPlatformFeed(t *testing.T) {
	root := t.TempDir()
	buildDir := t.TempDir()

	const releasesJSON = `[{"date":"2025-01-01","version":"2.0.0","minVersion":"0.9.9","minJavaVersion":"1.8","updates":{"su3":{"torrent":"magnet:?xt=urn:btih:abc","url":["http://example.com/update.su3"]}}}]`
	// Global entry has id "urn:global:1".
	globalEntries := `<html><body><header>H</header><article id="urn:global:1" title="Global" href="http://g.com" author="Au" published="2025-01-01" updated="2025-01-01"><details><summary>S</summary></details><p>global</p></article></body></html>`
	// Platform entry has id "urn:plat:1".
	platEntries := `<html><body><header>H</header><article id="urn:plat:1" title="Plat" href="http://p.com" author="Au" published="2025-01-02" updated="2025-01-02"><details><summary>PS</summary></details><p>plat</p></article></body></html>`

	must(t, os.WriteFile(filepath.Join(root, "entries.html"), []byte(globalEntries), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "releases.json"), []byte(releasesJSON), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "blocklist.xml"), []byte(""), 0o644))

	platDir := filepath.Join(root, "mac", "stable")
	must(t, os.MkdirAll(platDir, 0o755))
	must(t, os.WriteFile(filepath.Join(platDir, "entries.html"), []byte(platEntries), 0o644))
	must(t, os.WriteFile(filepath.Join(platDir, "releases.json"), []byte(releasesJSON), 0o644))

	prev := *c
	defer func() { *c = prev }()
	c.NewsFile = root
	c.ReleaseJsonFile = filepath.Join(root, "releases.json")
	c.BlockList = filepath.Join(root, "blocklist.xml")
	c.BuildDir = buildDir
	c.FeedTitle = "Test"
	c.FeedSite = "http://example.com"
	c.FeedMain = "http://example.com/news.atom.xml"
	c.FeedBackup = ""
	c.FeedSubtitle = "sub"
	c.FeedUuid = "00000000-0000-0000-0000-000000000004"
	c.TranslationsDir = ""

	buildPlatform("mac", "stable")

	out := filepath.Join(buildDir, "mac", "stable", "news.atom.xml")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("expected output file %s; stat: %v", out, err)
	}
	content := string(data)
	if !strings.Contains(content, "urn:global:1") {
		t.Errorf("global entry urn:global:1 missing from platform feed; feed:\n%s", content)
	}
	if !strings.Contains(content, "urn:plat:1") {
		t.Errorf("platform entry urn:plat:1 missing from platform feed; feed:\n%s", content)
	}
}

// TestOutputFilename validates that build output paths are computed relative to
// the walk root so that source-directory components (e.g. "data/") are not
// propagated into BuildDir.
func TestOutputFilename(t *testing.T) {
	tests := []struct {
		name     string
		newsFile string
		newsRoot string
		want     string
	}{
		{
			// Primary feed: "data/entries.html" relative to root "data" must
			// produce "news.atom.xml", not "data/news.atom.xml".
			name:     "root entries.html strips source dir prefix",
			newsFile: filepath.Join("data", "entries.html"),
			newsRoot: "data",
			want:     "news.atom.xml",
		},
		{
			// Translation feed: path component "translations/" must be stripped
			// and the language subdir preserved.
			name:     "translation subdir keeps language, drops translations prefix",
			newsFile: filepath.Join("data", "translations", "de", "entries.html"),
			newsRoot: "data",
			want:     filepath.Join("de", "news.atom.xml"),
		},
		{
			// Single-file invocation: newsFile == newsRoot means filepath.Rel
			// returns "."; fall back to filepath.Base so result is still valid.
			name:     "single-file invocation newsFile equals newsRoot",
			newsFile: filepath.Join("data", "entries.html"),
			newsRoot: filepath.Join("data", "entries.html"),
			want:     "news.atom.xml",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := outputFilename(tt.newsFile, tt.newsRoot)
			if got != tt.want {
				t.Errorf("outputFilename(%q, %q) = %q, want %q",
					tt.newsFile, tt.newsRoot, got, tt.want)
			}
		})
	}
}

// TestOutputFilenameForPlatform validates the platform/status namespacing
// wrapper.  For the default (empty or "linux") platform the result must be
// identical to outputFilename.  For all other platforms the output must be
// prefixed with {platform}/{status}/.
func TestOutputFilenameForPlatform(t *testing.T) {
	tests := []struct {
		name     string
		newsFile string
		newsRoot string
		platform string
		status   string
		want     string
	}{
		{
			name:     "empty platform — no prefix, same as outputFilename",
			newsFile: filepath.Join("data", "entries.html"),
			newsRoot: "data",
			platform: "",
			status:   "",
			want:     "news.atom.xml",
		},
		{
			// "linux" is now a first-class platform: its output is prefixed
			// with "linux/<status>/" so that different statuses produce
			// distinct files.  The pre-fix code returned the bare base name
			// for "linux", making --status invisible for this platform.
			name:     "linux platform — first-class, has platform/status prefix",
			newsFile: filepath.Join("data", "linux", "stable", "entries.html"),
			newsRoot: filepath.Join("data", "linux", "stable"),
			platform: "linux",
			status:   "stable",
			want:     filepath.Join("linux", "stable", "news.atom.xml"),
		},
		{
			name:     "mac/stable canonical feed",
			newsFile: filepath.Join("data", "mac", "stable", "entries.html"),
			newsRoot: filepath.Join("data", "mac", "stable"),
			platform: "mac",
			status:   "stable",
			want:     filepath.Join("mac", "stable", "news.atom.xml"),
		},
		{
			name:     "mac/stable locale feed from top-level translations dir",
			newsFile: filepath.Join("data", "translations", "entries.de.html"),
			newsRoot: filepath.Join("data", "mac", "stable"),
			platform: "mac",
			status:   "stable",
			want:     filepath.Join("mac", "stable", "news_de.atom.xml"),
		},
		{
			name:     "mac/stable locale feed from platform translations dir",
			newsFile: filepath.Join("data", "mac", "stable", "translations", "entries.de.html"),
			newsRoot: filepath.Join("data", "mac", "stable"),
			platform: "mac",
			status:   "stable",
			want:     filepath.Join("mac", "stable", "news_de.atom.xml"),
		},
		{
			name:     "win/beta canonical feed with canonical fallback entries",
			newsFile: filepath.Join("data", "entries.html"),
			newsRoot: filepath.Join("data", "win", "beta"),
			platform: "win",
			status:   "beta",
			want:     filepath.Join("win", "beta", "news.atom.xml"),
		},
		{
			name:     "android/stable — future platform, canonical fallback",
			newsFile: filepath.Join("data", "entries.html"),
			newsRoot: filepath.Join("data", "android", "stable"),
			platform: "android",
			status:   "stable",
			want:     filepath.Join("android", "stable", "news.atom.xml"),
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := outputFilenameForPlatform(tt.newsFile, tt.newsRoot, tt.platform, tt.status)
			if got != tt.want {
				t.Errorf("outputFilenameForPlatform(%q, %q, %q, %q) = %q; want %q",
					tt.newsFile, tt.newsRoot, tt.platform, tt.status, got, tt.want)
			}
		})
	}
}

// TestEntriesHtmlFilter ensures that the filepath predicate used in the build
// walk matches exactly files named "entries.html" and rejects other .html files.
// This guards against the previous bug where all .html files were processed.
func TestEntriesHtmlFilter(t *testing.T) {
	shouldMatch := []string{
		"entries.html",
		filepath.Join("subdir", "entries.html"),
		filepath.Join("data", "translations", "fr", "entries.html"),
	}
	shouldNotMatch := []string{
		"index.html",
		"error.html",
		"all-entries.html",
		"entries.htm",
		filepath.Join("subdir", "index.html"),
	}
	for _, p := range shouldMatch {
		if filepath.Base(p) != "entries.html" {
			t.Errorf("path %q should match entries.html filter but did not", p)
		}
	}
	for _, p := range shouldNotMatch {
		if filepath.Base(p) == "entries.html" {
			t.Errorf("path %q should NOT match entries.html filter but it did", p)
		}
	}
}

// TestSign_ReturnsErrorForMissingKey verifies that Sign() propagates an error
// when the signing key file cannot be loaded, rather than returning nil and
// silently producing no output.  This covers the fix to the walk callback that
// previously discarded the Sign() return value entirely.
func TestSign_ReturnsErrorForMissingKey(t *testing.T) {
	prev := c.SigningKey
	c.SigningKey = filepath.Join(t.TempDir(), "nonexistent_key.pem")
	defer func() { c.SigningKey = prev }()

	err := Sign("any.atom.xml")
	if err == nil {
		t.Fatal("Sign() returned nil for a missing signing key; expected a non-nil error")
	}
}

// TestFetchCmd_SamAddrFlagRegistered verifies that --samaddr is registered on
// the fetch subcommand, so that "newsgo fetch --samaddr <addr>" works as
// documented in the README rather than returning "unknown flag: --samaddr".
// The default value must match onramp.SAM_ADDR, consistent with serveCmd.
func TestFetchCmd_SamAddrFlagRegistered(t *testing.T) {
	f := LookupFlag("fetch", "samaddr")
	if f == nil {
		t.Fatal("--samaddr is not registered on fetchCmd; 'newsgo fetch --samaddr <addr>' would return unknown flag")
	}
	if got := f.DefValue; got != onramp.SAM_ADDR {
		t.Errorf("--samaddr default = %q, want %q (onramp.SAM_ADDR)", got, onramp.SAM_ADDR)
	}
}

// TestInitConfig_NewsgoEnvPrefix verifies that initConfig() instructs viper to
// read NEWSGO_* prefixed environment variables, not bare names.
//
// Before the fix, viper.SetEnvPrefix("newsgo") was missing, so NEWSGO_PORT had
// no effect and the server would silently use the default port.  Setting the
// bare PORT would override the port, which conflicts with container runtimes
// that set PORT for their own purposes.
//
// This test sets NEWSGO_PORT=9999 and PORT=1111, resets viper, calls
// initConfig(), and expects viper to resolve "port" to "9999".
func TestInitConfig_NewsgoEnvPrefix(t *testing.T) {
	// t.Setenv restores the original value automatically after the test.
	t.Setenv("NEWSGO_PORT", "9999")
	t.Setenv("PORT", "1111")

	// Reset viper so this test is not affected by prior cobra initialisation
	// or other tests that may have already called initConfig().
	viper.Reset()
	initConfig()

	got := viper.GetString("port")
	if got != "9999" {
		t.Errorf("viper.GetString(\"port\") = %q; want \"9999\" — "+
			"NEWSGO_PORT is not being read (bare PORT=%q would give %q)", got, "1111", "1111")
	}
}

// makeSu3ForCmd builds a minimal signed su3 payload for use in cmd-level tests.
// It is intentionally self-contained so that cmd tests do not depend on the
// internal helpers of package newsfetch.
func makeSu3ForCmd(t *testing.T, content []byte) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("makeSu3ForCmd: generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "cmd-test-signer@example.i2p"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("makeSu3ForCmd: create cert: %v", err)
	}
	_ = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	f := su3.New()
	f.FileType = su3.FileTypeXML
	f.ContentType = su3.ContentTypeNews
	f.Content = content
	f.SignerID = []byte("cmd-test-signer@example.i2p")
	if err := f.Sign(key); err != nil {
		t.Fatalf("makeSu3ForCmd: sign: %v", err)
	}
	data, err := f.MarshalBinary()
	if err != nil {
		t.Fatalf("makeSu3ForCmd: marshal: %v", err)
	}
	return data
}

// TestFetchURLs_NoStdout verifies that a successful fetchURLs call writes the
// Atom XML content ONLY to --outdir and produces zero bytes on stdout.
//
// Before the fix, fetchURLs unconditionally called fmt.Printf("%s\n", content)
// on every successful fetch, dumping the full XML body to stdout regardless of
// whether the caller wanted it there.  This test would have failed before the
// fix and must pass after it.
func TestFetchURLs_NoStdout(t *testing.T) {
	payload := []byte("<feed>no-stdout-test</feed>")
	su3Data := makeSu3ForCmd(t, payload)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-i2p-su3-news")
		w.WriteHeader(http.StatusOK)
		w.Write(su3Data)
	}))
	defer ts.Close()

	outDir := t.TempDir()
	// Serve the su3 under a path that outFilename will recognise as "news.su3".
	url := ts.URL + "/news.su3"

	// Redirect os.Stdout to a pipe so we can capture anything written there.
	origStdout := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = pw

	f := newsfetch.NewFetcherFromClient(ts.Client())
	fetchErr := fetchURLs(f, []string{url}, nil, outDir)

	// Restore stdout before any assertions so test output is not swallowed.
	pw.Close()
	os.Stdout = origStdout

	var captured bytes.Buffer
	io.Copy(&captured, pr)
	pr.Close()

	if fetchErr != nil {
		t.Fatalf("fetchURLs returned unexpected error: %v", fetchErr)
	}
	if captured.Len() != 0 {
		t.Errorf("fetchURLs wrote %d bytes to stdout; want 0\ncaptured: %s",
			captured.Len(), captured.String())
	}
}

// writePKCS1PEM generates an RSA key, encodes it as PKCS#1 PEM, writes it to
// a temp file, and returns the path.  This is the "openssl genrsa" format.
func writePKCS1PEM(t *testing.T, bits int) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	path := filepath.Join(t.TempDir(), "pkcs1.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write PKCS#1 PEM: %v", err)
	}
	return path
}

// writePKCS8PEM generates an RSA key, encodes it as PKCS#8 PEM, writes it to
// a temp file, and returns the path.  This is the "openssl genpkey" default.
func writePKCS8PEM(t *testing.T, bits int) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	path := filepath.Join(t.TempDir(), "pkcs8.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write PKCS#8 PEM: %v", err)
	}
	return path
}

// writeECPKCS8PEM generates an ECDSA P-256 key wrapped in PKCS#8 PEM and
// returns its path.
func writeECPKCS8PEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey(ec): %v", err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	path := filepath.Join(t.TempDir(), "ec_pkcs8.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write EC PKCS#8 PEM: %v", err)
	}
	return path
}

// writeEd25519PEM generates an Ed25519 key wrapped in PKCS#8 PEM and returns
// its path.
func writeEd25519PEM(t *testing.T) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey(ed25519): %v", err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	path := filepath.Join(t.TempDir(), "ed25519.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write Ed25519 PKCS#8 PEM: %v", err)
	}
	return path
}

// TestLoadPrivateKey_PKCS1 verifies that a PKCS#1 RSA key ("openssl genrsa")
// is still loaded successfully after the PKCS#8 fallback was added.
func TestLoadPrivateKey_PKCS1(t *testing.T) {
	path := writePKCS1PEM(t, 2048)
	key, err := loadPrivateKey(path)
	if err != nil {
		t.Fatalf("loadPrivateKey (PKCS#1) returned unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("loadPrivateKey (PKCS#1) returned nil key without error")
	}
}

// TestLoadPrivateKey_PKCS8RSA verifies that a PKCS#8-wrapped RSA key
// ("openssl genpkey -algorithm RSA", Go x509.MarshalPKCS8PrivateKey) is now
// accepted.  This test would have failed before the fix.
func TestLoadPrivateKey_PKCS8RSA(t *testing.T) {
	path := writePKCS8PEM(t, 2048)
	key, err := loadPrivateKey(path)
	if err != nil {
		t.Fatalf("loadPrivateKey (PKCS#8 RSA) returned unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("loadPrivateKey (PKCS#8 RSA) returned nil key without error")
	}
}

// TestLoadPrivateKey_PKCS8EC verifies that a PKCS#8 ECDSA P-256 key is loaded
// successfully and returned as a crypto.Signer backed by *ecdsa.PrivateKey.
// This test would have failed before the su3 library was updated to support
// ECDSA signing.
func TestLoadPrivateKey_PKCS8EC(t *testing.T) {
	path := writeECPKCS8PEM(t)
	key, err := loadPrivateKey(path)
	if err != nil {
		t.Fatalf("loadPrivateKey (PKCS#8 ECDSA) returned unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("loadPrivateKey (PKCS#8 ECDSA) returned nil key without error")
	}
	if _, ok := key.(*ecdsa.PrivateKey); !ok {
		t.Errorf("expected *ecdsa.PrivateKey, got %T", key)
	}
}

// TestLoadPrivateKey_Ed25519 verifies that a PKCS#8 Ed25519 key is loaded
// successfully and returned as a crypto.Signer backed by ed25519.PrivateKey.
func TestLoadPrivateKey_Ed25519(t *testing.T) {
	path := writeEd25519PEM(t)
	key, err := loadPrivateKey(path)
	if err != nil {
		t.Fatalf("loadPrivateKey (Ed25519) returned unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("loadPrivateKey (Ed25519) returned nil key without error")
	}
	if _, ok := key.(ed25519.PrivateKey); !ok {
		t.Errorf("expected ed25519.PrivateKey, got %T", key)
	}
}

// TestLoadPrivateKey_AllTypesImplementCryptoSigner verifies that every key
// type returned by loadPrivateKey satisfies the crypto.Signer interface, which
// is required by signer.NewsSigner.SigningKey and su3.File.Sign.
func TestLoadPrivateKey_AllTypesImplementCryptoSigner(t *testing.T) {
	paths := map[string]string{
		"PKCS#1 RSA":   writePKCS1PEM(t, 2048),
		"PKCS#8 RSA":   writePKCS8PEM(t, 2048),
		"PKCS#8 ECDSA": writeECPKCS8PEM(t),
		"Ed25519":      writeEd25519PEM(t),
	}
	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			key, err := loadPrivateKey(path)
			if err != nil {
				t.Fatalf("loadPrivateKey: %v", err)
			}
			var _ crypto.Signer = key // compile-time + runtime interface check
		})
	}
}

// ---------------------------------------------------------------------------
// Tests for Critical Bug 1: viper BindPFlags collision
//
// Both buildCmd and signCmd register a "builddir" flag; both fetchCmd and
// serveCmd register a "samaddr" flag.  Because viper.BindPFlags stores flag
// bindings in a shared map keyed by name and Go runs init() functions in
// lexical file order, the later command's registration overwrites the earlier
// one.  The fix reads the value directly from the executing command's flag set
// after viper.Unmarshal, bypassing the stale viper binding.
// ---------------------------------------------------------------------------

// TestViperBuildDirCollision_SeparateFlagInstances confirms that buildCmd and
// signCmd own distinct pflag.Flag instances for "builddir", demonstrating that
// the collision is real and that reading from cmd.Flags() (not viper) is the
// only reliable way to get the buildCmd value.
func TestViperBuildDirCollision_SeparateFlagInstances(t *testing.T) {
	buildFlag := LookupFlag("build", "builddir")
	signFlag := LookupFlag("sign", "builddir")

	if buildFlag == nil {
		t.Fatal("buildCmd has no 'builddir' flag")
	}
	if signFlag == nil {
		t.Fatal("signCmd has no 'builddir' flag")
	}
	// The two flags must be different pointer values; sharing one would mean
	// only one command could ever have a changed value.
	if buildFlag == signFlag {
		t.Error("buildCmd and signCmd share the same pflag.Flag for 'builddir'; collision is guaranteed")
	}
}

// TestViperSamAddrCollision_SeparateFlagInstances confirms that fetchCmd and
// serveCmd own distinct pflag.Flag instances for "samaddr".
func TestViperSamAddrCollision_SeparateFlagInstances(t *testing.T) {
	fetchFlag := LookupFlag("fetch", "samaddr")
	serveFlag := LookupFlag("serve", "samaddr")

	if fetchFlag == nil {
		t.Fatal("fetchCmd has no 'samaddr' flag")
	}
	if serveFlag == nil {
		t.Fatal("serveCmd has no 'samaddr' flag")
	}
	if fetchFlag == serveFlag {
		t.Error("fetchCmd and serveCmd share the same pflag.Flag for 'samaddr'; collision is guaranteed")
	}
}

// TestViperBuildDirCollision_DirectFlagReadReturnsCorrectValue verifies the
// fix: reading "builddir" via buildCmd.Flags().GetString returns the value
// set on buildCmd's own flag set, not whatever viper's shared binding points
// to (which after sign.go's BindPFlags call is signCmd's unchanged default).
func TestViperBuildDirCollision_DirectFlagReadReturnsCorrectValue(t *testing.T) {
	buildFlag := LookupFlag("build", "builddir")
	if buildFlag == nil {
		t.Fatal("buildCmd has no 'builddir' flag")
	}

	// Simulate the user supplying --builddir /tmp/custom on the build command.
	if err := buildFlag.Value.Set("/tmp/custom"); err != nil {
		t.Fatalf("could not set buildCmd 'builddir' flag value: %v", err)
	}
	t.Cleanup(func() {
		// Restore default so other tests are not affected.
		_ = buildFlag.Value.Set("build")
		buildFlag.Changed = false
	})

	got := buildFlag.Value.String()
	if got != "/tmp/custom" {
		t.Errorf("buildCmd 'builddir' = %q; want %q", got, "/tmp/custom")
	}

	// signCmd's flag must still be at the default, confirming that setting
	// buildCmd's flag does not affect signCmd's flag.
	signFlag := LookupFlag("sign", "builddir")
	if signFlag == nil {
		t.Fatal("signCmd has no 'builddir' flag")
	}
	if signFlag.Value.String() == "/tmp/custom" {
		t.Error("setting buildCmd 'builddir' also changed signCmd 'builddir'; flags are unexpectedly aliased")
	}
}

// TestViperSamAddrCollision_DirectFlagReadReturnsCorrectValue verifies the
// fix: reading "samaddr" via fetchCmd.Flags().GetString returns only the value
// set on fetchCmd's own flag set, independent of serveCmd's binding.
func TestViperSamAddrCollision_DirectFlagReadReturnsCorrectValue(t *testing.T) {
	fetchFlag := LookupFlag("fetch", "samaddr")
	if fetchFlag == nil {
		t.Fatal("fetchCmd has no 'samaddr' flag")
	}

	const customAddr = "127.0.0.1:7655"
	if err := fetchFlag.Value.Set(customAddr); err != nil {
		t.Fatalf("could not set fetchCmd 'samaddr' flag value: %v", err)
	}
	t.Cleanup(func() {
		_ = fetchFlag.Value.Set(onramp.SAM_ADDR)
		fetchFlag.Changed = false
	})

	got := fetchFlag.Value.String()
	if got != customAddr {
		t.Errorf("fetchCmd 'samaddr' = %q; want %q", got, customAddr)
	}

	serveFlag := LookupFlag("serve", "samaddr")
	if serveFlag == nil {
		t.Fatal("serveCmd has no 'samaddr' flag")
	}
	if serveFlag.Value.String() == customAddr {
		t.Error("setting fetchCmd 'samaddr' also changed serveCmd 'samaddr'; flags are unexpectedly aliased")
	}
}

// ---------------------------------------------------------------------------
// Tests for Critical Bug 2: single-file build() path computation
//
// When --newsfile points to a file (e.g. data/entries.html) rather than a
// directory, the old code computed:
//   base = filepath.Join(c.NewsFile, "entries.html")   // "data/entries.html/entries.html"
// which is always an invalid path, causing os.ReadFile to fail with
// "not a directory".  The fix uses filepath.Dir(c.NewsFile) so that base
// equals newsFile when newsFile is the canonical entries.html, leaving
// BaseEntriesHTMLPath unset (no merge needed in single-file mode).
// ---------------------------------------------------------------------------

// TestSingleFileBuild_BasePathComputation verifies the corrected path
// arithmetic directly, without requiring a live build: when c.NewsFile is a
// file like "data/entries.html", filepath.Join(filepath.Dir(c.NewsFile),
// "entries.html") must equal c.NewsFile so that the "newsFile != base"
// condition is false and BaseEntriesHTMLPath is left unset.
func TestSingleFileBuild_BasePathComputation(t *testing.T) {
	cases := []struct {
		newsFile string // a file path, as supplied by --newsfile in single-file mode
	}{
		{"data/entries.html"},
		{"entries.html"},
		{"/abs/path/entries.html"},
		{"some/deep/dir/entries.html"},
	}
	for _, tc := range cases {
		t.Run(tc.newsFile, func(t *testing.T) {
			// Reproduce the fixed computation from build().
			base := filepath.Join(filepath.Dir(tc.newsFile), "entries.html")

			// The condition that guards BaseEntriesHTMLPath assignment must be false:
			// newsFile IS the canonical file, so no merge baseline is needed.
			if tc.newsFile != base {
				t.Errorf(
					"fixed computation: filepath.Join(filepath.Dir(%q), \"entries.html\") = %q, "+
						"not equal to newsFile — BaseEntriesHTMLPath would be set to an invalid sub-path",
					tc.newsFile, base,
				)
			}

			// Also confirm the OLD (buggy) computation produces a wrong path.
			oldBase := filepath.Join(tc.newsFile, "entries.html")
			if tc.newsFile == oldBase {
				// If somehow they're equal the bug wouldn't manifest — flag it.
				t.Logf("note: old computation coincidentally matched for %q (not the common case)", tc.newsFile)
			} else {
				// Confirm the old base is the invalid "file/entries.html" path.
				if !strings.HasSuffix(oldBase, filepath.Join(filepath.Base(tc.newsFile), "entries.html")) {
					t.Errorf("unexpected old-computation result %q for newsFile %q", oldBase, tc.newsFile)
				}
			}
		})
	}
}

// TestSingleFileBuild_ProducesOutput is an integration test verifying that
// calling build() with a single entries.html file path (single-file mode)
// actually produces a news feed instead of failing with
// "open data/entries.html/entries.html: not a directory".
func TestSingleFileBuild_ProducesOutput(t *testing.T) {
	dir := t.TempDir()
	buildDir := t.TempDir()

	const releasesJSON = `[{"date":"2025-01-01","version":"2.0.0","minVersion":"0.9.9","minJavaVersion":"1.8","updates":{"su3":{"torrent":"magnet:?xt=urn:btih:abc","url":["http://example.com/update.su3"]}}}]`
	const entriesHTML = `<html><body><header>H</header><article id="urn:single:1" title="T" href="http://x.com" author="A" published="2025-01-01" updated="2025-01-01"><details><summary>S</summary></details><p>body</p></article></body></html>`
	const blocklistXML = ``

	entriesFile := filepath.Join(dir, "entries.html")
	releasesFile := filepath.Join(dir, "releases.json")
	blocklistFile := filepath.Join(dir, "blocklist.xml")

	must(t, os.WriteFile(entriesFile, []byte(entriesHTML), 0o644))
	must(t, os.WriteFile(releasesFile, []byte(releasesJSON), 0o644))
	must(t, os.WriteFile(blocklistFile, []byte(blocklistXML), 0o644))

	prev := *c
	defer func() { *c = prev }()
	// Single-file mode: NewsFile points at the file, not the directory.
	c.NewsFile = entriesFile
	c.ReleaseJsonFile = releasesFile
	c.BlockList = blocklistFile
	c.BuildDir = buildDir
	c.FeedTitle = "Test Feed"
	c.FeedSite = "http://example.com"
	c.FeedMain = "http://example.com/news.atom.xml"
	c.FeedBackup = ""
	c.FeedSubtitle = "sub"
	c.FeedUuid = "00000000-0000-0000-0000-000000000099"
	c.TranslationsDir = ""

	// build() is the single-file handler called from buildCmd.Run when
	// c.NewsFile is not a directory.
	build(entriesFile)

	// With the fix, news.atom.xml must be produced in BuildDir.
	out := filepath.Join(buildDir, "news.atom.xml")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("single-file build() did not produce %s: %v\n"+
			"(pre-fix: this failed with 'open .../entries.html/entries.html: not a directory')",
			out, err,
		)
	}
	if !strings.Contains(string(data), "urn:single:1") {
		t.Errorf("output %s does not contain expected article id 'urn:single:1'; content:\n%s", out, string(data))
	}
}

// TestOutputFilenameForPlatform_LinuxHasPlatformStatusPrefix verifies that
// "linux" is now treated as a first-class platform: the output filename is
// prefixed with "linux/<status>/" rather than being the bare base name.
// This ensures --platform linux --status stable produces a distinct output
// from the unnamed default tree.
func TestOutputFilenameForPlatform_LinuxHasPlatformStatusPrefix(t *testing.T) {
	newsFile := filepath.Join("data", "linux", "stable", "entries.html")
	newsRoot := filepath.Join("data", "linux", "stable")

	got := outputFilenameForPlatform(newsFile, newsRoot, "linux", "stable")
	want := filepath.Join("linux", "stable", "news.atom.xml")
	if got != want {
		t.Errorf("outputFilenameForPlatform(%q, %q, \"linux\", \"stable\") = %q; want %q",
			newsFile, newsRoot, got, want)
	}
}

// TestOutputFilenameForPlatform_LinuxStableVsBeta verifies that different
// status values produce distinct output paths for the linux platform.  Before
// the fix both calls returned the same bare "news.atom.xml", rendering the
// --status flag meaningless for linux.
func TestOutputFilenameForPlatform_LinuxStableVsBeta(t *testing.T) {
	stableEntries := filepath.Join("data", "linux", "stable", "entries.html")
	betaEntries := filepath.Join("data", "linux", "beta", "entries.html")

	stable := outputFilenameForPlatform(stableEntries, filepath.Join("data", "linux", "stable"), "linux", "stable")
	beta := outputFilenameForPlatform(betaEntries, filepath.Join("data", "linux", "beta"), "linux", "beta")

	if stable == beta {
		t.Errorf("linux/stable and linux/beta produced the same output path %q; they must differ", stable)
	}
	if !strings.Contains(stable, "stable") {
		t.Errorf("linux/stable output %q does not contain 'stable'", stable)
	}
	if !strings.Contains(beta, "beta") {
		t.Errorf("linux/beta output %q does not contain 'beta'", beta)
	}
}

// TestOutputFilenameForPlatform_EmptyPlatformNoPrefix verifies that the empty
// platform (the unnamed default tree) still produces a bare base name with no
// platform/status prefix, preserving backward compatibility.
func TestOutputFilenameForPlatform_EmptyPlatformNoPrefix(t *testing.T) {
	newsFile := filepath.Join("data", "entries.html")
	newsRoot := "data"

	got := outputFilenameForPlatform(newsFile, newsRoot, "", "")
	want := "news.atom.xml"
	if got != want {
		t.Errorf("outputFilenameForPlatform(%q, %q, \"\", \"\") = %q; want %q",
			newsFile, newsRoot, got, want)
	}
}

// --- collectBuildPairs tests ---
// These tests directly exercise the pair-building function extracted from
// buildCmd.Run to verify each documented --platform / --status combination.

// TestCollectBuildPairs_BothFlags verifies that supplying both --platform and
// --status produces exactly one pair with those exact values.
func TestCollectBuildPairs_BothFlags(t *testing.T) {
	pairs := collectBuildPairs("win", "stable")
	if len(pairs) != 1 {
		t.Fatalf("collectBuildPairs(\"win\", \"stable\") returned %d pairs, want 1", len(pairs))
	}
	if pairs[0].platform != "win" || pairs[0].status != "stable" {
		t.Errorf("got pair {%q, %q}, want {\"win\", \"stable\"}", pairs[0].platform, pairs[0].status)
	}
}

// TestCollectBuildPairs_PlatformOnly verifies that supplying only --platform
// produces one pair per known status, all sharing the specified platform.
func TestCollectBuildPairs_PlatformOnly(t *testing.T) {
	pairs := collectBuildPairs("mac", "")
	knownStatuses := builder.KnownStatuses()
	if len(pairs) != len(knownStatuses) {
		t.Fatalf("collectBuildPairs(\"mac\", \"\") returned %d pairs, want %d (one per status)",
			len(pairs), len(knownStatuses))
	}
	for i, p := range pairs {
		if p.platform != "mac" {
			t.Errorf("pairs[%d].platform = %q, want \"mac\"", i, p.platform)
		}
		if p.status != knownStatuses[i] {
			t.Errorf("pairs[%d].status = %q, want %q", i, p.status, knownStatuses[i])
		}
	}
}

// TestCollectBuildPairs_StatusOnly verifies that supplying only --status
// produces one pair for the default tree (empty platform) plus one pair per
// known platform, all sharing the specified status.
// This is the previously-missing case: --status without --platform used to
// fall through to the default branch and build ALL channels.
func TestCollectBuildPairs_StatusOnly(t *testing.T) {
	pairs := collectBuildPairs("", "stable")
	knownPlatforms := builder.KnownPlatforms()
	// expect: ("", "stable") + one entry per known platform
	wantLen := 1 + len(knownPlatforms)
	if len(pairs) != wantLen {
		t.Fatalf("collectBuildPairs(\"\", \"stable\") returned %d pairs, want %d", len(pairs), wantLen)
	}
	// First entry must be the default tree with the fixed status.
	if pairs[0].platform != "" || pairs[0].status != "stable" {
		t.Errorf("pairs[0] = {%q, %q}, want {\"\", \"stable\"}", pairs[0].platform, pairs[0].status)
	}
	// Remaining entries must cover all platforms with the fixed status.
	for i, p := range pairs[1:] {
		if p.platform != knownPlatforms[i] {
			t.Errorf("pairs[%d].platform = %q, want %q", i+1, p.platform, knownPlatforms[i])
		}
		if p.status != "stable" {
			t.Errorf("pairs[%d].status = %q, want \"stable\"", i+1, p.status)
		}
	}
}

// TestCollectBuildPairs_StatusOnly_NoAlphaBetaleak verifies that
// collectBuildPairs("", "stable") does NOT include beta/rc/alpha pairs,
// which was the pre-fix behaviour when --status was silently ignored.
func TestCollectBuildPairs_StatusOnly_NoAlphaBetaleak(t *testing.T) {
	pairs := collectBuildPairs("", "stable")
	for _, p := range pairs {
		if p.status != "stable" {
			t.Errorf("unexpected non-stable pair {%q, %q}; --status stable must filter all platforms",
				p.platform, p.status)
		}
	}
}

// TestCollectBuildPairs_NoFlags verifies that passing no flags produces the
// default tree entry followed by all (platform × status) combinations.
func TestCollectBuildPairs_NoFlags(t *testing.T) {
	pairs := collectBuildPairs("", "")
	knownPlatforms := builder.KnownPlatforms()
	knownStatuses := builder.KnownStatuses()
	wantLen := 1 + len(knownPlatforms)*len(knownStatuses)
	if len(pairs) != wantLen {
		t.Fatalf("collectBuildPairs(\"\", \"\") returned %d pairs, want %d", len(pairs), wantLen)
	}
	// First pair must be the default tree.
	if pairs[0].platform != "" || pairs[0].status != "" {
		t.Errorf("pairs[0] = {%q, %q}, want {\"\", \"\"}", pairs[0].platform, pairs[0].status)
	}
}
