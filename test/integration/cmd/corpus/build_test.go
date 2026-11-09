package main

import (
	"archive/zip"
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestZipPackageIsDeterministic pins what makes a rebuild write the same
// bytes: the content types first, the rest by name, every entry at the zip
// epoch.
func TestZipPackageIsDeterministic(t *testing.T) {
	t.Parallel()

	parts := map[string][]byte{"word/document.xml": []byte("<d/>"), "_rels/.rels": []byte("<r/>"), contentTypesPart: []byte("<t/>")}
	a, err := zipPackage(parts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := zipPackage(parts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two zips of the same parts differ")
	}
	zr, err := zip.NewReader(bytes.NewReader(a), int64(len(a)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if !f.Modified.Equal(zipEpoch) {
			t.Errorf("%s modified %v, want %v", f.Name, f.Modified, zipEpoch)
		}
	}
	if want := []string{contentTypesPart, "_rels/.rels", "word/document.xml"}; !slices.Equal(names, want) {
		t.Errorf("entries %v, want %v", names, want)
	}
}

// TestReadPartsKeepsRelsAndSkipsJunk pins that a part starting with a dot is
// zipped while what a file manager leaves beside a document is not.
func TestReadPartsKeepsRelsAndSkipsJunk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, p := range []string{contentTypesPart, "_rels/.rels", "word/document.xml", "options.json", ".DS_Store", "word/._document.xml", ".git/config"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	parts, err := readParts(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := slices.Sorted(maps.Keys(parts))
	if want := []string{contentTypesPart, "_rels/.rels", "word/document.xml"}; !slices.Equal(got, want) {
		t.Errorf("parts %v, want %v", got, want)
	}
}
