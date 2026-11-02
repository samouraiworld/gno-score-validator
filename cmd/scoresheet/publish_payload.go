package main

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/louisb/score-validator/internal/scoring"
)

// What publish puts on chain is public and permanent. pubCandidate is the
// allowlist of the design spec written as a type: a field that is not here
// cannot reach the realm. Scores are integers in hundredths because the realm
// stores no floats.
type pubCandidate struct {
	Operator string  // valoper operator address, g1...
	Moniker  string  // scorecard moniker, runs of spaces collapsed
	Total    int64   // score_corrected, hundredths
	Rank     int     // rank_corrected
	Github   int64   // GitHub bonus added to the total (github_corrected / 10), hundredths
	Arenas   []int64 // one score per edition arena, hundredths
}

// editionMeta is what OpenEdition receives.
type editionMeta struct {
	ComputedAt string   // YYYY-MM-DDTHH:MM:SSZ (UTC, no fraction), from generated_at
	SHA256     string   // hex SHA-256 of the scorecard file as read
	Arenas     []string // ids of the arenas in the total, scorecard order
}

var (
	operatorRe = regexp.MustCompile(`^g1[qpzry9x8gf2tvdw0s3jn54khce6mua7l]{38}$`)
	arenaIDRe  = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`) // the realm's arena name rule
	sha256Re   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// discordRe catches the shapes a Discord identity takes in text: an
	// invite or profile link, a <@id> mention, a legacy name#1234 tag, and
	// any "@". A modern handle is a bare word that cannot be told from a
	// moniker; many candidates use their handle as moniker, and that moniker
	// is already public in r/gnops/valopers.
	discordRe = regexp.MustCompile(`(?i)discord(app)?\.(gg|com)|<@[!&]?\d+>|#\d{4}\b|@`)
)

// hundredths turns a score into the realm's integer unit: 87.43 is 8743.
func hundredths(x float64) (int64, error) {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0, fmt.Errorf("score %v is not a finite number", x)
	}
	return int64(math.Round(x * 100)), nil
}

// rfc3339 reads generated_at, which score writes as a bare date today, as the
// YYYY-MM-DDTHH:MM:SSZ string publish always sends (UTC, fraction dropped);
// the realm accepts that form and a bare date, nothing else.
func rfc3339(generatedAt string) (string, error) {
	if t, err := time.Parse(time.RFC3339, generatedAt); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if t, err := time.Parse(time.DateOnly, generatedAt); err == nil {
		return t.Format(time.RFC3339), nil
	}
	return "", fmt.Errorf("generated_at %q is neither RFC 3339 nor YYYY-MM-DD", generatedAt)
}

// editionArenas lists the arenas in the total, in scorecard order. Mainnet is
// shown, not scored, and is not in snapshot.Arenas.
func editionArenas(s snapshot) ([]string, error) {
	var ids []string
	seen := map[string]bool{}
	for _, a := range s.Arenas {
		if !a.InTotal {
			continue
		}
		if !arenaIDRe.MatchString(a.ID) {
			return nil, fmt.Errorf("arena id %q cannot go on chain", a.ID)
		}
		if seen[a.ID] {
			return nil, fmt.Errorf("arena id %q appears twice", a.ID)
		}
		seen[a.ID] = true
		ids = append(ids, a.ID)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("the scorecard has no arena in the total")
	}
	if len(ids) > 16 {
		return nil, fmt.Errorf("the scorecard has %d arenas in the total; the realm takes at most 16", len(ids))
	}
	return ids, nil
}

func newEditionMeta(s snapshot, sha string) (editionMeta, error) {
	at, err := rfc3339(s.GeneratedAt)
	if err != nil {
		return editionMeta{}, err
	}
	arenas, err := editionArenas(s)
	if err != nil {
		return editionMeta{}, err
	}
	return editionMeta{ComputedAt: at, SHA256: sha, Arenas: arenas}, nil
}

func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

// buildCandidate copies the allowlisted fields of one scorecard row.
func buildCandidate(v snapValidator, arenas []string, operator string) (pubCandidate, error) {
	c := pubCandidate{Operator: operator, Moniker: collapseSpaces(v.Moniker), Rank: v.RankCorrected}
	var err error
	if c.Total, err = hundredths(v.ScoreCorrected); err != nil {
		return c, fmt.Errorf("%s: total: %w", c.Moniker, err)
	}
	if c.Github, err = hundredths(v.GithubCorrected / scoring.BonusDivisor); err != nil {
		return c, fmt.Errorf("%s: github: %w", c.Moniker, err)
	}
	for _, id := range arenas {
		if !v.Components.Has(id) {
			return c, fmt.Errorf("%s: no %s score in the scorecard", c.Moniker, id)
		}
		s, err := hundredths(v.Components.Get(id))
		if err != nil {
			return c, fmt.Errorf("%s: %s: %w", c.Moniker, id, err)
		}
		c.Arenas = append(c.Arenas, s)
	}
	return c, nil
}

