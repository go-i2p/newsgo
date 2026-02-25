// Package newssigner wraps reseed-tools su3 signing to package Atom XML feeds
// into signed su3 files for distribution via I2P news servers.
package newssigner

import (
	"crypto/rsa"
	"os"
	"strings"

	//"github.com/go-i2p/reseed-tools/su3"
	"i2pgit.org/go-i2p/reseed-tools/su3"
)

// NewsSigner signs Atom feed XML files and packages them into su3 files.
type NewsSigner struct {
	SignerID   string
	SigningKey *rsa.PrivateKey
}

// CreateSu3 reads the Atom XML file at xmldata, wraps it in an su3 container
// signed with ns.SigningKey, and writes the result to a file with the same
// base name but the ".atom.xml" suffix replaced by ".su3".
func (ns *NewsSigner) CreateSu3(xmldata string) error {
	su3File := su3.New()
	su3File.FileType = su3.FileTypeXML
	su3File.ContentType = su3.ContentTypeNews

	data, err := os.ReadFile(xmldata)
	if nil != err {
		return err
	}
	su3File.Content = data

	su3File.SignerID = []byte(ns.SignerID)
	su3File.Sign(ns.SigningKey)

	b, err := su3File.MarshalBinary()
	if err != nil {
		return err
	}
	outfile := strings.Replace(xmldata, ".atom.xml", ".su3", -1)
	return os.WriteFile(outfile, b, 0o644)
}
