package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTotalCapsAndWeighs(t *testing.T) {
	c := snapComponents{Challenge: 60, Test13: 60, Topaz: 60, Sapphire: 60, Pearl: 60, FireDrill: 60}
	if got := total(c, 50); got != 65 {
		t.Errorf("total = %g, want 60 + 50/10 = 65", got)
	}
	full := snapComponents{Challenge: 100, Test13: 100, Topaz: 100, Sapphire: 100, Pearl: 100, FireDrill: 100}
	if got := total(full, 100); got != 100 {
		t.Errorf("total = %g, want the cap of 100", got)
	}
}

func TestRankSharesTies(t *testing.T) {
	vs := []snapValidator{
		{Moniker: "a", ScoreCorrected: 70},
		{Moniker: "b", ScoreCorrected: 90},
		{Moniker: "c", ScoreCorrected: 70},
		{Moniker: "d", ScoreCorrected: 50},
	}
	rank(vs)
	want := map[string]int{"b": 1, "a": 2, "c": 2, "d": 4}
	for _, v := range vs {
		if v.RankCorrected != want[v.Moniker] {
			t.Errorf("%s: rank %d, want %d", v.Moniker, v.RankCorrected, want[v.Moniker])
		}
	}
}

func TestStrictNumber(t *testing.T) {
	row := []interface{}{"x", 12.5, "", nil, "#N/A", "12,5"}
	for i, want := range map[int]float64{1: 12.5, 2: 0, 3: 0, 9: 0} {
		if got, err := strictNumber(row, i); err != nil || got != want {
			t.Errorf("col %d: got %g, %v; want %g", i, got, err, want)
		}
	}
	for _, i := range []int{4, 5} {
		if _, err := strictNumber(row, i); err == nil {
			t.Errorf("col %d: %q accepted as a number", i, row[i])
		}
	}
}

func TestSlugAndEscape(t *testing.T) {
	for in, want := range map[string]string{
		"Umair_ChainGuard": "umair-chainguard", "  Node #1 ": "node-1", "***": "validator",
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
	if got := mdEscape("a|b_c"); got != `a\|b\_c` {
		t.Errorf("mdEscape = %q", got)
	}
}

// The scoreboard is committed, so rendering the same snapshot twice must give
// byte-identical files, and every candidate must get a page.
func TestRenderIsDeterministic(t *testing.T) {
	s, err := loadSnapshot(filepath.Join("..", "..", "data", "scorecard.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, b := t.TempDir(), t.TempDir()
	if err := render(s, a); err != nil {
		t.Fatal(err)
	}
	if err := render(s, b); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"README.md", filepath.Join(validatorsDir, "cogwheel.md")} {
		x, _ := os.ReadFile(filepath.Join(a, scoreboardDir, name))
		y, _ := os.ReadFile(filepath.Join(b, scoreboardDir, name))
		if len(x) == 0 || !bytes.Equal(x, y) {
			t.Errorf("%s differs between two renders, or is empty", name)
		}
	}
	pages, _ := os.ReadDir(filepath.Join(a, scoreboardDir, validatorsDir))
	if len(pages) != len(s.Validators) {
		t.Errorf("%d pages for %d validators", len(pages), len(s.Validators))
	}
}
