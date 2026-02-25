// Package newsfeed parses HTML news entry files and exposes their content as
// Article values suitable for embedding in an Atom feed.
package newsfeed

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"

	"github.com/anaskhan96/soup"
)

// xmlEsc returns s with XML-special characters replaced by their standard
// entity references, making the value safe for XML text content and attribute
// values.  encoding/xml.EscapeText is the canonical implementation: it handles
// &, <, >, ", and carriage return.
func xmlEsc(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s)) //nolint:errcheck — bytes.Buffer.Write never returns an error
	return buf.String()
}

// Feed parses an HTML entries file and exposes its <article> elements as
// individual Article values for use by NewsBuilder.
type Feed struct {
	HeaderTitle         string
	ArticlesSet         []string
	EntriesHTMLPath     string
	BaseEntriesHTMLPath string
	doc                 soup.Root
}

// LoadHTML reads the HTML file at EntriesHTMLPath, extracts the <header> title
// and all <article> elements into ArticlesSet. If BaseEntriesHTMLPath is also
// set, that file is read and its articles are appended after the primary set.
func (f *Feed) LoadHTML() error {
	data, err := os.ReadFile(f.EntriesHTMLPath)
	if err != nil {
		return fmt.Errorf("LoadHTML: error %s", err)
	}
	f.doc = soup.HTMLParse(string(data))
	f.HeaderTitle = f.doc.Find("header").FullText()
	articles := f.doc.FindAll("article")
	for _, article := range articles {
		f.ArticlesSet = append(f.ArticlesSet, article.HTML())
	}
	if f.BaseEntriesHTMLPath != "" {
		data, err := os.ReadFile(f.BaseEntriesHTMLPath)
		if err != nil {
			return fmt.Errorf("LoadHTML: error %s", err)
		}
		f.doc = soup.HTMLParse(string(data))
		f.HeaderTitle = f.doc.Find("header").FullText()
		articles := f.doc.FindAll("article")
		for _, article := range articles {
			f.ArticlesSet = append(f.ArticlesSet, article.HTML())
		}
	}
	return nil
}

// Length returns the number of articles loaded from the entries HTML.
func (f *Feed) Length() int {
	return len(f.ArticlesSet)
}

// Article parses the HTML of ArticlesSet[index] and returns a new Article
// populated with the attributes and summary text of that element.
func (f *Feed) Article(index int) *Article {
	html := soup.HTMLParse(f.ArticlesSet[index])
	articleData := html.Find("article").Attrs()
	articleSummary := html.Find("details").Find("summary").FullText()
	return &Article{
		UID:           articleData["id"],
		Title:         articleData["title"],
		Link:          articleData["href"],
		Author:        articleData["author"],
		PublishedDate: articleData["published"],
		UpdatedDate:   articleData["updated"],
		Summary:       articleSummary,
		content:       html.HTML(),
	}
}

// Article holds the metadata and HTML content of a single Atom feed entry,
// extracted from an <article> element in the entries HTML source.
type Article struct {
	UID           string
	Title         string
	Link          string
	Author        string
	PublishedDate string
	UpdatedDate   string
	Summary       string
	// content holds the raw HTML of the article element as parsed from the entries HTML source.
	// Content() extracts the body by skipping the wrapping <article> and <details>/<summary> nodes.
	content string
}

// Content returns the HTML body of the article, skipping the first 5 soup
// nodes which correspond to the wrapping <article> and <details>/<summary>
// elements. Guards against short or minimal articles that produce fewer nodes.
func (a *Article) Content() string {
	str := ""
	doc := soup.HTMLParse(string(a.content))
	articleBody := doc.FindAll("")
	if len(articleBody) <= 5 {
		return str
	}
	for _, v := range articleBody[5:] {
		str += v.HTML()
	}
	return str
}

// Entry renders the Article as an Atom <entry> XML fragment. All metadata
// fields are XML-escaped; the XHTML body from Content() is embedded verbatim
// inside a <content type="xhtml"> element and must not be double-escaped.
func (a *Article) Entry() string {
	// All text and attribute values are XML-escaped via xmlEsc so that special
	// characters such as '&' in URLs (?a=1&b=2) or '<' in titles do not
	// produce malformed XML.  Content() returns raw XHTML embedded inside
	// <content type="xhtml"> and must NOT be escaped — it is parsed as markup.
	return fmt.Sprintf(
		"<entry>\n\t<id>%s</id>\n\t<title>%s</title>\n\t<updated>%s</updated>\n\t<author><name>%s</name></author>\n\t<link href=\"%s\" rel=\"alternate\"/>\n\t<published>%s</published>\n\t<summary>%s</summary>\n\t<content type=\"xhtml\">\n\t\t<div xmlns=\"http://www.w3.org/1999/xhtml\">\n\t\t%s\n\t\t</div>\n\t</content>\n</entry>",
		xmlEsc(a.UID),
		xmlEsc(a.Title),
		xmlEsc(a.UpdatedDate),
		xmlEsc(a.Author),
		xmlEsc(a.Link),
		xmlEsc(a.PublishedDate),
		xmlEsc(a.Summary),
		a.Content(), // raw XHTML — embedded markup, must not be double-escaped
	)
}
