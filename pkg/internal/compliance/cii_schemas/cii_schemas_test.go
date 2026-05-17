package cii_schemas

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
	if s.TargetNamespace != CIINamespace {
		t.Errorf("namespace: got %q, want %q", s.TargetNamespace, CIINamespace)
	}
}

func TestLoadDefault_Memoised(t *testing.T) {
	// Two calls in a row should return pointer-equal schemas: the
	// sync.Once gate guarantees one load per process, and the cache
	// makes the second call free.
	a, err := LoadDefault(context.Background())
	if err != nil {
		t.Fatalf("first LoadDefault: %v", err)
	}
	b, err := LoadDefault(context.Background())
	if err != nil {
		t.Fatalf("second LoadDefault: %v", err)
	}
	if a != b {
		t.Error("LoadDefault returned different pointers across calls; cache is broken")
	}
}

func TestFS_ContainsRootSchema(t *testing.T) {
	f, err := FS().Open(D16BRoot)
	if err != nil {
		t.Fatalf("opening %s: %v", D16BRoot, err)
	}
	f.Close()
}
