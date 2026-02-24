package newsserver

import (
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

func TestOpenDirectory_ValidDir(t *testing.T) {
dir := t.TempDir()
if err := os.WriteFile(filepath.Join(dir, "test.xml"), []byte("<feed/>"), 0644); err != nil {
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
if err := os.WriteFile(filepath.Join(dir, "news.atom.xml"), content, 0644); err != nil {
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
if err := os.Mkdir(sub, 0755); err != nil {
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

func statsForTest(dir string) stats.NewsStats {
sf := filepath.Join(dir, "stats.json")
ns := stats.NewsStats{StateFile: sf}
ns.Load()
return ns
}
