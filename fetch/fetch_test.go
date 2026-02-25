package newsfetch

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-i2p/onramp"
	"i2pgit.org/go-i2p/reseed-tools/su3"
)

// makeSu3Bytes creates a minimal signed su3 payload using a freshly generated
// RSA key. It returns the raw su3 bytes and the signer certificate so callers
// can test both valid and invalid verification paths.
func makeSu3Bytes(t *testing.T, content []byte) ([]byte, *x509.Certificate, *rsa.PrivateKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-signer@example.i2p"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	f := su3.New()
	f.FileType = su3.FileTypeXML
	f.ContentType = su3.ContentTypeNews
	f.Content = content
	f.SignerID = []byte("test-signer@example.i2p")
	if err := f.Sign(key); err != nil {
		t.Fatalf("sign su3: %v", err)
	}
	data, err := f.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal su3: %v", err)
	}
	return data, cert, key
}

// TestVerifyAndUnpack_NoVerification checks that VerifyAndUnpack can parse a
// valid su3 and return its content when no certificates are provided.
func TestVerifyAndUnpack_NoVerification(t *testing.T) {
	want := []byte("<feed>test</feed>")
	data, _, _ := makeSu3Bytes(t, want)

	got, err := VerifyAndUnpack(data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content mismatch: got %q, want %q", got, want)
	}
}

// TestVerifyAndUnpack_ValidCert checks that VerifyAndUnpack accepts a correctly
// signed su3 when the matching certificate is in the trusted set.
func TestVerifyAndUnpack_ValidCert(t *testing.T) {
	want := []byte("<feed>verified</feed>")
	data, cert, _ := makeSu3Bytes(t, want)

	got, err := VerifyAndUnpack(data, []*x509.Certificate{cert})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content mismatch: got %q, want %q", got, want)
	}
}

// TestVerifyAndUnpack_WrongCert checks that VerifyAndUnpack rejects a su3 when
// none of the provided certificates match the signer.
func TestVerifyAndUnpack_WrongCert(t *testing.T) {
	data, _, _ := makeSu3Bytes(t, []byte("<feed>bad</feed>"))

	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "other@example.i2p"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &otherKey.PublicKey, otherKey)
	otherCert, _ := x509.ParseCertificate(certDER)

	_, err := VerifyAndUnpack(data, []*x509.Certificate{otherCert})
	if err == nil {
		t.Fatal("expected verification error, got nil")
	}
}

// TestVerifyAndUnpack_Garbage ensures bad input returns a descriptive error.
func TestVerifyAndUnpack_Garbage(t *testing.T) {
	_, err := VerifyAndUnpack([]byte("not an su3 file"), nil)
	if err == nil {
		t.Fatal("expected error parsing garbage input, got nil")
	}
}

// TestLoadCertificates_RoundTrip writes a PEM certificate to a temp file and
// checks that LoadCertificates can read it back.
func TestLoadCertificates_RoundTrip(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "load-test@example.i2p"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)

	dir := t.TempDir()
	path := filepath.Join(dir, "test.pem")
	fh, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	pem.Encode(fh, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	fh.Close()

	certs, err := LoadCertificates([]string{path})
	if err != nil {
		t.Fatalf("LoadCertificates: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1 cert, got %d", len(certs))
	}
}

// TestLoadCertificates_NoPaths checks that LoadCertificates returns an error
// when the path list is empty rather than panicking.
func TestLoadCertificates_NoPaths(t *testing.T) {
	_, err := LoadCertificates([]string{})
	if err == nil {
		t.Fatal("expected error for empty path list, got nil")
	}
}

// TestLoadCertificates_MissingFile checks that a missing cert file is caught.
func TestLoadCertificates_MissingFile(t *testing.T) {
	_, err := LoadCertificates([]string{"/nonexistent/cert.pem"})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestFetcher_FetchHTTP wires a plain httptest.Server (not I2P) to a Fetcher
// via a custom http.Client so that the fetch + verify + unpack pipeline can be
// exercised without a live I2P router.
func TestFetcher_FetchHTTP(t *testing.T) {
	want := []byte("<feed>http-test</feed>")
	su3Data, _, _ := makeSu3Bytes(t, want)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-i2p-su3-news")
		w.WriteHeader(http.StatusOK)
		w.Write(su3Data)
	}))
	defer ts.Close()

	// Override the HTTP client to use the plain TCP test server instead of I2P.
	f := &Fetcher{client: ts.Client()}

	got, err := f.FetchAndParse(ts.URL, nil)
	if err != nil {
		t.Fatalf("FetchAndParse: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content mismatch: got %q, want %q", got, want)
	}
}

