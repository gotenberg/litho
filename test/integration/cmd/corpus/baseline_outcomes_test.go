package main

import (
	"strings"
	"testing"
)

func TestDiffPerFixture_OutcomesAndScores(t *testing.T) {
	t.Parallel()

	want := baselineFeature{
		Outcomes: map[string]string{"a": outcomePass, "b": outcomeFail, "c": outcomeError, "d": outcomePass},
		Scores: map[string]map[string]float64{
			"x": {scoreText: 0.9, scoreLayout: 0.5},
			"y": {scoreText: 0.5},
		},
		ScoreTolerance: 0.02,
	}
	got := baselineFeature{
		Outcomes: map[string]string{"a": outcomeFail, "b": outcomePass, "c": outcomeError},
		Scores: map[string]map[string]float64{
			"x": {scoreText: 0.89, scoreLayout: 0.4},
		},
	}
	regressions, improvements, missing := diffPerFixture("wpt / reftest", want, got)
	joined := func(lines []string) string { return strings.Join(lines, "\n") }
	if len(regressions) != 2 || !strings.Contains(joined(regressions), "/ a: fail, baseline pass") ||
		!strings.Contains(joined(regressions), "/ x / layout") {
		t.Errorf("regressions:\n%s", joined(regressions))
	}
	if strings.Contains(joined(regressions), "text_f1") {
		t.Error("a drop within the tolerance regressed")
	}
	if len(improvements) != 1 || !strings.Contains(improvements[0], "/ b: pass, baseline fail") {
		t.Errorf("improvements:\n%s", joined(improvements))
	}
	if len(missing) != 2 {
		t.Errorf("missing:\n%s", joined(missing))
	}
}

func TestRecordPerFixture_RecordsAnErrorForAVerdictWithoutOutcome(t *testing.T) {
	t.Parallel()

	fr := &FeatureReport{
		RecordsOutcomes: true,
		ScoreTolerance:  0.02,
		Verdicts: []Verdict{
			{FixtureID: "a", Outcome: outcomePass, Scores: map[string]float64{scoreText: 0.123456}},
			{FixtureID: "b", Reason: "timeout"},
			{FixtureID: "c", Skipped: true},
		},
	}
	var bf baselineFeature
	recordPerFixture(&bf, fr)
	if bf.Outcomes["a"] != outcomePass || bf.Outcomes["b"] != outcomeError || len(bf.Outcomes) != 2 {
		t.Errorf("outcomes %v", bf.Outcomes)
	}
	if bf.Scores["a"][scoreText] != 0.1235 || len(bf.Scores["b"]) != 0 {
		t.Errorf("scores %v", bf.Scores)
	}
}
