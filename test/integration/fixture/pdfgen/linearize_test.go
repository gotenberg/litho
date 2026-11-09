package pdfgen

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestLinearizeMatchesQPDF builds small documents and compares the
// linearized output with the SHA-256 of what pikepdf 9.11 over qpdf 12.2
// wrote for the same document with save(deterministic_id=True,
// compress_streams=False, stream_decode_level=none, linearize=True). The
// pikepdf script is in each case's comment.
func TestLinearizeMatchesQPDF(t *testing.T) {
	cases := []struct {
		name  string
		build func() (*Doc, SaveOptions)
		want  string
	}{
		{
			// One page, no resources: part 8 and the shared object table's
			// part 8 fields stay empty.
			name: "one_page",
			build: func() (*Doc, SaveOptions) {
				pdf := New()
				pdf.AddPage(pdf.Indirect(D("Type", N("Page"), "MediaBox", A(0, 0, 200, 200),
					"Contents", pdf.Stream([]byte("0 0 m 10 10 l S\n")))))
				return pdf, SaveOptions{}
			},
			want: "e54fdf34f18494c062d82716b745f83a565fd4a6d3df5f930d2f99b5c08037c6",
		},
		{
			// Five pages: a font on every page (part 6), a form XObject on
			// pages 1 to 4 (part 8, shared identifiers), a private
			// ExtGState on page 3 (part 7) and an /Info dictionary (part 9,
			// and an /ID seed).
			name: "five_pages_shared",
			build: func() (*Doc, SaveOptions) {
				pdf := New()
				font := pdf.Indirect(D("Type", N("Font"), "Subtype", N("Type1"), "BaseFont", N("Helvetica")))
				xo := pdf.Stream([]byte("0 0 m 5 5 l S\n"), "Type", N("XObject"), "Subtype", N("Form"), "BBox", A(0, 0, 10, 10))
				for i := range 5 {
					res := D("Font", D("F1", font))
					if i > 0 {
						res.Set("XObject", D("X1", xo))
					}
					if i == 3 {
						res.Set("ExtGState", D("G", pdf.Indirect(D("Type", N("ExtGState"), "CA", 0.5))))
					}
					contents := pdf.Stream([]byte("BT /F1 12 Tf (p" + string(rune('0'+i)) + ") Tj ET /X1 Do\n"))
					pdf.AddPage(pdf.Indirect(D("Type", N("Page"), "MediaBox", A(0, 0, 612, 792),
						"Resources", res, "Contents", contents)))
				}
				pdf.Trailer.Set("Info", pdf.Indirect(D("Title", String("five"), "Producer", String("pdfgen"))))
				return pdf, SaveOptions{MinVersion: "1.6"}
			},
			want: "3ee54dc151ff40a7c7d63bc0a255087c38b2354282a46cbac005f5c0c7132d23",
		},
		{
			// An annotation whose /P points back to its page, outlines in
			// the first page section (/PageMode /UseOutlines, so the hint
			// stream carries /O), an indirect /OpenAction (part 4) and a
			// catalog /Metadata stream.
			name: "annots_outlines_first_page",
			build: func() (*Doc, SaveOptions) {
				pdf := New()
				var pages []*Dict
				for range 2 {
					contents := pdf.Stream([]byte("0 0 m 1 1 l S\n"))
					pages = append(pages, pdf.AddPage(pdf.Indirect(D("Type", N("Page"),
						"MediaBox", A(0, 0, 300, 300), "Contents", contents))))
				}
				annot := pdf.Indirect(D("Type", N("Annot"), "Subtype", N("Text"), "Rect", A(10, 10, 20, 20),
					"Contents", String("hi"), "P", pages[1]))
				pages[1].Set("Annots", A(annot))
				outlines := pdf.Indirect(D("Type", N("Outlines"), "Count", 1))
				item := pdf.Indirect(D("Title", String("One"), "Parent", outlines, "Dest", A(pages[1], N("Fit"))))
				outlines.Set("First", item).Set("Last", item)
				pdf.Root.Set("Outlines", outlines)
				pdf.Root.Set("PageMode", N("UseOutlines"))
				pdf.Root.Set("OpenAction", pdf.MakeIndirect(A(pages[0], N("Fit"))))
				pdf.Root.Set("Metadata", pdf.Stream([]byte("<x/>"), "Type", N("Metadata"), "Subtype", N("XML")))
				return pdf, SaveOptions{DisableFixMetadataVersion: true}
			},
			want: "4a4ccad4f35845cd20564a29dd40eb3ec1b8e183af43deb13d9e121be1ebc98c",
		},
		{
			// /MediaBox, /Resources and /Rotate on the page tree root before
			// the first page: qpdf's first page insertion pushes them down to
			// no page, so they vanish. Outlines go to part 9.
			name: "inherited_outlines_last",
			build: func() (*Doc, SaveOptions) {
				pdf := New()
				font := pdf.Indirect(D("Type", N("Font"), "Subtype", N("Type1"), "BaseFont", N("Courier")))
				pdf.Pages.Set("MediaBox", A(0, 0, 400, 400))
				pdf.Pages.Set("Resources", D("Font", D("F1", font)))
				pdf.Pages.Set("Rotate", 90)
				for range 3 {
					contents := pdf.Stream([]byte("BT /F1 9 Tf (x) Tj ET\n"))
					pdf.AddPage(pdf.Indirect(D("Type", N("Page"), "Contents", contents)))
				}
				pdf.Root.Set("Outlines", pdf.Indirect(D("Type", N("Outlines"), "Count", 0)))
				return pdf, SaveOptions{}
			},
			want: "21965d6e8664cb54821312f365857c833a30cd917062f140c346261005c24738",
		},
		{
			// /Resources and /Rotate on the page tree root after the pages:
			// optimize does not push them again, so they stay on /Pages and
			// their font is a part 9 object.
			name: "pages_attributes_after_pages",
			build: func() (*Doc, SaveOptions) {
				pdf := New()
				font := pdf.Indirect(D("Type", N("Font"), "Subtype", N("Type1"), "BaseFont", N("Courier")))
				for range 3 {
					contents := pdf.Stream([]byte("BT /F1 9 Tf (x) Tj ET\n"))
					pdf.AddPage(pdf.Indirect(D("Type", N("Page"), "MediaBox", A(0, 0, 400, 400), "Contents", contents)))
				}
				pdf.Pages.Set("Resources", D("Font", D("F1", font)))
				pdf.Pages.Set("Rotate", 90)
				return pdf, SaveOptions{}
			},
			want: "428c70f844019cf2b6d123c099b40059019b112dd5591f8f224905632d6a9511",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pdf, opts := tc.build()
			opts.Linearize = true
			out, err := pdf.Save(opts)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(out.Bytes)
			if got := hex.EncodeToString(sum[:]); got != tc.want {
				if dir := os.Getenv("PDFGEN_DUMP"); dir != "" {
					_ = os.WriteFile(filepath.Join(dir, tc.name+".pdf"), out.Bytes, 0o600) //nolint:errcheck // best-effort debugging aid.
				}
				t.Errorf("sha256 %s, want %s", got, tc.want)
			}
		})
	}
}
