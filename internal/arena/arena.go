// Package arena reads the scoring inputs kept in the repository: the arena
// list in config/arenas.json, one CSV of scores per arena under data/arenas/,
// and the candidate list in data/candidates.csv.
//
// Every join is by scoring.Key(moniker), never by address: validators rotate
// keys between chains, so an address matches almost nothing from one arena to
// the next.
package arena

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/louisb/score-validator/internal/scoring"
)

// Arena is one scored exercise. Its scores live in data/arenas/<ID>.csv.
type Arena struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Source string `json:"source"` // provenance, shown on the scoreboard
	Tab    string `json:"tab"`    // the spreadsheet tab freeze copies it from

	// The CSV headers differ from arena to arena because each was imported
	// its own way, so the config names the two columns that matter.
	MonikerColumn string `json:"moniker_column"`
	ScoreColumn   string `json:"score_column"`

	Colour  string `json:"colour"`
	InTotal bool   `json:"in_total"` // false: shown, not scored

	// Chain is the gnomonitoring chain id `fetch` reads this arena from.
	// Empty for an arena that is not an API chain.
	Chain string `json:"chain,omitempty"`

	// Detail names how the scorecard reads the rest of the CSV: the rubric
	// breakdown of a challenge or fire drill, or the onchain metrics of a
	// network. Empty means the arena publishes its score alone.
	Detail string `json:"detail,omitempty"`
}

// The detail kinds an arena can carry.
const (
	DetailChallenge = "challenge"
	DetailFireDrill = "firedrill"
	DetailOnchain   = "onchain"
)

// MainnetID is the config entry for the mainnet chain. It is shown on the
// page's own mainnet section, beside the scorecard and never in a total, so
// LoadConfig moves it out of Arenas into Config.Mainnet.
const MainnetID = "mainnet"

// Config is config/arenas.json.
type Config struct {
	Arenas []Arena `json:"arenas"`

	// Mainnet is the MainnetID entry, or nil. It is not one of Arenas: it
	// has no component, no tab and no weight.
	Mainnet *Arena `json:"-"`
}

// reservedIDs are the page's own sections. An arena named after one would get
// the same nav entry and section id, and the page would open the wrong one.
// MainnetID is the exception: its entry feeds the mainnet section itself.
var reservedIDs = map[string]bool{"overview": true, "profile": true, "github": true}

// LoadConfig reads and checks config/arenas.json.
func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.takeMainnet(path); err != nil {
		return c, err
	}
	seen := map[string]bool{}
	chains := map[string]bool{}
	if c.Mainnet != nil {
		chains[c.Mainnet.Chain] = true
	}
	for i, a := range c.Arenas {
		for field, v := range map[string]string{
			"id": a.ID, "label": a.Label, "tab": a.Tab,
			"moniker_column": a.MonikerColumn, "score_column": a.ScoreColumn,
		} {
			if strings.TrimSpace(v) == "" {
				return c, fmt.Errorf("%s: arena %d has no %s", path, i+1, field)
			}
		}
		switch a.Detail {
		case "", DetailChallenge, DetailFireDrill, DetailOnchain:
		default:
			return c, fmt.Errorf("%s: arena %q has unknown detail %q", path, a.ID, a.Detail)
		}
		if reservedIDs[a.ID] {
			return c, fmt.Errorf("%s: arena id %q is taken by a page of the scorecard", path, a.ID)
		}
		if seen[a.ID] {
			return c, fmt.Errorf("%s: arena %q is listed twice", path, a.ID)
		}
		seen[a.ID] = true
		if a.Chain != "" {
			// fetch picks the arena by chain; two would be a coin toss.
			if chains[a.Chain] {
				return c, fmt.Errorf("%s: chain %q is fetched by two arenas", path, a.Chain)
			}
			chains[a.Chain] = true
		}
	}
	if c.Divisor() == 0 {
		return c, fmt.Errorf("%s: no arena counts toward the total", path)
	}
	return c, nil
}

// takeMainnet moves the MainnetID entry out of Arenas. It must be shown, not
// scored, and fetched from a chain with its onchain metrics, since the
// mainnet section is a gnomonitoring report and nothing else.
func (c *Config) takeMainnet(path string) error {
	kept := c.Arenas[:0]
	for _, a := range c.Arenas {
		if a.ID != MainnetID {
			kept = append(kept, a)
			continue
		}
		if c.Mainnet != nil {
			return fmt.Errorf("%s: arena %q is listed twice", path, a.ID)
		}
		switch {
		case a.InTotal:
			return fmt.Errorf("%s: %s is shown, not scored, and needs in_total false", path, a.ID)
		case strings.TrimSpace(a.Chain) == "":
			return fmt.Errorf("%s: %s has no chain", path, a.ID)
		case a.Detail != DetailOnchain:
			return fmt.Errorf("%s: %s needs detail %q", path, a.ID, DetailOnchain)
		case strings.TrimSpace(a.MonikerColumn) == "" || strings.TrimSpace(a.ScoreColumn) == "":
			return fmt.Errorf("%s: %s needs moniker_column and score_column", path, a.ID)
		}
		m := a
		c.Mainnet = &m
	}
	c.Arenas = kept
	return nil
}

