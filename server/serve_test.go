package newsserver

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	stats "github.com/go-i2p/newsgo/server/stats"
)

func TestOpenDirectory_MissingDir(t *testing.T) {
	_, err := openDirectory("/nonexistent/directory/path")
	if err == nil {
		t.Fatal("expected error for missing directory, got nil")
	}
}

// TestFileChecksum_Consistent verifies that fileChecksum produces the correct
// SHA-256 hex digest without reading the full file into memory at once.  The
// expected value is computed independently using crypto/sha256.Sum256.
func TestFileChecksum_Consistent(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello, newsgo")
	path := filepath.Join(dir, "test.xml")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(content))
	got, err := fileChecksum(path)
	if err != nil {
		t.Fatalf("fileChecksum: %v", err)
	}
	if got != want {
		t.Errorf("fileChecksum = %q, want %q", got, want)
	}
}

// TestFileChecksum_Missing verifies fileChecksum returns an error for a
// non-existent path instead of panicking or returning an empty string.
func TestFileChecksum_Missing(t *testing.T) {
	_, err := fileChecksum("/nonexistent/path/to/file.xml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestOpenDirectory_ValidDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.xml"), []byte("<feed/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	listing, err := openDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if listing == "" {
		t.Error("expected non-empty directory listing, got empty string")
	}
}

// TestServeFile_StatFailure verifies that ServeFile returns an error (not a
// nil-dereference panic) when os.Stat fails on the target file.
func TestServeFile_StatFailure(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "news.atom.xml")
	s := &NewsServer{NewsDir: dir}
	rw := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/news.atom.xml", nil)
	err := s.ServeFile(missing, rq, rw)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestServeHTTP_MissingFile(t *testing.T) {
	dir := t.TempDir()
	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}
	rw := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/missing.atom.xml", nil)
	s.ServeHTTP(rw, rq)
	if rw.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rw.Code)
	}
}

func TestServeHTTP_ValidFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte("<feed/>")
	if err := os.WriteFile(filepath.Join(dir, "news.atom.xml"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}
	rw := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/news.atom.xml", nil)
	s.ServeHTTP(rw, rq)
	if rw.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rw.Code)
	}
	if rw.Body.String() != string(content) {
		t.Errorf("body mismatch: got %q, want %q", rw.Body.String(), content)
	}
}

func TestServeHTTP_DirectoryListing(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}
	rw := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/subdir", nil)
	s.ServeHTTP(rw, rq)
	if rw.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rw.Code)
	}
}

// TestFileType_AtomXML verifies that ".atom.xml" files are detected as Atom
// feeds and NOT as generic XML. filepath.Ext returns ".xml" for these files,
// so the old case ".atom.xml" switch arm was unreachable dead code. The fix
// uses strings.HasSuffix before the extension switch.
func TestFileType_AtomXML(t *testing.T) {
	tests := []struct {
		file      string
		wantType  string
		wantError bool
	}{
		{"news.atom.xml", "application/atom+xml", false},
		{"sub/news_de.atom.xml", "application/atom+xml", false},
		{"news.xml", "application/rss+xml", false},
		{"index.html", "text/html", false},
		{"update.su3", "application/x-i2p-su3-news", false},
		{"langstats.svg", "image/svg+xml", false},
	}
	for _, tt := range tests {
		got, err := fileType(tt.file)
		if tt.wantError {
			if err == nil {
				t.Errorf("fileType(%q): expected error, got nil", tt.file)
			}
			continue
		}
		if err != nil {
			t.Errorf("fileType(%q): unexpected error: %v", tt.file, err)
			continue
		}
		if got != tt.wantType {
			t.Errorf("fileType(%q) = %q, want %q", tt.file, got, tt.wantType)
		}
	}
}

func statsForTest(dir string) stats.NewsStats {
	sf := filepath.Join(dir, "stats.json")
	ns := stats.NewsStats{StateFile: sf}
	ns.Load()
	return ns
}
