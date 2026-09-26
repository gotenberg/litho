package content

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// inlineImageCheckInterval is how many bytes of inline image data
// [inlineImages.scan] reads between two checks of its context.
const inlineImageCheckInterval = 64 * 1024

// inlineImages reads the data of the inline images of one content stream as
// veraPDF reads it (PDFStreamParser.readInlineImage and checkInlineImage,
// veraPDF-parser 1.30), for a parse that reads the stream as veraPDF does.
//
// ISO 32000-1, 8.9.7 gives the data no length, so a reader has to find the EI
// that ends it. veraPDF takes the first E and I, whatever precedes them, that
// white space follows once the data has run for its /L or /Length bytes (ISO
// 32000-2, 8.9.7, Table 91), and whose next token, read past white space and
// comments, is no keyword outside ISO 32000-1, Annex A, Table A.1: such a
// keyword makes the EI part of the data. When that token is BI it reads the
// whole inline image that follows as part of the check, and an ID the data
// after it: that image is never an operation of its own, so no rule judges
// it, and its own EI is checked the same way. It then moves back by the bytes
// it read of the last image so read, less the bytes it had read of the first
// one, so reading resumes at the data of the last image, offset by the length
// of the data of the first: past its EI when the two lengths match, inside
// its data when the first is shorter, where what is left of it reads as
// operators, its EI among them ([IsUndefined]), and past what follows it when
// the first is longer.
//
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on one-page fixtures,
// and read the same way here:
//
//   - Two or three LZW inline images in a row fail the filter test, ISO
//     19005-1, 6.1.10 t2 and ISO 19005-2, 6.1.10 t1, once, and a DeviceRGB,
//     or interpolated, image after a DeviceGray one fails no DeviceRGB, or
//     interpolation, test, whether white space, one comment or several stand
//     between them, in a page's content or a form XObject's. An operator
//     between them makes both count.
//   - A first image 3 bytes shorter than the second leaves an EI that fails
//     the undefined-operator test, and one 9 bytes longer hides 9 bytes of the
//     operators after the second.
//   - An EI followed by a comment and then a keyword no operator spells is
//     part of the data, as is one followed by "zz", "n0", "B0", "Q%c", ")x" or
//     a BI whose dictionary ends in no ID, while one followed by "EI" ends it.
//     An EI read before the /L bytes is part of the data, and data with no EI
//     runs to the end of the stream.
//
// veraPDF moves back within a buffer of 2048 bytes (ASBufferedInFilter), which
// this reading does not model: a check that reads far past its EI, or a last
// image read with a check whose data is far longer, or shorter, than the
// first's, can move it back past what the buffer holds, and veraPDF then
// reads no operator of the stream at all. Measured as above, two images in a
// row 3000 bytes apart in length lose every finding of their stream, the
// colour of a fill before them included, while a comment of 1200 bytes after
// an EI loses nothing.
//
// The token after an EI is read once for each EI that white space follows,
// and two guards keep the reading of a stream linear in its length. A run of
// comments and white space that one check has skipped is not skipped again:
// a check that meets a comment inside it meets the same token past it, whose
// answer it takes. Stopping the check at the comment would keep it short
// too, but would end the data at an EI before a comment and a keyword that no
// operator spells, where veraPDF reads on. And the dictionaries the checks
// parse take at most the length of the stream in all, an anti-DoS guard, not
// spec-mandated: a dictionary is parsed once for each EI that white space and
// a BI follow, which only a crafted stream does in its image data, and past
// the bound such an EI is part of the data.
//
// # Corpus evidence
//
//   - All corpora: no content stream holds an inline image veraPDF reads with
//     the check of the EI before it, over 6915 streams holding BI and ID.
//   - All corpora: ending the data where veraPDF does changes what compliance
//     reports on 3 files at PDF/A-1b and PDF/A-2b, veraPDF files at their own
//     profile, each toward veraPDF's findings.
//   - govdocs thread_200_200176.pdf: an image on page 16 ends at an EI inside
//     its data, and the 10 undefined operators veraPDF reports after it, the
//     last the EI litho took for its end, were read as data.
//   - pdfjs bug1077808.pdf: the 2 undefined operators veraPDF reports were
//     read as image data.
//   - pdfjs issue16454.pdf: images on both pages end at an EI inside their
//     data. Page 1 reads the undefined operator veraPDF reports after one and
//     loses the fonts shown later, which veraPDF does not judge either, and
//     page 2, for which veraPDF reports nothing, loses its colour findings.
type inlineImages struct {
	data []byte
	// budget is what is left of the bytes [inlineImages.header] may read.
	budget int
	// skipFrom and skipTo bound the run of comments and white space the last
	// check that met a comment skipped, from that comment to the token past
	// the run, and skipped is that check's answer.
	skipFrom, skipTo int
	skipped          eiCheck
	// lastLength is the data length the dictionary of the last image read
	// states, which an ID that no BI begins reads its data with.
	lastLength dataLength
}

