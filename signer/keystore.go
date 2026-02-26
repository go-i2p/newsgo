package newssigner

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"os"

	keystore "github.com/pavlo-v-chernykh/keystore-go/v4"
	youmarkpkcs8 "github.com/youmark/pkcs8"
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
		return loadPKCS12(data, storePassword, entryPassword)
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

// ---- PKCS12 OIDs -----------------------------------------------------------

var (
	// RFC 7292 §A.2 — bag type OIDs
	oidPKCS8ShroudedKeyBag = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 10, 1, 2}

	// RFC 5652 / PKCS#7 content types
	oidDataContentType      = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidEncryptedContentType = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 6}
)

// ---- minimal ASN.1 structures for PKCS12 traversal ------------------------

// pfxPDU is the outermost SEQUENCE of a PKCS12/PFX file (RFC 7292 §4).
type pfxPDU struct {
	Version  int
	AuthSafe pkcs12ContentInfo
	MacData  asn1.RawValue `asn1:"optional"`
}

// pkcs12ContentInfo mirrors ContentInfo from PKCS#7 (RFC 5652 §5.2).
type pkcs12ContentInfo struct {
	ContentType asn1.ObjectIdentifier
	// [0] EXPLICIT ANY — we read it as a raw value and unwrap manually.
	Content asn1.RawValue `asn1:"tag:0,explicit,optional"`
}

// pkcs12SafeBag mirrors SafeBag from RFC 7292 §4.2.
type pkcs12SafeBag struct {
	Id    asn1.ObjectIdentifier
	Value asn1.RawValue // [0] EXPLICIT ANY — raw; we keep it unparsed.
	// bagAttributes is optional and we don't need it.
}

// ---- loadPKCS12 ------------------------------------------------------------

// loadPKCS12 parses a PKCS#12 / PFX file and returns the private key.
//
// Java PKCS12 keystores use TWO passwords: storePassword for the outer MAC
// (I2P hard-codes this as "changeit") and entryPassword for the individual
// key-bag encryption (the operator's KSPASS).  go-pkcs12's Decode has a
// single-password API so it cannot handle this split.
//
// Strategy:
//
//  1. Try go-pkcs12.Decode with each unique password — covers the common
//     case where both passwords are the same (non-I2P files) and lets
//     go-pkcs12 handle PKCS12 files where outer EncryptedData wraps the key.
//
//  2. Fall back to loadPKCS12DualPassword: manually walk the PKCS12 ASN.1
//     tree, find every PKCS8ShroudedKeyBag in plaintext SafeContents, and
//     decrypt each bag with entryPassword via youmark/pkcs8 (which speaks
//     PBES1 + PBES2).  The outer MAC is not verified — it was already
//     checked by go-pkcs12 in step 1 (if storePassword is correct), or can
//     be skipped if we only care about key extraction.
func loadPKCS12(data []byte, storePassword, entryPassword string) (crypto.Signer, error) {
	// Step 1: try go-pkcs12 with each unique candidate password.
	seen := make(map[string]bool)
	var unique []string
	for _, pw := range []string{entryPassword, storePassword, I2PDefaultKeystorePassword} {
		if !seen[pw] {
			seen[pw] = true
			unique = append(unique, pw)
		}
	}
	var lastSingleErr error
	for _, pw := range unique {
		key, _, err := pkcs12.Decode(data, pw)
		if err != nil {
			lastSingleErr = err
			continue
		}
		s, ok := key.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("keystore: PKCS12: key type %T does not implement crypto.Signer", key)
		}
		return s, nil
	}

	// Step 2: dual-password manual walk for Java-style PKCS12 where
	// storePassword (MAC) ≠ entryPassword (key encryption).
	key, err := loadPKCS12DualPassword(data, entryPassword)
	if err == nil {
		return key, nil
	}
	// Try storePassword as key-bag password too (edge case).
	if storePassword != entryPassword {
		if key2, err2 := loadPKCS12DualPassword(data, storePassword); err2 == nil {
			return key2, nil
		}
	}

	return nil, fmt.Errorf("keystore: PKCS12 decode failed (single-pw tries: %w; dual-pw try: %v)", lastSingleErr, err)
}

