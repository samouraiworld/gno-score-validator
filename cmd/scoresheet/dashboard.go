package main

import (
	"fmt"
	"strings"

	"google.golang.org/api/sheets/v4"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/scoring"
	"github.com/louisb/score-validator/internal/sheetx"
)

const dashTab = "Dashboard"

// Rows above the component table are fixed. Everything from the component
// table down is placed by dashLayout, because that table grows with the arena
// list and the standings grow with the candidate list.
const (
	methodBar  = 8
	methodTop  = methodBar + 1
	methodRows = 6
	profBar    = methodTop + methodRows
	profHdr    = profBar + 1 // moniker row, and the COMPONENT header
	onchRows   = 12
)

// Palette shared with the web scorecard. Arena colours come from
// config/arenas.json; these are the neutrals and the two accents the
// dashboard itself uses.
const (
	cInk      = "#0f1720"
	cInkSoft  = "#56636f"
	cMuted    = "#8794a4"
	cBorder   = "#e0e6ee"
	cSurface2 = "#f4f6f9"
	cTest13   = "#1baf7a"
	cSapphire = "#2a78d6"
	cGitHub   = "#e87ba4"
	cScaleLow = "#eaf2fd"
)

// dashLayout places every block that depends on the arena list or on the
// number of candidates. Rows are 1-based like the A1 references they feed;
// columns are 0-based like the API's grid ranges.
type dashLayout struct {
	arenas  []arena.Arena
	onchain []arena.Arena // the arenas with gnomonitoring metrics, one on-chain column each
	divisor int
	bilan   bilanColumns
	rows    int // validators on Bilan, from scoring.FirstRow

	profRows, onchBar, onchHdr, stdBar, stdHdr, stdFirst, helperRow, gridRows int

	// The standings are a rank in A, then Bilan from the moniker to the
	// GitHub login spilled from B, then a score bar.
	stdScore, stdLogin, stdSpark, dashCols, chartCol, helperCol, gridCols int
}

func newDashLayout(cfg arena.Config, rows int) dashLayout {
	d := dashLayout{arenas: cfg.Arenas, divisor: cfg.Divisor(), bilan: bilanLayout(cfg), rows: rows}
	for _, a := range cfg.Arenas {
		if a.Detail == arena.DetailOnchain {
			d.onchain = append(d.onchain, a)
		}
	}
	d.profRows = len(cfg.Arenas) + 2 // one per arena, the GitHub bonus, TOTAL
	d.onchBar = profHdr + d.profRows + 1
	d.onchHdr = d.onchBar + 1
	d.stdBar = d.onchHdr + onchRows + 1
	d.stdHdr = d.stdBar + 1
	d.stdFirst = d.stdHdr + 1
	d.helperRow = max(150, d.stdFirst+rows+2) // off-screen source for the distribution chart
	d.gridRows = max(200, d.helperRow+10)

	d.stdScore = d.bilan.Score + 1
	d.stdLogin = d.bilan.Login + 1
	d.stdSpark = d.stdLogin + 1
	d.dashCols = d.stdSpark + 1
	d.chartCol = d.dashCols // charts start immediately beyond the standings
	d.helperCol = d.dashCols + 1
	d.gridCols = d.dashCols + 3
	return d
}

func (d dashLayout) lastRow() int { return scoring.FirstRow + d.rows - 1 }

// bilanRange is one Bilan column over the validator rows, absolute.
func (d dashLayout) bilanRange(col int) string {
	c := sheetx.ColumnLetter(col)
	return fmt.Sprintf("%s!$%s$%d:$%s$%d", sheetx.Ref(scoring.TabBilan), c, scoring.FirstRow, c, d.lastRow())
}

// bilanLookup pulls one Bilan column for the validator chosen in the dropdown.
func (d dashLayout) bilanLookup(col int) string {
	return sheetx.Dashed(fmt.Sprintf(`XLOOKUP($C$%d;%s;%s)`,
		profHdr, d.bilanRange(bilanMoniker), d.bilanRange(col)))
}

