package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"golang.org/x/oauth2/google"

	"github.com/louisb/score-validator/internal/sheetx"
)

// runExport saves one tab as a PDF, so the result can actually be looked at
// rather than inferred from cell values. Formatting bugs — a truncated label,
// a bar that never varies — only show up in the render.
func runExport(e *env, args []string) error {
	fs, _ := subFlags("export", args)
	creds := fs.String("creds-file", "service-account.json", "service account key, for the export token")
	rng := fs.String("range", "A1:L52", "A1 range to render")
	rest := fs.Args()
	if len(rest) != 2 {
		return fmt.Errorf("usage: export <tab> <out.pdf>")
	}
	tab, out := rest[0], rest[1]

	ss, err := e.svc.Spreadsheets.Get(e.id).Do()
	if err != nil {
		return err
	}
	gid, ok := sheetx.SheetID(ss, tab)
	if !ok {
		return fmt.Errorf("tab %q not found", tab)
	}

	key, err := os.ReadFile(*creds)
	if err != nil {
		return err
	}
	cfg, err := google.JWTConfigFromJSON(key, "https://www.googleapis.com/auth/drive.readonly")
	if err != nil {
		return err
	}

	q := url.Values{
		"format": {"pdf"}, "gid": {fmt.Sprint(gid)}, "portrait": {"false"},
		"size": {"A3"}, "fitw": {"true"}, "gridlines": {"false"},
		"printtitle": {"false"}, "sheetnames": {"false"}, "pagenumbers": {"false"},
		"range": {*rng},
	}
	u := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/export?%s", e.id, q.Encode())

	resp, err := cfg.Client(e.ctx).Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("export returned %d: %.200s", resp.StatusCode, b)
	}

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	fmt.Printf("%s (gid %d) -> %s, %d bytes\n", tab, gid, out, n)
	return nil
}
