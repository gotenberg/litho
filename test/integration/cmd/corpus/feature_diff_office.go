package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture"
)

// referenceManifestName is the record corpus references writes beside the
// LibreOffice references it builds.
const referenceManifestName = "manifest.json"

// referenceManifest is <testdata>/<corpus>/<references dir>/manifest.json:
// what each reference was built from, and the sources the route refused.
type referenceManifest struct {
	Format      int                         `json:"format"`
	Image       string                      `json:"image"`
	LibreOffice string                      `json:"libreoffice"`
	References  map[string]referenceRecord  `json:"references"`
	Failed      map[string]referenceFailure `json:"failed"`
}

// referenceRecord is one reference: its source, as the corpus directory or
// the fetched directory names it, the SHA-256 of the source, of its
// options.json ("" without one) and of the PDF, the image that converted it,
// (or [microsoftRenderer]), and the pin the fetched documents come from with
// its value. CoreCommit is
// the LibreOffice core commit of a record written before the builder
// recorded the pin, read as the value of LIBREOFFICE_CORE_COMMIT.
type referenceRecord struct {
	Source         string `json:"source"`
	SourceSHA256   string `json:"source_sha256"`
	OptionsSHA256  string `json:"options_sha256"`
	PDFSHA256      string `json:"pdf_sha256"`
	Image          string `json:"image"`
	SourcePin      string `json:"source_pin,omitempty"`
	SourcePinValue string `json:"source_pin_value,omitempty"`
	CoreCommit     string `json:"libreoffice_core_commit,omitempty"`
	// BuiltAt is the day a Microsoft reference was built (YYYY-MM-DD):
	// Office for the web has no version to pin, so the day stands for it.
	BuiltAt string `json:"built_at,omitempty"`
}

// pinValue returns the value of the pin name the record was built at, ""
// when it records another pin or none.
func (r referenceRecord) pinValue(name string) string {
	switch {
	case r.SourcePin == name:
		return r.SourcePinValue
	case r.SourcePin == "" && name == "LIBREOFFICE_CORE_COMMIT":
		return r.CoreCommit
	}
	return ""
}

// referenceFailure is a source the route refused, with its answer.
type referenceFailure struct {
	Source        string `json:"source"`
	SourceSHA256  string `json:"source_sha256"`
	OptionsSHA256 string `json:"options_sha256"`
	Image         string `json:"image"`
	Status        string `json:"status"`
	Answer        string `json:"answer"`
}

// maxReferenceProblems bounds the problems a refusal lists before it counts
// the rest: the remedy is the same for one stale reference or three hundred.
const maxReferenceProblems = 8

// errStaleReferences reports references that were not built from the
// fixtures' current sources at the pinned image, or goldens that were not
// made from the references on disk.
var errStaleReferences = errors.New("stale references")

// Prepare checks that every fixture has a reference built at the pinned
// GOTENBERG_IMAGE (and, for a fetched corpus, at the pin its manifest's
// references.source_pin names) from its current source and options, and
// that the provisioned PDFs the goldens were made from are those references
// (see [referenceProblems]). Any disagreement fails the cell with the
// command that rebuilds what is stale, so no fixture is scored against a
// reference of something else. A fixture whose converter refused it, and
// would refuse it again, has no golden and is not scored: Prepare names
// it with the converter's answer.
func (f *diffFeature) Prepare(_ context.Context, fixtures []Fixture) error {
	f.mu.RLock()
	m, root, pins := f.m, f.testdataRoot, f.pins
	f.mu.RUnlock()
	if m == nil || m.References == nil {
		return nil
	}
	if pins == nil {
		resolved, err := loadPins()
		if err != nil {
			return err
		}
		pins = resolved.Values
	}
	borrowed, err := borrowedReferences(fixtures)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.borrowed = borrowed
	f.mu.Unlock()
	stale, _, regolden, refused, err := referenceProblems(m, root, fixtures, pins)
	if len(refused) > 0 {
		fmt.Printf("%s: %d fixtures have no reference, not scored: %s\n", m.Name, len(refused), summarize(refused))
	}
	switch {
	case err != nil:
		return fmt.Errorf("%w: %w (run %s)", errStaleReferences, err, prepareRemedy(m.Name))
	case len(stale) > 0:
		return fmt.Errorf("%w: %s (run %s)", errStaleReferences, summarize(stale), prepareRemedy(m.Name))
	case len(regolden) > 0:
		return fmt.Errorf("%w: the diff goldens were not made from the references of %s (run %s)",
			errStaleReferences, summarize(regolden), prepareRemedy(m.Name))
	}
	return nil
}