// netLookup pulls one metric from a network tab for the chosen validator.
func netLookup(tab, col string, text bool) string {
	get := fmt.Sprintf(`XLOOKUP($C$%d;%s!$A$2:$A$1000;%s!$%s$2:$%s$1000)`,
		profHdr, sheetx.Ref(tab), sheetx.Ref(tab), col, col)
	if text {
		return sheetx.Dashed(get)
	}
	return sheetx.Numeric(get)
}

// buildDashboard rebuilds the Dashboard tab from scratch. It holds formulas
// over Bilan and the arena tabs, which `sheet push` fills with values, so the
// profile dropdown and the charts stay live.
func (e *env) buildDashboard(d dashLayout) error {
	ss, err := e.svc.Spreadsheets.Get(e.id).
		Fields("sheets(properties,charts(chartId),bandedRanges,conditionalFormats)").Do()
	if err != nil {
		return err
	}

	id, ok := sheetx.SheetID(ss, dashTab)
	if !ok {
		created, err := e.svc.Spreadsheets.BatchUpdate(e.id, &sheets.BatchUpdateSpreadsheetRequest{
			Requests: []*sheets.Request{{AddSheet: &sheets.AddSheetRequest{
				Properties: &sheets.SheetProperties{Title: dashTab,
					GridProperties: &sheets.GridProperties{RowCount: int64(d.gridRows), ColumnCount: int64(d.gridCols)}}}}},
		}).Do()
		if err != nil {
			return err
		}
		id = created.Replies[0].AddSheet.Properties.SheetId
		fmt.Println("created tab", dashTab)
	}

	if err := e.wipeDashboard(ss, id, d); err != nil {
		return err
	}
	if err := e.writeDashboardValues(d); err != nil {
		return err
	}
	if err := e.formatDashboard(id, d); err != nil {
		return err
	}
	if err := e.dashboardCharts(id, d); err != nil {
		return err
	}
	fmt.Println("  Dashboard          rebuilt")
	return nil
}

// wipeDashboard clears cells, charts, banding and conditional rules, so the
// command is idempotent. Clearing cells alone leaves banding behind, and a
// second run then fails with "range already has alternating colors".
func (e *env) wipeDashboard(ss *sheets.Spreadsheet, id int64, d dashLayout) error {
	rows, cols := int64(d.gridRows), int64(d.gridCols)
	reqs := []*sheets.Request{
		{UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
			Properties: &sheets.SheetProperties{SheetId: id,
				GridProperties: &sheets.GridProperties{RowCount: rows, ColumnCount: cols}},
			Fields: "gridProperties.rowCount,gridProperties.columnCount"}},
		{UpdateCells: &sheets.UpdateCellsRequest{
			Range: sheetx.Range(id, 0, rows, 0, cols), Fields: "*"}},
	}
	for _, sh := range ss.Sheets {
		if sh.Properties.SheetId != id {
			continue
		}
		for _, c := range sh.Charts {
			reqs = append(reqs, &sheets.Request{
				DeleteEmbeddedObject: &sheets.DeleteEmbeddedObjectRequest{ObjectId: c.ChartId}})
		}
		for _, b := range sh.BandedRanges {
			reqs = append(reqs, &sheets.Request{
				DeleteBanding: &sheets.DeleteBandingRequest{BandedRangeId: b.BandedRangeId}})
		}
		for i := len(sh.ConditionalFormats) - 1; i >= 0; i-- {
			reqs = append(reqs, &sheets.Request{
				DeleteConditionalFormatRule: &sheets.DeleteConditionalFormatRuleRequest{
					SheetId: id, Index: int64(i)}})
		}
	}
	reqs = append(reqs, &sheets.Request{UnmergeCells: &sheets.UnmergeCellsRequest{
		Range: sheetx.Range(id, 0, rows, 0, cols)}})
	_, err := e.svc.Spreadsheets.BatchUpdate(e.id,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: reqs}).Do()
	return err
}

// scoreRule is the scoring rule in words, from the config: the scored arenas,
// the divisor, the GitHub bonus and the cap.
func (d dashLayout) scoreRule(name func(arena.Arena) string, sep string) string {
	var scored []string
	for _, a := range d.arenas {
		if a.InTotal {
			scored = append(scored, name(a))
		}
	}
	return fmt.Sprintf("( %s ) / %d   +   GitHub / %d,   capped at %d",
		strings.Join(scored, sep), d.divisor, scoring.BonusDivisor, scoring.MaxScore)
}

