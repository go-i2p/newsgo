package newsstats

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_MissingFile(t *testing.T) {
	n := &NewsStats{StateFile: "/nonexistent/stats.json"}
	n.Load()
	if n.DownloadLangs == nil {
		t.Fatal("DownloadLangs is nil after Load with missing file")
	}
}

// TestLoad_NullJSON verifies that a stats file containing "null" does not
// leave DownloadLangs as a nil map. Without the fix, json.Unmarshal succeeded
// but set the map to nil, causing a panic on the next Increment call.
func TestLoad_NullJSON(t *testing.T) {
	dir := t.TempDir()
	sf := filepath.Join(dir, "stats.json")
	if err := os.WriteFile(sf, []byte("null"), 0o644); err != nil {
		t.Fatal(err)
	}
	n := &NewsStats{StateFile: sf}
	n.Load()
	if n.DownloadLangs == nil {
		t.Fatal("DownloadLangs is nil after Load with null JSON file")
	}
}

func TestLoad_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	sf := filepath.Join(dir, "stats.json")
	if err := os.WriteFile(sf, []byte(`{"en_US":5,"de":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	n := &NewsStats{StateFile: sf}
	n.Load()
	if n.DownloadLangs == nil {
		t.Fatal("DownloadLangs is nil after Load with valid JSON")
	}
	if n.DownloadLangs["en_US"] != 5 {
		t.Errorf("expected en_US=5, got %d", n.DownloadLangs["en_US"])
	}
	if n.DownloadLangs["de"] != 2 {
		t.Errorf("expected de=2, got %d", n.DownloadLangs["de"])
	}
}

func TestLoad_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	sf := filepath.Join(dir, "stats.json")
	if err := os.WriteFile(sf, []byte(`{not valid json`), 0o644); err != nil {
		t.Fatal(err)
	}
	n := &NewsStats{StateFile: sf}
	n.Load()
	if n.DownloadLangs == nil {
		t.Fatal("DownloadLangs is nil after Load with malformed JSON")
	}
}

// TestIncrement_AfterNullJSON is the regression test for the nil map panic:
// Increment must not panic when called after Load on a "null" JSON file.
func TestIncrement_AfterNullJSON(t *testing.T) {
	dir := t.TempDir()
	sf := filepath.Join(dir, "stats.json")
	if err := os.WriteFile(sf, []byte("null"), 0o644); err != nil {
		t.Fatal(err)
	}
	n := &NewsStats{StateFile: sf}
	n.Load()
	rq := httptest.NewRequest(http.MethodGet, "/?lang=de", nil)
	n.Increment(rq) // must not panic
	if n.DownloadLangs["de"] != 1 {
		t.Errorf("expected de=1 after increment, got %d", n.DownloadLangs["de"])
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	sf := filepath.Join(dir, "stats.json")
	n := &NewsStats{
		StateFile:     sf,
		DownloadLangs: map[string]int{"en_US": 3, "fr": 1},
	}
	if err := n.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	n2 := &NewsStats{StateFile: sf}
	n2.Load()
	if n2.DownloadLangs["en_US"] != 3 {
		t.Errorf("expected en_US=3, got %d", n2.DownloadLangs["en_US"])
	}
	if n2.DownloadLangs["fr"] != 1 {
		t.Errorf("expected fr=1, got %d", n2.DownloadLangs["fr"])
	}
}
