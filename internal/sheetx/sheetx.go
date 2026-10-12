// Package sheetx wraps the bits of the Sheets API this tool leans on, and
// records the locale rules that make or break every formula it writes.
//
// The target spreadsheet is fr_FR. Three consequences follow, and each one
// produced a silent wrong answer before it was pinned down:
//
//   - Formulas sent through USER_ENTERED are parsed in the sheet's locale, so
//     the argument separator is ';' and not ','. A formula with commas comes
//     back as #ERROR! on every row.
//   - Array literals separate columns with '\' and rows with ';'. SPARKLINE's
//     options array is the usual victim.
//   - A sheet name containing a hyphen — "Score-test13" — must be quoted, or
//     the reference parses as the subtraction Score minus test13!A1 and yields
//     a plausible-looking wrong number instead of an error.
//
// Weights are written as fractions (/6, /10) rather than 0.25 and 0.1 so the
// decimal separator never enters a formula at all.
package sheetx

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// New builds a Sheets client from a Google service account key file. The
// spreadsheet must be shared with that account's client_email as an Editor.
func New(ctx context.Context, credentialsFile string) (*sheets.Service, error) {
	return sheets.NewService(ctx, option.WithCredentialsFile(credentialsFile))
}

// Ref quotes a sheet name for use in a formula. Always route tab names through
// it: unquoted hyphens turn a reference into arithmetic.
func Ref(tab string) string {
	return "'" + strings.ReplaceAll(tab, "'", "''") + "'"
}

// Color converts "#rrggbb" into the API's float-triple colour.
func Color(hex string) *sheets.Color {
	hex = strings.TrimPrefix(hex, "#")
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return &sheets.Color{}
	}
	return &sheets.Color{
		Red:   float64((v>>16)&0xff) / 255,
		Green: float64((v>>8)&0xff) / 255,
		Blue:  float64(v&0xff) / 255,
	}
}

// Range builds a GridRange from zero-based, half-open row and column bounds.
func Range(sheetID int64, r1, r2, c1, c2 int64) *sheets.GridRange {
	return &sheets.GridRange{
		SheetId:          sheetID,
		StartRowIndex:    r1,
		EndRowIndex:      r2,
		StartColumnIndex: c1,
		EndColumnIndex:   c2,
	}
}

// ColumnLetter turns a zero-based column index into its A1 letter.
func ColumnLetter(i int) string {
	s := ""
	for i >= 0 {
		s = string(rune('A'+i%26)) + s
		i = i/26 - 1
	}
	return s
}

// SheetID returns the numeric id of a tab, and whether it exists.
func SheetID(ss *sheets.Spreadsheet, title string) (int64, bool) {
	for _, s := range ss.Sheets {
		if s.Properties.Title == title {
			return s.Properties.SheetId, true
		}
	}
	return 0, false
}

// Format is a RepeatCell request whose field mask is relative to CellData.
// Callers pass bare CellFormat field names; the userEnteredFormat prefix the
// API demands is added here, because forgetting it is a 400, not a no-op.
func Format(sheetID int64, r1, r2, c1, c2 int64, f *sheets.CellFormat, fields string) *sheets.Request {
	parts := strings.Split(fields, ",")
	for i, p := range parts {
		parts[i] = "userEnteredFormat." + strings.TrimSpace(p)
	}
	return &sheets.Request{RepeatCell: &sheets.RepeatCellRequest{
		Range:  Range(sheetID, r1, r2, c1, c2),
		Cell:   &sheets.CellData{UserEnteredFormat: f},
		Fields: strings.Join(parts, ","),
	}}
}

// Merge merges a rectangle into a single cell.
func Merge(sheetID int64, r1, r2, c1, c2 int64) *sheets.Request {
	return &sheets.Request{MergeCells: &sheets.MergeCellsRequest{
		Range: Range(sheetID, r1, r2, c1, c2), MergeType: "MERGE_ALL"}}
}

// ColumnWidth sets a pixel width across a span of columns.
func ColumnWidth(sheetID int64, c1, c2, px int64) *sheets.Request {
	return dimension(sheetID, "COLUMNS", c1, c2, px)
}

// RowHeight sets a pixel height across a span of rows.
func RowHeight(sheetID int64, r1, r2, px int64) *sheets.Request {
	return dimension(sheetID, "ROWS", r1, r2, px)
}

func dimension(sheetID int64, dim string, a, b, px int64) *sheets.Request {
	return &sheets.Request{UpdateDimensionProperties: &sheets.UpdateDimensionPropertiesRequest{
		Range: &sheets.DimensionRange{SheetId: sheetID, Dimension: dim,
			StartIndex: a, EndIndex: b},
		Properties: &sheets.DimensionProperties{PixelSize: px},
		Fields:     "pixelSize",
	}}
}

// Text is a bold TextFormat at a given size and colour.
func Text(bold bool, size int64, hex string) *sheets.TextFormat {
	return &sheets.TextFormat{Bold: bold, FontSize: size, ForegroundColor: Color(hex)}
}

// SparkBar renders an in-cell progress bar out of 100.
//
// A single-value bar always fills its cell — the "max" option is ignored — so
// the value is paired with its remainder and the remainder painted as a track.
func SparkBar(ref, fill, track string) string {
	return fmt.Sprintf(
		`=IF(%s="";"";SPARKLINE({%s\MAX(0;100-%s)};{"charttype"\"bar";"color1"\"%s";"color2"\"%s"}))`,
		ref, ref, ref, fill, track)
}

// Numeric coerces a lookup result that may have been stored as text with a '.'
// decimal separator, which is how a US-formatted import lands in an fr_FR sheet.
func Numeric(expr string) string {
	return fmt.Sprintf(
		`=IFERROR(LET(v;%s;IF(ISNUMBER(v);v;VALUE(SUBSTITUTE(v;".";","))));"—")`, expr)
}

// Dashed returns expr, or an em dash when it resolves to an error or a blank.
func Dashed(expr string) string {
	return fmt.Sprintf(`=IFERROR(LET(v;%s;IF(v="";"—";v));"—")`, expr)
}
