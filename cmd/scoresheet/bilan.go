package main

import (
	"fmt"

	"google.golang.org/api/sheets/v4"

	"github.com/louisb/score-validator/internal/scoring"
	"github.com/louisb/score-validator/internal/sheetx"
)

// runBilan rewrites three things on the Bilan tab:
//
//   - column H, the GitHub participation score, restricted to contributions
//     from the cutoff year on;
//   - column I, the total, as a capped formula rather than a pasted number;
//   - columns J and K, the GitHub login and match confidence, so the bonus can
//     be audited from the same row instead of taken on trust.
//
// The human columns A-G are never touched.
func runBilan(e *env, args []string) error {
	_, dry := subFlags("bilan", args)

	links, err := scoring.LoadLinks(e.data("github-links.csv"))
	if err != nil {
		return err
	}

	// The sheet is the source of truth for row order — never assume it matches
	// the CSV, or a shuffle would silently attach scores to the wrong people.
	rng := fmt.Sprintf("%s!%s%d:%s%d", sheetx.Ref(scoring.TabBilan),
		scoring.ColMoniker, scoring.FirstRow, scoring.ColScore, scoring.LastRow)
	cur, err := e.svc.Spreadsheets.Values.Get(e.id, rng).Do()
	if err != nil {
		return err
	}
	if len(cur.Values) != scoring.NumRows {
		return fmt.Errorf("expected %d rows on %s, got %d",
			scoring.NumRows, scoring.TabBilan, len(cur.Values))
	}

	var github, score, audit [][]interface{}
	changed := 0
	for i, row := range cur.Values {
		r := scoring.FirstRow + i
		moniker := fmt.Sprint(row[0])

		link, linked := links[moniker]
		var newScore float64
		var login, confidence string
		if linked {
			newScore, login, confidence = link.Score, link.Login, link.Confidence
		}

		old := ""
		if len(row) > 7 {
			old = fmt.Sprint(row[7])
		}
		if fmt.Sprintf("%g", newScore) != old {
			changed++
			fmt.Printf("  row %-3d %-24s GitHub %q -> %g\n", r, moniker, old, newScore)
		}

		github = append(github, []interface{}{newScore})
		score = append(score, []interface{}{scoring.ScoreFormula(r)})
		audit = append(audit, []interface{}{login, confidence})
	}

	fmt.Printf("\n%d GitHub cell(s) change value; %d score formulas; %d audit rows\n",
		changed, len(score), len(audit))
	if *dry {
		fmt.Println("-dry — nothing written.")
		return nil
	}

	tab := sheetx.Ref(scoring.TabBilan)
	data := []*sheets.ValueRange{
		{Range: fmt.Sprintf("%s!%s%d", tab, scoring.ColGitHub, scoring.FirstRow), Values: github},
		{Range: fmt.Sprintf("%s!%s%d", tab, scoring.ColScore, scoring.FirstRow), Values: score},
		{Range: fmt.Sprintf("%s!%s1:%s1", tab, scoring.ColGitHubUser, scoring.ColConfidence),
			Values: [][]interface{}{{"Github-login", "Github-confidence"}}},
		{Range: fmt.Sprintf("%s!%s%d", tab, scoring.ColGitHubUser, scoring.FirstRow), Values: audit},
	}
	if _, err := e.svc.Spreadsheets.Values.BatchUpdate(e.id,
		&sheets.BatchUpdateValuesRequest{ValueInputOption: "USER_ENTERED", Data: data}).Do(); err != nil {
		return err
	}

	if err := e.confidencePills(); err != nil {
		return err
	}
	fmt.Println("Bilan updated.")
	return nil
}

// confidencePills colours the confidence column so a weak match cannot pass
// for a verified one at a glance.
func (e *env) confidencePills() error {
	ss, err := e.svc.Spreadsheets.Get(e.id).
		Fields("sheets(properties,conditionalFormats)").Do()
	if err != nil {
		return err
	}
	id, ok := sheetx.SheetID(ss, scoring.TabBilan)
	if !ok {
		return fmt.Errorf("tab %q not found", scoring.TabBilan)
	}

	var reqs []*sheets.Request
	// Drop our previous rules first, highest index down, so re-runs do not stack.
	for _, sh := range ss.Sheets {
		if sh.Properties.SheetId != id {
			continue
		}
		for i := len(sh.ConditionalFormats) - 1; i >= 0; i-- {
			reqs = append(reqs, &sheets.Request{
				DeleteConditionalFormatRule: &sheets.DeleteConditionalFormatRuleRequest{
					SheetId: id, Index: int64(i)}})
		}
	}

	col := int64(scoring.ColConfidence[0] - 'A')
	for _, p := range []struct{ word, fg, bg string }{
		{"certain", "#0b6b0b", "#e6f4e6"},
		{"strong", "#1c5cab", "#e7f0fb"},
		{"weak", "#6b7280", "#f1f3f5"},
	} {
		reqs = append(reqs, &sheets.Request{
			AddConditionalFormatRule: &sheets.AddConditionalFormatRuleRequest{
				Rule: &sheets.ConditionalFormatRule{
					Ranges: []*sheets.GridRange{
						sheetx.Range(id, scoring.FirstRow-1, scoring.LastRow, col, col+1)},
					BooleanRule: &sheets.BooleanRule{
						Condition: &sheets.BooleanCondition{Type: "TEXT_EQ",
							Values: []*sheets.ConditionValue{{UserEnteredValue: p.word}}},
						Format: &sheets.CellFormat{
							BackgroundColor: sheetx.Color(p.bg),
							TextFormat:      sheetx.Text(true, 0, p.fg)},
					}}}})
	}
	_, err = e.svc.Spreadsheets.BatchUpdate(e.id,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: reqs}).Do()
	return err
}
