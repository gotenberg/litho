package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// TestMain lets the test binary stand in for the corpus CLI as the child the
// robustness feature runs.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "convert-one" {
		os.Exit(runConvertOne(context.Background(), os.Args[2:]))
	}
	os.Exit(m.Run())
}

// TestRobustnessCompare pins each way a conversion fails its expectation.
func TestRobustnessCompare(t *testing.T) {
	t.Parallel()

	f := newRobustnessFeature()
	f.expect = map[string]robustnessExpectation{
		"refused": {Result: "ErrConvertMalformed"},
		"either":  {Result: "any", MaxRSSMiB: 10},
		"secret":  {Result: "pdf", Forbid: []string{"root:"}, Require: []string{"marker"}},
	}
	for _, tc := range []struct {
		id     string
		got    robustnessOutput
		metric string
	}{
		{"plain", robustnessOutput{Result: "pdf"}, ""},
		{"plain", robustnessOutput{Result: "error"}, metricUnexpected},
		{"plain", robustnessOutput{Crash: "signal: killed"}, metricCrash},
		{"plain", robustnessOutput{Result: "panic"}, metricPanic},
		{"plain", robustnessOutput{Result: "pdf", RSSMiB: robustnessMaxRSSMiB + 1}, metricOverMemory},
		{"plain", robustnessOutput{Result: "pdf", Fetches: 1}, metricFetch},
		{"refused", robustnessOutput{Result: "ErrConvertMalformed"}, ""},
		{"refused", robustnessOutput{Result: "pdf"}, metricUnexpected},
		{"either", robustnessOutput{Result: "ErrConvertInputTooLarge"}, ""},
		{"either", robustnessOutput{Result: "panic"}, metricPanic},
		{"either", robustnessOutput{Result: "pdf", RSSMiB: 11}, metricOverMemory},
		{"secret", robustnessOutput{Result: "pdf", Leaked: []string{"root:"}}, metricLeak},
		{"secret", robustnessOutput{Result: "pdf", Missing: []string{"marker"}}, metricMissing},
	} {
		out, err := json.Marshal(tc.got)
		if err != nil {
			t.Fatal(err)
		}
		v := f.Compare(tc.id, out)
		if tc.metric == "" {
			if !v.Match || v.Outcome != outcomePass {
				t.Errorf("%s %+v: %s, want a pass", tc.id, tc.got, v.Reason)
			}
			continue
		}
		if v.Match || v.Outcome != outcomeFail || v.Metrics[tc.metric] != 1 {
			t.Errorf("%s %+v: outcome %s, metrics %v, want a fail counting %s", tc.id, tc.got, v.Outcome, v.Metrics, tc.metric)
		}
	}
}

// TestConvertResultName pins the names the expectations give the
// conversion errors, wrapped as litho wraps them.
func TestConvertResultName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "pdf"},
		{fmt.Errorf("converting: %w", pdf.ErrConvertMalformed), "ErrConvertMalformed"},
		{fmt.Errorf("converting: %w", pdf.ErrConvertInputTooLarge), "ErrConvertInputTooLarge"},
		{fmt.Errorf("x: %w", pdf.ErrInternalPanic), "panic"},
		{fmt.Errorf("x: %w", context.DeadlineExceeded), "timeout"},
		{errors.New("other"), "error"},
	} {
		if got := convertResultName(tc.err); got != tc.want {
			t.Errorf("convertResultName(%v) = %s, want %s", tc.err, got, tc.want)
		}
	}
}

// TestRobustnessRun converts the docx corpus's base document and a hostile
// one in a child process, the test binary through [TestMain].
func TestRobustnessRun(t *testing.T) {
	t.Chdir(filepath.Join("..", "..", "..", ".."))

	fonts, err := fixtureFontDirs(fixtureFontsRoot(), diffFontsTarget)
	if err != nil {
		t.Skipf("fixture fonts: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base, err := readParts(filepath.Join(corpusRoot(), "litho", "docx", "parts", hostileBase))
	if err != nil {
		t.Fatal(err)
	}
	good, err := zipPackage(base)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name, name+".docx")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return filepath.Dir(p)
	}
	f := newRobustnessFeature()
	f.exe, f.fonts = exe, fonts
	f.expect = map[string]robustnessExpectation{"empty": {Result: "ErrConvertMalformed"}}
	for id, data := range map[string][]byte{"base": good, "empty": nil} {
		out, err := f.Run(t.Context(), write(id, data), id)
		if err != nil {
			t.Fatal(err)
		}
		if v := f.Compare(id, out); !v.Match {
			t.Errorf("%s: %s", id, v.Reason)
		}
		var got robustnessOutput
		if err := json.Unmarshal(out, &got); err != nil || (id == "base" && (got.Pages == 0 || got.RSSMiB == 0)) {
			t.Errorf("%s: %s, %v", id, strings.TrimSpace(string(out)), err)
		}
	}
}