// dataLength is the length the /L or /Length entry of an inline image
// dictionary states, set only when it states one.
type dataLength struct {
	n   int64
	set bool
}

// eiCheck is the answer of the check an EI undergoes.
type eiCheck struct {
	// ends reports that the EI ends the data.
	ends bool
	// next is the image the check read whole, or nil.
	next *nextImage
}

// nextImage is an inline image the check of an EI read whole.
type nextImage struct {
	// start is the offset of its data.
	start int
	// length is the data length its dictionary states. An ID no BI begins
	// has none of its own and reads its data with the length of the image
	// whose EI was checked, which inherits reports.
	length   dataLength
	inherits bool
}

// newInlineImages returns a reader of the inline images of data.
func newInlineImages(data []byte) *inlineImages {
	return &inlineImages{data: data, budget: len(data)}
}

// resume returns the offset a parse reading the stream as veraPDF does
// resumes at after the inline image whose ID keyword ends at idEnd, read with
// the data length its dictionary dict states, or with the length of the last
// image read when dict is nil, for an ID that no BI begins.
func (r *inlineImages) resume(ctx context.Context, idEnd int, dict ast.PdfDict) int {
	length := r.lastLength
	if dict != nil {
		length = inlineDataLength(dict)
	}
	first := r.dataStart(idEnd)
	start, end, last := first, -1, -1
	for {
		e, next := r.scan(ctx, start, length)
		if end < 0 {
			end = e
		}
		if next == nil {
			break
		}
		start, last = next.start, next.start
		if !next.inherits {
			length = next.length
		}
	}
	r.lastLength = length
	if last < 0 {
		return end
	}
	// Resuming past the end of the stream reads nothing more.
	return min(last+end-first, len(r.data))
}

// repairEnd returns the offset a repair's reading of the stream resumes at
// after the inline image whose ID keyword ends at idEnd, with dict its
// dictionary: past the data as PDF.js ends it ([pdfjsInlineImageEnd]) or as
// veraPDF ends the data of that image ([inlineImages.scan]), whichever runs
// longer.
//
// A repair writes a stream back, and no byte either reader takes for image
// data may move, or it moves the pixels a viewer paints, or the image data
// ISO 19005 judges, ISO 32000-1, 8.9.7 giving the data no length that a
// reader could resynchronise on. Reading on from the later end keeps both.
// The operators between the two ends, which one reader reads, are written
// back as they are: veraPDF reads the images a check reads whole as data,
// which the repair reads as images of their own, and the bytes it reads as
// operators once it moves back into their data are data to PDF.js.
// Deliberate leniency toward both readers: a stream either reads as ISO
// 32000-1, 8.9.7 has it keeps the operators they both read.
//
// Both scans stop at the end they find, and the reading resumes at the later
// one, so no scan reads a byte another scan of the stream has read, but the
// one PDF.js makes to the end of a stream holding no EI it takes, past
// which it met none.
//
// # Corpus evidence
//
//   - All corpora: the reading of a repair moves on 64 content streams of 5
//     files, 63 of which it failed to read. Read as a remediation reads
//     them, the PDF/A-2b violations of 3 of those files move, each to what
//     veraPDF 1.30 reports: pdfjs
//     bug1513120_reduced.pdf, issue10388_reduced.pdf and issue17794.pdf.
//   - All corpora: the default and aggressive PDF/A-2b remediations of the 5
//     files write the same bytes as before.
//   - pdfjs issue17794.pdf: its PDF/UA-1 remediation tags the inline image
//     it reads, 1309 structure elements where it wrote 1308, and veraPDF
//     reports the same findings on the output.
func (r *inlineImages) repairEnd(ctx context.Context, idEnd int, dict ast.PdfDict) int {
	vera, _ := r.scan(ctx, r.dataStart(idEnd), inlineDataLength(dict))
	pdfjs, _ := pdfjsInlineImageEnd(ctx, r.data, idEnd, dict)
	return max(vera, pdfjs)
}

