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
