package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeKositGolden(t *testing.T, dir string, jobs []kositJob) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(kositReport{Jobs: jobs})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "jobs.json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestSchematronFeature_LoadGoldens(t *testing.T) {
	dir := t.TempDir()
	writeKositGolden(t, dir, []kositJob{
		{File: "a.pdf", Verdict: "pass"},
		{File: "b.pdf", Verdict: "fail", Rules: []string{"BR-DE-1"}},
		{File: "c.pdf", Verdict: "skip", Reason: "no scenario applicable"},
	})
	f := newSchematronFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	for _, fx := range []string{"a", "b", "c"} {
		if !f.HasGolden(fx) {
			t.Errorf("HasGolden(%s): got false, want true", fx)
		}
	}
}

func TestSchematronFeature_LoadGoldens_NoFile(t *testing.T) {
	dir := t.TempDir()
	f := newSchematronFeature()
	if err := f.LoadGoldens(dir); !errors.Is(err, ErrNoGoldens) {
		t.Errorf("err: got %v, want errors.Is(_, ErrNoGoldens)", err)
	}
}

func TestSchematronFeature_Compare_SkipPropagates(t *testing.T) {
	dir := t.TempDir()
	writeKositGolden(t, dir, []kositJob{
		{File: "skipped.pdf", Verdict: "skip", Reason: "no scenario applicable"},
	})
	f := newSchematronFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	// Litho output is irrelevant when KoSIT skipped.
	output, _ := json.Marshal(schematronOutput{Valid: false, Failures: []string{"any"}})
	v := f.Compare("skipped", output)
	if !v.Skipped {
		t.Errorf("Skipped: got false, want true")
	}
	if v.Reason != "no scenario applicable" {
		t.Errorf("Reason: got %q, want %q", v.Reason, "no scenario applicable")
	}
	if v.Score != 0 {
		t.Errorf("Score: got %v, want 0 (skipped does not count)", v.Score)
	}
}

func TestSchematronFeature_Compare_BothPass(t *testing.T) {
	dir := t.TempDir()
	writeKositGolden(t, dir, []kositJob{{File: "ok.pdf", Verdict: "pass"}})
	f := newSchematronFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	output, _ := json.Marshal(schematronOutput{Valid: true})
	v := f.Compare("ok", output)
	if !v.Match {
		t.Errorf("Match: got false, want true")
	}
	if v.Score != 1.0 {
		t.Errorf("Score: got %v, want 1.0", v.Score)
	}
}

func TestSchematronFeature_Compare_BothFail(t *testing.T) {
	dir := t.TempDir()
	writeKositGolden(t, dir, []kositJob{
		{File: "broken.pdf", Verdict: "fail", Rules: []string{"BR-DE-1"}},
	})
	f := newSchematronFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	output, _ := json.Marshal(schematronOutput{Valid: false, Failures: []string{"clause CII test 9: ..."}})
	v := f.Compare("broken", output)
	if !v.Match {
		t.Errorf("Match: got false, want true (both report invalid)")
	}
}

func TestSchematronFeature_Compare_FalseNegative(t *testing.T) {
	// Litho passes, KoSIT fails: expected for most XRechnung fixtures
	// until litho gains Schematron evaluation. The cell counts it as
	// a false_negative so the regression tracker can watch the gap.
	dir := t.TempDir()
	writeKositGolden(t, dir, []kositJob{
		{
			File: "fx.pdf", Verdict: "fail", Rules: []string{"BR-DE-2"},
			Report: "<report>BR-DE-2 failed</report>",
		},
	})
	f := newSchematronFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	output, _ := json.Marshal(schematronOutput{Valid: true})
	v := f.Compare("fx", output)
	if v.Match {
		t.Errorf("Match: got true, want false")
	}
	if v.Metrics["false_negative"] != 1 {
		t.Errorf("false_negative: got %d, want 1", v.Metrics["false_negative"])
	}
	if len(v.Mismatches) != 1 {
		t.Fatalf("Mismatches: got %d, want 1", len(v.Mismatches))
	}
	var mm schematronMismatch
	if err := json.Unmarshal(v.Mismatches[0], &mm); err != nil {
		t.Fatalf("unmarshal mismatch: %v", err)
	}
	if mm.KositVerdict != "fail" || !mm.LithoValid {
		t.Errorf("mismatch: got kosit=%q litho=%v, want kosit=fail litho=true",
			mm.KositVerdict, mm.LithoValid)
	}
	if len(mm.KositRules) != 1 || mm.KositRules[0] != "BR-DE-2" {
		t.Errorf("mismatch rules: got %v, want [BR-DE-2]", mm.KositRules)
	}
}

func TestSchematronFeature_Compare_FalsePositive(t *testing.T) {
	// Litho says invalid, KoSIT says valid. Real bug signal: litho
	// flagged a structurally fine document.
	dir := t.TempDir()
	writeKositGolden(t, dir, []kositJob{{File: "ok.pdf", Verdict: "pass"}})
	f := newSchematronFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	output, _ := json.Marshal(schematronOutput{
		Valid:    false,
		Failures: []string{"clause INVOICE test 7: ..."},
	})
	v := f.Compare("ok", output)
	if v.Match {
		t.Errorf("Match: got true, want false")
	}
	if v.Metrics["false_positive"] != 1 {
		t.Errorf("false_positive: got %d, want 1", v.Metrics["false_positive"])
	}
}

func TestSchematronFeature_Compare_NoGolden(t *testing.T) {
	f := newSchematronFeature()
	v := f.Compare("orphan", []byte(`{"valid":true}`))
	if !v.Skipped {
		t.Errorf("Skipped: got false, want true")
	}
	if v.Reason != "no golden" {
		t.Errorf("Reason: got %q, want %q", v.Reason, "no golden")
	}
}
