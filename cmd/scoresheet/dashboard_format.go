package main

import (
	"google.golang.org/api/sheets/v4"

	"github.com/louisb/score-validator/internal/scoring"
	"github.com/louisb/score-validator/internal/sheetx"
)

func (e *env) formatDashboard(id int64) error {
	fmtc := func(r1, r2, c1, c2 int64, f *sheets.CellFormat, fields string) *sheets.Request {
		return sheetx.Format(id, r1, r2, c1, c2, f, fields)
	}
	merge := func(r1, r2, c1, c2 int64) *sheets.Request { return sheetx.Merge(id, r1, r2, c1, c2) }
	last := int64(stdFirst - 1 + scoring.NumRows)

	reqs := []*sheets.Request{
		{UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
			Properties: &sheets.SheetProperties{SheetId: id,
				GridProperties: &sheets.GridProperties{HideGridlines: true, FrozenRowCount: 6},
				TabColor:       sheetx.Color(cSapphire)},
			Fields: "gridProperties.hideGridlines,gridProperties.frozenRowCount,tabColor"}},

		// title band
		merge(0, 1, 0, 11), merge(1, 2, 0, 11),
		sheetx.RowHeight(id, 0, 1, 52), sheetx.RowHeight(id, 1, 2, 26),
		fmtc(0, 1, 0, 11, &sheets.CellFormat{
			BackgroundColor: sheetx.Color(cInk), TextFormat: sheetx.Text(true, 20, "#ffffff"),
			VerticalAlignment: "MIDDLE", Padding: &sheets.Padding{Left: 14},
		}, "backgroundColor,textFormat,verticalAlignment,padding"),
		fmtc(1, 2, 0, 11, &sheets.CellFormat{
			BackgroundColor: sheetx.Color(cInk), TextFormat: sheetx.Text(false, 10, "#9ba8b9"),
			VerticalAlignment: "MIDDLE", Padding: &sheets.Padding{Left: 14},
		}, "backgroundColor,textFormat,verticalAlignment,padding"),

		// KPI row
		sheetx.RowHeight(id, 3, 4, 20), sheetx.RowHeight(id, 4, 5, 38), sheetx.RowHeight(id, 5, 6, 20),
		fmtc(3, 4, 0, 9, &sheets.CellFormat{TextFormat: sheetx.Text(true, 8, cMuted)}, "textFormat"),
		fmtc(4, 5, 0, 9, &sheets.CellFormat{
			TextFormat: sheetx.Text(true, 20, cInk), VerticalAlignment: "MIDDLE",
		}, "textFormat,verticalAlignment"),
		fmtc(5, 6, 0, 9, &sheets.CellFormat{TextFormat: sheetx.Text(false, 9, cInkSoft)}, "textFormat"),

		// explanation: prose, not a data grid
		fmtc(methodTop-1, methodTop-1+methodRows, 0, 11, &sheets.CellFormat{
			TextFormat: sheetx.Text(false, 9, cInkSoft), WrapStrategy: "WRAP",
			VerticalAlignment: "MIDDLE", Padding: &sheets.Padding{Left: 6, Top: 2, Bottom: 2},
		}, "textFormat,wrapStrategy,verticalAlignment,padding"),
		fmtc(methodTop-1, methodTop, 0, 11, &sheets.CellFormat{
			TextFormat: sheetx.Text(true, 10, cInk)}, "textFormat"),

		// profile labels (col B) and values (col C)
		fmtc(profHdr, profHdr+5, 1, 2, &sheets.CellFormat{
			TextFormat: sheetx.Text(false, 0, cInkSoft)}, "textFormat"),
		fmtc(profHdr, profHdr+5, 2, 3, &sheets.CellFormat{
			TextFormat: sheetx.Text(true, 10, cInk), HorizontalAlignment: "LEFT",
		}, "textFormat,horizontalAlignment"),
		fmtc(profHdr-1, profHdr, 1, 2, &sheets.CellFormat{
			TextFormat: sheetx.Text(false, 0, cInkSoft)}, "textFormat"),
		fmtc(profHdr-1, profHdr, 2, 3, &sheets.CellFormat{
			BackgroundColor: sheetx.Color("#ffffff"), TextFormat: sheetx.Text(true, 11, cSapphire),
			Borders: box(cSapphire),
		}, "backgroundColor,textFormat,borders"),

		// component table
		fmtc(profHdr-1, profHdr, 3, 8, &sheets.CellFormat{
			TextFormat: sheetx.Text(true, 8, cMuted), Borders: underline(cBorder),
		}, "textFormat,borders"),
		fmtc(profHdr, profHdr+profRows, 5, 7, &sheets.CellFormat{
			HorizontalAlignment: "RIGHT",
			NumberFormat:        &sheets.NumberFormat{Type: "NUMBER", Pattern: "0.00"},
		}, "horizontalAlignment,numberFormat"),
		fmtc(profHdr+profRows-1, profHdr+profRows, 3, 8, &sheets.CellFormat{
			TextFormat: sheetx.Text(true, 10, cInk), Borders: overline(cInk),
		}, "textFormat,borders"),

		// on-chain table
		fmtc(onchHdr-1, onchHdr, 1, 9, &sheets.CellFormat{
			TextFormat: sheetx.Text(true, 8, cMuted), HorizontalAlignment: "CENTER",
			Borders: underline(cBorder),
		}, "textFormat,horizontalAlignment,borders"),
		fmtc(onchHdr-1, onchHdr, 1, 2, &sheets.CellFormat{
			HorizontalAlignment: "LEFT"}, "horizontalAlignment"),
		fmtc(onchHdr, onchHdr+onchRows, 1, 2, &sheets.CellFormat{
			TextFormat: sheetx.Text(false, 0, cInkSoft)}, "textFormat"),
		fmtc(onchHdr, onchHdr+onchRows, 3, 9, &sheets.CellFormat{
			HorizontalAlignment: "CENTER", TextFormat: sheetx.Text(true, 10, cInk),
		}, "horizontalAlignment,textFormat"),
		fmtc(onchHdr+2, onchHdr+3, 3, 9, &sheets.CellFormat{
			NumberFormat: &sheets.NumberFormat{Type: "NUMBER", Pattern: `0.0"%"`}}, "numberFormat"),
		fmtc(onchHdr+8, onchHdr+9, 3, 9, &sheets.CellFormat{
			NumberFormat: &sheets.NumberFormat{Type: "NUMBER", Pattern: "0.000"}}, "numberFormat"),
		fmtc(onchHdr+11, onchHdr+12, 3, 9, &sheets.CellFormat{
			TextFormat: sheetx.Text(false, 7, cMuted), WrapStrategy: "WRAP",
		}, "textFormat,wrapStrategy"),

		// standings
		fmtc(stdHdr-1, stdHdr, 0, 11, &sheets.CellFormat{
			BackgroundColor: sheetx.Color(cInk), TextFormat: sheetx.Text(true, 9, "#ffffff"),
			HorizontalAlignment: "CENTER", VerticalAlignment: "MIDDLE",
		}, "backgroundColor,textFormat,horizontalAlignment,verticalAlignment"),
		sheetx.RowHeight(id, stdHdr-1, stdHdr, 28),
		// Components keep the automatic format: an explicit "0.##" pattern
		// leaves a dangling decimal separator on whole numbers.
		fmtc(stdFirst-1, last, 3, 8, &sheets.CellFormat{
			HorizontalAlignment: "RIGHT"}, "horizontalAlignment"),
		fmtc(stdFirst-1, last, 8, 9, &sheets.CellFormat{
			HorizontalAlignment: "RIGHT", TextFormat: sheetx.Text(true, 10, cInk),
			NumberFormat: &sheets.NumberFormat{Type: "NUMBER", Pattern: "0.00"},
		}, "horizontalAlignment,textFormat,numberFormat"),
		fmtc(stdFirst-1, last, 0, 1, &sheets.CellFormat{
			HorizontalAlignment: "CENTER", TextFormat: sheetx.Text(false, 0, cMuted),
		}, "horizontalAlignment,textFormat"),
		fmtc(stdFirst-1, last, 1, 2, &sheets.CellFormat{
			TextFormat: sheetx.Text(true, 10, cInk)}, "textFormat"),
		fmtc(stdFirst-1, last, 2, 3, &sheets.CellFormat{
			TextFormat: sheetx.Text(false, 0, cInkSoft)}, "textFormat"),
		fmtc(stdFirst-1, last, 9, 10, &sheets.CellFormat{
			TextFormat: sheetx.Text(false, 9, cInkSoft)}, "textFormat"),

		fmtc(helperRow-1, helperRow, 12, 14, &sheets.CellFormat{
			TextFormat: sheetx.Text(true, 9, cMuted)}, "textFormat"),

		sheetx.ColumnWidth(id, 0, 1, 46), sheetx.ColumnWidth(id, 1, 2, 190),
		sheetx.ColumnWidth(id, 2, 3, 160), sheetx.ColumnWidth(id, 3, 9, 84),
		sheetx.ColumnWidth(id, 9, 10, 140), sheetx.ColumnWidth(id, 10, 11, 120),
		sheetx.ColumnWidth(id, 11, 12, 24), sheetx.ColumnWidth(id, 12, 14, 110),

		{AddBanding: &sheets.AddBandingRequest{BandedRange: &sheets.BandedRange{
			Range: sheetx.Range(id, stdFirst-1, last, 0, 11),
			RowProperties: &sheets.BandingProperties{
				FirstBandColor:  sheetx.Color("#ffffff"),
				SecondBandColor: sheetx.Color("#f8fafc")}}}},

		// Score is a magnitude, so the scale is one hue, light to dark.
		{AddConditionalFormatRule: &sheets.AddConditionalFormatRuleRequest{
			Rule: &sheets.ConditionalFormatRule{
				Ranges: []*sheets.GridRange{sheetx.Range(id, stdFirst-1, last, 8, 9)},
				GradientRule: &sheets.GradientRule{
					Minpoint: &sheets.InterpolationPoint{Color: sheetx.Color(cScaleLow), Type: "MIN"},
					Maxpoint: &sheets.InterpolationPoint{Color: sheetx.Color(cSapphire), Type: "MAX"},
				}}}},

		{SetDataValidation: &sheets.SetDataValidationRequest{
			Range: sheetx.Range(id, profHdr-1, profHdr, 2, 3),
			Rule: &sheets.DataValidationRule{
				Condition: &sheets.BooleanCondition{Type: "ONE_OF_RANGE",
					Values: []*sheets.ConditionValue{{UserEnteredValue: fmt_(scoring.TabBilan)}}},
				ShowCustomUi: true, Strict: true,
				InputMessage: "Pick a validator to fill the panel below."}}},
	}

	for _, bar := range []int64{methodBar, profBar, onchBar, stdBar} {
		reqs = append(reqs,
			fmtc(bar-1, bar, 0, 11, &sheets.CellFormat{
				BackgroundColor: sheetx.Color(cSurface2), TextFormat: sheetx.Text(true, 10, cInk),
				Padding: &sheets.Padding{Left: 6},
			}, "backgroundColor,textFormat,padding"),
			merge(bar-1, bar, 0, 11))
	}
	for r := int64(methodTop - 1); r < methodTop-1+methodRows; r++ {
		reqs = append(reqs, merge(r, r+1, 0, 11))
	}
	for r := int64(profHdr - 1); r < profHdr+profRows; r++ {
		reqs = append(reqs, merge(r, r+1, 3, 5))
	}
	for r := int64(onchHdr - 1); r < onchHdr+onchRows; r++ {
		reqs = append(reqs, merge(r, r+1, 3, 5), merge(r, r+1, 5, 7), merge(r, r+1, 7, 9))
	}

	_, err := e.svc.Spreadsheets.BatchUpdate(e.id,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: reqs}).Do()
	return err
}

