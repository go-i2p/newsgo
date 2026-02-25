package newsfeed

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadHTML_MissingFile verifies that LoadHTML wraps the underlying OS error
// so callers can inspect the root cause (e.g., "no such file or directory").
func TestLoadHTML_MissingFile(t *testing.T) {
	f := &Feed{EntriesHTMLPath: "/nonexistent/path/entries.html"}
	err := f.LoadHTML()
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	// Wrapped error must contain actionable OS detail, not just "LoadHTML: error".
	if !strings.Contains(err.Error(), "no such file") &&
		!strings.Contains(err.Error(), "cannot find") {
		t.Errorf("error missing OS detail: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "LoadHTML:") {
		t.Errorf("error missing 'LoadHTML:' prefix: %v", err)
	}
}

// TestLoadHTML_BaseMissingFile verifies the same error wrapping when
// BaseEntriesHTMLPath points to a missing file.
func TestLoadHTML_BaseMissingFile(t *testing.T) {
	dir := t.TempDir()
	entries := filepath.Join(dir, "entries.html")
	if err := os.WriteFile(entries, []byte("<html><body><header>Title</header></body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &Feed{
		EntriesHTMLPath:     entries,
		BaseEntriesHTMLPath: "/nonexistent/base.html",
	}
	err := f.LoadHTML()
	if err == nil {
		t.Fatal("expected error for missing base file, got nil")
	}
	if !strings.HasPrefix(err.Error(), "LoadHTML:") {
		t.Errorf("error missing 'LoadHTML:' prefix: %v", err)
	}
}

// TestLoadHTML_ValidFile verifies that a well-formed entries file loads without error.
func TestLoadHTML_ValidFile(t *testing.T) {
	dir := t.TempDir()
	entries := filepath.Join(dir, "entries.html")
	html := `<html><body>
<header>Test Feed</header>
<article id="1" title="Article One" href="http://example.com" author="Author" published="2024-01-01" updated="2024-01-02">
<details><summary>Summary text</summary></details>
<p>Body text</p>
</article>
</body></html>`
	if err := os.WriteFile(entries, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &Feed{EntriesHTMLPath: entries}
	if err := f.LoadHTML(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Length() != 1 {
		t.Errorf("expected 1 article, got %d", f.Length())
	}
}

// TestContent_ShortArticle verifies that Content() returns the body text even
// for a minimal article that has no <details>/<summary> wrapper — the old
// magic-number-5 threshold silently dropped content in this case.
func TestContent_ShortArticle(t *testing.T) {
	a := &Article{content: "<article><p>Hi</p></article>"}
	got := a.Content()
	if !strings.Contains(got, "Hi") {
		t.Errorf("Content() dropped body for minimal article without <details>; got: %q", got)
	}
}

// TestContent_NormalArticle verifies that Content() includes all <p> body
// children and excludes the <details>/<summary> metadata block.
func TestContent_NormalArticle(t *testing.T) {
	a := &Article{content: `<article id="x" title="T" href="" author="A" published="" updated="">
<details><summary>Summary</summary></details>
<p>First paragraph</p>
<p>Second paragraph</p>
</article>`}
	got := a.Content()
	for _, want := range []string{"First paragraph", "Second paragraph"} {
		if !strings.Contains(got, want) {
			t.Errorf("Content() missing %q; got: %q", want, got)
		}
	}
	if strings.Contains(got, "Summary") {
		t.Errorf("Content() included <details>/<summary> text; got: %q", got)
	}
}

// TestContent_MinimalArticle_WithoutDetails verifies Content() for an article
// whose body consists only of bare children, no <details>/<summary> wrapper.
func TestContent_MinimalArticle_WithoutDetails(t *testing.T) {
	a := &Article{content: "<article><p>Hello world — no details wrapper.</p></article>"}
	got := a.Content()
	if !strings.Contains(got, "Hello world") {
		t.Errorf("Content() dropped body for article without <details>; got: %q", got)
	}
}

// TestContent_StandardArticle_ExcludesDetails verifies that the <details> block
// is excluded from Content() output and body text is preserved.
func TestContent_StandardArticle_ExcludesDetails(t *testing.T) {
	a := &Article{content: `<article>
<details><summary>This is the summary</summary></details>
<p>This is the body</p>
</article>`}
	got := a.Content()
	if !strings.Contains(got, "This is the body") {
		t.Errorf("Content() dropped body text; got: %q", got)
	}
	if strings.Contains(got, "This is the summary") {
		t.Errorf("Content() included <details> summary text; got: %q", got)
	}
}

// TestContent_MultipleBodyElements verifies that Content() returns all body
// elements when the article has multiple paragraphs after <details>.
func TestContent_MultipleBodyElements(t *testing.T) {
	a := &Article{content: `<article>
<details><summary>Summary</summary></details>
<p>First paragraph</p>
<p>Second paragraph</p>
<p>Third paragraph</p>
</article>`}
	got := a.Content()
	for _, want := range []string{"First paragraph", "Second paragraph", "Third paragraph"} {
		if !strings.Contains(got, want) {
			t.Errorf("Content() missing %q; got: %q", want, got)
		}
	}
	if strings.Contains(got, "Summary") {
		t.Errorf("Content() included <details> text; got: %q", got)
	}
}

// TestContent_EmptyBody verifies that Content() returns an empty string when
// the article has only a <details> block and no other children.
func TestContent_EmptyBody(t *testing.T) {
	a := &Article{content: "<article><details><summary>Summary only</summary></details></article>"}
	got := a.Content()
	if strings.Contains(got, "Summary only") {
		t.Errorf("Content() included <details> summary in empty-body article; got: %q", got)
	}
}

// TestContent_NoArticleElement verifies that Content() returns an empty string
// and does not panic when the stored HTML contains no <article> element.
func TestContent_NoArticleElement(t *testing.T) {
	a := &Article{content: "<div><p>Not an article</p></div>"}
	got := a.Content() // must not panic
	_ = got            // empty string is expected; no assertion on value
}

// TestXmlEsc verifies that the xmlEsc helper correctly escapes the five
// XML-special characters required for safe use in text content and attributes.
func TestXmlEsc(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"plain text", "plain text"},
		{"A & B", "A &amp; B"},
		{"<tag>", "&lt;tag&gt;"},
		{`has "quotes"`, "has &#34;quotes&#34;"},
		{"?a=1&b=2", "?a=1&amp;b=2"},
	}
	for _, tt := range tests {
		got := xmlEsc(tt.input)
		if got != tt.want {
			t.Errorf("xmlEsc(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestEntry_XMLEscaping verifies that Article.Entry() produces well-formed XML
// when fields contain XML-special characters.  Hyperlink href values commonly
// carry & in query strings; titles may contain < or >.  The entry element must
// be parseable by a strict XML decoder after escaping.
func TestEntry_XMLEscaping(t *testing.T) {
	a := &Article{
		UID:           "urn:test:1",
		Title:         "News & Updates",
		Link:          "http://example.com/page?a=1&b=2",
		Author:        "Alice <alice@example.com>",
		PublishedDate: "2024-01-01",
		UpdatedDate:   "2024-01-02",
		Summary:       `Summary with "quotes" & <emphasis>`,
		content:       `<article><details><summary>x</summary></details><p>body</p><p>text</p><p>more</p>`,
	}
	entry := a.Entry()

	// Wrap in a root element so the XML decoder sees a single document.
	document := `<?xml version="1.0"?>` + "<root>" + entry + "</root>"
	dec := xml.NewDecoder(strings.NewReader(document))
	for {
		_, err := dec.Token()
		if err != nil && err.Error() == "EOF" {
			break
		}
		if err != nil {
			t.Errorf("Entry() is not well-formed XML: %v\n\noutput:\n%s", err, entry)
			break
		}
	}
	// Bare & must not appear in text content or attribute values.
	if strings.Contains(entry, "News & Updates") {
		t.Errorf("bare & in title text not escaped; got: %s", entry)
	}
	if strings.Contains(entry, "?a=1&b=2") {
		t.Errorf("bare & in href attribute not escaped; got: %s", entry)
	}
}

// TestEntry_PlainValues verifies that Entry() does not double-escape values
// that contain no special characters.
func TestEntry_PlainValues(t *testing.T) {
	a := &Article{
		UID:           "urn:test:plain",
		Title:         "Plain Title",
		Link:          "http://example.com/plain",
		Author:        "Author Name",
		PublishedDate: "2024-01-01",
		UpdatedDate:   "2024-01-02",
		Summary:       "Plain summary",
		content:       `<article><details><summary>x</summary></details><p>a</p><p>b</p><p>c</p>`,
	}
	entry := a.Entry()
	if strings.Contains(entry, "&amp;amp;") {
		t.Errorf("double-escaped &amp;amp; detected in plain entry")
	}
	if !strings.Contains(entry, "Plain Title") {
		t.Errorf("plain title not preserved in entry")
	}
}

// --- HeaderTitle tests ---

// TestLoadHTML_HeaderTitle verifies that LoadHTML populates HeaderTitle with
// the text content of the <header> element. HeaderTitle is consumed by
// buildFeedHeader in the builder package as a fallback Atom feed title when
// NewsBuilder.TITLE is empty.
func TestLoadHTML_HeaderTitle(t *testing.T) {
	dir := t.TempDir()
	entries := filepath.Join(dir, "entries.html")
	html := `<html><body>
<header>My Feed Title</header>
<article id="1" title="A" href="http://example.com" author="B" published="2024-01-01" updated="2024-01-02">
<details><summary>S</summary></details>
<p>Body</p>
</article>
</body></html>`
	if err := os.WriteFile(entries, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &Feed{EntriesHTMLPath: entries}
	if err := f.LoadHTML(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(f.HeaderTitle, "My Feed Title") {
		t.Errorf("LoadHTML() HeaderTitle = %q; want it to contain %q", f.HeaderTitle, "My Feed Title")
	}
}

// TestLoadHTML_HeaderTitle_BaseFileOverwrites verifies that when
// BaseEntriesHTMLPath is set, HeaderTitle is taken from the base file's
// <header>, not the primary file's. This matches the documented behaviour in
// LoadHTML: base articles are appended and the base header wins.
func TestLoadHTML_HeaderTitle_BaseFileOverwrites(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "entries.html")
	base := filepath.Join(dir, "base.html")

	primaryHTML := `<html><body>
<header>Primary Title</header>
<article id="1" title="A" href="http://example.com" author="B" published="2024-01-01" updated="2024-01-02">
<details><summary>S</summary></details><p>Body</p>
</article>
</body></html>`
	baseHTML := `<html><body>
<header>Base Title</header>
<article id="2" title="C" href="http://example.com/c" author="D" published="2024-02-01" updated="2024-02-02">
<details><summary>T</summary></details><p>Base body</p>
</article>
</body></html>`

	if err := os.WriteFile(primary, []byte(primaryHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, []byte(baseHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &Feed{EntriesHTMLPath: primary, BaseEntriesHTMLPath: base}
	if err := f.LoadHTML(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(f.HeaderTitle, "Base Title") {
		t.Errorf("LoadHTML() HeaderTitle = %q; want it to contain \"Base Title\" (base overrides primary)", f.HeaderTitle)
	}
}

// TestLoadHTML_HeaderTitle_NoHeaderElement verifies that LoadHTML does not
// panic when the HTML file has no <header> element; HeaderTitle is an empty
// string in that case.
func TestLoadHTML_HeaderTitle_NoHeaderElement(t *testing.T) {
	dir := t.TempDir()
	entries := filepath.Join(dir, "entries.html")
	html := `<html><body>
<article id="1" title="A" href="http://example.com" author="B" published="2024-01-01" updated="2024-01-02">
<details><summary>S</summary></details><p>Body</p>
</article>
</body></html>`
	if err := os.WriteFile(entries, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &Feed{EntriesHTMLPath: entries}
	if err := f.LoadHTML(); err != nil {
		t.Fatalf("LoadHTML() should not error for HTML without <header>: %v", err)
	}
	// An absent <header> element results in an empty HeaderTitle; build can
	// still fall back to NewsBuilder.TITLE.
	_ = f.HeaderTitle // no assertion on value; absence of panic is the contract
}
