package newsbuilder

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"time"

	newsfeed "github.com/go-i2p/newsgo/builder/feed"
	"github.com/google/uuid"
	"github.com/yosssi/gohtml"
)

type NewsBuilder struct {
	Feed         newsfeed.Feed
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
	content, err := os.ReadFile(nb.ReleasesJson)
	if err != nil {
		return "", err
	}
	var payload []map[string]interface{}
	if err = json.Unmarshal(content, &payload); err != nil {
		return "", err
	}
	if len(payload) == 0 {
		return "", fmt.Errorf("JSONtoXML: releases JSON array is empty")
	}
	release := payload[0]

	releasedate, err := jsonStr(release, "date")
	if err != nil {
		return "", err
	}
	version, err := jsonStr(release, "version")
	if err != nil {
		return "", err
	}
	minVersion, err := jsonStr(release, "minVersion")
	if err != nil {
		return "", err
	}
	minJavaVersion, err := jsonStr(release, "minJavaVersion")
	if err != nil {
		return "", err
	}

	updatesRaw, ok := release["updates"]
	if !ok || updatesRaw == nil {
		return "", fmt.Errorf("JSONtoXML: missing field \"updates\"")
	}
	updatesMap, ok := updatesRaw.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("JSONtoXML: field \"updates\" is not an object")
	}
	su3Raw, ok := updatesMap["su3"]
	if !ok || su3Raw == nil {
		return "", fmt.Errorf("JSONtoXML: missing field \"updates.su3\"")
	}
	su3, ok := su3Raw.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("JSONtoXML: field \"updates.su3\" is not an object")
	}

	magnet, err := jsonStr(su3, "torrent")
	if err != nil {
		return "", err
	}
	urlsRaw, ok := su3["url"]
	if !ok || urlsRaw == nil {
		return "", fmt.Errorf("JSONtoXML: missing field \"updates.su3.url\"")
	}
	urlSlice, ok := urlsRaw.([]interface{})
	if !ok {
		return "", fmt.Errorf("JSONtoXML: field \"updates.su3.url\" is not an array")
	}

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

func (nb *NewsBuilder) Build() (string, error) {
	if err := nb.Feed.LoadHTML(); err != nil {
		return "", fmt.Errorf("Build: error %s", err.Error())
	}
	// Use UTC explicitly so the hardcoded +00:00 offset is always correct.
	// Dividing nanoseconds by 1,000,000 gives milliseconds (0-999); %03d
	// zero-pads to the 3-digit width required by RFC 3339.
	currentTime := time.Now().UTC()
	str := "<?xml version='1.0' encoding='UTF-8'?>"
	str += "<feed xmlns:i2p=\"http://geti2p.net/en/docs/spec/updates\" xmlns=\"http://www.w3.org/2005/Atom\" xml:lang=\"en\">"
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
	blocklistBytes, err := os.ReadFile(nb.BlocklistXML)
	if err != nil {
		return "", err
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

func Builder(newsFile, releasesJson, blocklistXML string) *NewsBuilder {
	nb := &NewsBuilder{
		Feed: newsfeed.Feed{
			EntriesHTMLPath: newsFile,
		},
		ReleasesJson: releasesJson,
		BlocklistXML: blocklistXML,
		URNID:        uuid.New().String(),
		TITLE:        "I2P News",
		SITEURL:      "http://i2p-projekt.i2p",
		MAINFEED:     "http://tc73n4kivdroccekirco7rhgxdg5f3cjvbaapabupeyzrqwv5guq.b32.i2p/news.atom.xml",
		BACKUPFEED:   "http://dn3tvalnjz432qkqsvpfdqrwpqkw3ye4n4i2uyfr4jexvo3sp5ka.b32.i2p/news/news.atom.xml",
		SUBTITLE:     "News feed, and router updates",
	}
	return nb
}
