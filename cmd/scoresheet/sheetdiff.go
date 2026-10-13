package main

import (
	"flag"
	"fmt"
	"strconv"

	"github.com/louisb/score-validator/internal/scoring"
	"github.com/louisb/score-validator/internal/sheetx"
)

// sheetDiff is one place where the sheet does not say what the repo says: a
// cell, a row present on one side only, rows in another order, or a missing
// tab. Row is the sheet row, 1-based, or 0 when the sheet has no such row.
type sheetDiff struct {
	Tab     string
	Row     int
	Moniker string
	Column  string
	Sheet   string
	Repo    string
	What    string // set for anything but a cell
}

func (d sheetDiff) String() string {
	where := d.Tab
	if d.Row > 0 {
		where += fmt.Sprintf("  row %d", d.Row)
	}
	if d.Moniker != "" {
		where += "  " + d.Moniker
	}
	if d.What != "" {
		return where + ": " + d.What
	}
	return fmt.Sprintf("%s  [%s]  sheet %q, repo %q", where, d.Column, d.Sheet, d.Repo)
}

// runSheetDiff lists every difference between the sheet and the repo, and
// writes nothing. It exits non-zero when there is one, which is what lets
// `sheet push` refuse to overwrite an edit made in the sheet.
//
// Values are compared, not formulas: a formula that evaluates to what the
// repo says is not a difference.
func runSheetDiff(e *env, args []string) error {
	fs := flag.NewFlagSet("sheet diff", flag.ExitOnError)
	_ = fs.Parse(args)

	_, want, err := expectedTabs(e.root)
	if err != nil {
		return err
	}
	diffs, err := e.diffSheet(want)
	if err != nil {
		return err
	}
	for _, d := range diffs {
		fmt.Println(" ", d)
	}
	if len(diffs) > 0 {
		return fmt.Errorf("the sheet differs from the repo in %d place(s)", len(diffs))
	}
	fmt.Println("the sheet matches the repo.")
	return nil
}

// diffSheet reads every tab the repo owns, in one call, and compares it with
// what the repo says it should hold.
func (e *env) diffSheet(want []sheetTab) ([]sheetDiff, error) {
	ss, err := e.svc.Spreadsheets.Get(e.id).Fields("sheets.properties.title").Do()
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, s := range ss.Sheets {
		present[s.Properties.Title] = true
	}

	var diffs []sheetDiff
	var tabs []sheetTab
	var ranges []string
	for _, t := range want {
		if !present[t.Name] {
			diffs = append(diffs, sheetDiff{Tab: t.Name, What: "tab missing from the sheet"})
			continue
		}
		tabs = append(tabs, t)
		ranges = append(ranges, sheetx.Ref(t.Name))
	}
	if len(ranges) == 0 {
		return diffs, nil
	}
	got, err := e.svc.Spreadsheets.Values.BatchGet(e.id).Ranges(ranges...).
		ValueRenderOption("UNFORMATTED_VALUE").Do()
	if err != nil {
		return nil, err
	}
	if len(got.ValueRanges) != len(tabs) {
		return nil, fmt.Errorf("asked for %d tabs, got %d", len(tabs), len(got.ValueRanges))
	}
	for i, t := range tabs {
		diffs = append(diffs, diffTab(t, got.ValueRanges[i].Values)...)
	}
	return diffs, nil
}

