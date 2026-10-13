package main

import (
	"encoding/csv"
	"fmt"
	"os"

	"github.com/louisb/score-validator/internal/scoring"
	"github.com/louisb/score-validator/internal/sheetx"
)

// runFreeze copies the arena tabs named in config/arenas.json into
// data/arenas/<id>.csv, and Bilan's moniker and Discord columns into
// data/candidates.csv, so the scores are recorded in git and not only in a
// spreadsheet anyone with access can edit. It reads the sheet and writes local
// files only.
//
// Values are read unformatted: numbers come out with a dot decimal separator
// whatever the sheet's locale.
func runFreeze(e *env, args []string) error {
	_, dry := subFlags("freeze", args)

	cfg, err := e.arenas()
	if err != nil {
		return err
	}
	if !*dry {
		if err := os.MkdirAll(e.data("arenas"), 0o755); err != nil {
			return err
		}
	}
	for _, a := range cfg.Arenas {
		rows, err := e.sheetValues(sheetx.Ref(a.Tab))
		if err != nil {
			return err
		}
		path := a.File(e.data(""))
		fmt.Printf("  %-18s %4d rows -> %s\n", a.Tab, len(rows), path)
		if *dry {
			continue
		}
		if err := writeFrozenCSV(path, rows); err != nil {
			return err
		}
	}

	rows, err := e.sheetValues(fmt.Sprintf("%s!%s%d:%s%d", sheetx.Ref(scoring.TabBilan),
		scoring.ColMoniker, scoring.FirstRow, scoring.ColDiscord, scoring.LastRow))
	if err != nil {
		return err
	}
	path := e.data("candidates.csv")
	fmt.Printf("  %-18s %4d rows -> %s\n", scoring.TabBilan, len(rows), path)
	if *dry {
		fmt.Println("-dry — nothing written.")
		return nil
	}
	return writeFrozenCSV(path, append([][]interface{}{{"moniker", "discord"}}, rows...))
}

func writeFrozenCSV(path string, rows [][]interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.WriteAll(textRows(rows)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
