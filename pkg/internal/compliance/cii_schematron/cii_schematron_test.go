package cii_schematron

import (
	"context"
	"testing"
)

func TestLoadDefault_Succeeds(t *testing.T) {
	s, err := LoadDefault(context.Background())
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	if s == nil {
		t.Fatal("LoadDefault returned nil schema")
	}
	if s.QueryBinding != "xslt2" {
		t.Errorf("QueryBinding: got %q, want %q", s.QueryBinding, "xslt2")
	}
	if len(s.PatternOrder) == 0 {
		t.Error("PatternOrder empty; EN 16931 should ship multiple patterns")
	}
	// Every pattern should be indexed by PatternOrder; spot-check
	// one of the EN 16931 canonical pattern ids to catch a regression
	// where the bump lost a known pattern.
	if _, ok := s.Patterns["EN16931-CII-Model"]; !ok {
		t.Errorf("missing canonical pattern EN16931-CII-Model; have %v", s.PatternOrder)
	}
}

func TestLoadDefault_Memoised(t *testing.T) {
	a, err := LoadDefault(context.Background())
	if err != nil {
		t.Fatalf("first LoadDefault: %v", err)
	}
	b, err := LoadDefault(context.Background())
	if err != nil {
		t.Fatalf("second LoadDefault: %v", err)
	}
	if a != b {
		t.Error("LoadDefault returned different pointers; cache is broken")
	}
}

func TestFS_ContainsRoot(t *testing.T) {
	f, err := FS().Open(EN16931Root)
	if err != nil {
		t.Fatalf("opening %s: %v", EN16931Root, err)
	}
	f.Close()
}

func TestLoadXRechnung_Succeeds(t *testing.T) {
	s, err := LoadXRechnung(context.Background())
	if err != nil {
		t.Fatalf("LoadXRechnung: %v", err)
	}
	if s == nil {
		t.Fatal("LoadXRechnung returned nil")
	}
	if len(s.PatternOrder) == 0 {
		t.Error("XRechnung bundle has no patterns")
	}
	// Spot-check one of the canonical XRechnung patterns.
	if _, ok := s.Patterns["cii-pattern"]; !ok {
		t.Errorf("missing cii-pattern; have %v", s.PatternOrder)
	}
}
