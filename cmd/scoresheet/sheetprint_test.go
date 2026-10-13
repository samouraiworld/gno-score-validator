package main

import (
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/api/sheets/v4"
)

func TestTabHash(t *testing.T) {
	rows := [][]interface{}{
		{"moniker", "score"},
		{"Alpha", 90.0},
		{"", 50.0, ""},
	}
	h := tabHash(rows)

	// What the API leaves out or pads does not change the hash.
	padded := [][]interface{}{
		{"moniker", "score", nil},
		{"Alpha", 90.0, ""},
		{"", 50.0},
		{},
		{"", nil},
	}
	if tabHash(padded) != h {
		t.Fatal("trailing blank cells and rows changed the hash")
	}

	for name, edit := range map[string]func(r [][]interface{}){
		"number as text": func(r [][]interface{}) { r[1][1] = "90" },
		"value":          func(r [][]interface{}) { r[1][1] = 91.0 },
		"case":           func(r [][]interface{}) { r[1][0] = "alpha" },
		"order":          func(r [][]interface{}) { r[1], r[2] = r[2], r[1] },
		"cell moved":     func(r [][]interface{}) { r[2][0], r[2][1] = 50.0, "" },
	} {
		c := asSheet(rows)
		edit(c)
		if tabHash(c) == h {
			t.Errorf("%s: hash unchanged", name)
		}
	}
	if tabHash(nil) == h || tabHash(nil) != tabHash([][]interface{}{{}}) {
		t.Fatal("empty tab")
	}
}

func TestEditedSince(t *testing.T) {
	last := &pushPrint{PushedAt: "2026-10-13T10:00:00Z", Tabs: map[string]string{
		"Bilan": "b", "Score-pearl": "p", "Score-old": "o"}}
	want := []string{"Bilan", "Score-pearl", "Score-new"}

	if e := editedSince(last, map[string]string{"Bilan": "b", "Score-pearl": "p"}, want); len(e) != 0 {
		t.Fatalf("untouched sheet, new tab not created yet, old tab out of config: %v", e)
	}
	e := editedSince(last, map[string]string{"Bilan": "x", "Score-new": "n"}, want)
	if got := strings.Join(e, ", "); got != "Bilan, Score-pearl (deleted), Score-new (not in the last push)" {
		t.Fatalf("edited: %s", got)
	}
}

func TestPushGate(t *testing.T) {
	last := &pushPrint{PushedAt: "2026-10-13T10:00:00Z", Tabs: map[string]string{"Bilan": "b"}}
	cases := []struct {
		name   string
		diffs  int
		last   *pushPrint
		edited []string
		force  bool
		refuse string // substring of the error, "" when the push goes ahead
	}{
		{"sheet already matches, first push", 0, nil, nil, false, ""},
		{"sheet already matches, edited since", 0, last, []string{"Bilan"}, false, ""},
		{"first push", 3, nil, nil, false, "no push recorded"},
		{"first push forced", 3, nil, nil, true, ""},
		{"repo moved only", 3, last, nil, false, ""},
		{"sheet edited", 3, last, []string{"Bilan"}, false, "edited since the last push"},
		{"sheet edited, forced", 3, last, []string{"Bilan"}, true, ""},
	}
	for _, c := range cases {
		note, err := pushGate(c.diffs, c.last, c.edited, c.force)
		switch {
		case c.refuse == "" && err != nil:
			t.Errorf("%s: refused: %v", c.name, err)
		case c.refuse != "" && (err == nil || !strings.Contains(err.Error(), c.refuse)):
			t.Errorf("%s: want refusal %q, got %v", c.name, c.refuse, err)
		case err == nil && note == "":
			t.Errorf("%s: no note", c.name)
		}
	}
	if _, err := pushGate(3, last, []string{"Bilan", "Score-pearl"}, false); err == nil ||
		!strings.Contains(err.Error(), "Bilan, Score-pearl") {
		t.Fatalf("refusal must name the edited tabs: %v", err)
	}
}

func TestPrintStatus(t *testing.T) {
	last := &pushPrint{PushedAt: "2026-10-13T10:00:00Z"}
	for _, c := range []struct {
		last   *pushPrint
		edited []string
		want   string
	}{
		{nil, nil, "no push recorded"},
		{last, nil, "matches the last push (2026-10-13T10:00:00Z)"},
		{last, []string{"Bilan"}, "changed since the last push (2026-10-13T10:00:00Z): Bilan"},
	} {
		if got := printStatus(c.last, c.edited); !strings.Contains(got, c.want) {
			t.Errorf("got %q, want it to contain %q", got, c.want)
		}
	}
}

func TestReadPrint(t *testing.T) {
	value, _ := json.Marshal(pushPrint{PushedAt: "2026-10-13T10:00:00Z", Tabs: map[string]string{"Bilan": "b"}})
	other := &sheets.DeveloperMetadata{MetadataId: 1, MetadataKey: "someone-else", MetadataValue: "x"}
	ours := &sheets.DeveloperMetadata{MetadataId: 2, MetadataKey: printKey, MetadataValue: string(value)}

	p, ids, err := readPrint([]*sheets.DeveloperMetadata{other})
	if err != nil || p != nil || len(ids) != 0 {
		t.Fatalf("none recorded: %v %v %v", p, ids, err)
	}
	p, ids, err = readPrint([]*sheets.DeveloperMetadata{other, ours})
	if err != nil || p == nil || p.Tabs["Bilan"] != "b" || len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("recorded: %v %v %v", p, ids, err)
	}
	if _, _, err := readPrint([]*sheets.DeveloperMetadata{ours, ours}); err == nil {
		t.Fatal("two records must be refused")
	}
	bad := &sheets.DeveloperMetadata{MetadataId: 3, MetadataKey: printKey, MetadataValue: "{"}
	if _, _, err := readPrint([]*sheets.DeveloperMetadata{bad}); err == nil {
		t.Fatal("an unreadable record must be refused")
	}
}

func TestPrintRequests(t *testing.T) {
	p := pushPrint{PushedAt: "2026-10-13T10:00:00Z", Tabs: map[string]string{"Bilan": "b"}}
	reqs, err := printRequests([]int64{7}, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 || reqs[0].DeleteDeveloperMetadata == nil ||
		reqs[0].DeleteDeveloperMetadata.DataFilter.DeveloperMetadataLookup.MetadataId != 7 {
		t.Fatalf("old record not deleted first: %+v", reqs)
	}
	m := reqs[1].CreateDeveloperMetadata.DeveloperMetadata
	if m.MetadataKey != printKey || m.Visibility != "PROJECT" || !m.Location.Spreadsheet {
		t.Fatalf("record: %+v", m)
	}
	back, _, err := readPrint([]*sheets.DeveloperMetadata{m})
	if err != nil || back.PushedAt != p.PushedAt || back.Tabs["Bilan"] != "b" {
		t.Fatalf("round trip: %v %v", back, err)
	}
	if reqs, _ := printRequests(nil, p); len(reqs) != 1 {
		t.Fatalf("first record: %d requests", len(reqs))
	}
}
