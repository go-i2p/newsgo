package newsserver

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// TestContainsPath verifies the containment helper used by the path-traversal
// guard in ServeHTTP.
func TestContainsPath(t *testing.T) {
	tests := []struct {
		root   string
		target string
		want   bool
	}{
		// Exact match of the root itself must be allowed (directory listing).
		{"/srv/news", "/srv/news", true},
		// Normal file inside root — must be allowed.
		{"/srv/news", "/srv/news/news.atom.xml", true},
		// Nested subdirectory — must be allowed.
		{"/srv/news", "/srv/news/sub/dir/file.su3", true},
		// Path that merely shares a prefix string is NOT inside root.
		{"/srv/news", "/srv/news-extra/secret", false},
		// Classic ../ traversal that filepath.Clean resolves.
		{"/srv/news", "/etc/passwd", false},
		// One level above root.
		{"/srv/news", "/srv", false},
	}
	for _, tt := range tests {
		got := containsPath(tt.root, tt.target)
		if got != tt.want {
			t.Errorf("containsPath(%q, %q) = %v, want %v", tt.root, tt.target, got, tt.want)
		}
	}
}

// TestServeHTTP_PathTraversal verifies that requests containing ".." components
// are rejected with HTTP 400 Bad Request before any filesystem access occurs.
func TestServeHTTP_PathTraversal(t *testing.T) {
	// NewsDir points to a real temp directory; the traversal targets /etc/passwd
	// which lives outside it.  The test must receive 400 regardless of whether
	// /etc/passwd exists on the test host.
	dir := t.TempDir()
	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}

	traversals := []string{
		"/../../../etc/passwd",
		"/../../etc/shadow",
		"/../secret.txt",
		"/%2e%2e/%2e%2e/etc/passwd", // percent-encoded (net/http decodes these)
	}
	for _, urlPath := range traversals {
		rw := httptest.NewRecorder()
		rq := httptest.NewRequest(http.MethodGet, urlPath, nil)
		s.ServeHTTP(rw, rq)
		if rw.Code != http.StatusBadRequest {
			t.Errorf("path %q: expected 400 Bad Request, got %d", urlPath, rw.Code)
		}
	}
}

// TestOpenDirectory_FileLinksAreRelative verifies that file entries in a
// directory listing use only the bare filename as the link href, not a path
// component derived from the filesystem path.  Before the fix, a link for
// "news.atom.xml" inside a "de/" subdirectory was rendered as "de/news.atom.xml"
// instead of "news.atom.xml". When the request URL ends with a trailing slash
// ("/de/"), browsers resolved "de/news.atom.xml" relative to "/de/" as
// "/de/de/news.atom.xml" — a doubled path that always 404ed.
func TestOpenDirectory_FileLinksAreRelative(t *testing.T) {
	// Create a directory structure: root/de/news.atom.xml
	root := t.TempDir()
	sub := filepath.Join(root, "de")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "news.atom.xml"), []byte("<feed/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	listing, err := openDirectory(sub)
	if err != nil {
		t.Fatalf("openDirectory: %v", err)
	}

	// The link text and href for "news.atom.xml" must be exactly the filename,
	// not "de/news.atom.xml" which would double the directory when the URL has
	// a trailing slash.
	if !strings.Contains(listing, "(news.atom.xml)") {
		t.Errorf("expected link href '(news.atom.xml)' in listing, got:\n%s", listing)
	}
	if strings.Contains(listing, "de/news.atom.xml") {
		t.Errorf("link href must not contain directory prefix 'de/'; got doubled path in listing:\n%s", listing)
	}
}

// TestFileCheck_LangStats_NoStatRequired verifies that fileCheck returns nil
// for langstats.svg even when no file with that name exists on disk.  The
// stats SVG is generated on-demand by Stats.Graph and is never written to the
// news directory, so it must bypass the os.Stat existence check.
func TestFileCheck_LangStats_NoStatRequired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, statsGraphFilename) // does NOT exist on disk
	if err := fileCheck(path); err != nil {
		t.Errorf("fileCheck(%q): expected nil for stats graph path, got %v", path, err)
	}
}

// TestFileCheck_ArbitrarySVG_Missing verifies that fileCheck returns a non-nil
// error for a *.svg path that is neither the stats graph filename nor present
// on disk.  Before the fix every *.svg path bypassed os.Stat, so a fabricated
// URL like /secret.svg always returned nil here, leading to an HTTP 200.
func TestFileCheck_ArbitrarySVG_Missing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.svg") // does NOT exist on disk
	if err := fileCheck(path); err == nil {
		t.Errorf("fileCheck(%q): expected error for missing arbitrary .svg, got nil", path)
	}
}

