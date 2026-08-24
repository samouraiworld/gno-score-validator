package main

import (
	"fmt"

	"google.golang.org/api/sheets/v4"

	"github.com/louisb/score-validator/internal/scoring"
	"github.com/louisb/score-validator/internal/sheetx"
)

const dashTab = "Dashboard"

// Block positions. Keeping them here means inserting a section is a one-line
// change rather than a hunt through hard-coded row numbers.
const (
	methodBar  = 8
	methodTop  = methodBar + 1
	methodRows = 6
	profBar    = methodTop + methodRows
	profHdr    = profBar + 1 // moniker row, and the COMPONENT header
	profRows   = 7           // component table rows below the header
	onchBar    = profHdr + profRows + 1
	onchHdr    = onchBar + 1
	onchRows   = 12
	stdBar     = onchHdr + onchRows + 1
	stdHdr     = stdBar + 1
	stdFirst   = stdHdr + 1
	helperRow  = 150 // off-screen source for the distribution chart
)

// Palette shared with the web scorecard. The network hues come from the
// gemstones the testnets are named after: topaz gold, sapphire blue.
const (
	cInk       = "#0f1720"
	cInkSoft   = "#56636f"
	cMuted     = "#8794a4"
	cBorder    = "#e0e6ee"
	cSurface2  = "#f4f6f9"
	cChallenge = "#eb6834"
	cTest13    = "#1baf7a"
	cTopaz     = "#eda100"
	cSapphire  = "#2a78d6"
	cGitHub    = "#e87ba4"
	cScaleLow  = "#eaf2fd"
)

// bilanLookup pulls one Bilan column for the validator chosen in the dropdown.
func bilanLookup(col string) string {
	return sheetx.Dashed(fmt.Sprintf(`XLOOKUP($C$%d;%s!$%s$%d:$%s$%d;%s!$%s$%d:$%s$%d)`,
		profHdr,
		sheetx.Ref(scoring.TabBilan), scoring.ColMoniker, scoring.FirstRow, scoring.ColMoniker, scoring.LastRow,
		sheetx.Ref(scoring.TabBilan), col, scoring.FirstRow, col, scoring.LastRow))
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

func runDashboard(e *env, _ []string) error {
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
					GridProperties: &sheets.GridProperties{RowCount: 200, ColumnCount: 14}}}}},
		}).Do()
		if err != nil {
			return err
		}
		id = created.Replies[0].AddSheet.Properties.SheetId
		fmt.Println("created tab", dashTab)
	}

	if err := e.wipeDashboard(ss, id); err != nil {
		return err
	}
	if err := e.writeDashboardValues(); err != nil {
		return err
	}
	fmt.Println("values written")
	if err := e.formatDashboard(id); err != nil {
		return err
	}
	fmt.Println("formatting applied")
	if err := e.dashboardCharts(id); err != nil {
		return err
	}
	fmt.Println("charts added")
	return nil
}

// wipeDashboard clears cells, charts, banding and conditional rules, so the
// command is idempotent. Clearing cells alone leaves banding behind, and a
// second run then fails with "range already has alternating colors".
func (e *env) wipeDashboard(ss *sheets.Spreadsheet, id int64) error {
	reqs := []*sheets.Request{{UpdateCells: &sheets.UpdateCellsRequest{
		Range: sheetx.Range(id, 0, 200, 0, 14), Fields: "*"}}}
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
		Range: sheetx.Range(id, 0, 200, 0, 14)}})
	_, err := e.svc.Spreadsheets.BatchUpdate(e.id,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: reqs}).Do()
	return err
}

func methodText() [][]interface{} {
	return [][]interface{}{
		{fmt.Sprintf("Score  =  ( Test13-challenge + Score-test13 + Score-topaz + Score-sapphire ) / %d   +   GitHub / %d,   capped at %d",
			scoring.ArenaDivisor, scoring.BonusDivisor, scoring.MaxScore)},
		{"The four arenas carry equal weight: each contributes at most 25 points. GitHub is a bonus on top, worth at most 10 — it can lift a candidate, never sink one."},
		{fmt.Sprintf("GitHub score  =  %d × merged PRs  +  %d × unmerged PRs  +  %d × issues opened  +  %d × commits  +  %d × comment threads. A merged pull request is the only artefact proving code landed, so it outweighs intent and discussion.",
			scoring.WeightMergedPR, scoring.WeightUnmergedPR, scoring.WeightIssue,
			scoring.WeightCommit, scoring.WeightComment)},
		{fmt.Sprintf("Only contributions dated %d or later count toward the bonus. One candidate is affected: Santala-Research, whose three gnolang pull requests are typo fixes from 2023 that predate test13 entirely.", scoring.CutoffYear)},
		{"12 of the 98 candidates could be tied to a GitHub account, each verified by hand. A blank GitHub column is absence of evidence, not evidence of absence — it never costs points."},
		{"Fire drill is not scored yet and carries no weight. Adding it later means rebalancing the other four, or totals will run past 100."},
	}
}

