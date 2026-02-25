// Package newsbuilder assembles I2P Atom news feed XML documents from HTML
// entry sources, a release JSON descriptor, and an optional blocklist fragment.
package newsbuilder

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"time"

	newsfeed "github.com/go-i2p/newsgo/builder/feed"
	"github.com/yosssi/gohtml"
)

// NewsBuilder holds the configuration required to assemble an I2P Atom news
// feed. Feed provides the HTML entry source; the remaining fields control the
// XML metadata emitted in the feed header and release element.
type NewsBuilder struct {
	Feed         newsfeed.Feed
	Language     string // BCP 47 tag, e.g. "de", "zh-TW"; defaults to "en" when empty
	ReleasesJson string
	BlocklistXML string
	URNID        string
	TITLE        string
	SITEURL      string
	MAINFEED     string
	BACKUPFEED   string
	SUBTITLE     string
}

// xmlEsc returns s with XML-special characters replaced by their standard
// entity references, making the value safe for XML text content and attribute
// values.  encoding/xml.EscapeText is the canonical implementation: it handles
// &, <, >, ", and carriage return.
func xmlEsc(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s)) //nolint:errcheck — bytes.Buffer.Write never returns an error
	return buf.String()
}

// jsonStr safely extracts a string value from a JSON map, returning a
// descriptive error if the key is absent or has a non-string type. This
// avoids the unrecovered panics that bare type assertions cause when the
// releases JSON is malformed or incomplete.
func jsonStr(m map[string]interface{}, key string) (string, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", fmt.Errorf("JSONtoXML: missing field %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("JSONtoXML: field %q is not a string (got %T)", key, v)
	}
	return s, nil
}

// parseReleasesJSON reads the JSON file at path, decodes it as an array of
// release objects, and returns the first element. An error is returned when
// the file cannot be read, the content is not valid JSON, or the array is empty.
func parseReleasesJSON(path string) (map[string]interface{}, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload []map[string]interface{}
	if err = json.Unmarshal(content, &payload); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("JSONtoXML: releases JSON array is empty")
	}
	return payload[0], nil
}

// extractReleaseMetadata retrieves the four required scalar string fields
// from a release JSON object: date, version, minVersion, and minJavaVersion.
// It returns a descriptive error if any field is absent or has a non-string type.
func extractReleaseMetadata(release map[string]interface{}) (releasedate, version, minVersion, minJavaVersion string, err error) {
	releasedate, err = jsonStr(release, "date")
	if err != nil {
		return releasedate, version, minVersion, minJavaVersion, err
	}
	version, err = jsonStr(release, "version")
	if err != nil {
		return releasedate, version, minVersion, minJavaVersion, err
	}
	minVersion, err = jsonStr(release, "minVersion")
	if err != nil {
		return releasedate, version, minVersion, minJavaVersion, err
	}
	minJavaVersion, err = jsonStr(release, "minJavaVersion")
	return releasedate, version, minVersion, minJavaVersion, err
}

// navigateToSU3Map resolves the "updates"→"su3" path within a release JSON
// object and returns the su3 map. It returns a descriptive error if any
// intermediate field is absent or has an unexpected type.
func navigateToSU3Map(release map[string]interface{}) (map[string]interface{}, error) {
	updatesRaw, ok := release["updates"]
	if !ok || updatesRaw == nil {
		return nil, fmt.Errorf("JSONtoXML: missing field \"updates\"")
	}
	updatesMap, ok := updatesRaw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("JSONtoXML: field \"updates\" is not an object")
	}
	su3Raw, ok := updatesMap["su3"]
	if !ok || su3Raw == nil {
		return nil, fmt.Errorf("JSONtoXML: missing field \"updates.su3\"")
	}
	su3, ok := su3Raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("JSONtoXML: field \"updates.su3\" is not an object")
	}
	return su3, nil
}

// extractSU3Update retrieves the torrent magnet link and the list of download
// URL values from the su3 update section of a release JSON object. It returns
// a descriptive error if any expected field is absent or has an unexpected type.
func extractSU3Update(release map[string]interface{}) (magnet string, urlSlice []interface{}, err error) {
	su3, err := navigateToSU3Map(release)
	if err != nil {
		return magnet, urlSlice, err
	}
	magnet, err = jsonStr(su3, "torrent")
	if err != nil {
		return magnet, urlSlice, err
	}
	urlsRaw, ok := su3["url"]
	if !ok || urlsRaw == nil {
		return "", nil, fmt.Errorf("JSONtoXML: missing field \"updates.su3.url\"")
	}
	urlSlice, ok = urlsRaw.([]interface{})
	if !ok {
		return "", nil, fmt.Errorf("JSONtoXML: field \"updates.su3.url\" is not an array")
	}
	return magnet, urlSlice, err
}

