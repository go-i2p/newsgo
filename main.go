// Package main is the entry point for the newsgo binary.
// All command-line parsing, sub-command dispatch, config-file loading, and
// environment-variable overrides are handled by the cmd/ package via Cobra
// and Viper.  main() simply delegates to cmd.Execute().
package main

import "github.com/go-i2p/newsgo/cmd"

func main() { cmd.Execute() }
