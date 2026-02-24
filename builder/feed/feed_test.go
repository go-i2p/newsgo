package newsfeed

import (
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
