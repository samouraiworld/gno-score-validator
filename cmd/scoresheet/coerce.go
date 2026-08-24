package main

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/api/sheets/v4"

	"github.com/louisb/score-validator/internal/scoring"
	"github.com/louisb/score-validator/internal/sheetx"
)

// coerceTargets are the columns known to have arrived as text. The network
// tabs were imported with a '.' decimal separator; the challenge tab's Bonus
// column carries a leading '+'.
var coerceTargets = []struct {
	tab  string
	cols []string
}{
	{scoring.Networks[0], []string{"E", "G", "K"}},
	{scoring.Networks[1], []string{"E", "G", "K"}},
	{scoring.Networks[2], []string{"E", "G", "K"}},
	{scoring.TabChallenge, []string{"F"}},
}

// numberFormats keep the same metric presented identically across tabs.
var numberFormats = map[string]string{
	"E": "0.0",   // signing rate
	"G": "0.0",   // blocks proposed
	"K": "0.000", // incident rate
	"F": "0.00",  // challenge bonus
}

// runCoerce rewrites text numbers as real numbers.
//
// Values go back as JSON numbers rather than strings, which sidesteps locale
// parsing entirely — no separator question ever arises. Anything that does not
// parse is left exactly as it is.
func runCoerce(e *env, args []string) error {
	_, dry := subFlags("coerce", args)

	ss, err := e.svc.Spreadsheets.Get(e.id).Do()
	if err != nil {
		return err
	}

	var writes []*sheets.ValueRange
	var formats []*sheets.Request
	total := 0

	for _, t := range coerceTargets {
		id, ok := sheetx.SheetID(ss, t.tab)
		if !ok {
			return fmt.Errorf("tab %q not found", t.tab)
		}
		for _, col := range t.cols {
			rng := fmt.Sprintf("%s!%s2:%s1000", sheetx.Ref(t.tab), col, col)
			vr, err := e.svc.Spreadsheets.Values.Get(e.id, rng).
				ValueRenderOption("UNFORMATTED_VALUE").Do()
			if err != nil {
				return fmt.Errorf("%s: %w", rng, err)
			}

			var out [][]interface{}
			var converted, kept, refused int
			var samples []string
			for _, row := range vr.Values {
				if len(row) == 0 || row[0] == nil {
					out = append(out, []interface{}{""})
					continue
				}
				s, isText := row[0].(string)
				if !isText {
					out = append(out, []interface{}{row[0]})
					kept++
					continue
				}
				trimmed := strings.TrimSpace(s)
				if trimmed == "" {
					out = append(out, []interface{}{""})
					continue
				}
				f, err := strconv.ParseFloat(strings.TrimPrefix(trimmed, "+"), 64)
				if err != nil {
					out = append(out, []interface{}{s}) // genuinely not a number
					refused++
					continue
				}
				out = append(out, []interface{}{f})
				converted++
				if len(samples) < 3 {
					samples = append(samples, fmt.Sprintf("%q→%g", trimmed, f))
				}
			}

			if converted == 0 {
				fmt.Printf("  %-18s %s  nothing to convert (%d already numeric)\n", t.tab, col, kept)
			} else {
				total += converted
				fmt.Printf("  %-18s %s  %d converted, %d untouched, %d refused  %v\n",
					t.tab, col, converted, kept, refused, samples)
				writes = append(writes, &sheets.ValueRange{
					Range: fmt.Sprintf("%s!%s2", sheetx.Ref(t.tab), col), Values: out})
			}

			if pat, ok := numberFormats[col]; ok {
				ci := int64(col[0] - 'A')
				formats = append(formats, sheetx.Format(id, 1, 1000, ci, ci+1,
					&sheets.CellFormat{
						NumberFormat:        &sheets.NumberFormat{Type: "NUMBER", Pattern: pat},
						HorizontalAlignment: "RIGHT",
					}, "numberFormat,horizontalAlignment"))
			}
		}
	}

	fmt.Printf("\n%d cells to convert\n", total)
	if *dry {
		fmt.Println("-dry — nothing written.")
		return nil
	}
	if len(writes) > 0 {
		if _, err := e.svc.Spreadsheets.Values.BatchUpdate(e.id,
			&sheets.BatchUpdateValuesRequest{ValueInputOption: "RAW", Data: writes}).Do(); err != nil {
			return err
		}
	}
	if len(formats) > 0 {
		if _, err := e.svc.Spreadsheets.BatchUpdate(e.id,
			&sheets.BatchUpdateSpreadsheetRequest{Requests: formats}).Do(); err != nil {
			return err
		}
	}
	fmt.Println("converted and formatted.")
	return nil
}
