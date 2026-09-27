package pdf

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// declarationPDF builds a two-page document whose header declares header,
// whose catalog carries catalogExtra and whose trailer carries trailerExtra.
// Page 1 draws text and holds a text-field widget, so every edit that forces a
// full rewrite has something to act on.
func declarationPDF(header, catalogExtra, trailerExtra string) []byte {
	content := "BT /F1 12 Tf 72 700 Td (Hello) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [6 0 R] >> " + catalogExtra + " >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R /Annots [6 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (value) /Rect [72 600 272 620] /P 3 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-" + header + "\n%\xE2\xE3\xCF\xD3\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R %s >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, trailerExtra, xref)
	return buf.Bytes()
}

// declarationPassword opens every encrypted output these tests write.
const declarationPassword = "user"

// rewriteEdit is one way to make litho write a document in full.
type rewriteEdit struct {
	name string
	// floor is the version the edit's own constructs require, "" when none.
	floor string
	// encrypts reports whether the output opens only with
	// declarationPassword.
	encrypts bool
	write    func(ctx context.Context, r *Reader, dst *bytes.Buffer) error
}

// writerEdit returns a rewriteEdit that queues edit on a Writer over the
// source and writes the result.
func writerEdit(name string, edit func(w *Writer, r *Reader) error, opts ...WriteOption) rewriteEdit {
	return rewriteEdit{name: name, write: func(ctx context.Context, r *Reader, dst *bytes.Buffer) error {
		w := NewWriter(r)
		defer w.Close()
		if err := edit(w, r); err != nil {
			return err
		}
		return w.WriteTo(ctx, dst, opts...)
	}}
}

// fullRewriteEdits lists every public path that writes a document in full
// rather than appending to it.
func fullRewriteEdits() []rewriteEdit {
	encrypt := func(alg EncryptAlgorithm) func(w *Writer, _ *Reader) error {
		return func(w *Writer, _ *Reader) error {
			w.Encrypt(EncryptConfig{UserPassword: declarationPassword, OwnerPassword: "owner", Algorithm: alg, Permissions: -1})
			return nil
		}
	}
	rc4 := writerEdit("Encrypt RC4-128", encrypt(EncryptRC4_128))
	rc4.encrypts = true
	aes := writerEdit("Encrypt AES-256", encrypt(EncryptAES256))
	aes.encrypts = true
	// AES-256 selects the AESV3 crypt filter, which litho declares as an
	// extension to PDF 1.7.
	aes.floor = "1.7"

	return []rewriteEdit{
		writerEdit("WithFullRewrite", func(*Writer, *Reader) error { return nil }, WithFullRewrite()),
		writerEdit("DeletePages", func(w *Writer, _ *Reader) error { return w.DeletePages(2) }),
		writerEdit("InsertPages", func(w *Writer, r *Reader) error { return w.InsertPages(2, r, 1) }),
		writerEdit("FlattenForm", func(w *Writer, _ *Reader) error { w.FlattenForm(); return nil }),
		rc4,
		aes,
		{name: "Split", write: func(ctx context.Context, r *Reader, dst *bytes.Buffer) error {
			return Split(ctx, r, []int{1}, dst)
		}},
		{name: "Merge", write: func(ctx context.Context, r *Reader, dst *bytes.Buffer) error {
			return Merge(ctx, []*Reader{r, r}, dst)
		}},
	}
}

// openDeclaration opens data, with the test password when it is encrypted.
func openDeclaration(t *testing.T, data []byte, encrypted bool) *Reader {
	t.Helper()
	var opts []OpenOption
	if encrypted {
		opts = append(opts, WithPassword(declarationPassword))
	}
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return r
}

// TestFullRewrite_KeepsTheSourceDeclaration pins that a full rewrite declares
// the version its source declares, in the header and in the catalog alike.
//
// The rewrite copies the catalog, and its /Version with it, but it wrote the
// header from the version the reader reports, which follows the catalog: ISO
// 32000-1, 7.5.2 lets the catalog entry stand in for the header, and litho
// reads it in both directions as pdf.js does. A PDF 2.0 file whose catalog
// says /1.7 came out of every edit below as %PDF-1.7, so the file read as 1.7
// by every reader, and Table 28, which takes whichever of the two is later,
// no longer saw a PDF 2.0 file. That broke ISO 19005-4, 6.1.2 on a PDF/A-4
// source. The same rewrite lowered a %PDF-1.7 header under a catalog /1.4 to
// %PDF-1.4.
//
// # Corpus evidence
//
//   - litho a4syntax_4.pdf: its header is %PDF-2.0 and its catalog /Version
//     /1.7, and page insertion and deletion, flattening and encryption each
//     wrote %PDF-1.7.
func TestFullRewrite_KeepsTheSourceDeclaration(t *testing.T) {
	t.Parallel()
	sources := []struct {
		header, catalog string
		// wantHeader is the header the output declares, before an edit's
		// own floor applies.
		wantHeader string
	}{
		{header: "2.0", catalog: "1.7", wantHeader: "2.0"},
		{header: "1.7", catalog: "1.4", wantHeader: "1.7"},
		// A catalog entry later than the header raises the document's
		// version (Table 28), and the header the rewrite writes says so,
		// as it did before.
		{header: "1.4", catalog: "1.7", wantHeader: "1.7"},
		{header: "2.0", catalog: "", wantHeader: "2.0"},
	}
	for _, src := range sources {
		catalogExtra := ""
		if src.catalog != "" {
			catalogExtra = "/Version /" + src.catalog
		}
		data := declarationPDF(src.header, catalogExtra, "")
		for _, edit := range fullRewriteEdits() {
			t.Run(fmt.Sprintf("%s over %%PDF-%s /%s", edit.name, src.header, src.catalog), func(t *testing.T) {
				t.Parallel()
				ctx := context.Background()
				r := openDeclaration(t, data, false)
				defer r.Close()
				sourceVersion := r.Version()

				var out bytes.Buffer
				if err := edit.write(ctx, r, &out); err != nil {
					t.Fatalf("write: %v", err)
				}

				wantHeader, wantVersion := src.wantHeader, sourceVersion
				if edit.floor != "" {
					wantHeader = writer.AtLeastVersion(wantHeader, edit.floor)
					wantVersion = writer.AtLeastVersion(wantVersion, edit.floor)
				}
				if got := pdfHeader(out.Bytes()); got != "%PDF-"+wantHeader {
					t.Errorf("header = %q, want %%PDF-%s", got, wantHeader)
				}

				o := openDeclaration(t, out.Bytes(), edit.encrypts)
				defer o.Close()
				if got := o.Version(); got != wantVersion {
					t.Errorf("output reads as version %q, want %q, what the source reads as", got, wantVersion)
				}
			})
		}
	}
}

