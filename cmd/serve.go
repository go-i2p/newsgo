package cmd

import (
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	server "github.com/go-i2p/newsgo/server"
	"github.com/go-i2p/onramp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve newsfeeds from a directory",
	Run: func(cmd *cobra.Command, args []string) {
		viper.Unmarshal(c)
		s := server.Serve(c.NewsDir, c.StatsFile)

		// Probe for a SAM gateway lazily — only when actually serving and
		// only when the user has not already passed --i2p=true.  Probing at
		// package-init time (before flag parsing) would add a blocking
		// net.Listen syscall to every invocation including build/sign/help.
		if !c.I2P {
			c.I2P = isSamAround()
		}

		if c.Host != "" {
			go func() {
				if err := serveHTTP(s, c.Host, c.Port); err != nil {
					panic(err)
				}
			}()
		}
		if c.I2P {
			go func() {
				if err := serveI2P(s, c.SamAddr); err != nil {
					panic(err)
				}
			}()
		}
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		go func() {
			for sig := range sigCh {
				log.Println("captured:", sig)
				// Log any stats persistence failure so operators know the
				// download counters were lost (e.g. read-only stats file).
				if err := s.Stats.Save(); err != nil {
					log.Printf("Stats.Save: %v", err)
				}
				os.Exit(0)
			}
		}()
		i := 0
		for {
			time.Sleep(time.Minute)
			log.Printf("Running for %d minutes.", i)
			i++
		}
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().String("newsdir", "build", "directory to serve news from")
	serveCmd.Flags().String("statsfile", "build/stats.json", "file to store stats in")
	// --host and --port match the README and main.go flag names.
	// The previous --http flag (combined host:port string) is removed.
	serveCmd.Flags().String("host", "127.0.0.1", "host to serve news files on")
	serveCmd.Flags().String("port", "9696", "port to serve news files on")
	// --i2p matches the README boolean flag name.
	// --samaddr is an advanced override for the SAM gateway address; it does
	// not replace --i2p as the primary I2P toggle.
	serveCmd.Flags().Bool("i2p", false, "serve news files directly to I2P using SAMv3")
	serveCmd.Flags().String("samaddr", onramp.SAM_ADDR, "advanced: SAMv3 gateway address when --i2p is enabled")

	viper.BindPFlags(serveCmd.Flags())
}

// isSamAround probes 127.0.0.1:7656 to check whether a SAMv3 gateway is
// running.  Returns true when the port is already bound (SAM is present).
// Must only be called after flag.Parse / inside a command handler — never at
// package-init time — to avoid blocking syscalls for unrelated sub-commands.
func isSamAround() bool {
	ln, err := net.Listen("tcp", "127.0.0.1:7656")
	if err != nil {
		return true
	}
	ln.Close()
	return false
}

// serveHTTP starts an HTTP listener on host:port and serves s.
func serveHTTP(s *server.NewsServer, host, port string) error {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	return http.Serve(ln, s)
}

// serveI2P starts a SAMv3 garlic listener and serves s over I2P.
// samAddr is an optional override for the SAMv3 gateway address; an empty
// string uses the onramp-library default (127.0.0.1:7656).
func serveI2P(s *server.NewsServer, samAddr string) error {
	var (
		garlic *onramp.Garlic
		err    error
	)
	if samAddr != "" {
		garlic, err = onramp.NewGarlic("newsgo", samAddr, onramp.OPT_DEFAULTS)
		if err != nil {
			return err
		}
	} else {
		garlic = &onramp.Garlic{}
	}
	defer garlic.Close()
	ln, err := garlic.Listen()
	if err != nil {
		return err
	}
	defer ln.Close()
	return http.Serve(ln, s)
}