func (d dashLayout) methodText() [][]interface{} {
	rows := [][]interface{}{
		{"Score  =  " + d.scoreRule(func(a arena.Arena) string { return a.Tab }, " + ")},
		{fmt.Sprintf("The %d arenas carry equal weight: each contributes at most %.1f points. GitHub is a bonus on top, worth at most %d — it can lift a candidate, never sink one.",
			d.divisor, float64(scoring.MaxScore)/float64(d.divisor), scoring.MaxScore/scoring.BonusDivisor)},
		{fmt.Sprintf("GitHub score  =  %d × merged PRs  +  %d × unmerged PRs  +  %d × issues opened  +  %d × commits  +  %d × comment threads. A merged pull request is the only artefact proving code landed, so it outweighs intent and discussion.",
			scoring.WeightMergedPR, scoring.WeightUnmergedPR, scoring.WeightIssue,
			scoring.WeightCommit, scoring.WeightComment)},
		{fmt.Sprintf("Only contributions dated %d or later count toward the bonus. One candidate is affected: Santala-Research, whose three gnolang pull requests are typo fixes from 2023 that predate test13 entirely.", scoring.CutoffYear)},
		// The counts below are formulas, not text. The fire drill stayed open
		// after its first export and grew from 49 submitters to 59 while a typed
		// "49" sat on this tab, contradicting the standings under it.
		{d.liveCount(`COUNTIF(%s;"?*")`, d.bilan.Login,
			" of the %d candidates could be tied to a GitHub account, each verified by hand. A blank GitHub column is absence of evidence, not evidence of absence — it never costs points.", "")},
		{""},
	}
	for i, a := range d.arenas {
		if a.Detail == arena.DetailFireDrill {
			rows[5] = []interface{}{d.liveCount(`COUNTIF(%s;">0")`, bilanArena0+i,
				" of the %d candidates submitted.",
				"The pearl-1 fire drill measured upload time, metadata, log quality and incident response. ")}
			break
		}
	}
	return rows
}

// componentRows breaks the total into what each arena contributed.
func (d dashLayout) componentRows() [][]interface{} {
	type comp struct {
		label   string
		col     int
		divisor int // 0: shown, not part of the total
		color   string
	}
	var comps []comp
	for i, a := range d.arenas {
		c := comp{a.Label, bilanArena0 + i, 0, a.Colour}
		if a.InTotal {
			c.divisor = d.divisor
		}
		if c.color == "" {
			c.color = cMuted
		}
		comps = append(comps, c)
	}
	comps = append(comps, comp{"GitHub bonus", d.bilan.GitHub, scoring.BonusDivisor, cGitHub})

	var out [][]interface{}
	for i, c := range comps {
		raw := fmt.Sprintf("$F$%d", profHdr+1+i)
		points := ""
		if c.divisor > 0 {
			points = fmt.Sprintf(`=IFERROR(%s/%d;"—")`, raw, c.divisor)
		}
		out = append(out, []interface{}{c.label, "", d.bilanLookup(c.col), points,
			sheetx.SparkBar(raw, c.color, cScaleLow)})
	}
	out = append(out, []interface{}{"TOTAL", "", "",
		fmt.Sprintf(`=MIN(%d;SUM($G$%d:$G$%d))`, scoring.MaxScore, profHdr+1, profHdr+len(comps)), ""})
	return out
}

// onchainRows lays out the per-network metric table. Column letters follow the
// network tabs: C score, D tier, E sign, F vp, G proposer, H critical,
// I warning, J freq, K freq_rate, L downtime, M missed.
func (d dashLayout) onchainRows() [][]interface{} {
	metrics := []struct {
		label string
		col   string
		text  bool
	}{
		{"Tier", "D", true},
		{"Network score", "C", false},
		{"Signing rate", "E", false},
		{"Voting power", "F", false},
		{"Blocks proposed", "G", false},
		{"Critical events", "H", false},
		{"Warnings", "I", false},
		{"Downtime windows", "J", false},
		{"Incident rate", "K", false},
		{"Downtime (blocks)", "L", false},
		{"Missed blocks", "M", false},
		{"Operator address", "B", true},
	}
	var out [][]interface{}
	for _, m := range metrics {
		row := []interface{}{m.label, ""}
		for _, a := range d.onchain {
			row = append(row, netLookup(a.Tab, m.col, m.text))
		}
		out = append(out, row)
	}
	return out
}

