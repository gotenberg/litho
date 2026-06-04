package tagging

import (
	"strings"
	"testing"
)

func TestInjectMarks_WrapsAndPreserves(t *testing.T) {
	content := []byte("AAA BBB CCC")
	spans := []markSpan{
		{start: 0, end: 3, mcid: 0, tag: RoleH1},
		{start: 4, end: 7, mcid: 1, tag: RoleP},
	}
	out, err := injectMarks(content, spans)
	if err != nil {
		t.Fatalf("injectMarks: %v", err)
	}
	got := string(out)

	for _, want := range []string{
		"/H1 <</MCID 0>> BDC\nAAA\nEMC\n",
		"/P <</MCID 1>> BDC\nBBB\nEMC\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\ngot: %q", want, got)
		}
	}
	// The unmarked tail and the inter-span gap survive verbatim.
	if !strings.HasSuffix(got, " CCC") {
		t.Errorf("trailing content not preserved: %q", got)
	}
	if strings.Count(got, "BDC") != 2 || strings.Count(got, "EMC") != 2 {
		t.Errorf("BDC/EMC counts wrong: %q", got)
	}
}

func TestInjectMarks_Empty(t *testing.T) {
	content := []byte("AAA")
	out, err := injectMarks(content, nil)
	if err != nil || string(out) != "AAA" {
		t.Errorf("empty spans: got %q, %v; want unchanged", out, err)
	}
}

func TestInjectMarks_Rejects(t *testing.T) {
	content := []byte("AAAA")
	cases := map[string][]markSpan{
		"overlap":      {{start: 0, end: 3}, {start: 2, end: 4}},
		"out of range": {{start: 0, end: 99}},
		"negative":     {{start: -1, end: 2}},
		"inverted":     {{start: 3, end: 1}},
	}
	for name, spans := range cases {
		if _, err := injectMarks(content, spans); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