// componentRows breaks the total into what each arena contributed.
func componentRows() [][]interface{} {
	comps := []struct {
		label   string
		col     string
		divisor int
		color   string
	}{
		{"Test13 challenge", scoring.ColChallenge, scoring.ArenaDivisor, cChallenge},
		{"Score · test13", scoring.ColTest13, scoring.ArenaDivisor, cTest13},
		{"Score · topaz", scoring.ColTopaz, scoring.ArenaDivisor, cTopaz},
		{"Score · sapphire", scoring.ColSapphire, scoring.ArenaDivisor, cSapphire},
		{"GitHub bonus", scoring.ColGitHub, scoring.BonusDivisor, cGitHub},
	}
	var out [][]interface{}
	for i, c := range comps {
		raw := fmt.Sprintf("$F$%d", profHdr+1+i)
		out = append(out, []interface{}{c.label, "", bilanLookup(c.col),
			fmt.Sprintf(`=IFERROR(%s/%d;"—")`, raw, c.divisor),
			sheetx.SparkBar(raw, c.color, cScaleLow)})
	}
	out = append(out, []interface{}{"Fire drill", "", "—", "—", ""})
	out = append(out, []interface{}{"TOTAL", "", "",
		fmt.Sprintf(`=MIN(%d;SUM($G$%d:$G$%d))`, scoring.MaxScore, profHdr+1, profHdr+len(comps)), ""})
	return out
}

// onchainRows lays out the per-network metric table. Column letters follow the
// network tabs: C score, D tier, E sign, F vp, G proposer, H critical,
// I warning, J freq, K freq_rate, L downtime, M missed.
func onchainRows() [][]interface{} {
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
		for i, net := range scoring.Networks {
			row = append(row, netLookup(net, m.col, m.text))
			if i < len(scoring.Networks)-1 {
				row = append(row, "")
			}
		}
		out = append(out, row)
	}
	return out
}