// buildReleaseXML assembles the <i2p:release> XML fragment from validated
// release metadata and SU3 update fields. All string values are XML-escaped
// before insertion. An error is returned if any URL element in urlSlice is
// not a string.
func buildReleaseXML(releasedate, version, minVersion, minJavaVersion, magnet string, urlSlice []interface{}) (string, error) {
	// Attribute values are quoted and XML-escaped as required by the XML specification.
	str := "<i2p:release date=\"" + xmlEsc(releasedate) + "\" minVersion=\"" + xmlEsc(minVersion) + "\" minJavaVersion=\"" + xmlEsc(minJavaVersion) + "\">\n"
	str += "<i2p:version>" + xmlEsc(version) + "</i2p:version>"
	str += "<i2p:update type=\"su3\">"
	str += "<i2p:torrent href=\"" + xmlEsc(magnet) + "\"/>"
	for i, u := range urlSlice {
		us, ok := u.(string)
		if !ok {
			return "", fmt.Errorf("JSONtoXML: updates.su3.url[%d] is not a string", i)
		}
		str += "<i2p:url href=\"" + xmlEsc(us) + "\"/>"
	}
	str += "</i2p:update>"
	str += "</i2p:release>"
	return str, nil
}

// JSONtoXML reads the releases JSON file and returns the corresponding
// <i2p:release> XML fragment. All type assertions are guarded so that
// malformed input returns a descriptive error instead of panicking.
//
// Example output:
//
//	<i2p:release date="2022-11-21" minVersion="0.9.9" minJavaVersion="1.8">
//	  <i2p:version>2.0.0</i2p:version>
//	  <i2p:update type="su3">...</i2p:update>
//	</i2p:release>
func (nb *NewsBuilder) JSONtoXML() (string, error) {
	release, err := parseReleasesJSON(nb.ReleasesJson)
	if err != nil {
		return "", err
	}
	releasedate, version, minVersion, minJavaVersion, err := extractReleaseMetadata(release)
	if err != nil {
		return "", err
	}
	magnet, urlSlice, err := extractSU3Update(release)
	if err != nil {
		return "", err
	}
	return buildReleaseXML(releasedate, version, minVersion, minJavaVersion, magnet, urlSlice)
}

// validateBlocklistXML checks that content is a valid XML fragment suitable
// for embedding directly inside the <feed> element.  Two conditions are
// rejected:
//
//  1. A leading XML declaration (<?xml …?>).  The outer feed document already
//     carries one; two XML declarations in the same byte stream are forbidden
//     by the XML specification (§2.8).
//
//  2. Content that is not well-formed XML.  Broken fragments propagate
//     silently to every downstream consumer (feed readers, the su3 packager).
//
// An empty blocklist is allowed and produces no fragment in the output feed.
func validateBlocklistXML(content []byte) error {
	if len(content) == 0 {
		return nil
	}
	// Reject an embedded XML declaration before attempting to parse, since the
	// declaration is valid XML on its own but illegal inside a larger document.
	if bytes.HasPrefix(bytes.TrimSpace(content), []byte("<?xml")) {
		return fmt.Errorf("validateBlocklistXML: blocklist must not contain an XML declaration")
	}
	// Wrap in a namespace-aware root element so the XML decoder sees a single
	// well-formed document.  The i2p namespace prefix is declared here because
	// blocklist fragments commonly use <i2p:blocklist> and similar elements;
	// without the declaration the xml.Decoder would report an unbound prefix.
	wrapped := append([]byte(`<_root xmlns:i2p="http://geti2p.net/en/docs/spec/updates">`), content...)
	wrapped = append(wrapped, []byte(`</_root>`)...)
	dec := xml.NewDecoder(bytes.NewReader(wrapped))
	for {
		_, err := dec.Token()
		if err != nil && err.Error() == "EOF" {
			break
		}
		if err != nil {
			return fmt.Errorf("validateBlocklistXML: malformed XML fragment: %w", err)
		}
	}
	return nil
}

