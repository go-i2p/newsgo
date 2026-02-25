package newssigner

import (
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// generateTestKey produces a 2048-bit RSA key for use in signer tests.
// 2048 bits is chosen to keep test runtime reasonable while remaining
// large enough to exercise the full signing pipeline.
func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

// TestCreateSu3_WrongExtension_ReturnsError verifies that CreateSu3 refuses to
// process a file whose path does not end with ".atom.xml".  Without this guard,
// strings.Replace returns the input path unchanged, and os.WriteFile silently
// overwrites the source file with binary su3 data.
func TestCreateSu3_WrongExtension_ReturnsError(t *testing.T) {
	ns := &NewsSigner{
		SignerID:   "test@example.i2p",
		SigningKey: generateTestKey(t),
	}
	err := ns.CreateSu3("myfeed.xml")
	if err == nil {
		t.Fatal("expected error for non-.atom.xml path, got nil")
	}
	if !strings.Contains(err.Error(), ".atom.xml") {
		t.Errorf("expected error to mention .atom.xml suffix; got: %v", err)
	}
}

// TestCreateSu3_NoExtension_ReturnsError verifies that CreateSu3 refuses to
// process a path with no file extension.
func TestCreateSu3_NoExtension_ReturnsError(t *testing.T) {
	ns := &NewsSigner{
		SignerID:   "test@example.i2p",
		SigningKey: generateTestKey(t),
	}
	err := ns.CreateSu3("myfeed")
	if err == nil {
		t.Fatal("expected error for no-extension path, got nil")
	}
}

// TestCreateSu3_EmptyPath_ReturnsError verifies that CreateSu3 rejects an
// empty string rather than proceeding with a path that cannot produce a valid
// output filename.
func TestCreateSu3_EmptyPath_ReturnsError(t *testing.T) {
	ns := &NewsSigner{
		SignerID:   "test@example.i2p",
		SigningKey: generateTestKey(t),
	}
	err := ns.CreateSu3("")
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

// TestCreateSu3_CorrectSuffix_ProducesFile verifies that CreateSu3 creates a
// .su3 output file alongside a valid .atom.xml input without modifying the
// source file.
func TestCreateSu3_CorrectSuffix_ProducesFile(t *testing.T) {
	dir := t.TempDir()
	key := generateTestKey(t)
	xmlPath := filepath.Join(dir, "news.atom.xml")
	su3Path := filepath.Join(dir, "news.su3")

	xmlContent := []byte("<feed/>")
	if err := os.WriteFile(xmlPath, xmlContent, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ns := &NewsSigner{SignerID: "test@example.i2p", SigningKey: key}
	if err := ns.CreateSu3(xmlPath); err != nil {
		t.Fatalf("CreateSu3: %v", err)
	}

	if _, err := os.Stat(su3Path); err != nil {
		t.Errorf("expected output file %s to exist: %v", su3Path, err)
	}
}

// TestCreateSu3_SourceFileUnchanged verifies that the source .atom.xml file
// retains its original content after CreateSu3 runs — the bug this guards
// against is the output path colliding with the input path, causing the source
// to be overwritten with su3 binary data.
func TestCreateSu3_SourceFileUnchanged(t *testing.T) {
	dir := t.TempDir()
	key := generateTestKey(t)
	xmlPath := filepath.Join(dir, "news.atom.xml")

	xmlContent := []byte("<feed><title>test</title></feed>")
	if err := os.WriteFile(xmlPath, xmlContent, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ns := &NewsSigner{SignerID: "test@example.i2p", SigningKey: key}
	if err := ns.CreateSu3(xmlPath); err != nil {
		t.Fatalf("CreateSu3: %v", err)
	}

	got, err := os.ReadFile(xmlPath)
	if err != nil {
		t.Fatalf("re-read source file: %v", err)
	}
	if string(got) != string(xmlContent) {
		t.Errorf("source file was modified by CreateSu3; got %q, want %q", got, xmlContent)
	}
}

// TestCreateSu3_MissingSource_ReturnsError verifies that a missing .atom.xml
// file produces an error rather than creating an empty or corrupt output file.
func TestCreateSu3_MissingSource_ReturnsError(t *testing.T) {
	ns := &NewsSigner{
		SignerID:   "test@example.i2p",
		SigningKey: generateTestKey(t),
	}
	err := ns.CreateSu3("/nonexistent/path/news.atom.xml")
	if err == nil {
		t.Fatal("expected error for missing source file, got nil")
	}
}

// TestCreateSu3_OutputPathDerivation verifies that the output filename is
// derived by replacing the ".atom.xml" suffix with ".su3" and that no other
// part of the path is affected (e.g. a directory component containing
// ".atom.xml" as a substring must not be mangled).
func TestCreateSu3_OutputPathDerivation(t *testing.T) {
	// Use a directory name that contains ".atom.xml" as a non-suffix substring
	// to confirm that only the filename suffix is replaced.
	dir := t.TempDir()
	subdir := filepath.Join(dir, "feeds.atom.xml.dir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	xmlPath := filepath.Join(subdir, "news.atom.xml")
	su3Path := filepath.Join(subdir, "news.su3")

	if err := os.WriteFile(xmlPath, []byte("<feed/>"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ns := &NewsSigner{SignerID: "test@example.i2p", SigningKey: generateTestKey(t)}
	if err := ns.CreateSu3(xmlPath); err != nil {
		t.Fatalf("CreateSu3: %v", err)
	}
	if _, err := os.Stat(su3Path); err != nil {
		t.Errorf("expected su3 output at %s: %v", su3Path, err)
	}
}
