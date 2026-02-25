// Package newsserver provides an http.Handler that serves I2P news feed files
// from a directory and tracks per-language su3 download statistics.
package newsserver

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	stats "github.com/go-i2p/newsgo/server/stats"
	"gitlab.com/golang-commonmark/markdown"
)

// statsGraphFilename is the canonical URL-path basename for the
// dynamically-generated language-statistics SVG bar chart. This file is
// never written to NewsDir — it is rendered on-demand by Stats.Graph — so
// it is the only .svg path that legitimately bypasses the on-disk existence
// check in fileCheck. All other *.svg requests go through os.Stat and
// receive HTTP 404 if no matching file exists.
const statsGraphFilename = "langstats.svg"

// checksumEntry holds a single cached SHA-256 digest together with the file
// modification time used to detect stale entries.
type checksumEntry struct {
	modTime time.Time
	sum     string
}

// checksumCache is a concurrency-safe, mtime-keyed store for SHA-256 digests.
// An entry is considered fresh only when its stored ModTime equals the current
// file ModTime, so the cache is never stale longer than one file modification.
type checksumCache struct {
	mu    sync.RWMutex
	items map[string]checksumEntry
}

// get returns (sum, true) when a fresh (non-stale) entry exists for path.
func (c *checksumCache) get(path string, modTime time.Time) (string, bool) {
	c.mu.RLock()
	entry, ok := c.items[path]
	c.mu.RUnlock()
	if ok && entry.modTime.Equal(modTime) {
		return entry.sum, true
	}
	return "", false
}

// set stores a digest for path with the given modification time.
func (c *checksumCache) set(path string, modTime time.Time, sum string) {
	c.mu.Lock()
	c.items[path] = checksumEntry{modTime: modTime, sum: sum}
	c.mu.Unlock()
}

// globalChecksumCache is the package-level instance used by fileChecksum.
// It is intentionally not a field of NewsServer so that a warm cache survives
// across multiple handler invocations within the same process lifetime.
var globalChecksumCache = &checksumCache{
	items: make(map[string]checksumEntry),
}

// NewsServer is an http.Handler that serves news feed files from NewsDir and
// records su3 download statistics via Stats.
type NewsServer struct {
	NewsDir string
	Stats   stats.NewsStats
}

var serveTest http.Handler = &NewsServer{}

// containsPath reports whether target is contained within (or equal to) root.
// Both paths must already be absolute and clean (produced by filepath.Clean).
func containsPath(root, target string) bool {
	// Exact match: the request resolves to the root directory itself.
	if target == root {
		return true
	}
	// Prefix match: target must start with root followed by the OS path
	// separator so that a root of "/srv/news" does not falsely contain
	// "/srv/news-extra/secret".
	return strings.HasPrefix(target, root+string(filepath.Separator))
}

// ServeHTTP implements http.Handler. It resolves the request URL path against
// NewsDir, rejects path traversal attempts, and delegates to ServeFile.
func (n *NewsServer) ServeHTTP(rw http.ResponseWriter, rq *http.Request) {
	path := rq.URL.Path
	file := filepath.Join(n.NewsDir, path)
	// Reject any request whose resolved path escapes NewsDir.  filepath.Join
	// calls filepath.Clean which resolves ".." components, so comparing the
	// cleaned result against the cleaned NewsDir root is sufficient.
	newsDir := filepath.Clean(n.NewsDir)
	if !containsPath(newsDir, file) {
		log.Printf("ServeHTTP: path traversal rejected: %q", rq.URL.Path)
		http.Error(rw, "Bad Request", http.StatusBadRequest)
		return
	}
	if err := fileCheck(file); err != nil {
		log.Println("ServeHTTP:", err.Error())
		rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
		rw.WriteHeader(http.StatusNotFound)
		return
	}
	if err := n.ServeFile(file, rq, rw); err != nil {
		log.Println("ServeHTTP:", err.Error())
		// Reset Content-Type so that error responses do not carry a feed-
		// specific media type (e.g. application/atom+xml).  ServeFile sets the
		// Content-Type header before performing its os.Stat; if that stat
		// fails the header map already contains the wrong type.  Overwriting
		// it here (before WriteHeader flushes headers to the client) ensures
		// that HTTP clients receive a plain-text error response they can parse.
		rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
		rw.WriteHeader(http.StatusNotFound)
	}
}

