package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/louisb/score-validator/internal/sheetx"
)

// runDump prints ranges as the sheet renders them. Useful for checking that a
// formula evaluated rather than landing as text or #ERROR!.
func runDump(e *env, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(`dump needs at least one range, e.g. "Bilan!A1:L3"`)
	}
	for _, rng := range args {
		vr, err := e.svc.Spreadsheets.Values.Get(e.id, rng).Do()
		if err != nil {
			return fmt.Errorf("%s: %w", rng, err)
		}
		fmt.Printf("=== %s (%d rows) ===\n", rng, len(vr.Values))
		for i, row := range vr.Values {
			fmt.Printf("  r%-4d %v\n", i+1, row)
		}
	}
	return nil
}

// runAudit reports cells holding a number that the sheet stores as text.
//
// This is not cosmetic: SUM and AVERAGE skip text silently, so a column of
// text numbers averages to zero without any warning. Two causes show up here —
// a '.' decimal separator in an fr_FR sheet, and a leading '+' as in "+0.50".
func runAudit(e *env, _ []string) error {
	ss, err := e.svc.Spreadsheets.Get(e.id).Do()
	if err != nil {
		return err
	}
	clean := true
	for _, sh := range ss.Sheets {
		title := sh.Properties.Title
		vr, err := e.svc.Spreadsheets.Values.
			Get(e.id, fmt.Sprintf("%s!A1:AB1000", sheetx.Ref(title))).
			ValueRenderOption("UNFORMATTED_VALUE").Do()
		if err != nil {
			return fmt.Errorf("%s: %w", title, err)
		}
		if len(vr.Values) < 2 {
			continue
		}
		header := vr.Values[0]
		for c := range header {
			var textNums int
			var sample string
			for _, row := range vr.Values[1:] {
				if c >= len(row) || row[c] == nil {
					continue
				}
				s, ok := row[c].(string)
				if !ok {
					continue
				}
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if _, err := strconv.ParseFloat(strings.TrimPrefix(s, "+"), 64); err == nil {
					textNums++
					if sample == "" {
						sample = s
					}
				}
			}
			if textNums > 0 {
				clean = false
				fmt.Printf("%-18s %-3s %-24v %d text numbers, e.g. %q\n",
					title, sheetx.ColumnLetter(c), header[c], textNums, sample)
			}
		}
	}
	if clean {
		fmt.Println("no numbers stored as text.")
	}
	return nil
}
