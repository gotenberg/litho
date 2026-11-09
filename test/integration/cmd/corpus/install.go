package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// errNothingToInstall reports a corpus whose PDFs are neither committed nor
// built into its testdata by the host.
var errNothingToInstall = errors.New("no committed PDFs and no references")

// installPDFs puts a corpus's PDFs into the pdfs/ directory of its testdata,
// the one the provisioning image derives the goldens from, for the corpora
// that download nothing: the PDFs a corpus commits in its source directory
// ([Manifest.CommittedPDFs]), or the references the host built into its
// testdata ([Manifest.References]), each checked against the SHA-256 their
// manifest.json records, so the goldens come from the references the corpus
// CLI checks. It records the pdfs stage in versions.json.
func installPDFs(m *Manifest, testdataRoot string) error {
	out := filepath.Join(testdataRoot, "pdfs")
	var (
		count int
		err   error
	)
	switch {
	case m.CommittedPDFs != "":
		count, err = installCommitted(filepath.Join(m.SourceDir(), filepath.FromSlash(m.CommittedPDFs)), out)
	case m.References != nil:
		count, err = installReferences(filepath.Join(testdataRoot, filepath.FromSlash(m.References.Dir)), out, m.References.BuiltBy)
	default:
		return errNothingToInstall
	}
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d PDFs installed in %s\n", m.Name, count, out)
	pins := map[string]string{}
	return mergeStageRecord(testdataRoot, pdfsStage, map[string]any{"recorded_at": time.Now().UTC().Format(time.RFC3339), "pins": pins})
}

// installCommitted copies the PDFs at the top of src into out.
func installCommitted(src, out string) (int, error) {
	pdfs, err := topPDFs(src, false)
	if err != nil {
		return 0, err
	}
	if len(pdfs) == 0 {
		return 0, fmt.Errorf("no PDF under %s", src)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return 0, err
	}
	for _, p := range pdfs {
		data, err := os.ReadFile(p) //nolint:gosec // a committed corpus PDF
		if err != nil {
			return 0, err
		}
		if err := os.WriteFile(filepath.Join(out, filepath.Base(p)), data, 0o644); err != nil { //nolint:gosec // testdata is world-readable
			return 0, err
		}
	}
	return len(pdfs), nil
}

// installReferences copies the references the manifest.json of src records
// into out, refusing one whose SHA-256 is not the recorded one.
func installReferences(src, out, builtBy string) (int, error) {
	data, err := os.ReadFile(filepath.Join(src, referenceManifestName)) //nolint:gosec // the references the host built
	if err != nil {
		return 0, fmt.Errorf("no reference manifest in %s: run %s: %w", src, builtBy, err)
	}
	var man referenceManifest
	if err := json.Unmarshal(data, &man); err != nil {
		return 0, fmt.Errorf("decode %s: %w", filepath.Join(src, referenceManifestName), err)
	}
	if len(man.References) == 0 {
		return 0, fmt.Errorf("no reference under %s: run %s", src, builtBy)
	}
	names := make([]string, 0, len(man.References))
	for n := range man.References {
		names = append(names, n)
	}
	sort.Strings(names)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return 0, err
	}
	for _, n := range names {
		p := filepath.Join(src, n+".pdf")
		pdf, err := os.ReadFile(p) //nolint:gosec // a reference the host built
		if err != nil {
			return 0, fmt.Errorf("%s: run %s: %w", p, builtBy, err)
		}
		sum := sha256.Sum256(pdf)
		if hex.EncodeToString(sum[:]) != man.References[n].PDFSHA256 {
			return 0, fmt.Errorf("%s is not the reference %s records: run %s", p, referenceManifestName, builtBy)
		}
		if err := os.WriteFile(filepath.Join(out, n+".pdf"), pdf, 0o644); err != nil { //nolint:gosec // testdata is world-readable
			return 0, err
		}
	}
	return len(names), nil
}