func (e *env) writeDashboardValues(d dashLayout) error {
	bilan := sheetx.Ref(scoring.TabBilan)
	scoreRange := d.bilanRange(d.bilan.Score)
	monikerRange := d.bilanRange(bilanMoniker)
	loginRange := d.bilanRange(d.bilan.Login)

	ids := make([]string, len(d.arenas))
	for i, a := range d.arenas {
		ids[i] = a.ID
	}
	var onchainIDs []interface{}
	for _, a := range d.onchain {
		onchainIDs = append(onchainIDs, a.ID)
	}
	stdHeader := []interface{}{"#", "Moniker", "Discord"}
	for _, id := range ids {
		stdHeader = append(stdHeader, id)
	}
	stdHeader = append(stdHeader, "GitHub", "Score", "GitHub login", "Score bar")
	chosen := make([]string, d.bilan.Login+1)
	for i := range chosen {
		chosen[i] = itoa(i + 1)
	}

	vals := []*sheets.ValueRange{
		{Range: dashTab + "!A1", Values: [][]interface{}{{"Gno Validator Scorecard"}}},
		{Range: dashTab + "!A2", Values: [][]interface{}{{fmt.Sprintf(
			"%d test13 candidates  ·  Score = %s  ·  generated from the git repo",
			d.rows, d.scoreRule(func(a arena.Arena) string { return a.ID }, " + "))}}},

		{Range: dashTab + "!A4", Values: [][]interface{}{
			{"CANDIDATES", "", "MEDIAN SCORE", "", "TOP SCORE", "", "MEAN SCORE", "", "GITHUB IDENTIFIED"}}},
		{Range: dashTab + "!A5", Values: [][]interface{}{{
			fmt.Sprintf("=COUNTA(%s)", monikerRange), "",
			fmt.Sprintf("=ROUND(MEDIAN(%s);2)", scoreRange), "",
			fmt.Sprintf("=ROUND(MAX(%s);2)", scoreRange), "",
			fmt.Sprintf("=ROUND(AVERAGE(%s);2)", scoreRange), "",
			fmt.Sprintf(`=COUNTIF(%s;"<>")`, loginRange)}}},
		{Range: dashTab + "!A6", Values: [][]interface{}{{
			fmt.Sprintf("scored across %d arenas", d.divisor), "",
			"half score above, half below", "",
			fmt.Sprintf("=XLOOKUP(MAX(%s);%s;%s)", scoreRange, scoreRange, monikerRange), "",
			"average of all candidates", "",
			"GitHub account verified by hand"}}},

		{Range: fmt.Sprintf("%s!A%d", dashTab, methodBar),
			Values: [][]interface{}{{"HOW THE SCORE IS BUILT"}}},
		{Range: fmt.Sprintf("%s!A%d", dashTab, methodTop), Values: d.methodText()},

		{Range: fmt.Sprintf("%s!A%d", dashTab, profBar),
			Values: [][]interface{}{{"VALIDATOR PROFILE"}}},
		{Range: fmt.Sprintf("%s!B%d", dashTab, profHdr),
			Values: [][]interface{}{{"Moniker", "ITRocket"}}},
		{Range: fmt.Sprintf("%s!B%d", dashTab, profHdr+1), Values: [][]interface{}{
			{"Discord", d.bilanLookup(bilanDiscord)},
			{"Rank", fmt.Sprintf(`=IFERROR(RANK($C$%d;%s);"—")`, profHdr+3, scoreRange)},
			{"Score / 100", d.bilanLookup(d.bilan.Score)},
			{"GitHub login", d.bilanLookup(d.bilan.Login)},
			{"Match confidence", d.bilanLookup(d.bilan.Confidence)},
		}},
		{Range: fmt.Sprintf("%s!D%d", dashTab, profHdr),
			Values: [][]interface{}{{"COMPONENT", "", "RAW", "POINTS", ""}}},
		{Range: fmt.Sprintf("%s!D%d", dashTab, profHdr+1), Values: d.componentRows()},

		{Range: fmt.Sprintf("%s!A%d", dashTab, d.onchBar),
			Values: [][]interface{}{{"ON-CHAIN DETAIL  ·  current year"}}},
		{Range: fmt.Sprintf("%s!B%d", dashTab, d.onchHdr), Values: [][]interface{}{
			append([]interface{}{"METRIC", ""}, onchainIDs...)}},
		{Range: fmt.Sprintf("%s!B%d", dashTab, d.onchHdr+1), Values: d.onchainRows()},

		{Range: fmt.Sprintf("%s!A%d", dashTab, d.stdBar), Values: [][]interface{}{{"STANDINGS"}}},
		{Range: fmt.Sprintf("%s!A%d", dashTab, d.stdHdr), Values: [][]interface{}{stdHeader}},
		// One spilling formula keeps the table live: rewrite Bilan and it reorders.
		{Range: fmt.Sprintf("%s!A%d", dashTab, d.stdFirst), Values: [][]interface{}{{
			fmt.Sprintf("=SEQUENCE(%d)", d.rows),
			fmt.Sprintf("=SORT(CHOOSECOLS(%s!A%d:%s%d;%s);%d;FALSE)",
				bilan, scoring.FirstRow, sheetx.ColumnLetter(d.bilan.Confidence), d.lastRow(),
				strings.Join(chosen, ";"), d.bilan.Score+1)}}},

		{Range: fmt.Sprintf("%s!%s%d", dashTab, sheetx.ColumnLetter(d.helperCol), d.helperRow),
			Values: [][]interface{}{{"Score band", "Candidates"}}},
	}

	var bands [][]interface{}
	for _, b := range [][2]int{{0, 20}, {20, 40}, {40, 60}, {60, 80}, {80, 101}} {
		label := fmt.Sprintf("%d–%d", b[0], b[1])
		if b[1] > scoring.MaxScore {
			label = fmt.Sprintf("%d–%d", b[0], scoring.MaxScore)
		}
		bands = append(bands, []interface{}{label,
			fmt.Sprintf(`=COUNTIFS(%s;">=%d";%s;"<%d")`, scoreRange, b[0], scoreRange, b[1])})
	}
	vals = append(vals, &sheets.ValueRange{
		Range:  fmt.Sprintf("%s!%s%d", dashTab, sheetx.ColumnLetter(d.helperCol), d.helperRow+1),
		Values: bands})

	var sparks [][]interface{}
	score := sheetx.ColumnLetter(d.stdScore)
	for r := d.stdFirst; r < d.stdFirst+d.rows; r++ {
		sparks = append(sparks, []interface{}{
			sheetx.SparkBar(fmt.Sprintf("%s%d", score, r), cSapphire, cScaleLow)})
	}
	vals = append(vals, &sheets.ValueRange{
		Range: fmt.Sprintf("%s!%s%d", dashTab, sheetx.ColumnLetter(d.stdSpark), d.stdFirst), Values: sparks})

	_, err := e.svc.Spreadsheets.Values.BatchUpdate(e.id, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "USER_ENTERED", Data: vals}).Do()
	return err
}

// liveCount builds a sentence whose count is a COUNTIF over one Bilan column,
// so the tab stays right when the data under it moves. countExpr takes the
// column range; rest takes the candidate total. Quotes are doubled because the
// text sits inside a formula string literal.
func (d dashLayout) liveCount(countExpr string, col int, rest, lead string) string {
	count := fmt.Sprintf(countExpr, d.bilanRange(col))
	q := func(t string) string { return strings.ReplaceAll(t, `"`, `""`) }
	return fmt.Sprintf(`="%s"&%s&"%s"`, q(lead), count, q(fmt.Sprintf(rest, d.rows)))
}
