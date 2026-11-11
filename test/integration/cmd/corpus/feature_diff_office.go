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
	"slices"
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

// referenceFailure is a source the route refused, with its answer, or one
// no reference was asked for because it sets litho's own options, which
// LithoOptions names (see [referencePlan]): its status is "0" and its
// answer says why.
type referenceFailure struct {
	Source        string   `json:"source"`
	SourceSHA256  string   `json:"source_sha256"`
	OptionsSHA256 string   `json:"options_sha256"`
	Image         string   `json:"image"`
	Status        string   `json:"status"`
	Answer        string   `json:"answer"`
	LithoOptions  []string `json:"litho_options,omitempty"`
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
	plan, err := planReferences(fixtures)
	if err != nil {
		return err
	}
	// Notices, not the report: stdout carries the report alone.
	if notice := plan.lithoOnlyNotice(m.Name, fixtures); notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}
	fixtures, unbuilt := withoutUnbuiltMicrosoftReferences(m, root, fixtures)
	if len(unbuilt) > 0 {
		fmt.Fprintln(os.Stderr, unbuiltMicrosoftNotice(m, unbuilt))
	}
	f.mu.Lock()
	f.borrowed, f.lithoOnly = plan.borrowed, plan.lithoOnly
	f.mu.Unlock()
	check, err := referenceProblems(m, root, fixtures, pins)
	if len(check.refused) > 0 {
		fmt.Fprintf(os.Stderr, "%s: %d fixtures have no reference, not scored: %s\n", m.Name, len(check.refused), summarize(check.refused))
	}
	switch {
	case err != nil:
		return fmt.Errorf("%w: %w (run %s)", errStaleReferences, err, prepareRemedy(m.Name))
	case len(check.stale) > 0:
		return fmt.Errorf("%w: %s (run %s)", errStaleReferences, summarize(check.stale), prepareRemedy(m.Name))
	case len(check.regolden) > 0:
		return fmt.Errorf("%w: the diff goldens were not made from the references of %s (run %s)",
			errStaleReferences, summarize(check.regolden), prepareRemedy(m.Name))
	}
	return nil
}

// referenceCheck is what [referenceProblems] finds wrong with a corpus's
// references.
type referenceCheck struct {
	// stale lists what is wrong with the reference of each fixture whose
	// reference is missing, refused or built from something else (see
	// [checkReference]).
	stale []string
	// rebuild names the fixtures another build may fix: those of stale,
	// and those that set litho's own options whose record in the reference
	// manifest does not say so for their current source (see
	// [referencePlan]), which a build records without asking any engine.
	rebuild []string
	// regolden names the fixtures whose provisioned PDF, the one the
	// goldens were made from, is not their reference.
	regolden []string
	// refused lists the fixtures the converter refused from their current
	// source, which a build would refuse again: they have no golden.
	refused []string
}

// referenceProblems compares a corpus's references with its fixtures (see
// [referenceCheck]). root is the corpus's testdata root. A build corpus's
// hostile documents and a fixture that borrows another's reference have no
// reference of their own and are skipped, and so is a fixture that sets
// litho's own options, but for its record (see [referencePlan]). err
// reports an unreadable reference manifest or an invalid reference_of.
func referenceProblems(m *Manifest, root string, fixtures []Fixture, pins map[string]string) (referenceCheck, error) {
	var check referenceCheck
	refDir := filepath.Join(root, filepath.FromSlash(m.References.Dir))
	data, err := os.ReadFile(filepath.Join(refDir, referenceManifestName)) //nolint:gosec // provisioned record under the corpus testdata root
	if err != nil {
		return check, fmt.Errorf("%s unreadable: %w", filepath.Join(refDir, referenceManifestName), err)
	}
	var man referenceManifest
	if err := json.Unmarshal(data, &man); err != nil {
		return check, fmt.Errorf("decode %s: %w", filepath.Join(refDir, referenceManifestName), err)
	}
	hostile := unreferencedFixtures(m)
	plan, err := planReferences(fixtures)
	if err != nil {
		return check, err
	}
	for _, fx := range fixtures {
		name := reportName(fx.ID)
		if keys, ok := plan.lithoOnly[fx.ID]; ok {
			if !recordsLithoOptions(man, m.References.Engine, name, fx, keys) {
				check.rebuild = append(check.rebuild, name)
			}
			continue
		}
		if hostile[fx.ID] || plan.ownsNoReference(fx.ID) {
			continue
		}
		why, again := checkReference(man, m.References, name, fx, pins)
		if why != "" && again {
			check.stale = append(check.stale, why)
			check.rebuild = append(check.rebuild, name)
			continue
		}
		if why != "" {
			check.refused = append(check.refused, why)
			continue
		}
		// The goldens come from the provisioned copy of each reference,
		// pdfs/<name>.pdf: a copy that differs was made from another build.
		got, err := fileSHA256(filepath.Join(root, "pdfs", name+".pdf"))
		if err != nil || got != man.References[name].PDFSHA256 {
			check.regolden = append(check.regolden, name)
		}
	}
	return check, nil
}

// recordsLithoOptions reports whether the reference manifest records the
// fixture name, which sets the litho-only keys, as built from its current
// source and options without a reference: no reference, and a failure that
// names those keys.
func recordsLithoOptions(man referenceManifest, engine, name string, fx Fixture, keys []string) bool {
	fail, ok := man.Failed[name]
	if hasKey(man.References, name) || !ok || !slices.Equal(fail.LithoOptions, keys) {
		return false
	}
	srcSum, optSum, srcErr := sourceSums(engine, fx.PDFPath)
	return srcErr == nil && fail.SourceSHA256 == srcSum && fail.OptionsSHA256 == optSum
}

// sourceSums returns the digests a reference records of the source at path:
// the source's (see [sourceDigest]) and its options.json's, "" without one.
func sourceSums(engine, path string) (srcSum, optSum string, srcErr error) {
	srcSum, srcErr = sourceDigest(engine, path)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		if got, err := fileSHA256(filepath.Join(path, fixture.OptionsFile)); err == nil {
			optSum = got
		}
	}
	return srcSum, optSum, srcErr
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
	srcSum, optSum, srcErr := sourceSums(refs.Engine, fx.PDFPath)
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