// referenceProblems compares a corpus's references with its fixtures: stale
// lists what is wrong with the reference of each fixture whose reference is
// missing, refused or built from something else (see [checkReference]),
// rebuild the fixtures among them another build may fix, regolden the
// fixtures whose provisioned PDF, the one the goldens were made from, is not
// their reference, refused the fixtures the converter refused from their
// current source, which a build would refuse again: they have no golden.
// root is the corpus's testdata root. A build corpus's hostile documents
// and a fixture that borrows another's reference (see [borrowedReferences])
// have no reference of their own and are skipped. err reports an unreadable
// reference manifest or a reference_of that names no fixture to borrow from.
func referenceProblems(m *Manifest, root string, fixtures []Fixture, pins map[string]string) (stale, rebuild, regolden, refused []string, err error) {
	refDir := filepath.Join(root, filepath.FromSlash(m.References.Dir))
	data, err := os.ReadFile(filepath.Join(refDir, referenceManifestName)) //nolint:gosec // provisioned record under the corpus testdata root
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%s unreadable: %w", filepath.Join(refDir, referenceManifestName), err)
	}
	var man referenceManifest
	if err := json.Unmarshal(data, &man); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("decode %s: %w", filepath.Join(refDir, referenceManifestName), err)
	}
	unreferenced := unreferencedFixtures(m)
	borrowed, err := borrowedReferences(fixtures)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	for _, fx := range fixtures {
		if _, borrows := borrowed[fx.ID]; unreferenced[fx.ID] || borrows {
			continue
		}
		name := reportName(fx.ID)
		why, again := checkReference(man, m.References, name, fx, pins)
		if why != "" && again {
			stale = append(stale, why)
			rebuild = append(rebuild, name)
			continue
		}
		if why != "" {
			refused = append(refused, why)
			continue
		}
		// The goldens come from the provisioned copy of each reference,
		// pdfs/<name>.pdf: a copy that differs was made from another build.
		got, err := fileSHA256(filepath.Join(root, "pdfs", name+".pdf"))
		if err != nil || got != man.References[name].PDFSHA256 {
			regolden = append(regolden, name)
		}
	}
	return stale, rebuild, regolden, refused, nil
}

// unreferencedFixtures names the fixtures of m no reference is built for: a
// build corpus's hostile documents, which only the robustness feature reads.
func unreferencedFixtures(m *Manifest) map[string]bool {
	format, err := buildFormat(m)
	if err != nil || !isBuildCorpus(m) {
		return nil
	}
	out := map[string]bool{}
	for _, h := range hostileDocs(format) {
		out[h.name] = true
	}
	return out
}