// TestFetcher_FetchHTTP_NotFound verifies that a 404 response is surfaced as
// an error rather than silently returning empty content.
func TestFetcher_FetchHTTP_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	f := &Fetcher{client: ts.Client()}
	_, err := f.Fetch(ts.URL)
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
}

// TestNewFetcherFromGarlic_Construction verifies that NewFetcherFromGarlic
// accepts a caller-supplied *onramp.Garlic and returns a non-nil Fetcher
// without opening a SAM session (the zero-value Garlic is valid for
// construction; the session is only opened on the first Dial).
func TestNewFetcherFromGarlic_Construction(t *testing.T) {
	g := &onramp.Garlic{}
	f := NewFetcherFromGarlic(g)
	if f == nil {
		t.Fatal("expected non-nil Fetcher from NewFetcherFromGarlic")
	}
	if f.client == nil {
		t.Fatal("expected Fetcher.client to be non-nil")
	}
}

// TestNewFetcherFromGarlic_Pipeline verifies the full fetch+verify+unpack
// pipeline when the Fetcher was created via NewFetcherFromGarlic.  The
// garlic-backed transport is replaced with a plain test-server client so the
// test can run without a live I2P router, while still exercising all code
// paths in Fetch and FetchAndParse.
func TestNewFetcherFromGarlic_Pipeline(t *testing.T) {
	want := []byte("<feed>from-garlic</feed>")
	su3Data, _, _ := makeSu3Bytes(t, want)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-i2p-su3-news")
		w.WriteHeader(http.StatusOK)
		w.Write(su3Data)
	}))
	defer ts.Close()

	g := &onramp.Garlic{}
	f := NewFetcherFromGarlic(g)
	// Swap in the plain test-server client so no SAM connection is needed.
	f.client = ts.Client()

	got, err := f.FetchAndParse(ts.URL, nil)
	if err != nil {
		t.Fatalf("FetchAndParse: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content mismatch: got %q, want %q", got, want)
	}
}

// TestNewFetcherFromClient_Pipeline verifies that NewFetcherFromClient produces
// a working Fetcher when given a plain *http.Client, exercising the constructor
// added for test injection.
func TestNewFetcherFromClient_Pipeline(t *testing.T) {
	want := []byte("<feed>from-client</feed>")
	su3Data, _, _ := makeSu3Bytes(t, want)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(su3Data)
	}))
	defer ts.Close()

	f := NewFetcherFromClient(ts.Client())
	got, err := f.FetchAndParse(ts.URL, nil)
	if err != nil {
		t.Fatalf("FetchAndParse: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content mismatch: got %q, want %q", got, want)
	}
}

// TestCloseSharedGarlic_Idempotent verifies that calling CloseSharedGarlic when
// no session was ever initialised is a no-op (does not panic), and that multiple
// sequential calls are safe.
func TestCloseSharedGarlic_Idempotent(t *testing.T) {
	// These calls should be safe regardless of package-level state: if
	// sharedGarlic is nil (never initialised in this process up to this point,
	// or already closed by a prior test), the function must return silently.
	CloseSharedGarlic()
	CloseSharedGarlic()
}

// TestCloseSharedGarlic_Concurrent races concurrent CloseSharedGarlic calls
// against each other to confirm that garlicMu prevents a data race on the
// sharedGarlic pointer.  Run with "go test -race ./fetch/..." to exercise the
// race detector.
func TestCloseSharedGarlic_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			CloseSharedGarlic()
		}()
	}
	wg.Wait()
}

// TestNewFetcher_AfterClose_ReturnsError verifies that NewFetcher called after
// CloseSharedGarlic returns a non-nil error instead of panicking via a nil
// pointer dereference inside NewFetcherFromGarlic.  This covers the
// use-after-close scenario documented in the AUDIT.
func TestNewFetcher_AfterClose_ReturnsError(t *testing.T) {
	CloseSharedGarlic() // ensure session is marked closed regardless of prior state
	_, err := NewFetcher("")
	if err == nil {
		t.Fatal("expected error from NewFetcher after CloseSharedGarlic, got nil")
	}
}

// TestNewFetcher_AfterClose_ErrorWrapsErrGarlicClosed verifies that the error
// returned by NewFetcher after CloseSharedGarlic wraps ErrGarlicClosed so
// callers can detect the specific condition with errors.Is.
func TestNewFetcher_AfterClose_ErrorWrapsErrGarlicClosed(t *testing.T) {
	CloseSharedGarlic()
	_, err := NewFetcher("")
	if err == nil {
		t.Fatal("expected error from NewFetcher after CloseSharedGarlic, got nil")
	}
	if !errors.Is(err, ErrGarlicClosed) {
		t.Errorf("expected errors.Is(err, ErrGarlicClosed) to be true; got: %v", err)
	}
}
