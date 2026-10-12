package main

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/louisb/score-validator/internal/scoring"
)

// parityArenas maps each component to its frozen CSV and the columns holding
// the moniker and the score. The headers differ from tab to tab because each
// arena was imported its own way.
var parityArenas = []struct {
	name, file, moniker, score string
	set                        func(*snapComponents, float64)
}{
	{"challenge", "test13-challenge.csv", "Moniker", "Score", func(c *snapComponents, s float64) { c.Challenge = s }},
	{"test13", "score-test13.csv", "moniker", "Score", func(c *snapComponents, s float64) { c.Test13 = s }},
	{"topaz", "score-topaz.csv", "moniker", "Score", func(c *snapComponents, s float64) { c.Topaz = s }},
	{"sapphire", "score-sapphire.csv", "moniker", "current_year_score", func(c *snapComponents, s float64) { c.Sapphire = s }},
	{"pearl", "score-pearl.csv", "moniker", "current_year_score", func(c *snapComponents, s float64) { c.Pearl = s }},
	{"firedrill", "score-firedrill.csv", "moniker", "score", func(c *snapComponents, s float64) { c.FireDrill = s }},
}

// readCSV returns the rows of a CSV file as maps keyed by header.
func readCSV(t *testing.T, path string) []map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// freeze drops trailing empty cells, so rows can be shorter than the header.
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(rows) < 2 {
		t.Fatalf("%s: no data rows", path)
	}
	var out []map[string]string
	for _, r := range rows[1:] {
		m := map[string]string{}
		for i, h := range rows[0] {
			if i < len(r) {
				m[h] = r[i]
			}
		}
		out = append(out, m)
	}
	return out
}

// TestParityWithFrozenCSVs recomputes every total from the CSVs in git alone
// and checks it against data/scorecard.json. It is the acceptance test for
// moving the scoring out of the Sheet: as long as it passes, the repo holds
// everything the scores are made of.
//
// The rules mirror Bilan: candidates are Bilan's column A, every join is by
// scoring.Key, and a moniker absent from an arena scores 0 there. The
// uncorrected GitHub score still comes from the github blocks of
// scorecard.json, the one input that lives nowhere else yet.
func TestParityWithFrozenCSVs(t *testing.T) {
	frozen := filepath.Join("..", "..", "data", "frozen")

	raw, err := os.ReadFile(filepath.Join("..", "..", "data", "scorecard.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	stored := map[string]snapValidator{}
	for _, v := range snap.Validators {
		stored[scoring.Key(v.Moniker)] = v
	}

	scores := map[string]map[string]float64{}
	for _, a := range parityArenas {
		byKey := map[string]float64{}
		for _, r := range readCSV(t, filepath.Join(frozen, a.file)) {
			k := scoring.Key(r[a.moniker])
			if k == "" {
				continue
			}
			s, err := strconv.ParseFloat(strings.TrimSpace(r[a.score]), 64)
			if err != nil {
				t.Errorf("%s: %s: score %q is not a number", a.file, r[a.moniker], r[a.score])
				continue
			}
			if _, dup := byKey[k]; dup {
				t.Errorf("%s: moniker %q appears twice", a.file, r[a.moniker])
				continue
			}
			byKey[k] = s
		}
		scores[a.name] = byKey
	}

	links, err := scoring.LoadLinks(filepath.Join("..", "..", "data", "github-links.csv"))
	if err != nil {
		t.Fatal(err)
	}

	candidates := readCSV(t, filepath.Join(frozen, "bilan.csv"))
	if len(candidates) != len(snap.Validators) {
		t.Errorf("bilan.csv has %d candidates, scorecard.json %d", len(candidates), len(snap.Validators))
	}
	seen := map[string]bool{}
	for _, r := range candidates {
		moniker := r["Moniker"]
		k := scoring.Key(moniker)
		if seen[k] {
			t.Errorf("bilan.csv: moniker %q appears twice", moniker)
			continue
		}
		seen[k] = true
		want, ok := stored[k]
		if !ok {
			t.Errorf("%s: in bilan.csv, missing from scorecard.json", moniker)
			continue
		}

		var c snapComponents
		for _, a := range parityArenas {
			a.set(&c, scores[a.name][k])
		}
		if c != want.Components {
			t.Errorf("%s: components %+v, scorecard.json has %+v", moniker, c, want.Components)
		}

		corrected := links[k].Score
		current := corrected
		if len(want.Github) > 0 {
			var g struct {
				AllTime map[string]float64 `json:"all_time"`
			}
			if err := json.Unmarshal(want.Github, &g); err != nil {
				t.Fatalf("%s: github block: %v", moniker, err)
			}
			current = githubScore(g.AllTime)
		}
		checks := []struct {
			field     string
			got, want float64
		}{
			{"github_corrected", corrected, want.GithubCorrected},
			{"github_current", current, want.GithubCurrent},
			{"score_corrected", total(c, corrected), want.ScoreCorrected},
			{"score_current", total(c, current), want.ScoreCurrent},
		}
		for _, ch := range checks {
			if absDiff(ch.got, ch.want) > 1e-9 {
				t.Errorf("%s: %s = %g from the CSVs, scorecard.json has %g", moniker, ch.field, ch.got, ch.want)
			}
		}
	}
}
