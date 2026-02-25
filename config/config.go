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
	FeedUuid   string `mapstructure:"feeduri"`
	BuildDir   string `mapstructure:"builddir"`
	SignerId   string `mapstructure:"signerid"`
	SigningKey string `mapstructure:"signingkey"`

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
}
