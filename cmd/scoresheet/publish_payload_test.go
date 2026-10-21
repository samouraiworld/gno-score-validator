package main

import (
	"math"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// testOperator returns a well-formed, made-up operator address: g1 and 38
// bech32 characters encoding i.
func testOperator(i int) string {
	const charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
	b := []byte(strings.Repeat("q", 38))
	for p := 37; i > 0 && p >= 0; p-- {
		b[p] = charset[i%32]
		i /= 32
	}
	return "g1" + string(b)
}

func TestHundredths(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want int64
	}{
		{0, 0}, {87.43, 8743}, {92.86666666666666, 9287}, {40.733333333333334, 4073}, {100, 10000},
	} {
		got, err := hundredths(c.in)
		if err != nil || got != c.want {
			t.Errorf("hundredths(%v) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
	if _, err := hundredths(math.NaN()); err == nil {
		t.Error("NaN accepted")
	}
}

func TestRFC3339(t *testing.T) {
	for in, want := range map[string]string{
		"2026-10-13":                "2026-10-13T00:00:00Z",
		"2026-10-13T09:00:00Z":      "2026-10-13T09:00:00Z",
		"2026-10-13T09:00:00+02:00": "2026-10-13T07:00:00Z",
		"2026-10-13T09:00:00.5Z":    "2026-10-13T09:00:00Z", // the realm refuses fractions
	} {
		got, err := rfc3339(in)
		if err != nil || got != want {
			t.Errorf("rfc3339(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := rfc3339("13/10/2026"); err == nil {
		t.Error("13/10/2026 accepted")
	}
}

func TestBuildCandidateKeepsAllowlistOnly(t *testing.T) {
	v := snapValidator{
		Moniker: "  Grand   Valley ", Discord: "grand_dc",
		Components: snapComponents{
			{ID: "challenge", Score: 92.86666666666666},
			{ID: "test13", Score: 40.733333333333334},
		},
		GithubCurrent: 72, GithubCorrected: 37, ScoreCurrent: 99, ScoreCorrected: 70.5,
		RankCurrent: 1, RankCorrected: 3,
	}
	c, err := buildCandidate(v, []string{"challenge", "test13"}, testOperator(1))
	if err != nil {
		t.Fatal(err)
	}
	want := pubCandidate{Operator: testOperator(1), Moniker: "Grand Valley", Total: 7050, Rank: 3, Github: 370, Arenas: []int64{9287, 4073}}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("got %+v, want %+v", c, want)
	}
}

func TestBuildCandidateRefusesMissingArena(t *testing.T) {
	v := snapValidator{Moniker: "Node", Components: snapComponents{{ID: "challenge", Score: 50}}, RankCorrected: 1}
	_, err := buildCandidate(v, []string{"challenge", "firedrill"}, testOperator(1))
	if err == nil || !strings.Contains(err.Error(), "no firedrill score") {
		t.Fatalf("err = %v, want a missing firedrill score", err)
	}
}

func TestEncodeLine(t *testing.T) {
	c := pubCandidate{Operator: testOperator(1), Moniker: "Grand Valley", Total: 7050, Rank: 3, Github: 370, Arenas: []int64{9287, 4073}}
	got, err := encodeLine(c)
	if err != nil {
		t.Fatal(err)
	}
	if want := testOperator(1) + "|Grand Valley|7050|3|370|9287,4073"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEncodeLineRefusesWhatTheRealmRefuses(t *testing.T) {
	ok := pubCandidate{Operator: testOperator(1), Moniker: "Node", Total: 5000, Rank: 1, Github: 0, Arenas: []int64{5000}}
	for _, tc := range []struct {
		name          string
		mutate        func(*pubCandidate)
		wantSubstring string
	}{
		{"pipe in moniker", func(c *pubCandidate) { c.Moniker = "a|b" }, "| or @"},
		{"at in moniker", func(c *pubCandidate) { c.Moniker = "me@node" }, "| or @"},
		{"newline in moniker", func(c *pubCandidate) { c.Moniker = "a\nb" }, "control character"},
		{"C1 control U+0085", func(c *pubCandidate) { c.Moniker = "a\u0085b" }, "control character"},
		{"DEL U+007F", func(c *pubCandidate) { c.Moniker = "a\u007fb" }, "control character"},
		{"bidi U+202E", func(c *pubCandidate) { c.Moniker = "a\u202eb" }, "bidi control"},
		{"bidi U+202A", func(c *pubCandidate) { c.Moniker = "a\u202ab" }, "bidi control"},
		{"bidi U+2066", func(c *pubCandidate) { c.Moniker = "a\u2066b" }, "bidi control"},
		{"bidi U+2069", func(c *pubCandidate) { c.Moniker = "a\u2069b" }, "bidi control"},
		{"empty moniker", func(c *pubCandidate) { c.Moniker = "" }, "empty moniker"},
		{"moniker of 65 B", func(c *pubCandidate) { c.Moniker = strings.Repeat("n", 65) }, "longer than 64"},
		{"invalid UTF-8", func(c *pubCandidate) { c.Moniker = "a\xffb" }, "not valid UTF-8"},
		{"leading space", func(c *pubCandidate) { c.Moniker = " Node" }, "starts or ends"},
		{"trailing space", func(c *pubCandidate) { c.Moniker = "Node " }, "starts or ends"},
		{"rank above 100000", func(c *pubCandidate) { c.Rank = 100001 }, "rank 100001"},
		{"bad operator", func(c *pubCandidate) { c.Operator = "g1bio" }, "not a g1 address"},
		{"total above 100", func(c *pubCandidate) { c.Total = 10001 }, "total 10001"},
		{"negative arena", func(c *pubCandidate) { c.Arenas = []int64{-1} }, "arena score -1"},
		{"bonus above 10", func(c *pubCandidate) { c.Github = 1001 }, "GitHub bonus 1001"},
		{"rank zero", func(c *pubCandidate) { c.Rank = 0 }, "rank 0"},
		{"no arena", func(c *pubCandidate) { c.Arenas = nil }, "no arena score"},
	} {
		c := ok
		c.Arenas = append([]int64(nil), ok.Arenas...)
		tc.mutate(&c)
		_, err := encodeLine(c)
		if err == nil {
			t.Errorf("%s: accepted", tc.name)
		} else if !strings.Contains(err.Error(), tc.wantSubstring) {
			t.Errorf("%s: error %q lacks %q", tc.name, err, tc.wantSubstring)
		}
	}
	if _, err := encodeLine(ok); err != nil {
		t.Fatalf("valid candidate refused: %v", err)
	}
	// Zero-width joiners (U+200B..U+200F) stay allowed, for emoji monikers.
	zwj := ok
	zwj.Moniker = "a\u200db"
	if _, err := encodeLine(zwj); err != nil {
		t.Fatalf("moniker with a ZWJ refused: %v", err)
	}
}

func TestEditionArenasRefusesDuplicate(t *testing.T) {
	s := snapshot{Arenas: []snapArena{{ID: "challenge", InTotal: true}, {ID: "test13", InTotal: true}, {ID: "challenge", InTotal: true}}}
	if _, err := editionArenas(s); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("err = %v, want a duplicate arena", err)
	}
	s.Arenas = s.Arenas[:2]
	if ids, err := editionArenas(s); err != nil || !reflect.DeepEqual(ids, []string{"challenge", "test13"}) {
		t.Fatalf("ids = %v, err = %v", ids, err)
	}
}

func TestGuardPublicRefusesEmailAndDiscordShapes(t *testing.T) {
	for _, v := range []string{"jane.doe@example.org", "<@123456789012>", "olduser#1234", "discord.gg/abcd", "https://discord.com/users/1", "@handle"} {
		err := guardPublic("moniker", v)
		if err == nil {
			t.Errorf("%q accepted", v)
			continue
		}
		if strings.Contains(err.Error(), v) {
			t.Errorf("error echoes the value: %v", err)
		}
	}
	for _, v := range []string{"Cogwheel", "Grand Valley", "Vicky_Pulsican", "Santala-Research", "0xyzBlock", "1XP"} {
		if err := guardPublic("moniker", v); err != nil {
			t.Errorf("%q refused: %v", v, err)
		}
	}
}

func TestPayloadLinesSortsByRankThenMoniker(t *testing.T) {
	cands := []pubCandidate{
		{Operator: testOperator(1), Moniker: "Zeta", Total: 5000, Rank: 2, Arenas: []int64{5000}},
		{Operator: testOperator(2), Moniker: "Alpha", Total: 5000, Rank: 2, Arenas: []int64{5000}},
		{Operator: testOperator(3), Moniker: "Top", Total: 9000, Rank: 1, Arenas: []int64{9000}},
	}
	lines, err := payloadLines(cands)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range lines {
		got = append(got, strings.Split(l, "|")[1])
	}
	if want := []string{"Top", "Alpha", "Zeta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

func TestPayloadLinesRefusesDuplicateOperator(t *testing.T) {
	cands := []pubCandidate{
		{Operator: testOperator(1), Moniker: "A", Total: 5000, Rank: 1, Arenas: []int64{5000}},
		{Operator: testOperator(1), Moniker: "B", Total: 4000, Rank: 2, Arenas: []int64{4000}},
	}
	if _, err := payloadLines(cands); err == nil || !strings.Contains(err.Error(), "appears twice") {
		t.Fatalf("err = %v, want a duplicate operator", err)
	}
}

func TestBatches(t *testing.T) {
	lines := make([]string, 45)
	for i := range lines {
		lines[i] = strconv.Itoa(i)
	}
	got, err := batches(lines, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || strings.Count(got[0], "\n") != 19 || strings.Count(got[2], "\n") != 4 {
		t.Fatalf("batches = %q", got)
	}
	if strings.HasSuffix(got[0], "\n") {
		t.Fatal("a payload ends with a newline")
	}
	if _, err := batches(lines, 0); err == nil {
		t.Fatal("batch size 0 accepted")
	}
	if got, _ := batches(nil, 20); len(got) != 0 {
		t.Fatalf("no line gave %d payloads", len(got))
	}
}

// Every real candidate must encode and pass the guard, and each line must
// carry the allowlisted fields only: one text field, the scorecard moniker.
func TestRealScorecardEncodes(t *testing.T) {
	s, err := loadSnapshot(filepath.Join("..", "..", "data", "scorecard.json"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := newEditionMeta(s, strings.Repeat("0", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := guardMeta(meta); err != nil {
		t.Fatal(err)
	}
	byOperator := map[string]snapValidator{}
	var cands []pubCandidate
	for i, v := range s.Validators {
		op := testOperator(i + 1)
		byOperator[op] = v
		c, err := buildCandidate(v, meta.Arenas, op)
		if err != nil {
			t.Fatal(err)
		}
		cands = append(cands, c)
	}
	lines, err := payloadLines(cands)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != len(s.Validators) {
		t.Fatalf("%d lines for %d validators", len(lines), len(s.Validators))
	}
	for _, l := range lines {
		f := strings.Split(l, "|")
		if len(f) != 6 {
			t.Fatalf("%d fields in %q", len(f), l)
		}
		v := byOperator[f[0]]
		if f[1] != collapseSpaces(v.Moniker) {
			t.Errorf("moniker field %q is not the scorecard moniker %q", f[1], v.Moniker)
		}
		scores := strings.Split(f[5], ",")
		if len(scores) != len(meta.Arenas) {
			t.Errorf("%d arena scores in %q, want %d", len(scores), l, len(meta.Arenas))
		}
		nums := append([]string{}, f[2:5]...)
		nums = append(nums, scores...)
		for _, n := range nums {
			if _, err := strconv.ParseInt(n, 10, 64); err != nil {
				t.Errorf("%q in %q is not an integer", n, l)
			}
		}
	}
}
