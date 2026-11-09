package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestDropStaleGoldens_KeepsWhatTheReferencesDidNotMake proves a reference
// rebuilt since the goldens were made drops the provisioned PDFs and the diff
// goldens, and nothing else: litho/docx's robustness golden path is its built
// documents, which an earlier version deleted with the goldens.
func TestDropStaleGoldens_KeepsWhatTheReferencesDidNotMake(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	sha := func(s string) string {
		h := sha256.Sum256([]byte(s))
		return hex.EncodeToString(h[:])
	}
	// The reference is current, its provisioned copy is not.
	man, err := json.Marshal(referenceManifest{Format: 1, Image: "img:1", References: map[string]referenceRecord{
		"memo": {SourceSHA256: sha("docx"), PDFSHA256: sha("new reference"), Image: "img:1"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, root, map[string]string{
		"test/integration/corpus/x/docx/sources/memo/memo.docx":     "docx",
		"test/integration/testdata/x/docx/references/manifest.json": string(man),
		"test/integration/testdata/x/docx/pdfs/memo.pdf":            "old reference",
		"test/integration/testdata/x/docx/goldens/diff/render/a":    "golden",
		"test/integration/testdata/x/docx/built/memo/memo.docx":     "built",
	})
	m := &Manifest{
		Name: "x/docx", Source: "x/docx", PDFRoot: "pdfs",
		Inputs:     &InputSpec{Glob: "sources/*"},
		References: &ReferenceSpec{Dir: "references", Engine: engineLibreOffice},
		Goldens:    map[string]string{"diff": "goldens/diff", "robustness": "built"},
	}
	if err := dropStaleGoldens(m, &resolvedPins{Values: map[string]string{"GOTENBERG_IMAGE": "img:1"}}); err != nil {
		t.Fatal(err)
	}
	td := filepath.Join(root, "test", "integration", "testdata", "x", "docx")
	for _, gone := range []string{"pdfs", "goldens/diff"} {
		if _, err := os.Stat(filepath.Join(td, gone)); err == nil {
			t.Errorf("%s survived a stale reference", gone)
		}
	}
	for _, kept := range []string{"built/memo/memo.docx", "references/manifest.json"} {
		if _, err := os.Stat(filepath.Join(td, kept)); err != nil {
			t.Errorf("%s was removed: %v", kept, err)
		}
	}
}

// TestDropStaleGoldens_RederivesAChangedCommittedPDF proves a committed PDF
// rebuilt since its corpus was provisioned drops the provisioned PDFs and
// every golden, so the goldens step derives them from the new bytes, and an
// unchanged corpus keeps them.
func TestDropStaleGoldens_RederivesAChangedCommittedPDF(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFiles(t, root, map[string]string{
		"test/integration/corpus/x/pdf/pdfs/a.pdf":                  "%PDF-1.7 a",
		"test/integration/testdata/x/pdf/pdfs/a.pdf":                "%PDF-1.7 a",
		"test/integration/testdata/x/pdf/goldens/compliance/a.json": "{}",
		"test/integration/testdata/x/pdf/goldens/text/a.json":       "{}",
	})
	m := &Manifest{
		Name: "x/pdf", Source: "x/pdf", PDFRoot: "pdfs", CommittedPDFs: "pdfs",
		Goldens: map[string]string{"compliance": "goldens/compliance", "text": "goldens/text"},
	}
	pins := &resolvedPins{Values: map[string]string{}}
	td := filepath.Join(root, "test", "integration", "testdata", "x", "pdf")
	if err := dropStaleGoldens(m, pins); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(td, "goldens", "text", "a.json")); err != nil {
		t.Fatalf("an unchanged corpus lost its goldens: %v", err)
	}
	writeFiles(t, root, map[string]string{"test/integration/corpus/x/pdf/pdfs/a.pdf": "%PDF-1.7 a, rebuilt"})
	if err := dropStaleGoldens(m, pins); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"pdfs", "goldens/compliance", "goldens/text"} {
		if _, err := os.Stat(filepath.Join(td, gone)); err == nil {
			t.Errorf("%s survived a rebuilt committed PDF", gone)
		}
	}
}
