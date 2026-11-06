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
	// "govdocs", "pdf20").
	Name string `json:"name"`

	// Source is the directory under test/integration/corpus/ that holds
	// the corpus's download.sh. Defaults to Name when empty.
	Source string `json:"source,omitempty"`

	// PDFRoot is the subdirectory of the corpus testdata root that holds
	// the flat pdfs/ tree. Empty means the testdata root itself.
	PDFRoot string `json:"pdf_root,omitempty"`

	// Profiles lists the VeraPDF profile directory names. Their meaning
	// depends on ProfileMode: "expand" cartesians every PDF over the list,
	// "auto" uses the list as the allow-set for XMP-claimed profiles, and
	// "none" ignores it entirely.
	Profiles []string `json:"profiles,omitempty"`

	// ProfileMode controls how PDFs are expanded into fixtures.
	//
	//   "expand" (default): every PDF yields len(Profiles) fixtures with
	//   IDs "<profile>/<basename>". Used by the VeraPDF corpus, where the
	//   reference batch report drives compliance for every profile.
	//
	//   "none": one fixture per PDF, ID = basename. Used by corpora whose
	//   features are profile-agnostic (PDF.js text/metadata oracle).
	//
	//   "auto": one fixture per PDF, profile read from the PDF's XMP
	//   pdfaid:part/conformance and pdfuaid:part. PDFs that claim a
	//   profile get ID "<profile>/<basename>"; PDFs without a claim get
	//   ID = basename and skip profile-keyed features (compliance,
	//   remediation). Used by govdocs and pdf20.
	ProfileMode string `json:"profile_mode,omitempty"`

	// Goldens maps feature name to the directory (relative to the corpus
	// testdata root) that holds the golden files. Features absent from
	// this map render as "[no golden]" in the scoreboard.
	Goldens map[string]string `json:"goldens,omitempty"`

	// GoldenOverrides maps feature name to a committed file, relative to
	// the corpus source directory, that corrects goldens the oracle reads
	// wrong. Each entry names the reading it replaces and why, so a
	// divergence from the oracle stays auditable in git while the goldens
	// themselves stay the oracle's raw output. Only a feature that
	// implements [GoldenOverrider] accepts one.
	GoldenOverrides map[string]string `json:"golden_overrides,omitempty"`

	// Skips maps a fixture ID to a reason explaining why the runner must
	// skip it (infinite loop, decompression bomb). Skipped fixtures do
	// not reach any feature.
	Skips map[string]string `json:"skips,omitempty"`

	// Inputs, when set, makes the corpus a converter corpus: its fixtures
	// are the source documents a converter feature turns into PDFs, not
	// PDFs, and PDFRoot and ProfileMode do not apply. See [InputSpec].
	Inputs *InputSpec `json:"inputs,omitempty"`

	// GoldensInSource resolves the Goldens paths against the corpus source
	// directory rather than its testdata root: the references of a corpus
	// whose expectations are committed (a reftest list, extracted pixel
	// grids) live in git beside its manifest.
	GoldensInSource bool `json:"goldens_in_source,omitempty"`

	// ProvisionedBy names the make target that provisions the corpus on the
	// host, for a corpus the Docker image has no part in. corpus init skips
	// such a corpus and says which target to run instead.
	ProvisionedBy string `json:"provisioned_by,omitempty"`
}

// InputSpec lists the source documents of a converter corpus. Exactly one of
// List and Glob is set.
type InputSpec struct {
	// List names a committed JSON file, relative to the corpus source
	// directory, holding an array of objects with an "id" field: each is one
	// fixture, whose source is the file at that ID's path under the corpus
	// testdata root. The wpt corpus lists its reftests this way, the files
	// themselves fetched into testdata.
	List string `json:"list,omitempty"`

	// Glob matches the sources, relative to the corpus source directory,
	// with exactly one "*": each match is one fixture, whose ID is the text
	// the "*" matched ("sources/*/index.html" makes "invoice" of
	// "sources/invoice/index.html").
	Glob string `json:"glob,omitempty"`
}

// Profile mode constants. Empty string aliases "expand" for backward
// compatibility with manifests written before the field existed.
const (
	ProfileModeExpand = "expand"
	ProfileModeNone   = "none"
	ProfileModeAuto   = "auto"
)

// EffectiveProfileMode returns ProfileMode with empty defaulted to
// "expand".
func (m *Manifest) EffectiveProfileMode() string {
	if m.ProfileMode == "" {
		return ProfileModeExpand
	}
	return m.ProfileMode
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
// feature, or "" when the manifest does not declare a golden. The path is
// under the testdata root, or under the corpus source directory when
// [Manifest.GoldensInSource] is set.
func (m *Manifest) GoldenDir(feature string) string {
	rel, ok := m.Goldens[feature]
	if !ok {
		return ""
	}
	if m.GoldensInSource {
		return filepath.Join(m.SourceDir(), rel)
	}
	return filepath.Join(m.TestdataRoot(), rel)
}

// SourceDir returns the corpus source directory under
// test/integration/corpus/, the one holding its manifest.
func (m *Manifest) SourceDir() string {
	return filepath.Join(corpusRoot(), m.Source)
}

// GoldenOverridesPath returns the path to the committed golden override
// file for a feature, or "" when the manifest declares none.
func (m *Manifest) GoldenOverridesPath(feature string) string {
	rel, ok := m.GoldenOverrides[feature]
	if !ok {
		return ""
	}
	return filepath.Join(corpusRoot(), m.Source, rel)
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
