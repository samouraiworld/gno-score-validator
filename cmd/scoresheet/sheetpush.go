package main

import (
	"flag"
	"fmt"
	"time"

	"google.golang.org/api/sheets/v4"

	"github.com/louisb/score-validator/internal/scoring"
	"github.com/louisb/score-validator/internal/sheetx"
)

// sheetNote is left on A1 of every tab push writes, where an editor's eye
// lands first.
const sheetNote = "Generated from the git repo — edit the CSV by pull request"

// runSheetPush writes the repo into the spreadsheet: Bilan and every arena
// tab as values, never formulas, then the Dashboard, the notes and the lock.
//
// It runs `sheet diff` first. A difference is overwritten without -force
// only when the sheet still holds exactly what the last push wrote: then the
// repo moved, not the sheet. Otherwise, or when no push was recorded, it
// refuses unless -force. The lock keeps everyone but the file owner out; the
// print is what catches the owner's own edits, and a protection someone
// removed. An edit worth keeping goes into the repo first: `freeze` copies
// the tabs back to the CSVs, for a pull request.
//
// Each push that ends with the sheet matching the repo records a new print
// (pushPrint) from that final read.
func runSheetPush(e *env, args []string) error {
	fs := flag.NewFlagSet("sheet push", flag.ExitOnError)
	dry := fs.Bool("dry", false, "print the plan without writing")
	force := fs.Bool("force", false, "overwrite a sheet edited since the last push, or the first push")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, tabs, err := expectedTabs(e.root)
	if err != nil {
		return err
	}
	diffs, hashes, err := e.diffSheet(tabs)
	if err != nil {
		return err
	}
	last, old, err := e.lastPush()
	if err != nil {
		return err
	}
	for _, d := range diffs {
		fmt.Println(" ", d)
	}
	var edited []string
	if last != nil {
		edited = editedSince(last, hashes, tabNames(tabs))
	}
	note, err := pushGate(len(diffs), last, edited, *force)
	if err != nil {
		return err
	}
	fmt.Println(note)
	for _, t := range tabs {
		fmt.Printf("  %-18s %4d rows\n", t.Name, len(t.Rows)-1)
	}

	d := newDashLayout(cfg, len(tabs[0].Rows)-1)
	if *dry {
		fmt.Println("  Dashboard          rebuilt")
		fmt.Println("lock plan:")
		if err := e.lockSheet(true); err != nil {
			return err
		}
		return nil
	}

	if err := e.writeTabs(tabs); err != nil {
		return err
	}
	if err := e.confidencePills(d); err != nil {
		return err
	}
	if err := e.buildDashboard(d); err != nil {
		return err
	}
	if err := e.noteTabs(append(tabNames(tabs), dashTab)); err != nil {
		return err
	}
	if err := e.lockSheet(false); err != nil {
		return err
	}

	// The push is only done when a fresh read agrees with the repo. That read
	// is what the print records, so the next push compares like with like.
	after, hashes, err := e.diffSheet(tabs)
	if err != nil {
		return err
	}
	for _, d := range after {
		fmt.Println(" ", d)
	}
	if len(after) > 0 {
		return fmt.Errorf("pushed, but the sheet still differs from the repo in %d place(s); no print recorded", len(after))
	}
	p := pushPrint{PushedAt: time.Now().UTC().Format(time.RFC3339), Tabs: hashes}
	if err := e.recordPush(old, p); err != nil {
		return fmt.Errorf("pushed, but recording the print failed: %w", err)
	}
	fmt.Printf("pushed; the sheet matches the repo (print recorded %s).\n", p.PushedAt)
	return nil
}

func tabNames(tabs []sheetTab) []string {
	names := make([]string, len(tabs))
	for i, t := range tabs {
		names[i] = t.Name
	}
	return names
}

