package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// signFieldTestPDF returns a one-page document with the text field Name and
// the unsigned signature field Approval, whose dictionary gains sigEntries,
// extra being objects 6 and on.
func signFieldTestPDF(sigEntries string, extra ...string) []byte {
	objects := append([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R] >>",
		"<< /FT /Tx /T (Name) /V (Jane) /Type /Annot /Subtype /Widget /Rect [0 0 0 0] /P 3 0 R >>",
		"<< /FT /Sig /T (Approval) /Type /Annot /Subtype /Widget /Rect [0 0 0 0] /P 3 0 R " + sigEntries + " >>",
	}, extra...)
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// requireQPDF skips the test when qpdf, which reads the fields, is absent.
func requireQPDF(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("qpdf"); err != nil {
		t.Skip("qpdf is not on PATH")
	}
}

// TestMutationSign_SignsAnExistingField runs mutate-sign on documents whose
// unsigned signature field holds a /Lock or seed values. Each path keeps its
// promise, so each matches, and the inventory counts the field signed or
// refused.
func TestMutationSign_SignsAnExistingField(t *testing.T) {
	t.Parallel()
	requireQPDF(t)
	for _, tc := range []struct {
		name            string
		data            []byte
		signed, refused int
	}{
		{"no entry", signFieldTestPDF(""), 1, 0},
		{"a lock", signFieldTestPDF("/Lock 6 0 R", "<< /Type /SigFieldLock /Action /Include /Fields [(Name)] >>"), 1, 0},
		{"seed values it meets", signFieldTestPDF("/SV << /Ff 8 /SubFilter [/ETSI.CAdES.detached] /Reasons [(Approved)] >>"), 1, 0},
		{"seed values it cannot meet", signFieldTestPDF("/SV << /Ff 2 /SubFilter [/adbe.x509.rsa_sha1] >>"), 0, 1},
		{"an undefined lock action", signFieldTestPDF("/Lock << /Action /Some >>"), 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newMutationFeature(specNamed(t, "sign"))
			v := runMutationFixtures(t, f, tc.data, "PDF_A-2b")[0]
			if v.Skipped || !v.Match {
				t.Errorf("skipped=%t match=%t reason %q, want a match", v.Skipped, v.Match, v.Reason)
			}
			if v.Metrics[metricFieldSigned] != tc.signed || v.Metrics[metricFieldRefused] != tc.refused {
				t.Errorf("metrics %v, want %d signed and %d refused", v.Metrics, tc.signed, tc.refused)
			}
		})
	}
}

// TestVerifySignFields_CatchesABrokenPromise feeds the checks trials that
// break what the field promises: a lock the output does not honour, and a
// refusal the field's seed values do not explain. Each counts promise_broken.
func TestVerifySignFields_CatchesABrokenPromise(t *testing.T) {
	t.Parallel()
	requireQPDF(t)
	ctx := context.Background()
	signer, err := sharedSigner()
	if err != nil {
		t.Fatal(err)
	}
	f := newMutationFeature(specNamed(t, "sign"))
	f.signer = signer
	in := &mutationInput{}

	t.Run("a lock the output ignores", func(t *testing.T) {
		t.Parallel()
		locked := signFieldTestPDF("/Lock 6 0 R", "<< /Type /SigFieldLock /Action /All >>")
		form, err := readQPDFForm(ctx, f.qpdf, locked)
		if err != nil {
			t.Fatal(err)
		}
		// The output of signing the same field without its lock: nothing
		// turns read-only and no FieldMDP transform is recorded.
		trial := f.signInto(ctx, signFieldTestPDF(""), "Approval")
		if trial.err != nil {
			t.Fatal(trial.err)
		}
		failures, _ := f.verifySignFields(ctx, in, form, []signFieldTrial{trial})
		if !hasFailure(failures, metricPromiseBroken, "flags") || !hasFailure(failures, metricPromiseBroken, "FieldMDP") {
			t.Errorf("failures %v, want the flags and the FieldMDP transform reported", failures)
		}
	})

	t.Run("a refusal the seed values do not explain", func(t *testing.T) {
		t.Parallel()
		form, err := readQPDFForm(ctx, f.qpdf, signFieldTestPDF("/SV << /Ff 64 /DigestMethod [/SHA256] >>"))
		if err != nil {
			t.Fatal(err)
		}
		trial := signFieldTrial{name: "Approval", err: fmt.Errorf("planted: %w", pdf.ErrSeedValueUnmet)}
		failures, counts := f.verifySignFields(ctx, in, form, []signFieldTrial{trial})
		if !hasFailure(failures, metricPromiseBroken, "planted") || counts[metricFieldRefused] != 0 {
			t.Errorf("failures %v counts %v, want the refusal reported and not counted", failures, counts)
		}
	})
}

// hasFailure reports whether failures holds one of metric whose detail
// contains substr.
func hasFailure(failures []mutationFailure, metric, substr string) bool {
	for _, f := range failures {
		if f.Metric == metric && strings.Contains(f.Detail, substr) {
			return true
		}
	}
	return false
}

// TestMutationCountersAreUncompared keeps every inventory count a mutate-*
// feature emits out of the comparison. A count of what a run reached is
// neither better higher nor lower, and the cost comparison would read a rise
// as a regression.
func TestMutationCountersAreUncompared(t *testing.T) {
	t.Parallel()
	for _, spec := range mutationSpecs {
		for _, c := range spec.counters {
			if !uncomparedMetrics[c] || costMetrics[c] {
				t.Errorf("mutate-%s counts %s, which must be in uncomparedMetrics and not in costMetrics", spec.name, c)
			}
		}
	}
}
