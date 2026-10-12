package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

// The HTML page is meant to be shared, so it embeds a public copy of the
// snapshot rather than the snapshot itself. data/scorecard.json keeps the
// identity evidence — real names, bios, blogs, notes quoting e-mail addresses
// and Discord handles — because it is the only record of how each GitHub
// account was matched; none of that may leave the private repository.

// payloadRe matches the one-line JSON island the HTML page reads at startup.
var payloadRe = regexp.MustCompile(
	`(?s)(<script id="payload" type="application/json">)(.*?)(</script>)`)

// publicGithubFields are the keys of a github block the page may carry: the
// public account, how confidently it was matched, and the contribution counts.
var publicGithubFields = []string{
	"login", "profile", "confidence", "match_rule", "repos",
	"all_time", "since_2025", "merged_pr_urls", "related",
}

// publicRelatedFields are the keys kept on each related account.
var publicRelatedFields = []string{"login", "profile"}

// emailRe catches an address that slipped into a field thought to be public.
var emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// publicPayload returns the compact JSON the page embeds: the snapshot with
// Discord handles dropped and every github block cut down to an allowlist.
// An allowlist, not a blocklist, so a field added to the evidence later stays
// private until someone decides otherwise.
func publicPayload(s snapshot) ([]byte, error) {
	pub := s
	pub.Validators = make([]snapValidator, len(s.Validators))
	for i, v := range s.Validators {
		v.Discord = ""
		if len(v.Github) > 0 {
			g, err := publicGithub(v.Github)
			if err != nil {
				return nil, fmt.Errorf("%s: github block: %w", v.Moniker, err)
			}
			v.Github = g
		}
		pub.Validators[i] = v
	}
	out, err := json.Marshal(pub)
	if err != nil {
		return nil, err
	}
	// The address itself is not echoed: the terminal may be shared too.
	if loc := emailRe.FindIndex(out); loc != nil {
		return nil, fmt.Errorf("public payload still carries an e-mail address at byte %d", loc[0])
	}
	return out, nil
}

func publicGithub(raw json.RawMessage) (json.RawMessage, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return raw, nil
	}
	var full map[string]json.RawMessage
	if err := json.Unmarshal(raw, &full); err != nil {
		return nil, err
	}
	kept := pick(full, publicGithubFields)
	if rel, ok := kept["related"]; ok {
		var accounts []map[string]json.RawMessage
		if err := json.Unmarshal(rel, &accounts); err != nil {
			return nil, fmt.Errorf("related: %w", err)
		}
		for i, a := range accounts {
			accounts[i] = pick(a, publicRelatedFields)
		}
		b, err := json.Marshal(accounts)
		if err != nil {
			return nil, err
		}
		kept["related"] = b
	}
	return json.Marshal(kept)
}

func pick(m map[string]json.RawMessage, keys []string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

// embedPage replaces the page's payload with the public copy of s.
func embedPage(s snapshot, page string) error {
	payload, err := publicPayload(s)
	if err != nil {
		return err
	}
	html, err := os.ReadFile(page)
	if err != nil {
		return err
	}
	if !payloadRe.Match(html) {
		return fmt.Errorf(`%s: no <script id="payload"> block`, page)
	}
	// Literal $ in the JSON would be read as a capture reference by Expand.
	embedded := payloadRe.ReplaceAllFunc(html, func(m []byte) []byte {
		g := payloadRe.FindSubmatch(m)
		return append(append(append([]byte{}, g[1]...), payload...), g[3]...)
	})
	return os.WriteFile(page, embedded, 0o644)
}
