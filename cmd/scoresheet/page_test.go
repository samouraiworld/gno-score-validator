package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The page payload must keep what the GitHub view draws and nothing that
// identifies a person beyond their public GitHub account.
func TestPublicPayloadDropsPrivateFields(t *testing.T) {
	s, err := loadSnapshot(filepath.Join("..", "..", "data", "scorecard.json"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := publicPayload(s)
	if err != nil {
		t.Fatal(err)
	}
	var pub struct {
		Validators []map[string]json.RawMessage `json:"validators"`
	}
	if err := json.Unmarshal(out, &pub); err != nil {
		t.Fatal(err)
	}
	if len(pub.Validators) != len(s.Validators) {
		t.Fatalf("%d validators in the payload, want %d", len(pub.Validators), len(s.Validators))
	}
	linked := 0
	for _, v := range pub.Validators {
		if _, ok := v["discord"]; ok {
			t.Fatalf("payload carries a Discord handle")
		}
		raw, ok := v["github"]
		if !ok {
			continue
		}
		linked++
		var g map[string]json.RawMessage
		if err := json.Unmarshal(raw, &g); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"name", "bio", "blog", "note", "evidence"} {
			if _, ok := g[k]; ok {
				t.Errorf("github block keeps private field %q", k)
			}
		}
		for _, k := range []string{"login", "profile", "confidence", "all_time", "since_2025", "merged_pr_urls"} {
			if _, ok := g[k]; !ok {
				t.Errorf("github block lost %q, which the page draws", k)
			}
		}
		if rel, ok := g["related"]; ok && bytes.Contains(rel, []byte(`"note"`)) {
			t.Errorf("related account keeps its note")
		}
	}
	if linked == 0 {
		t.Error("no github block survived; the page's GitHub view would be empty")
	}
}

// The committed page must embed exactly the public payload, so a page
// re-embedded by an older build, with the full evidence, fails here.
func TestCommittedPageEmbedsPublicPayload(t *testing.T) {
	s, err := loadSnapshot(filepath.Join("..", "..", "data", "scorecard.json"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := publicPayload(s)
	if err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(filepath.Join("..", "..", "web", "validator-scorecard.html"))
	if err != nil {
		t.Fatal(err)
	}
	m := payloadRe.FindSubmatch(html)
	if m == nil {
		t.Fatal("no payload block in the page")
	}
	if !bytes.Equal(m[2], want) {
		t.Error("page payload differs from publicPayload(scorecard.json); run `scoresheet render`")
	}
	if emailRe.Match(html) {
		t.Error("page carries an e-mail address")
	}
	if len(s.Arenas) == 0 || !bytes.Contains(m[2], []byte(`"arenas"`)) {
		t.Error("page payload has no arenas")
	}
	for _, stale := range [][]byte{[]byte("const ARENA_WEIGHT = 1 / 6"), []byte("const SERIES = [")} {
		if bytes.Contains(html, stale) {
			t.Errorf("page still contains hardcoded arena JS %q", stale)
		}
	}
}
