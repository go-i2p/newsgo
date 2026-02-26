// Package config defines the Conf struct used by the cmd package to bind cobra
// flags and viper configuration values into a single typed structure.
package config

// Conf holds the configuration values populated by viper from cobra flags,
// environment variables, or a config file.
//
// mapstructure tags are required wherever the lowercased Go field name does
// not match the cobra flag name that viper binds.  Without them,
// viper.Unmarshal silently leaves those fields at their zero value.
type Conf struct {
	NewsDir string
	// StatsFile is stored at the path given by --statsfile.
	StatsFile string `mapstructure:"statsfile"`
	// Host and Port are the TCP address components for the HTTP listener,
	// matching the --host / --port flags documented in the README.
	// (The previous Http field combined them into a host:port string and is
	// no longer used.)
	Host string
	Port string
	// I2P enables SAMv3 co-hosting when true.  Corresponds to --i2p bool,
	// which matches the README flag name.
	I2P bool `mapstructure:"i2p"`
	// SamAddr is an advanced override for the SAMv3 gateway address when
	// --i2p is enabled.  Empty string means use the onramp default.
	SamAddr  string `mapstructure:"samaddr"`
	NewsFile string `mapstructure:"newsfile"`
	// BlockList is populated from the --blockfile flag (matches README).
	BlockList string `mapstructure:"blockfile"`
	// ReleaseJsonFile is populated from the --releasejson flag.
	// Without this tag viper would look for the key "releasejsonfile", which
	// has no corresponding flag and is always empty.
	ReleaseJsonFile string `mapstructure:"releasejson"`
	FeedTitle       string `mapstructure:"feedtitle"`
	FeedSubtitle    string `mapstructure:"feedsubtitle"`
	FeedSite        string `mapstructure:"feedsite"`
	FeedMain        string `mapstructure:"feedmain"`
	FeedBackup      string `mapstructure:"feedbackup"`
	// FeedUuid is populated from the --feeduri flag (matches README).
	// Without this tag viper would look for the key "feeduuid".
	FeedUuid string `mapstructure:"feeduri"`
	BuildDir string `mapstructure:"builddir"`
	// TranslationsDir is the directory searched for "entries.{locale}.html"
	// translation files.  When empty the build command defaults to the
	// "translations" subdirectory of the newsfile directory.
	TranslationsDir string `mapstructure:"translationsdir"`
	SignerId        string `mapstructure:"signerid"`
	SigningKey      string `mapstructure:"signingkey"`
	// KeystorePass is the JKS/PKCS12 *store* password — the password that
	// unlocks the keystore container itself.  For keystores created by I2P
	// (KeyStoreUtil / SU3File) this is always "changeit".  Leave empty to use
	// that default.  Corresponds to the -p flag of SU3File.
	KeystorePass string `mapstructure:"keystorepass"`
	// KeyEntryPass is the *key entry* password — the password that unlocks the
	// specific private key inside the JKS.  This is what I2P prompts for
	// interactively (keypw in SU3File / bulkSignCLI) and what should be stored
	// as KSPASS in su3.vars.  Corresponds to --keyentrypass on the CLI.
	KeyEntryPass string `mapstructure:"keyentrypass"`

	// Fetch subcommand options.

	// NewsURL is the primary news feed URL to fetch from (--newsurl).
	NewsURL string `mapstructure:"newsurl"`
	// NewsURLs holds additional / backup feed URLs (--newsurls, comma-separated).
	NewsURLs []string `mapstructure:"newsurls"`
	// OutDir is the directory where fetched and unpacked files are stored.
	OutDir string `mapstructure:"outdir"`
	// TrustedCerts lists PEM certificate files used to verify su3 signatures.
	// An empty slice skips signature verification.
	TrustedCerts []string `mapstructure:"trustedcerts"`
	// SkipVerify disables su3 signature verification when true.
	SkipVerify bool `mapstructure:"skipverify"`

	// Platform filters the build to a single OS target when non-empty.
	// Recognised values: "linux", "mac", "mac-arm64", "win",
	//                    "android", "ios".
	// Empty string means build the top-level (default/Linux) feeds only.
	Platform string `mapstructure:"platform"`

	// Status filters the build to a single release channel when non-empty.
	// Recognised values: "stable", "beta", "alpha", "rc".
	// Empty string means build all statuses found under the platform directory.
	Status string `mapstructure:"status"`
}
