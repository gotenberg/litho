package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// costSmallInputs are the generated inputs small enough to build and convert
// in a unit test, one of each format.
var costSmallInputs = []string{"text-line", "html-report-s", "md-report-s", "docx-report-s", "odt-report-s", "xlsx-sales-s", "ods-sales-s", "csv-10k", "pptx-20slides", "odp-deck-s"}

// TestCostRowsNameKnownInputs checks every row reads an input the set
// defines, under a name no other row takes.
func TestCostRowsNameKnownInputs(t *testing.T) {
	t.Parallel()

	inputs := costInputs()
	seen := map[string]bool{}
	for _, r := range costRows() {
		if _, ok := inputs[r.input]; !ok {
			t.Errorf("row %s reads the unknown input %q", r.name(), r.input)
		}
		if seen[r.name()] {
			t.Errorf("two rows are named %s", r.name())
		}
		seen[r.name()] = true
		if _, ok := costOps[r.op]; !ok && r.op != "convert" && r.op != "cold" {
			t.Errorf("row %s runs the unknown operation %q", r.name(), r.op)
		}
	}
	for _, in := range costPDFs {
		if in.write != nil || in.pdf == "" {
			t.Errorf("the PDF input %s names no corpus file", in.name)
		}
	}
}

// TestCostGeneratedInputsAreStable builds the small inputs twice and checks
// both builds wrote the same bytes, so a name reads the same file on every
// machine.
func TestCostGeneratedInputsAreStable(t *testing.T) {
	t.Parallel()

	inputs := costInputs()
	var want []costInput
	for _, name := range costSmallInputs {
		want = append(want, inputs[name])
	}
	a, b := t.TempDir(), t.TempDir()
	for _, dir := range []string{a, b} {
		if err := buildCostInputs(dir, want, func(string) {}); err != nil {
			t.Fatal(err)
		}
	}
	for _, in := range want {
		x, err := os.ReadFile(filepath.Join(a, in.name, in.file))
		if err != nil {
			t.Fatal(err)
		}
		y, err := os.ReadFile(filepath.Join(b, in.name, in.file))
		if err != nil {
			t.Fatal(err)
		}
		if len(x) == 0 || !bytes.Equal(x, y) {
			t.Errorf("%s: two builds wrote %d and %d bytes that differ", in.name, len(x), len(y))
		}
	}
	// A build finds what is there and writes nothing again.
	if err := buildCostInputs(a, want, func(s string) { t.Errorf("rebuilt: %s", s) }); err != nil {
		t.Fatal(err)
	}
}

