package newsstats

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
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

// TestIncrement_ZeroValue verifies that Increment does not panic on a
// zero-value NewsStats (i.e., when Load has never been called and
// DownloadLangs is nil). Prior to the lazy-initialisation fix, calling
// Increment on a directly-constructed &NewsStats{} panicked with
// "assignment to entry in nil map".
func TestIncrement_ZeroValue(t *testing.T) {
	n := &NewsStats{}
	rq := httptest.NewRequest(http.MethodGet, "/?lang=de", nil)
	n.Increment(rq) // must not panic
	n.mu.RLock()
	got := n.DownloadLangs["de"]
	n.mu.RUnlock()
	if got != 1 {
		t.Errorf("expected de=1 after increment on zero-value struct, got %d", got)
	}
}

// TestIncrement_ZeroValue_DefaultLang verifies that Increment uses "en_US"
// when no lang query parameter is supplied, even on a zero-value NewsStats.
func TestIncrement_ZeroValue_DefaultLang(t *testing.T) {
	n := &NewsStats{}
	rq := httptest.NewRequest(http.MethodGet, "/news.su3", nil)
	n.Increment(rq) // no lang param — should default to en_US
	n.mu.RLock()
	got := n.DownloadLangs["en_US"]
	n.mu.RUnlock()
	if got != 1 {
		t.Errorf("expected en_US=1 for missing lang param, got %d", got)
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

// TestIncrement_ConcurrentSafety verifies that Increment is safe to call from
// multiple goroutines simultaneously. Before the mutex was added, concurrent
// calls produced a "fatal error: concurrent map writes" runtime panic. Running
// this test with -race will detect any remaining data-race.
func TestIncrement_ConcurrentSafety(t *testing.T) {
	n := &NewsStats{DownloadLangs: make(map[string]int)}

	const goroutines = 50
	const callsEach = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			rq := httptest.NewRequest(http.MethodGet, "/?lang=en_US", nil)
			for j := 0; j < callsEach; j++ {
				n.Increment(rq)
			}
		}()
	}
	wg.Wait()

	want := goroutines * callsEach
	n.mu.RLock()
	got := n.DownloadLangs["en_US"]
	n.mu.RUnlock()
	if got != want {
		t.Errorf("concurrent increments: got %d, want %d", got, want)
	}
}

// TestSave_ConcurrentWithIncrement verifies that Save can be called while
// Increment goroutines are running without a data race.
func TestSave_ConcurrentWithIncrement(t *testing.T) {
	dir := t.TempDir()
	sf := filepath.Join(dir, "stats.json")
	n := &NewsStats{
		StateFile:     sf,
		DownloadLangs: make(map[string]int),
	}

	var wg sync.WaitGroup
	// 20 concurrent incrementers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rq := httptest.NewRequest(http.MethodGet, "/?lang=de", nil)
			for j := 0; j < 50; j++ {
				n.Increment(rq)
			}
		}()
	}
	// 5 concurrent savers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Errors are expected when the file hasn't been written yet; we
			// only care that Save does not panic or race.
			_ = n.Save()
		}()
	}
	wg.Wait()
}
