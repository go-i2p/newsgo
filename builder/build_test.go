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

// TestBuilder_NoURNID verifies that Builder() does not pre-generate a UUID.
// URNID must be set by the cmd layer (either from --feeduri or via a single
// uuid.NewString() call), not inside the constructor.  If Builder() generated
// a UUID, the cmd layer's subsequent assignment would silently discard it,
// wasting an allocation and obscuring ownership of the value.
func TestBuilder_NoURNID(t *testing.T) {
	dir := t.TempDir()
	nb := Builder(
		filepath.Join(dir, "entries.html"),
		filepath.Join(dir, "releases.json"),
		filepath.Join(dir, "blocklist.xml"),
	)
	if nb.URNID != "" {
		t.Errorf("Builder() URNID = %q; want empty string — UUID generation is the caller's responsibility", nb.URNID)
	}
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

// TestBuild_MissingBlocklistFile verifies that Build() succeeds and produces a
// valid feed when BlocklistXML points to a file that does not exist.  Before
// the fix, os.ReadFile returned "no such file or directory" and Build() aborted
// immediately, blocking first-time users who had not created the blocklist file.
func TestBuild_MissingBlocklistFile(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	// Point BlocklistXML at a path that does not exist.
	nb.BlocklistXML = filepath.Join(dir, "does_not_exist.xml")

	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build() with missing blocklist: expected success, got error: %v", err)
	}
	if feed == "" {
		t.Error("Build() with missing blocklist returned empty feed string")
	}
	// The produced feed must still be parseable XML.
	dec := xml.NewDecoder(strings.NewReader(feed))
	for {
		_, err := dec.Token()
		if err != nil && err.Error() == "EOF" {
			break
		}
		if err != nil {
			t.Errorf("feed produced with missing blocklist is not well-formed XML: %v", err)
			break
		}
	}
}

// TestBuild_EmptyBlocklistPath verifies Build() succeeds when BlocklistXML is
// the empty string (i.e. --blockfile ""), consistent with the same design
// intent: absence of a blocklist is not an error.
func TestBuild_EmptyBlocklistPath(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	nb.BlocklistXML = ""

	_, err := nb.Build()
	if err != nil {
		t.Fatalf("Build() with empty BlocklistXML path: expected success, got error: %v", err)
	}
}

// --- LocaleFromPath tests ---

