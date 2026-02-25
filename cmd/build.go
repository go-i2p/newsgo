package cmd

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	builder "github.com/go-i2p/newsgo/builder"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// buildCmd represents the build command
var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build Atom XML newsfeeds from HTML entries",
	Run: func(cmd *cobra.Command, args []string) {
		// Populate the shared config struct from cobra flags and any config
		// file. Without this call every field of c is the zero value (empty
		// string), causing the very first os.Stat(c.NewsFile) to panic.
		viper.Unmarshal(c)

		f, e := os.Stat(c.NewsFile)
		if e != nil {
			log.Fatalf("build: stat %s: %v", c.NewsFile, e)
		}
		if f.IsDir() {
			err := filepath.Walk(c.NewsFile,
				func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					// Only process files named "entries.html".  Filtering by
					// extension alone would cause index.html, error pages, and
					// other co-located HTML files to be parsed as Atom entry
					// sources, producing spurious or malformed output feeds.
					if filepath.Base(path) == "entries.html" {
						build(path)
					}
					return nil
				})
			if err != nil {
				log.Println(err)
			}
		} else {
			build(c.NewsFile)
		}
	},
}

func init() {
	rootCmd.AddCommand(buildCmd)
	buildCmd.Flags().String("newsfile", "data", "entries to pass to news generator. If passed a directory, all 'entries.html' files in the directory will be processed")
	// Flag name matches README: --blockfile (was incorrectly "blocklist").
	// config.Conf.BlockList carries the mapstructure:"blockfile" tag so that
	// viper.Unmarshal maps the flag value to the right field.
	buildCmd.Flags().String("blockfile", "data/blocklist.xml", "block list file to pass to news generator")
	buildCmd.Flags().String("releasejson", "data/releases.json", "json file describing an update to pass to news generator")
	buildCmd.Flags().String("feedtitle", "I2P News", "title to use for the RSS feed to pass to news generator")
	buildCmd.Flags().String("feedsubtitle", "News feed, and router updates", "subtitle to use for the RSS feed to pass to news generator")
	buildCmd.Flags().String("feedsite", "http://i2p-projekt.i2p", "site for the RSS feed to pass to news generator")
	buildCmd.Flags().String("feedmain", defaultFeedURL(), "Primary newsfeed for updates to pass to news generator")
	buildCmd.Flags().String("feedbackup", "http://dn3tvalnjz432qkqsvpfdqrwpqkw3ye4n4i2uyfr4jexvo3sp5ka.b32.i2p/news/news.atom.xml", "Backup newsfeed for updates to pass to news generator")
	// Flag name matches README: --feeduri (was incorrectly "feeduid").
	// config.Conf.FeedUuid carries the mapstructure:"feeduri" tag.
	buildCmd.Flags().String("feeduri", "", "UUID to use for the RSS feed to pass to news generator. Random if omitted")
	buildCmd.Flags().String("builddir", "build", "Build directory to output feeds to")
	// Note: samaddr is registered on serveCmd inside cmd/serve.go; do NOT
	// re-register it here — pflag panics on duplicate flag definitions.

	viper.BindPFlags(buildCmd.Flags())
}

func defaultFeedURL() string {
	// Always return the static fallback URL as the default.  The previous
	// implementation called onramp.NewGarlic at flag-init time (before any
	// flags are parsed and before c is populated), meaning c.SamAddr was
	// always empty and the garlic Listen always failed, so the static URL
	// was returned anyway.  Removing the live-probe eliminates an unnecessary
	// SAM connection attempt during flag initialization.
	return "http://tc73n4kivdroccekirco7rhgxdg5f3cjvbaapabupeyzrqwv5guq.b32.i2p/news.atom.xml"
}

func build(newsFile string) {
	news := builder.Builder(newsFile, c.ReleaseJsonFile, c.BlockList)
	news.TITLE = c.FeedTitle
	news.SITEURL = c.FeedSite
	news.MAINFEED = c.FeedMain
	news.BACKUPFEED = c.FeedBackup
	news.SUBTITLE = c.FeedSubtitle
	// Use the user-supplied UUID when provided; generate a random one only
	// when none was given (the previous code had this condition inverted).
	if c.FeedUuid != "" {
		news.URNID = c.FeedUuid
	} else {
		news.URNID = uuid.NewString()
	}

	// BaseEntriesHTMLPath must point to the root entries.html in the top-
	// level source directory (c.NewsFile), not to a sub-path derived from
	// the individual file being processed.  Using newsFile here produced a
	// path like "data/translations/de/entries.html/entries.html" which is
	// always invalid.
	base := filepath.Join(c.NewsFile, "entries.html")
	if newsFile != base {
		news.Feed.BaseEntriesHTMLPath = base
	}
	if feed, err := news.Build(); err != nil {
		log.Printf("Build error: %s", err)
	} else {
		// Output filename is derived from the individual file being processed
		// (newsFile), not from the root directory flag (c.NewsFile).  Using
		// c.NewsFile caused every file in the walk to map to the same output
		// path, silently overwriting all but the last feed.
		filename := outputFilename(newsFile, c.NewsFile)
		if err := os.MkdirAll(filepath.Join(c.BuildDir, filepath.Dir(filename)), 0o755); err != nil {
			log.Fatalf("build: mkdir %s: %v", filepath.Join(c.BuildDir, filepath.Dir(filename)), err)
		}
		if err = os.WriteFile(filepath.Join(c.BuildDir, filename), []byte(feed), 0o644); err != nil {
			log.Fatalf("build: write %s: %v", filepath.Join(c.BuildDir, filename), err)
		}
	}
}

// outputFilename derives the relative output path (.atom.xml) for a given
// source entries.html path.  newsRoot is the walk start directory (c.NewsFile);
// stripping the root prefix prevents output files from landing under a spurious
// source-directory sub-path inside BuildDir (e.g. "build/data/news.atom.xml"
// instead of "build/news.atom.xml").  For single-file invocations where
// newsFile == newsRoot, only the base name is used so the result is still valid.
func outputFilename(newsFile, newsRoot string) string {
	rel, err := filepath.Rel(newsRoot, newsFile)
	if err != nil || rel == "." {
		rel = filepath.Base(newsFile)
	}
	f := strings.Replace(rel, ".html", ".atom.xml", -1)
	f = strings.Replace(f, "entries.", "news_", -1)
	// Use "translations/" (with path separator) instead of bare "translations"
	// to avoid producing a leading separator that makes filepath.Join treat the
	// result as an absolute path.
	f = strings.Replace(f, "translations"+string(filepath.Separator), "", -1)
	return strings.Replace(f, "news_atom", "news.atom", -1)
}
