package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// mutationTestPDF returns a plain two-page PDF, each page drawing one line of
// text in Helvetica, so every mutation has text to keep and a page to move.
func mutationTestPDF(t *testing.T) []byte {
	t.Helper()
	page := func(text string) string {
		return fmt.Sprintf("BT /F1 12 Tf 72 700 Td (%s) Tj ET", text)
	}
	first, second := page("First page text"), page("Second page text")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(first), first),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(second), second),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// runMutationFixtures runs f over one PDF at the given profiles, the way the
// runner does after Prepare, and returns the verdicts in profile order.
func runMutationFixtures(t *testing.T, f *mutationFeature, data []byte, profiles ...string) []Verdict {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	fixtures := make([]Fixture, 0, len(profiles))
	for _, p := range profiles {
		fixtures = append(fixtures, Fixture{ID: p + "/doc", PDFPath: path})
	}
	if err := f.Prepare(context.Background(), fixtures); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	verdicts := make([]Verdict, 0, len(fixtures))
	for _, fx := range fixtures {
		out, err := f.Run(context.Background(), fx.PDFPath, fx.ID)
		if err != nil {
			t.Fatalf("Run(%s): %v", fx.ID, err)
		}
		verdicts = append(verdicts, f.Compare(fx.ID, out))
	}
	return verdicts
}

// specNamed returns the mutation spec called name.
func specNamed(t *testing.T, name string) mutationSpec {
	t.Helper()
	for _, s := range mutationSpecs {
		if s.name == name {
			return s
		}
	}
	t.Fatalf("no mutation spec %q", name)
	return mutationSpec{}
}

// TestMutationFeatures_CleanDocumentPassesEveryCheck runs every mutation on a
// plain document none of them has a reason to break. Each must write, reopen,
// keep its promises and move the rule set only as far as it may, at a PDF/A
// and a PDF/UA profile, and emit every metric it owns, zero included.
func TestMutationFeatures_CleanDocumentPassesEveryCheck(t *testing.T) {
	t.Parallel()
	data := mutationTestPDF(t)
	for _, spec := range mutationSpecs {
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()
			f := newMutationFeature(spec)
			for _, v := range runMutationFixtures(t, f, data, "PDF_A-2b", "PDF_UA-1") {
				if v.Skipped || !v.Match {
					t.Errorf("%s: skipped=%t match=%t reason %q, want a match", v.FixtureID, v.Skipped, v.Match, v.Reason)
				}
				for _, m := range spec.metrics() {
					if got, ok := v.Metrics[m]; !ok || got != 0 {
						t.Errorf("%s: metric %s = %d (present %t), want an emitted 0", v.FixtureID, m, got, ok)
					}
				}
			}
		})
	}
}

// TestMutationFeature_PDFLevelFailureCountsOnTheAnchorOnly plants a plan that
// promises one page too many. The page count does not depend on the profile,
// so only the PDF's first fixture counts it, and the other still validates.
func TestMutationFeature_PDFLevelFailureCountsOnTheAnchorOnly(t *testing.T) {
	t.Parallel()
	spec := specNamed(t, "sign")
	apply := spec.apply
	spec.apply = func(ctx context.Context, f *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
		plan, err := apply(ctx, f, in, w)
		plan.pages++
		return plan, err
	}
	verdicts := runMutationFixtures(t, newMutationFeature(spec), mutationTestPDF(t), "PDF_A-2b", "PDF_UA-1")
	anchor, other := verdicts[0], verdicts[1]
	if anchor.Match || anchor.Metrics[metricPagesWrong] != 1 {
		t.Errorf("anchor: match=%t pages_wrong=%d reason %q, want a pages_wrong mismatch", anchor.Match, anchor.Metrics[metricPagesWrong], anchor.Reason)
	}
	if !strings.Contains(anchor.Reason, "2 pages, want 3") {
		t.Errorf("anchor reason %q does not name the promised count", anchor.Reason)
	}
	if !other.Match || other.Metrics[metricPagesWrong] != 0 {
		t.Errorf("other profile: match=%t pages_wrong=%d, want a match that does not count the PDF again", other.Match, other.Metrics[metricPagesWrong])
	}
}

