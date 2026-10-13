package scoring

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFrozenTabs(t *testing.T) {
	for _, tab := range []string{"Score-test13", "score-pearl", "Score-firedrill", "Test13-challenge"} {
		if !IsFrozen(tab) {
			t.Errorf("%s should be frozen", tab)
		}
	}
	if IsFrozen("Score-gnoland") {
		t.Error("a mainnet tab must stay writable")
	}
}

func TestLoadLinksIgnoresWeakAndMatchesAnyCase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.csv")
	csv := "moniker,discord,login,confidence,score\n" +
		"AviaOne,d,member,weak,500\n" +
		"AviaOne,d,brand,strong,40\n" +
		"aviaone,d,owner,certain,60\n"
	if err := os.WriteFile(path, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	links, err := LoadLinks(path)
	if err != nil {
		t.Fatal(err)
	}
	l, ok := links[Key("AVIAONE ")]
	if !ok || l.Login != "owner" || l.Score != 60 {
		t.Errorf("got %+v, want the certain row, found under any case", l)
	}
}
