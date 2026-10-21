package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchOperatorsNormalisesCaseAndSpaces(t *testing.T) {
	roster := []valoper{{"Grand Valley", testOperator(1)}, {"kgnodes", testOperator(2)}}
	matched, unmatched, err := matchOperators([]string{" grand  valley ", "KGNodes", "Cogwheel"}, roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if matched["grand valley"] != testOperator(1) || matched["kgnodes"] != testOperator(2) {
		t.Fatalf("matched = %v", matched)
	}
	if len(unmatched) != 1 || unmatched[0].Moniker != "Cogwheel" || unmatched[0].Reason != reasonNotRegistered {
		t.Fatalf("unmatched = %+v", unmatched)
	}
}

func TestMatchOperatorsRefusesToGuessAmbiguous(t *testing.T) {
	roster := []valoper{{"Node", testOperator(1)}, {"node", testOperator(2)}}
	matched, unmatched, err := matchOperators([]string{"Node"}, roster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 0 || len(unmatched) != 1 || unmatched[0].Reason != reasonAmbiguous {
		t.Fatalf("matched %v, unmatched %+v", matched, unmatched)
	}
	matched, _, err = matchOperators([]string{"Node"}, roster, map[string]string{"node": testOperator(2)})
	if err != nil || matched["node"] != testOperator(2) {
		t.Fatalf("override did not resolve the ambiguity: %v, %v", matched, err)
	}
}

func TestMatchOperatorsOverrideWins(t *testing.T) {
	roster := []valoper{{"Cumulo", testOperator(1)}, {"Cumulo Pro", testOperator(2)}}
	matched, _, err := matchOperators([]string{"Cumulo"}, roster, map[string]string{"cumulo": testOperator(2)})
	if err != nil || matched["cumulo"] != testOperator(2) {
		t.Fatalf("matched %v, err %v", matched, err)
	}
}

func TestMatchOperatorsRefusesBadOverrides(t *testing.T) {
	roster := []valoper{{"A", testOperator(1)}, {"B", testOperator(2)}}
	for name, c := range map[string]struct {
		monikers  []string
		overrides map[string]string
		want      string
	}{
		"unknown candidate":   {[]string{"A"}, map[string]string{"zed": testOperator(1)}, "not a scorecard candidate"},
		"unregistered":        {[]string{"A"}, map[string]string{"a": testOperator(9)}, "no valoper record"},
		"two on one operator": {[]string{"A", "B"}, map[string]string{"b": testOperator(1)}, "same operator"},
		"duplicate moniker":   {[]string{"A", " a"}, nil, "twice"},
	} {
		_, _, err := matchOperators(c.monikers, roster, c.overrides)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
}

func writeMap(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "valoper-map.csv")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValoperMap(t *testing.T) {
	got, err := loadValoperMap(filepath.Join(t.TempDir(), "absent.csv"))
	if err != nil || len(got) != 0 {
		t.Fatalf("missing file: %v, %v", got, err)
	}
	got, err = loadValoperMap(writeMap(t, "moniker,operator_address\nGrand  Valley,"+testOperator(1)+"\n"))
	if err != nil || got["grand valley"] != testOperator(1) {
		t.Fatalf("got %v, err %v", got, err)
	}
	for name, body := range map[string]string{
		"bad header":  "name,address\nA," + testOperator(1) + "\n",
		"bad address": "moniker,operator_address\nA,g1nope\n",
		"duplicate":   "moniker,operator_address\nA," + testOperator(1) + "\na," + testOperator(2) + "\n",
		"empty name":  "moniker,operator_address\n ," + testOperator(1) + "\n",
	} {
		if _, err := loadValoperMap(writeMap(t, body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The committed map must always load.
func TestCommittedValoperMapLoads(t *testing.T) {
	if _, err := loadValoperMap(filepath.Join("..", "..", "data", "valoper-map.csv")); err != nil {
		t.Fatal(err)
	}
}
