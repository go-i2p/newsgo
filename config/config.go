package config

// Conf holds the configuration values populated by viper from cobra flags,
// environment variables, or a config file.
//
// mapstructure tags are required wherever the lowercased Go field name does
// not match the cobra flag name that viper binds.  Without them,
// viper.Unmarshal silently leaves those fields at their zero value.
type Conf struct {
	NewsDir   string
	StatsFile string
	Http      string
	SamAddr   string
	NewsFile  string
	// BlockList is populated from the --blockfile flag (matches README).
	BlockList string `mapstructure:"blockfile"`
	// ReleaseJsonFile is populated from the --releasejson flag.
	// Without this tag viper would look for the key "releasejsonfile", which
	// has no corresponding flag and is always empty.
	ReleaseJsonFile string `mapstructure:"releasejson"`
	FeedTitle       string
	FeedSubtitle    string
	FeedSite        string
	FeedMain        string
	FeedBackup      string
	// FeedUuid is populated from the --feeduri flag (matches README).
	// Without this tag viper would look for the key "feeduuid".
	FeedUuid   string `mapstructure:"feeduri"`
	BuildDir   string
	SignerId   string
	SigningKey string
}
