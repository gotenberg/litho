package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// laterRoundCLIPDF builds a one-page document whose PDF/A-1b remediation keeps
// a second round: its XMP packet writes dc:title and dc:creator as attributes,
// which the first round strips and only the second, run on the first round's
// output, injects in the form ISO 19005-1, 6.7.3 requires. Without the binary
// header comment ISO 19005-1, 6.1.2 requires, the first round rewrites the
// file in full.
func laterRoundCLIPDF(binaryComment bool) []byte {
	const xmp = `<?xpacket begin="` + "\xEF\xBB\xBF" + `" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/" dc:title="Fixture" dc:creator="litho"/>` +
		`</rdf:RDF></x:xmpmeta><?xpacket end="w"?>`
	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(xmp), xmp),
		"<< /Title (Fixture) /Author (litho) >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	if binaryComment {
		buf.WriteString("%\xE2\xE3\xCF\xD3\n")
	}
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 7 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// pdfa1bFindings validates the file at path against PDF/A-1b and returns the
// text of each violation, sorted, with the /Rotate of every page.
func pdfa1bFindings(t *testing.T, path string) ([]string, []int) {
	t.Helper()

	ctx := context.Background()
	r, err := pdf.OpenFile(ctx, path)
	if err != nil {
		t.Fatalf("opening %s: %v", filepath.Base(path), err)
	}
	defer r.Close()

	errs, err := r.ValidatePDFA(ctx, models.PDFA1b)
	if err != nil {
		t.Fatalf("validating %s: %v", filepath.Base(path), err)
	}
	texts := make([]string, len(errs))
	for i, e := range errs {
		texts[i] = e.Error()
	}
	slices.Sort(texts)

	n, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("counting the pages of %s: %v", filepath.Base(path), err)
	}
	rotations := make([]int, n)
	for i := range n {
		page, err := r.Page(ctx, i+1)
		if err != nil {
			t.Fatalf("reading page %d of %s: %v", i+1, filepath.Base(path), err)
		}
		rotations[i] = page.Rotate
	}
	return texts, rotations
}

// TestPipeRotateAfterRemediateKeepsEveryRound pins that a stage queued after a
// remediate stage keeps the repairs of every round the remediation kept.
//
// `litho pipe f remediate -profile PDF/A-1b rotate -angle 90` wrote a file
// that failed ISO 19005-1, 6.7.3 where `litho pipe f remediate -profile
// PDF/A-1b` did not: the rotation made the remediation output stale, and the
// Writer serialized afresh from state that held the first round's repairs
// alone, while the second round had injected the dc:title and dc:creator the
// clause requires.
func TestPipeRotateAfterRemediateKeepsEveryRound(t *testing.T) {
	for _, tc := range []struct {
		name          string
		binaryComment bool
	}{
		{name: "an incremental first round", binaryComment: true},
		{name: "a rewritten first round"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := writeTempPDF(t, laterRoundCLIPDF(tc.binaryComment))
			dir := t.TempDir()

			plain := filepath.Join(dir, "plain.pdf")
			if rc, _, stderr := runPipeCLI(t, "pipe", "-o", plain, src, "remediate", "-profile", "PDF/A-1b"); rc != exitOK {
				t.Fatalf("pipe remediate: exit %d; stderr: %s", rc, stderr)
			}
			rotated := filepath.Join(dir, "rotated.pdf")
			if rc, _, stderr := runPipeCLI(t, "pipe", "-o", rotated, src,
				"remediate", "-profile", "PDF/A-1b", "rotate", "-angle", "90"); rc != exitOK {
				t.Fatalf("pipe remediate rotate: exit %d; stderr: %s", rc, stderr)
			}

			want, _ := pdfa1bFindings(t, plain)
			got, rotations := pdfa1bFindings(t, rotated)
			if !slices.Equal(got, want) {
				t.Errorf("the rotated output validates to %q, want what the remediation alone leaves: %q", got, want)
			}
			for i, rot := range rotations {
				if rot != 90 {
					t.Errorf("page %d has rotation %d, want 90", i+1, rot)
				}
			}
		})
	}
}
