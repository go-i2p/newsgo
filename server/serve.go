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

type NewsServer struct {
	NewsDir string
	Stats   stats.NewsStats
}

var serveTest http.Handler = &NewsServer{}

func (n *NewsServer) ServeHTTP(rw http.ResponseWriter, rq *http.Request) {
	path := rq.URL.Path
	file := filepath.Join(n.NewsDir, path)
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
	if filepath.Ext(file) == ".svg" {
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

// openDirectory returns a Markdown directory listing for wd. It returns an
// error rather than calling log.Fatal so that callers inside HTTP handlers
// can surface a proper HTTP error response instead of killing the process.
func openDirectory(wd string) (string, error) {
	files, err := os.ReadDir(wd)
	if err != nil {
		return "", fmt.Errorf("openDirectory: %w", err)
	}
	var readme string
	log.Println("Navigating directory:", wd)
	nwd := strings.Join(strings.Split(wd, "/")[1:], "/")
	readme += fmt.Sprintf("%s\n", filepath.Base(wd))
	readme += fmt.Sprintf("%s\n", head(len(filepath.Base(wd))))
	readme += fmt.Sprintf("%s\n", "")
	readme += fmt.Sprintf("%s\n", "![language stats](langstats.svg)")
	readme += fmt.Sprintf("%s\n", "")
	readme += fmt.Sprintf("%s\n", "**Directory Listing:**")
	readme += fmt.Sprintf("%s\n", "")
	for _, entry := range files {
		info, err := entry.Info()
		if err != nil {
			log.Println("Listing: stat error:", err)
			continue
		}
		if !entry.IsDir() {
			// Use log.Println so this output goes through the same log
			// destination as all other server messages (timestamped, etc.).
			log.Println(entry.Name(), entry.IsDir())
			xname := filepath.Join(wd, entry.Name())
			// Stream the file through sha256 rather than reading it fully
			// into memory.  This avoids large per-request allocations when
			// the build directory contains multi-megabyte .su3 files.
			sum, err := fileChecksum(xname)
			if err != nil {
				log.Println("Listing error:", err)
				sum = "(checksum unavailable)"
			}
			readme += fmt.Sprintf(" - [%s](%s/%s) : `%d` : `%s` - `%s`\n", entry.Name(), filepath.Base(nwd), filepath.Base(entry.Name()), info.Size(), info.Mode(), sum)
		} else {
			log.Println(entry.Name(), entry.IsDir())
			readme += fmt.Sprintf(" - [%s](%s/) : `%d` : `%s`\n", entry.Name(), entry.Name(), info.Size(), info.Mode())
		}
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
	if ftype == "image/svg+xml" {
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
		content, err := openDirectory(file)
		if err != nil {
			return fmt.Errorf("ServeFile: %w", err)
		}
		rw.Write(hTML(content))
		return nil
	}
	bytes, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("ServeFile: %s", err)
	}

	log.Println("ServeFile: ", file, ftype)
	rw.Write(bytes)
	return nil
}

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
