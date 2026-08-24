// Command scoresheet reads and rewrites the "Score Gno-Validator" spreadsheet.
//
//	scoresheet -creds key.json -id <spreadsheet> <command> [flags]
//
// Commands:
//
//	audit       report numbers stored as text, tab by tab (read-only)
//	dump        print cell ranges (read-only)
//	coerce      convert those text numbers into real numbers
//	bilan       rewrite the Score column and the GitHub audit columns
//	dashboard   build the Dashboard tab from scratch
//	export      save a tab as PDF, to eyeball the result
//
// Every writing command accepts -dry, which prints the plan and changes
// nothing. Start there.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/api/sheets/v4"

	"github.com/louisb/score-validator/internal/sheetx"
)

type env struct {
	ctx  context.Context
	svc  *sheets.Service
	id   string
	root string // repo root, for data files
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
	if *id == "" {
		fmt.Fprintln(os.Stderr, "error: -id is required")
		os.Exit(2)
	}

	ctx := context.Background()
	svc, err := sheetx.New(ctx, *creds)
	if err != nil {
		fatal(err)
	}
	e := &env{ctx: ctx, svc: svc, id: *id, root: *root}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "audit":
		err = runAudit(e, rest)
	case "dump":
		err = runDump(e, rest)
	case "coerce":
		err = runCoerce(e, rest)
	case "bilan":
		err = runBilan(e, rest)
	case "dashboard":
		err = runDashboard(e, rest)
	case "export":
		err = runExport(e, rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fatal(err)
	}
}

func (e *env) data(name string) string { return filepath.Join(e.root, "data", name) }

func usage() {
	fmt.Fprint(os.Stderr, `scoresheet -creds key.json -id <spreadsheet> <command> [flags]

  audit       report numbers stored as text (read-only)
  dump        print cell ranges, e.g. dump "Bilan!A1:K3"  (read-only)
  coerce      convert text numbers into real numbers      [-dry]
  bilan       rewrite Score + GitHub audit columns         [-dry]
  dashboard   rebuild the Dashboard tab
  export      save a tab as PDF, e.g. export Dashboard out.pdf

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
