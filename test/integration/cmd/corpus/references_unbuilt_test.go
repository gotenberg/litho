package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestWithoutUnbuiltMicrosoftReferences checks that a Microsoft corpus
// leaves out the fixtures whose reference was never built, and those that
// borrow one, while a refused or built one stays, and so does one that sets
// litho's own options, which needs no sign-in, and that another engine
// keeps every fixture.
func TestWithoutUnbuiltMicrosoftReferences(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var fixtures []Fixture
	for _, id := range []string{"built", "refused", "new", "borrower", "marked"} {
		dir := filepath.Join(root, "sources", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".docx"), []byte("PK"), 0o600); err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, Fixture{ID: id, PDFPath: dir})
	}
	if err := os.WriteFile(filepath.Join(root, "sources", "borrower", "options.json"), []byte(`{"landscape": true, "reference_of": "new"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sources", "marked", "options.json"), []byte(`{"trackedChanges": "marked"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	refs := filepath.Join(root, "references")
	if err := os.MkdirAll(refs, 0o755); err != nil {
		t.Fatal(err)
	}
	man := `{"format": 1, "references": {"built": {"image": "microsoft-graph"}}, "failed": {"refused": {"image": "microsoft-graph", "status": "400"}}}`
	if err := os.WriteFile(filepath.Join(refs, referenceManifestName), []byte(man), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{Name: "probe/docx", References: &ReferenceSpec{Dir: "references", Engine: engineMicrosoft}}
	kept, unbuilt := withoutUnbuiltMicrosoftReferences(m, root, fixtures)
	var keptIDs []string
	for _, fx := range kept {
		keptIDs = append(keptIDs, fx.ID)
	}
	if want := []string{"built", "refused", "marked"}; !slices.Equal(keptIDs, want) {
		t.Errorf("kept %v, want %v", keptIDs, want)
	}
	if want := []string{"new", "borrower"}; !slices.Equal(unbuilt, want) {
		t.Errorf("unbuilt %v, want %v", unbuilt, want)
	}
	m.References.Engine = engineLibreOffice
	if kept, unbuilt := withoutUnbuiltMicrosoftReferences(m, root, fixtures); len(kept) != len(fixtures) || len(unbuilt) != 0 {
		t.Errorf("LibreOffice corpus: kept %d, unbuilt %v; want every fixture kept", len(kept), unbuilt)
	}
}
