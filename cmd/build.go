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

		// Critical fix: bypass the viper BindPFlags collision with signCmd.
		// Both buildCmd and signCmd register a "builddir" flag; because Go
		// processes source files in lexical order (build.go before sign.go),
		// sign.go's viper.BindPFlags call overwrites the "builddir" binding so
		// that viper.pflags["builddir"] always points to signCmd's (unchanged)
		// flag. Reading directly from this command's flag set guarantees we get
		// whatever value the user actually provided on the build sub-command.
		if bd, err := cmd.Flags().GetString("builddir"); err == nil {
			c.BuildDir = bd
		}

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
		for _, pr := range collectBuildPairs(c.Platform, c.Status) {
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

// buildPair holds the (platform, status) combination for a single build step.
// An empty platform means the default feed tree; an empty status means all
// known statuses are iterated by the caller.
type buildPair struct{ platform, status string }

// collectBuildPairs returns the ordered list of (platform, status) pairs for
// directory-mode builds.  It implements the documented --platform / --status
// filter semantics:
//
//   - Both flags set  → exactly one pair.
//   - --platform only → all known statuses for that platform.
//   - --status only   → that status for the default tree and every known platform.
//   - Neither flag    → default tree (empty/empty) then all platform×status combos.
//
// Extracting this logic from the Run closure makes it independently testable.
func collectBuildPairs(platform, status string) []buildPair {
	switch {
	case platform != "" && status != "":
		// Explicit platform+status: build exactly one combination.
		return []buildPair{{platform, status}}

	case platform != "":
		// Platform specified without status: try every known status.
		var pairs []buildPair
		for _, s := range builder.KnownStatuses() {
			pairs = append(pairs, buildPair{platform, s})
		}
		return pairs

	case status != "":
		// Status specified without platform: apply that status to the default
		// tree and every known platform.  This is the previously-missing case
		// that caused --status to be silently ignored when --platform was absent.
		pairs := []buildPair{{"", status}}
		for _, p := range builder.KnownPlatforms() {
			pairs = append(pairs, buildPair{p, status})
		}
		return pairs

	default:
		// Neither flag set: build the default tree first, then every
		// (platform, status) combination.
		pairs := []buildPair{{"", ""}}
		for _, p := range builder.KnownPlatforms() {
			for _, s := range builder.KnownStatuses() {
				pairs = append(pairs, buildPair{p, s})
			}
		}
		return pairs
	}
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

// resolveReleasesPath returns the resolved releases.json path for a platform
// build and whether the build should proceed. For the default tree the global
// --releasejson flag value is used. For named platforms the platform-specific
// releases.json is preferred and the global file is used as the fallback.
// When the resolved file does not exist, ok is false and a diagnostic message
// is logged; the caller should return without building any feeds.
func resolveReleasesPath(dataDir string, isDefault bool, globalPath, platform, status string) (path string, ok bool) {
	if isDefault {
		path = globalPath
	} else {
		path = resolveOverrideFile(filepath.Join(dataDir, "releases.json"), globalPath)
	}
	if _, err := os.Stat(path); err != nil {
		if isDefault {
			log.Printf("build: skipping default tree: releases.json not found at %s", path)
		} else {
			log.Printf("build: skipping %s/%s: no releases.json found (platform or global)", platform, status)
		}
		return path, false
	}
	return path, true
}

// resolveBlocklistPath returns the resolved blocklist.xml path for a platform
// build. For the default tree the global config value is returned unchanged.
// For named platforms the platform-specific file is preferred, with the global
// file used as the fallback so the jar-feed blocklist is always the baseline.
func resolveBlocklistPath(dataDir string, isDefault bool, globalPath string) string {
	if isDefault {
		return globalPath
	}
	return resolveOverrideFile(filepath.Join(dataDir, "blocklist.xml"), globalPath)
}

// resolveEntriesPath returns the entries.html to use as the primary source for
// a platform build. For the default tree the canonical entries.html is returned
// directly. For named platforms a platform-specific entries.html is used when
// it exists; otherwise the canonical file is returned as the fallback.
func resolveEntriesPath(dataDir, canonicalEntries string, isDefault bool) string {
	if isDefault {
		return canonicalEntries
	}
	platformEntries := filepath.Join(dataDir, "entries.html")
	if _, err := os.Stat(platformEntries); err == nil {
		return platformEntries
	}
	return canonicalEntries
}

// resolveTranslationsDir returns the translations directory for a platform
// build. For the default tree the --translationsdir flag value is honoured,
// with a fallback derived from the news-file directory. For named platforms the
// platform-specific translations directory is preferred and the top-level one
// is used when it is absent.
func resolveTranslationsDir(dataDir string, isDefault bool, newsFile, configTransDir string) string {
	if isDefault {
		if configTransDir != "" {
			return configTransDir
		}
		return filepath.Join(newsFile, "translations")
	}
	platformTransDir := filepath.Join(dataDir, "translations")
	if _, err := os.Stat(platformTransDir); err == nil {
		return platformTransDir
	}
	return filepath.Join(newsFile, "translations")
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
	isDefault := platform == ""

	// For non-default platforms the data directory must exist; a missing
	// directory means the combination has not been set up yet — skip silently.
	if !isDefault {
		if _, err := os.Stat(dataDir); err != nil {
			return
		}
	}

	releasesPath, ok := resolveReleasesPath(dataDir, isDefault, c.ReleaseJsonFile, platform, status)
	if !ok {
		return
	}

	blocklistPath := resolveBlocklistPath(dataDir, isDefault, c.BlockList)
	canonicalEntries := filepath.Join(c.NewsFile, "entries.html")
	entriesPath := resolveEntriesPath(dataDir, canonicalEntries, isDefault)
	transDir := resolveTranslationsDir(dataDir, isDefault, c.NewsFile, c.TranslationsDir)

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

	// BaseEntriesHTMLPath is the root entries.html that acts as the merge
	// baseline for locale/overlay files.  When build() is called in single-
	// file mode, newsFile IS c.NewsFile (a file path, e.g. "data/entries.html")
	// — not a directory.  filepath.Dir extracts the parent directory so that
	// base resolves to the same path as newsFile, making the condition false
	// and leaving BaseEntriesHTMLPath unset (correct: no merge is needed when
	// the caller already pointed at the canonical file).
	//
	// The previous code used filepath.Join(c.NewsFile, "entries.html") which,
	// when c.NewsFile was "data/entries.html", produced the always-invalid
	// path "data/entries.html/entries.html", causing LoadHTML to fail with
	// "not a directory" for every single-file invocation.
	base := filepath.Join(filepath.Dir(c.NewsFile), "entries.html")
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
// platform/status sub-path when a named platform is specified.  An empty
// platform (the default tree) returns the bare base name.  All named
// platforms — including "linux" — get a platform/status/ prefix so that
// their outputs are distinct from each other and from the default feed.
// newsRoot should be the platform-specific data directory (dataDir) so that
// outputFilename produces a path relative to that directory; the
// platform/status prefix is then prepended to place the file in the correct
// sub-tree of BuildDir.
func outputFilenameForPlatform(newsFile, newsRoot, platform, status string) string {
	base := outputFilename(newsFile, newsRoot)
	if platform == "" {
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
