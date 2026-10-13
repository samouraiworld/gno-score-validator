package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/api/sheets/v4"
)

// printKey names the developer metadata where a push records its print.
const printKey = "scoresheet-push"

// pushPrint is what a successful push leaves in the spreadsheet's developer
// metadata: when it ran, and a hash of each tab it owns as read back right
// after writing. The next push compares the sheet with this print, not with
// the repo, to tell a repo change (the sheet still holds what was pushed) from
// an edit made in the sheet.
//
// The metadata is stored with PROJECT visibility on the spreadsheet itself:
// no one sees or edits it from the Sheets UI, only this service account's
// project through the API.
type pushPrint struct {
	PushedAt string            `json:"pushed_at"`
	Tabs     map[string]string `json:"tabs"` // tab name -> tabHash
}

// tabHash fingerprints a tab as read back unformatted. Trailing blank cells
// and rows are dropped, since the API leaves them out as it pleases; a number
// and the same digits stored as text hash differently.
func tabHash(rows [][]interface{}) string {
	canon := [][]string{}
	for _, r := range trimBlankRows(rows) {
		cells := []string{}
		for _, c := range r {
			cells = append(cells, cellPrint(c))
		}
		for len(cells) > 0 && cells[len(cells)-1] == "" {
			cells = cells[:len(cells)-1]
		}
		canon = append(canon, cells)
	}
	b, _ := json.Marshal(canon)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// cellPrint renders a cell with its type, "" for a blank one.
func cellPrint(c interface{}) string {
	switch v := c.(type) {
	case nil:
		return ""
	case float64:
		return "n" + strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		return "b" + strconv.FormatBool(v)
	}
	if s := fmt.Sprint(c); s != "" {
		return "s" + s
	}
	return ""
}

// editedSince lists the tabs the repo owns (want) whose sheet content no
// longer matches the last push. current holds the hash of every wanted tab
// present in the sheet. A tab the last push wrote and the sheet lost was
// deleted; a tab the sheet holds but the last push did not write was made by
// hand. A wanted tab absent from both is new in the repo, not an edit.
func editedSince(last *pushPrint, current map[string]string, want []string) []string {
	var edited []string
	for _, name := range want {
		h, present := current[name]
		r, recorded := last.Tabs[name]
		switch {
		case present && recorded && h != r:
			edited = append(edited, name)
		case present && !recorded:
			edited = append(edited, name+" (not in the last push)")
		case !present && recorded:
			edited = append(edited, name+" (deleted)")
		}
	}
	return edited
}

// pushGate decides whether push may overwrite the sheet, given how many
// places differ from the repo, the last push's print (nil when none was
// recorded) and the tabs edited since. It returns the line to print when the
// push goes ahead.
//
// A sheet that matches the repo, or still holds exactly what the last push
// wrote, has nothing to lose. Otherwise the difference may be an edit made
// in the sheet, and only -force overwrites it.
func pushGate(diffs int, last *pushPrint, edited []string, force bool) (string, error) {
	switch {
	case diffs == 0:
		return "the sheet matches the repo; rewriting it anyway, with the lock.", nil
	case last == nil && !force:
		return "", fmt.Errorf("the sheet differs from the repo in %d place(s) and no push recorded what it held: "+
			"rerun with -force once the list above holds only changes made in the repo", diffs)
	case last == nil:
		return fmt.Sprintf("no push recorded; %d difference(s) above will be overwritten (-force).", diffs), nil
	case len(edited) > 0 && !force:
		return "", fmt.Errorf("the sheet was edited since the last push (%s): %s; refusing to overwrite it: "+
			"bring the edit into the repo (freeze, then a pull request), or rerun with -force to discard it",
			last.PushedAt, strings.Join(edited, ", "))
	case len(edited) > 0:
		return fmt.Sprintf("edited since the last push: %s; %d difference(s) above will be overwritten (-force).",
			strings.Join(edited, ", "), diffs), nil
	}
	return fmt.Sprintf("the sheet holds the last push (%s); %d difference(s) above come from the repo.",
		last.PushedAt, diffs), nil
}

// printStatus is the line sheet diff adds about the last push.
func printStatus(last *pushPrint, edited []string) string {
	switch {
	case last == nil:
		return "no push recorded."
	case len(edited) > 0:
		return fmt.Sprintf("the sheet changed since the last push (%s): %s", last.PushedAt, strings.Join(edited, ", "))
	}
	return fmt.Sprintf("the sheet matches the last push (%s).", last.PushedAt)
}

// readPrint finds the print among the spreadsheet's developer metadata, and
// the ids holding it so a new push can replace them. Two prints, or one that
// does not parse, are refused rather than guessed between.
func readPrint(all []*sheets.DeveloperMetadata) (*pushPrint, []int64, error) {
	var found []*sheets.DeveloperMetadata
	for _, m := range all {
		if m.MetadataKey == printKey {
			found = append(found, m)
		}
	}
	switch len(found) {
	case 0:
		return nil, nil, nil
	case 1:
	default:
		return nil, nil, fmt.Errorf("%d %q records in the spreadsheet metadata, want at most one", len(found), printKey)
	}
	var p pushPrint
	if err := json.Unmarshal([]byte(found[0].MetadataValue), &p); err != nil {
		return nil, nil, fmt.Errorf("%q record: %w", printKey, err)
	}
	return &p, []int64{found[0].MetadataId}, nil
}

// printRequests replaces the recorded print with p, in one batch.
func printRequests(old []int64, p pushPrint) ([]*sheets.Request, error) {
	value, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var reqs []*sheets.Request
	for _, id := range old {
		reqs = append(reqs, &sheets.Request{DeleteDeveloperMetadata: &sheets.DeleteDeveloperMetadataRequest{
			DataFilter: &sheets.DataFilter{DeveloperMetadataLookup: &sheets.DeveloperMetadataLookup{MetadataId: id}}}})
	}
	reqs = append(reqs, &sheets.Request{CreateDeveloperMetadata: &sheets.CreateDeveloperMetadataRequest{
		DeveloperMetadata: &sheets.DeveloperMetadata{
			MetadataKey:   printKey,
			MetadataValue: string(value),
			Location:      &sheets.DeveloperMetadataLocation{Spreadsheet: true},
			Visibility:    "PROJECT",
		}}})
	return reqs, nil
}

// lastPush reads the recorded print, nil when there is none.
func (e *env) lastPush() (*pushPrint, []int64, error) {
	ss, err := e.svc.Spreadsheets.Get(e.id).Fields("developerMetadata").Do()
	if err != nil {
		return nil, nil, err
	}
	return readPrint(ss.DeveloperMetadata)
}

// recordPush stores p as the last push, replacing the old print.
func (e *env) recordPush(old []int64, p pushPrint) error {
	reqs, err := printRequests(old, p)
	if err != nil {
		return err
	}
	_, err = e.svc.Spreadsheets.BatchUpdate(e.id,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: reqs}).Do()
	return err
}
