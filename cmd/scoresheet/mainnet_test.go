package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/monitoring"
)

func mainnetReport() []monitoring.Validator {
	row := func(moniker string, score float64) monitoring.Validator {
		return monitoring.Validator{Moniker: moniker, Address: "g1" + moniker,
			Periods: map[string]monitoring.Metrics{monitoring.PeriodYear: {Score: score, Tier: "Good"}}}
	}
	return []monitoring.Validator{row("core-1", 70), row("Cogwheel", 90), row("berty", 90)}
}

func testMainnetArena() arena.Arena {
	return arena.Arena{ID: arena.MainnetID, Chain: "gnoland-1", MonikerColumn: "moniker",
		ScoreColumn: "current_year_score", Detail: arena.DetailOnchain}
}

// writeMainnet writes report as fetch would and returns the data directory.
func writeMainnet(t *testing.T, report []monitoring.Validator) string {
	t.Helper()
	rows, err := fetchRows(report, monitoring.PeriodYear, "2026-10-12")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := testMainnetArena().File(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeCSV(path, rows); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFetchRows(t *testing.T) {
	report := mainnetReport()
	p := 99.5
	m := report[0].Periods[monitoring.PeriodYear]
	m.Proposer, m.SignRate = &p, 99.99960470454
	report[0].Periods[monitoring.PeriodYear] = m

	rows, err := fetchRows(report, monitoring.PeriodYear, "2026-10-12")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(rows[0], ","); got != strings.Join(fetchHeader(monitoring.PeriodYear), ",") ||
		!strings.HasPrefix(got, "moniker,address,current_year_score,") || !strings.HasSuffix(got, ",fetched_at") {
		t.Errorf("header %s", got)
	}
	var order []string
	for _, r := range rows[1:] {
		order = append(order, r[0])
	}
	if got := strings.Join(order, ","); got != "berty,Cogwheel,core-1" {
		t.Errorf("order %s, want by moniker", got)
	}
	core := rows[3]
	if core[4] != "99.99960470454" || core[6] != "99.5" || core[13] != "2026-10-12" {
		t.Errorf("core-1 row %v: want full-precision sign, proposer 99.5, date", core)
	}
	if rows[1][6] != "" {
		t.Errorf("null proposer written as %q, want blank", rows[1][6])
	}
}

func TestFetchRowsRefusesBadReports(t *testing.T) {
	if _, err := fetchRows(nil, monitoring.PeriodYear, "d"); err == nil {
		t.Error("empty report accepted")
	}
	if _, err := fetchRows(mainnetReport(), monitoring.PeriodWeek, "d"); err == nil {
		t.Error("missing period accepted")
	}
	dup := append(mainnetReport(), mainnetReport()[1])
	dup[3].Moniker = "COGWHEEL"
	if _, err := fetchRows(dup, monitoring.PeriodYear, "d"); err == nil {
		t.Error("duplicate moniker accepted")
	}
}

func TestFetchPeriod(t *testing.T) {
	if p, err := fetchPeriod(testMainnetArena()); err != nil || p != monitoring.PeriodYear {
		t.Errorf("period %q, %v", p, err)
	}
	for _, a := range []arena.Arena{
		{ID: "a", MonikerColumn: "moniker", ScoreColumn: "Score"},
		{ID: "a", MonikerColumn: "moniker", ScoreColumn: "all_time_score"},
		{ID: "a", MonikerColumn: "Moniker", ScoreColumn: "current_year_score"},
	} {
		if _, err := fetchPeriod(a); err == nil {
			t.Errorf("%+v accepted", a)
		}
	}
}

// fetch must keep refresh's frozen guard: every scored chain in config is a
// final arena, and only mainnet may be rewritten without -force.
func TestFetchGuardsFrozenArenas(t *testing.T) {
	cfg, err := arena.LoadConfig(filepath.Join("..", "..", "config", "arenas.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, chain := range []string{"sapphire-1", "pearl-1"} {
		a, ok := cfg.ByChain(chain)
		if !ok {
			t.Fatalf("%s: no arena", chain)
		}
		if guardFrozen(a.Tab, false) == nil {
			t.Errorf("%s: frozen arena %s rewritable without -force", chain, a.ID)
		}
	}
	a, ok := cfg.ByChain("gnoland-1")
	if !ok || a.ID != arena.MainnetID || a.InTotal {
		t.Fatalf("gnoland-1 -> %+v, want the mainnet entry, not in total", a)
	}
	if err := guardFrozen(a.Tab, false); err != nil {
		t.Error(err)
	}
	if _, ok := cfg.ByChain("topaz-1"); ok {
		t.Error("topaz-1 is fetchable: it would wipe the frozen topaz arena")
	}
}

func TestLoadMainnet(t *testing.T) {
	dir := writeMainnet(t, mainnetReport())
	a := testMainnetArena()
	m, err := loadMainnet(arena.Config{Mainnet: &a}, dir, []snapValidator{{Moniker: "cogwheel"}})
	if err != nil {
		t.Fatal(err)
	}
	if m.Chain != "gnoland-1" || m.Period != monitoring.PeriodYear || m.FetchedAt != "2026-10-12" {
		t.Errorf("block %s/%s/%s", m.Chain, m.Period, m.FetchedAt)
	}
	var order []string
	for _, r := range m.Validators {
		order = append(order, r.Moniker)
		if r.Candidate != (r.Moniker == "Cogwheel") {
			t.Errorf("%s: candidate = %v", r.Moniker, r.Candidate)
		}
		if r.Proposer != nil {
			t.Errorf("%s: blank proposer read as %v", r.Moniker, *r.Proposer)
		}
	}
	if got := strings.Join(order, ","); got != "berty,Cogwheel,core-1" {
		t.Errorf("order %s, want score then moniker", got)
	}

	if m, err := loadMainnet(arena.Config{}, dir, nil); m != nil || err != nil {
		t.Errorf("no mainnet in config: %v, %v", m, err)
	}
}

func TestLoadMainnetRefusesBadFiles(t *testing.T) {
	a := testMainnetArena()
	cfg := arena.Config{Mainnet: &a}
	for name, edit := range map[string]func(string) string{
		"two dates":      func(s string) string { return strings.Replace(s, "2026-10-12\n", "2026-10-11\n", 1) },
		"no date column": func(s string) string { return strings.Replace(s, ",fetched_at", ",when", 1) },
		"duplicate": func(s string) string {
			return s + strings.Replace(strings.Split(s, "\n")[1], "berty", "BERTY", 1) + "\n"
		},
		"wrong period": func(s string) string { return strings.ReplaceAll(s, "current_year_", "current_week_") },
	} {
		dir := writeMainnet(t, mainnetReport())
		path := a.File(dir)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(edit(string(b))), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadMainnet(cfg, dir, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Mainnet is shown, never scored: carrying it must not move a total, and the
// scoreboard must list it apart from the arenas.
func TestMainnetStaysOutOfTotals(t *testing.T) {
	a := testMainnetArena()
	m, err := loadMainnet(arena.Config{Mainnet: &a}, writeMainnet(t, mainnetReport()), nil)
	if err != nil {
		t.Fatal(err)
	}
	c, arenas := testComponents(60), testSnapArenas()
	s := snapshot{GeneratedAt: "2026-10-12", Arenas: arenas, Mainnet: m,
		Validators: []snapValidator{{Moniker: "a", Components: c, ScoreCorrected: total(c, arenas, 0)}}}
	if got := total(c, arenas, 0); got != 60 {
		t.Errorf("total = %g, want 60", got)
	}
	var b bytes.Buffer
	renderMainnet(&b, s.Mainnet)
	if !strings.Contains(b.String(), "shown, not scored") || !strings.Contains(b.String(), "| berty |") {
		t.Errorf("mainnet section missing rows or its not-scored label:\n%s", b.String())
	}
	if _, err := publicPayload(s); err != nil {
		t.Errorf("public payload refuses the mainnet block: %v", err)
	}
}
