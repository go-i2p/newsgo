package newssigner

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"hash"
	"math/big"
	"os"
	"unicode/utf16"

	keystore "github.com/pavlo-v-chernykh/keystore-go/v4"
	youmarkpkcs8 "github.com/youmark/pkcs8"
	xpbkdf2 "golang.org/x/crypto/pbkdf2"
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

// ---- Additional OIDs for PKCS#7 EncryptedData / PBE decryption -------------

var (
	// PBES2 and PBKDF2 (RFC 8018)
	oidPBES2  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 13}
	oidPBKDF2 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 12}

	// Legacy PKCS#12 PBE algorithms (RFC 7292 Appendix C)
	oidPBEWithSHAAnd3KeyTripleDESCBC = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 1, 3}

	// HMAC PRF OIDs for PBKDF2
	oidHmacWithSHA1   = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 7}
	oidHmacWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 9}
	oidHmacWithSHA512 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 11}

	// AES-CBC OIDs (NIST)
	oidAES128CBC = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 2}
	oidAES192CBC = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 22}
	oidAES256CBC = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 42}
)

// ---- ASN.1 structures for PKCS#7 EncryptedData and PBE parameters ----------

// pkcs7EncryptedData is the outer EncryptedData SEQUENCE (RFC 5652 §8).
type pkcs7EncryptedData struct {
	Version              int
	EncryptedContentInfo pkcs7EncryptedContentInfo
}

// pkcs7EncryptedContentInfo holds the content type, algorithm, and ciphertext.
type pkcs7EncryptedContentInfo struct {
	ContentType                asn1.ObjectIdentifier
	ContentEncryptionAlgorithm pkix.AlgorithmIdentifier
	// [0] IMPLICIT EncryptedContent OPTIONAL — raw ciphertext bytes.
	EncryptedContent asn1.RawValue `asn1:"tag:0,optional"`
}

// pbes2ASNParams is the ASN.1 encoding of PBES2-params (RFC 8018 §6.2).
type pbes2ASNParams struct {
	KDFAlg       pkix.AlgorithmIdentifier
	EncSchemeAlg pkix.AlgorithmIdentifier
}

// pbkdf2ASNParams is the ASN.1 encoding of PBKDF2-params (RFC 8018 §5.2).
type pbkdf2ASNParams struct {
	Salt           asn1.RawValue
	IterationCount int
	KeyLength      int                      `asn1:"optional"`
	PRFAlg         pkix.AlgorithmIdentifier `asn1:"optional"`
}

// pkcs12PBEASNParams is the ASN.1 encoding of pbeParams for legacy PKCS#12 PBE.
type pkcs12PBEASNParams struct {
	Salt       []byte
	Iterations int
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
	// storePassword (MAC / outer container) ≠ entryPassword (key bag encryption).
	// We pass both so the walker can decrypt encryptedData containers with
	// storePassword and individual key bags with entryPassword.
	key, err := loadPKCS12DualPassword(data, storePassword, entryPassword)
	if err == nil {
		return key, nil
	}
	// Edge case: swap passwords in case the caller accidentally reversed them.
	if storePassword != entryPassword {
		if key2, err2 := loadPKCS12DualPassword(data, entryPassword, storePassword); err2 == nil {
			return key2, nil
		}
	}

	return nil, fmt.Errorf("keystore: PKCS12 decode failed (single-pw tries: %w; dual-pw try: %v)", lastSingleErr, err)
}