func (e *env) writeDashboardValues() error {
	bilan := sheetx.Ref(scoring.TabBilan)
	scoreRange := fmt.Sprintf("%s!$%s$%d:$%s$%d", bilan,
		scoring.ColScore, scoring.FirstRow, scoring.ColScore, scoring.LastRow)
	monikerRange := fmt.Sprintf("%s!$%s$%d:$%s$%d", bilan,
		scoring.ColMoniker, scoring.FirstRow, scoring.ColMoniker, scoring.LastRow)

	vals := []*sheets.ValueRange{
		{Range: dashTab + "!A1", Values: [][]interface{}{{"Gno Validator Scorecard"}}},
		{Range: dashTab + "!A2", Values: [][]interface{}{{fmt.Sprintf(
			"%d test13 candidates  ·  Score = (challenge + test13 + topaz + sapphire) / %d  +  GitHub / %d,  capped at %d",
			scoring.NumRows, scoring.ArenaDivisor, scoring.BonusDivisor, scoring.MaxScore)}}},

		{Range: dashTab + "!A4", Values: [][]interface{}{
			{"CANDIDATES", "", "MEDIAN SCORE", "", "TOP SCORE", "", "MEAN SCORE", "", "GITHUB IDENTIFIED"}}},
		{Range: dashTab + "!A5", Values: [][]interface{}{{
			fmt.Sprintf("=COUNTA(%s)", monikerRange), "",
			fmt.Sprintf("=ROUND(MEDIAN(%s);2)", scoreRange), "",
			fmt.Sprintf("=ROUND(MAX(%s);2)", scoreRange), "",
			fmt.Sprintf("=ROUND(AVERAGE(%s);2)", scoreRange), "",
			fmt.Sprintf(`=COUNTIF(%s!%s%d:%s%d;"<>")`, bilan,
				scoring.ColGitHubUser, scoring.FirstRow, scoring.ColGitHubUser, scoring.LastRow)}}},
		{Range: dashTab + "!A6", Values: [][]interface{}{{
			"scored across four arenas", "",
			"half score above, half below", "",
			fmt.Sprintf("=XLOOKUP(MAX(%s);%s;%s)", scoreRange, scoreRange, monikerRange), "",
			"average of all candidates", "",
			"GitHub account verified by hand"}}},

		{Range: fmt.Sprintf("%s!A%d", dashTab, methodBar),
			Values: [][]interface{}{{"HOW THE SCORE IS BUILT"}}},
		{Range: fmt.Sprintf("%s!A%d", dashTab, methodTop), Values: methodText()},

		{Range: fmt.Sprintf("%s!A%d", dashTab, profBar),
			Values: [][]interface{}{{"VALIDATOR PROFILE"}}},
		{Range: fmt.Sprintf("%s!B%d", dashTab, profHdr),
			Values: [][]interface{}{{"Moniker", "ITRocket"}}},
		{Range: fmt.Sprintf("%s!B%d", dashTab, profHdr+1), Values: [][]interface{}{
			{"Discord", bilanLookup(scoring.ColDiscord)},
			{"Rank", fmt.Sprintf(`=IFERROR(RANK($C$%d;%s);"—")`, profHdr+3, scoreRange)},
			{"Score / 100", bilanLookup(scoring.ColScore)},
			{"GitHub login", bilanLookup(scoring.ColGitHubUser)},
			{"Match confidence", bilanLookup(scoring.ColConfidence)},
		}},
		{Range: fmt.Sprintf("%s!D%d", dashTab, profHdr),
			Values: [][]interface{}{{"COMPONENT", "", "RAW", "POINTS", ""}}},
		{Range: fmt.Sprintf("%s!D%d", dashTab, profHdr+1), Values: componentRows()},

		{Range: fmt.Sprintf("%s!A%d", dashTab, onchBar),
			Values: [][]interface{}{{"ON-CHAIN DETAIL  ·  current year"}}},
		{Range: fmt.Sprintf("%s!B%d", dashTab, onchHdr), Values: [][]interface{}{
			{"METRIC", "", scoring.Networks[0][6:], "", scoring.Networks[1][6:], "", scoring.Networks[2][6:]}}},
		{Range: fmt.Sprintf("%s!B%d", dashTab, onchHdr+1), Values: onchainRows()},

		{Range: fmt.Sprintf("%s!A%d", dashTab, stdBar), Values: [][]interface{}{{"STANDINGS"}}},
		{Range: fmt.Sprintf("%s!A%d", dashTab, stdHdr), Values: [][]interface{}{{
			"#", "Moniker", "Discord", "Challenge", "test13", "topaz", "sapphire",
			"GitHub", "Score", "GitHub login", "Score bar"}}},
		// One spilling formula keeps the table live: edit Bilan and it reorders.
		{Range: fmt.Sprintf("%s!A%d", dashTab, stdFirst), Values: [][]interface{}{{
			fmt.Sprintf("=SEQUENCE(%d)", scoring.NumRows),
			fmt.Sprintf("=SORT(CHOOSECOLS(%s!A%d:K%d;1;2;3;4;5;6;8;9;10);8;FALSE)",
				bilan, scoring.FirstRow, scoring.LastRow)}}},

		{Range: fmt.Sprintf("%s!M%d", dashTab, helperRow),
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
		Range: fmt.Sprintf("%s!M%d", dashTab, helperRow+1), Values: bands})

	var sparks [][]interface{}
	for r := stdFirst; r < stdFirst+scoring.NumRows; r++ {
		sparks = append(sparks, []interface{}{
			sheetx.SparkBar(fmt.Sprintf("I%d", r), cSapphire, cScaleLow)})
	}
	vals = append(vals, &sheets.ValueRange{
		Range: fmt.Sprintf("%s!K%d", dashTab, stdFirst), Values: sparks})

	_, err := e.svc.Spreadsheets.Values.BatchUpdate(e.id, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "USER_ENTERED", Data: vals}).Do()
	return err
}
