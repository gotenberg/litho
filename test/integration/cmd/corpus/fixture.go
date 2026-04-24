package main

import (
	"encoding/json"
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

// LoadFixtures enumerates the fixtures for the given manifest. The
// enumeration strategy comes from [Manifest.FixtureSource].
func LoadFixtures(m *Manifest) ([]Fixture, error) {
	switch m.FixtureSource {
	case "pdfjs-manifest":
		return loadPDFJSFixtures(m)
	case "walk-profiles":
		return walkProfileFixtures(m)
	case "walk-flat-x-profiles":
		return walkFlatByProfileFixtures(m)
	default:
		return nil, fmt.Errorf("unknown fixture_source %q", m.FixtureSource)
	}
}

type pdfjsManifestEntry struct {
	ID       string `json:"id"`
	File     string `json:"file"`
	MD5      string `json:"md5"`
	Type     string `json:"type"`
	LastPage int    `json:"lastPage"`
}

// loadPDFJSFixtures reads the pdf.js-style test_manifest.json listing
// and yields a Fixture per entry, deduplicated on File.
func loadPDFJSFixtures(m *Manifest) ([]Fixture, error) {
	root := m.TestdataRoot()
	manifestPath := filepath.Join(root, m.FixtureManifest)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read fixture manifest %s: %w", manifestPath, err)
	}
	var entries []pdfjsManifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse fixture manifest: %w", err)
	}
	seen := make(map[string]bool, len(entries))
	fixtures := make([]Fixture, 0, len(entries))
	for _, e := range entries {
		if seen[e.File] {
			continue
		}
		seen[e.File] = true
		reason := m.Skips[e.ID]
		fixtures = append(fixtures, Fixture{
			ID:         e.ID,
			PDFPath:    filepath.Join(root, e.File),
			SkipReason: reason,
		})
	}
	return fixtures, nil
}

// walkProfileFixtures walks the VeraPDF layout: one subdirectory per
// profile, each holding a clause/test tree of .pdf files. The fixture
// ID is the corpus-relative path without extension.
func walkProfileFixtures(m *Manifest) ([]Fixture, error) {
	root := m.TestdataRoot()
	pdfRoot := root
	if m.PDFRoot != "" {
		pdfRoot = filepath.Join(root, m.PDFRoot)
	}

	var fixtures []Fixture
	entries, err := os.ReadDir(pdfRoot)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", pdfRoot, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !isProfileDir(name) {
			continue
		}
		profileRoot := filepath.Join(pdfRoot, name)
		if err := filepath.Walk(profileRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() || !strings.HasSuffix(path, ".pdf") {
				return nil
			}
			rel, err := filepath.Rel(pdfRoot, path)
			if err != nil {
				return nil
			}
			id := strings.TrimSuffix(rel, ".pdf")
			fixtures = append(fixtures, Fixture{
				ID:         id,
				PDFPath:    path,
				SkipReason: m.Skips[id],
			})
			return nil
		}); err != nil {
			return nil, fmt.Errorf("walk %s: %w", profileRoot, err)
		}
	}
	return fixtures, nil
}

// isProfileDir reports whether a directory name matches a VeraPDF
// profile convention (PDF_A-* or PDF_UA-*).
func isProfileDir(name string) bool {
	return strings.HasPrefix(name, "PDF_A-") || strings.HasPrefix(name, "PDF_UA-")
}

// walkFlatByProfileFixtures walks a flat pdfs/ directory and emits one
// fixture per (pdf, profile) pair. ID is "<profile>/<basename>", the
// same shape the compliance feature's golden index already expects.
// Used by real-world corpora (Govdocs, customer PDFs) where the fixture
// tree is not pre-segregated by conformance target.
func walkFlatByProfileFixtures(m *Manifest) ([]Fixture, error) {
	if len(m.Profiles) == 0 {
		return nil, fmt.Errorf("walk-flat-x-profiles requires a non-empty profiles list in manifest")
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