// loadPKCS12DualPassword manually walks the PKCS12 ASN.1 tree and decrypts
// every PKCS8ShroudedKeyBag using keyPassword.  The outer MAC is not verified.
//
// storePassword is used to decrypt encryptedData ContentInfo containers (Java
// 9+ format wraps both key and cert SafeContents in such containers).
// keyPassword is used to decrypt the individual PKCS8ShroudedKeyBag entries.
//
// Java 9+ default structure:
//
//	PFX
//	  authSafe (data) → OCTET STRING → AuthenticatedSafe
//	    ContentInfo (encryptedData)   ← decrypted with storePassword
//	      → SafeContents
//	           SafeBag (pkcs8ShroudedKeyBag) ← decrypted with keyPassword
//	    ContentInfo (encryptedData)   ← certs, encrypted with storePassword
//	  macData (uses storePassword — ignored here)
//
// Older (OpenSSL / go-pkcs12 / Java < 9) structure:
//
//	PFX
//	  authSafe (data) → OCTET STRING → AuthenticatedSafe
//	    ContentInfo (data) → OCTET STRING → SafeContents
//	      SafeBag (pkcs8ShroudedKeyBag) ← encrypted with keyPassword
//	    ContentInfo (encryptedData)      ← certs, encrypted with storePassword
func loadPKCS12DualPassword(data []byte, storePassword, keyPassword string) (crypto.Signer, error) {
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

	// 4. Walk each ContentInfo.  Handle both:
	//   • data (plaintext SafeContents) — traditional OpenSSL / go-pkcs12 / Java < 9
	//   • encryptedData — Java 9+ default (outer container encrypted with storePassword)
	for _, ci := range contentInfos {
		var safeContentsData []byte
		switch {
		case ci.ContentType.Equal(oidDataContentType):
			// 5a. Plaintext SafeContents — just unwrap the OCTET STRING.
			var unwrapErr error
			safeContentsData, unwrapErr = asn1UnwrapOctetString(ci.Content.Bytes)
			if unwrapErr != nil {
				continue
			}
		case ci.ContentType.Equal(oidEncryptedContentType):
			// 5b. EncryptedData container — decrypt with storePassword;
			//     also try keyPassword in case they happen to be the same.
			var encOuter pkcs7EncryptedData
			if _, parseErr := asn1.Unmarshal(ci.Content.Bytes, &encOuter); parseErr != nil {
				continue
			}
			var decErr error
			safeContentsData, decErr = decryptPKCS7EncryptedContent(encOuter.EncryptedContentInfo, storePassword)
			if decErr != nil && keyPassword != storePassword {
				// Try keyPassword as container password (non-standard files).
				safeContentsData, decErr = decryptPKCS7EncryptedContent(encOuter.EncryptedContentInfo, keyPassword)
			}
			if decErr != nil {
				continue
			}
		default:
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
			//    of EncryptedPrivateKeyInfo.  youmark/pkcs8 decrypts PBES2 bags.
			//    For legacy PKCS#12 PBE (e.g. PBEWithSHAAnd3KeyTripleDESCBC),
			//    we fall back to our own implementation.
			encPKCS8DER := bag.Value.Bytes
			iface, err := youmarkpkcs8.ParsePKCS8PrivateKey(encPKCS8DER, pw)
			if err != nil {
				// youmark/pkcs8 doesn't support legacy PKCS#12 PBE algorithms.
				// Try our own decryption path (handles PBES2 + PKCS#12 3DES PBE).
				iface, err = decryptPKCS8ShroudedKeyBag(encPKCS8DER, keyPassword)
			}
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

// ---- PKCS#7 EncryptedData decryption helpers --------------------------------

// decryptPKCS7EncryptedContent decrypts the ciphertext in a
// pkcs7EncryptedContentInfo using password (UTF-8 string).  Supports:
//   - PBES2 with PBKDF2 + AES-128/192/256-CBC (Java 9+ default)
//   - PBEWithSHAAnd3KeyTripleDESCBC (Java < 9 legacy, RFC 7292 Appendix B KDF)
func decryptPKCS7EncryptedContent(ci pkcs7EncryptedContentInfo, password string) ([]byte, error) {
	ciphertext := ci.EncryptedContent.Bytes // [0] IMPLICIT → raw ciphertext
	algo := ci.ContentEncryptionAlgorithm
	switch {
	case algo.Algorithm.Equal(oidPBES2):
		// Password is UTF-8 for PBES2 (RFC 8018 §6.2 / go-pkcs12 note).
		return pbes2DecryptContent(algo.Parameters.FullBytes, ciphertext, []byte(password))
	case algo.Algorithm.Equal(oidPBEWithSHAAnd3KeyTripleDESCBC):
		// Password must be BMP-encoded (UTF-16 BE, null-terminated) for PKCS#12 PBE.
		return pkcs12TripleDESDecryptContent(algo.Parameters.FullBytes, ciphertext, pkcs12BMPPassword(password))
	default:
		return nil, fmt.Errorf("keystore: PKCS7 EncryptedContent: unsupported algorithm %v", algo.Algorithm)
	}
}

// pbes2DecryptContent decrypts PBES2-encrypted content (RFC 8018 §6.2).
func pbes2DecryptContent(paramsFullBytes, ciphertext, password []byte) ([]byte, error) {
	var params pbes2ASNParams
	if _, err := asn1.Unmarshal(paramsFullBytes, &params); err != nil {
		return nil, fmt.Errorf("PBES2 params: %w", err)
	}
	if !params.KDFAlg.Algorithm.Equal(oidPBKDF2) {
		return nil, fmt.Errorf("PBES2: unsupported KDF %v", params.KDFAlg.Algorithm)
	}
	var kdf pbkdf2ASNParams
	if _, err := asn1.Unmarshal(params.KDFAlg.Parameters.FullBytes, &kdf); err != nil {
		return nil, fmt.Errorf("PBKDF2 params: %w", err)
	}
	if kdf.Salt.Tag != asn1.TagOctetString {
		return nil, fmt.Errorf("PBKDF2: unsupported salt type (tag %d)", kdf.Salt.Tag)
	}
	var hashFn func() hash.Hash
	switch {
	case kdf.PRFAlg.Algorithm.Equal(oidHmacWithSHA256),
		len(kdf.PRFAlg.Algorithm) == 0: // default per RFC 8018 = hmacWithSHA1, but Java defaults to SHA-256
		hashFn = sha256.New
	case kdf.PRFAlg.Algorithm.Equal(oidHmacWithSHA1):
		hashFn = sha1.New
	case kdf.PRFAlg.Algorithm.Equal(oidHmacWithSHA512):
		hashFn = sha512.New
	default:
		return nil, fmt.Errorf("PBKDF2: unsupported PRF %v", kdf.PRFAlg.Algorithm)
	}
	var keyLen int
	switch {
	case params.EncSchemeAlg.Algorithm.Equal(oidAES256CBC):
		keyLen = 32
	case params.EncSchemeAlg.Algorithm.Equal(oidAES192CBC):
		keyLen = 24
	case params.EncSchemeAlg.Algorithm.Equal(oidAES128CBC):
		keyLen = 16
	default:
		return nil, fmt.Errorf("PBES2: unsupported encryption scheme %v", params.EncSchemeAlg.Algorithm)
	}
	key := xpbkdf2.Key(password, kdf.Salt.Bytes, kdf.IterationCount, keyLen, hashFn)
	// AES-CBC parameters field is an OCTET STRING containing the 16-byte IV.
	iv := params.EncSchemeAlg.Parameters.Bytes
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("PBES2 AES cipher: %w", err)
	}
	if len(iv) != block.BlockSize() {
		return nil, fmt.Errorf("PBES2 AES: unexpected IV length %d (want %d)", len(iv), block.BlockSize())
	}
	if len(ciphertext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("PBES2: ciphertext length %d is not a multiple of block size %d", len(ciphertext), block.BlockSize())
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	return pbeUnpad(plaintext, block.BlockSize())
}

// pkcs12TripleDESDecryptContent decrypts content encrypted with
// PBEWithSHAAnd3KeyTripleDESCBC using bmpPassword (BMP-encoded, null-terminated).
func pkcs12TripleDESDecryptContent(paramsFullBytes, ciphertext, bmpPassword []byte) ([]byte, error) {
	var params pkcs12PBEASNParams
	if _, err := asn1.Unmarshal(paramsFullBytes, &params); err != nil {
		return nil, fmt.Errorf("PKCS12 PBE params: %w", err)
	}
	sha1Hash := func(in []byte) []byte { s := sha1.Sum(in); return s[:] }
	key := pkcs12RFC7292KDF(sha1Hash, 20, 64, params.Salt, bmpPassword, params.Iterations, 1, 24)
	iv := pkcs12RFC7292KDF(sha1Hash, 20, 64, params.Salt, bmpPassword, params.Iterations, 2, 8)
	block, err := des.NewTripleDESCipher(key)
	if err != nil {
		return nil, fmt.Errorf("PKCS12 3DES cipher: %w", err)
	}
	if len(ciphertext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("PKCS12 3DES: ciphertext length %d is not block-aligned", len(ciphertext))
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	return pbeUnpad(plaintext, block.BlockSize())
}

// pkcs12BMPPassword converts a UTF-8 password to the BMP (UTF-16 BE,
// null-terminated) form required by legacy PKCS#12 PBE algorithms.
func pkcs12BMPPassword(s string) []byte {
	encoded := utf16.Encode([]rune(s))
	out := make([]byte, len(encoded)*2+2) // +2 for UTF-16 null terminator
	for i, r := range encoded {
		out[i*2] = byte(r >> 8)
		out[i*2+1] = byte(r)
	}
	return out
}

// pkcs12RFC7292KDF implements the PKCS#12 key derivation function defined in
// RFC 7292 Appendix B.2.  hashFn is the hash function; u = hash output size in
// bytes; v = hash input block size in bytes.  Ported from go-pkcs12's pbkdf.go.
func pkcs12RFC7292KDF(hashFn func([]byte) []byte, u, v int, salt, password []byte, iterations int, id byte, size int) []byte {
	D := bytes.Repeat([]byte{id}, v)
	S := pkcs12FillRepeats(salt, v)
	P := pkcs12FillRepeats(password, v)
	I := append(S, P...)
	c := (size + u - 1) / u
	A := make([]byte, c*u)
	one := big.NewInt(1)
	var IjBuf []byte
	for i := 0; i < c; i++ {
		Ai := hashFn(append(D, I...))
		for j := 1; j < iterations; j++ {
			Ai = hashFn(Ai)
		}
		copy(A[i*u:], Ai)
		if i < c-1 {
			B := make([]byte, 0, v)
			for len(B) < v {
				B = append(B, Ai...)
			}
			B = B[:v]
			Bbi := new(big.Int).SetBytes(B)
			Ij := new(big.Int)
			for j := 0; j < len(I)/v; j++ {
				Ij.SetBytes(I[j*v : (j+1)*v])
				Ij.Add(Ij, Bbi)
				Ij.Add(Ij, one)
				Ijb := Ij.Bytes()
				if len(Ijb) > v {
					Ijb = Ijb[len(Ijb)-v:]
				}
				if len(Ijb) < v {
					if IjBuf == nil {
						IjBuf = make([]byte, v)
					}
					n := v - len(Ijb)
					for k := 0; k < n; k++ {
						IjBuf[k] = 0
					}
					copy(IjBuf[n:], Ijb)
					Ijb = IjBuf
				}
				copy(I[j*v:(j+1)*v], Ijb)
			}
		}
	}
	return A[:size]
}

// pkcs12FillRepeats returns a byte slice of length v*⌈len(data)/v⌉ consisting
// of repeats of data (the last copy may be truncated).
func pkcs12FillRepeats(data []byte, v int) []byte {
	if len(data) == 0 {
		return nil
	}
	outputLen := v * ((len(data) + v - 1) / v)
	out := bytes.Repeat(data, (outputLen+len(data)-1)/len(data))
	return out[:outputLen]
}

// pbeUnpad removes PKCS#7 padding from a decrypted CBC block.
func pbeUnpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("keystore: PBE unpad: empty data")
	}
	psLen := int(data[len(data)-1])
	if psLen == 0 || psLen > blockSize || psLen > len(data) {
		return nil, fmt.Errorf("keystore: PBE unpad: invalid padding length %d (blockSize=%d)", psLen, blockSize)
	}
	for _, b := range data[len(data)-psLen:] {
		if int(b) != psLen {
			return nil, fmt.Errorf("keystore: PBE unpad: inconsistent padding bytes")
		}
	}
	return data[:len(data)-psLen], nil
}

// decryptPKCS8ShroudedKeyBag decrypts an EncryptedPrivateKeyInfo DER blob using
// the PBE algorithms in decryptPKCS7EncryptedContent.  This serves as a
// fallback when youmark/pkcs8 doesn't support the algorithm (e.g. legacy
// PKCS#12 PBEWithSHAAnd3KeyTripleDESCBC).
func decryptPKCS8ShroudedKeyBag(encPKCS8DER []byte, password string) (interface{}, error) {
	// EncryptedPrivateKeyInfo ::= SEQUENCE {
	//   encryptionAlgorithm AlgorithmIdentifier,
	//   encryptedData       OCTET STRING
	// }
	var epki struct {
		Algorithm pkix.AlgorithmIdentifier
		Data      []byte
	}
	if _, err := asn1.Unmarshal(encPKCS8DER, &epki); err != nil {
		return nil, fmt.Errorf("parse EncryptedPrivateKeyInfo: %w", err)
	}
	// Reuse decryptPKCS7EncryptedContent by constructing a pseudo-ContentInfo
	// whose EncryptedContent.Bytes holds the raw ciphertext.
	ci := pkcs7EncryptedContentInfo{
		ContentEncryptionAlgorithm: epki.Algorithm,
		EncryptedContent: asn1.RawValue{
			Class: asn1.ClassContextSpecific,
			Tag:   0,
			Bytes: epki.Data,
		},
	}
	plaintext, err := decryptPKCS7EncryptedContent(ci, password)
	if err != nil {
		return nil, fmt.Errorf("decrypt EncryptedPrivateKeyInfo: %w", err)
	}
	// The plaintext is a PrivateKeyInfo (unencrypted PKCS#8 DER).
	key, err := x509.ParsePKCS8PrivateKey(plaintext)
	if err != nil {
		// Also try PKCS#1 RSA and SEC 1 EC formats in case the inner format differs.
		return parseKeyDER(plaintext)
	}
	return key, nil
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