// checkReference returns what is wrong with the reference of one fixture,
// or "" when it was built from the fixture's current source at the pins:
// the image's and, when refs names a source pin, the pin the fetched
// documents come from. rebuild is false for a source the converter refused
// from the same source, options and builder, which another build would
// refuse again: the only case why is set and rebuild false.
func checkReference(man referenceManifest, refs *ReferenceSpec, name string, fx Fixture, pins map[string]string) (why string, rebuild bool) {
	srcSum, srcErr := sourceDigest(refs.Engine, fx.PDFPath)
	optSum := ""
	if info, err := os.Stat(fx.PDFPath); err == nil && info.IsDir() {
		if got, err := fileSHA256(filepath.Join(fx.PDFPath, fixture.OptionsFile)); err == nil {
			optSum = got
		}
	}
	image := referenceBuilder(refs.Engine, pins)
	rec, ok := man.References[name]
	if !ok {
		if fail, refused := man.Failed[name]; refused {
			again := srcErr != nil || fail.Image != image || fail.SourceSHA256 != srcSum || fail.OptionsSHA256 != optSum
			return fmt.Sprintf("%s was refused by %s (HTTP %s: %s)", name, fail.Image, fail.Status, refusalCause(fail.Answer)), again
		}
		return name + " has no reference", true
	}
	if rec.Image != image {
		return fmt.Sprintf("%s was built with %s, the corpus wants %s", name, rec.Image, image), true
	}
	if sourcePin := refs.SourcePin; sourcePin != "" {
		if got, want := rec.pinValue(sourcePin), pins[sourcePin]; got != want {
			return fmt.Sprintf("%s was built from the documents of %s %q, the pin is %q", name, sourcePin, got, want), true
		}
	}
	if srcErr != nil || srcSum != rec.SourceSHA256 {
		return name + "'s source changed since its reference was built", true
	}
	if optSum != rec.OptionsSHA256 {
		return name + "'s options changed since its reference was built", true
	}
	return "", false
}

// refusalCause shortens a converter's answer to its error code when it
// carries one (Graph's ErrorCode=WordCorruptFile, InputFormatNotSupported),
// and to its first line otherwise.
func refusalCause(answer string) string {
	for _, key := range []string{"ErrorCode=", `"InnerError":{"Code":"`} {
		if _, rest, ok := strings.Cut(answer, key); ok {
			if end := strings.IndexFunc(rest, func(r rune) bool { return r == ' ' || r == '"' || r == ',' }); end > 0 {
				return rest[:end]
			}
		}
	}
	first, _, _ := strings.Cut(answer, "\n")
	return first
}

// summarize joins the first problems and counts the rest.
func summarize(problems []string) string {
	if len(problems) <= maxReferenceProblems {
		return strings.Join(problems, "; ")
	}
	return fmt.Sprintf("%s; and %d more", strings.Join(problems[:maxReferenceProblems], "; "), len(problems)-maxReferenceProblems)
}

// fileSHA256 returns the hex SHA-256 of a file.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path) //nolint:gosec // corpus source or provisioned reference
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// errBorrowedReference reports an options.json whose reference_of names no
// fixture of its corpus that has a reference of its own.
var errBorrowedReference = errors.New("invalid reference_of")

// borrowedReferences maps each fixture of an Office corpus whose
// options.json names reference_of to the fixture whose reference stands for
// its own (see fixture.OfficeOptions.ReferenceOf). It fails when one names
// itself, a fixture the corpus does not list, or a fixture that borrows in
// turn. A source that does not load borrows nothing.
func borrowedReferences(fixtures []Fixture) (map[string]string, error) {
	ids := make(map[string]bool, len(fixtures))
	for _, fx := range fixtures {
		ids[fx.ID] = true
	}
	out := map[string]string{}
	for _, fx := range fixtures {
		info, err := os.Stat(fx.PDFPath)
		if err != nil || !info.IsDir() {
			continue
		}
		src, err := fixture.LoadOffice(fx.PDFPath)
		if err != nil || src.Options.ReferenceOf == "" {
			continue
		}
		other := src.Options.ReferenceOf
		if other == fx.ID || !ids[other] {
			return nil, fmt.Errorf("%w: %s names %q, not another fixture of its corpus", errBorrowedReference, fx.ID, other)
		}
		out[fx.ID] = other
	}
	for id, other := range out {
		if _, chained := out[other]; chained {
			return nil, fmt.Errorf("%w: %s names %s, which borrows a reference too", errBorrowedReference, id, other)
		}
	}
	return out, nil
}
