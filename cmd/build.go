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
		// For some reason this is the only way passing booleans from cobra to viper works
		viper.GetViper().Set("i2p", i2p)

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
	buildCmd.Flags().String("blocklist", "data/blocklist.xml", "block list file to pass to news generator")
	buildCmd.Flags().String("releasejson", "data/releases.json", "json file describing an update to pass to news generator")
	buildCmd.Flags().String("feedtitle", "I2P News", "title to use for the RSS feed to pass to news generator")
	buildCmd.Flags().String("feedsubtitle", "News feed, and router updates", "subtitle to use for the RSS feed to pass to news generator")
	buildCmd.Flags().String("feedsite", "http://i2p-projekt.i2p", "site for the RSS feed to pass to news generator")
	buildCmd.Flags().String("feedmain", defaultFeedURL(), "Primary newsfeed for updates to pass to news generator")
	buildCmd.Flags().String("feedbackup", "http://dn3tvalnjz432qkqsvpfdqrwpqkw3ye4n4i2uyfr4jexvo3sp5ka.b32.i2p/news/news.atom.xml", "Backup newsfeed for updates to pass to news generator")
	buildCmd.Flags().String("feeduid", "", "UUID to use for the RSS feed to pass to news generator. Random if omitted")
	buildCmd.Flags().String("builddir", "build", "Build directory to output feeds to")
	buildCmd.Flags().BoolVar(&i2p, "i2p", false, "Enable I2P support")

	viper.BindPFlags(buildCmd.Flags())

}

func defaultFeedURL() string {
	if !c.I2P {
		return "http://tc73n4kivdroccekirco7rhgxdg5f3cjvbaapabupeyzrqwv5guq.b32.i2p/news.atom.xml"
	}
	garlic := &onramp.Garlic{}
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
	if c.FeedUuid == "" {
		news.URNID = c.FeedUuid
	} else {
		news.URNID = uuid.NewString()
	}

	base := filepath.Join(newsFile, "entries.html")
	if newsFile != base {
		news.Feed.BaseEntriesHTMLPath = base
	}
	if feed, err := news.Build(); err != nil {
		log.Printf("Build error: %s", err)
	} else {
		filename := strings.Replace(strings.Replace(strings.Replace(strings.Replace(c.NewsFile, ".html", ".atom.xml", -1), "entries.", "news_", -1), "translations", "", -1), "news_atom", "news.atom", -1)
		if err := os.MkdirAll(filepath.Join(c.BuildDir, filepath.Dir(filename)), 0755); err != nil {
			panic(err)
		}
		if err = os.WriteFile(filepath.Join(c.BuildDir, filename), []byte(feed), 0644); err != nil {
			panic(err)
		}
	}
}
