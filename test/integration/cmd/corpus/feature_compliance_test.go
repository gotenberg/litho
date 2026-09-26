package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// complianceFixture builds a compliance feature holding one golden and
// scores litho's output for it.
func complianceFixture(t *testing.T, golden *complianceGolden, got complianceOutput) Verdict {
	t.Helper()
	f := newComplianceFeature()
	f.goldens["PDF_A-2b/fx"] = golden
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	return f.Compare("PDF_A-2b/fx", out)
}

func nonCompliantGolden(rules ...complianceRule) *complianceGolden {
	return &complianceGolden{ProfileDir: "PDF_A-2b", FailedRules: rules}
}

// TestComplianceCompare_RuleSetMismatch pins the case the verdict cannot
// see: a fixture both tools call non-compliant, on which litho drops one rule
// class or invents one. matched stays a verdict comparison, so the cell still
// matches, and ruleset_mismatch carries the divergence.
func TestComplianceCompare_RuleSetMismatch(t *testing.T) {
	t.Parallel()
	golden := nonCompliantGolden(
		complianceRule{Clause: "6.2.4.3", TestNumber: 2},
		complianceRule{Clause: "6.1.7.2", TestNumber: 1},
	)
	tests := []struct {
		name      string
		rules     []string
		lithoOnly []string
		veraOnly  []string
		reason    string
	}{
		{
			name:     "litho drops a rule",
			rules:    []string{"6.1.7.2-t01"},
			veraOnly: []string{"6.2.4.3-t02"},
			reason:   "rule set differs: VeraPDF only 6.2.4.3-t02",
		},
		{
			name:      "litho invents a rule",
			rules:     []string{"6.1.7.2-t01", "6.2.4.3-t02", "6.9-t01"},
			lithoOnly: []string{"6.9-t01"},
			reason:    "rule set differs: litho only 6.9-t01",
		},
		{
			name:      "litho swaps one rule for another",
			rules:     []string{"6.1.7.2-t01", "6.9-t01"},
			lithoOnly: []string{"6.9-t01"},
			veraOnly:  []string{"6.2.4.3-t02"},
			reason:    "rule set differs: litho only 6.9-t01; VeraPDF only 6.2.4.3-t02",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := complianceFixture(t, golden, complianceOutput{Rules: tc.rules})
			if !v.Match {
				t.Errorf("verdict: both sides are non-compliant, want a match, got %q", v.Reason)
			}
			if got := v.Metrics["ruleset_mismatch"]; got != 1 {
				t.Errorf("ruleset_mismatch: got %d, want 1", got)
			}
			if v.Reason != tc.reason {
				t.Errorf("reason: got %q, want %q", v.Reason, tc.reason)
			}
			if len(v.Mismatches) != 1 {
				t.Fatalf("mismatches: got %d, want 1", len(v.Mismatches))
			}
			var mm complianceMismatch
			if err := json.Unmarshal(v.Mismatches[0], &mm); err != nil {
				t.Fatalf("decode mismatch: %v", err)
			}
			if !slices.Equal(mm.LithoOnlyRules, tc.lithoOnly) {
				t.Errorf("litho_only_rules: got %v, want %v", mm.LithoOnlyRules, tc.lithoOnly)
			}
			if !slices.Equal(mm.VerapdfOnlyRules, tc.veraOnly) {
				t.Errorf("verapdf_only_rules: got %v, want %v", mm.VerapdfOnlyRules, tc.veraOnly)
			}
		})
	}
}

// TestComplianceCompare_RuleSetAgreement is the control: equal sets, in any
// order and with VeraPDF naming a rule twice, score zero. The key is emitted
// at zero so the baseline keeps a bar for the metric on a cell where no
// fixture diverges.
func TestComplianceCompare_RuleSetAgreement(t *testing.T) {
	t.Parallel()
	golden := nonCompliantGolden(
		complianceRule{Clause: "6.2.4.3", TestNumber: 2},
		complianceRule{Clause: "6.1.7.2", TestNumber: 1},
		complianceRule{Clause: "6.2.4.3", TestNumber: 2},
	)
	v := complianceFixture(t, golden, complianceOutput{Rules: []string{"6.2.4.3-t02", "6.1.7.2-t01"}})
	if !v.Match {
		t.Errorf("verdict: got a mismatch, %q", v.Reason)
	}
	got, ok := v.Metrics["ruleset_mismatch"]
	if !ok || got != 0 {
		t.Errorf("ruleset_mismatch: got %d (present %v), want 0 and present", got, ok)
	}
	if v.Reason != "" || len(v.Mismatches) != 0 {
		t.Errorf("agreement reported a divergence: reason %q, %d mismatches", v.Reason, len(v.Mismatches))
	}

	compliant := complianceFixture(t, &complianceGolden{ProfileDir: "PDF_A-2b", Compliant: true},
		complianceOutput{Compliant: true})
	if got, ok := compliant.Metrics["ruleset_mismatch"]; !ok || got != 0 {
		t.Errorf("compliant cell ruleset_mismatch: got %d (present %v), want 0 and present", got, ok)
	}
}

