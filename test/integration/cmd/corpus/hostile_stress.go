package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
)

// stressCorpus is the corpus of large documents `corpus build` generates
// to prove a conversion's memory budget: sources at the input cap, many
// large pictures, sheets of many cells, documents of many pages. Its
// documents are never committed: the build writes the same bytes each
// time.
const stressCorpus = "litho/stress"

// Sizes of the stress documents.
const (
	// stressPhotoWidth and stressPhotoHeight are the sides of a picture of
	// the picture probes: 12 megapixels, a phone camera's photograph.
	stressPhotoWidth, stressPhotoHeight = 4000, 3000
	// stressCellsPerRow is how many fields a row of a CSV probe holds.
	stressCellsPerRow = 10
	// stressDocxPages is how many pages the long DOCX breaks into.
	stressDocxPages = 2000
)

// stressDoc is one document of the stress corpus.
type stressDoc struct {
	name string
	// file is the document's file name in its directory; the other files
	// write writes beside it are its resources.
	file string
	// write writes the document and its resources into dir.
	write func(dir string) error
}

// stressDocs lists the documents of the stress corpus, in a fixed order.
func stressDocs() []stressDoc {
	var docs []stressDoc
	for _, mib := range []int{1, 4, 16} {
		docs = append(docs,
			stressDoc{fmt.Sprintf("text-%dmib", mib), "index.txt", func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "index.txt"), stressText(mib<<20), 0o644) //nolint:gosec // testdata is world-readable
			}},
			stressDoc{fmt.Sprintf("html-%dmib", mib), "index.html", func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "index.html"), stressHTML(mib<<20), 0o644) //nolint:gosec // testdata is world-readable
			}},
		)
	}
	for _, kind := range []string{"png", "jpeg"} {
		for _, n := range []int{10, 50} {
			docs = append(docs, stressDoc{fmt.Sprintf("html-%s-%d", kind, n), "index.html", func(dir string) error {
				return writeStressPictures(dir, kind, n)
			}})
		}
	}
	for _, cells := range []int{100_000, 1_000_000} {
		docs = append(docs, stressDoc{fmt.Sprintf("csv-%dk", cells/1000), "index.csv", func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "index.csv"), stressCSV(cells), 0o644) //nolint:gosec // testdata is world-readable
		}})
	}
	docs = append(docs, stressDoc{fmt.Sprintf("docx-%dpages", stressDocxPages), "index.docx", func(dir string) error {
		data, err := stressDocx(stressDocxPages)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "index.docx"), data, 0o644) //nolint:gosec // testdata is world-readable
	}})
	return docs
}

// buildStress writes every stress document into <out>/<name>/, replacing
// what an earlier build wrote there.
func buildStress(ctx context.Context, m *Manifest) error {
	out := buildOut(m)
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	docs := stressDocs()
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return err
		}
		dir := filepath.Join(out, d.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := d.write(dir); err != nil {
			return fmt.Errorf("%s: %w", d.name, err)
		}
	}
	fmt.Printf("%s: %d stress documents built into %s\n", m.Name, len(docs), out)
	return nil
}

// stressFixtures lists the fixtures of the stress corpus, each the
// directory its document was built into, refusing one not built yet.
func stressFixtures(m *Manifest) ([]Fixture, error) {
	out := buildOut(m)
	docs := stressDocs()
	fixtures := make([]Fixture, 0, len(docs))
	for _, d := range docs {
		dir := filepath.Join(out, d.name)
		if _, err := os.Stat(filepath.Join(dir, d.file)); err != nil {
			return nil, fmt.Errorf("%w: %s is missing (run make corpus-build CORPUS=%s)", errInputsUnfetched, dir, m.Name)
		}
		fixtures = append(fixtures, Fixture{ID: d.name, PDFPath: dir})
	}
	return fixtures, nil
}

// stressWords are the words the text probes are made of.
var stressWords = []string{"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing", "elit", "sed", "do", "eiusmod", "tempor", "incididunt", "labore", "magna", "aliqua"} //nolint:gochecknoglobals // static table