// fmt_ builds the dropdown source range over the Bilan monikers.
func fmt_(tab string) string {
	return "=" + sheetx.Ref(tab) + "!$" + scoring.ColMoniker + "$" +
		itoa(scoring.FirstRow) + ":$" + scoring.ColMoniker + "$" + itoa(scoring.LastRow)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func box(hex string) *sheets.Borders {
	b := &sheets.Border{Style: "SOLID", Color: sheetx.Color(hex)}
	return &sheets.Borders{Top: b, Bottom: b, Left: b, Right: b}
}

func underline(hex string) *sheets.Borders {
	return &sheets.Borders{Bottom: &sheets.Border{Style: "SOLID", Color: sheetx.Color(hex)}}
}

func overline(hex string) *sheets.Borders {
	return &sheets.Borders{Top: &sheets.Border{Style: "SOLID", Color: sheetx.Color(hex)}}
}

func (e *env) dashboardCharts(id int64) error {
	anchor := func(row, col int64) *sheets.EmbeddedObjectPosition {
		return &sheets.EmbeddedObjectPosition{OverlayPosition: &sheets.OverlayPosition{
			AnchorCell:  &sheets.GridCoordinate{SheetId: id, RowIndex: row, ColumnIndex: col},
			WidthPixels: 540, HeightPixels: 400}}
	}
	reqs := []*sheets.Request{
		{AddChart: &sheets.AddChartRequest{Chart: &sheets.EmbeddedChart{
			Position: anchor(3, 12),
			Spec: &sheets.ChartSpec{
				Title: "Top 15 by score", Subtitle: "points out of 100",
				BasicChart: &sheets.BasicChartSpec{
					ChartType: "BAR", LegendPosition: "NO_LEGEND",
					Axis: []*sheets.BasicChartAxis{{Position: "BOTTOM_AXIS", Title: "Score"}},
					Domains: []*sheets.BasicChartDomain{{Domain: &sheets.ChartData{
						SourceRange: &sheets.ChartSourceRange{Sources: []*sheets.GridRange{
							sheetx.Range(id, stdFirst-1, stdFirst-1+15, 1, 2)}}}}},
					Series: []*sheets.BasicChartSeries{{
						Series: &sheets.ChartData{SourceRange: &sheets.ChartSourceRange{
							Sources: []*sheets.GridRange{
								sheetx.Range(id, stdFirst-1, stdFirst-1+15, 8, 9)}}},
						Color: sheetx.Color(cSapphire)}}}}}}},
		{AddChart: &sheets.AddChartRequest{Chart: &sheets.EmbeddedChart{
			Position: anchor(22, 12),
			Spec: &sheets.ChartSpec{
				Title: "Score distribution", Subtitle: "candidates per 20-point band",
				BasicChart: &sheets.BasicChartSpec{
					ChartType: "COLUMN", LegendPosition: "NO_LEGEND",
					Domains: []*sheets.BasicChartDomain{{Domain: &sheets.ChartData{
						SourceRange: &sheets.ChartSourceRange{Sources: []*sheets.GridRange{
							sheetx.Range(id, helperRow, helperRow+5, 12, 13)}}}}},
					Series: []*sheets.BasicChartSeries{{
						Series: &sheets.ChartData{SourceRange: &sheets.ChartSourceRange{
							Sources: []*sheets.GridRange{
								sheetx.Range(id, helperRow, helperRow+5, 13, 14)}}},
						Color: sheetx.Color(cTest13)}}}}}}},
	}
	_, err := e.svc.Spreadsheets.BatchUpdate(e.id,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: reqs}).Do()
	return err
}
