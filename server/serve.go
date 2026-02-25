// Package newsserver provides an http.Handler that serves I2P news feed files
// from a directory and tracks per-language su3 download statistics.
package newsserver

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
		rw.WriteHeader(404)
		return
	}
	if err := n.ServeFile(file, rq, rw); err != nil {
		log.Println("ServeHTTP:", err.Error())
		rw.WriteHeader(404)
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
		return "text/html", nil
	}
}

// fileChecksum computes a SHA-256 checksum for the file at path by streaming
// its contents through a hash.Hash.  Reading in chunks avoids allocating the
// entire file content into memory, which matters for large .su3 files that
// can be several megabytes each and are present in every directory listing.
func fileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("fileChecksum: open %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("fileChecksum: hash %s: %w", path, err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
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
func formatEntryLine(wd string, entry os.DirEntry, info os.FileInfo) string {
	log.Println(entry.Name(), entry.IsDir())
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

// serveStaticFile reads the regular file at path and writes its contents to rw,
// logging the filename and resolved content-type.
func serveStaticFile(file, ftype string, rw http.ResponseWriter) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("ServeFile: %s", err)
	}
	log.Println("ServeFile: ", file, ftype)
	rw.Write(data) //nolint:errcheck
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
	rw.Header().Add("Content-Type", ftype)
	// Only the canonical stats-graph basename is rendered dynamically.
	// An actual .svg file on disk (e.g. a logo) should be served as a
	// static file through the normal path below.
	if filepath.Base(file) == statsGraphFilename {
		n.Stats.Graph(rw)
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
	return serveStaticFile(file, ftype, rw)
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
