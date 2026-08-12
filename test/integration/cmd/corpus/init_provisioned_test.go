package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeCorpus lays out a corpus testdata root: one PDF under pdfs/ and a
// directory per golden, each populated unless named in empty.
func writeCorpus(t *testing.T, goldens map[string]string, empty map[string]bool, withPDF bool) string {
	t.Helper()
	root := t.TempDir()

	if withPDF {
		pdfs := filepath.Join(root, "pdfs")
		if err := os.MkdirAll(pdfs, 0o755); err != nil {
			t.Fatalf("mkdir pdfs: %v", err)
		}
		if err := os.WriteFile(filepath.Join(pdfs, "a.pdf"), []byte("%PDF-1.7\n"), 0o600); err != nil {
			t.Fatalf("write pdf: %v", err)
		}
	}

	for feature, rel := range goldens {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir golden %s: %v", feature, err)
		}
		if empty[feature] {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "g.json"), []byte("{}"), 0o600); err != nil {
			t.Fatalf("write golden %s: %v", feature, err)
		}
	}
	return root
}

// autoGoldens mirrors the govdocs and pdf20 manifests: three VeraPDF-derived
// features plus two that the container always fills.
func autoGoldens() map[string]string {
	return map[string]string{
		"compliance":             "goldens/compliance",
		"remediation":            "goldens/remediation",
		"remediation-aggressive": "goldens/remediation-aggressive",
		"metadata":               "goldens/metadata",
		"text":                   "goldens/text",
	}
}

func veraEmpty() map[string]bool {
	return map[string]bool{"compliance": true, "remediation": true, "remediation-aggressive": true}
}

// TestProvisionedAcceptsClaimlessAutoCorpus pins that an auto-mode corpus whose
// scan found no profile claim counts as provisioned.
//
// run-verapdf.sh buckets PDFs by the profile each claims in XMP. govdocs (1091
// PDFs) and pdf20 (7) claim none, so claims.csv has zero rows and the three
// VeraPDF golden directories stay empty forever. Requiring them to be non-empty
// marked both corpora unprovisioned on every run, and `make corpus-check` shelled
// out to an emulated `docker build` that never finished.
func TestProvisionedAcceptsClaimlessAutoCorpus(t *testing.T) {
	t.Parallel()

	root := writeCorpus(t, autoGoldens(), veraEmpty(), true)
	if err := os.WriteFile(filepath.Join(root, "claims.csv"), nil, 0o600); err != nil {
		t.Fatalf("write claims.csv: %v", err)
	}
	m := &Manifest{Name: "govdocs", PDFRoot: "pdfs", ProfileMode: ProfileModeAuto, Goldens: autoGoldens()}

	if !provisioned(root, m) {
		t.Error("a claimless auto-mode corpus should be provisioned, got false (init would run docker build)")
	}
}

// TestProvisionedRequiresScanToHaveRun checks that a MISSING claims.csv still
// forces provisioning. An absent file means the scan never ran, which is not the
// same as a scan that ran and found nothing.
func TestProvisionedRequiresScanToHaveRun(t *testing.T) {
	t.Parallel()

	root := writeCorpus(t, autoGoldens(), veraEmpty(), true)
	m := &Manifest{Name: "govdocs", PDFRoot: "pdfs", ProfileMode: ProfileModeAuto, Goldens: autoGoldens()}

	if provisioned(root, m) {
		t.Error("no claims.csv means the scan never ran; want not provisioned")
	}
}

// TestProvisionedRequiresGoldenDirToExist checks the leniency is bounded: the
// VeraPDF golden may be empty, but it must have been created.
func TestProvisionedRequiresGoldenDirToExist(t *testing.T) {
	t.Parallel()

	goldens := autoGoldens()
	root := writeCorpus(t, goldens, veraEmpty(), true)
	if err := os.WriteFile(filepath.Join(root, "claims.csv"), nil, 0o600); err != nil {
		t.Fatalf("write claims.csv: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "goldens/compliance")); err != nil {
		t.Fatalf("remove golden: %v", err)
	}
	m := &Manifest{Name: "govdocs", PDFRoot: "pdfs", ProfileMode: ProfileModeAuto, Goldens: goldens}

	if provisioned(root, m) {
		t.Error("a missing golden directory must still force provisioning")
	}
}

// TestProvisionedStillRequiresNonEmptyGoldensElsewhere pins that the exemption is
// scoped to claimless auto-mode corpora. verapdf and litho use "expand", where an
// empty compliance golden means VeraPDF really did fail.
func TestProvisionedStillRequiresNonEmptyGoldensElsewhere(t *testing.T) {
	t.Parallel()

	goldens := map[string]string{
		"compliance":             "goldens/compliance",
		"remediation":            "goldens/remediation",
		"remediation-aggressive": "goldens/remediation-aggressive",
	}
	root := writeCorpus(t, goldens, veraEmpty(), true)
	if err := os.WriteFile(filepath.Join(root, "claims.csv"), nil, 0o600); err != nil {
		t.Fatalf("write claims.csv: %v", err)
	}
	m := &Manifest{Name: "verapdf", PDFRoot: "pdfs", ProfileMode: ProfileModeExpand, Goldens: goldens}

	if provisioned(root, m) {
		t.Error("an expand-mode corpus with an empty compliance golden is not provisioned")
	}
}

// TestProvisionedRequiresNonVeraGoldens checks a claimless auto-mode corpus still
// needs the goldens the container always produces. Only the VeraPDF stage is
// legitimately empty.
func TestProvisionedRequiresNonVeraGoldens(t *testing.T) {
	t.Parallel()

	empty := veraEmpty()
	empty["text"] = true
	root := writeCorpus(t, autoGoldens(), empty, true)
	if err := os.WriteFile(filepath.Join(root, "claims.csv"), nil, 0o600); err != nil {
		t.Fatalf("write claims.csv: %v", err)
	}
	m := &Manifest{Name: "govdocs", PDFRoot: "pdfs", ProfileMode: ProfileModeAuto, Goldens: autoGoldens()}

	if provisioned(root, m) {
		t.Error("an empty text golden must force provisioning")
	}
}

// TestProvisionedRequiresPDFs checks an empty PDF tree is never provisioned, so a
// wiped corpus re-downloads instead of scoring zero fixtures.
func TestProvisionedRequiresPDFs(t *testing.T) {
	t.Parallel()

	root := writeCorpus(t, autoGoldens(), veraEmpty(), false)
	if err := os.WriteFile(filepath.Join(root, "claims.csv"), nil, 0o600); err != nil {
		t.Fatalf("write claims.csv: %v", err)
	}
	m := &Manifest{Name: "govdocs", PDFRoot: "pdfs", ProfileMode: ProfileModeAuto, Goldens: autoGoldens()}

	if provisioned(root, m) {
		t.Error("a corpus with no PDFs must be provisioned, got true")
	}
}

// TestProvisionedRejectsEmptyManifest keeps the original contract: a manifest
// declaring no goldens is never provisioned.
func TestProvisionedRejectsEmptyManifest(t *testing.T) {
	t.Parallel()

	root := writeCorpus(t, nil, nil, true)
	if provisioned(root, &Manifest{Name: "x", PDFRoot: "pdfs"}) {
		t.Error("a manifest with no goldens is not provisioned")
	}
}
