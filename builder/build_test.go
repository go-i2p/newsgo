package newsbuilder

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// validReleasesJSON is a minimal releases.json fixture for testing.
const validReleasesJSON = `[{
"date": "2022-11-21",
"version": "2.0.0",
"minVersion": "0.9.9",
"minJavaVersion": "1.8",
"updates": {
"su3": {
"torrent": "magnet:?xt=urn:btih:abc123",
"url": [
"http://stats.i2p/i2p/2.0.0/i2pupdate.su3",
"http://example.b32.i2p/releases/2.0.0/i2pupdate.su3"
]
}
}
}]`

// writeFixtures writes the minimum set of files needed by NewsBuilder.Build()
// into dir and returns a configured *NewsBuilder pointing at them.
func writeFixtures(t *testing.T, dir string) *NewsBuilder {
	t.Helper()
	releasesPath := filepath.Join(dir, "releases.json")
	blocklistPath := filepath.Join(dir, "blocklist.xml")
	entriesPath := filepath.Join(dir, "entries.html")

	if err := os.WriteFile(releasesPath, []byte(validReleasesJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	// Minimal valid blocklist fragment (must be XML-parseable in context).
	if err := os.WriteFile(blocklistPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	html := `<html><body>
<header>Test Feed</header>
<article id="urn:test:1" title="Title" href="http://example.com"
         author="Author" published="2024-01-01" updated="2024-01-02">
<details><summary>Summary</summary></details>
<p>Body</p>
</article>
</body></html>`
	if err := os.WriteFile(entriesPath, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}

	nb := Builder(entriesPath, releasesPath, blocklistPath)
	nb.URNID = "00000000-0000-0000-0000-000000000000"
	return nb
}

// --- JSONtoXML tests ---

// TestJSONtoXML_ValidInput verifies that well-formed JSON produces the expected
// XML fragment without panicking.
func TestJSONtoXML_ValidInput(t *testing.T) {
	dir := t.TempDir()
	rp := filepath.Join(dir, "releases.json")
	if err := os.WriteFile(rp, []byte(validReleasesJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	nb := &NewsBuilder{ReleasesJson: rp}
	got, err := nb.JSONtoXML()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Attribute values must be quoted.
	if !strings.Contains(got, `date="2022-11-21"`) {
		t.Errorf("date attribute not quoted; output: %s", got)
	}
	if !strings.Contains(got, `minVersion="0.9.9"`) {
		t.Errorf("minVersion attribute not quoted; output: %s", got)
	}
	if !strings.Contains(got, `minJavaVersion="1.8"`) {
		t.Errorf("minJavaVersion attribute not quoted; output: %s", got)
	}
	// Version element must be present.
	if !strings.Contains(got, "<i2p:version>2.0.0</i2p:version>") {
		t.Errorf("version element missing; output: %s", got)
	}
	// Both URLs must appear.
	if !strings.Contains(got, "stats.i2p") {
		t.Errorf("first URL missing; output: %s", got)
	}
	if !strings.Contains(got, "example.b32.i2p") {
		t.Errorf("second URL missing; output: %s", got)
	}
}

// TestJSONtoXML_MissingUpdatesKey verifies that an absent "updates" key returns
// a descriptive error instead of panicking with a nil interface conversion.
func TestJSONtoXML_MissingUpdatesKey(t *testing.T) {
	dir := t.TempDir()
	rp := filepath.Join(dir, "releases.json")
	// No "updates" field.
	j := `[{"date":"2022-11-21","version":"2.0.0","minVersion":"0.9.9","minJavaVersion":"1.8"}]`
	if err := os.WriteFile(rp, []byte(j), 0o644); err != nil {
		t.Fatal(err)
	}
	nb := &NewsBuilder{ReleasesJson: rp}
	_, err := nb.JSONtoXML()
	if err == nil {
		t.Fatal("expected error for missing 'updates' key, got nil")
	}
}

// TestJSONtoXML_MissingSu3Key verifies that an absent "su3" key returns an error.
func TestJSONtoXML_MissingSu3Key(t *testing.T) {
	dir := t.TempDir()
	rp := filepath.Join(dir, "releases.json")
	j := `[{"date":"2022-11-21","version":"2.0.0","minVersion":"0.9.9","minJavaVersion":"1.8","updates":{}}]`
	if err := os.WriteFile(rp, []byte(j), 0o644); err != nil {
		t.Fatal(err)
	}
	nb := &NewsBuilder{ReleasesJson: rp}
	_, err := nb.JSONtoXML()
	if err == nil {
		t.Fatal("expected error for missing 'su3' key, got nil")
	}
}

// TestJSONtoXML_EmptyArray verifies that an empty JSON array returns an error
// rather than an index-out-of-range panic.
func TestJSONtoXML_EmptyArray(t *testing.T) {
	dir := t.TempDir()
	rp := filepath.Join(dir, "releases.json")
	if err := os.WriteFile(rp, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	nb := &NewsBuilder{ReleasesJson: rp}
	_, err := nb.JSONtoXML()
	if err == nil {
		t.Fatal("expected error for empty releases array, got nil")
	}
}

// TestJSONtoXML_MissingStringField verifies that a missing scalar field returns
// a clear error.
func TestJSONtoXML_MissingStringField(t *testing.T) {
	dir := t.TempDir()
	rp := filepath.Join(dir, "releases.json")
	// "version" is absent.
	j := `[{"date":"2022-11-21","minVersion":"0.9.9","minJavaVersion":"1.8",
         "updates":{"su3":{"torrent":"magnet:x","url":["http://example.com"]}}}]`
	if err := os.WriteFile(rp, []byte(j), 0o644); err != nil {
		t.Fatal(err)
	}
	nb := &NewsBuilder{ReleasesJson: rp}
	_, err := nb.JSONtoXML()
	if err == nil {
		t.Fatal("expected error for missing 'version' field, got nil")
	}
}

// --- Build() timestamp tests ---

// TestBuild_TimestampIsUTC verifies that the <updated> timestamp uses a UTC
// time value. The old code used time.Now() (local time) with a hardcoded
// +00:00 offset, which produces a wrong timestamp on non-UTC hosts.
func TestBuild_TimestampIsUTC(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	// gohtml.Format wraps the XML; look for the updated element content.
	// The timestamp must end with +00:00 and the fractional seconds must be
	// exactly 3 digits (milliseconds).
	rfc3339ms := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}\+00:00`)
	if !rfc3339ms.MatchString(feed) {
		t.Errorf("no RFC-3339 millisecond timestamp with +00:00 found in output;\ngot: %s", feed)
	}
}

// TestBuild_UpdatedElementHasNoTrailingNewline verifies that the text content
// of the <updated> element is a bare RFC-3339 timestamp with no embedded
// newline characters within the timestamp itself.
// The old format string contained a literal "\n" which was injected into the
// timestamp value, causing strict Atom validators and timestamp parsers to fail.
// Note: gohtml.Format adds surrounding whitespace indentation, so we TrimSpace
// before checking the timestamp content.
func TestBuild_UpdatedElementHasNoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	// Extract the text between <updated> and </updated>.
	start := strings.Index(feed, "<updated>")
	end := strings.Index(feed, "</updated>")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("<updated> element not found in output:\n%s", feed)
	}
	// gohtml.Format adds surrounding indentation; TrimSpace to isolate the value.
	content := strings.TrimSpace(feed[start+len("<updated>") : end])
	// The trimmed value must match the RFC-3339 millisecond pattern exactly.
	// Any embedded newline in the timestamp (from the old \n in Sprintf) would
	// cause this match to fail because the regex anchors to full-string match.
	rfc3339exact := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}\+00:00$`)
	if !rfc3339exact.MatchString(content) {
		t.Errorf("<updated> text is not a clean RFC-3339 timestamp; got %q", content)
	}
}

// TestBuild_AttributesAreQuoted verifies that the <i2p:release> element has
// all its attribute values enclosed in double quotes, as required by XML.
func TestBuild_AttributesAreQuoted(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(feed, `date="2022-11-21"`) {
		t.Errorf(`date attribute not quoted; output snippet: %s`, excerptAround(feed, "i2p:release"))
	}
	if !strings.Contains(feed, `minVersion="0.9.9"`) {
		t.Errorf(`minVersion attribute not quoted`)
	}
	if !strings.Contains(feed, `minJavaVersion="1.8"`) {
		t.Errorf(`minJavaVersion attribute not quoted`)
	}
}

// TestBuild_ProducesWellFormedXML verifies that the generated Atom feed can be
// parsed by the standard XML decoder.
func TestBuild_ProducesWellFormedXML(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	// xml.Unmarshal into a generic token stream is the simplest well-formedness check.
	dec := xml.NewDecoder(strings.NewReader(feed))
	for {
		_, err := dec.Token()
		if err != nil && err.Error() == "EOF" {
			break
		}
		if err != nil {
			t.Errorf("generated feed is not well-formed XML: %v", err)
			break
		}
	}
}

// excerptAround returns a short substring of s centred on the first occurrence
// of substr, useful for test failure messages.
func excerptAround(s, substr string) string {
	idx := strings.Index(s, substr)
	if idx < 0 {
		return s
	}
	start := idx - 100
	if start < 0 {
		start = 0
	}
	end := idx + 200
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}

// TestBuild_XMLEscapingInMetadata verifies that XML-special characters in
// NewsBuilder metadata fields (TITLE, SUBTITLE, SITEURL) are escaped before
// being inserted into the feed, producing well-formed XML.  A bare '&' in a
// title or URL is extremely common in real deployments.
func TestBuild_XMLEscapingInMetadata(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	nb.TITLE = "I2P News & Updates"
	nb.SUBTITLE = "Feed for <i2p> network"
	nb.SITEURL = "http://example.com/?a=1&b=2"

	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	dec := xml.NewDecoder(strings.NewReader(feed))
	for {
		_, err := dec.Token()
		if err != nil && err.Error() == "EOF" {
			break
		}
		if err != nil {
			t.Errorf("feed with special chars in metadata is not well-formed XML: %v\n\nfeed:\n%s", err, feed)
			break
		}
	}
	// Bare & must not survive into the output.
	if strings.Contains(feed, "News & Updates") {
		t.Errorf("bare & in TITLE not escaped; feed snippet: %s", excerptAround(feed, "title"))
	}
	if strings.Contains(feed, "?a=1&b=2") {
		t.Errorf("bare & in SITEURL not escaped")
	}
}

// TestBuild_XMLEscapingInArticle verifies that an article whose href contains
// '&' produces well-formed XML in the full generated feed.  This is the most
// common real-world trigger: news links frequently include query parameters.
func TestBuild_XMLEscapingInArticle(t *testing.T) {
	dir := t.TempDir()
	entriesPath := filepath.Join(dir, "entries.html")
	releasesPath := filepath.Join(dir, "releases.json")
	blocklistPath := filepath.Join(dir, "blocklist.xml")

	if err := os.WriteFile(releasesPath, []byte(validReleasesJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocklistPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// Use HTML-encoded &amp; in the href value — the HTML parser decodes this
	// to a bare '&', which xmlEsc must then re-encode as '&amp;' in the XML.
	html := `<html><body>
<header>Feed</header>
<article id="urn:test:1" title="A &amp; B" href="http://example.com/page?a=1&amp;b=2"
         author="Author" published="2024-01-01" updated="2024-01-02">
<details><summary>Summary &amp; more</summary></details>
<p>Body paragraph</p>
<p>Extra paragraph</p>
</article>
</body></html>`
	if err := os.WriteFile(entriesPath, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	nb := Builder(entriesPath, releasesPath, blocklistPath)
	nb.URNID = "00000000-0000-0000-0000-000000000000"

	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	dec := xml.NewDecoder(strings.NewReader(feed))
	for {
		_, err := dec.Token()
		if err != nil && err.Error() == "EOF" {
			break
		}
		if err != nil {
			t.Errorf("feed with & in article href is not well-formed XML: %v\n\nfeed:\n%s", err, feed)
			break
		}
	}
}

// TestJSONtoXML_XMLEscapingInURLs verifies that URL values in releases.json
// that contain '&' (e.g. from torrent magnet links or tracker query strings)
// are XML-escaped in the generated <i2p:update> fragment.
func TestJSONtoXML_XMLEscapingInURLs(t *testing.T) {
	dir := t.TempDir()
	rp := filepath.Join(dir, "releases.json")
	// Torrent magnet link contains multiple & separators; these are common.
	j := `[{"date":"2022-11-21","version":"2.0.0","minVersion":"0.9.9","minJavaVersion":"1.8",
"updates":{"su3":{"torrent":"magnet:?xt=urn:btih:abc&tr=http://example.com&dn=file",
"url":["http://stats.i2p/update.su3?a=1&b=2"]}}}]`
	if err := os.WriteFile(rp, []byte(j), 0o644); err != nil {
		t.Fatal(err)
	}
	nb := &NewsBuilder{ReleasesJson: rp}
	got, err := nb.JSONtoXML()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The fragment must be parseable as XML (wrap in a root element for the decoder).
	doc := "<root xmlns:i2p=\"http://geti2p.net/en/docs/spec/updates\">" + got + "</root>"
	dec := xml.NewDecoder(strings.NewReader(doc))
	for {
		_, err := dec.Token()
		if err != nil && err.Error() == "EOF" {
			break
		}
		if err != nil {
			t.Errorf("JSONtoXML with & in URL is not well-formed XML: %v\n\noutput:\n%s", err, got)
			break
		}
	}
	// Bare & must not appear in the output.
	if strings.Contains(got, "btih:abc&tr=") {
		t.Errorf("bare & in torrent href not escaped")
	}
}

// --- validateBlocklistXML / blocklist validation tests ---

// TestValidateBlocklistXML_Empty verifies that an empty blocklist is accepted
// without error.  This is the most common production case.
func TestValidateBlocklistXML_Empty(t *testing.T) {
	if err := validateBlocklistXML([]byte("")); err != nil {
		t.Errorf("empty blocklist: unexpected error: %v", err)
	}
}

// TestValidateBlocklistXML_ValidFragment verifies that a well-formed XML
// fragment with no declaration is accepted.
func TestValidateBlocklistXML_ValidFragment(t *testing.T) {
	fragment := []byte(`<i2p:blocklist xmlns:i2p="http://geti2p.net/en/docs/spec/updates"><i2p:block host="bad.i2p"/></i2p:blocklist>`)
	if err := validateBlocklistXML(fragment); err != nil {
		t.Errorf("valid fragment: unexpected error: %v", err)
	}
}

// TestValidateBlocklistXML_XMLDeclaration verifies that a blocklist starting
// with an XML declaration is rejected.  Two XML declarations in one document
// are forbidden by the XML specification.
func TestValidateBlocklistXML_XMLDeclaration(t *testing.T) {
	withDecl := []byte(`<?xml version='1.0'?><i2p:blocklist xmlns:i2p="http://geti2p.net/en/docs/spec/updates"/>`)
	err := validateBlocklistXML(withDecl)
	if err == nil {
		t.Fatal("expected error for blocklist with XML declaration, got nil")
	}
	if !strings.Contains(err.Error(), "declaration") {
		t.Errorf("error should mention declaration; got: %v", err)
	}
}

// TestValidateBlocklistXML_MalformedXML verifies that a blocklist with broken
// XML markup is rejected with a descriptive error rather than silently spliced.
func TestValidateBlocklistXML_MalformedXML(t *testing.T) {
	malformed := []byte(`<i2p:blocklist><unclosed`)
	err := validateBlocklistXML(malformed)
	if err == nil {
		t.Fatal("expected error for malformed XML blocklist, got nil")
	}
}

// TestValidateBlocklistXML_UnclosedElement verifies that an unclosed element
// (which would corrupt the feed document tree) is rejected.
func TestValidateBlocklistXML_UnclosedElement(t *testing.T) {
	unclosed := []byte(`<i2p:blocklist xmlns:i2p="http://example.com">`)
	err := validateBlocklistXML(unclosed)
	if err == nil {
		t.Fatal("expected error for unclosed element, got nil")
	}
}

// TestBuild_BlocklistWithDeclaration verifies that Build() returns an error
// (not a corrupted feed) when the blocklist file contains an XML declaration.
func TestBuild_BlocklistWithDeclaration(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	// Overwrite the blocklist with a declaration-bearing file.
	bad := `<?xml version='1.0'?><i2p:blocklist xmlns:i2p="http://geti2p.net/en/docs/spec/updates"/>`
	if err := os.WriteFile(nb.BlocklistXML, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := nb.Build()
	if err == nil {
		t.Fatal("expected Build() to return error for blocklist with XML declaration, got nil")
	}
}

// TestBuild_MalformedBlocklist verifies that Build() returns an error when the
// blocklist file contains broken XML instead of silently producing an invalid feed.
func TestBuild_MalformedBlocklist(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	if err := os.WriteFile(nb.BlocklistXML, []byte("<broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := nb.Build()
	if err == nil {
		t.Fatal("expected Build() to return error for malformed blocklist, got nil")
	}
}

// TestBuild_ValidBlocklistFragment verifies that a well-formed blocklist
// fragment without an XML declaration is accepted and appears in the feed.
func TestBuild_ValidBlocklistFragment(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	fragment := `<i2p:blocklist xmlns:i2p="http://geti2p.net/en/docs/spec/updates"><i2p:block host="evil.i2p"/></i2p:blocklist>`
	if err := os.WriteFile(nb.BlocklistXML, []byte(fragment), 0o644); err != nil {
		t.Fatal(err)
	}
	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build() with valid blocklist fragment: %v", err)
	}
	// The fragment content must appear in the output.
	if !strings.Contains(feed, "evil.i2p") {
		t.Errorf("blocklist content not present in feed output")
	}
	// The overall feed must still be well-formed XML.
	dec := xml.NewDecoder(strings.NewReader(feed))
	for {
		_, err := dec.Token()
		if err != nil && err.Error() == "EOF" {
			break
		}
		if err != nil {
			t.Errorf("feed with blocklist fragment is not well-formed XML: %v", err)
			break
		}
	}
}
