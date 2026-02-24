package newsstats

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/wcharczuk/go-chart/v2"
)

type NewsStats struct {
	DownloadLangs map[string]int
	StateFile     string
}

func (n *NewsStats) Graph(rw http.ResponseWriter) {
	bars := []chart.Value{
		{Value: float64(0), Label: "baseline"},
	}
	log.Println("Graphing")
	total := 0
	for k, v := range n.DownloadLangs {
		log.Printf("Label: %s Value: %d", k, v)
		total += v
		bars = append(bars, chart.Value{Value: float64(v), Label: k})
	}
	bars = append(bars, chart.Value{Value: float64(total), Label: "Total Requests / Approx. Updates Handled"})

	graph := chart.BarChart{
		Title: "Downloads by language",
		Background: chart.Style{
			Padding: chart.Box{
				Top:   40,
				Left:  10,
				Right: 10,
			},
		},
		Height:   256,
		BarWidth: 20,
		Bars:     bars,
	}
	err := graph.Render(chart.SVG, rw)
	if err != nil {
		log.Println("Graph: error", err)
	}
}

func (n *NewsStats) Increment(rq *http.Request) {
	q := rq.URL.Query()
	lang := q.Get("lang")
	if lang != "" {
		n.DownloadLangs[lang]++
	} else {
		n.DownloadLangs["en_US"]++
	}
}

func (n *NewsStats) Save() error {
	bytes, err := json.Marshal(n.DownloadLangs)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(n.StateFile, bytes, 0o644); err != nil {
		return err
	}
	return nil
}

// Load reads persisted download stats from StateFile. It is safe under all
// failure modes: missing file, malformed JSON, and a file containing the JSON
// value "null" (which would otherwise unmarshal successfully into a nil map,
// causing a panic on the next Increment call).
func (n *NewsStats) Load() {
	bytes, err := ioutil.ReadFile(n.StateFile)
	if err != nil {
		// File missing or unreadable — start with an empty map.
		n.DownloadLangs = make(map[string]int)
		return
	}
	if err := json.Unmarshal(bytes, &n.DownloadLangs); err != nil {
		// Malformed JSON — start with an empty map.
		n.DownloadLangs = make(map[string]int)
		return
	}
	// A stats file containing the JSON value "null" unmarshals successfully
	// but sets DownloadLangs to nil, which panics on the next map write.
	if n.DownloadLangs == nil {
		n.DownloadLangs = make(map[string]int)
	}
}
