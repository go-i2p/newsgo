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

// TestContent_ShortArticle verifies that Content() does not panic when the
// parsed HTML yields fewer than 5 nodes. The old code sliced articleBody[5:]
// unconditionally, causing a runtime panic on minimal articles.
func TestContent_ShortArticle(t *testing.T) {
	a := &Article{content: "<article><p>Hi</p></article>"}
	got := a.Content() // must not panic
	_ = got
}

// TestContent_NormalArticle verifies that Content() does not panic for a
// normal article that produces more than 5 soup nodes.
func TestContent_NormalArticle(t *testing.T) {
	a := &Article{content: `<article id="x" title="T" href="" author="A" published="" updated="">
<details><summary>Summary</summary></details>
<p>First paragraph</p>
<p>Second paragraph</p>
</article>`}
	got := a.Content() // must not panic
	_ = got
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