// encodeLine writes one AddCandidates line,
// operator|moniker|total|rank|github|s1,s2,...,sN, after checking each field
// against what the realm accepts. Errors name the rank, never the moniker,
// which may be the offending value.
func encodeLine(c pubCandidate) (string, error) {
	if !operatorRe.MatchString(c.Operator) {
		return "", fmt.Errorf("rank %d: operator %q is not a g1 address", c.Rank, c.Operator)
	}
	if err := checkMoniker(c.Moniker); err != nil {
		return "", fmt.Errorf("rank %d: %w", c.Rank, err)
	}
	if c.Rank < 1 || c.Rank > 100000 {
		return "", fmt.Errorf("%s: rank %d is outside 1..100000", c.Moniker, c.Rank)
	}
	if c.Total < 0 || c.Total > 10000 {
		return "", fmt.Errorf("%s: total %d is outside 0..10000", c.Moniker, c.Total)
	}
	if c.Github < 0 || c.Github > 1000 {
		return "", fmt.Errorf("%s: GitHub bonus %d is outside 0..1000", c.Moniker, c.Github)
	}
	if len(c.Arenas) == 0 {
		return "", fmt.Errorf("%s: no arena score", c.Moniker)
	}
	scores := make([]string, len(c.Arenas))
	for i, s := range c.Arenas {
		if s < 0 || s > 10000 {
			return "", fmt.Errorf("%s: arena score %d is outside 0..10000", c.Moniker, s)
		}
		scores[i] = strconv.FormatInt(s, 10)
	}
	return fmt.Sprintf("%s|%s|%d|%d|%d|%s", c.Operator, c.Moniker, c.Total, c.Rank, c.Github, strings.Join(scores, ",")), nil
}

// checkMoniker mirrors the realm's isMoniker exactly: 1 to 64 bytes of valid
// UTF-8, no leading or trailing Unicode space, no "|" or "@", no rune below
// U+0020, no DEL or C1 control (U+007F..U+009F), no bidi control (U+202A..U+202E,
// U+2066..U+2069). U+200B..U+200F stay allowed (zero-width joiners in emoji).
// Reasons stay distinct; none echoes the moniker.
func checkMoniker(m string) error {
	if m == "" {
		return fmt.Errorf("empty moniker")
	}
	if len(m) > 64 {
		return fmt.Errorf("moniker is longer than 64 bytes")
	}
	if !utf8.ValidString(m) {
		return fmt.Errorf("moniker is not valid UTF-8")
	}
	if strings.TrimSpace(m) != m {
		return fmt.Errorf("moniker starts or ends with a space")
	}
	for _, r := range m {
		switch {
		case r == '|' || r == '@':
			return fmt.Errorf("moniker holds a character the realm refuses (| or @)")
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			return fmt.Errorf("moniker holds a control character")
		case (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069):
			return fmt.Errorf("moniker holds a bidi control character")
		}
	}
	return nil
}

// guardPublic refuses a value shaped like an e-mail address or a Discord
// identity. It names the field, never the value: the terminal may be shared.
func guardPublic(field, value string) error {
	if emailRe.MatchString(value) || discordRe.MatchString(value) {
		return fmt.Errorf("%s looks like an e-mail address or a Discord handle; nothing sent", field)
	}
	return nil
}

func guardMeta(m editionMeta) error {
	if err := guardPublic("computedAt", m.ComputedAt); err != nil {
		return err
	}
	return guardPublic("arenas", strings.Join(m.Arenas, ","))
}

// payloadLines encodes the candidates in rank then moniker order, the order
// the realm returns them in, guards each line and refuses a duplicate
// operator.
func payloadLines(cands []pubCandidate) ([]string, error) {
	sorted := append([]pubCandidate(nil), cands...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Rank != sorted[j].Rank {
			return sorted[i].Rank < sorted[j].Rank
		}
		return sorted[i].Moniker < sorted[j].Moniker
	})
	seen := map[string]bool{}
	lines := make([]string, 0, len(sorted))
	for _, c := range sorted {
		if seen[c.Operator] {
			return nil, fmt.Errorf("operator %s appears twice", c.Operator)
		}
		seen[c.Operator] = true
		line, err := encodeLine(c)
		if err != nil {
			return nil, err
		}
		if err := guardPublic(fmt.Sprintf("candidate ranked %d", c.Rank), line); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// batches cuts lines into AddCandidates payloads of at most size lines each.
func batches(lines []string, size int) ([]string, error) {
	if size < 1 {
		return nil, fmt.Errorf("batch size %d: must be at least 1", size)
	}
	var out []string
	for start := 0; start < len(lines); start += size {
		end := min(start+size, len(lines))
		out = append(out, strings.Join(lines[start:end], "\n"))
	}
	return out, nil
}
