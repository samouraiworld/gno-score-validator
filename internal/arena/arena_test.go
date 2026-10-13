package arena

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/louisb/score-validator/internal/scoring"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The committed config must agree with the divisor the sheet formula still
// uses, until the formula is generated from the config too.
func TestRepoConfigMatchesTheScoringRule(t *testing.T) {
	c, err := LoadConfig(filepath.Join("..", "..", "config", "arenas.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Divisor() != scoring.ArenaDivisor {
		t.Errorf("config divides by %d, scoring.ArenaDivisor is %d", c.Divisor(), scoring.ArenaDivisor)
	}
	data := filepath.Join("..", "..", "data")
	for _, a := range c.Arenas {
		if _, err := LoadScores(data, a); err != nil {
			t.Error(err)
		}
	}
	if _, err := LoadCandidates(filepath.Join(data, "candidates.csv")); err != nil {
		t.Error(err)
	}
}

func TestLoadConfigRejects(t *testing.T) {
	ok := `{"id":"a","label":"A","tab":"T","moniker_column":"m","score_column":"s","in_total":true}`
	for name, body := range map[string]string{
		"duplicate id":   `{"arenas":[` + ok + `,` + ok + `]}`,
		"missing label":  `{"arenas":[{"id":"a","tab":"T","moniker_column":"m","score_column":"s","in_total":true}]}`,
		"nothing total":  `{"arenas":[{"id":"a","label":"A","tab":"T","moniker_column":"m","score_column":"s"}]}`,
		"unknown detail": `{"arenas":[{"id":"a","label":"A","tab":"T","moniker_column":"m","score_column":"s","in_total":true,"detail":"x"}]}`,
	} {
		path := filepath.Join(t.TempDir(), "arenas.json")
		write(t, path, body)
		if _, err := LoadConfig(path); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLoadScores(t *testing.T) {
	a := Arena{ID: "x", MonikerColumn: "moniker", ScoreColumn: "score"}
	cases := map[string]struct {
		body    string
		wantErr string
	}{
		"good":           {"moniker,address,score\nOffellNode,g1,90\n,g2,50\nCumulo,g3,80.5\n", ""},
		"duplicate":      {"moniker,score\nOffellNode,90\noffellnode ,80\n", "listed twice"},
		"not a number":   {"moniker,score\nOffellNode,#N/A\n", "not a number"},
		"blank score":    {"moniker,score\nOffellNode,\n", "not a number"},
		"missing column": {"moniker,Score\nOffellNode,90\n", "needs columns"},
	}
	for name, c := range cases {
		dir := t.TempDir()
		write(t, a.File(dir), c.body)
		got, err := LoadScores(dir, a)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: err = %v, want %q", name, err, c.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 2 || got["offellnode"] != 90 || got["cumulo"] != 80.5 {
			t.Errorf("%s: got %v, want two rows keyed by lowercased moniker, blank skipped", name, got)
		}
	}
}

func TestLoadCandidatesRejectsDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidates.csv")
	write(t, path, "moniker,discord\nCumulo,a\nCUMULO,b\n")
	if _, err := LoadCandidates(path); err == nil {
		t.Error("accepted a moniker listed twice")
	}
}
