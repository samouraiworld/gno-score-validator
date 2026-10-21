package main

import (
	"fmt"
	"strings"
	"testing"
)

func rosterLine(moniker, operator string) string {
	return fmt.Sprintf(" * [%s](/r/gnops/valopers:%s) - [profile](/r/demo/profile:u/%s)\n", moniker, operator, operator)
}

const rosterIntro = "# Welcome to the **Valopers** realm\n\nUse [Register](/r/gnops/valopers$help&func=Register).\n\n"

type fakeRenderer struct {
	pages map[string]string
	asked []string
}

func (f *fakeRenderer) Render(pkgPath, path string) (string, error) {
	if pkgPath != valopersRealm {
		return "", fmt.Errorf("unexpected realm %s", pkgPath)
	}
	f.asked = append(f.asked, path)
	return f.pages[path], nil
}

func TestParseValoperList(t *testing.T) {
	raw := rosterIntro + rosterLine("KonsorTech", testOperator(1)) + rosterLine("Grand Valley", testOperator(2)) +
		"\n**1** | [2](?page=2)\n"
	got := parseValoperList(raw)
	if len(got) != 2 || got[0] != (valoper{"KonsorTech", testOperator(1)}) || got[1] != (valoper{"Grand Valley", testOperator(2)}) {
		t.Fatalf("got %+v", got)
	}
}

func TestFetchValopersSinglePage(t *testing.T) {
	f := &fakeRenderer{pages: map[string]string{"": rosterIntro + rosterLine("A", testOperator(1)) + rosterLine("B", testOperator(2))}}
	got, err := fetchValopers(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(f.asked) != 1 {
		t.Fatalf("got %d valopers after %d reads", len(got), len(f.asked))
	}
}

func TestFetchValopersWalksPages(t *testing.T) {
	f := &fakeRenderer{pages: map[string]string{
		"":        rosterIntro + rosterLine("A", testOperator(1)) + rosterLine("B", testOperator(2)) + "\n**1** | [2](?page=2)\n",
		"?page=2": rosterLine("C", testOperator(3)) + "\n[1](?page=1) | **2**\n",
	}}
	got, err := fetchValopers(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[2].Moniker != "C" {
		t.Fatalf("got %+v", got)
	}
	if strings.Join(f.asked, ",") != ",?page=2" {
		t.Fatalf("read pages %q", f.asked)
	}
}

func TestFetchValopersRefusesAdvertisedEmptyPage(t *testing.T) {
	f := &fakeRenderer{pages: map[string]string{
		"": rosterIntro + rosterLine("A", testOperator(1)) + "\n[2](?page=2)\n",
	}}
	if _, err := fetchValopers(f); err == nil || !strings.Contains(err.Error(), "page 2 is empty") {
		t.Fatalf("err = %v, want an empty advertised page", err)
	}
}

func TestFetchValopersRefusesRepeatedPage(t *testing.T) {
	page := rosterLine("A", testOperator(1)) + "\n[2](?page=2) [3](?page=3)\n"
	f := &fakeRenderer{pages: map[string]string{"": rosterIntro + page, "?page=2": page}}
	if _, err := fetchValopers(f); err == nil || !strings.Contains(err.Error(), "page 2 repeats") {
		t.Fatalf("err = %v, want a repeated page", err)
	}
}

func TestFetchValopersEmptyRegistry(t *testing.T) {
	f := &fakeRenderer{pages: map[string]string{"": rosterIntro + "\n\nNo valopers to display."}}
	got, err := fetchValopers(f)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %d valopers, err %v", len(got), err)
	}
}

func TestFetchValopersStopsAtPageCap(t *testing.T) {
	pages := map[string]string{}
	for p := 1; p <= maxValoperPages; p++ {
		path := ""
		if p > 1 {
			path = fmt.Sprintf("?page=%d", p)
		}
		pages[path] = rosterLine(fmt.Sprintf("V%d", p), testOperator(p)) + fmt.Sprintf("[next](?page=%d)\n", p+1)
	}
	if _, err := fetchValopers(&fakeRenderer{pages: pages}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want the page cap", err)
	}
}
