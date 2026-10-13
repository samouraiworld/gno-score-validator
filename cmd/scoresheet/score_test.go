package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/scoring"
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

func TestAddingArenaFlowsThroughScorecardAndRender(t *testing.T) {
	source := filepath.Join("..", "..")
	root := t.TempDir()
	for _, dir := range []string{"config", "data"} {
		if err := copyTestTree(filepath.Join(source, dir), filepath.Join(root, dir)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(source, "web", "validator-scorecard.html"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "web", "validator-scorecard.html"), page, 0o644); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(root, "config", "arenas.json")
	cfg, err := arena.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Arenas = append(cfg.Arenas, arena.Arena{
		ID: "extra", Label: "Extra arena", Source: "test fixture", Tab: "Extra",
		MonikerColumn: "moniker", ScoreColumn: "score", Colour: "#123456", InTotal: true,
	})
	config, err := json.MarshalIndent(cfg, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, append(config, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "arenas", "extra.csv"), []byte("moniker,score\nCogwheel,70\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runScore(root, nil); err != nil {
		t.Fatal(err)
	}
	got, err := loadSnapshot(filepath.Join(root, "data", "scorecard.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Arenas) != 7 || got.Arenas[6].ID != "extra" {
		t.Fatalf("arenas = %+v, want extra as the seventh", got.Arenas)
	}
	var weights map[string]float64
	if err := json.Unmarshal(got.Weights, &weights); err != nil {
		t.Fatal(err)
	}
	for _, a := range got.Arenas {
		if a.InTotal && weights[a.ID] != 1.0/7.0 {
			t.Errorf("weight[%s] = %g, want 1/7", a.ID, weights[a.ID])
		}
	}
	var cogwheel snapValidator
	for _, v := range got.Validators {
		if v.Moniker == "Cogwheel" {
			cogwheel = v
			break
		}
	}
	if cogwheel.Components.Get("extra") != 70 {
		t.Fatalf("extra component = %g, want 70", cogwheel.Components.Get("extra"))
	}
	var counted []float64
	for _, a := range got.Arenas {
		if a.InTotal {
			counted = append(counted, cogwheel.Components.Get(a.ID))
		}
	}
	if want := scoring.Total(counted, cogwheel.GithubCorrected); absDiff(cogwheel.ScoreCorrected, want) > 1e-9 {
		t.Errorf("total = %g, want %g from seven arenas", cogwheel.ScoreCorrected, want)
	}
	index, err := os.ReadFile(filepath.Join(root, scoreboardDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(index, []byte("Extra arena")) || !bytes.Contains(index, []byte(") / 7 + GitHub")) {
		t.Errorf("scoreboard does not contain the added arena and /7 formula")
	}
}

func copyTestTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		to := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		if strings.HasSuffix(entry.Name(), ".tmp") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(to, data, 0o644)
	})
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
