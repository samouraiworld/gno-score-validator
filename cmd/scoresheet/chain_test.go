package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okBase(data string) string {
	return fmt.Sprintf(`{"Error":null,"Data":%q,"Events":null,"Log":"","Info":""}`, base64.StdEncoding.EncodeToString([]byte(data)))
}

// fakeNode answers JSON-RPC like a gno.land node: status, and abci_query
// from answers keyed by "<path> <decoded data>", each a ResponseBase.
func fakeNode(t *testing.T, network string, answers map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params map[string]string `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "status":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":"x","result":{"node_info":{"network":%q}}}`, network)
		case "abci_query":
			data, _ := base64.StdEncoding.DecodeString(req.Params["data"])
			base, ok := answers[req.Params["path"]+" "+string(data)]
			if !ok {
				t.Errorf("unexpected query %s %s", req.Params["path"], data)
				http.Error(w, "unexpected", http.StatusInternalServerError)
				return
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":"x","result":{"response":{"ResponseBase":%s,"Height":"0"}}}`, base)
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNewRPCClientAddsScheme(t *testing.T) {
	if got := newRPCClient("127.0.0.1:26657").url; got != "http://127.0.0.1:26657" {
		t.Fatalf("url = %q", got)
	}
	if got := newRPCClient("https://rpc.gno.land:443").url; got != "https://rpc.gno.land:443" {
		t.Fatalf("url = %q", got)
	}
}

func TestRPCNetwork(t *testing.T) {
	srv := fakeNode(t, "gnoland-1", nil)
	got, err := newRPCClient(srv.URL).Network()
	if err != nil || got != "gnoland-1" {
		t.Fatalf("Network() = %q, %v", got, err)
	}
}

func TestRPCRender(t *testing.T) {
	srv := fakeNode(t, "dev", map[string]string{"vm/qrender gno.land/r/gnops/valopers:?page=2": okBase("page two")})
	got, err := newRPCClient(srv.URL).Render(valopersRealm, "?page=2")
	if err != nil || got != "page two" {
		t.Fatalf("Render = %q, %v", got, err)
	}
}

func TestRPCEvalUnquotesGoString(t *testing.T) {
	raw := `("{\"m\":\"\U0001fae9\x7f\"}" string)`
	srv := fakeNode(t, "dev", map[string]string{"vm/qeval " + defaultRealm + ".LatestJSON()": okBase(raw)})
	got, err := newRPCClient(srv.URL).Eval(defaultRealm, "LatestJSON()")
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"m\":\"\U0001fae9\x7f\"}"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDecodeQevalStringRefusesOtherTypes(t *testing.T) {
	if _, err := decodeQevalString("(2 int)"); err == nil {
		t.Fatal("an int result was accepted as a string")
	}
}

func TestRPCAbciErrorKeepsType(t *testing.T) {
	base := `{"Error":{"@type":"/vm.InvalidPkgPathError"},"Data":null,"Log":"--= Error =--\nData: vm.InvalidPkgPathError{}\nMsg Traces:"}`
	srv := fakeNode(t, "dev", map[string]string{"vm/qeval " + defaultRealm + ".OpenEditionJSON()": base})
	_, err := newRPCClient(srv.URL).Eval(defaultRealm, "OpenEditionJSON()")
	if !isRealmMissing(err) {
		t.Fatalf("err = %v, want a missing-realm error", err)
	}
	if !strings.Contains(err.Error(), "InvalidPkgPathError") {
		t.Fatalf("err = %v does not name the error type", err)
	}
}

func TestRPCHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()
	if _, err := newRPCClient(srv.URL).Network(); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("err = %v, want HTTP 502", err)
	}
}

type stubEval map[string]string

func (s stubEval) Eval(_, expr string) (string, error) {
	v, ok := s[expr]
	if !ok {
		return "", fmt.Errorf("unexpected eval %s", expr)
	}
	return v, nil
}

func TestReadOpenEdition(t *testing.T) {
	got, err := readOpenEdition(stubEval{"OpenEditionJSON()": "null"}, defaultRealm)
	if err != nil || got != nil {
		t.Fatalf("null: %+v, %v", got, err)
	}
	got, err = readOpenEdition(stubEval{"OpenEditionJSON()": `{"number":2,"scorecardSHA256":"ab","operators":["g1x"]}`}, defaultRealm)
	if err != nil || got.Number != 2 || got.ScorecardSHA256 != "ab" || len(got.Operators) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := readOpenEdition(stubEval{"OpenEditionJSON()": `{"number":2,"scorecardSHA256":"ab","operators":[],"extra":1}`}, defaultRealm); err == nil {
		t.Fatal("an unknown key was accepted: the contract changed unnoticed")
	}
}

func TestReadLatestParsesContractShape(t *testing.T) {
	js := `{"number":1,"computedAt":"2026-10-13T09:00:00Z","scorecardSHA256":"ab","arenas":["challenge","test13"],` +
		`"candidates":[{"operator":"g1x","moniker":"AviaOne","total":8743,"rank":3,"github":230,"arenaScores":[9000,8500]}]}`
	got, err := readLatest(stubEval{"LatestJSON()": js}, defaultRealm)
	if err != nil {
		t.Fatal(err)
	}
	c := got.Candidates[0]
	if got.Number != 1 || len(got.Arenas) != 2 || c.Moniker != "AviaOne" || c.Total != 8743 || c.Rank != 3 || c.Github != 230 || c.ArenaScores[1] != 8500 {
		t.Fatalf("got %+v", got)
	}
	if got, err := readLatest(stubEval{"LatestJSON()": "null"}, defaultRealm); err != nil || got != nil {
		t.Fatalf("null: %+v, %v", got, err)
	}
}
