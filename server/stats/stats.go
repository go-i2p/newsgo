// Package newsstats tracks per-language su3 download counts and persists them
// to a JSON file. All exported methods are safe for concurrent use.
package newsstats

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/wcharczuk/go-chart/v2"
)

// NewsStats tracks per-language download counts and can persist them to a
// JSON file. All exported methods are safe for concurrent use: reads hold a
// shared read-lock while writes hold the exclusive write-lock.
type NewsStats struct {
	// mu protects DownloadLangs. It must not be copied after first use.
	mu            sync.RWMutex
	DownloadLangs map[string]int
	StateFile     string
}

// Graph renders a bar chart of per-language download counts as SVG into rw.
func (n *NewsStats) Graph(rw http.ResponseWriter) {
	n.mu.RLock()
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
	n.mu.RUnlock()
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

// Increment records one su3 download. The lang query parameter selects the
// language bucket; requests with no lang value are counted under "en_US".
// Safe for concurrent use.
func (n *NewsStats) Increment(rq *http.Request) {
	q := rq.URL.Query()
	lang := q.Get("lang")
	if lang == "" {
		lang = "en_US"
	}
	n.mu.Lock()
	n.DownloadLangs[lang]++
	n.mu.Unlock()
}

// Save persists the current download counts to StateFile as JSON.
// Safe for concurrent use: it holds a read lock while serialising.
func (n *NewsStats) Save() error {
	n.mu.RLock()
	data, err := json.Marshal(n.DownloadLangs)
	n.mu.RUnlock()
	if err != nil {
		return err
	}
	if err := os.WriteFile(n.StateFile, data, 0o644); err != nil {
		return err
	}
	return nil
}

// Load reads persisted download stats from StateFile. It is safe under all
// failure modes: missing file, malformed JSON, and a file containing the JSON
// value "null" (which would otherwise unmarshal successfully into a nil map,
// causing a panic on the next Increment call).
//
// Load is typically called once during initialisation; the write lock ensures
// safety if Load and Increment are ever called concurrently.
func (n *NewsStats) Load() {
	n.mu.Lock()
	defer n.mu.Unlock()

	data, err := os.ReadFile(n.StateFile)
	if err != nil {
		// File missing or unreadable — start with an empty map.
		n.DownloadLangs = make(map[string]int)
		return
	}
	if err := json.Unmarshal(data, &n.DownloadLangs); err != nil {
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