// TestCostMeasuresEachFormat converts the small input of each format in a
// child process, the test binary through [TestMain], and checks the child
// read every quantity a conversion has.
func TestCostMeasuresEachFormat(t *testing.T) {
	t.Chdir(filepath.Join("..", "..", "..", ".."))

	fonts, err := fixtureFontDirs(fixtureFontsRoot(), diffFontsTarget)
	if err != nil {
		t.Skipf("fixture fonts: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	inputs := costInputs()
	dir := t.TempDir()
	var want []costInput
	for _, name := range costSmallInputs {
		want = append(want, inputs[name])
	}
	if err := buildCostInputs(dir, want, func(string) {}); err != nil {
		t.Fatal(err)
	}
	m := costMeasurer{exe: exe, fonts: fonts, copies: 2}
	for _, in := range want {
		row := costRow{op: "convert", input: in.name}
		if in.name == "text-line" {
			row.op = "cold"
		}
		res, proc, err := m.run(t.Context(), row, costSource(in, dir, ""), "")
		if err != nil {
			t.Fatalf("%s: %v", in.name, err)
		}
		c := res.Cost
		if res.Result != "pdf" || c.OutputPages == 0 || c.OutputBytes == 0 || c.InputBytes == 0 || c.AllocBytes == 0 || c.HeapPeakBytes == 0 || c.Copies != 2 || proc.seconds <= 0 {
			t.Errorf("%s: %s %s, %+v, %+v", in.name, res.Result, res.Detail, *c, proc)
		}
		if c.BudgetPeakBytes == 0 && in.name != "pptx-20slides" && in.name != "odp-deck-s" {
			t.Errorf("%s: the budget's peak was not read", in.name)
		}
	}
}

// TestCostMeasuresAPDFOperation runs the PDF operations on a PDF the test
// converts, writing profiles for one.
func TestCostMeasuresAPDFOperation(t *testing.T) {
	t.Chdir(filepath.Join("..", "..", "..", ".."))

	fonts, err := fixtureFontDirs(fixtureFontsRoot(), diffFontsTarget)
	if err != nil {
		t.Skipf("fixture fonts: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	in := costInputs()["html-report-s"]
	if err := buildCostInputs(dir, []costInput{in}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	// The PDF is the conversion of the report.
	src := filepath.Join(dir, "report.pdf")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	job := &costJob{out: f}
	run, _, err := costOperation("convert", costSource(in, dir, ""), fonts)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	m := costMeasurer{exe: exe, fonts: fonts, copies: 1}
	ops := make([]string, 0, len(costOps))
	for op := range costOps {
		ops = append(ops, op)
	}
	slices.Sort(ops)
	profiles := filepath.Join(dir, "profiles")
	for _, op := range ops {
		profile := ""
		if op == "rewrite" {
			profile = profiles
		}
		res, _, err := m.run(t.Context(), costRow{op: op, input: "report"}, src, profile)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if res.Result != "pdf" || res.Cost.AllocBytes == 0 {
			t.Errorf("%s: %s %s", op, res.Result, res.Detail)
		}
		if op != "render" && op != "pdfa" && op != "pdfua" && res.Cost.OutputBytes == 0 {
			t.Errorf("%s: produced nothing", op)
		}
	}
	for _, name := range []string{costCPUProfile, costHeapProfile, costAllocProfile, costMutexProfile, costBlockProfile} {
		if info, err := os.Stat(filepath.Join(profiles, name)); err != nil || info.Size() == 0 {
			t.Errorf("profile %s: %v", name, err)
		}
	}
}

// TestCostReadingSummarizes checks the median, the spread, the ratios and
// the comparison of two readings.
func TestCostReadingSummarizes(t *testing.T) {
	t.Parallel()

	rr := costRowReading{Row: "x", Op: "convert", InputBytes: 100}
	for _, wall := range []float64{3, 1, 2} {
		rr.add(convertOneResult{Result: "pdf", Cost: &costSample{WallSeconds: wall, AllocBytes: 400, BudgetPeakBytes: 50, InputBytes: 100}}, costProcess{rss: 200}, nil)
	}
	rr.summarize()
	if rr.Median["wall_s"] != 2 || rr.Min["wall_s"] != 1 || rr.Max["wall_s"] != 3 || costSpread(rr, "wall_s") != 1 {
		t.Errorf("wall: median %v, min %v, max %v", rr.Median["wall_s"], rr.Min["wall_s"], rr.Max["wall_s"])
	}
	if rr.Median["rss_per_input"] != 2 || rr.Median["rss_per_budget"] != 4 || rr.Median["alloc_per_input"] != 4 {
		t.Errorf("ratios: %v", rr.Median)
	}
	after := rr
	after.Median = map[string]float64{"wall_s": 1}
	dir := t.TempDir()
	write := func(name string, rows ...costRowReading) string {
		data, err := json.Marshal(costReading{Rows: rows})
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	var out bytes.Buffer
	over, err := compareCost(&out, write("a.json", rr), write("b.json", after, costRowReading{Row: "y"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "-50.0") || !strings.Contains(out.String(), "y (after only)") || len(over) != 0 {
		t.Errorf("comparison, %v over:\n%s", over, out.String())
	}

	// A median past its bound is named; one at the bound or under it, or
	// a column with no bound, is not.
	grown := rr
	grown.Median = map[string]float64{"alloc_bytes": 410, "budget_peak_bytes": 51, "wall_s": 9}
	limits, err := parseCostGrowth("alloc_bytes:2.5, budget_peak_bytes:0")
	if err != nil {
		t.Fatal(err)
	}
	over, err = compareCost(io.Discard, write("c.json", rr), write("d.json", grown), limits)
	if err != nil {
		t.Fatal(err)
	}
	if len(over) != 1 || !strings.Contains(over[0], "x budget MiB") {
		t.Errorf("over the bounds: %q", over)
	}
	for _, bad := range []string{"alloc_bytes", "alloc_bytes:x", "nothing:2", "alloc_bytes:-1"} {
		if _, err := parseCostGrowth(bad); err == nil {
			t.Errorf("--max-growth=%s was accepted", bad)
		}
	}

	// The heap a conversion adds, past the process's, over its budget.
	held := costRowReading{Row: "h", Op: "convert", InputBytes: 1}
	held.add(convertOneResult{Result: "pdf", Cost: &costSample{HeapPeakBytes: 70 << 20, HeapBaseBytes: 10 << 20, BudgetPeakBytes: 40 << 20}}, costProcess{}, nil)
	held.summarize()
	if r := held.Median["heap_per_budget"]; r != 1.5 {
		t.Errorf("heap per budget %v, want 1.5", r)
	}
	if got := heapOverBudget([]costRowReading{held, rr}, 1.4); len(got) != 1 || !strings.HasPrefix(got[0], "h:") {
		t.Errorf("over 1.4: %q", got)
	}
	if got := heapOverBudget([]costRowReading{held}, 1.5); len(got) != 0 {
		t.Errorf("over 1.5: %q", got)
	}
	if got := heapOverBudget([]costRowReading{held}, 0); got != nil {
		t.Errorf("no bound: %q", got)
	}

	failed := costRowReading{Row: "z"}
	failed.add(convertOneResult{}, costProcess{}, errCostCrash)
	failed.summarize()
	if failed.Result != "crash" || failed.Median != nil {
		t.Errorf("a crashed run: %+v", failed)
	}
}

// TestSelectCostRows checks names and globs select rows, and a pattern no
// row matches fails.
func TestSelectCostRows(t *testing.T) {
	t.Parallel()

	rows := costRows()
	got, err := selectCostRows(rows, "html-1mib, rewrite/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 || got[0].name() != "html-1mib" || got[1].op != "rewrite" {
		t.Errorf("selected %v", got)
	}
	if _, err := selectCostRows(rows, "nothing-*"); err == nil {
		t.Error("a pattern no row matches was accepted")
	}
}
