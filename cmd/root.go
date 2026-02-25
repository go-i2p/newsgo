package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-i2p/newsgo/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	c       *config.Conf = &config.Conf{}
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "newsgo",
	Short: "I2P News Server Tool/Library. A whole lot faster than the python one. Otherwise compatible",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// ExecuteWithArgs runs the command tree with the provided argument list instead
// of os.Args.  It is intended for use in tests where invoking specific
// sub-commands without modifying os.Args is required.
func ExecuteWithArgs(args []string) error {
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

// LookupFlag looks up a flag on the named sub-command.  commandName must be
// one of "serve", "build", "sign", or "fetch"; use "" to look up a persistent
// root flag.  Returns nil when the command or flag is not found.
func LookupFlag(commandName, flagName string) *pflag.Flag {
	if commandName == "" {
		return rootCmd.PersistentFlags().Lookup(flagName)
	}
	sub, _, err := rootCmd.Find([]string{commandName})
	if err != nil || sub == nil {
		return nil
	}
	return sub.Flags().Lookup(flagName)
}

func init() {
	cobra.OnInitialize(initConfig)

	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.newsgo.yaml)")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".newsgo" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".newsgo")
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	// SetEnvPrefix ensures that only NEWSGO_* variables are mapped, matching
	// the documented interface ("NEWSGO_PORT", "NEWSGO_NEWSDIR", etc.).
	// Without this call viper reads bare names like PORT, which collides with
	// variables set by container runtimes and shell environments.
	viper.SetEnvPrefix("newsgo")
	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
