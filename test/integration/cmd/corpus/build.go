package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gotenberg/litho/test/integration/fixture"
)

// buildFormats are the formats a build corpus may be in (see
// [InputSpec.Build]): the last element of its name.
var buildFormats = map[string]bool{"docx": true, "xlsx": true, "pptx": true} //nolint:gochecknoglobals // static table

// contentTypesPart is the part an OPC package names its content types in
// (ECMA-376 Part 2, 10.1.2.2), zipped first as Office writes it.
const contentTypesPart = "[Content_Types].xml"

// zipEpoch is the modification time every built entry carries, so a rebuild
// writes the same bytes: the earliest a zip DOS time holds.
var zipEpoch = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a constant time

// runBuild builds the documents of the build corpora into their testdata.
func runBuild(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	corpus := fs.String("corpus", "", "Comma-separated build corpora (empty = every corpus with inputs.build)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}
	corpora, err := buildCorpora(splitCSV(*corpus))
	if err != nil {
		fmt.Fprintf(os.Stderr, "build: %v\n", err)
		return 1
	}
	for _, m := range corpora {
		if err := buildCorpus(ctx, m); err != nil {
			fmt.Fprintf(os.Stderr, "build: %s: %v\n", m.Name, err)
			return 1
		}
	}
	return 0
}

// buildCorpora loads the named corpora, or every build corpus when none is
// named, refusing a named one that builds nothing.
func buildCorpora(names []string) ([]*Manifest, error) {
	if len(names) == 0 {
		all, err := manifests()
		if err != nil {
			return nil, err
		}
		var out []*Manifest
		for _, m := range all {
			if isBuildCorpus(m) {
				out = append(out, m)
			}
		}
		return out, nil
	}
	out := make([]*Manifest, 0, len(names))
	for _, name := range names {
		m, err := LoadManifest(name)
		if err != nil {
			return nil, err
		}
		if !isBuildCorpus(m) {
			return nil, fmt.Errorf("%s builds no documents", name)
		}
		out = append(out, m)
	}
	return out, nil
}

// isBuildCorpus reports whether m's documents are built by `corpus build`.
func isBuildCorpus(m *Manifest) bool {
	return m.Inputs != nil && m.Inputs.Build != ""
}

// buildFormat returns the format of a build corpus's documents.
func buildFormat(m *Manifest) (string, error) {
	format := path.Base(m.Name)
	if !buildFormats[format] {
		return "", fmt.Errorf("%s: a build corpus is named after its format (docx, xlsx, pptx), not %q", m.Name, format)
	}
	return format, nil
}

// buildOut returns the directory a build corpus's documents are built into.
func buildOut(m *Manifest) string {
	return filepath.Join(m.TestdataRoot(), filepath.FromSlash(m.Inputs.ListRoot))
}

// partsNames returns the names of the unpacked documents of a build corpus,
// sorted.
func partsNames(m *Manifest) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(m.SourceDir(), filepath.FromSlash(m.Inputs.Build)))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && validInputID(e.Name()) && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// builtNames returns every fixture name of a build corpus: its unpacked
// documents, then its hostile ones.
func builtNames(m *Manifest) ([]string, error) {
	format, err := buildFormat(m)
	if err != nil {
		return nil, err
	}
	names, err := partsNames(m)
	if err != nil {
		return nil, err
	}
	for _, h := range hostileDocs(format) {
		names = append(names, h.name)
	}
	return names, nil
}

// builtInputFixtures lists the fixtures of a build corpus, each the
// directory its document was built into, refusing one not built yet.
func builtInputFixtures(m *Manifest) ([]Fixture, error) {
	format, err := buildFormat(m)
	if err != nil {
		return nil, err
	}
	names, err := builtNames(m)
	if err != nil {
		return nil, err
	}
	out := buildOut(m)
	fixtures := make([]Fixture, 0, len(names))
	for _, name := range names {
		dir := filepath.Join(out, name)
		if _, err := os.Stat(filepath.Join(dir, name+"."+format)); err != nil {
			return nil, fmt.Errorf("%w: %s is missing (run make corpus-build CORPUS_NAME=%s)", errInputsUnfetched, dir, m.Name)
		}
		fixtures = append(fixtures, Fixture{ID: name, PDFPath: dir})
	}
	return fixtures, nil
}

