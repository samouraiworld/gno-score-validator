package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/scoring"
)

// TestParityWithRepoCSVs recomputes every total from the CSVs in git alone
// and checks it against data/scorecard.json. It is the acceptance test for
// moving the scoring out of the Sheet: as long as it passes, the repo holds
// everything the scores are made of.
//
// The rules mirror Bilan: candidates are data/candidates.csv, every join is by
// scoring.Key, and a moniker absent from an arena scores 0 there. The
// uncorrected GitHub score comes from data/github/evidence.json, whose blocks
// must also match the ones scorecard.json publishes byte for byte.
func TestParityWithRepoCSVs(t *testing.T) {
	data := filepath.Join("..", "..", "data")
	cfg, err := arena.LoadConfig(filepath.Join("..", "..", "config", "arenas.json"))
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(data, "scorecard.json"))
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
	for _, a := range cfg.Arenas {
		if _, ok := componentSetters[a.ID]; !ok {
			t.Fatalf("arena %q has no scorecard component", a.ID)
		}
		byKey, err := arena.LoadScores(data, a)
		if err != nil {
			t.Fatal(err)
		}
		scores[a.ID] = byKey
	}

	evidence, err := loadEvidence(filepath.Join(data, evidenceFile))
	if err != nil {
		t.Fatal(err)
	}
	published := 0
	for _, v := range snap.Validators {
		if len(v.Github) > 0 {
			published++
		}
	}
	if published != len(evidence) {
		t.Errorf("scorecard.json publishes %d github blocks, %s holds %d", published, evidenceFile, len(evidence))
	}

	links, err := scoring.LoadLinks(filepath.Join(data, "github-links.csv"))
	if err != nil {
		t.Fatal(err)
	}

	candidates, err := arena.LoadCandidates(filepath.Join(data, "candidates.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != len(snap.Validators) {
		t.Errorf("candidates.csv has %d candidates, scorecard.json %d", len(candidates), len(snap.Validators))
	}
	for _, cand := range candidates {
		moniker := cand.Moniker
		k := scoring.Key(moniker)
		want, ok := stored[k]
		if !ok {
			t.Errorf("%s: in candidates.csv, missing from scorecard.json", moniker)
			continue
		}

		var c snapComponents
		for _, a := range cfg.Arenas {
			componentSetters[a.ID](&c, scores[a.ID][k])
		}
		if cand.Discord != want.Discord {
			t.Errorf("%s: discord %q in candidates.csv, scorecard.json has %q", moniker, cand.Discord, want.Discord)
		}
		if c != want.Components {
			t.Errorf("%s: components %+v, scorecard.json has %+v", moniker, c, want.Components)
		}

		corrected := links[k].Score
		current := corrected
		if block, ok := evidence[k]; ok {
			var a, b bytes.Buffer
			if err := json.Compact(&a, block); err != nil {
				t.Fatalf("%s: evidence block: %v", moniker, err)
			}
			if err := json.Compact(&b, want.Github); err != nil {
				t.Fatalf("%s: scorecard github block: %v", moniker, err)
			}
			if !bytes.Equal(a.Bytes(), b.Bytes()) {
				t.Errorf("%s: github block in scorecard.json differs from %s", moniker, evidenceFile)
			}
			var g struct {
				AllTime map[string]float64 `json:"all_time"`
			}
			if err := json.Unmarshal(block, &g); err != nil {
				t.Fatalf("%s: github block: %v", moniker, err)
			}
			current = scoring.GithubScore(g.AllTime)
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