// writeTabs creates missing tabs, grows grids that are too small, clears
// every tab and writes it under RAW. RAW stores a float64 as a number and a
// string as text, whatever the sheet's locale: no fr_FR parsing of "12.6" as
// the 12th of June, no formula. The number format is reset too, so a cell
// left over from such a misreading stops rendering as a date.
func (e *env) writeTabs(tabs []sheetTab) error {
	ss, err := e.svc.Spreadsheets.Get(e.id).Fields("sheets(properties)").Do()
	if err != nil {
		return err
	}
	var grow []*sheets.Request
	for _, t := range tabs {
		rows, cols := int64(len(t.Rows)), int64(0)
		for _, r := range t.Rows {
			cols = max(cols, int64(len(r)))
		}
		props := sheetProps(ss, t.Name)
		if props == nil {
			grow = append(grow, &sheets.Request{AddSheet: &sheets.AddSheetRequest{
				Properties: &sheets.SheetProperties{Title: t.Name,
					GridProperties: &sheets.GridProperties{RowCount: rows + 10, ColumnCount: cols}}}})
			fmt.Println("  created tab", t.Name)
			continue
		}
		g := props.GridProperties
		if g != nil && (g.RowCount < rows || g.ColumnCount < cols) {
			grow = append(grow, &sheets.Request{UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
				Properties: &sheets.SheetProperties{SheetId: props.SheetId,
					GridProperties: &sheets.GridProperties{
						RowCount: max(g.RowCount, rows), ColumnCount: max(g.ColumnCount, cols)}},
				Fields: "gridProperties.rowCount,gridProperties.columnCount"}})
		}
	}
	if len(grow) > 0 {
		if _, err := e.svc.Spreadsheets.BatchUpdate(e.id,
			&sheets.BatchUpdateSpreadsheetRequest{Requests: grow}).Do(); err != nil {
			return err
		}
		if ss, err = e.svc.Spreadsheets.Get(e.id).Fields("sheets(properties)").Do(); err != nil {
			return err
		}
	}

	var ranges []string
	var data []*sheets.ValueRange
	var formats []*sheets.Request
	for _, t := range tabs {
		ref := sheetx.Ref(t.Name)
		ranges = append(ranges, ref)
		data = append(data, &sheets.ValueRange{Range: ref + "!A1", Values: t.Rows})
		p := sheetProps(ss, t.Name)
		formats = append(formats, &sheets.Request{RepeatCell: &sheets.RepeatCellRequest{
			Range:  &sheets.GridRange{SheetId: p.SheetId, ForceSendFields: []string{"SheetId"}},
			Cell:   &sheets.CellData{UserEnteredFormat: &sheets.CellFormat{}},
			Fields: "userEnteredFormat.numberFormat"}})
	}
	if _, err := e.svc.Spreadsheets.Values.BatchClear(e.id,
		&sheets.BatchClearValuesRequest{Ranges: ranges}).Do(); err != nil {
		return err
	}
	if _, err := e.svc.Spreadsheets.Values.BatchUpdate(e.id, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "RAW", Data: data}).Do(); err != nil {
		return err
	}
	if _, err := e.svc.Spreadsheets.BatchUpdate(e.id,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: formats}).Do(); err != nil {
		return err
	}
	for _, t := range tabs {
		fmt.Printf("  %-18s written\n", t.Name)
	}
	return nil
}

func sheetProps(ss *sheets.Spreadsheet, title string) *sheets.SheetProperties {
	for _, s := range ss.Sheets {
		if s.Properties.Title == title {
			return s.Properties
		}
	}
	return nil
}

// noteTabs leaves sheetNote on A1 of each tab.
func (e *env) noteTabs(titles []string) error {
	ss, err := e.svc.Spreadsheets.Get(e.id).Fields("sheets(properties(sheetId,title))").Do()
	if err != nil {
		return err
	}
	var reqs []*sheets.Request
	for _, title := range titles {
		id, ok := sheetx.SheetID(ss, title)
		if !ok {
			return fmt.Errorf("tab %q not found", title)
		}
		reqs = append(reqs, &sheets.Request{UpdateCells: &sheets.UpdateCellsRequest{
			Range:  sheetx.Range(id, 0, 1, 0, 1),
			Rows:   []*sheets.RowData{{Values: []*sheets.CellData{{Note: sheetNote}}}},
			Fields: "note"}})
	}
	_, err = e.svc.Spreadsheets.BatchUpdate(e.id,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: reqs}).Do()
	return err
}

// confidencePills colours Bilan's confidence column so a weak match cannot
// pass for a verified one at a glance.
func (e *env) confidencePills(d dashLayout) error {
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
	// Drop the previous rules first, highest index down, so reruns do not stack.
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

	col := int64(d.bilan.Confidence)
	for _, p := range []struct{ word, fg, bg string }{
		{"certain", "#0b6b0b", "#e6f4e6"},
		{"strong", "#1c5cab", "#e7f0fb"},
		{"weak", "#6b7280", "#f1f3f5"},
	} {
		reqs = append(reqs, &sheets.Request{
			AddConditionalFormatRule: &sheets.AddConditionalFormatRuleRequest{
				Rule: &sheets.ConditionalFormatRule{
					Ranges: []*sheets.GridRange{
						sheetx.Range(id, scoring.FirstRow-1, int64(d.lastRow()), col, col+1)},
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
