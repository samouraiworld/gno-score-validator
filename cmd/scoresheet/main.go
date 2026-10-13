// Command scoresheet computes the validator scorecard from the files in this
// repo, and writes it out to the "Score Gno-Validator" spreadsheet.
//
//	scoresheet [-root .] <command> [flags]                        (offline)
//	scoresheet -creds key.json -id <spreadsheet> <command> [flags] (sheet)
//
// Commands:
//
//	score       compute scorecard.json, the page and the scoreboard from the repo files
//	render      regenerate the scoreboard and the page payload from scorecard.json
//	fetch       write a chain's live gnomonitoring report to data/arenas/
//	firedrill   rebuild the fire drill arena CSV from the portal exports
//	sheet diff  list every cell where the sheet differs from the repo (read-only)
//	sheet push  write the repo into the sheet, rebuild the Dashboard, lock it
//	sheet lock  protect every tab so only the service account can edit it
//	freeze      copy the arena tabs back into data/arenas/ (reads the sheet)
//	audit       report numbers stored as text, tab by tab (read-only)
//	dump        print cell ranges (read-only)
//	export      save a tab as PDF, to eyeball the result
//
// The repo is the source of truth and the sheet an output. Every writing
// command accepts -dry, which prints the plan and changes nothing. Start
// there. The arenas are final: fetch and firedrill refuse to rewrite them
// without -force.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/api/sheets/v4"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/sheetx"
)

type env struct {
	ctx   context.Context
	svc   *sheets.Service
	id    string
	root  string // repo root, for data files
	creds string // key file, whose client_email sheet lock names as editor
}

func main() {
	creds := flag.String("creds", "service-account.json", "Google service account key file")
	id := flag.String("id", "", "spreadsheet id (required)")
	root := flag.String("root", ".", "repo root, for files under data/")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	// score works from the files in the repo alone: no sheet, no key.
	if args[0] == "score" {
		if err := runScore(*root, args[1:]); err != nil {
			fatal(err)
		}
		return
	}
	// render works from the committed snapshot alone: no sheet, no key.
	if args[0] == "render" {
		if err := runRender(*root, args[1:]); err != nil {
			fatal(err)
		}
		return
	}
	// fetch reads gnomonitoring, which is public, and writes a CSV.
	if args[0] == "fetch" {
		if err := runFetch(*root, args[1:]); err != nil {
			fatal(err)
		}
		return
	}
	// firedrill reads the portal exports and writes a CSV.
	if args[0] == "firedrill" {
		if err := runFiredrill(&env{root: *root}, args[1:]); err != nil {
			fatal(err)
		}
		return
	}
	if next, ok := retired[args[0]]; ok {
		fatal(fmt.Errorf("%s is retired: the sheet is an output of the repo now; %s", args[0], next))
	}
	if *id == "" {
		fmt.Fprintln(os.Stderr, "error: -id is required")
		os.Exit(2)
	}

	ctx := context.Background()
	svc, err := sheetx.New(ctx, *creds)
	if err != nil {
		fatal(err)
	}
	e := &env{ctx: ctx, svc: svc, id: *id, root: *root, creds: *creds}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "audit":
		err = runAudit(e, rest)
	case "dump":
		err = runDump(e, rest)
	case "freeze":
		err = runFreeze(e, rest)
	case "export":
		err = runExport(e, rest)
	case "sheet":
		err = runSheet(e, rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fatal(err)
	}
}

// retired names each command the move to the repo as source of truth
// removed, and what replaces it.
var retired = map[string]string{
	"snapshot":  "run score, then sheet push",
	"bilan":     "edit data/github-links.csv, run score, then sheet push",
	"refresh":   "run fetch -chain <id>, score, then sheet push",
	"coerce":    "sheet push writes numbers, never text; audit still checks",
	"dashboard": "sheet push rebuilds it",
}

func (e *env) data(name string) string { return filepath.Join(e.root, "data", name) }

// arenas reads config/arenas.json, the list of scored arenas.
func (e *env) arenas() (arena.Config, error) {
	return arena.LoadConfig(filepath.Join(e.root, "config", "arenas.json"))
}

func usage() {
	fmt.Fprint(os.Stderr, `scoresheet [-root .] <command> [flags]                         (offline)
scoresheet -creds key.json -id <spreadsheet> <command> [flags]  (sheet)

Offline, from config/ and data/ (no -id):
  score       compute data/scorecard.json, re-embed it in the page and
              regenerate the scoreboard                               [-dry]
  render      regenerate scoreboard/ and the page payload from data/scorecard.json
  fetch       write a chain's live gnomonitoring report to its data/arenas/ CSV,
              chosen by the chain field of config/arenas.json; then run score
                                        -chain <id> [-dry] [-force]
  firedrill   rebuild the fire drill arena CSV from the portal exports;
              then run score                          [-dry] [-force]

The sheet, an output of the repo:
  sheet diff  list every cell where the sheet differs from data/scorecard.json and
              the arena CSVs; exits 1 when one does (read-only)
  sheet push  write Bilan and the arena tabs as values, rebuild the Dashboard,
              note and lock every tab; refuses when sheet diff finds a
              difference                              [-dry] [-force]
  sheet lock  protect every tab, editable by the service account only;
              the file owner keeps edit rights regardless  [-dry]
  freeze      copy the arena tabs to data/arenas/*.csv and Bilan A:B to
              data/candidates.csv, to bring a sheet edit into a pull request [-dry]
  audit       report numbers stored as text (read-only)
  dump        print cell ranges, e.g. dump "Bilan!A1:L3"  (read-only)
  export      save a tab as PDF, e.g. export Dashboard out.pdf

Arenas are final. fetch and firedrill refuse to rewrite them without -force.

The spreadsheet must be shared with the service account's client_email as an
Editor. Read it from the key file with:

  python3 -c "import json;print(json.load(open('service-account.json'))['client_email'])"
`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// subFlags builds a flag set for a subcommand, sharing the top-level usage.
func subFlags(name string, args []string) (*flag.FlagSet, *bool) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	dry := fs.Bool("dry", false, "print the plan without writing")
	_ = fs.Parse(args)
	return fs, dry
}
