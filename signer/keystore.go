package newssigner

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"fmt"
	"os"

	keystore "github.com/pavlo-v-chernykh/keystore-go/v4"
	"software.sslmate.com/src/go-pkcs12"
)

// LoadKeyFromKeystore reads a Java KeyStore (.ks/.jks) or PKCS#12 (.p12/.pfx)
// file entirely into memory, extracts the first private key entry, and returns
// it as a crypto.Signer.  Nothing is written to disk at any point.
//
// Format is detected by magic bytes:
//
//	JKS   — 4-byte magic 0xFEED FEED
//	PKCS12 — DER SEQUENCE tag (0x30)
//
// password may be empty for unprotected keystores.  For JKS files produced by
// I2P (net.i2p.crypto.KeyStoreUtil / SU3File), pass the same password used
// when the keystore was created — typically the value of KSPASS in su3.vars.
func LoadKeyFromKeystore(path, password string) (crypto.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("keystore: read %s: %w", path, err)
	}

	// JKS magic: 0xFEED FEED
	if len(data) >= 4 &&
		data[0] == 0xFE && data[1] == 0xED &&
		data[2] == 0xFE && data[3] == 0xED {
		return loadJKS(data, password)
	}

	// PKCS12 / PFX: DER SEQUENCE tag
	if len(data) > 0 && data[0] == 0x30 {
		return loadPKCS12(data, password)
	}

	return nil, fmt.Errorf("keystore: %s: unrecognised keystore format (expected JKS 0xFEEDFEED or PKCS12 DER 0x30)", path)
}

// loadJKS parses a JKS keystore and returns the first private key entry.
func loadJKS(data []byte, password string) (crypto.Signer, error) {
	ks := keystore.New()
	if err := ks.Load(bytes.NewReader(data), []byte(password)); err != nil {
		return nil, fmt.Errorf("keystore: JKS load: %w", err)
	}

	for _, alias := range ks.Aliases() {
		if !ks.IsPrivateKeyEntry(alias) {
			continue
		}
		entry, err := ks.GetPrivateKeyEntry(alias, []byte(password))
		if err != nil {
			return nil, fmt.Errorf("keystore: JKS get key %q: %w", alias, err)
		}
		key, err := parseKeyDER(entry.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("keystore: JKS alias %q: %w", alias, err)
		}
		return key, nil
	}

	return nil, fmt.Errorf("keystore: JKS: no private key entry found")
}

// loadPKCS12 parses a PKCS#12 / PFX file and returns the private key.
func loadPKCS12(data []byte, password string) (crypto.Signer, error) {
	key, _, err := pkcs12.Decode(data, password)
	if err != nil {
		return nil, fmt.Errorf("keystore: PKCS12 decode: %w", err)
	}
	s, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("keystore: PKCS12: key type %T does not implement crypto.Signer", key)
	}
	return s, nil
}

// parseKeyDER attempts to parse a DER-encoded private key, trying PKCS#8,
// PKCS#1 (RSA), and SEC 1 (EC) in order.
func parseKeyDER(der []byte) (crypto.Signer, error) {
	if parsed, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if s, ok := parsed.(crypto.Signer); ok {
			return s, nil
		}
	}
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("keystore: cannot parse DER private key (tried PKCS#8, PKCS#1 RSA, SEC1 EC)")
}
