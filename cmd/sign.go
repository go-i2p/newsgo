package cmd

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	signer "github.com/go-i2p/newsgo/signer"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// signCmd represents the sign command
var signCmd = &cobra.Command{
	Use:   "sign",
	Short: "Sign newsfeeds with local keys",
	Run: func(cmd *cobra.Command, args []string) {
		viper.Unmarshal(c)

		// Sign walks the build output directory for .atom.xml feeds produced
		// by the build command.  Walking the source directory for .html files
		// would call CreateSu3 on them; because CreateSu3 derives the output
		// path by replacing ".atom.xml" with ".su3", a .html input path is
		// unchanged and the source file is overwritten with binary su3 data.
		f, e := os.Stat(c.BuildDir)
		if e != nil {
			panic(e)
		}
		if f.IsDir() {
			err := filepath.Walk(c.BuildDir,
				func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					if strings.HasSuffix(path, ".atom.xml") {
						// Capture and log the error so that a key-load failure,
						// su3 marshal error, or write error is visible to the
						// operator.  The walk continues so that other feed files
						// are still attempted, but the non-zero result is surfaced.
						if err := Sign(path); err != nil {
							log.Printf("Sign(%s): %v", path, err)
						}
					}
					return nil
				})
			if err != nil {
				log.Println(err)
			}
		} else {
			// Capture and report the error in the single-file path so
			// that key-load failures, su3 marshal errors, and write errors
			// are visible to the operator — consistent with the directory
			// walk path above which logs Sign() errors.
			if err := Sign(c.BuildDir); err != nil {
				log.Printf("Sign(%s): %v", c.BuildDir, err)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(signCmd)

	// Here you will define your flags and configuration settings.

	signCmd.Flags().String("signerid", "null@example.i2p", "ID to use when signing the news")
	signCmd.Flags().String("signingkey", "signing_key.pem", "Path to a signing key")
	// builddir must match the flag registered by buildCmd so that the sign
	// command operates on the same output directory where feeds were written.
	signCmd.Flags().String("builddir", "build", "Build directory containing .atom.xml feeds to sign")

	viper.BindPFlags(signCmd.Flags())
}

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	privPem, err := os.ReadFile(path)
	if nil != err {
		return nil, err
	}

	// pem.Decode returns (nil, rest) when the input contains no PEM block
	// (e.g. empty file, DER-encoded key, wrong file).  Accessing privDer.Bytes
	// without this nil check causes a runtime panic.
	privDer, _ := pem.Decode(privPem)
	if privDer == nil {
		return nil, fmt.Errorf("loadPrivateKey: no PEM block found in %s", path)
	}
	privKey, err := x509.ParsePKCS1PrivateKey(privDer.Bytes)
	if nil != err {
		return nil, err
	}

	return privKey, nil
}

// Sign loads the configured private key and signs the Atom XML feed at
// xmlfeed, producing a co-located .su3 file. It returns any error encountered
// during key loading or su3 creation.
func Sign(xmlfeed string) error {
	sk, err := loadPrivateKey(c.SigningKey)
	if err != nil {
		return err
	}
	signer := signer.NewsSigner{
		SignerID:   c.SignerId,
		SigningKey: sk,
	}
	return signer.CreateSu3(xmlfeed)
}
