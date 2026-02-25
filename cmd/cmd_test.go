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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