// TestComplianceCompare_VerdictMismatchCountsBoth: a false negative is also a
// rule-set mismatch, since litho reports none of the rules VeraPDF fails, and
// the reason names both.
func TestComplianceCompare_VerdictMismatchCountsBoth(t *testing.T) {
	t.Parallel()
	v := complianceFixture(t, nonCompliantGolden(complianceRule{Clause: "6.6.2.1", TestNumber: 1}),
		complianceOutput{Compliant: true})
	if v.Match {
		t.Fatal("verdict: litho compliant against a non-compliant golden matched")
	}
	if v.Metrics["false_negative"] != 1 || v.Metrics["ruleset_mismatch"] != 1 {
		t.Errorf("metrics: got %v, want false_negative=1 and ruleset_mismatch=1", v.Metrics)
	}
	want := "false negative: litho compliant, VeraPDF non-compliant; rule set differs: VeraPDF only 6.6.2.1-t01"
	if v.Reason != want {
		t.Errorf("reason: got %q, want %q", v.Reason, want)
	}
}

// TestDiffBaseline_RuleSetMismatchIsCostMetric pins the direction: more cells
// whose rule set diverges is the regression, and it fails the check while
// matched, the verdict comparison, does not move.
func TestDiffBaseline_RuleSetMismatchIsCostMetric(t *testing.T) {
	t.Parallel()
	cell := func(n int) *Baseline {
		return &Baseline{Corpora: map[string]baselineCorpus{
			"litho": {Features: map[string]baselineFeature{
				"compliance": {Matched: 616, Metrics: map[string]int{"ruleset_mismatch": n}},
			}},
		}}
	}
	regs, imps, _ := diffBaseline(cell(3), cell(40))
	if len(imps) != 0 || len(regs) != 1 {
		t.Fatalf("rise: regs=%v imps=%v, want one regression", regs, imps)
	}
	if !strings.Contains(regs[0], "litho / compliance / ruleset_mismatch: got 40, baseline 3") {
		t.Errorf("regression line: %s", regs[0])
	}
	regs, imps, _ = diffBaseline(cell(3), cell(2))
	if len(regs) != 0 || len(imps) != 1 {
		t.Errorf("drop: regs=%v imps=%v, want one improvement", regs, imps)
	}
}

// TestPrintText_ListsRuleSetMismatches: a cell whose verdicts all match
// still lists the fixtures whose rule sets diverge, with the rules.
func TestPrintText_ListsRuleSetMismatches(t *testing.T) {
	t.Parallel()
	board := &Scoreboard{Corpora: []*CorpusReport{{
		Name: "litho",
		Features: []*FeatureReport{{
			Name:    "compliance",
			Matched: 2,
			Metrics: map[string]int{"ruleset_mismatch": 1},
			Verdicts: []Verdict{
				{FixtureID: "PDF_A-2b/a", Match: true, Metrics: map[string]int{"ruleset_mismatch": 0}},
				{
					FixtureID: "PDF_A-2b/b", Match: true,
					Reason:  "rule set differs: VeraPDF only 6.2.4.3-t02",
					Metrics: map[string]int{"ruleset_mismatch": 1},
				},
			},
		}},
	}}}
	var buf bytes.Buffer
	board.PrintText(&buf)
	out := buf.String()
	for _, want := range []string{
		"ruleset_mismatch=1",
		"litho / compliance rule-set mismatches (1):",
		"PDF_A-2b/b: rule set differs: VeraPDF only 6.2.4.3-t02",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text scoreboard lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "PDF_A-2b/a:") {
		t.Errorf("text scoreboard lists an agreeing fixture:\n%s", out)
	}
}
