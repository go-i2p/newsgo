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
		if !f.IsDir() {
			// Single-file mode: unchanged behaviour.
			build(c.NewsFile)
			return
		}

		// Directory mode: determine the (platform, status) pairs to build.
		type pair struct{ platform, status string }
		var pairs []pair

		switch {
		case c.Platform != "" && c.Status != "":
			// Explicit platform+status: build exactly one combination.
			pairs = []pair{{c.Platform, c.Status}}
		case c.Platform != "":
			// Platform specified without status: try every known status.
			for _, s := range builder.KnownStatuses() {
				pairs = append(pairs, pair{c.Platform, s})
			}
		default:
			// Neither flag set: build the default (Linux) tree first, then
			// every (platform, status) combination present in the data dir.
			pairs = append(pairs, pair{"", ""})
			for _, p := range builder.KnownPlatforms() {
				for _, s := range builder.KnownStatuses() {
					pairs = append(pairs, pair{p, s})
				}
			}
		}

		for _, pr := range pairs {
			buildPlatform(pr.platform, pr.status)
		}
	},
}

func init() {
	rootCmd.AddCommand(buildCmd)
	buildCmd.Flags().String("platform", "", "target platform (linux|mac|mac-arm64|win|android|ios); empty = all")
	buildCmd.Flags().String("status", "", "release channel (stable|beta|rc|alpha); empty = all found")
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
	buildCmd.Flags().String("translationsdir", "", "Directory containing entries.{locale}.html translation files. Defaults to the 'translations' subdirectory of --newsfile when omitted")
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

// resolveOverrideFile returns platformPath when that file exists, otherwise
// returns globalFallback.  It encodes the "platform-specific file overrides
// the global file, but only when present" policy used for both releases.json
// and blocklist.xml.
func resolveOverrideFile(platformPath, globalFallback string) string {
	if _, err := os.Stat(platformPath); err == nil {
		return platformPath
	}
	return globalFallback
}

// buildPlatform builds all feeds (canonical English + locale variants) for a
// single (platform, status) combination.  When platform is empty or "linux",
// the top-level data directory is used (preserving the existing default
// behaviour).
//
// Opt-in rule for non-default platforms: the platform data directory must
// exist — this is the operator's signal that the platform is configured.
// Within that directory, releases.json and blocklist.xml are optional
// override files; absent files fall back to their global counterparts so
// that the global (jar) newsfeed content is always the baseline.
//
// Articles from the global (jar) feed are merged into every per-platform
// feed: when a platform-specific entries.html exists it is loaded first and
// the global entries.html is appended via Feed.BaseEntriesHTMLPath; when no
// platform entries.html is present the global file is used directly.
func buildPlatform(platform, status string) {
	dataDir := builder.PlatformDataDir(c.NewsFile, platform, status)
	isDefault := platform == "" || platform == "linux"

	// For non-default platforms the data directory must exist; a missing
	// directory means the combination has not been set up yet — skip silently.
	if !isDefault {
		if _, err := os.Stat(dataDir); err != nil {
			return
		}
	}

	// Resolve releases.json.
	// Default tree: honour the explicit --releasejson flag unchanged.
	// Platform tree: use the platform-specific file when present; fall back
	// to the global releases.json so the jar feed is always the baseline.
	var releasesPath string
	if isDefault {
		releasesPath = c.ReleaseJsonFile
	} else {
		releasesPath = resolveOverrideFile(
			filepath.Join(dataDir, "releases.json"),
			c.ReleaseJsonFile,
		)
	}
	// Gate: the resolved releases.json must actually exist.
	if _, err := os.Stat(releasesPath); err != nil {
		if isDefault {
			log.Printf("build: skipping default tree: releases.json not found at %s", releasesPath)
		} else {
			log.Printf("build: skipping %s/%s: no releases.json found (platform or global)", platform, status)
		}
		return
	}

	// Resolve blocklist.xml.
	// Platform-specific blocklist overrides the global one when present;
	// otherwise the global (jar) blocklist is used as the baseline.
	var blocklistPath string
	if isDefault {
		blocklistPath = c.BlockList
	} else {
		blocklistPath = resolveOverrideFile(
			filepath.Join(dataDir, "blocklist.xml"),
			c.BlockList,
		)
	}

	canonicalEntries := filepath.Join(c.NewsFile, "entries.html")

	// Determine the entries.html to use as the primary source for the
	// canonical English feed.
	// For non-default platforms: use the platform-specific overlay when it
	// exists; otherwise fall back to the global canonical file.
	// The global entries are always merged in via Feed.BaseEntriesHTMLPath
	// inside buildForPlatform whenever the primary source differs from the
	// global canonical file, ensuring that jar-feed articles are always
	// present in every per-platform output.
	entriesPath := canonicalEntries
	if !isDefault {
		platformEntries := filepath.Join(dataDir, "entries.html")
		if _, err := os.Stat(platformEntries); err == nil {
			entriesPath = platformEntries
		}
	}

	// Resolve the translations directory.  For the default tree honour the
	// explicit --translationsdir flag.  For platform-specific trees prefer the
	// per-platform translations directory and fall back to the top-level one.
	var transDir string
	if isDefault {
		transDir = c.TranslationsDir
		if transDir == "" {
			transDir = filepath.Join(c.NewsFile, "translations")
		}
	} else {
		platformTransDir := filepath.Join(dataDir, "translations")
		if _, err := os.Stat(platformTransDir); err == nil {
			transDir = platformTransDir
		} else {
			transDir = filepath.Join(c.NewsFile, "translations")
		}
	}

	// Build canonical English feed.
	buildForPlatform(entriesPath, dataDir, releasesPath, blocklistPath, canonicalEntries, platform, status)

	// Build per-locale feeds.
	for _, tf := range builder.DetectTranslationFiles(transDir) {
		buildForPlatform(tf, dataDir, releasesPath, blocklistPath, canonicalEntries, platform, status)
	}
}

// buildForPlatform is the per-file build step used by buildPlatform.  It is
// analogous to the existing build() function but accepts explicit dataDir,
// releasesPath, blocklistPath, and platform/status parameters instead of
// reading them from the global config directly.
//
// blocklistPath is the already-resolved blocklist file (platform-specific when
// present, global jar-feed blocklist otherwise); releasesPath likewise.
// canonicalEntries is the global jar-feed entries.html; it is set as
// Feed.BaseEntriesHTMLPath whenever newsFile differs from it so that global
// articles are always merged into the per-platform output.
func buildForPlatform(newsFile, dataDir, releasesPath, blocklistPath, canonicalEntries, platform, status string) {
	news := builder.Builder(newsFile, releasesPath, blocklistPath)
	news.Language = builder.LocaleFromPath(newsFile)
	news.TITLE = c.FeedTitle
	news.SITEURL = c.FeedSite
	news.MAINFEED = c.FeedMain
	news.BACKUPFEED = c.FeedBackup
	news.SUBTITLE = c.FeedSubtitle
	if c.FeedUuid != "" {
		news.URNID = c.FeedUuid
	} else {
		news.URNID = uuid.NewString()
	}
	if newsFile != canonicalEntries {
		news.Feed.BaseEntriesHTMLPath = canonicalEntries
	}
	if feed, err := news.Build(); err != nil {
		log.Printf("Build error: %s", err)
	} else {
		filename := outputFilenameForPlatform(newsFile, dataDir, platform, status)
		if err := os.MkdirAll(filepath.Join(c.BuildDir, filepath.Dir(filename)), 0o755); err != nil {
			log.Fatalf("build: mkdir %s: %v", filepath.Join(c.BuildDir, filepath.Dir(filename)), err)
		}
		if err = os.WriteFile(filepath.Join(c.BuildDir, filename), []byte(feed), 0o644); err != nil {
			log.Fatalf("build: write %s: %v", filepath.Join(c.BuildDir, filename), err)
		}
	}
}

func build(newsFile string) {
	news := builder.Builder(newsFile, c.ReleaseJsonFile, c.BlockList)
	// Set the BCP 47 language tag derived from the source filename so that
	// each translated feed carries the correct xml:lang attribute.
	// LocaleFromPath returns "en" for the canonical entries.html.
	news.Language = builder.LocaleFromPath(newsFile)
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

// outputFilenameForPlatform calls outputFilename and prepends the
// platform/status sub-path when platform is non-empty and not "linux".
// newsRoot should be the platform-specific data directory (dataDir) so that
// outputFilename produces a path relative to that directory; the
// platform/status prefix is then prepended to place the file in the correct
// sub-tree of BuildDir.
func outputFilenameForPlatform(newsFile, newsRoot, platform, status string) string {
	base := outputFilename(newsFile, newsRoot)
	if platform == "" || platform == "linux" {
		return base
	}
	return filepath.Join(platform, status, base)
}

// outputFilename derives the relative output path (.atom.xml) for a given
// source entries.html path.  newsRoot is the walk start directory (c.NewsFile);
// stripping the root prefix prevents output files from landing under a spurious
// source-directory sub-path inside BuildDir (e.g. "build/data/news.atom.xml"
// instead of "build/news.atom.xml").  For single-file invocations where
// newsFile == newsRoot, only the base name is used so the result is still valid.
//
// When newsFile is outside newsRoot (e.g. a custom --translationsdir that is
// not a subdirectory of --newsfile), filepath.Rel returns a path with leading
// ".." components.  In that case, and when the relative path cannot be
// computed at all, the function falls back to the base name only so the output
// always lands at the top level of BuildDir with the expected locale suffix.
func outputFilename(newsFile, newsRoot string) string {
	rel, err := filepath.Rel(newsRoot, newsFile)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
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
