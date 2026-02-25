// Package newsfetch provides library functionality for fetching, verifying,
// and unpacking I2P news files (.su3) from a news server over I2P.
//
// A single onramp.Garlic session is shared across all Fetcher instances in a
// process via a package-level singleton so that repeated fetches (e.g. primary
// feed then backup feed) do not open additional SAM sessions.
package newsfetch

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-i2p/onramp"
	"i2pgit.org/go-i2p/reseed-tools/su3"
)

// garlicOnce guards the creation of the shared Garlic session.
var (
	garlicOnce   sync.Once
	sharedGarlic *onramp.Garlic
	garlicErr    error
)

// initSharedGarlic initialises the package-level Garlic session exactly once.
// samAddr may be empty, in which case the onramp default (127.0.0.1:7656) is
// used.  Should be called before the first Fetcher is constructed.
func initSharedGarlic(samAddr string) (*onramp.Garlic, error) {
	garlicOnce.Do(func() {
		if samAddr != "" {
			sharedGarlic, garlicErr = onramp.NewGarlic("newsgo", samAddr, onramp.OPT_DEFAULTS)
		} else {
			sharedGarlic = &onramp.Garlic{}
		}
	})
	return sharedGarlic, garlicErr
}

// CloseSharedGarlic closes the package-level Garlic session.  Call this once
// all Fetchers are no longer needed (e.g. in a defer after the fetch command
// completes).  It is safe to call even if the session was never opened.
func CloseSharedGarlic() {
	if sharedGarlic != nil {
		sharedGarlic.Close()
	}
}

// Fetcher fetches news files from an I2P news server using a shared Garlic
// session.
type Fetcher struct {
	client *http.Client
}

// transportFromGarlic builds an *http.Transport that routes connections
// through g.DialContext.  All Fetcher constructors use this helper so that
// timeout values are defined in exactly one place.
func transportFromGarlic(g *onramp.Garlic) *http.Transport {
	return &http.Transport{
		DialContext:           g.DialContext,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
	}
}

// NewFetcher returns a Fetcher that routes HTTP requests through the shared
// I2P Garlic session.  samAddr is an optional override for the SAMv3 gateway
// address; pass an empty string to use the onramp default.
func NewFetcher(samAddr string) (*Fetcher, error) {
	g, err := initSharedGarlic(samAddr)
	if err != nil {
		return nil, fmt.Errorf("newsfetch: init garlic: %w", err)
	}
	return NewFetcherFromGarlic(g), nil
}

// NewFetcherFromGarlic returns a Fetcher that routes HTTP requests through the
// provided *onramp.Garlic session.  The caller retains ownership of g and is
// responsible for calling g.Close() when it is no longer needed.
//
// Use this constructor when you already have a Garlic session (e.g. one shared
// with a news server or another subsystem) and want to avoid opening a second
// SAM session solely for news fetching.
func NewFetcherFromGarlic(g *onramp.Garlic) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Transport: transportFromGarlic(g),
			Timeout:   5 * time.Minute,
		},
	}
}

// Fetch performs an HTTP GET of url over I2P and returns the raw response body.
// The caller is responsible for closing any resources; the returned bytes are a
// complete copy of the response body.
func (f *Fetcher) Fetch(url string) ([]byte, error) {
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("newsfetch: GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("newsfetch: GET %s: unexpected status %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("newsfetch: read body %s: %w", url, err)
	}
	return data, nil
}

// su3Magic is the 6-byte file identity prefix all valid su3 files start with.
const su3Magic = "I2Psu3"

// VerifyAndUnpack parses the raw su3 bytes, optionally verifies the signature
// against one of the provided trusted X.509 certificates, and returns the
// inner content bytes (the Atom XML payload).
//
// certs may be nil or empty, in which case signature verification is skipped.
// When certs are supplied the signature must be valid under at least one of
// them; if none match a wrapped error is returned.
func VerifyAndUnpack(data []byte, certs []*x509.Certificate) ([]byte, error) {
	if len(data) < len(su3Magic) || string(data[:len(su3Magic)]) != su3Magic {
		return nil, fmt.Errorf("newsfetch: data is not a valid su3 file (missing magic header)")
	}
	f := su3.New()
	if err := f.UnmarshalBinary(data); err != nil {
		return nil, fmt.Errorf("newsfetch: unmarshal su3: %w", err)
	}

	if len(certs) > 0 {
		var lastErr error
		for _, c := range certs {
			if err := f.VerifySignature(c); err == nil {
				lastErr = nil
				break
			} else {
				lastErr = err
			}
		}
		if lastErr != nil {
			return nil, fmt.Errorf("newsfetch: signature verification failed: %w", lastErr)
		}
	}

	return f.Content, nil
}

// FetchAndParse fetches the su3 file at url, verifies it with certs (if any),
// and returns the inner Atom XML content.  This is the primary high-level
// entry point for the fetch command.
func (f *Fetcher) FetchAndParse(url string, certs []*x509.Certificate) ([]byte, error) {
	data, err := f.Fetch(url)
	if err != nil {
		return nil, err
	}
	return VerifyAndUnpack(data, certs)
}

// LoadCertificates reads PEM-encoded X.509 certificates from a set of file
// paths and returns the parsed certificate pool.  Files may contain multiple
// PEM blocks.  At least one valid certificate must be found in the combined
// set or an error is returned.
func LoadCertificates(paths []string) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("newsfetch: read cert file %s: %w", path, err)
		}
		for len(raw) > 0 {
			var block *pem.Block
			block, raw = pem.Decode(raw)
			if block == nil {
				break
			}
			if block.Type != "CERTIFICATE" {
				continue
			}
			c, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("newsfetch: parse cert in %s: %w", path, err)
			}
			certs = append(certs, c)
		}
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("newsfetch: no valid certificates found in %v", paths)
	}
	return certs, nil
}
