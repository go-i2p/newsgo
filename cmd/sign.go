package cmd

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

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

		f, e := os.Stat(c.NewsFile)
		if e != nil {
			panic(e)
		}
		if f.IsDir() {
			err := filepath.Walk(c.NewsFile,
				func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					ext := filepath.Ext(path)
					if ext == ".html" {
						Sign(path)
					}
					return nil
				})
			if err != nil {
				log.Println(err)
			}
		} else {
			Sign(c.NewsFile)
		}

	},
}

func init() {
	rootCmd.AddCommand(signCmd)

	// Here you will define your flags and configuration settings.

	signCmd.Flags().String("signerid", "null@example.i2p", "ID to use when signing the news")
	signCmd.Flags().String("signingkey", "signing_key.pem", "Path to a signing key")

	viper.BindPFlags(signCmd.Flags())
}

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	privPem, err := ioutil.ReadFile(path)
	if nil != err {
		return nil, err
	}

	privDer, _ := pem.Decode(privPem)
	privKey, err := x509.ParsePKCS1PrivateKey(privDer.Bytes)
	if nil != err {
		return nil, err
	}

	return privKey, nil
}

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