// diffTab compares one tab. The header is compared by position; data rows are
// matched by moniker under scoring.Key, never by address. Rows without a
// moniker are matched in order among themselves. A moniker the sheet lists
// twice leaves its second row unmatched, reported as only in the sheet.
func diffTab(want sheetTab, got [][]interface{}) []sheetDiff {
	got = trimBlankRows(got)
	var header []interface{}
	if len(want.Rows) > 0 {
		header = want.Rows[0]
	}
	column := func(j int) string {
		if j < len(header) {
			if s := cellText(header[j]); s != "" {
				return s
			}
		}
		return sheetx.ColumnLetter(j)
	}
	diffs := diffRow(want.Name, 1, "", header, rowAt(got, 0), column)

	var wantRows, gotRows [][]interface{}
	if len(want.Rows) > 1 {
		wantRows = want.Rows[1:]
	}
	if len(got) > 1 {
		gotRows = got[1:]
	}
	wantIDs, gotIDs := rowIDs(wantRows, want.Key), rowIDs(gotRows, want.Key)
	pos := map[string]int{}
	for j, id := range gotIDs {
		pos[id] = j
	}

	used := make([]bool, len(gotRows))
	unmatched, moved := 0, -1
	for i, id := range wantIDs {
		moniker := cellText(cellAt(wantRows[i], want.Key))
		j, ok := pos[id]
		if !ok {
			unmatched++
			diffs = append(diffs, sheetDiff{Tab: want.Name, Moniker: moniker, What: "in the repo, missing from the sheet"})
			continue
		}
		used[j] = true
		if j != i && moved < 0 {
			moved = j + 2
		}
		diffs = append(diffs, diffRow(want.Name, j+2, moniker, wantRows[i], gotRows[j], column)...)
	}
	for j, u := range used {
		if !u {
			unmatched++
			diffs = append(diffs, sheetDiff{Tab: want.Name, Row: j + 2,
				Moniker: cellText(cellAt(gotRows[j], want.Key)), What: "in the sheet only"})
		}
	}
	// With a row missing on either side every later row shifts, so order is
	// only worth reporting when both sides hold the same rows.
	if unmatched == 0 && moved > 0 {
		diffs = append(diffs, sheetDiff{Tab: want.Name, Row: moved, What: "rows are not in repo order"})
	}
	return diffs
}

func diffRow(tab string, row int, moniker string, want, got []interface{}, column func(int) string) []sheetDiff {
	var diffs []sheetDiff
	n := len(want)
	if len(got) > n {
		n = len(got)
	}
	for j := 0; j < n; j++ {
		w, g := cellAt(want, j), cellAt(got, j)
		if w == nil {
			w = ""
		}
		if !sameCell(w, g) {
			diffs = append(diffs, sheetDiff{Tab: tab, Row: row, Moniker: moniker,
				Column: column(j), Sheet: cellText(g), Repo: cellText(w)})
		}
	}
	return diffs
}

// rowIDs names each row for matching: its moniker under Key, or its rank
// among the rows that have none. A repeated moniker gets a name no repo row
// carries.
func rowIDs(rows [][]interface{}, key int) []string {
	ids := make([]string, len(rows))
	seen := map[string]int{}
	blank := 0
	for i, r := range rows {
		k := scoring.Key(cellText(cellAt(r, key)))
		if k == "" {
			ids[i] = "#" + strconv.Itoa(blank)
			blank++
			continue
		}
		seen[k]++
		ids[i] = "=" + k
		if seen[k] > 1 {
			ids[i] += "#" + strconv.Itoa(seen[k])
		}
	}
	return ids
}

// sameCell compares a repo cell with a sheet cell. A repo number must be a
// sheet number: a number stored as text is a difference, the fr_FR trap the
// README describes. A repo string is compared with the sheet cell rendered the
// way freeze writes it.
func sameCell(want, got interface{}) bool {
	if w, ok := want.(float64); ok {
		g, ok := got.(float64)
		return ok && closeEnough(w, g)
	}
	return cellText(got) == cellText(want)
}

// cellText renders a cell the way textRows does, nil as "".
func cellText(c interface{}) string {
	switch v := c.(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fmt.Sprint(c)
}

func cellAt(row []interface{}, j int) interface{} {
	if j < 0 || j >= len(row) {
		return nil
	}
	return row[j]
}

func rowAt(rows [][]interface{}, i int) []interface{} {
	if i >= len(rows) {
		return nil
	}
	return rows[i]
}

// trimBlankRows drops trailing rows with no filled cell.
func trimBlankRows(rows [][]interface{}) [][]interface{} {
	for len(rows) > 0 {
		blank := true
		for _, c := range rows[len(rows)-1] {
			if cellText(c) != "" {
				blank = false
				break
			}
		}
		if !blank {
			break
		}
		rows = rows[:len(rows)-1]
	}
	return rows
}