// TestLocaleFromPath covers all 35 locale variants present in i2p.newsxml,
// the canonical English source, and several edge cases.
func TestLocaleFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		// Canonical English — no locale segment.
		{"data/entries.html", "en"},
		// Bare filename, no directory prefix.
		{"entries.html", "en"},
		// Simple two-letter tags.
		{"data/translations/entries.ar.html", "ar"},
		{"data/translations/entries.az.html", "az"},
		{"data/translations/entries.cs.html", "cs"},
		{"data/translations/entries.da.html", "da"},
		{"data/translations/entries.de.html", "de"},
		{"data/translations/entries.el.html", "el"},
		{"data/translations/entries.es.html", "es"},
		{"data/translations/entries.fa.html", "fa"},
		{"data/translations/entries.fi.html", "fi"},
		{"data/translations/entries.fr.html", "fr"},
		{"data/translations/entries.gl.html", "gl"},
		{"data/translations/entries.he.html", "he"},
		{"data/translations/entries.hu.html", "hu"},
		{"data/translations/entries.id.html", "id"},
		{"data/translations/entries.it.html", "it"},
		{"data/translations/entries.ja.html", "ja"},
		{"data/translations/entries.ko.html", "ko"},
		{"data/translations/entries.nb.html", "nb"},
		{"data/translations/entries.nl.html", "nl"},
		{"data/translations/entries.pl.html", "pl"},
		{"data/translations/entries.pt.html", "pt"},
		{"data/translations/entries.ro.html", "ro"},
		{"data/translations/entries.ru.html", "ru"},
		{"data/translations/entries.sv.html", "sv"},
		{"data/translations/entries.tk.html", "tk"},
		{"data/translations/entries.tr.html", "tr"},
		{"data/translations/entries.uk.html", "uk"},
		{"data/translations/entries.vi.html", "vi"},
		{"data/translations/entries.yo.html", "yo"},
		{"data/translations/entries.zh.html", "zh"},
		// Three-letter / script tags.
		{"data/translations/entries.ast.html", "ast"},
		{"data/translations/entries.gan.html", "gan"},
		// Underscore-separated regional subtags — must normalise to hyphen form.
		{"data/translations/entries.es_AR.html", "es-AR"},
		{"data/translations/entries.pt_BR.html", "pt-BR"},
		{"data/translations/entries.zh_TW.html", "zh-TW"},
		// Edge: path contains no directory component.
		{"entries.de.html", "de"},
		// Edge: non-entries HTML file must return "en" (no locale segment).
		{"data/index.html", "en"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := LocaleFromPath(tc.path)
			if got == "" {
				t.Errorf("LocaleFromPath(%q) returned empty string; want %q", tc.path, tc.want)
			}
			if got != tc.want {
				t.Errorf("LocaleFromPath(%q) = %q; want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestLocaleFromPath_NoPanic verifies that LocaleFromPath does not panic for
// any of the 34 known translation locale filenames.
func TestLocaleFromPath_NoPanic(t *testing.T) {
	locales := []string{
		"ar", "ast", "az", "cs", "da", "de", "el", "es", "es_AR", "fa",
		"fi", "fr", "gan", "gl", "he", "hu", "id", "it", "ja", "ko",
		"nb", "nl", "pl", "pt", "pt_BR", "ro", "ru", "sv", "tk", "tr",
		"uk", "vi", "yo", "zh", "zh_TW",
	}
	for _, loc := range locales {
		path := "data/translations/entries." + loc + ".html"
		got := LocaleFromPath(path)
		if got == "" {
			t.Errorf("LocaleFromPath(%q) must not return empty string", path)
		}
		if got == "en" {
			t.Errorf("LocaleFromPath(%q) returned \"en\" for a known translation locale", path)
		}
	}
}

// --- DetectTranslationFiles tests ---

// TestDetectTranslationFiles_Empty verifies that a non-existent or empty
// directory returns nil without panicking.
func TestDetectTranslationFiles_Empty(t *testing.T) {
	dir := t.TempDir()
	if got := DetectTranslationFiles(filepath.Join(dir, "nonexistent")); got != nil {
		t.Errorf("expected nil for missing dir; got %v", got)
	}
	if got := DetectTranslationFiles(dir); got != nil {
		t.Errorf("expected nil for empty dir; got %v", got)
	}
}

// TestDetectTranslationFiles_Discovers verifies that only "entries.{locale}.html"
// files are returned and that other HTML files are ignored.
func TestDetectTranslationFiles_Discovers(t *testing.T) {
	dir := t.TempDir()
	keep := []string{"entries.de.html", "entries.pt_BR.html", "entries.zh_TW.html"}
	skip := []string{"index.html", "entries.html", "README.md", "entries.md"}
	for _, name := range append(keep, skip...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := DetectTranslationFiles(dir)
	if len(got) != len(keep) {
		t.Fatalf("expected %d files; got %d: %v", len(keep), len(got), got)
	}
	byBase := make(map[string]bool)
	for _, p := range got {
		byBase[filepath.Base(p)] = true
	}
	for _, name := range keep {
		if !byBase[name] {
			t.Errorf("expected %q in results; got %v", name, got)
		}
	}
	for _, name := range skip {
		if byBase[name] {
			t.Errorf("unexpected file %q in results", name)
		}
	}
}

// TestDetectTranslationFiles_SubdirsIgnored verifies that subdirectories
// inside the translations dir are not returned as translation files.
func TestDetectTranslationFiles_SubdirsIgnored(t *testing.T) {
	dir := t.TempDir()
	// A subdirectory named like a translation file must be ignored.
	if err := os.Mkdir(filepath.Join(dir, "entries.de.html"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "entries.fr.html"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	got := DetectTranslationFiles(dir)
	if len(got) != 1 || filepath.Base(got[0]) != "entries.fr.html" {
		t.Errorf("expected only entries.fr.html; got %v", got)
	}
}

// --- xml:lang end-to-end tests ---

// TestBuild_DefaultLanguageIsEnglish verifies that a NewsBuilder constructed
// without setting Language emits xml:lang="en" in the feed header, preserving
// backward compatibility for callers that construct NewsBuilder directly.
func TestBuild_DefaultLanguageIsEnglish(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	// Language field is intentionally left at its zero value.
	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(feed, `xml:lang="en"`) {
		t.Errorf(`expected xml:lang="en" in feed header; got feed snippet: %s`,
			excerptAround(feed, "xml:lang"))
	}
}

// TestBuild_LanguageFieldPropagatedToHeader verifies that setting Language on
// NewsBuilder results in the correct xml:lang attribute value in the feed.
func TestBuild_LanguageFieldPropagatedToHeader(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	nb.Language = "de"
	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(feed, `xml:lang="de"`) {
		t.Errorf(`expected xml:lang="de"; feed snippet: %s`,
			excerptAround(feed, "xml:lang"))
	}
}

// TestBuild_RegionalLocaleInHeader verifies that a regional BCP 47 tag
// (e.g. "pt-BR") round-trips correctly through the feed header.
func TestBuild_RegionalLocaleInHeader(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	nb.Language = "pt-BR"
	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(feed, `xml:lang="pt-BR"`) {
		t.Errorf(`expected xml:lang="pt-BR"; feed snippet: %s`,
			excerptAround(feed, "xml:lang"))
	}
}

// TestBuild_XmlLangAttributeIsWellFormedXML verifies that an xml:lang value
// containing a hyphen (regional subtag) does not break XML well-formedness.
func TestBuild_XmlLangAttributeIsWellFormedXML(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	nb.Language = "zh-TW"
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
			t.Errorf("feed with xml:lang=\"zh-TW\" is not well-formed XML: %v", err)
			break
		}
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

// --- Feed.HeaderTitle fallback tests ---

// TestBuild_HeaderTitle_UsedAsFallback verifies that when NewsBuilder.TITLE is
// empty the <title> element in the Atom feed is sourced from the <header>
// element of the entries HTML file (Feed.HeaderTitle). This is the primary
// consumer of the previously-dead HeaderTitle field.
func TestBuild_HeaderTitle_UsedAsFallback(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	// Clear the explicit TITLE so the fallback path is exercised.
	nb.TITLE = ""
	// The entries.html fixture written by writeFixtures has
	// <header>Test Feed</header>, which LoadHTML() stores in HeaderTitle.
	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	// gohtml.Format may add whitespace inside elements; use a regexp that
	// tolerates leading/trailing whitespace within <title>…</title>.
	titleRe := regexp.MustCompile(`(?s)<title>\s*Test Feed\s*</title>`)
	if !titleRe.MatchString(feed) {
		t.Errorf("expected <title>Test Feed</title> from HTML header fallback; feed snippet:\n%s",
			excerptAround(feed, "title"))
	}
}

// TestBuild_HeaderTitle_ExplicitTitleWins verifies that when both TITLE and
// Feed.HeaderTitle are non-empty, TITLE takes precedence.
func TestBuild_HeaderTitle_ExplicitTitleWins(t *testing.T) {
	dir := t.TempDir()
	nb := writeFixtures(t, dir)
	nb.TITLE = "Explicit Title"
	// entries.html has <header>Test Feed</header>; that must not win.
	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	// gohtml.Format may add whitespace inside elements; use a regexp that
	// tolerates leading/trailing whitespace within <title>…</title>.
	titleRe := regexp.MustCompile(`(?s)<title>\s*Explicit Title\s*</title>`)
	if !titleRe.MatchString(feed) {
		t.Errorf("expected <title>Explicit Title</title>; feed snippet:\n%s",
			excerptAround(feed, "title"))
	}
	if strings.Contains(feed, "Test Feed") {
		t.Errorf("HeaderTitle leaked into feed when TITLE was set; feed snippet:\n%s",
			excerptAround(feed, "title"))
	}
}

// TestBuild_HeaderTitle_BothEmpty verifies that when neither TITLE nor
// HeaderTitle is set the <title> element is present but empty, producing
// well-formed XML without panicking.
func TestBuild_HeaderTitle_BothEmpty(t *testing.T) {
	dir := t.TempDir()
	// Write entries.html without a <header> element so HeaderTitle stays empty.
	releasesPath := filepath.Join(dir, "releases.json")
	blocklistPath := filepath.Join(dir, "blocklist.xml")
	entriesPath := filepath.Join(dir, "entries.html")
	if err := os.WriteFile(releasesPath, []byte(validReleasesJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocklistPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	html := `<html><body>
<article id="urn:test:1" title="T" href="http://example.com"
         author="A" published="2024-01-01" updated="2024-01-02">
<details><summary>S</summary></details>
<p>Body</p>
</article>
</body></html>`
	if err := os.WriteFile(entriesPath, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	nb := Builder(entriesPath, releasesPath, blocklistPath)
	nb.URNID = "00000000-0000-0000-0000-000000000000"
	nb.TITLE = "" // explicit clear
	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	// <title></title> is still well-formed XML.
	dec := xml.NewDecoder(strings.NewReader(feed))
	for {
		_, err := dec.Token()
		if err != nil && err.Error() == "EOF" {
			break
		}
		if err != nil {
			t.Errorf("feed with empty title is not well-formed XML: %v", err)
			break
		}
	}
}

// TestBuild_HeaderTitle_XMLEscaped verifies that a <header> containing
// XML-special characters is escaped correctly in the Atom <title> element.
func TestBuild_HeaderTitle_XMLEscaped(t *testing.T) {
	dir := t.TempDir()
	releasesPath := filepath.Join(dir, "releases.json")
	blocklistPath := filepath.Join(dir, "blocklist.xml")
	entriesPath := filepath.Join(dir, "entries.html")
	if err := os.WriteFile(releasesPath, []byte(validReleasesJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocklistPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// Header title contains & which must be escaped as &amp; in XML.
	html := `<html><body>
<header>News &amp; Updates</header>
<article id="urn:test:1" title="T" href="http://example.com"
         author="A" published="2024-01-01" updated="2024-01-02">
<details><summary>S</summary></details>
<p>Body</p>
</article>
</body></html>`
	if err := os.WriteFile(entriesPath, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	nb := Builder(entriesPath, releasesPath, blocklistPath)
	nb.URNID = "00000000-0000-0000-0000-000000000000"
	nb.TITLE = "" // use HeaderTitle fallback
	feed, err := nb.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	// Bare & must not appear in the XML output.
	if strings.Contains(feed, "News & Updates") {
		t.Errorf("bare & in HeaderTitle not XML-escaped; feed snippet:\n%s",
			excerptAround(feed, "title"))
	}
	// The output must still be well-formed XML.
	dec := xml.NewDecoder(strings.NewReader(feed))
	for {
		_, err := dec.Token()
		if err != nil && err.Error() == "EOF" {
			break
		}
		if err != nil {
			t.Errorf("feed with escaped HeaderTitle is not well-formed XML: %v\nfeed:\n%s", err, feed)
			break
		}
	}
}
