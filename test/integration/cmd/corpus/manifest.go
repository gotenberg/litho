package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Manifest describes one corpus: where its PDFs live on disk, which
// conformance profiles to expand them against, and where the goldens
// sit. Every corpus under test/integration/corpus/ ships one
// manifest.json with this exact shape.
type Manifest struct {
	// Name is the corpus identifier used in CLI flags and the baseline: the
	// corpus directory under test/integration/corpus/ ("pdfjs",
	// "libreoffice/docx"). Defaults to that directory when empty.
	Name string `json:"name"`

	// Source is the directory under test/integration/corpus/ that holds
	// the corpus's manifest, with "/" separators. Defaults
	// to that directory when empty.
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
	// grids) live in git beside its manifest. Such a corpus has no goldens
	// step: corpus prepare only fetches it.
	GoldensInSource bool `json:"goldens_in_source,omitempty"`

	// References, when set, names the reference PDFs a converter corpus's
	// diff goldens come from, which corpus references builds into the
	// testdata. See [ReferenceSpec].
	References *ReferenceSpec `json:"references,omitempty"`

	// Fetch, when set, names where `corpus fetch` downloads the corpus's
	// files from on the host. See [FetchSpec].
	Fetch *FetchSpec `json:"fetch,omitempty"`

	// CommittedPDFs, when set, names the directory, relative to the corpus
	// source directory, whose PDFs are the corpus's PDFs: the litho/pdf and
	// litho/text fixtures, which corpus build writes and unit tests read.
	// corpus goldens copies them into the testdata before the image derives
	// the goldens.
	CommittedPDFs string `json:"committed_pdfs,omitempty"`
}

// ReferenceSpec names the reference PDFs of a converter corpus that corpus
// references builds into the testdata with the pinned Gotenberg image: Dir, under the testdata root, holds one
// <name>.pdf per fixture, the fixture ID with "/" written "__", and a
// manifest.json recording what each was built from (corpus references), so a
// reference built from another pin or from a source that changed since is
// refused.
type ReferenceSpec struct {
	// Dir is the reference directory, relative to the testdata root.
	Dir string `json:"dir"`
	// Engine is the Gotenberg route the references come from:
	// [engineLibreOffice] or [engineChromium].
	Engine string `json:"engine"`
	// SourcePin names the versions.env pin the fetched documents come
	// from (LIBREOFFICE_CORE_COMMIT, POI_COMMIT): a reference built from
	// the documents of another value is stale. Empty for a corpus whose
	// sources are committed.
	SourcePin string `json:"source_pin,omitempty"`
}

// InputSpec lists the source documents of a converter corpus: List, Glob,
// Build, or several, whose fixture IDs must then differ.
type InputSpec struct {
	// List names a committed JSON file, relative to the corpus source
	// directory, holding an array of objects with an "id" field: each is one
	// fixture, whose source is the file at that ID's path under the corpus
	// testdata root, or under its ListRoot subdirectory. The wpt corpus lists
	// its reftests this way, the files themselves fetched into testdata.
	List string `json:"list,omitempty"`

	// ListRoot is the subdirectory of the testdata root the List IDs
	// resolve under; empty is the testdata root itself.
	ListRoot string `json:"list_root,omitempty"`

	// Build names a committed directory, relative to the corpus source
	// directory, whose subdirectories are unpacked documents, one per
	// fixture: `corpus build` zips each into <ListRoot>/<name>/<name>.<ext>,
	// the extension the corpus name ends with (litho/docx), beside the
	// hostile documents it generates for that format (see [hostileDocs]).
	// A parts directory's options.json is copied beside its document
	// rather than zipped into it.
	Build string `json:"build,omitempty"`

	// Glob matches the sources, relative to the corpus source directory,
	// with exactly one "*": each match is one fixture, whose ID is the text
	// the "*" matched ("sources/*/index.html" makes "invoice" of
	// "sources/invoice/index.html").
	Glob string `json:"glob,omitempty"`
}

// Profile mode constants. Empty string aliases "expand" for backward
// compatibility with manifests written before the field existed.
// The engines a [ReferenceSpec] names.
const (
	engineLibreOffice = "libreoffice"
	engineChromium    = "chromium"
)

// prepareRemedy is the command that brings a corpus to a scorable state:
// every message about a missing or stale input, reference or golden names it.
func prepareRemedy(name string) string {
	return "make corpus-prepare CORPUS=" + name
}

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

