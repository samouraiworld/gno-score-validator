package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/louisb/score-validator/internal/monitoring"
)

func mainnetReport() []monitoring.Validator {
	row := func(moniker string, score float64) monitoring.Validator {
		return monitoring.Validator{Moniker: moniker, Address: "g1" + moniker,
			Periods: map[string]monitoring.Metrics{monitoring.PeriodYear: {Score: score, Tier: "Good"}}}
	}
	return []monitoring.Validator{row("core-1", 70), row("Cogwheel", 90), row("berty", 90)}
}

func TestMainnetFromReport(t *testing.T) {
	vs := []snapValidator{{Moniker: "cogwheel"}}
	m, err := mainnetFromReport(mainnetReport(), "gnoland-1", monitoring.PeriodYear, vs)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, r := range m.Validators {
		order = append(order, r.Moniker)
		if r.Candidate != (r.Moniker == "Cogwheel") {
			t.Errorf("%s: candidate = %v", r.Moniker, r.Candidate)
		}
	}
	if got := strings.Join(order, ","); got != "berty,Cogwheel,core-1" {
		t.Errorf("order %s, want score then moniker", got)
	}
}

func TestMainnetFromReportRefusesBadReports(t *testing.T) {
	if _, err := mainnetFromReport(nil, "gnoland-1", monitoring.PeriodYear, nil); err == nil {
		t.Error("empty report accepted")
	}
	if _, err := mainnetFromReport(mainnetReport(), "gnoland-1", monitoring.PeriodWeek, nil); err == nil {
		t.Error("missing period accepted")
	}
	dup := append(mainnetReport(), mainnetReport()[1])
	dup[3].Moniker = "COGWHEEL"
	if _, err := mainnetFromReport(dup, "gnoland-1", monitoring.PeriodYear, nil); err == nil {
		t.Error("duplicate moniker accepted")
	}
}

// Mainnet is shown, never scored: carrying it must not move a total, and the
// scoreboard must list it apart from the arenas.
func TestMainnetStaysOutOfTotals(t *testing.T) {
	m, err := mainnetFromReport(mainnetReport(), "gnoland-1", monitoring.PeriodYear, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := snapComponents{Challenge: 60, Test13: 60, Topaz: 60, Sapphire: 60, Pearl: 60, FireDrill: 60}
	s := snapshot{GeneratedAt: "2026-10-12", Mainnet: m,
		Validators: []snapValidator{{Moniker: "a", Components: c, ScoreCorrected: total(c, 0)}}}
	if got := total(c, 0); got != 60 {
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
