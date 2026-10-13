package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEvidenceRejectsBadFiles(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate under any case": `{"AviaOne": {"login": "a"}, "aviaone ": {"login": "b"}}`,
		"null block":               `{"AviaOne": null}`,
		"empty moniker":            `{" ": {"login": "a"}}`,
	} {
		path := filepath.Join(t.TempDir(), "evidence.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadEvidence(path); err == nil {
			t.Errorf("%s: loadEvidence accepted it", name)
		}
	}
}

func TestLoadEvidenceKeysByMoniker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(path, []byte(`{"AviaOne": {"login": "a"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, err := loadEvidence(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(ev["aviaone"]) != `{"login": "a"}` {
		t.Errorf("got %q, want the raw block under its lowercased moniker", ev["aviaone"])
	}
}
