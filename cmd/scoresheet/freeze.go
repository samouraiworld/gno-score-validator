package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/louisb/score-validator/internal/scoring"
	"github.com/louisb/score-validator/internal/sheetx"
)

// runFreeze copies the final arena tabs and Bilan into data/frozen/, one CSV
// per tab, so the scores are recorded in git and not only in a spreadsheet
// anyone with access can edit. It reads the sheet and writes local files only.
//
// Values are read unformatted: numbers come out with a dot decimal separator
// whatever the sheet's locale, and Bilan's formulas come out as their results.
func runFreeze(e *env, args []string) error {
	_, dry := subFlags("freeze", args)

	dir := e.data("frozen")
	tabs := append([]string{scoring.TabBilan}, scoring.FrozenTabs...)
	if !*dry {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	for _, tab := range tabs {
		rows, err := e.sheetValues(sheetx.Ref(tab))
		if err != nil {
			return err
		}
		path := filepath.Join(dir, strings.ToLower(tab)+".csv")
		fmt.Printf("  %-18s %4d rows -> %s\n", tab, len(rows), path)
		if *dry {
			continue
		}
		if err := writeFrozenCSV(path, rows); err != nil {
			return err
		}
	}
	if *dry {
		fmt.Println("-dry — nothing written.")
	}
	return nil
}

func writeFrozenCSV(path string, rows [][]interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	for _, r := range rows {
		rec := make([]string, len(r))
		for i, c := range r {
			if v, ok := c.(float64); ok {
				rec[i] = strconv.FormatFloat(v, 'f', -1, 64)
			} else {
				rec[i] = fmt.Sprint(c)
			}
		}
		if err := w.Write(rec); err != nil {
			f.Close()
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