// buildFeedHeader constructs the Atom feed XML preamble for the given
// NewsBuilder and timestamp. It emits the XML declaration, <feed> opening tag,
// id, title, updated timestamp, link elements, generator, and subtitle.
//
// The xml:lang attribute is set from nb.Language; it defaults to "en" when
// nb.Language is empty to preserve backward-compatible output for callers that
// construct NewsBuilder directly without setting the Language field.
func buildFeedHeader(nb *NewsBuilder, currentTime time.Time) string {
	lang := nb.Language
	if lang == "" {
		lang = "en"
	}
	str := "<?xml version='1.0' encoding='UTF-8'?>"
	str += "<feed xmlns:i2p=\"http://geti2p.net/en/docs/spec/updates\" xmlns=\"http://www.w3.org/2005/Atom\" xml:lang=\"" + xmlEsc(lang) + "\">"
	str += "<id>" + "urn:uuid:" + xmlEsc(nb.URNID) + "</id>"
	str += "<title>" + xmlEsc(nb.TITLE) + "</title>"
	milli := currentTime.Nanosecond() / 1_000_000
	// No trailing newline: the \n was previously injected into the element text,
	// causing RFC-3339 parsers and strict Atom validators to reject the timestamp.
	t := fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02d.%03d+00:00",
		currentTime.Year(), currentTime.Month(), currentTime.Day(),
		currentTime.Hour(), currentTime.Minute(), currentTime.Second(), milli)
	str += "<updated>" + t + "</updated>"
	str += "<link href=\"" + xmlEsc(nb.SITEURL) + "\"/>"
	str += "<link href=\"" + xmlEsc(nb.MAINFEED) + "\" rel=\"self\"/>"
	if nb.BACKUPFEED != "" {
		str += "<link href=\"" + xmlEsc(nb.BACKUPFEED) + "\" rel=\"alternate\"/>"
	}
	str += "<generator uri=\"http://idk.i2p/newsgo\" version=\"0.1.0\">newsgo</generator>"
	str += "<subtitle>" + xmlEsc(nb.SUBTITLE) + "</subtitle>"
	return str
}

// readBlocklistContent reads the blocklist XML file at path. A missing file is
// treated as an empty blocklist and returns (nil, nil). Only unexpected I/O
// errors such as permission failures are propagated as errors.
func readBlocklistContent(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("Build: reading blocklist: %w", err)
	}
	return data, nil
}

// Build assembles a complete Atom feed XML document from the loaded HTML
// entries, blocklist, and release JSON, and returns it as a formatted string.
// An error is returned if the HTML cannot be loaded, the blocklist is invalid,
// or the release JSON cannot be parsed.
func (nb *NewsBuilder) Build() (string, error) {
	if err := nb.Feed.LoadHTML(); err != nil {
		return "", fmt.Errorf("Build: error %s", err.Error())
	}
	// Use UTC explicitly so the hardcoded +00:00 offset is always correct.
	// Dividing nanoseconds by 1,000,000 gives milliseconds (0-999); %03d
	// zero-pads to the 3-digit width required by RFC 3339.
	str := buildFeedHeader(nb, time.Now().UTC())
	blocklistBytes, err := readBlocklistContent(nb.BlocklistXML)
	if err != nil {
		return "", err
	}
	// Validate before splicing: a blocklist with an XML declaration or broken
	// markup would silently corrupt the output feed and every .su3 built from it.
	if err := validateBlocklistXML(blocklistBytes); err != nil {
		return "", fmt.Errorf("Build: %w", err)
	}
	str += string(blocklistBytes)
	jsonxml, err := nb.JSONtoXML()
	if err != nil {
		return "", err
	}
	str += jsonxml
	for index := range nb.Feed.ArticlesSet {
		art := nb.Feed.Article(index)
		str += art.Entry()
	}
	str += "</feed>"
	return gohtml.Format(str), nil
}

// Builder returns a *NewsBuilder configured with sensible defaults for the I2P
// news feed.  newsFile is the path to the entries HTML source, releasesJson is
// the path to the releases JSON file, and blocklistXML is the optional path to
// an additional XML blocklist fragment (empty string disables it).
//
// URNID is intentionally left as the zero value (empty string) so that callers
// own exactly one UUID-generation call.  Callers MUST set URNID before calling
// Build(); the cmd layer handles this by honouring the --feeduri flag or
// generating a fresh uuid.NewString() precisely once per feed.
func Builder(newsFile, releasesJson, blocklistXML string) *NewsBuilder {
	nb := &NewsBuilder{
		Feed: newsfeed.Feed{
			EntriesHTMLPath: newsFile,
		},
		ReleasesJson: releasesJson,
		BlocklistXML: blocklistXML,
		// URNID is deliberately not set here; see function-level comment above.
		TITLE:      "I2P News",
		SITEURL:    "http://i2p-projekt.i2p",
		MAINFEED:   "http://tc73n4kivdroccekirco7rhgxdg5f3cjvbaapabupeyzrqwv5guq.b32.i2p/news.atom.xml",
		BACKUPFEED: "http://dn3tvalnjz432qkqsvpfdqrwpqkw3ye4n4i2uyfr4jexvo3sp5ka.b32.i2p/news/news.atom.xml",
		SUBTITLE:   "News feed, and router updates",
	}
	return nb
}
