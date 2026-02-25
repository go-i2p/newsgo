package cmd

import (
	"crypto"
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
			log.Fatalf("sign: stat %s: %v", c.BuildDir, e)
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

// loadPrivateKey reads a PEM-encoded private key from path and returns it as
// a crypto.Signer. Supported formats and types:
//   - PKCS#1 RSA ("RSA PRIVATE KEY" — openssl genrsa)
//   - PKCS#8 RSA ("PRIVATE KEY" — openssl genpkey -algorithm RSA)
//   - PKCS#8 ECDSA on P-256, P-384, or P-521
//   - PKCS#8 Ed25519
//
// The returned value is one of *rsa.PrivateKey, *ecdsa.PrivateKey, or
// ed25519.PrivateKey, all of which implement crypto.Signer and are accepted
// by signer.NewsSigner and the updated su3.File.Sign.
func loadPrivateKey(path string) (crypto.Signer, error) {
	privPem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// pem.Decode returns (nil, rest) when the input contains no PEM block
	// (e.g. empty file, DER-encoded key, wrong file path).
	privDer, _ := pem.Decode(privPem)
	if privDer == nil {
		return nil, fmt.Errorf("loadPrivateKey: no PEM block found in %s", path)
	}

	// Fast path: classic PKCS#1 RSAPrivateKey encoding (openssl genrsa, reseed-tools keygen).
	if key, err := x509.ParsePKCS1PrivateKey(privDer.Bytes); err == nil {
		return key, nil
	}

	// PKCS#8: covers RSA, ECDSA (P-256/384/521), and Ed25519.
	parsed, err := x509.ParsePKCS8PrivateKey(privDer.Bytes)
	if err != nil {
		return nil, fmt.Errorf("loadPrivateKey: %s is not a valid PKCS#1 or PKCS#8 private key: %w", path, err)
	}
	key, ok := parsed.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("loadPrivateKey: %s contains %T which does not implement crypto.Signer", path, parsed)
	}
	return key, nil
}

// Sign loads the configured private key and signs the Atom XML feed at
// xmlfeed, producing a co-located .su3 file. It returns any error encountered
// during key loading or su3 creation. Supports RSA (PKCS#1 and PKCS#8),
// ECDSA (P-256, P-384, P-521), and Ed25519 signing keys.
func Sign(xmlfeed string) error {
	sk, err := loadPrivateKey(c.SigningKey)
	if err != nil {
		return err
	}
	newsSigner := signer.NewsSigner{
		SignerID:   c.SignerId,
		SigningKey: sk,
	}
	return newsSigner.CreateSu3(xmlfeed)
}
