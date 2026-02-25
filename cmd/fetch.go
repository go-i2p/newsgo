package cmd

import (
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	newsfetch "github.com/go-i2p/newsgo/fetch"
	"github.com/go-i2p/onramp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// fetchCmd represents the fetch command
var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch, verify, and unpack a news feed from an I2P news server",
	Long: `fetch downloads one or more .su3 news files from I2P news servers over SAMv3,
verifies signatures with trusted certificates (if provided), unpacks the inner
Atom XML, and writes it to the output directory.

All fetches in a single invocation share one onramp.Garlic SAM session.

Examples:
  # Fetch without signature verification:
  newsgo fetch --newsurl http://tc73n4kivdroccekirco7rhgxdg5f3cjvbaapabupeyzrqwv5guq.b32.i2p/news/news.su3

  # Fetch and verify with a trusted certificate:
  newsgo fetch --newsurl <url> --trustedcerts /path/to/news.crt

  # Try a primary URL then a backup:
  newsgo fetch --newsurl <primary> --newsurls <backup1>,<backup2>`,
	Run: func(cmd *cobra.Command, args []string) {
		viper.Unmarshal(c)

		urls := collectURLs(c.NewsURL, c.NewsURLs)
		if len(urls) == 0 {
			log.Fatal("fetch: no URL supplied; use --newsurl or --newsurls")
		}

		var certs []*x509.Certificate
		if !c.SkipVerify && len(c.TrustedCerts) > 0 {
			loaded, err := newsfetch.LoadCertificates(c.TrustedCerts)
			if err != nil {
				log.Fatalf("fetch: load certificates: %v", err)
			}
			certs = loaded
		}

		fetcher, err := newsfetch.NewFetcher(c.SamAddr)
		if err != nil {
			log.Fatalf("fetch: create fetcher: %v", err)
		}
		defer newsfetch.CloseSharedGarlic()

		if err := os.MkdirAll(c.OutDir, 0o755); err != nil {
			log.Fatalf("fetch: create outdir %s: %v", c.OutDir, err)
		}

		if err := fetchURLs(fetcher, urls, certs, c.OutDir); err != nil {
			log.Fatalf("fetch: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(fetchCmd)

	fetchCmd.Flags().String("newsurl", "", "primary news feed URL to fetch (.su3 over I2P)")
	fetchCmd.Flags().StringSlice("newsurls", nil, "additional/backup news feed URLs (tried in order after --newsurl)")
	fetchCmd.Flags().String("outdir", "build", "directory to write unpacked Atom XML files to")
	fetchCmd.Flags().StringSlice("trustedcerts", nil, "PEM certificate files whose public keys are trusted to verify su3 signatures")
	fetchCmd.Flags().Bool("skipverify", false, "skip su3 signature verification (not recommended for production)")
	// --samaddr is also registered here (not only on serveCmd) because the
	// README documents it as a fetch option.  Using the same default as
	// serve.go (onramp.SAM_ADDR) so both commands behave consistently.
	fetchCmd.Flags().String("samaddr", onramp.SAM_ADDR, "advanced: SAMv3 gateway address for I2P fetches")

	viper.BindPFlags(fetchCmd.Flags())
}

// collectURLs merges the single primary URL with the slice of backup URLs,
// deduplicating while preserving order.
func collectURLs(primary string, backups []string) []string {
	seen := make(map[string]bool)
	var result []string
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u != "" && !seen[u] {
			seen[u] = true
			result = append(result, u)
		}
	}
	add(primary)
	for _, u := range backups {
		// StringSlice values from cobra already split on commas, but a user
		// may pass a raw comma-separated string via an env var / config file.
		for _, part := range strings.Split(u, ",") {
			add(part)
		}
	}
	return result
}

// outFilename derives the output filename for a fetched su3 URL.
// "news.su3" → "news.atom.xml"; other names → "fetched.atom.xml".
func outFilename(url string) string {
	base := filepath.Base(url)
	if strings.HasSuffix(base, ".su3") {
		base = strings.TrimSuffix(base, ".su3") + ".atom.xml"
	} else {
		base = "fetched.atom.xml"
	}
	return base
}

// fetchURLs attempts to fetch each URL in order.  On the first successful
// fetch-verify-unpack it writes the output and returns nil.  If all URLs fail,
// all errors are aggregated and returned.
func fetchURLs(f *newsfetch.Fetcher, urls []string, certs []*x509.Certificate, outDir string) error {
	var errs []string
	for _, url := range urls {
		content, err := f.FetchAndParse(url, certs)
		if err != nil {
			log.Printf("fetch: %s: %v (trying next URL)", url, err)
			errs = append(errs, fmt.Sprintf("%s: %v", url, err))
			continue
		}
		outPath := filepath.Join(outDir, outFilename(url))
		if err := os.WriteFile(outPath, content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", outPath, err)
		}
		log.Printf("fetch: saved %d bytes to %s", len(content), outPath)
		return nil
	}
	return fmt.Errorf("all URLs failed: %s", strings.Join(errs, "; "))
}