// loadPKCS12DualPassword manually walks the PKCS12 ASN.1 tree and decrypts
// every PKCS8ShroudedKeyBag using keyPassword.  The outer MAC is not verified.
//
// This handles the common Java pattern:
//
//	PFX
//	  authSafe (data) → OCTET STRING → AuthenticatedSafe
//	    ContentInfo (data) → OCTET STRING → SafeContents
//	      SafeBag (pkcs8ShroudedKeyBag)
//	        [0] EncryptedPrivateKeyInfo  ← encrypted with keyPassword
//	    ContentInfo (encryptedData)      ← certs, encrypted with storePassword
//	      … (ignored — we only need the key)
//	  macData (uses storePassword — ignored here)
func loadPKCS12DualPassword(data []byte, keyPassword string) (crypto.Signer, error) {
	// 1. Parse outer PFX.
	var pfx pfxPDU
	if rest, err := asn1.Unmarshal(data, &pfx); err != nil {
		return nil, fmt.Errorf("keystore: PKCS12 ASN.1 parse PFX: %w", err)
	} else if len(rest) != 0 {
		return nil, fmt.Errorf("keystore: PKCS12 trailing bytes after PFX (%d)", len(rest))
	}
	if !pfx.AuthSafe.ContentType.Equal(oidDataContentType) {
		return nil, fmt.Errorf("keystore: PKCS12 authSafe contentType unsupported: %v", pfx.AuthSafe.ContentType)
	}

	// 2. Unwrap authSafe OCTET STRING → AuthenticatedSafe bytes.
	// pfx.AuthSafe.Content is [0] EXPLICIT; its .Bytes hold the inner DER.
	authSafeData, err := asn1UnwrapOctetString(pfx.AuthSafe.Content.Bytes)
	if err != nil {
		return nil, fmt.Errorf("keystore: PKCS12 authSafe OCTET STRING: %w", err)
	}

	// 3. Parse AuthenticatedSafe = SEQUENCE OF ContentInfo.
	var contentInfos []pkcs12ContentInfo
	if _, err := asn1.UnmarshalWithParams(authSafeData, &contentInfos, ""); err != nil {
		// Try as a raw SEQUENCE in case Go's SEQUENCE-OF heuristic differs.
		var seq asn1.RawValue
		if _, err2 := asn1.Unmarshal(authSafeData, &seq); err2 != nil {
			return nil, fmt.Errorf("keystore: PKCS12 AuthenticatedSafe: %w", err)
		}
		rest := seq.Bytes
		for len(rest) > 0 {
			var ci pkcs12ContentInfo
			var leftover []byte
			if leftover, err = asn1.Unmarshal(rest, &ci); err != nil {
				return nil, fmt.Errorf("keystore: PKCS12 ContentInfo element: %w", err)
			}
			contentInfos = append(contentInfos, ci)
			rest = leftover
		}
	}

	pw := []byte(keyPassword)

	// 4. Walk each ContentInfo, only handle plaintext (data) ones.
	for _, ci := range contentInfos {
		if !ci.ContentType.Equal(oidDataContentType) {
			// EncryptedData containers (usually certs); skip — we only need the key.
			continue
		}
		// 5. Unwrap the OCTET STRING wrapping SafeContents.
		safeContentsData, err := asn1UnwrapOctetString(ci.Content.Bytes)
		if err != nil {
			continue
		}

		// 6. Parse SafeContents = SEQUENCE OF SafeBag (iteratively).
		var outerSeq asn1.RawValue
		if _, err := asn1.Unmarshal(safeContentsData, &outerSeq); err != nil {
			continue
		}
		rest := outerSeq.Bytes
		for len(rest) > 0 {
			var bag pkcs12SafeBag
			var leftover []byte
			if leftover, err = asn1.Unmarshal(rest, &bag); err != nil {
				break
			}
			rest = leftover

			if !bag.Id.Equal(oidPKCS8ShroudedKeyBag) {
				continue
			}

			// 7. bag.Value is the [0] EXPLICIT wrapper; .Bytes is the inner DER
			//    of EncryptedPrivateKeyInfo.  youmark/pkcs8 decrypts it.
			encPKCS8DER := bag.Value.Bytes
			iface, err := youmarkpkcs8.ParsePKCS8PrivateKey(encPKCS8DER, pw)
			if err != nil {
				// Wrong password or unsupported cipher; try next bag.
				continue
			}
			s, ok := iface.(crypto.Signer)
			if !ok {
				return nil, fmt.Errorf("keystore: PKCS12 shrouded key bag: key type %T does not implement crypto.Signer", iface)
			}
			return s, nil
		}
	}

	return nil, fmt.Errorf("keystore: PKCS12 dual-password: no PKCS8ShroudedKeyBag found or all decryption attempts failed")
}

// asn1UnwrapOctetString parses a DER-encoded OCTET STRING and returns its
// content bytes.
func asn1UnwrapOctetString(der []byte) ([]byte, error) {
	var octets []byte
	if _, err := asn1.Unmarshal(der, &octets); err != nil {
		return nil, fmt.Errorf("asn1 OCTET STRING: %w", err)
	}
	return octets, nil
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