// indirectInfo matches a trailer /Info written as an indirect reference.
var indirectInfo = regexp.MustCompile(`/Info \d+ 0 R`)

// TestFullRewrite_CarriesADirectInfo pins that a full rewrite keeps a document
// information dictionary the source's trailer holds directly.
//
// Only a reference was copied, so a direct /Info vanished with every title,
// author and date in it. ISO 32000-1, 7.5.5 (Table 15) makes /Info an
// indirect reference, and the rewrite writes it as one: a new file has no
// reason to repeat what the source got wrong, and an encrypted output then
// encrypts it with the rest (7.6.1), where a direct dictionary has no object
// number to key it.
//
// # Corpus evidence
//
//   - litho xrefstm_2b.pdf: its cross-reference stream holds a direct /Info,
//     and page insertion and deletion, flattening and encryption each wrote
//     the file without one.
func TestFullRewrite_CarriesADirectInfo(t *testing.T) {
	t.Parallel()
	data := declarationPDF("1.7", "", "/Info << /Title (Direct title) /Author (Direct author) >>")
	for _, edit := range fullRewriteEdits() {
		t.Run(edit.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := openDeclaration(t, data, false)
			defer r.Close()
			if meta, err := r.Metadata(ctx); err != nil || meta["Title"] != "Direct title" {
				t.Fatalf("source Metadata = %v, %v; the fixture's direct /Info is not read", meta, err)
			}

			var out bytes.Buffer
			if err := edit.write(ctx, r, &out); err != nil {
				t.Fatalf("write: %v", err)
			}

			o := openDeclaration(t, out.Bytes(), edit.encrypts)
			defer o.Close()
			meta, err := o.Metadata(ctx)
			if err != nil {
				t.Fatalf("output Metadata: %v", err)
			}
			if meta["Title"] != "Direct title" || meta["Author"] != "Direct author" {
				t.Errorf("output Title %q, Author %q; want the source's direct /Info", meta["Title"], meta["Author"])
			}
			if !indirectInfo.Match(out.Bytes()) {
				t.Errorf("the output trailer holds no /Info reference (ISO 32000-1, Table 15)")
			}
		})
	}
}

// TestMerge_DeclaresTheLatestSource pins that a merge declares the latest
// version among its sources, in the header and in the catalog /Version it
// copies from the first source.
//
// The catalog entry comes from the first source alone, and litho reads it in
// both directions, so a first source whose catalog said /1.4 made a merge
// with a PDF 2.0 source read as 1.4 under a %PDF-2.0 header. The header was
// the latest version the reader reported for any source, which lowered a
// %PDF-2.0 source with a catalog /1.7 to %PDF-1.7, as a full rewrite did.
func TestMerge_DeclaresTheLatestSource(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                    string
		first, firstCatalog     string
		second                  string
		wantHeader, wantReadsAs string
	}{
		{"a later second source raises the copied catalog entry", "1.4", "/Version /1.4", "2.0", "2.0", "2.0"},
		{"an earlier second source lowers nothing", "2.0", "/Version /1.7", "1.4", "2.0", "1.7"},
		{"no catalog entry leaves the header to declare", "1.4", "", "2.0", "2.0", "2.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			a := openDeclaration(t, declarationPDF(tc.first, tc.firstCatalog, ""), false)
			defer a.Close()
			b := openDeclaration(t, declarationPDF(tc.second, "", ""), false)
			defer b.Close()

			var out bytes.Buffer
			if err := Merge(ctx, []*Reader{a, b}, &out); err != nil {
				t.Fatalf("Merge: %v", err)
			}
			if got := pdfHeader(out.Bytes()); got != "%PDF-"+tc.wantHeader {
				t.Errorf("header = %q, want %%PDF-%s", got, tc.wantHeader)
			}
			o := openDeclaration(t, out.Bytes(), false)
			defer o.Close()
			if got := o.Version(); got != tc.wantReadsAs {
				t.Errorf("merge reads as version %q, want %q", got, tc.wantReadsAs)
			}
		})
	}
}
