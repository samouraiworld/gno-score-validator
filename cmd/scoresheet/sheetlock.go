package main

import (
	"encoding/json"
	"fmt"
	"os"

	"google.golang.org/api/sheets/v4"
)

// lockDescription marks the protections this tool owns, so a rerun replaces
// them instead of stacking a second one, and protections set by hand are left
// alone.
const lockDescription = "scoresheet lock: generated from the git repo, edit the CSV by pull request"

// runSheetLock protects every tab of the spreadsheet so that only the service
// account can edit it. Google then refuses a hand edit outright
// (warningOnly false) rather than asking for confirmation.
//
// The file owner can always edit a protected range: Sheets offers no way to
// lock the owner out. `sheet push` catches what that leaves open, by running
// `sheet diff` before it writes anything.
func runSheetLock(e *env, args []string) error {
	_, dry := subFlags("sheet lock", args)
	return e.lockSheet(*dry)
}

// lockSheet replaces this tool's protections, one per tab, in a single batch
// update: the batch is atomic, so no tab is left unprotected between the
// delete and the add.
func (e *env) lockSheet(dry bool) error {
	email, err := serviceAccountEmail(e.creds)
	if err != nil {
		return err
	}
	ss, err := e.svc.Spreadsheets.Get(e.id).
		Fields("sheets(properties(sheetId,title),protectedRanges)").Do()
	if err != nil {
		return err
	}
	reqs := lockRequests(ss, email)
	for _, sh := range ss.Sheets {
		ours, others := 0, 0
		for _, p := range sh.ProtectedRanges {
			if p.Description == lockDescription {
				ours++
			} else {
				others++
			}
		}
		state := "lock added"
		if ours > 0 {
			state = fmt.Sprintf("lock replaced (%d)", ours)
		}
		if others > 0 {
			state += fmt.Sprintf(", %d other protection(s) left as they are", others)
		}
		fmt.Printf("  %-18s %s\n", sh.Properties.Title, state)
	}
	fmt.Printf("editor: %s (the file owner keeps edit rights regardless)\n", email)
	if dry {
		fmt.Println("-dry — nothing written.")
		return nil
	}
	if _, err := e.svc.Spreadsheets.BatchUpdate(e.id,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: reqs}).Do(); err != nil {
		return err
	}
	fmt.Println("sheet locked.")
	return nil
}

// lockRequests deletes this tool's protections and adds one per tab covering
// the whole tab, editable by the service account alone.
func lockRequests(ss *sheets.Spreadsheet, email string) []*sheets.Request {
	var reqs []*sheets.Request
	for _, sh := range ss.Sheets {
		for _, p := range sh.ProtectedRanges {
			if p.Description == lockDescription {
				reqs = append(reqs, &sheets.Request{DeleteProtectedRange: &sheets.DeleteProtectedRangeRequest{
					ProtectedRangeId: p.ProtectedRangeId}})
			}
		}
	}
	for _, sh := range ss.Sheets {
		reqs = append(reqs, &sheets.Request{AddProtectedRange: &sheets.AddProtectedRangeRequest{
			ProtectedRange: &sheets.ProtectedRange{
				// A range with only a sheet id is the whole tab. Tab 0 is a
				// valid id that omitempty would drop, so it is sent explicitly.
				Range: &sheets.GridRange{SheetId: sh.Properties.SheetId,
					ForceSendFields: []string{"SheetId"}},
				Description:     lockDescription,
				WarningOnly:     false,
				Editors:         &sheets.Editors{Users: []string{email}},
				ForceSendFields: []string{"WarningOnly"},
			}}})
	}
	return reqs
}

// serviceAccountEmail reads the client_email of the key file the tool runs as.
func serviceAccountEmail(creds string) (string, error) {
	b, err := os.ReadFile(creds)
	if err != nil {
		return "", err
	}
	var key struct {
		ClientEmail string `json:"client_email"`
	}
	if err := json.Unmarshal(b, &key); err != nil {
		return "", fmt.Errorf("%s: %w", creds, err)
	}
	if key.ClientEmail == "" {
		return "", fmt.Errorf("%s: no client_email", creds)
	}
	return key.ClientEmail, nil
}
