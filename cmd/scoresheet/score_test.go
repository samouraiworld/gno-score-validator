package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestScoreReproducesCommittedScorecard is the acceptance test for `score`:
// computed from config/ and data/ alone, the scorecard must come out byte for
// byte as committed. Only generated_at is taken from the committed file, since
// it is the day the command runs.
func TestScoreReproducesCommittedScorecard(t *testing.T) {
	root := filepath.Join("..", "..")
	path := filepath.Join(root, "data", "scorecard.json")
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	prev, err := loadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	next, err := buildScorecard(root, prev)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(next, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if !bytes.Equal(got, committed) {
		out := filepath.Join(t.TempDir(), "scorecard.json")
		if err := os.WriteFile(out, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Errorf("score output differs from %s; diff it against %s", path, out)
	}
}

// A respelt evidence block would otherwise vanish from the page silently.
func TestBuildScorecardRefusesOrphanEvidence(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("config/arenas.json", `{"arenas":[{"id":"pearl","label":"P","tab":"T","moniker_column":"moniker","score_column":"score","in_total":true}]}`)
	write("data/arenas/pearl.csv", "moniker,score\nalpha,50\n")
	write("data/candidates.csv", "moniker,discord\nalpha,a\n")
	write("data/github-links.csv", "moniker,discord,github_login,confidence,github_score\nalpha,a,al,certain,10\n")
	write("data/github/evidence.json", `{"ghost":{"all_time":{"commits":1}}}`)
	if _, err := buildScorecard(root, snapshot{}); err == nil {
		t.Error("accepted an evidence block matching no candidate")
	}
}