// buildCorpus zips each unpacked document of m and writes its hostile
// documents, each into <out>/<name>/<name>.<format>, replacing what an
// earlier build wrote there.
func buildCorpus(ctx context.Context, m *Manifest) error {
	format, err := buildFormat(m)
	if err != nil {
		return err
	}
	names, err := partsNames(m)
	if err != nil {
		return err
	}
	out := buildOut(m)
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	partsDir := filepath.Join(m.SourceDir(), filepath.FromSlash(m.Inputs.Build))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		src := filepath.Join(partsDir, name)
		data, err := zipParts(src)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := writeBuilt(out, name, format, data); err != nil {
			return err
		}
		options, err := os.ReadFile(filepath.Join(src, fixture.OptionsFile)) //nolint:gosec // a committed corpus source
		switch {
		case err == nil:
			if err := os.WriteFile(filepath.Join(out, name, fixture.OptionsFile), options, 0o644); err != nil { //nolint:gosec // testdata is world-readable
				return err
			}
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
	}
	base, err := readParts(filepath.Join(partsDir, hostileBase))
	if err != nil {
		return fmt.Errorf("the hostile documents start from %s: %w", hostileBase, err)
	}
	hostile := hostileDocs(format)
	for _, h := range hostile {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := h.build(base)
		if err != nil {
			return fmt.Errorf("%s: %w", h.name, err)
		}
		if err := writeBuilt(out, h.name, format, data); err != nil {
			return err
		}
	}
	fmt.Printf("%s: %d documents and %d hostile ones built into %s\n", m.Name, len(names), len(hostile), out)
	return nil
}

// writeBuilt writes a built document to <out>/<name>/<name>.<format>.
func writeBuilt(out, name, format string, data []byte) error {
	dir := filepath.Join(out, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name+"."+format), data, 0o644) //nolint:gosec // testdata is world-readable
}

// readParts reads an unpacked document: every file under dir by its slash
// path, but the options.json at its top and what [junkFile] names.
func readParts(dir string) (map[string][]byte, error) {
	parts := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir && junkFile(d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == fixture.OptionsFile {
			return nil
		}
		data, err := os.ReadFile(p) //nolint:gosec // a committed corpus source
		if err != nil {
			return err
		}
		parts[rel] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("no part under %s", dir)
	}
	return parts, nil
}

// zipParts zips the unpacked document under dir (see [zipPackage]).
func zipParts(dir string) ([]byte, error) {
	parts, err := readParts(dir)
	if err != nil {
		return nil, err
	}
	if _, ok := parts[contentTypesPart]; !ok {
		return nil, fmt.Errorf("%s has no %s", dir, contentTypesPart)
	}
	return zipPackage(parts)
}

// zipPackage zips parts deflated, the content types first and the rest by
// name, every entry at [zipEpoch]: the same parts always zip to the same
// bytes.
func zipPackage(parts map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range packageOrder(parts) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: zipEpoch})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(parts[name]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// packageOrder returns the part names in the order a package is zipped:
// the content types first, then by name.
func packageOrder(parts map[string][]byte) []string {
	names := make([]string, 0, len(parts))
	for n := range parts {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := names[i] == contentTypesPart, names[j] == contentTypesPart
		if a != b {
			return a
		}
		return names[i] < names[j]
	})
	return names
}

// cloneParts returns a copy of parts whose map, not whose byte slices, the
// caller may change.
func cloneParts(parts map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(parts))
	for k, v := range parts {
		out[k] = slices.Clone(v)
	}
	return out
}

// junkFile reports whether d is no part of an unpacked document but what a
// file manager leaves beside one: a hidden directory, macOS's .DS_Store and
// AppleDouble files. A part may start with a dot: _rels/.rels does.
func junkFile(d fs.DirEntry) bool {
	n := d.Name()
	return (d.IsDir() && strings.HasPrefix(n, ".")) || n == ".DS_Store" || strings.HasPrefix(n, "._")
}