// stressParagraph returns a paragraph of eighty words drawn from r.
func stressParagraph(r *rand.Rand) string {
	var b strings.Builder
	for i := range 80 {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(stressWords[r.IntN(len(stressWords))])
	}
	return b.String()
}

// stressText returns n bytes of paragraphs separated by blank lines.
func stressText(n int) []byte {
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a fixed seed, for the same bytes each build
	var b bytes.Buffer
	for b.Len() < n {
		b.WriteString(stressParagraph(r))
		b.WriteString("\n\n")
	}
	return b.Bytes()[:n]
}

// stressHTML returns an HTML page of paragraphs of n bytes at most.
func stressHTML(n int) []byte {
	const head, tail = "<!doctype html><html><body>\n", "</body></html>\n"
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a fixed seed, for the same bytes each build
	var b bytes.Buffer
	b.WriteString(head)
	for {
		p := "<p>" + stressParagraph(r) + "</p>\n"
		if b.Len()+len(p)+len(tail) > n {
			break
		}
		b.WriteString(p)
	}
	b.WriteString(tail)
	return b.Bytes()
}

// writeStressPictures writes an HTML page showing n distinct pictures of
// 12 megapixels, each a page wide, and the pictures beside it.
func writeStressPictures(dir, kind string, n int) error {
	var page strings.Builder
	page.WriteString("<!doctype html><html><body>\n")
	img := image.NewRGBA(image.Rect(0, 0, stressPhotoWidth, stressPhotoHeight))
	for i := range n {
		name := fmt.Sprintf("photo-%02d.%s", i, kind)
		fmt.Fprintf(&page, "<p><img src=%q style=\"width:100%%\"></p>\n", name)
		stressPhoto(img, i)
		var buf bytes.Buffer
		var err error
		if kind == "png" {
			err = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img)
		} else {
			err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil { //nolint:gosec // testdata is world-readable
			return err
		}
	}
	page.WriteString("</body></html>\n")
	return os.WriteFile(filepath.Join(dir, "index.html"), []byte(page.String()), 0o644) //nolint:gosec // testdata is world-readable
}

// stressPhoto paints img with gradients that differ for each index i, so
// every picture of a page is distinct.
func stressPhoto(img *image.RGBA, i int) {
	b := img.Bounds()
	for y := range b.Dy() {
		for x := range b.Dx() {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 255 / b.Dx()), G: uint8(y * 255 / b.Dy()), B: uint8((x + y + 37*i) % 256), A: 255}) //nolint:gosec // each value is reduced below 256
		}
	}
}

// stressCSV returns a CSV of cells fields, numbers and short words,
// [stressCellsPerRow] a row.
func stressCSV(cells int) []byte {
	r := rand.New(rand.NewPCG(3, 4)) //nolint:gosec // a fixed seed, for the same bytes each build
	var b bytes.Buffer
	for i := range cells {
		c := i % stressCellsPerRow
		if c > 0 {
			b.WriteByte(',')
		}
		if c%2 == 0 {
			fmt.Fprintf(&b, "%d", r.IntN(1_000_000))
		} else {
			fmt.Fprintf(&b, "w%d", r.IntN(1000))
		}
		if c == stressCellsPerRow-1 {
			b.WriteByte('\n')
		}
	}
	return b.Bytes()
}

// stressDocx returns a WordprocessingML document of pages pages, each a
// paragraph of text ending in a page break.
func stressDocx(pages int) ([]byte, error) {
	r := rand.New(rand.NewPCG(5, 6)) //nolint:gosec // a fixed seed, for the same bytes each build
	var body strings.Builder
	for i := range pages {
		fmt.Fprintf(&body, `<w:p><w:r><w:t>Page %d. %s</w:t></w:r></w:p>`, i+1, stressParagraph(r))
		if i < pages-1 {
			body.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
		}
	}
	return zipPackage(map[string][]byte{
		contentTypesPart: []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`),
		"_rels/.rels": []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`),
		"word/document.xml": []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() +
			`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>`),
	})
}
