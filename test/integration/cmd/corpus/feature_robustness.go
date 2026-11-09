package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// robustnessFile is the committed file, in a build corpus's source
// directory, holding how each fixture's conversion is expected to end.
const robustnessFile = "robustness.json"

// robustnessMaxRSSMiB bounds the peak memory of one conversion, in MiB,
// unless the fixture's expectation sets its own: the resident set of the
// child process, font index included.
const robustnessMaxRSSMiB = 1024

// stderrTail is how much of a crashed child's standard error a verdict
// quotes.
const stderrTail = 600

// Metrics the robustness feature counts, one per way a conversion fails it.
const (
	metricCrash      = "crash"
	metricPanic      = "panic"
	metricOverMemory = "over_memory"
	metricFetch      = "fetch"
	metricLeak       = "leak"
	metricMissing    = "missing_text"
	metricUnexpected = "unexpected"
)

// robustnessExpectation is how one fixture's conversion must end.
type robustnessExpectation struct {
	// Result is "pdf", the name of a conversion sentinel (see
	// [convertSentinels]), or "any" for any end but a crash or a panic.
	Result string `json:"result"`
	// MaxRSSMiB overrides [robustnessMaxRSSMiB].
	MaxRSSMiB int64 `json:"max_rss_mib,omitempty"`
	// Forbid lists strings the PDF's text must not hold: what a document
	// tries to pull in from the machine converting it.
	Forbid []string `json:"forbid,omitempty"`
	// Require lists strings the PDF's text must hold: what tells a
	// document read from one printed blank.
	Require []string `json:"require,omitempty"`
	// Why says what the fixture probes and why it must end this way.
	Why string `json:"why,omitempty"`
}

// robustnessOutput is one conversion as the parent saw it.
type robustnessOutput struct {
	convertOneResult
	RSSMiB  int64   `json:"rss_mib"`
	Seconds float64 `json:"seconds"`
	// Crash holds the tail of the standard error of a child that exited
	// without printing a result: a fatal error the converter's recover
	// cannot catch (stack exhaustion, out of memory), or a kill.
	Crash string `json:"crash,omitempty"`
}

// robustnessFeature converts every document of a build corpus, hostile ones
// included, in a child process, and checks each conversion ends the way
// the corpus's robustness.json expects: a PDF, unless it names the error
// the document must be refused with. Any end must come without a crash or
// a recovered panic, under the memory bound, without a network request,
// without the text of a local file the document points at, and with the
// text the expectation requires. The oracle is
// the expectation file: GoldenSource is "litho-self" and the golden path
// the manifest declares is ignored.
type robustnessFeature struct {
	mu     sync.RWMutex
	expect map[string]robustnessExpectation
	fonts  []string
	// exe is the binary whose convert-one subcommand converts a fixture:
	// this one unless a test sets another.
	exe string
	// source is the corpus source directory, read by [SetManifest].
	source string
}

func newRobustnessFeature() *robustnessFeature { return &robustnessFeature{} }

func (f *robustnessFeature) Name() string         { return "robustness" }
func (f *robustnessFeature) GoldenSource() string { return "litho-self" }

// RecordsOutcomes makes the baseline record each fixture's outcome.
func (f *robustnessFeature) RecordsOutcomes() {}

// SetManifest records the corpus source directory robustness.json is in.
func (f *robustnessFeature) SetManifest(m *Manifest) {
	f.mu.Lock()
	f.source = m.SourceDir()
	f.mu.Unlock()
}

