package cmd

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	builder "github.com/go-i2p/newsgo/builder"
	"github.com/go-i2p/onramp"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// buildCmd represents the build command
var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build newsfeeds from XML",
	Run: func(cmd *cobra.Command, args []string) {
		// Populate the shared config struct from cobra flags and any config
		// file. Without this call every field of c is the zero value (empty
		// string), causing the very first os.Stat(c.NewsFile) to panic.
		viper.Unmarshal(c)

		f, e := os.Stat(c.NewsFile)
		if e != nil {
			panic(e)
		}
		if f.IsDir() {
			err := filepath.Walk(c.NewsFile,
				func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					ext := filepath.Ext(path)
					if ext == ".html" {
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
	if c.SamAddr == "" {
		return "http://tc73n4kivdroccekirco7rhgxdg5f3cjvbaapabupeyzrqwv5guq.b32.i2p/news.atom.xml"
	}
	garlic, _ := onramp.NewGarlic("newsgo", c.SamAddr, onramp.OPT_DEFAULTS)
	defer garlic.Close()
	ln, err := garlic.Listen()
	if err != nil {
		return "http://tc73n4kivdroccekirco7rhgxdg5f3cjvbaapabupeyzrqwv5guq.b32.i2p/news.atom.xml"
	}
	defer ln.Close()
	return "http://" + ln.Addr().String() + "/news.atom.xml"
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
		filename := strings.Replace(strings.Replace(strings.Replace(strings.Replace(newsFile, ".html", ".atom.xml", -1), "entries.", "news_", -1), "translations", "", -1), "news_atom", "news.atom", -1)
		if err := os.MkdirAll(filepath.Join(c.BuildDir, filepath.Dir(filename)), 0755); err != nil {
			panic(err)
		}
		if err = os.WriteFile(filepath.Join(c.BuildDir, filename), []byte(feed), 0644); err != nil {
			panic(err)
		}
	}
}
