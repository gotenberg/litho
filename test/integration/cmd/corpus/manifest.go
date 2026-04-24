package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Manifest describes one corpus: where its PDFs live on disk, which
// conformance profiles to expand them against, and where the goldens
// sit. Every corpus under test/integration/corpus/ ships one
// manifest.json with this exact shape.
type Manifest struct {
	// Name is the corpus identifier used in CLI flags ("pdfjs", "verapdf",
	// "govdocs").
	Name string `json:"name"`

	// Source is the directory under test/integration/corpus/ that holds
	// the corpus's download.sh. Defaults to Name when empty.
	Source string `json:"source,omitempty"`

	// PDFRoot is the subdirectory of the corpus testdata root that holds
	// the flat pdfs/ tree. Empty means the testdata root itself.
	PDFRoot string `json:"pdf_root,omitempty"`

	// Profiles lists the VeraPDF profile directory names to expand each
	// PDF against. Every PDF yields len(Profiles) fixtures with IDs
	// "<profile>/<basename>".
	Profiles []string `json:"profiles"`

	// Goldens maps feature name to the directory (relative to the corpus
	// testdata root) that holds the golden files. Features absent from
	// this map render as "[no golden]" in the scoreboard.
	Goldens map[string]string `json:"goldens,omitempty"`

	// Skips maps a fixture ID to a reason explaining why the runner must
	// skip it (infinite loop, decompression bomb). Skipped fixtures do
	// not reach any feature.
	Skips map[string]string `json:"skips,omitempty"`
}

// LoadManifest reads the manifest whose Name field matches name. It
// walks every corpus directory under test/integration/corpus/ and
// returns the first match. The source directory name and the corpus
// Name need not coincide (e.g. directory "pdf.js" hosts corpus "pdfjs").
func LoadManifest(name string) (*Manifest, error) {
	root := corpusRoot()
	if root == "" {
		return nil, errors.New("no corpus directory found")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name(), "manifest.json")
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var m Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		if m.Name == "" {
			m.Name = e.Name()
		}
		if m.Source == "" {
			m.Source = e.Name()
		}
		if m.Name == name {
			return &m, nil
		}
	}
	return nil, fmt.Errorf("manifest not found for corpus %q", name)
}

func corpusRoot() string {
	candidates := []string{
		filepath.Join("test", "integration", "corpus"),
		"corpus",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return ""
}

// TestdataRoot returns the absolute path to the corpus testdata
// directory. The directory holds the provisioned PDFs and goldens and
// sits next to the source directory (same leaf name).
func (m *Manifest) TestdataRoot() string {
	leaf := m.Source
	if leaf == "" {
		leaf = m.Name
	}
	candidates := []string{
		filepath.Join("test", "integration", "testdata", leaf),
		filepath.Join("testdata", leaf),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return filepath.Join("test", "integration", "testdata", leaf)
}

// GoldenDir returns the absolute path to the golden directory for a
// feature, or "" when the manifest does not declare a golden.
func (m *Manifest) GoldenDir(feature string) string {
	rel, ok := m.Goldens[feature]
	if !ok {
		return ""
	}
	return filepath.Join(m.TestdataRoot(), rel)
}

// ListCorpora returns the Name of every corpus whose source directory
// under test/integration/corpus/ ships a manifest.json.
func ListCorpora() ([]string, error) {
	root := corpusRoot()
	if root == "" {
		return nil, errors.New("no corpus directory found")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, e.Name(), "manifest.json"))
		if err != nil {
			continue
		}
		var m Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		name := m.Name
		if name == "" {
			name = e.Name()
		}
		names = append(names, name)
	}
	return names, nil
}
