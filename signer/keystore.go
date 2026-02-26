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

// I2PDefaultKeystorePassword is the constant keystore store password that all
// I2P components (KeyStoreUtil, SU3File, SAM, console …) use by default.  It
// corresponds to KeyStoreUtil.DEFAULT_KEYSTORE_PASSWORD in the Java source.
const I2PDefaultKeystorePassword = "changeit"

// LoadKeyFromKeystore reads a Java KeyStore (.ks/.jks) or PKCS#12 (.p12/.pfx)
// file entirely into memory, extracts a private key entry, and returns it as a
// crypto.Signer.  Nothing is written to disk at any point.
//
// Format is detected by magic bytes:
//
//	JKS    — 4-byte magic 0xFEEDFEED
//	PKCS12 — DER SEQUENCE tag (0x30)
//
// storePassword unlocks the keystore container itself.  For JKS files created
// by I2P this is always "changeit" (I2PDefaultKeystorePassword); pass an empty
// string to use that default.
//
// entryPassword unlocks the specific private key entry — this is the value the
// operator stores in KSPASS in su3.vars (prompted interactively by SU3File).
//
// alias selects which key entry to load.  I2P always uses the signer e-mail
// (SIGNER in su3.vars) as the alias.  Pass an empty string to load the first
// private key entry found.
func LoadKeyFromKeystore(path, storePassword, entryPassword, alias string) (crypto.Signer, error) {
	if storePassword == "" {
		storePassword = I2PDefaultKeystorePassword
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("keystore: read %s: %w", path, err)
	}

	// JKS magic: 0xFEED FEED
	if len(data) >= 4 &&
		data[0] == 0xFE && data[1] == 0xED &&
		data[2] == 0xFE && data[3] == 0xED {
		return loadJKS(data, storePassword, entryPassword, alias)
	}

	// PKCS12 / PFX: DER SEQUENCE tag
	if len(data) > 0 && data[0] == 0x30 {
		return loadPKCS12(data, storePassword)
	}

	return nil, fmt.Errorf("keystore: %s: unrecognised keystore format (expected JKS 0xFEEDFEED or PKCS12 DER 0x30)", path)
}

// loadJKS parses a JKS keystore.
// storePassword unlocks the container; entryPassword unlocks the key entry.
// If alias is non-empty only that entry is tried; otherwise the first private
// key entry found is returned.
func loadJKS(data []byte, storePassword, entryPassword, alias string) (crypto.Signer, error) {
	ks := keystore.New()
	if err := ks.Load(bytes.NewReader(data), []byte(storePassword)); err != nil {
		// Retry with entryPassword in case the caller accidentally swapped them.
		if err2 := ks.Load(bytes.NewReader(data), []byte(entryPassword)); err2 != nil {
			return nil, fmt.Errorf("keystore: JKS load (store pw %q): %w", storePassword, err)
		}
	}

	tryAlias := func(a string) (crypto.Signer, error) {
		entry, err := ks.GetPrivateKeyEntry(a, []byte(entryPassword))
		if err != nil {
			return nil, fmt.Errorf("keystore: JKS get key %q: %w", a, err)
		}
		key, err := parseKeyDER(entry.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("keystore: JKS alias %q: %w", a, err)
		}
		return key, nil
	}

	// If caller supplied an alias (= SIGNER email in I2P), use it directly.
	if alias != "" {
		return tryAlias(alias)
	}

	// Fall back: return the first private key entry found.
	for _, a := range ks.Aliases() {
		if ks.IsPrivateKeyEntry(a) {
			return tryAlias(a)
		}
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
