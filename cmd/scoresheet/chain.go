package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// defaultRealm holds the published editions.
const defaultRealm = "gno.land/r/samcrew/validator_scores"

// rpcClient reads a gno.land node over JSON-RPC: no key, no gnokey, nothing
// written.
type rpcClient struct {
	url  string
	http *http.Client
}

func newRPCClient(remote string) *rpcClient {
	u := remote
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	return &rpcClient{url: u, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *rpcClient) call(method string, params map[string]string, result any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "scoresheet", "method": method, "params": params})
	if err != nil {
		return err
	}
	resp, err := c.http.Post(c.url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", method, resp.StatusCode)
	}
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
			Data    string `json:"data"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxRPCBody)).Decode(&r); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if r.Error != nil {
		return fmt.Errorf("%s: %s %s", method, nodeText(r.Error.Message), nodeText(r.Error.Data))
	}
	return json.Unmarshal(r.Result, result)
}

// Network returns the chain id the node serves. publish checks it before
// anything else: a URL proves nothing about the chain behind it.
func (c *rpcClient) Network() (string, error) {
	var st struct {
		NodeInfo struct {
			Network string `json:"network"`
		} `json:"node_info"`
	}
	if err := c.call("status", map[string]string{}, &st); err != nil {
		return "", err
	}
	return st.NodeInfo.Network, nil
}

// abciError is a query the node refused, with its amino type.
type abciError struct {
	Type   string
	Detail string
}

func (e *abciError) Error() string {
	if e.Detail == "" {
		return nodeText(e.Type)
	}
	return nodeText(e.Type) + ": " + nodeText(e.Detail)
}

// maxRPCBody caps one node response; the largest publish reads (a roster
// page, an edition of 200 candidates) stay far below it.
const maxRPCBody = 8 << 20

// nodeText escapes what a node wrote before it reaches the terminal, so a
// hostile node cannot send control sequences.
func nodeText(s string) string {
	q := strconv.QuoteToGraphic(s)
	return q[1 : len(q)-1]
}

// isRealmMissing reports whether err says the package is not on chain.
func isRealmMissing(err error) bool {
	var e *abciError
	return errors.As(err, &e) && e.Type == "/vm.InvalidPkgPathError"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func (c *rpcClient) abciQuery(path, data string) (string, error) {
	var r struct {
		Response struct {
			ResponseBase struct {
				Error json.RawMessage `json:"Error"`
				Data  []byte          `json:"Data"`
				Log   string          `json:"Log"`
			} `json:"ResponseBase"`
		} `json:"response"`
	}
	params := map[string]string{"path": path, "data": base64.StdEncoding.EncodeToString([]byte(data))}
	if err := c.call("abci_query", params, &r); err != nil {
		return "", err
	}
	base := r.Response.ResponseBase
	if len(base.Error) > 0 && string(base.Error) != "null" {
		var e struct {
			Type  string `json:"@type"`
			Value string `json:"value"`
		}
		_ = json.Unmarshal(base.Error, &e)
		detail := e.Value
		if detail == "" {
			detail = strings.TrimPrefix(base.Log, "--= Error =--\n")
		}
		return "", &abciError{Type: e.Type, Detail: firstLine(detail)}
	}
	return string(base.Data), nil
}

// Render is vm/qrender: what gnoweb shows at <pkgPath>:<path>.
func (c *rpcClient) Render(pkgPath, path string) (string, error) {
	return c.abciQuery("vm/qrender", pkgPath+":"+path)
}

// Eval is vm/qeval of a function returning one string, decoded.
func (c *rpcClient) Eval(pkgPath, expr string) (string, error) {
	raw, err := c.abciQuery("vm/qeval", pkgPath+"."+expr)
	if err != nil {
		return "", err
	}
	return decodeQevalString(raw)
}

// decodeQevalString unwraps qeval's rendering of a string result,
// ("<Go-quoted text>" string). The text is Go-quoted, not JSON: it may hold
// \U and \x escapes that only strconv.Unquote reads.
func decodeQevalString(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	const suffix = " string)"
	if !strings.HasPrefix(raw, "(") || !strings.HasSuffix(raw, suffix) {
		return "", fmt.Errorf("qeval result is not a string: %.80q", raw)
	}
	return strconv.Unquote(raw[1 : len(raw)-len(suffix)])
}

// realmReader evaluates read-only functions of a realm.
type realmReader interface {
	Eval(pkgPath, expr string) (string, error)
}

// The realm's JSON contract (design spec, "JSON shapes").
type openEditionJSON struct {
	Number          int      `json:"number"`
	ScorecardSHA256 string   `json:"scorecardSHA256"`
	Operators       []string `json:"operators"`
}

type editionJSON struct {
	Number          int             `json:"number"`
	ComputedAt      string          `json:"computedAt"`
	ScorecardSHA256 string          `json:"scorecardSHA256"`
	Arenas          []string        `json:"arenas"`
	Candidates      []candidateJSON `json:"candidates"`
}

type candidateJSON struct {
	Operator    string  `json:"operator"`
	Moniker     string  `json:"moniker"`
	Total       int64   `json:"total"`
	Rank        int     `json:"rank"`
	Github      int64   `json:"github"`
	ArenaScores []int64 `json:"arenaScores"`
}

// decodeStrict refuses unknown keys, so a realm that drifted from the
// contract stops the run instead of being half read.
func decodeStrict(s string, v any) error {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func readOpenEdition(r realmReader, realm string) (*openEditionJSON, error) {
	s, err := r.Eval(realm, "OpenEditionJSON()")
	if err != nil {
		return nil, err
	}
	if s == "null" {
		return nil, nil
	}
	var e openEditionJSON
	if err := decodeStrict(s, &e); err != nil {
		return nil, fmt.Errorf("OpenEditionJSON: %w", err)
	}
	return &e, nil
}

func readLatest(r realmReader, realm string) (*editionJSON, error) {
	s, err := r.Eval(realm, "LatestJSON()")
	if err != nil {
		return nil, err
	}
	if s == "null" {
		return nil, nil
	}
	var e editionJSON
	if err := decodeStrict(s, &e); err != nil {
		return nil, fmt.Errorf("LatestJSON: %w", err)
	}
	return &e, nil
}
