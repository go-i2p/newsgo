// Package newssigner wraps reseed-tools su3 signing to package Atom XML feeds
// into signed su3 files for distribution via I2P news servers.
package newssigner

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"fmt"
	"os"
	"strings"

	"i2pgit.org/go-i2p/reseed-tools/su3"
)

// NewsSigner signs Atom feed XML files and packages them into su3 files.
// SigningKey must be a crypto.Signer — typically *rsa.PrivateKey, *ecdsa.PrivateKey,
// or ed25519.PrivateKey. The su3 SignatureType is auto-detected from the key type
// so callers do not need to set it manually.
type NewsSigner struct {
	SignerID   string
	SigningKey crypto.Signer
}

// sigTypeForKey returns the su3 SignatureType constant that matches the
// concrete type of key. RSA defaults to SHA-512; ECDSA picks the hash that
// matches the curve's security level; Ed25519 uses the prehash (ph) variant
// required by the I2P SU3 specification.
func sigTypeForKey(key crypto.Signer) (uint16, error) {
	switch k := key.(type) {
	case *rsa.PrivateKey:
		return su3.SigTypeRSAWithSHA512, nil
	case *ecdsa.PrivateKey:
		switch k.Curve.Params().Name {
		case "P-256":
			return su3.SigTypeECDSAWithSHA256, nil
		case "P-384":
			return su3.SigTypeECDSAWithSHA384, nil
		case "P-521":
			return su3.SigTypeECDSAWithSHA512, nil
		default:
			return 0, fmt.Errorf("newssigner: unsupported ECDSA curve %s", k.Curve.Params().Name)
		}
	case ed25519.PrivateKey:
		return su3.SigTypeEdDSASHA512Ed25519ph, nil
	default:
		return 0, fmt.Errorf("newssigner: unsupported key type %T", key)
	}
}

// CreateSu3 reads the Atom XML file at xmldata, wraps it in an su3 container
// signed with ns.SigningKey, and writes the result to a file with the same
// base name but the ".atom.xml" suffix replaced by ".su3".
func (ns *NewsSigner) CreateSu3(xmldata string) error {
	su3File := su3.New()
	su3File.FileType = su3.FileTypeXML
	su3File.ContentType = su3.ContentTypeNews

	sigType, err := sigTypeForKey(ns.SigningKey)
	if err != nil {
		return err
	}
	su3File.SignatureType = sigType

	data, err := os.ReadFile(xmldata)
	if err != nil {
		return err
	}
	su3File.Content = data

	su3File.SignerID = []byte(ns.SignerID)
	if err := su3File.Sign(ns.SigningKey); err != nil {
		return fmt.Errorf("newssigner: sign %s: %w", xmldata, err)
	}

	b, err := su3File.MarshalBinary()
	if err != nil {
		return err
	}
	outfile := strings.Replace(xmldata, ".atom.xml", ".su3", -1)
	return os.WriteFile(outfile, b, 0o644)
}
