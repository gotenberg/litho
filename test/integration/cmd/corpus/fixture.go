package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Fixture is one PDF plus the identifier the goldens are keyed on.
type Fixture struct {
	// ID is the key used to look up goldens. Stable across runs.
	ID string

	// PDFPath is the absolute path to the fixture PDF on disk.
	PDFPath string

	// SkipReason is non-empty when the manifest marks the fixture as
	// skip-worthy (infinite loop, decompression bomb). The runner omits
	// skipped fixtures from every feature.
	SkipReason string
}

// LoadFixtures walks the manifest's flat pdfs/ tree and emits one
// fixture per (pdf, profile) pair. ID is "<profile>/<basename>", which
// is what the compliance golden index expects.
func LoadFixtures(m *Manifest) ([]Fixture, error) {
	if len(m.Profiles) == 0 {
		return nil, errors.New("manifest profiles list is empty")
	}
	pdfRoot := m.TestdataRoot()
	if m.PDFRoot != "" {
		pdfRoot = filepath.Join(m.TestdataRoot(), m.PDFRoot)
	}

	var pdfs []string
	if err := filepath.Walk(pdfRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".pdf") {
			return nil
		}
		pdfs = append(pdfs, path)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk %s: %w", pdfRoot, err)
	}

	fixtures := make([]Fixture, 0, len(pdfs)*len(m.Profiles))
	for _, path := range pdfs {
		base := strings.TrimSuffix(filepath.Base(path), ".pdf")
		for _, profile := range m.Profiles {
			id := profile + "/" + base
			fixtures = append(fixtures, Fixture{
				ID:         id,
				PDFPath:    path,
				SkipReason: m.Skips[id],
			})
		}
	}
	return fixtures, nil
}

// isProfileDir reports whether a directory name matches a VeraPDF
// profile convention (PDF_A-* or PDF_UA-*).
func isProfileDir(name string) bool {
	return strings.HasPrefix(name, "PDF_A-") || strings.HasPrefix(name, "PDF_UA-")
}