// TestMutationFeature_WriteFailureSkipsTheOtherProfiles plants a mutation
// error the Writer does not document as a refusal: the anchor counts
// write_failed, and the other fixtures, with no output to validate, skip.
func TestMutationFeature_WriteFailureSkipsTheOtherProfiles(t *testing.T) {
	t.Parallel()
	spec := specNamed(t, "stamp")
	spec.apply = func(context.Context, *mutationFeature, *mutationInput, *pdf.Writer) (mutationPlan, error) {
		return mutationPlan{}, fmt.Errorf("planted failure")
	}
	verdicts := runMutationFixtures(t, newMutationFeature(spec), mutationTestPDF(t), "PDF_A-2b", "PDF_UA-1")
	if v := verdicts[0]; v.Match || v.Metrics[metricWriteFailed] != 1 {
		t.Errorf("anchor: match=%t write_failed=%d, want a write_failed mismatch", v.Match, v.Metrics[metricWriteFailed])
	}
	if v := verdicts[1]; !v.Skipped || !strings.Contains(v.Reason, "PDF_A-2b/doc") {
		t.Errorf("other profile: skipped=%t reason %q, want a skip naming the anchor", v.Skipped, v.Reason)
	}
}

// TestMutationFeature_DocumentedRefusalSkipsEveryFixture keeps a refusal the
// Writer documents out of the failure counts: a redaction of content it
// cannot remove exactly is a skip, not a regression.
func TestMutationFeature_DocumentedRefusalSkipsEveryFixture(t *testing.T) {
	t.Parallel()
	spec := specNamed(t, "redact")
	spec.apply = func(context.Context, *mutationFeature, *mutationInput, *pdf.Writer) (mutationPlan, error) {
		return mutationPlan{}, fmt.Errorf("planted: %w", pdf.ErrRedactionRefused)
	}
	for _, v := range runMutationFixtures(t, newMutationFeature(spec), mutationTestPDF(t), "PDF_A-2b", "PDF_UA-1") {
		if !v.Skipped || len(v.Metrics) != 0 {
			t.Errorf("%s: skipped=%t metrics=%v, want a skip with no metrics", v.FixtureID, v.Skipped, v.Metrics)
		}
	}
}

// TestMutationFeature_RuleSetChangeIsCountedPerProfile plants an allowance
// that demands a rule the output cannot fail, so every profile's cell counts
// ruleset_changed on its own.
func TestMutationFeature_RuleSetChangeIsCountedPerProfile(t *testing.T) {
	t.Parallel()
	spec := specNamed(t, "sign")
	spec.allowance = func(ruleContext) ruleAllowance {
		return ruleAllowance{mustFail: []string{"99.9-t01"}}
	}
	for _, v := range runMutationFixtures(t, newMutationFeature(spec), mutationTestPDF(t), "PDF_A-2b", "PDF_UA-1") {
		if v.Match || v.Metrics[metricRulesetChanged] != 1 || !strings.Contains(v.Reason, "does not fail 99.9-t01") {
			t.Errorf("%s: match=%t ruleset_changed=%d reason %q, want a ruleset_changed mismatch", v.FixtureID, v.Match, v.Metrics[metricRulesetChanged], v.Reason)
		}
	}
}