// TestServeHTTP_ArbitrarySVG_Returns404 verifies that an HTTP request for a
// *.svg URL that does not match langstats.svg and has no corresponding file on
// disk receives HTTP 404 instead of the stats bar chart.
func TestServeHTTP_ArbitrarySVG_Returns404(t *testing.T) {
	dir := t.TempDir()
	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}
	for _, name := range []string{"secret.svg", "nonexistent.svg", "logo.svg"} {
		rw := httptest.NewRecorder()
		rq := httptest.NewRequest(http.MethodGet, "/"+name, nil)
		s.ServeHTTP(rw, rq)
		if rw.Code != http.StatusNotFound {
			t.Errorf("GET /%s: expected 404, got %d", name, rw.Code)
		}
	}
}

// TestServeHTTP_LangStatsSVG_Returns200 verifies that a request for the
// canonical langstats.svg path returns HTTP 200 with an SVG body served by
// Stats.Graph, even though langstats.svg is never written to NewsDir.
func TestServeHTTP_LangStatsSVG_Returns200(t *testing.T) {
	dir := t.TempDir()
	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}
	rw := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/"+statsGraphFilename, nil)
	s.ServeHTTP(rw, rq)
	if rw.Code != http.StatusOK {
		t.Errorf("GET /langstats.svg: expected 200, got %d", rw.Code)
	}
	ct := rw.Header().Get("Content-Type")
	if !strings.Contains(ct, "image/svg+xml") {
		t.Errorf("GET /langstats.svg: expected Content-Type image/svg+xml, got %q", ct)
	}
}

