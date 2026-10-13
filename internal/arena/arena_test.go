package arena

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// The committed config and every file it names must load.
func TestRepoConfigLoads(t *testing.T) {
	c, err := LoadConfig(filepath.Join("..", "..", "config", "arenas.json"))
	if err != nil {
		t.Fatal(err)
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
	if c.Mainnet == nil || c.Mainnet.InTotal || c.Mainnet.Chain != "gnoland-1" {
		t.Fatalf("mainnet entry %+v, want gnoland-1 shown, not scored", c.Mainnet)
	}
	if _, err := LoadScores(data, *c.Mainnet); err != nil {
		t.Error(err)
	}
}

// The mainnet entry leaves Arenas, so no component, divisor or tab reads it.
func TestLoadConfigTakesMainnetOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arenas.json")
	write(t, path, `{"arenas":[
		{"id":"a","label":"A","tab":"T","moniker_column":"m","score_column":"s","in_total":true,"chain":"c-1"},
		{"id":"mainnet","label":"M","moniker_column":"m","score_column":"s","chain":"gnoland-1","detail":"onchain"}]}`)
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Arenas) != 1 || c.Arenas[0].ID != "a" || c.Divisor() != 1 {
		t.Errorf("arenas %+v, want a alone", c.Arenas)
	}
	if c.Mainnet == nil || c.Mainnet.ID != MainnetID {
		t.Fatalf("mainnet %+v", c.Mainnet)
	}
	if a, ok := c.ByChain("gnoland-1"); !ok || a.ID != MainnetID {
		t.Errorf("ByChain(gnoland-1) = %+v, %v", a, ok)
	}
	if a, ok := c.ByChain("c-1"); !ok || a.ID != "a" {
		t.Errorf("ByChain(c-1) = %+v, %v", a, ok)
	}
	if _, ok := c.ByChain(""); ok {
		t.Error("ByChain(\"\") matched an arena without a chain")
	}
}

func TestLoadConfigRejects(t *testing.T) {
	ok := `{"id":"a","label":"A","tab":"T","moniker_column":"m","score_column":"s","in_total":true}`
	for name, body := range map[string]string{
		"duplicate id":      `{"arenas":[` + ok + `,` + ok + `]}`,
		"missing label":     `{"arenas":[{"id":"a","tab":"T","moniker_column":"m","score_column":"s","in_total":true}]}`,
		"nothing total":     `{"arenas":[{"id":"a","label":"A","tab":"T","moniker_column":"m","score_column":"s"}]}`,
		"unknown detail":    `{"arenas":[{"id":"a","label":"A","tab":"T","moniker_column":"m","score_column":"s","in_total":true,"detail":"x"}]}`,
		"reserved id":       `{"arenas":[{"id":"github","label":"A","tab":"T","moniker_column":"m","score_column":"s","in_total":true}]}`,
		"mainnet scored":    `{"arenas":[` + ok + `,{"id":"mainnet","label":"M","moniker_column":"m","score_column":"s","chain":"g-1","in_total":true,"detail":"onchain"}]}`,
		"mainnet no chain":  `{"arenas":[` + ok + `,{"id":"mainnet","label":"M","moniker_column":"m","score_column":"s","detail":"onchain"}]}`,
		"mainnet detail":    `{"arenas":[` + ok + `,{"id":"mainnet","label":"M","moniker_column":"m","score_column":"s","chain":"g-1","detail":"challenge"}]}`,
		"mainnet twice":     `{"arenas":[` + ok + `,{"id":"mainnet","label":"M","moniker_column":"m","score_column":"s","chain":"g-1","detail":"onchain"},{"id":"mainnet","label":"M","moniker_column":"m","score_column":"s","chain":"g-1","detail":"onchain"}]}`,
		"tab twice":         `{"arenas":[` + ok + `,` + strings.Replace(ok, `"id":"a"`, `"id":"b"`, 1) + `]}`,
		"tab Bilan":         `{"arenas":[` + strings.Replace(ok, `"tab":"T"`, `"tab":"bilan"`, 1) + `]}`,
		"tab Dashboard":     `{"arenas":[` + strings.Replace(ok, `"tab":"T"`, `"tab":"Dashboard"`, 1) + `]}`,
		"mainnet on Bilan":  `{"arenas":[` + ok + `,{"id":"mainnet","label":"M","tab":"Bilan","moniker_column":"m","score_column":"s","chain":"g-1","detail":"onchain"}]}`,
		"mainnet tab taken": `{"arenas":[` + ok + `,{"id":"mainnet","label":"M","tab":"t","moniker_column":"m","score_column":"s","chain":"g-1","detail":"onchain"}]}`,
		"chain twice":       `{"arenas":[` + strings.Replace(ok, "}", `,"chain":"g-1"}`, 1) + `,{"id":"mainnet","label":"M","moniker_column":"m","score_column":"s","chain":"g-1","detail":"onchain"}]}`,
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