// TestMutationFeature_ProfilelessFixtureSkipsTheRuleCheck runs a corpus
// without profiles, the "none" mode: the PDF-level checks run, the rule check
// has no profile to run at, and ruleset_changed still reads 0.
func TestMutationFeature_ProfilelessFixtureSkipsTheRuleCheck(t *testing.T) {
	t.Parallel()
	f := newMutationFeature(specNamed(t, "stamp"))
	path := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(path, mutationTestPDF(t), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	out, err := f.Run(context.Background(), path, "doc")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	v := f.Compare("doc", out)
	if !v.Match || v.Metrics[metricRulesetChanged] != 0 {
		t.Errorf("match=%t ruleset_changed=%d reason %q, want a match", v.Match, v.Metrics[metricRulesetChanged], v.Reason)
	}
}

// TestMutationFeature_CompareCountsEachMetricOnce keeps one PDF from counting
// three times for three wrong pages.
func TestMutationFeature_CompareCountsEachMetricOnce(t *testing.T) {
	t.Parallel()
	f := newMutationFeature(specNamed(t, "flatten"))
	blob, err := json.Marshal(mutationOutput{Anchor: true, Failures: []mutationFailure{
		{Metric: metricTextWrong, Detail: "page 1"},
		{Metric: metricTextWrong, Detail: "page 2"},
		{Metric: metricPromiseBroken, Detail: "field left"},
	}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	v := f.Compare("PDF_A-2b/doc", blob)
	if v.Match || v.Metrics[metricTextWrong] != 1 || v.Metrics[metricPromiseBroken] != 1 {
		t.Errorf("match=%t metrics=%v, want one of each", v.Match, v.Metrics)
	}
	if _, ok := v.Metrics[metricQPDFFailed]; !ok {
		t.Errorf("metrics=%v, want qpdf_failed emitted at zero", v.Metrics)
	}
	if len(v.Mismatches) != 1 {
		t.Errorf("mismatches = %d, want the failure record", len(v.Mismatches))
	}
}

// TestMutationMetricsAreCostMetrics keeps every metric a mutate-* feature
// emits in costMetrics. Without the entry, the comparator reads a rise in a
// failure count as an improvement.
func TestMutationMetricsAreCostMetrics(t *testing.T) {
	t.Parallel()
	for _, spec := range mutationSpecs {
		for _, m := range spec.metrics() {
			if !costMetrics[m] {
				t.Errorf("mutate-%s emits %s, which costMetrics does not list", spec.name, m)
			}
		}
	}
}

// TestMutationFeatures_AreRegistered keeps every spec reachable from --feature.
func TestMutationFeatures_AreRegistered(t *testing.T) {
	t.Parallel()
	names := make(map[string]bool)
	for _, ff := range featureRegistry {
		names[ff().Name()] = true
	}
	for _, spec := range mutationSpecs {
		if !names["mutate-"+spec.name] {
			t.Errorf("mutate-%s is not in featureRegistry", spec.name)
		}
	}
}

func TestRuleAllowance_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		a       ruleAllowance
		in, out []string
		want    string
	}{
		{name: "unchanged", in: []string{"6.2.2-t01"}, out: []string{"6.2.2-t01"}},
		{name: "added", in: nil, out: []string{"6.2.2-t01"}, want: "added 6.2.2-t01"},
		{name: "removed", in: []string{"6.2.2-t01"}, out: nil, want: "removed 6.2.2-t01"},
		{
			name: "clause prefix allows a removal",
			a:    ruleAllowance{mayRemove: []ruleMatch{"6.1."}},
			in:   []string{"6.1.13-t11", "6.1.3-t02"},
		},
		{
			name: "clause prefix does not reach a sibling clause",
			a:    ruleAllowance{mayRemove: []ruleMatch{"6.1."}},
			in:   []string{"6.10-t01"},
			want: "removed 6.10-t01",
		},
		{
			name: "exact id allows an addition",
			a:    ruleAllowance{mayAdd: []ruleMatch{"6.2.4.3-t02"}},
			out:  []string{"6.2.4.3-t02"},
		},
		{
			name: "exact id is not a prefix",
			a:    ruleAllowance{mayAdd: []ruleMatch{"6.2.4.3-t02"}},
			out:  []string{"6.2.4.3-t021"},
			want: "added 6.2.4.3-t021",
		},
		{name: "any removal", a: ruleAllowance{anyRemove: true}, in: []string{"7.1-t03", "6.2.2-t02"}},
		{
			name: "must fail",
			a:    ruleAllowance{mustFail: []string{"6.1.3-t02"}},
			want: "does not fail 6.1.3-t02",
		},
		{
			name: "must fail is an allowed addition",
			a:    ruleAllowance{mustFail: []string{"6.1.3-t02"}},
			out:  []string{"6.1.3-t02"},
		},
		{
			name: "must pass",
			a:    ruleAllowance{anyRemove: true, mustPass: []string{"7.1-t11"}},
			in:   []string{"7.1-t11"},
			out:  []string{"7.1-t11"},
			want: "still fails 7.1-t11",
		},
		{
			name: "every kind at once, sorted",
			a:    ruleAllowance{mustFail: []string{"9-t01"}},
			in:   []string{"2-t01", "1-t01"},
			out:  []string{"4-t01", "3-t01"},
			want: "added 3-t01 4-t01; removed 1-t01 2-t01; does not fail 9-t01",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.a.check(tc.in, tc.out); got != tc.want {
				t.Errorf("check = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMutationAllowances_Stamp pins what a stamp may change: the unembedded
// standard font and the DeviceRGB fill, and a page's inherited resources made
// explicit. Anything else it adds is a finding.
func TestMutationAllowances_Stamp(t *testing.T) {
	t.Parallel()
	spec := specNamed(t, "stamp")
	for _, tc := range []struct {
		profile string
		in, out []string
		ok      bool
	}{
		{"PDF_A-1b", nil, []string{"6.3.4-t01", "6.2.3.3-t01"}, true},
		{"PDF_A-2b", []string{"6.2.2-t02"}, []string{"6.2.11.4.1-t01", "6.2.4.3-t02"}, true},
		{"PDF_A-4", nil, []string{"6.2.10.4.1-t01", "6.2.4.3-t02"}, true},
		{"PDF_UA-1", nil, []string{"7.21.4.1-t01", "7.2-t34"}, true},
		{"PDF_UA-2", nil, []string{"8.4.5.5.1-t01"}, true},
		{"PDF_UA-1", nil, []string{"7.1-t03"}, false},
		{"PDF_A-2b", nil, []string{"6.2.4.3-t03"}, false},
		{"PDF_A-2b", []string{"6.2.4.3-t03"}, nil, false},
	} {
		got := spec.allowance(ruleContext{profile: tc.profile, in: tc.in, out: tc.out}).check(tc.in, tc.out)
		if (got == "") != tc.ok {
			t.Errorf("%s %v -> %v: check %q, want ok=%t", tc.profile, tc.in, tc.out, got, tc.ok)
		}
	}
}

// TestMutationAllowances_Encrypt requires the Encrypt rule at every PDF/A
// level and nowhere else.
func TestMutationAllowances_Encrypt(t *testing.T) {
	t.Parallel()
	spec := specNamed(t, "encrypt")
	for _, profile := range []string{"PDF_A-1b", "PDF_A-2u", "PDF_A-3b", "PDF_A-4"} {
		if got := spec.allowance(ruleContext{profile: profile}).check(nil, nil); got != "does not fail 6.1.3-t02" {
			t.Errorf("%s, an output without the Encrypt rule: check %q", profile, got)
		}
		if got := spec.allowance(ruleContext{profile: profile}).check(nil, []string{"6.1.3-t02"}); got != "" {
			t.Errorf("%s, an output with the Encrypt rule: check %q, want ok", profile, got)
		}
	}
	if got := spec.allowance(ruleContext{profile: "PDF_UA-1"}).check([]string{"7.16-t01"}, nil); got != "" {
		t.Errorf("PDF_UA-1, permissions reset: check %q, want ok", got)
	}
}

// TestMutationAllowances_PageWithoutResources lets every mutation that writes
// a page pass ISO 19005-4, 6.2.2 test 3, which a page with no resource
// dictionary fails and whose rewrite the Writer gives one, and fail it
// nowhere: an output that fails it where the input did not is a finding.
func TestMutationAllowances_PageWithoutResources(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"stamp", "pages", "sign", "encrypt"} {
		spec := specNamed(t, name)
		out := []string{}
		if name == "encrypt" {
			out = encryptionRules[familyA4]
		}
		if got := spec.allowance(ruleContext{profile: "PDF_A-4"}).check([]string{"6.2.2-t03"}, out); got != "" {
			t.Errorf("%s at PDF_A-4, 6.2.2-t03 passes: check %q, want ok", name, got)
		}
		if got := spec.allowance(ruleContext{profile: "PDF_A-4"}).check(nil, append([]string{"6.2.2-t03"}, out...)); got != "added 6.2.2-t03" {
			t.Errorf("%s at PDF_A-4, 6.2.2-t03 fails: check %q, want it added", name, got)
		}
	}
}

// TestVerifyEncrypt_ReservedPermissionBits pins the P entry mutate-encrypt
// reads from the bytes of an output, and the reserved bits of ISO 32000-2,
// Table 22 it checks there: bits 1-2 clear, bits 7-8 and 13-32 set. The
// litho output of [TestMutationFeatures_CleanDocumentPassesEveryCheck] is the
// control on a real file.
func TestVerifyEncrypt_ReservedPermissionBits(t *testing.T) {
	t.Parallel()

	encrypted := func(p string) []byte {
		return []byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\n" +
			"9 0 obj\n<< /Filter /Standard /V 5 /R 6 /Length 256 /Perms <00> /P " + p + " >>\nendobj\n" +
			"19 0 obj\n<< /Type /Annot /P 1 0 R >>\nendobj\n" +
			"trailer\n<< /Root 1 0 R /Encrypt 9 0 R >>\n%%EOF\n")
	}
	for _, tc := range []struct {
		name    string
		p       string
		want    int32
		wantBad bool
	}{
		{name: "every permission", p: "-4", want: -4},
		{name: "no permission", p: "-3904", want: -3904},
		{name: "written unsigned", p: "4294967292", want: -4},
		{name: "bits 1-2 set", p: "-1", want: -1, wantBad: true},
		{name: "bits 7-8 clear", p: "-196", want: -196, wantBad: true},
		{name: "bits 13-32 clear", p: "4092", want: 4092, wantBad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := encryptionPermissions(encrypted(tc.p))
			if err != nil {
				t.Fatalf("encryptionPermissions: %v", err)
			}
			if got != tc.want {
				t.Fatalf("P = %d, want %d", got, tc.want)
			}
			if broken := reservedPermissionBits(got); (broken != "") != tc.wantBad {
				t.Errorf("reservedPermissionBits(%d) = %q, want broken %t", got, broken, tc.wantBad)
			}
		})
	}

	for _, tc := range []struct {
		name string
		data string
	}{
		{name: "no /Encrypt entry", data: "trailer\n<< /Root 1 0 R >>\n"},
		{name: "an undefined dictionary", data: "trailer\n<< /Root 1 0 R /Encrypt 9 0 R >>\n"},
		{name: "no P entry", data: "9 0 obj\n<< /Filter /Standard /Perms <00> >>\nendobj\ntrailer\n<< /Encrypt 9 0 R >>\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if p, err := encryptionPermissions([]byte(tc.data)); err == nil {
				t.Errorf("encryptionPermissions = %d, want an error", p)
			}
		})
	}
}

// TestMutationAllowances_AutoTag separates a tagged output, whose structure
// rules may move, from a deferral, which must change nothing.
func TestMutationAllowances_AutoTag(t *testing.T) {
	t.Parallel()
	spec := specNamed(t, "autotag")
	untagged := []string{"6.2-t01", "7.1-t11", "7.1-t03"}

	tagged := []string{"7.2-t22"}
	if got := spec.allowance(ruleContext{profile: "PDF_UA-1", in: untagged, out: tagged}).check(untagged, tagged); got != "" {
		t.Errorf("tagged output: check %q, want ok", got)
	}
	noMarkInfo := []string{"6.2-t01"}
	if got := spec.allowance(ruleContext{profile: "PDF_UA-1", in: untagged, out: noMarkInfo}).check(untagged, noMarkInfo); got != "still fails 6.2-t01" {
		t.Errorf("tree without /MarkInfo: check %q", got)
	}
	deferred := []string{"6.2-t01", "7.1-t11"}
	if got := spec.allowance(ruleContext{profile: "PDF_UA-1", in: untagged, out: deferred}).check(untagged, deferred); got != "removed 7.1-t03" {
		t.Errorf("a deferral that changed something: check %q", got)
	}
	if got := spec.allowance(ruleContext{profile: "PDF_A-2b", in: nil, out: []string{"6.7.3.3-t01"}}).check(nil, []string{"6.7.3.3-t01"}); got != "added 6.7.3.3-t01" {
		t.Errorf("a level B profile: check %q, want the strict allowance", got)
	}
}

func TestQPDFKinds(t *testing.T) {
	t.Parallel()
	const path = "/tmp/litho-mutate-1.pdf"
	clean := "checking " + path + "\nPDF Version: 1.7\nFile is not encrypted\nFile is not linearized\n" +
		"No syntax or stream encoding errors found; the file may still contain\nerrors that qpdf cannot detect\n"
	if kinds := qpdfKinds(clean, path, nil); len(kinds) != 0 {
		t.Errorf("clean output: kinds %v, want none", kinds)
	}

	warned := "checking " + path + "\nWARNING: " + path + ", object 15 0 at offset 1747: this widget annotation is not reachable\n" +
		"qpdf: operation succeeded with warnings\n"
	renumbered := strings.ReplaceAll(warned, "15 0 at offset 1747", "9 0 at offset 20")
	a, b := qpdfKinds(warned, path, nil), qpdfKinds(renumbered, path, nil)
	want := "WARNING: FILE, object N N at offset N: this widget annotation is not reachable"
	if len(a) != 1 || !a[want] || len(b) != 1 || !b[want] {
		t.Errorf("warned output: kinds %v and %v, want the one class %q", a, b, want)
	}

	fatal := "qpdf: " + path + ": invalid password\n"
	if kinds := qpdfKinds(fatal, path, nil); !kinds["qpdf: FILE: invalid password"] {
		t.Errorf("fatal output: kinds %v", kinds)
	}
}

// TestQPDFKinds_ExitWithoutMessage keeps a failing exit that printed nothing
// recognisable from reading as clean.
func TestQPDFKinds_ExitWithoutMessage(t *testing.T) {
	t.Parallel()
	err := exec.Command("sh", "-c", "exit 2").Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Skipf("sh did not exit with a status: %v", err)
	}
	if kinds := qpdfKinds("garbled\n", "/x.pdf", exitErr); !kinds["exit status 2"] {
		t.Errorf("kinds %v, want the exit status", kinds)
	}
}

