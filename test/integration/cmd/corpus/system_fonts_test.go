package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSystemFontsSources_Load checks that every source the CI job converts
// inside the Gotenberg image reads as the diff cell reads a source.
func TestSystemFontsSources_Load(t *testing.T) {
	t.Parallel()
	dirs, err := filepath.Glob(filepath.Join("testdata", "system-fonts", "*"))
	if err != nil || len(dirs) < 10 {
		t.Fatalf("%d sources, %v", len(dirs), err)
	}
	for _, dir := range dirs {
		office := !isFile(filepath.Join(dir, "index.html"))
		data, _, err := sourceSettings(dir, office, nil)
		if err != nil || len(data) == 0 {
			t.Errorf("%s: %d bytes, %v", dir, len(data), err)
		}
	}
	if _, err := os.Stat(filepath.Join("testdata", "system-fonts", "scripts-odt", "scripts.fodt")); err != nil {
		t.Error("no Office source")
	}
}
