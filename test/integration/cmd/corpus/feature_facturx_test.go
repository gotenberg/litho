package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeMustangGolden(t *testing.T, dir string, jobs []mustangJob) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(mustangReport{Jobs: jobs})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "jobs.json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestFacturxFeature_LoadGoldens(t *testing.T) {
	dir := t.TempDir()
	writeMustangGolden(t, dir, []mustangJob{
		{File: "a.pdf", Valid: true, Report: "ok"},
		{File: "b.pdf", Valid: false, Report: "missing fx:DocumentType"},
	})
	f := newFacturxFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	if !f.HasGolden("a") {
		t.Errorf("HasGolden(a): got false, want true")
	}
	if !f.HasGolden("b") {
		t.Errorf("HasGolden(b): got false, want true")
	}
	if f.HasGolden("ghost") {
		t.Errorf("HasGolden(ghost): got true, want false")
	}
}

func TestFacturxFeature_LoadGoldens_NoFile(t *testing.T) {
	dir := t.TempDir()
	f := newFacturxFeature()
	err := f.LoadGoldens(dir)
	if !errors.Is(err, ErrNoGoldens) {
		t.Errorf("err: got %v, want errors.Is(_, ErrNoGoldens)", err)
	}
}

func TestFacturxFeature_LoadGoldens_EmptyJobs(t *testing.T) {
	dir := t.TempDir()
	writeMustangGolden(t, dir, nil)
	f := newFacturxFeature()
	err := f.LoadGoldens(dir)
	if !errors.Is(err, ErrNoGoldens) {
		t.Errorf("err: got %v, want errors.Is(_, ErrNoGoldens)", err)
	}
}

func TestFacturxFeature_Compare_Match(t *testing.T) {
	dir := t.TempDir()
	writeMustangGolden(t, dir, []mustangJob{{File: "ok.pdf", Valid: true}})
	f := newFacturxFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	output, _ := json.Marshal(facturxOutput{Valid: true})
	v := f.Compare("ok", output)
	if !v.Match {
		t.Errorf("Match: got false, want true")
	}
	if v.Score != 1.0 {
		t.Errorf("Score: got %v, want 1.0", v.Score)
	}
	if v.Skipped {
		t.Errorf("Skipped: got true, want false")
	}
}

func TestFacturxFeature_Compare_FalseNegative(t *testing.T) {
	// Mustang says invalid, litho says valid: litho missed a rule.
	dir := t.TempDir()
	writeMustangGolden(t, dir, []mustangJob{
		{File: "broken.pdf", Valid: false, Report: "missing fx:Version"},
	})
	f := newFacturxFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	output, _ := json.Marshal(facturxOutput{Valid: true})
	v := f.Compare("broken", output)
	if v.Match {
		t.Errorf("Match: got true, want false")
	}
	if v.Metrics["false_negative"] != 1 {
		t.Errorf("false_negative metric: got %d, want 1", v.Metrics["false_negative"])
	}
	if len(v.Mismatches) != 1 {
		t.Fatalf("Mismatches: got %d entries, want 1", len(v.Mismatches))
	}
	var mm facturxMismatch
	if err := json.Unmarshal(v.Mismatches[0], &mm); err != nil {
		t.Fatalf("unmarshal mismatch: %v", err)
	}
	if mm.MustangValid || !mm.LithoValid {
		t.Errorf("mismatch verdicts: got mustang=%v litho=%v, want mustang=false litho=true",
			mm.MustangValid, mm.LithoValid)
	}
	if mm.MustangReport != "missing fx:Version" {
		t.Errorf("mismatch report: got %q, want %q", mm.MustangReport, "missing fx:Version")
	}
}

func TestFacturxFeature_Compare_FalsePositive(t *testing.T) {
	// Mustang says valid, litho says invalid: litho over-flagged.
	dir := t.TempDir()
	writeMustangGolden(t, dir, []mustangJob{{File: "ok.pdf", Valid: true}})
	f := newFacturxFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	output, _ := json.Marshal(facturxOutput{
		Valid:    false,
		Failures: []string{"clause INVOICE test 7: ..."},
	})
	v := f.Compare("ok", output)
	if v.Match {
		t.Errorf("Match: got true, want false")
	}
	if v.Metrics["false_positive"] != 1 {
		t.Errorf("false_positive metric: got %d, want 1", v.Metrics["false_positive"])
	}
}

func TestFacturxFeature_Compare_NoGolden(t *testing.T) {
	f := newFacturxFeature()
	v := f.Compare("orphan", []byte(`{"valid":true}`))
	if !v.Skipped {
		t.Errorf("Skipped: got false, want true")
	}
	if v.Reason != "no golden" {
		t.Errorf("Reason: got %q, want %q", v.Reason, "no golden")
	}
}