// LoadGoldens reads the corpus's robustness.json, which may be absent (every
// fixture then must convert), and the fixture fonts.
func (f *robustnessFeature) LoadGoldens(_ string) error {
	f.mu.RLock()
	source, exe := f.source, f.exe
	f.mu.RUnlock()
	expect := map[string]robustnessExpectation{}
	data, err := os.ReadFile(filepath.Join(source, robustnessFile)) //nolint:gosec // a committed corpus file
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &expect); err != nil {
			return fmt.Errorf("decode %s: %w", filepath.Join(source, robustnessFile), err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	for id, e := range expect {
		if e.Result == "" {
			return fmt.Errorf("%s: %s expects no result", robustnessFile, id)
		}
	}
	fonts, err := fixtureFontDirs(fixtureFontsRoot(), diffFontsTarget)
	if err != nil {
		return err
	}
	if exe == "" {
		if exe, err = os.Executable(); err != nil {
			return err
		}
	}
	f.mu.Lock()
	f.expect, f.fonts, f.exe = expect, fonts, exe
	f.mu.Unlock()
	return nil
}

// HasGolden is true for every fixture: one robustness.json does not list
// is expected to convert.
func (f *robustnessFeature) HasGolden(string) bool { return true }

// expectation returns how fixtureID's conversion must end.
func (f *robustnessFeature) expectation(fixtureID string) robustnessExpectation {
	f.mu.RLock()
	defer f.mu.RUnlock()
	e, ok := f.expect[fixtureID]
	if !ok {
		e = robustnessExpectation{Result: "pdf"}
	}
	if e.MaxRSSMiB == 0 {
		e.MaxRSSMiB = robustnessMaxRSSMiB
	}
	return e
}

// Run converts the fixture in a child process and measures it.
func (f *robustnessFeature) Run(ctx context.Context, srcPath, fixtureID string) ([]byte, error) {
	e := f.expectation(fixtureID)
	f.mu.RLock()
	exe, fonts := f.exe, f.fonts
	f.mu.RUnlock()
	cmd := exec.CommandContext(ctx, exe, "convert-one", //nolint:gosec // this binary, a fixture path
		"--fonts="+strings.Join(fonts, string(os.PathListSeparator)), "--forbid="+strings.Join(e.Forbid, "\n"), "--require="+strings.Join(e.Require, "\n"), srcPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	out := robustnessOutput{Seconds: time.Since(start).Seconds()}
	if cmd.ProcessState != nil {
		out.RSSMiB = maxRSS(cmd.ProcessState) >> 20
	}
	if err := json.Unmarshal(stdout.Bytes(), &out.convertOneResult); runErr != nil || err != nil {
		tail := stderr.String()
		if len(tail) > stderrTail {
			tail = "..." + tail[len(tail)-stderrTail:]
		}
		out.Crash = fmt.Sprintf("%v: %s", runErr, strings.TrimSpace(tail))
	}
	return json.Marshal(out)
}

// Compare checks the conversion ended as expected, within its bounds.
func (f *robustnessFeature) Compare(fixtureID string, output []byte) Verdict {
	v := Verdict{FixtureID: fixtureID, Metrics: map[string]int{}}
	var got robustnessOutput
	if err := json.Unmarshal(output, &got); err != nil {
		v.Outcome, v.Reason = outcomeError, fmt.Sprintf("decode output: %v", err)
		return v
	}
	e := f.expectation(fixtureID)
	var problems []string
	fail := func(metric, format string, args ...any) {
		v.Metrics[metric]++
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	switch {
	case got.Crash != "":
		fail(metricCrash, "crashed: %s", got.Crash)
	case got.Result == "panic":
		fail(metricPanic, "panicked: %s", got.Detail)
	case e.Result != "any" && got.Result != e.Result:
		detail := ""
		if got.Detail != "" {
			detail = " (" + got.Detail + ")"
		}
		fail(metricUnexpected, "ended %s%s, expected %s", got.Result, detail, e.Result)
	}
	if got.RSSMiB > e.MaxRSSMiB {
		fail(metricOverMemory, "peaked at %d MiB, past %d MiB", got.RSSMiB, e.MaxRSSMiB)
	}
	if got.Fetches > 0 {
		fail(metricFetch, "tried %d network requests", got.Fetches)
	}
	if len(got.Leaked) > 0 {
		fail(metricLeak, "the PDF holds %q", got.Leaked)
	}
	if len(got.Missing) > 0 {
		fail(metricMissing, "the PDF lacks %q", got.Missing)
	}
	v.Match = len(problems) == 0
	v.Outcome = outcomeFail
	if v.Match {
		v.Outcome, v.Score = outcomePass, 1
	}
	v.Reason = strings.Join(problems, "; ")
	return v
}