func TestCompareText(t *testing.T) {
	t.Parallel()
	in := &mutationInput{text: []pageText{{text: "abc"}, {text: "def"}, {err: "unreadable"}}}
	out := []pageText{{text: "def"}, {text: "abc"}, {text: "anything"}}

	if got := compareText(in, out, []int{1, 0, 2}, 0, false); got != nil {
		t.Errorf("moved pages: %v, want none", got)
	}
	if got := compareText(in, out, []int{0, 1, 2}, 0, false); len(got) != 1 || !strings.Contains(got[0].Detail, "page 1 (input page 1)") {
		t.Errorf("swapped pages: %v, want page 1 named", got)
	}
	if got := compareText(in, out, []int{0, 0, 2}, 1, false); got != nil {
		t.Errorf("from 1: %v, want page 1 skipped", got)
	}
	superset := []pageText{{text: "c-b-a+field"}}
	if got := compareText(in, superset, []int{0}, 0, true); got != nil {
		t.Errorf("superset: %v, want none", got)
	}
	if got := compareText(in, []pageText{{text: "ab"}}, []int{0}, 0, true); len(got) != 1 {
		t.Errorf("lost rune: %v, want one failure", got)
	}
	if got := compareText(in, []pageText{{err: "boom"}}, []int{0}, 0, false); len(got) != 1 || !strings.Contains(got[0].Detail, "boom") {
		t.Errorf("unreadable output page: %v, want the error", got)
	}
}