// ByChain returns the arena, mainnet included, that fetch reads from chain.
func (c Config) ByChain(chain string) (Arena, bool) {
	if c.Mainnet != nil && c.Mainnet.Chain == chain {
		return *c.Mainnet, true
	}
	for _, a := range c.Arenas {
		if a.Chain != "" && a.Chain == chain {
			return a, true
		}
	}
	return Arena{}, false
}

// Divisor is the number of arenas that count toward the total. Each one
// carries equal weight, so adding an arena is one config entry plus its CSV,
// and the divisor follows on its own.
func (c Config) Divisor() int {
	n := 0
	for _, a := range c.Arenas {
		if a.InTotal {
			n++
		}
	}
	return n
}

// File is where an arena's scores live, under the data directory.
func (a Arena) File(dataDir string) string {
	return filepath.Join(dataDir, "arenas", a.ID+".csv")
}

// LoadScores reads an arena's CSV into scoring.Key(moniker) -> score.
//
// Rows with a blank moniker are skipped: a validator the monitor could not
// name matches no candidate. A moniker listed twice, under any case, is an
// error rather than a silent choice between two rows, and so is a score that
// is not a number.
func LoadScores(dataDir string, a Arena) (map[string]float64, error) {
	path := a.File(dataDir)
	header, rows, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	mi, si := column(header, a.MonikerColumn), column(header, a.ScoreColumn)
	if mi < 0 || si < 0 {
		return nil, fmt.Errorf("%s: needs columns %q and %q, has %v", path, a.MonikerColumn, a.ScoreColumn, header)
	}
	out := map[string]float64{}
	for n, r := range rows {
		moniker := field(r, mi)
		k := scoring.Key(moniker)
		if k == "" {
			continue
		}
		s, err := strconv.ParseFloat(field(r, si), 64)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %s scores %q, not a number", path, n+2, moniker, field(r, si))
		}
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("%s line %d: %s is listed twice", path, n+2, moniker)
		}
		out[k] = s
	}
	return out, nil
}

// Rows returns an arena's CSV data rows, header excluded, cells untrimmed.
// Rows may be shorter than the header.
func (a Arena) Rows(dataDir string) ([][]string, error) {
	_, rows, err := readCSV(a.File(dataDir))
	return rows, err
}

// Table returns an arena's CSV header and data rows, as Rows does.
func (a Arena) Table(dataDir string) ([]string, [][]string, error) {
	return readCSV(a.File(dataDir))
}

// Column returns the index of a header cell, or -1.
func Column(header []string, name string) int { return column(header, name) }

// Candidate is one row of data/candidates.csv: a validator in the scored
// window, and the Discord handle it signed up with.
type Candidate struct {
	Moniker string
	Discord string
}

// LoadCandidates reads data/candidates.csv, in file order.
func LoadCandidates(path string) ([]Candidate, error) {
	header, rows, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	mi, di := column(header, "moniker"), column(header, "discord")
	if mi < 0 || di < 0 {
		return nil, fmt.Errorf("%s: needs columns moniker and discord, has %v", path, header)
	}
	var out []Candidate
	seen := map[string]bool{}
	for n, r := range rows {
		c := Candidate{Moniker: field(r, mi), Discord: field(r, di)}
		k := scoring.Key(c.Moniker)
		if k == "" {
			return nil, fmt.Errorf("%s line %d: blank moniker", path, n+2)
		}
		if seen[k] {
			return nil, fmt.Errorf("%s line %d: %s is listed twice", path, n+2, c.Moniker)
		}
		seen[k] = true
		out = append(out, c)
	}
	return out, nil
}

// readCSV returns the header and the data rows. Rows may be shorter than the
// header: the spreadsheet export drops trailing empty cells.
func readCSV(path string) ([]string, [][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(rows) < 2 {
		return nil, nil, fmt.Errorf("%s: no data rows", path)
	}
	return rows[0], rows[1:], nil
}

func column(header []string, name string) int {
	for i, h := range header {
		if strings.TrimSpace(h) == name {
			return i
		}
	}
	return -1
}

func field(row []string, i int) string {
	if i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}