func fileCheck(file string) error {
	// statsGraphFilename is generated on-demand by Stats.Graph and never
	// written to disk, so skip the existence check for that one name only.
	// Every other *.svg path (and all other extensions) must pass os.Stat.
	if filepath.Base(file) == statsGraphFilename {
		return nil
	}
	if _, err := os.Stat(file); err != nil {
		return fmt.Errorf("fileCheck: %s", err)
	}
	return nil
}

func fileType(file string) (string, error) {
	base := filepath.Base(file)
	if base == "" {
		return "", fmt.Errorf("fileType: Invalid file path passed to type determinator")
	}
	// filepath.Ext returns only the last dot-segment (e.g. ".xml" for
	// "news.atom.xml"), so compound extensions like ".atom.xml" must be
	// tested with strings.HasSuffix before falling back to filepath.Ext.
	if strings.HasSuffix(base, ".atom.xml") {
		// RFC 4287 / IANA media type for Atom feeds.
		return "application/atom+xml", nil
	}
	extension := filepath.Ext(base)
	switch extension {
	case ".su3":
		return "application/x-i2p-su3-news", nil
	case ".html":
		return "text/html", nil
	case ".xml":
		return "application/rss+xml", nil
	case ".svg":
		return "image/svg+xml", nil
	default:
		// Consult the OS / Go built-in MIME type database before falling back
		// to application/octet-stream. This ensures that CSS, JS, PNG and
		// other common files are served with the correct Content-Type instead
		// of the misleading text/html that the old default emitted.
		if t := mime.TypeByExtension(extension); t != "" {
			return t, nil
		}
		return "application/octet-stream", nil
	}
}

// fileChecksum returns the SHA-256 hex digest for the file at path.
// Digests are cached keyed by (path, mtime): when the file has not changed
// since the last computation the cached value is returned immediately,
// avoiding a full file read on every directory-listing request.
// A news-server build directory typically contains ~80–100 files; without
// caching every directory listing request would hash all files from disk.
func fileChecksum(path string) (string, error) {
	// Stat first to get the modification time for the cache key.
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("fileChecksum: stat %s: %w", path, err)
	}
	modTime := fi.ModTime()

	// Return cached digest when the file content has not changed.
	if sum, ok := globalChecksumCache.get(path, modTime); ok {
		return sum, nil
	}

	// Cache miss or stale entry: stream the file through sha256.
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("fileChecksum: open %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("fileChecksum: hash %s: %w", path, err)
	}
	sum := fmt.Sprintf("%x", h.Sum(nil))
	globalChecksumCache.set(path, modTime, sum)
	return sum, nil
}

// buildDirectoryHeader generates the Markdown heading block for a directory
// listing page: the base name of wd as the title, a ruler of equal signs, and
// fixed preamble lines including the language-stats SVG and a bold section label.
func buildDirectoryHeader(wd string) string {
	base := filepath.Base(wd)
	header := fmt.Sprintf("%s\n", base)
	header += fmt.Sprintf("%s\n", head(len(base)))
	header += fmt.Sprintf("%s\n", "")
	header += fmt.Sprintf("%s\n", "![language stats](langstats.svg)")
	header += fmt.Sprintf("%s\n", "")
	header += fmt.Sprintf("%s\n", "**Directory Listing:**")
	header += fmt.Sprintf("%s\n", "")
	return header
}

// formatEntryLine produces a single Markdown list item for a directory entry.
// For regular files the line includes the file size, permissions, and SHA-256
// checksum; for subdirectories the checksum is omitted and a trailing slash is
// appended to the link target. Checksum errors are logged and replaced with a
// human-readable placeholder rather than aborting the listing.
// The per-entry debug log that was here previously has been removed: it emitted
// one log line per file in the directory on every directory-listing request,
// producing ~60 log lines per request on a typical news server and drowning
// real operational events in noise.
func formatEntryLine(wd string, entry os.DirEntry, info os.FileInfo) string {
	if entry.IsDir() {
		return fmt.Sprintf(" - [%s](%s/) : `%d` : `%s`\n", entry.Name(), entry.Name(), info.Size(), info.Mode())
	}
	xname := filepath.Join(wd, entry.Name())
	sum, err := fileChecksum(xname)
	if err != nil {
		log.Println("Listing error:", err)
		sum = "(checksum unavailable)"
	}
	return fmt.Sprintf(" - [%s](%s) : `%d` : `%s` - `%s`\n", entry.Name(), entry.Name(), info.Size(), info.Mode(), sum)
}