// InlineImageEnds finds where the data of the inline images of one content
// stream ends as a repair reads the stream ([ForRepair], and see
// [inlineImages.repairEnd]), for a walk of the stream that reads its tokens
// itself. It holds the state that keeps reading the images of the stream
// linear in its length, so one value serves the one stream it was made for.
// It is not safe for concurrent use.
type InlineImageEnds struct {
	images *inlineImages
}

// NewInlineImageEnds returns the finder of the ends of the inline images of
// data, a content stream's decoded bytes.
func NewInlineImageEnds(data []byte) *InlineImageEnds {
	return &InlineImageEnds{images: newInlineImages(data)}
}

// End returns the offset in the stream past the data of the inline image whose
// ID keyword ends at idEnd, with dict its dictionary, where a repair reads
// on.
func (e *InlineImageEnds) End(ctx context.Context, idEnd int, dict ast.PdfDict) int {
	return e.images.repairEnd(ctx, idEnd, dict)
}

// dataStart returns the offset of the data of an inline image whose ID
// keyword ends at idEnd: past the white-space byte that follows ID (ISO
// 32000-1, 8.9.7), when one does.
func (r *inlineImages) dataStart(idEnd int) int {
	if idEnd < len(r.data) && isVeraPDFSpace(r.data[idEnd]) {
		return idEnd + 1
	}
	return idEnd
}

// scan reads the data of an inline image that starts at start, with the data
// length its dictionary states, and returns the offset just past the EI that
// ends it, or the end of the stream when none does, and the image the check
// of that EI read whole, or nil.
func (r *inlineImages) scan(ctx context.Context, start int, length dataLength) (int, *nextImage) {
	data := r.data
	from := start
	if length.set {
		// An EI ends the data only once the bytes read, "EI" included, reach
		// the length.
		if length.n > int64(len(data)-start) {
			return len(data), nil
		}
		from = max(start, start+int(length.n)-2)
	}
	for j := from; j+2 < len(data); j++ {
		if (j-from)%inlineImageCheckInterval == 0 && ctx.Err() != nil {
			return len(data), nil
		}
		if data[j] != 'E' || data[j+1] != 'I' || !isVeraPDFSpace(data[j+2]) {
			continue
		}
		if c := r.check(ctx, j+2); c.ends {
			return j + 2, c.next
		}
	}
	return len(data), nil
}

// check answers whether the EI that ends at pos ends the data of its inline
// image, reading the token after it past white space and comments.
func (r *inlineImages) check(ctx context.Context, pos int) eiCheck {
	data := r.data
	q := pos
	for q < len(data) && isVeraPDFSpace(data[q]) {
		q++
	}
	if q == len(data) || data[q] != '%' {
		return r.token(ctx, q)
	}
	// A comment that starts inside the run the last check skipped ends at
	// the same end of line, so the rest of that run leads to the same token.
	if r.skipFrom <= q && q < r.skipTo {
		return r.skipped
	}
	from := q
	for q < len(data) {
		if isVeraPDFSpace(data[q]) {
			q++
			continue
		}
		if data[q] != '%' {
			break
		}
		for q < len(data) && data[q] != '\n' && data[q] != '\r' {
			q++
		}
	}
	c := r.token(ctx, q)
	r.skipFrom, r.skipTo, r.skipped = from, q, c
	return c
}

