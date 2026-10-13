package main

import (
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/api/sheets/v4"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/scoring"
)

// asSheet turns repo rows into what the Sheets API returns for them read
// unformatted: the same cells, numbers as float64.
func asSheet(rows [][]interface{}) [][]interface{} {
	out := make([][]interface{}, len(rows))
	for i, r := range rows {
		out[i] = append([]interface{}(nil), r...)
	}
	return out
}

func TestExpectedTabsFromRepo(t *testing.T) {
	root := filepath.Join("..", "..")
	cfg, tabs, err := expectedTabs(root)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := arena.LoadCandidates(filepath.Join(root, "data", "candidates.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1+len(cfg.Arenas) {
		t.Fatalf("%d tabs, want Bilan plus %d arenas", len(tabs), len(cfg.Arenas))
	}
	bilan := tabs[0]
	if bilan.Name != scoring.TabBilan || len(bilan.Rows) != 1+len(candidates) {
		t.Fatalf("Bilan: %s with %d rows", bilan.Name, len(bilan.Rows))
	}
	cols := bilanLayout(cfg)
	if got := len(bilan.Rows[0]); got != cols.Confidence+1 {
		t.Fatalf("Bilan header has %d columns, want %d", got, cols.Confidence+1)
	}
	for _, r := range bilan.Rows[1:] {
		for j := bilanArena0; j <= cols.Score; j++ {
			if _, ok := r[j].(float64); !ok {
				t.Fatalf("%v: column %d is %T, want a number", r[0], j, r[j])
			}
		}
	}
	for i, a := range cfg.Arenas {
		tab := tabs[1+i]
		if tab.Name != a.Tab {
			t.Fatalf("tab %d is %s, want %s", i+1, tab.Name, a.Tab)
		}
		for _, r := range tab.Rows[1:] {
			if m, ok := cellAt(r, tab.Key).(string); !ok {
				t.Fatalf("%s: moniker %v is %T, want text", a.Tab, m, cellAt(r, tab.Key))
			}
		}
		if d := diffTab(tab, asSheet(tab.Rows)); len(d) != 0 {
			t.Fatalf("%s differs from itself: %v", a.Tab, d[0])
		}
	}
}

func TestNumericColumns(t *testing.T) {
	rows := [][]string{
		{"007", "1.5", "g1abc", "", "x"},
		{"12", "-2", "g1def", "", "3"},
		{"ab", ".5", "", ""},
	}
	got := numericColumns(5, rows, 0)
	want := []bool{false, true, false, false, false}
	for j := range want {
		if got[j] != want[j] {
			t.Fatalf("column %d numeric = %v, want %v", j, got[j], want[j])
		}
	}
	for _, s := range []string{"inf", "NaN", "0x1p-2", "1e5", "1,5", "+1"} {
		if decimal.MatchString(s) {
			t.Fatalf("%q passes as a decimal", s)
		}
	}
}

func TestDiffTab(t *testing.T) {
	want := sheetTab{Name: "T", Key: 0, Rows: [][]interface{}{
		{"moniker", "score", "tier"},
		{"Alpha", 90.0, "A"},
		{"", 50.0, "B"},
		{"beta", 80.0, ""},
	}}
	same := func(edit func([][]interface{}) [][]interface{}) []sheetDiff {
		return diffTab(want, edit(asSheet(want.Rows)))
	}

	if d := same(func(r [][]interface{}) [][]interface{} {
		r[1][0] = "ALPHA " // the join is by Key, but the cell itself still differs
		r[3] = []interface{}{"beta", 80.0}
		return append(r, []interface{}{}, []interface{}{"", nil})
	}); len(d) != 1 || d[0].Column != "moniker" {
		t.Fatalf("case and trailing blanks: %v", d)
	}

	d := same(func(r [][]interface{}) [][]interface{} {
		r[1][1] = "90" // a number stored as text
		r[3][1] = 80.0000000001
		return r
	})
	if len(d) != 1 || d[0].Row != 2 || d[0].Column != "score" {
		t.Fatalf("text number: %v", d)
	}

	d = same(func(r [][]interface{}) [][]interface{} {
		return [][]interface{}{r[0], r[3], r[2], r[1]}
	})
	if len(d) != 1 || !strings.Contains(d[0].What, "order") {
		t.Fatalf("reorder: %v", d)
	}

	d = same(func(r [][]interface{}) [][]interface{} {
		return [][]interface{}{r[0], r[1], r[2], {"gamma", 1.0}, {"Alpha", 90.0, "A"}}
	})
	var what []string
	for _, x := range d {
		what = append(what, x.Moniker+":"+x.What)
	}
	if got := strings.Join(what, ","); got != "beta:in the repo, missing from the sheet,gamma:in the sheet only,Alpha:in the sheet only" {
		t.Fatalf("missing and extra rows: %s", got)
	}

	if d := diffTab(want, nil); len(d) != 3+3 {
		t.Fatalf("empty tab: %d diffs, want 3 header cells and 3 rows", len(d))
	}
}

func TestScorecardStaleIsRefused(t *testing.T) {
	root := filepath.Join("..", "..")
	card, err := loadSnapshot(filepath.Join(root, "data", "scorecard.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkScorecardFresh(root, card); err != nil {
		t.Fatal(err)
	}
	card.Validators[3].Components[4].Score += 1
	if err := checkScorecardFresh(root, card); err == nil || !strings.Contains(err.Error(), "run score") {
		t.Fatalf("stale scorecard accepted: %v", err)
	}
}

func TestLockRequests(t *testing.T) {
	ss := &sheets.Spreadsheet{Sheets: []*sheets.Sheet{
		{Properties: &sheets.SheetProperties{SheetId: 0, Title: "Bilan"},
			ProtectedRanges: []*sheets.ProtectedRange{
				{ProtectedRangeId: 7, Description: lockDescription},
				{ProtectedRangeId: 8, Description: "set by hand"},
			}},
		{Properties: &sheets.SheetProperties{SheetId: 42, Title: "Score-pearl"}},
	}}
	reqs := lockRequests(ss, "bot@example.iam")
	if len(reqs) != 3 || reqs[0].DeleteProtectedRange == nil || reqs[0].DeleteProtectedRange.ProtectedRangeId != 7 {
		t.Fatalf("want one delete of our lock then two adds, got %d requests", len(reqs))
	}
	for i, r := range reqs[1:] {
		p := r.AddProtectedRange.ProtectedRange
		b, err := p.Range.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"sheetId"`) || p.Range.SheetId != ss.Sheets[i].Properties.SheetId {
			t.Fatalf("add %d: range %s does not name its tab", i, b)
		}
		if p.WarningOnly || len(p.Editors.Users) != 1 || p.Editors.Users[0] != "bot@example.iam" || p.Description != lockDescription {
			t.Fatalf("add %d: %+v", i, p)
		}
	}
}