// LoadManifest reads the manifest whose Name field matches name, among the
// corpora [corpusDirs] finds. The source directory and the corpus Name need
// not coincide, though every corpus today names itself by its directory.
func LoadManifest(name string) (*Manifest, error) {
	ms, err := manifests()
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		if m.Name == name {
			return m, nil
		}
	}
	return nil, fmt.Errorf("manifest not found for corpus %q", name)
}

// manifests reads the manifest of every corpus [corpusDirs] finds, in
// directory order. A Name or Source left empty takes the directory, its
// path under test/integration/corpus/ with "/" separators.
func manifests() ([]*Manifest, error) {
	root := corpusRoot()
	if root == "" {
		return nil, errors.New("no corpus directory found")
	}
	dirs, err := corpusDirs(root)
	if err != nil {
		return nil, err
	}
	out := make([]*Manifest, 0, len(dirs))
	for _, dir := range dirs {
		p := filepath.Join(root, filepath.FromSlash(dir), "manifest.json")
		data, err := os.ReadFile(p) //nolint:gosec // a committed manifest under the corpus root
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		var m Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		if m.Name == "" {
			m.Name = dir
		}
		if m.Source == "" {
			m.Source = dir
		}
		out = append(out, &m)
	}
	return out, nil
}

// corpusDirs lists the corpus directories under root, as paths relative to
// it with "/" separators: every directory holding a manifest.json, and every
// such directory one level down in a directory holding none. A source that
// feeds several corpora groups them that way, one per format:
// libreoffice/docx, litho/html.
func corpusDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if hasManifest(filepath.Join(root, e.Name())) {
			dirs = append(dirs, e.Name())
			continue
		}
		subs, err := os.ReadDir(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filepath.Join(root, e.Name()), err)
		}
		for _, sub := range subs {
			if sub.IsDir() && hasManifest(filepath.Join(root, e.Name(), sub.Name())) {
				dirs = append(dirs, e.Name()+"/"+sub.Name())
			}
		}
	}
	return dirs, nil
}

// hasManifest reports whether dir holds a manifest.json file.
func hasManifest(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "manifest.json"))
	return err == nil && info.Mode().IsRegular()
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
// directory. The directory holds the provisioned PDFs and goldens, at the
// source directory's path under test/integration/testdata/.
func (m *Manifest) TestdataRoot() string {
	leaf := m.Source
	if leaf == "" {
		leaf = m.Name
	}
	leaf = filepath.FromSlash(leaf)
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
	return filepath.Join(corpusRoot(), filepath.FromSlash(m.Source))
}

// GoldenOverridesPath returns the path to the committed golden override
// file for a feature, or "" when the manifest declares none.
func (m *Manifest) GoldenOverridesPath(feature string) string {
	rel, ok := m.GoldenOverrides[feature]
	if !ok {
		return ""
	}
	return filepath.Join(corpusRoot(), filepath.FromSlash(m.Source), rel)
}

// ListCorpora returns the Name of every corpus [corpusDirs] finds.
func ListCorpora() ([]string, error) {
	ms, err := manifests()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(ms))
	for _, m := range ms {
		names = append(names, m.Name)
	}
	return names, nil
}

// corpusFlagUsage documents the --corpus flag every subcommand takes.
const corpusFlagUsage = "Corpora, comma-separated: names, globs or sources (litho/docx,*/xlsx,poi); empty = all"

// selectManifests returns the corpora patterns select, in name order, every
// corpus when there is none. A pattern is a corpus name, a path.Match glob
// over the names (poi/*, */docx), or a source, the first element of a name
// (poi). A pattern selecting nothing is an error, so a typo does not pass as
// an empty selection.
func selectManifests(patterns []string) ([]*Manifest, error) {
	all, err := manifests()
	if err != nil {
		return nil, err
	}
	if len(patterns) == 0 {
		return all, nil
	}
	var out []*Manifest
	matched := make(map[string]bool, len(patterns))
	for _, m := range all {
		hit := false
		for _, p := range patterns {
			if corpusMatches(p, m.Name) {
				matched[p], hit = true, true
			}
		}
		if hit {
			out = append(out, m)
		}
	}
	for _, p := range patterns {
		if !matched[p] {
			return nil, fmt.Errorf("no corpus matches %q", p)
		}
	}
	return out, nil
}

// corpusMatches reports whether pattern selects the corpus name.
func corpusMatches(pattern, name string) bool {
	if ok, err := path.Match(pattern, name); err == nil && ok {
		return true
	}
	source, _, _ := strings.Cut(name, "/")
	return pattern == source
}