// openDirectory returns a Markdown directory listing for wd. It returns an
// error rather than calling log.Fatal so that callers inside HTTP handlers
// can surface a proper HTTP error response instead of killing the process.
func openDirectory(wd string) (string, error) {
	files, err := os.ReadDir(wd)
	if err != nil {
		return "", fmt.Errorf("openDirectory: %w", err)
	}
	log.Println("Navigating directory:", wd)
	readme := buildDirectoryHeader(wd)
	for _, entry := range files {
		info, err := entry.Info()
		if err != nil {
			log.Println("Listing: stat error:", err)
			continue
		}
		readme += formatEntryLine(wd, entry, info)
	}
	return readme, nil
}

func hTML(mdtxt string) []byte {
	md := markdown.New(markdown.XHTMLOutput(true))
	return []byte(md.RenderToString([]byte(mdtxt)))
}

func head(num int) string {
	var r string
	for i := 0; i < num; i++ {
		r += "="
	}
	return r
}

// serveDirectory generates a Markdown directory listing for file, converts it
// to HTML, and writes the result to rw.
func serveDirectory(file string, rw http.ResponseWriter) error {
	content, err := openDirectory(file)
	if err != nil {
		return fmt.Errorf("ServeFile: %w", err)
	}
	rw.Write(hTML(content)) //nolint:errcheck
	return nil
}

// serveStaticFile streams the regular file at path to rw using
// http.ServeContent, which:
//
//   - reads the file in chunks instead of slurping the whole thing into a
//     []byte buffer — important for multi-MB .su3 news files;
//   - honours If-Modified-Since / If-None-Match, avoiding redundant transfers;
//   - honours Range requests, enabling resumable downloads.
//
// The Content-Type header must already be set on rw before this is called
// (ServeFile does this); http.ServeContent will not override an existing value.
func serveStaticFile(file, ftype string, rw http.ResponseWriter, rq *http.Request) error {
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("ServeFile: %s", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("ServeFile: stat %s: %w", file, err)
	}
	log.Println("ServeFile:", file, ftype)
	// http.ServeContent streams content and handles conditional/range GETs.
	// It uses the Content-Type already set in rw.Header() and will not sniff
	// or override it.
	http.ServeContent(rw, rq, filepath.Base(file), fi.ModTime(), f)
	return nil
}

// ServeFile determines the content type of file, increments su3 download
// statistics when appropriate, writes the Content-Type header, and either
// renders an HTML directory listing or streams the file contents to rw.
func (n *NewsServer) ServeFile(file string, rq *http.Request, rw http.ResponseWriter) error {
	ftype, err := fileType(file)
	if err != nil {
		return fmt.Errorf("ServeFile: %s", err)
	}
	if ftype == "application/x-i2p-su3-news" {
		// Log stats here
		n.Stats.Increment(rq)
	}
	// Set (not Add) so that any Content-Type written by upstream middleware is
	// replaced rather than duplicated. RFC 7231 §3.1.1.5 treats Content-Type
	// as a singleton field; duplicate values are non-conformant.
	rw.Header().Set("Content-Type", ftype)
	// Only the canonical stats-graph basename is rendered dynamically.
	// An actual .svg file on disk (e.g. a logo) should be served as a
	// static file through the normal path below.
	if filepath.Base(file) == statsGraphFilename {
		// Graph buffers the render internally; it only writes to rw when
		// rendering succeeds, so a failure here means no bytes have been
		// committed yet and we can safely send an HTTP 500 response.
		if err := n.Stats.Graph(rw); err != nil {
			log.Printf("ServeFile: stats graph render failed: %v", err)
			rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
			rw.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(rw, "Internal Server Error")
		}
		return nil
	}
	// Check whether the path is a directory. The os.Stat error must not be
	// discarded: if the file was removed between fileCheck and ServeFile,
	// f would be nil and f.IsDir() would panic.
	f, err := os.Stat(file)
	if err != nil {
		return fmt.Errorf("ServeFile: stat %s: %w", file, err)
	}
	if f.IsDir() {
		return serveDirectory(file, rw)
	}
	return serveStaticFile(file, ftype, rw, rq)
}

// Serve constructs a NewsServer rooted at newsDir and loads any previously
// persisted download statistics from newsStats. Both paths are stored on the
// returned server; newsStats is also passed to stats.NewsStats.Load.
func Serve(newsDir, newsStats string) *NewsServer {
	s := &NewsServer{
		NewsDir: newsDir,
		Stats: stats.NewsStats{
			StateFile: newsStats,
		},
	}
	s.Stats.Load()
	return s
}