// token answers the check of an EI by the token that starts at q, as veraPDF
// reads it (PDFStreamParser.parseNextToken): an object, the end of the
// stream, or an operator of Annex A, Table A.1 ends the data, and a keyword
// outside the table does not.
func (r *inlineImages) token(ctx context.Context, q int) eiCheck {
	data := r.data
	if q >= len(data) {
		return eiCheck{ends: true}
	}
	switch c := data[q]; {
	case c == '(' || c == '<' || c == '[' || c == '/' || c == '.' || c == '+' || c == '-' || c >= '0' && c <= '9':
		return eiCheck{ends: true}
	case c == 'n' || c == 't' || c == 'f':
		switch w := r.word(q); w {
		case "null", "true", "false":
			return eiCheck{ends: true}
		default:
			return eiCheck{ends: IsOperator(w)}
		}
	case c == 'B':
		w := r.word(q)
		if w != "BI" {
			return eiCheck{ends: IsOperator(w)}
		}
		next := r.header(ctx, q+len(w))
		return eiCheck{ends: next != nil, next: next}
	case c == 'I':
		// veraPDF reads an I as the ID of an inline image, and reads the data
		// after it as it reads any other.
		if q+1 < len(data) && data[q+1] == 'D' {
			return eiCheck{ends: true, next: &nextImage{start: r.dataStart(q + 2), inherits: true}}
		}
		return eiCheck{}
	}
	w := r.operator(q)
	return eiCheck{ends: w == "" || IsOperator(w)}
}

// word returns the run of characters that starts at q up to white space or a
// delimiter (ISO 32000-1, 7.2.2), as veraPDF reads a keyword that may be
// null, true, false or BI.
func (r *inlineImages) word(q int) string {
	end := q
	for end < len(r.data) && !isVeraPDFSpace(r.data[end]) && !isDelimiter(r.data[end]) {
		end++
	}
	return string(r.data[q:end])
}

// operator returns the operator that starts at q as veraPDF reads one
// (PDFStreamParser.nextOperator): a run up to white space, "[", "]", "<", "(",
// "/" or a digit, save that d0 and d1 keep their digit (ISO 32000-1, 9.6.5,
// Table 113). A run of no characters is no token at all.
func (r *inlineImages) operator(q int) string {
	data := r.data
	end := q
	for end < len(data) {
		c := data[end]
		if isVeraPDFSpace(c) || c == '[' || c == ']' || c == '<' || c == '(' || c == '/' || c >= '0' && c <= '9' {
			break
		}
		end++
		if c == 'd' && end < len(data) && (data[end] == '0' || data[end] == '1') {
			end++
		}
	}
	return string(data[q:end])
}

// header reads the dictionary of the inline image whose BI keyword ends at
// q, up to the ID that ends it, as veraPDF reads it when it checks an EI,
// and returns the image, or nil when the dictionary holds anything but
// key/value pairs before an ID. The bytes it reads come out of the budget
// [inlineImages] describes, and a dictionary that would run past what is
// left of it reads as no image.
func (r *inlineImages) header(ctx context.Context, q int) *nextImage {
	if r.budget <= 0 {
		return nil
	}
	end := min(len(r.data), q+r.budget)
	p := newParser(r.data[q:end], readOptions{})
	dict, err := p.ParseInlineImageHeader(ctx)
	read := int(p.Position())
	r.budget -= max(read, 1)
	if err != nil || p.Abandoned() {
		return nil
	}
	return &nextImage{start: r.dataStart(q + read), length: inlineDataLength(dict)}
}

// inlineDataLength returns the data length an inline image dictionary states,
// by its /L entry, or by /Length when it has none, as veraPDF reads it
// (PDInlineImage.getInlineImageKey): an abbreviation the dictionary holds
// comes before the full key (ISO 32000-1, 8.9.7, Table 93).
func inlineDataLength(dict ast.PdfDict) dataLength {
	v, ok := dict["L"]
	if !ok {
		v = dict["Length"]
	}
	switch n := v.(type) {
	case ast.PdfInt:
		return dataLength{n: int64(n), set: true}
	case ast.PdfReal:
		return dataLength{n: int64(n), set: true}
	}
	return dataLength{}
}

// isVeraPDFSpace reports whether c is white space (ISO 32000-1, 7.2.2,
// Table 1).
func isVeraPDFSpace(c byte) bool {
	switch c {
	case 0, '\t', '\n', '\f', '\r', ' ':
		return true
	}
	return false
}

// isDelimiter reports whether c is a delimiter (ISO 32000-1, 7.2.2, Table 2).
func isDelimiter(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}
