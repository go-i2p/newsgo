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
		if c.Http != "" {
			go func() {
				if err := serve(s); err != nil {
					panic(err)
				}
			}()
		}
		if c.SamAddr != "" {
			go func() {
				if err := serveI2P(s); err != nil {
					panic(err)
				}
			}()
		}
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() {
			for sig := range c {
				log.Println("captured: ", sig)
				s.Stats.Save()
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
	serveCmd.Flags().String("http", "127.0.0.1:9696", "listen for HTTP requests. Empty string to disable")
	serveCmd.Flags().String("samaddr", onramp.SAM_ADDR, "SAMv3 gateway address. Empty string to disable")

	viper.BindPFlags(serveCmd.Flags())

}

func serveI2P(s *server.NewsServer) error {
	garlic, err := onramp.NewGarlic("newsgo", c.SamAddr, onramp.OPT_DEFAULTS)
	if err != nil {
		return err
	}
	defer garlic.Close()
	ln, err := garlic.Listen()
	if err != nil {
		return err
	}
	defer ln.Close()
	return http.Serve(ln, s)
}

func serve(s *server.NewsServer) error {
	ln, err := net.Listen("tcp", c.Http)
	if err != nil {
		return err
	}
	return http.Serve(ln, s)
}
