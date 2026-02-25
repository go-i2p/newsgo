package cmd

import (
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

		// Fail fast rather than spinning forever with no listeners.
		// The default for --host is "127.0.0.1" (never empty), so this
		// condition only fires on deliberate misconfiguration.
		if noListenerConfigured(c.Host, c.I2P) {
			log.Fatalf("serve: no listener configured: --host is empty and --i2p is false; at least one must be enabled")
		}

		if c.Host != "" {
			go func() {
				// log.Fatalf produces a human-readable message and exits
				// cleanly (exit code 1) instead of printing a raw panic
				// traceback.  The most common cause is the TCP port already
				// being bound, which is a routine operational error.
				if err := serveHTTP(s, c.Host, c.Port); err != nil {
					log.Fatalf("serveHTTP: %v", err)
				}
			}()
		}
		if c.I2P {
			go func() {
				// Use Printf (not Fatalf): I2P auto-detection is best-effort.
				// A false-positive port match or a transient SAM startup failure
				// should degrade gracefully rather than pulling down the clearnet
				// listener alongside it.  The operator can pass --i2p=false
				// explicitly if auto-detection fires on a non-SAM process.
				if err := serveI2P(s, c.SamAddr); err != nil {
					log.Printf("serveI2P: %v (I2P listener disabled)", err)
				}
			}()
		}
		sigCh := make(chan os.Signal, 1)
		// Register both SIGINT (Ctrl-C) and SIGTERM (systemctl stop, docker stop,
		// Kubernetes pod termination) so stats are persisted on any graceful stop.
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
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

// isSamAround probes the default SAMv3 address to check whether a gateway is
// running.  Returns true when something accepts a TCP connection on the port.
//
// Implementation note: we use Dial rather than Listen.  Listen would fail
// whenever *any* process has the port open — true SAM present, or an unrelated
// service.  Dial only succeeds when a listener is actually accepting connections,
// which is a closer proxy for "SAM is ready".  The probe remains best-effort:
// a non-SAM process on port 7656 still returns true, but the I2P goroutine
// handles that failure gracefully (see caller in serveCmd.Run).
//
// Must only be called inside a command handler — never at package-init time —
// to avoid blocking syscalls for unrelated sub-commands (build, sign, help).
func isSamAround() bool {
	return checkPortListening("127.0.0.1:7656")
}

// checkPortListening dials addr with a short timeout and returns true when a
// listener accepts the connection.  Extracted from isSamAround for testability
// so unit tests can bind their own listener on a random port instead of
// requiring port 7656 to be free (or occupied by SAM).
func checkPortListening(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		// Connection refused, timeout, or network error → nothing listening.
		return false
	}
	conn.Close()
	return true
}

// noListenerConfigured reports whether the serve command would start with zero
// active listeners. It is extracted as a named function so the condition can
// be unit-tested without invoking log.Fatalf. Returns true only when host is
// the empty string (--host "") AND i2p is false — both clearnet and I2P
// listeners are disabled simultaneously.
func noListenerConfigured(host string, i2p bool) bool {
	return host == "" && !i2p
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
