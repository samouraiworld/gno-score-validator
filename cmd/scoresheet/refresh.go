package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/api/sheets/v4"

	"github.com/louisb/score-validator/internal/monitoring"
	"github.com/louisb/score-validator/internal/scoring"
	"github.com/louisb/score-validator/internal/sheetx"
)

// A network tab holds one period of a gnomonitoring report, one validator per
// row, sorted by address. Column C is the only one Bilan reads — through
// VLOOKUP on the moniker in column A — but the rest is what makes a score
// arguable, so the whole row is rewritten together.
const (
	refreshFirstRow = 2
	refreshLastCol  = "M"
	refreshMaxRow   = 1000 // rows past the data are cleared out to here
)

// refreshColumns name the metrics in sheet order, after moniker and address.
// The header is written from these, prefixed by the period, so a tab can never
// claim to hold a window it does not.
var refreshColumns = []string{
	"score", "tier", "sign", "vp", "proposer",
	"critical", "warning", "freq", "freq_rate", "downtime", "missed",
}

// runRefresh rewrites a network tab from the live gnomonitoring report.
//
// Values go out as JSON numbers under RAW, never as formatted strings. That is
// not a style preference: the proposer column of Score-sapphire arrived as
// text through an fr_FR import, and "12.6" was read as the 12th of June, so
// fifty rows held date serials near 46185 instead of a reliability percentage.
// A JSON number cannot be reinterpreted, and the number format is reset here
// so a cell left over from the date reading stops rendering as one.
func runRefresh(e *env, args []string) error {
	// Not subFlags: it parses on the spot, and these have to be registered
	// before the parse or -chain is an unknown flag.
	fs := flag.NewFlagSet("refresh", flag.ExitOnError)
	dry := fs.Bool("dry", false, "print the plan without writing")
	chain := fs.String("chain", "", "gnomonitoring chain id (required)")
	tab := fs.String("tab", "", "tab to rewrite (required)")
	period := fs.String("period", monitoring.PeriodYear, "report period to write")
	force := fs.Bool("force", false, "rewrite a frozen arena, or accept a report that shrank")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// No defaults: a default tab let "refresh -chain pearl-1" silently
	// rewrite Score-sapphire with pearl data.
	if *chain == "" || *tab == "" {
		return fmt.Errorf("refresh needs both -chain and -tab")
	}
	if !*dry {
		if err := guardFrozen(*tab, *force); err != nil {
			return err
		}
	}

	report, err := monitoring.Fetch(e.ctx, *chain)
	if err != nil {
		return err
	}
	if len(report) == 0 {
		return fmt.Errorf("%s: empty report, refusing to wipe %s", *chain, *tab)
	}
	sort.Slice(report, func(i, j int) bool { return report[i].Address < report[j].Address })

	ss, err := e.svc.Spreadsheets.Get(e.id).Do()
	if err != nil {
		return err
	}
	sheetID, ok := sheetx.SheetID(ss, *tab)
	if !ok {
		return fmt.Errorf("tab %q not found", *tab)
	}

	current, err := e.currentScores(*tab)
	if err != nil {
		return err
	}

	header := []interface{}{"moniker", "address"}
	for _, c := range refreshColumns {
		header = append(header, *period+"_"+c)
	}

	rows := [][]interface{}{header}
	seen := make(map[string]bool, len(report))
	var added, changed int

	for _, v := range report {
		m, ok := v.Periods[*period]
		if !ok {
			return fmt.Errorf("%s: no period %q in report", v.Address, *period)
		}

		// A null proposer reliability means the validator was never asked to
		// propose. Writing 0 would read as a failure to do so.
		var proposer interface{} = ""
		if m.Proposer != nil {
			proposer = *m.Proposer
		}

		rows = append(rows, []interface{}{
			v.Moniker, v.Address,
			m.Score, m.Tier, m.SignRate, m.VotingPower, proposer,
			m.Critical, m.Warning, m.Incidents, m.IncidentRate, m.Downtime, m.Missed,
		})

		// Bilan looks up by moniker, case-insensitively, so that is the key
		// the diff has to speak in — an address rotation is invisible to it.
		key := strings.ToLower(strings.TrimSpace(v.Moniker))
		seen[key] = true
		old, known := current[key]
		switch {
		case !known:
			added++
			fmt.Printf("  + %-26s %-12s score %g\n", v.Moniker, "(new)", m.Score)
		case old != m.Score:
			changed++
			fmt.Printf("  ~ %-26s %6g -> %-6g\n", v.Moniker, old, m.Score)
		}
	}

	var dropped []string
	for key := range current {
		if !seen[key] {
			dropped = append(dropped, key)
		}
	}
	sort.Strings(dropped)
	for _, d := range dropped {
		fmt.Printf("  - %-26s %6g -> gone from the report, Bilan falls back to 0\n", d, current[d])
	}

	fmt.Printf("\n%s <- %s/%s: %d rows (%d new, %d changed, %d dropped)\n",
		*tab, *chain, *period, len(report), added, changed, len(dropped))
	if *dry {
		fmt.Println("-dry — nothing written.")
		return nil
	}
	// topaz-1 once answered with 2 validators where the tab held 85: a report
	// that loses a fifth of the tab is an outage or a retired chain, not news.
	if len(dropped)*5 > len(current) && !*force {
		return fmt.Errorf("%d of %d validators would drop out of %s; rerun with -force if that is real",
			len(dropped), len(current), *tab)
	}

	ref := sheetx.Ref(*tab)
	if _, err := e.svc.Spreadsheets.Values.Update(e.id,
		fmt.Sprintf("%s!A1:%s%d", ref, refreshLastCol, len(rows)),
		&sheets.ValueRange{Values: rows}).ValueInputOption("RAW").Do(); err != nil {
		return err
	}

	// The report shrinks as well as grows; stale rows below the new data would
	// keep answering VLOOKUP with a score their validator no longer has.
	if first := refreshFirstRow + len(report); first <= refreshMaxRow {
		if _, err := e.svc.Spreadsheets.Values.Clear(e.id,
			fmt.Sprintf("%s!A%d:%s%d", ref, first, refreshLastCol, refreshMaxRow),
			&sheets.ClearValuesRequest{}).Do(); err != nil {
			return err
		}
	}

	var formats []*sheets.Request
	for col, pattern := range numberFormats {
		if col == "F" { // the challenge bonus, not a network column
			continue
		}
		ci := int64(col[0] - 'A')
		formats = append(formats, sheetx.Format(sheetID, 1, refreshMaxRow, ci, ci+1,
			&sheets.CellFormat{
				NumberFormat:        &sheets.NumberFormat{Type: "NUMBER", Pattern: pattern},
				HorizontalAlignment: "RIGHT",
			}, "numberFormat,horizontalAlignment"))
	}
	if _, err := e.svc.Spreadsheets.BatchUpdate(e.id,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: formats}).Do(); err != nil {
		return err
	}

	fmt.Printf("%s rewritten from %s.\n", *tab, *chain)
	return nil
}

// guardFrozen refuses to rewrite a final arena result unless forced.
func guardFrozen(tab string, force bool) error {
	if scoring.IsFrozen(tab) && !force {
		return fmt.Errorf("%s holds a final arena result and is frozen; rerun with -force only to correct it", tab)
	}
	return nil
}

// currentScores reads the tab's moniker -> score column as it stands, keyed
// the way VLOOKUP matches it, so the diff survives a change of case.
func (e *env) currentScores(tab string) (map[string]float64, error) {
	vr, err := e.svc.Spreadsheets.Values.
		Get(e.id, fmt.Sprintf("%s!A%d:C%d", sheetx.Ref(tab), refreshFirstRow, refreshMaxRow)).
		ValueRenderOption("UNFORMATTED_VALUE").Do()
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(vr.Values))
	for _, row := range vr.Values {
		if len(row) < 3 || row[0] == nil {
			continue
		}
		moniker := strings.ToLower(strings.TrimSpace(fmt.Sprint(row[0])))
		if moniker == "" {
			continue
		}
		score, _ := row[2].(float64)
		out[moniker] = score
	}
	return out, nil
}