// TestServeHTTP_ExistingSVGFile_Served verifies that a real *.svg file present
// on disk is served as a static file (not hijacked by Stats.Graph) when its
// name is not langstats.svg.
func TestServeHTTP_ExistingSVGFile_Served(t *testing.T) {
	dir := t.TempDir()
	svgContent := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`)
	svgFile := filepath.Join(dir, "logo.svg")
	if err := os.WriteFile(svgFile, svgContent, 0o644); err != nil {
		t.Fatal(err)
	}
	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}
	rw := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/logo.svg", nil)
	s.ServeHTTP(rw, rq)
	if rw.Code != http.StatusOK {
		t.Errorf("GET /logo.svg: expected 200, got %d", rw.Code)
	}
	if !bytes.Equal(rw.Body.Bytes(), svgContent) {
		t.Errorf("GET /logo.svg: body mismatch\ngot:  %q\nwant: %q", rw.Body.Bytes(), svgContent)
	}
}

// TestServeHTTP_ContentType_SingleValue verifies that a response carries
// exactly one Content-Type value even when upstream middleware has pre-set the
// header. Before the fix, ServeFile called rw.Header().Add("Content-Type", …)
// which appended a second value instead of replacing the first.
func TestServeHTTP_ContentType_SingleValue(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "news.atom.xml"), []byte("<feed/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}
	rw := httptest.NewRecorder()
	// Simulate middleware that pre-sets an incorrect Content-Type.
	rw.Header().Set("Content-Type", "text/plain")

	rq := httptest.NewRequest(http.MethodGet, "/news.atom.xml", nil)
	s.ServeHTTP(rw, rq)

	values := rw.Result().Header["Content-Type"]
	if len(values) != 1 {
		t.Errorf("Content-Type header has %d value(s), want exactly 1: %v", len(values), values)
	}
	if len(values) > 0 && values[0] != "application/atom+xml" {
		t.Errorf("Content-Type = %q, want %q", values[0], "application/atom+xml")
	}
}

// TestServeHTTP_ConditionalGET_NotModified verifies that the server returns
// HTTP 304 Not Modified when the client supplies an If-Modified-Since header
// that is at or after the file's modification time. Before the fix,
// serveStaticFile used os.ReadFile + rw.Write, which always sends HTTP 200
// with the full body regardless of the If-Modified-Since header.
func TestServeHTTP_ConditionalGET_NotModified(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "news.atom.xml")
	if err := os.WriteFile(fpath, []byte("<feed/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(fpath)
	if err != nil {
		t.Fatal(err)
	}

	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}
	rw := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/news.atom.xml", nil)
	// Set If-Modified-Since to the file's exact modification time.
	rq.Header.Set("If-Modified-Since", fi.ModTime().UTC().Format(http.TimeFormat))
	s.ServeHTTP(rw, rq)

	if rw.Code != http.StatusNotModified {
		t.Errorf("conditional GET: expected 304 Not Modified, got %d", rw.Code)
	}
}

// TestServeHTTP_RangeRequest verifies that the server returns HTTP 206 Partial
// Content for a well-formed Range request. Before the fix, serveStaticFile
// used rw.Write which ignores Range headers entirely and always returns 200
// with the full body.
func TestServeHTTP_RangeRequest(t *testing.T) {
	dir := t.TempDir()
	content := []byte("<feed>hello</feed>")
	if err := os.WriteFile(filepath.Join(dir, "news.atom.xml"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}
	rw := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/news.atom.xml", nil)
	// Request only the first 6 bytes.
	rq.Header.Set("Range", "bytes=0-5")
	s.ServeHTTP(rw, rq)

	if rw.Code != http.StatusPartialContent {
		t.Errorf("range GET: expected 206 Partial Content, got %d", rw.Code)
	}
	got := rw.Body.Bytes()
	want := content[0:6]
	if !bytes.Equal(got, want) {
		t.Errorf("range body = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Content-Type on error responses (AUDIT.md: "HTTP 404 error responses carry
// the wrong Content-Type header")
// ---------------------------------------------------------------------------

// TestServeFile_SetsContentTypeBeforeError verifies the per-function behaviour
// that makes ServeHTTP's reset necessary: ServeFile writes the Content-Type
// header before its os.Stat call, so when stat fails the ResponseWriter header
// map already contains the feed-specific type (application/atom+xml).
// ServeHTTP must overwrite this value before calling WriteHeader(404).
func TestServeFile_SetsContentTypeBeforeError(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "news.atom.xml")
	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}
	rw := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/news.atom.xml", nil)

	err := s.ServeFile(missing, rq, rw)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	// ServeFile sets Content-Type before stat — this is the pre-WriteHeader
	// state that ServeHTTP must correct.
	ct := rw.Header().Get("Content-Type")
	if ct != "application/atom+xml" {
		t.Errorf("ServeFile pre-error Content-Type = %q, want application/atom+xml", ct)
	}
}

// TestServeHTTP_404ContentType_PlainText verifies the end-to-end fix: any 404
// response emitted by ServeHTTP carries Content-Type text/plain, not a
// feed-specific media type. The test exercises both the fileCheck-failure path
// (file never exists) and the ServeFile-failure path (file removed after
// fileCheck would have passed).
func TestServeHTTP_404ContentType_PlainText(t *testing.T) {
	dir := t.TempDir()
	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}

	// Case 1: file never existed — fileCheck 404 path.
	rw := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/news.atom.xml", nil)
	s.ServeHTTP(rw, rq)
	if rw.Code != http.StatusNotFound {
		t.Fatalf("case 1: expected 404, got %d", rw.Code)
	}
	ct := rw.Header().Get("Content-Type")
	if strings.Contains(ct, "application/atom+xml") {
		t.Errorf("case 1: 404 Content-Type = %q; must not contain application/atom+xml", ct)
	}
}

// TestServeHTTP_404ContentType_AfterContentTypeSet verifies that even when
// Content-Type has been set on the ResponseWriter before WriteHeader(404),
// ServeHTTP resets it to text/plain so HTTP clients never parse a 404
// body as Atom XML. The test manually sets a feed Content-Type first to
// simulate the state that ServeFile leaves the ResponseWriter in when it
// encounters an os.Stat failure after already writing the Content-Type header.
func TestServeHTTP_404ContentType_AfterContentTypeSet(t *testing.T) {
	dir := t.TempDir()
	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}

	rw := httptest.NewRecorder()
	// Pre-set a feed Content-Type to mimic what ServeFile does before failing.
	rw.Header().Set("Content-Type", "application/atom+xml")

	rq := httptest.NewRequest(http.MethodGet, "/nonexistent.atom.xml", nil)
	s.ServeHTTP(rw, rq)
	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rw.Code)
	}
	ct := rw.Header().Get("Content-Type")
	if strings.Contains(ct, "application/atom+xml") {
		t.Errorf("404 Content-Type = %q; ServeHTTP must reset it to text/plain before WriteHeader", ct)
	}
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("404 Content-Type = %q; want text/plain", ct)
	}
}

// ---------------------------------------------------------------------------
// Checksum cache (AUDIT.md: "SHA-256 checksums recomputed on every directory
// listing request")
// ---------------------------------------------------------------------------

// TestFileChecksum_CacheReturnsSameValue verifies that calling fileChecksum
// twice for the same unchanged file returns the expected SHA-256 digest on
// both calls (not a stale or empty cache hit).
func TestFileChecksum_CacheReturnsSameValue(t *testing.T) {
	dir := t.TempDir()
	content := []byte("cached content")
	path := filepath.Join(dir, "feed.atom.xml")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(content))

	first, err := fileChecksum(path)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := fileChecksum(path)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first != want {
		t.Errorf("first = %q, want %q", first, want)
	}
	if second != want {
		t.Errorf("second = %q, want %q", second, want)
	}
}

// TestFileChecksum_CacheInvalidatedOnModify verifies that when a file is
// overwritten (changing its mtime), fileChecksum discards the stale cache
// entry and returns the new digest.
func TestFileChecksum_CacheInvalidatedOnModify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feed.atom.xml")
	v1 := []byte("version one")
	if err := os.WriteFile(path, v1, 0o644); err != nil {
		t.Fatal(err)
	}
	sum1, err := fileChecksum(path)
	if err != nil {
		t.Fatalf("first checksum: %v", err)
	}

	// Wait at least one filesytem mtime tick so the new write has a
	// distinct ModTime. On most operating systems mtime has 1-second
	// granularity on FAT and 1-nanosecond on ext4/tmpfs, so sleeping
	// a short period is sufficient for CI runners using tmpfs.
	time.Sleep(10 * time.Millisecond)

	v2 := []byte("version two — modified")
	if err := os.WriteFile(path, v2, 0o644); err != nil {
		t.Fatal(err)
	}
	// Force a distinct mtime in case the filesystem has coarse-grained
	// mtime and the sleep above was not enough.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	sum2, err := fileChecksum(path)
	if err != nil {
		t.Fatalf("second checksum: %v", err)
	}
	if sum1 == sum2 {
		t.Errorf("expected different digests after file modification, got same: %q", sum1)
	}
	want2 := fmt.Sprintf("%x", sha256.Sum256(v2))
	if sum2 != want2 {
		t.Errorf("post-modify checksum = %q, want %q", sum2, want2)
	}
}

// TestFileChecksum_MissingFile_ReturnsError verifies that fileChecksum returns
// a non-nil error and an empty string when the target path does not exist.
// The cache must not be consulted for an os.Stat failure.
func TestFileChecksum_MissingFile_ReturnsError(t *testing.T) {
	sum, err := fileChecksum("/nonexistent/does/not/exist.atom.xml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if sum != "" {
		t.Errorf("expected empty string on error, got %q", sum)
	}
}

// TestFileChecksum_ConcurrentSafe verifies that fileChecksum is safe under
// concurrent access. Many goroutines hash the same file simultaneously;
// all must receive the same correct digest without data races.
func TestFileChecksum_ConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	content := []byte("concurrent access test payload")
	path := filepath.Join(dir, "news.atom.xml")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(content))

	const workers = 20
	results := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			results[i], errs[i] = fileChecksum(path)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d: unexpected error: %v", i, err)
		}
		if results[i] != want {
			t.Errorf("worker %d: got %q, want %q", i, results[i], want)
		}
	}
}

// TestChecksumCache_GetSet exercises the cache helper methods directly,
// confirming that get returns false for an unknown path and true for a
// previously stored entry with matching mtime.
func TestChecksumCache_GetSet(t *testing.T) {
	c := &checksumCache{items: make(map[string]checksumEntry)}
	now := time.Now()

	if _, ok := c.get("/no/such/path", now); ok {
		t.Error("get on empty cache should return false")
	}

	c.set("/some/path", now, "aabbcc")
	sum, ok := c.get("/some/path", now)
	if !ok {
		t.Error("get after set should return true")
	}
	if sum != "aabbcc" {
		t.Errorf("cached sum = %q, want aabbcc", sum)
	}

	// Different mtime → cache miss.
	if _, ok := c.get("/some/path", now.Add(time.Second)); ok {
		t.Error("get with different mtime should return false (stale)")
	}
}

// TestOpenDirectory_ListingIncludesChecksum verifies that a directory listing
// generated by openDirectory includes a non-empty SHA-256 checksum for each
// regular file present. This is a regression guard to ensure the cache does
// not silently suppress the checksum field from the rendered Markdown.
func TestOpenDirectory_ListingIncludesChecksum(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "news.atom.xml"), []byte("<feed/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	listing, err := openDirectory(dir)
	if err != nil {
		t.Fatalf("openDirectory: %v", err)
	}
	// A non-empty checksum is 64 hex characters. The listing must contain a
	// sequence that looks like a SHA-256 hex digest.
	if !strings.Contains(listing, "news.atom.xml") {
		t.Error("listing does not include the expected file name")
	}
	// Crude but sufficient: a real checksum is 64 hex chars; "(checksum unavailable)"
	// would fail this length check.
	for _, line := range strings.Split(listing, "\n") {
		if strings.Contains(line, "news.atom.xml") {
			if strings.Contains(line, "(checksum unavailable)") {
				t.Errorf("listing shows checksum error for news.atom.xml: %s", line)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// fileType MIME fallback (AUDIT.md: "fileType() returns text/html for
// unrecognised extensions")
// ---------------------------------------------------------------------------

// TestFileType_UnknownExtension_OctetStream verifies that a completely unknown
// file extension is served as application/octet-stream rather than the
// misleading text/html that the old default produced.  A browser that receives
// text/html for a binary file will attempt to render it as markup, corrupting
// the download.
func TestFileType_UnknownExtension_OctetStream(t *testing.T) {
	got, err := fileType("archive.xyzunknownext")
	if err != nil {
		t.Fatalf("fileType: unexpected error: %v", err)
	}
	if got == "text/html" {
		t.Errorf("fileType for unknown extension returned %q; must not return text/html", got)
	}
	if got != "application/octet-stream" {
		t.Errorf("fileType for unknown extension = %q, want application/octet-stream", got)
	}
}

// TestFileType_CSS_NotHTML verifies that .css files receive a CSS Content-Type
// (from Go's built-in MIME database) and not text/html.  The go standard
// library's mime package includes text/css; charset=utf-8 for .css regardless
// of the operating system, so this assertion is portable.
func TestFileType_CSS_NotHTML(t *testing.T) {
	got, err := fileType("style.css")
	if err != nil {
		t.Fatalf("fileType: unexpected error: %v", err)
	}
	if got == "text/html" {
		t.Errorf("fileType(.css) = %q; must not return text/html", got)
	}
	if !strings.Contains(got, "text/css") {
		t.Errorf("fileType(.css) = %q; expected to contain text/css", got)
	}
}

// TestFileType_JSON_NotHTML verifies that .json files receive
// application/json and not text/html.
func TestFileType_JSON_NotHTML(t *testing.T) {
	got, err := fileType("data.json")
	if err != nil {
		t.Fatalf("fileType: unexpected error: %v", err)
	}
	if got == "text/html" {
		t.Errorf("fileType(.json) = %q; must not return text/html", got)
	}
}

// TestServeHTTP_CSSFile_ContentType verifies the end-to-end behaviour: a .css
// file present in NewsDir is served with a Content-Type that does not contain
// text/html.  Browsers that receive text/html for a stylesheet ignore it
// entirely rather than applying the rules.
func TestServeHTTP_CSSFile_ContentType(t *testing.T) {
	dir := t.TempDir()
	cssContent := []byte("body { color: red; }")
	if err := os.WriteFile(filepath.Join(dir, "style.css"), cssContent, 0o644); err != nil {
		t.Fatal(err)
	}
	s := &NewsServer{NewsDir: dir, Stats: statsForTest(dir)}
	rw := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/style.css", nil)
	s.ServeHTTP(rw, rq)
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	ct := rw.Header().Get("Content-Type")
	if ct == "text/html" {
		t.Errorf("CSS file served with Content-Type text/html; want text/css")
	}
	if !strings.Contains(ct, "text/css") {
		t.Errorf("CSS Content-Type = %q; expected to contain text/css", ct)
	}
}

// ---------------------------------------------------------------------------
// statsForTest constructs a NewsStats suitable for use in tests. It
// initialises DownloadLangs directly rather than calling Load so that
// the embedded sync.RWMutex is never used before the value is returned.
// Returning a struct that embeds a mutex after the mutex has been used
// (as Load does) triggers a "copies lock value" go vet diagnostic.
func statsForTest(dir string) stats.NewsStats {
	sf := filepath.Join(dir, "stats.json")
	return stats.NewsStats{
		StateFile:     sf,
		DownloadLangs: make(map[string]int),
	}
}
