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
// and the pin the fetched documents come from with its value. CoreCommit is
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

// Prepare checks, for a corpus whose references the host builds, that every
// fixture has a reference built at the pinned GOTENBERG_IMAGE (and, for a
// fetched corpus, at the pin its manifest's references.source_pin names)
// from its current source and
// options, and that the provisioned PDFs the goldens were made from are
// those references. Any disagreement fails the cell with the command that
// rebuilds what is stale, so no fixture is scored against a reference of
// something else. A corpus whose references git versions passes.
func (f *diffFeature) Prepare(_ context.Context, fixtures []Fixture) error {
	f.mu.RLock()
	refs, root, corpus, pins := f.refs, f.testdataRoot, f.corpus, f.pins
	f.mu.RUnlock()
	if refs == nil {
		return nil
	}
	if pins == nil {
		resolved, err := loadPins()
		if err != nil {
			return err
		}
		pins = resolved.Values
	}
	refDir := filepath.Join(root, filepath.FromSlash(refs.Dir))
	data, err := os.ReadFile(filepath.Join(refDir, referenceManifestName)) //nolint:gosec // provisioned record under the corpus testdata root
	if err != nil {
		return fmt.Errorf("%w: %s unreadable (run %s): %w", errStaleReferences, filepath.Join(refDir, referenceManifestName), refs.BuiltBy, err)
	}
	var man referenceManifest
	if err := json.Unmarshal(data, &man); err != nil {
		return fmt.Errorf("%w: decode %s: %w", errStaleReferences, referenceManifestName, err)
	}
	var stale, regolden []string
	for _, fx := range fixtures {
		name := reportName(fx.ID)
		if problem := checkReference(man, refs.SourcePin, name, fx, pins); problem != "" {
			stale = append(stale, problem)
			continue
		}
		// The goldens come from the provisioned copy of each reference,
		// pdfs/<name>.pdf: a copy that differs was made from another build.
		got, err := fileSHA256(filepath.Join(root, "pdfs", name+".pdf"))
		if err != nil || got != man.References[name].PDFSHA256 {
			regolden = append(regolden, name)
		}
	}
	switch {
	case len(stale) > 0:
		return fmt.Errorf("%w: %s (run %s)", errStaleReferences, summarize(stale), refs.BuiltBy)
	case len(regolden) > 0:
		return fmt.Errorf("%w: the diff goldens were not made from the references of %s (run rm -rf %s %s, then corpus init --corpus=%s)",
			errStaleReferences, summarize(regolden), filepath.Join(root, "pdfs"), filepath.Join(root, "goldens", "diff"), corpus)
	}
	return nil
}

// checkReference returns what is wrong with the reference of one fixture,
// or "" when it was built from the fixture's current source at the pins:
// the image's and, when sourcePin is not "", the pin the fetched documents
// come from.
func checkReference(man referenceManifest, sourcePin, name string, fx Fixture, pins map[string]string) string {
	rec, ok := man.References[name]
	if !ok {
		if fail, refused := man.Failed[name]; refused {
			return fmt.Sprintf("%s was refused by the route (HTTP %s: %s)", name, fail.Status, fail.Answer)
		}
		return name + " has no reference"
	}
	if want := pins["GOTENBERG_IMAGE"]; rec.Image != want {
		return fmt.Sprintf("%s was built with %s, the pin is %s", name, rec.Image, want)
	}
	source, err := fixture.LoadOffice(fx.PDFPath)
	if err != nil {
		return fmt.Sprintf("%s: %v", name, err)
	}
	if sourcePin != "" {
		if got, want := rec.pinValue(sourcePin), pins[sourcePin]; got != want {
			return fmt.Sprintf("%s was built from the documents of %s %q, the pin is %q", name, sourcePin, got, want)
		}
	}
	if got, err := fileSHA256(source.Document); err != nil || got != rec.SourceSHA256 {
		return name + "'s source changed since its reference was built"
	}
	opts := ""
	if info, err := os.Stat(fx.PDFPath); err == nil && info.IsDir() {
		if got, err := fileSHA256(filepath.Join(fx.PDFPath, fixture.OptionsFile)); err == nil {
			opts = got
		}
	}
	if opts != rec.OptionsSHA256 {
		return name + "'s options changed since its reference was built"
	}
	return ""
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
