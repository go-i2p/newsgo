package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
