package remediation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// matchUnknownOps matches the content-stream violations that report an
// operator not defined in ISO 32000: ISO 19005-2/3/4, 6.2.2 t1 and ISO
// 19005-1, 6.2.10 t1. These are dead bytes at the viewer level, so stripping
// them is content-preserving.
//
// The part-1 spelling is matched by the rule's Object, as [matchAggressive]
// matches it, because clause strings are reused across parts. Claiming 6.2.2
// alone left every part-1 finding to the aggressive pass, which reads page
// content only, runs only when content may change, and never sees a number
// written with an exponent, which the content parser reads as a number.
//
// It also matches an implementation-limit violation (ISO 19005-1, 6.1.12;
// ISO 19005-2, 6.1.13) whose number is the largest value veraPDF gives a
// number it cannot read ([compliance.SaturatedNumberNote]). The content parse
// reads that value in a run that spells no number of ISO 32000-1, 7.3.3, such
// as "-" or ".020.594", and the repair that answers it is the one here, which
// respells the run as the number it begins with, 0 when it holds no digit
// ([repairMalformedNumbers], and [rewriteNestedMalformedNumbers] where an
// array or a dictionary holds the run). Clamping it to the limit, as the
// content-limits repair clamps a number past it ([fixContentLimits]), would
// write a value no viewer drew.
//
// # Corpus evidence
//
//   - All corpora: at PDF/A-1b the part-1 claim clears 6.2.10 t1 for litho and
//     veraPDF on 13 default cells, among them pdfjs bug1130815.pdf with 1674
//     undefined operators, and on 3 aggressive cells whose operator sits in a
//     form XObject the aggressive pass never reads.
//   - pdfjs poppler-90-0-fuzzed.pdf: at PDF/A-1b default veraPDF stops
//     reporting 6.2.10 t1, and litho's count drops from 1617 to 1571.
//   - pdfjs issue15604.pdf, issue9252.pdf and bug1753983.pdf: veraPDF failed
//     the output of their PDF/A-1b and PDF/A-2b remediations, default and
//     aggressive, on the limit a run such as "-." or "-" breaks, and passes it
//     now that the claim reaches the repair.
func matchUnknownOps(v compliance.Violation) bool {
	if v.Rule.Object == "Op_Undefined" {
		return true
	}
	if clausePrefix(v, "6.1.12", "6.1.13") && contextContains(v, compliance.SaturatedNumberNote) {
		return true
	}
	if !clausePrefix(v, "6.2.2") {
		return false
	}
	return descriptionContains(v, "undefined operator") ||
		descriptionContains(v, "not defined in ISO") ||
		descriptionContains(v, "unknown operator") ||
		descriptionContains(v, "operator \"")
}

// unknownOpsTally counts what [fixUnknownOps] changed.
type unknownOpsTally struct {
	// removed counts the undefined operators ([content.IsUndefined]) that were
	// stripped.
	removed int
	// repaired counts malformed numbers folded back into their operator or
	// respelled where an array or a dictionary holds them.
	repaired int
	// exponents counts numbers written with an exponent that were rewritten
	// as the number a viewer reads in them.
	exponents int
	// nonFinite counts numbers written with no finite value, such as "inf",
	// that were rewritten as 0.
	nonFinite int
	// separated counts operators that ran on from the operator before them
	// with no white space between them, and were written back apart.
	separated int
	// delimited counts operators written against a delimiter veraPDF reads
	// into them, which were written back apart from it.
	delimited int
	// strays counts the operators veraPDF reads from a stray delimiter where
	// an operator stands, whose delimiters were removed.
	strays int
}

func (t *unknownOpsTally) add(o unknownOpsTally) {
	t.removed += o.removed
	t.repaired += o.repaired
	t.exponents += o.exponents
	t.nonFinite += o.nonFinite
	t.separated += o.separated
	t.delimited += o.delimited
	t.strays += o.strays
}

// fixUnknownOps strips undefined operators ([content.IsUndefined]) from every
// content stream a page draws, which [walkContentStreams] reaches: the page's
// own content, and the form XObjects, tiling patterns, Type 3 glyphs and
// annotation appearances it draws. Viewers ignore these tokens per ISO 32000
// §A.2, so removing a genuine one has no visible effect and only eliminates
// the PDF/A violation.
//
// That premise holds for a real operator alone, so each stream is repaired
// first by [repairContentData]: a number written with an exponent, which
// veraPDF reads as a number and an undefined operator, is rewritten as the
// number PDF.js reads in it, a number written with no finite value becomes 0,
// and a numeric literal the parser could not read surfaces as an operator whose
// removal would take the operands of the operator that follows. See
// [rewriteExponentNumbers], [rewriteNonFiniteNumbers] and the worked Tm example
// on [repairMalformedNumbers].
//
// The walk is the one the hex-string repair takes, so the streams repaired are
// the ones compliance judges the undefined-operator tests over (ISO 19005-1,
// 6.2.10 t1; ISO 19005-2/3/4, 6.2.2 t1). This fixer used to read page content,
// the forms page resources name and appearance streams alone, so an undefined
// operator in a Type 3 glyph or a tiling pattern stayed a residual after every
// remediation.
//
// # Corpus evidence
//
//   - All corpora: run on every file, whatever it reports, the wider walk
//     changes what the repair writes in 3 files.
//   - pdfjs issue18122.pdf: a tiling pattern whose data runs past a
//     misspelled "endsteam" holds 2 undefined operators, which litho and
//     veraPDF no longer report after a PDF/A-1b, PDF/A-2b or PDF/A-4
//     remediation, default or aggressive.
//   - pdfjs issue5039.pdf: the 4 undefined operators of a Type 3 glyph go for
//     both validators, see [rewriteNonFiniteNumbers].
//   - pdfjs poppler-90-0-fuzzed.pdf: 87 fuzzed Type 3 glyphs are rewritten,
//     which neither validator judges for their operators. The findings of the
//     outputs do not change but for the abbreviated filter names those glyphs
//     carried (ISO 19005-2, 6.1.7.2 t1; ISO 19005-4, 6.1.6.2 t1), 104 to 69
//     for litho and veraPDF at PDF/A-2b and PDF/A-4, and the text and images
//     do not change.
func fixUnknownOps(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	part := compliance.PDFAPart(profile)
	var total unknownOpsTally
	if _, err := walkContentStreams(ctx, alloc, doc, "unknown-ops", func(ctx context.Context, data []byte) ([]byte, func()) {
		newData, t := repairContentData(ctx, data, part)
		if t == (unknownOpsTally{}) {
			return nil, nil
		}
		return newData, func() { total.add(t) }
	}); err != nil {
		return nil, err
	}

	var fixes []string
	if total.removed > 0 {
		fixes = append(fixes, fmt.Sprintf("removed %d unknown content-stream operator(s)", total.removed))
	}
	// Reported separately: a repair rewrites the stream without removing an
	// operator, so folding it into the removal count would say an operator went
	// that did not, and saying nothing would rewrite page content silently.
	if total.repaired > 0 {
		fixes = append(fixes, fmt.Sprintf("repaired %d malformed number(s) in content streams", total.repaired))
	}
	if total.exponents > 0 {
		fixes = append(fixes, fmt.Sprintf("rewrote %d number(s) written with an exponent in content streams", total.exponents))
	}
	if total.nonFinite > 0 {
		fixes = append(fixes, fmt.Sprintf("rewrote %d number(s) written with no finite value in content streams as 0", total.nonFinite))
	}
	if total.separated > 0 {
		fixes = append(fixes, fmt.Sprintf("separated %d content-stream operator(s) written with no white space before them", total.separated))
	}
	if total.delimited > 0 {
		fixes = append(fixes, fmt.Sprintf("separated %d content-stream operator(s) from the delimiter written against them", total.delimited))
	}
	if total.strays > 0 {
		fixes = append(fixes, fmt.Sprintf("removed the stray delimiters of %d undefined content-stream operator(s)", total.strays))
	}
	return fixes, nil
}

// repairContentData repairs one content stream's decoded bytes: numbers
// written with an exponent become the numbers a viewer reads in them
// ([rewriteExponentNumbers]), numbers written with no finite value become 0
// ([rewriteNonFiniteNumbers]), malformed numbers in an array or a dictionary
// become the numbers they begin with ([rewriteNestedMalformedNumbers]) and the
// others fold back into their operator ([repairMalformedNumbers]), operators
// run together with no white space between them come apart
// ([separateGluedOperators]), and undefined operators ([content.IsUndefined])
// go.
// It returns the bytes to write and what it changed; a zero tally means
// nothing is to be written. The bytes can be empty: a stream holding nothing
// but undefined operators is written back with none.
//
// The stream is read as a repair reads it ([content.ForRepair]): a run that
// starts with a digit keeps its boundaries, and one that spells no number,
// such as ".020.594" or "-", is the keyword [repairMalformedNumbers] respells.
//
// A stream whose only defects are exponents, numbers with no finite value and
// malformed numbers in an array or a dictionary is written back as spliced, not
// re-serialized, so nothing else in it moves.
// Removing an operator or folding a number re-serializes the parsed
// operations, which cannot round-trip an inline image: the parser consumes its
// data off-token, so the BI operation keeps its dictionary alone, and writing
// it back leaves "<< ... >> BI" with no ID, data or EI, a content stream no
// reader can parse (ISO 32000-1, 8.9.7). A stream holding one has the repairs
// spliced in place of the operations they change instead, and its images
// carried across byte for byte ([spliceRepairedOperations]). It used to keep
// its operators and malformed numbers as a reported residual, as
// [fixContentStreams] keeps them, and still does where the splice cannot
// stand. So does a stream holding a name with a NUL byte, which
// [serializeOps] has no conforming spelling for.
//
// # Corpus evidence
//
//   - All corpora: 132 files draw inline images, and one holds a stream the
//     repair changes beside one: pdfjs poppler-90-0-fuzzed.pdf, a fuzzed Type
//     3 glyph that writes a garbled operator and "-5cm" ahead of its image.
//     Its PDF/A-1b, PDF/A-2b and PDF/A-4 remediations, default and
//     aggressive, now remove both, which neither validator judges in that
//     glyph: the findings of litho and veraPDF 1.30, the extracted text, the
//     images and the bytes of its 36 inline images do not change.
func repairContentData(ctx context.Context, data []byte, part int) ([]byte, unknownOpsTally) {
	var t unknownOpsTally
	data, t.exponents = rewriteExponentNumbers(ctx, data, part)
	data, t.nonFinite = rewriteNonFiniteNumbers(ctx, data)
	data, nested := rewriteNestedMalformedNumbers(ctx, data, part)
	spliced := func() ([]byte, unknownOpsTally) {
		if t.exponents == 0 && t.nonFinite == 0 && nested == 0 {
			return nil, unknownOpsTally{}
		}
		return data, unknownOpsTally{exponents: t.exponents, nonFinite: t.nonFinite, repaired: nested}
	}

	// The operations are read from the bytes with every run veraPDF reads as
	// numbers inside an array or a dictionary written as those numbers, which
	// the reading of a repair would otherwise drop when they are written back.
	// The runs stay as written in a stream that is only spliced.
	parsed, split := splitNestedNumberRuns(ctx, data)
	ops, err := content.ParseStreamData(ctx, parsed, content.ForRepair())
	if err != nil {
		return spliced()
	}
	ops, folded := repairMalformedNumbers(ops)
	t.repaired = folded + nested + split
	ops, t.separated, t.removed = dropUndefinedOperators(ops)
	t.delimited, t.strays = delimitedOperators(ctx, data)
	if t.removed == 0 && folded == 0 && t.separated == 0 && t.delimited == 0 && t.strays == 0 {
		return spliced()
	}
	if slices.ContainsFunc(ops, func(op content.Operation) bool { return op.Operator == "BI" }) {
		return spliceInlineImageStream(ctx, parsed, t, nested+split, spliced)
	}
	out, err := serializeOps(filterOps(ops))
	if err != nil {
		return spliced()
	}
	return out, t
}

// spliceInlineImageStream is the end of [repairContentData] for a stream that
// holds an inline image: the repairs are spliced in place of the operations
// they change ([spliceRepairedOperations]), and what they changed is counted
// from the splice and from what validation reads in the spliced stream. A
// splice that cannot stand leaves the stream to the token splices, spliced.
// runs counts the malformed numbers of arrays and dictionaries parsed already
// holds respelled.
func spliceInlineImageStream(ctx context.Context, parsed []byte, t unknownOpsTally, runs int, spliced func() ([]byte, unknownOpsTally)) ([]byte, unknownOpsTally) {
	out, s, ok := spliceRepairedOperations(ctx, parsed)
	if !ok {
		return spliced()
	}
	delimited, strays := delimitedOperators(ctx, out)
	t.repaired = s.folded + runs
	t.separated, t.removed = s.separated, s.removed
	t.delimited, t.strays = max(t.delimited-delimited, 0), max(t.strays-strays, 0)
	return out, t
}

// splitNestedNumberRuns returns data with every run where an array or a
// dictionary holds a number, outside an inline image's dictionary, that
// validation reads as several numbers ([veraPDFNumbers]), such as "3-5",
// written as those numbers apart, "3 -5", and how many it wrote. A run
// validation reads as the largest value veraPDF gives a number it cannot read
// is [rewriteNestedMalformedNumbers]'s.
//
// ISO 32000-1, 7.3.3 gives a number one sign at most, before its digits, so
// "3-5" spells none, and the reading of a repair ([content.ForRepair]) keeps
// it whole as a keyword, which an array cannot hold: the parse drops it, and a
// stream written back from that reading lost the element. veraPDF, whose
// reading ISO 19005 validation takes, ends a number before a sign
// (BaseParser.readNumber, veraPDF-parser 1.30), so it reads 3 then -5, which
// fail nothing. Written as those numbers, the run stays what validation read
// in the source. The runs are spliced in place, so every other byte of the
// stream is carried across unchanged.
//
// A stream that is only spliced keeps such a run as written: PDF.js reads
// "3-5" as 35 (Lexer.getNumber skips a sign inside a number), so no spelling
// keeps what both readers read, and the run fails no test to answer.
//
// # Corpus evidence
//
//   - pdfjs bug1130815.pdf: writes 13 such runs, "3-5", "3-8", "1-8" and
//     "11-8", in the TJ arrays of its damaged page stream. Its PDF/A-1b,
//     PDF/A-2b and PDF/A-4 remediations, default and aggressive, remove the
//     stream's undefined operators and write it back, which dropped each of
//     them. They keep them now, and the violations litho and veraPDF 1.30
//     report on the outputs, and their extracted text, do not change.
func splitNestedNumberRuns(ctx context.Context, data []byte) ([]byte, int) {
	splices, _ := repairTokenSplices(ctx, data, func(tok lexer.Token, at tokenPlace) (string, bool) {
		if tok.Type != lexer.TokenKeyword || at.depth == 0 || at.inlineImage || !malformedNumber(tok.Value) || saturatedRun(ctx, tok.Value) {
			return "", false
		}
		return veraPDFNumbers(tok.Value)
	})
	if len(splices) == 0 {
		return data, 0
	}
	return applyTokenSplices(data, splices), len(splices)
}

// veraPDFNumbers returns the numbers veraPDF reads in the run tok, written
// apart as it spells them, "3 -5" for "3-5", and false unless it reads two
// numbers or more there and nothing else. The run is read as validation reads
// a content stream ([lexer.Lexer.WithVeraPDFTokens]).
func veraPDFNumbers(tok string) (string, bool) {
	lex := lexer.NewLexer(ioat.NewReader(strings.NewReader(tok), int64(len(tok)), 0)).WithVeraPDFTokens()
	var nums []string
	for {
		t, err := lex.NextToken()
		if err != nil || t.Type == lexer.TokenError {
			return "", false
		}
		if t.Type == lexer.TokenEOF {
			break
		}
		if t.Type != lexer.TokenNumber {
			return "", false
		}
		nums = append(nums, t.Value)
	}
	if len(nums) < 2 {
		return "", false
	}
	return strings.Join(nums, " "), true
}

// rewriteNestedMalformedNumbers returns data with every run where an array or
// a dictionary holds a number, an inline image's dictionary among them, that
// validation reads as the largest value veraPDF gives a number it cannot read
// ([saturatedRun]), replaced by the number [repairMalformedNumbers] salvages
// from such a run where an operand stands: its longest numeric prefix, 0 when
// it holds no digit, written inside the part's limits by [plainNumber]. It
// returns how many it replaced.
//
// ISO 32000-1, 7.3.3 gives "-" and "1.5.5" no number. Validation reads them as
// veraPDF does, as the largest 64-bit integer and the largest double, which
// fail the implementation limits of ISO 19005-1, 6.1.12 t1 and t2 and ISO
// 19005-2, 6.1.13 t1 and t2 in a dash array, a TJ array, a property list or an
// inline image's dictionary as they fail them where an operand stands (see
// [compliance.SaturatedNumberNote]). PDF.js reads "-" there as 0
// (Lexer.getNumber), the number the respelling writes. A run veraPDF reads as
// numbers inside the limits, such as "3-5", which it reads as 3 then -5, fails
// nothing and is left as written, save in a stream written back from its
// parsed operations ([splitNestedNumberRuns]).
//
// The reading of a repair ([content.ForRepair]) keeps such a run whole as a
// keyword, which an array or a dictionary cannot hold, and the parse drops it:
// "[- 2] 0 d" read as the dash array [2], and a stream written back from that
// reading for another repair changed its dash pattern. Respelled first, the run
// stays the element it was. The runs are spliced in place, so every other byte
// of the stream, inline image data included, is carried across unchanged. A
// stream the lexer cannot read to its end has the runs before the failure
// respelled and the rest left as written.
//
// # Corpus evidence
//
//   - pdfjs bug1354114.pdf and bug1953099.pdf: write a lone "-" in a TJ array,
//     which veraPDF 1.30 fails the integer limit on at PDF/A-1b and PDF/A-2b.
//     Their PDF/A-1b and PDF/A-2b remediations, default and aggressive, no
//     longer fail it for litho or veraPDF, and their extracted text does not
//     change. bug1953099.pdf passes both validators at both profiles.
//   - pdfjs bug1130815.pdf: its damaged page stream holds 14 such runs in its
//     arrays. 13 of them, such as "3-5", are numbers inside the limits to
//     veraPDF, which [splitNestedNumberRuns] keeps, and "1-", 1 then the
//     integer "-", becomes 1.
//   - All corpora: no other file holds such a run in an array or a dictionary
//     of a content stream.
func rewriteNestedMalformedNumbers(ctx context.Context, data []byte, part int) ([]byte, int) {
	splices, _ := repairTokenSplices(ctx, data, func(tok lexer.Token, at tokenPlace) (string, bool) {
		if tok.Type != lexer.TokenKeyword || at.depth == 0 || !malformedNumber(tok.Value) || !saturatedRun(ctx, tok.Value) {
			return "", false
		}
		return plainNumber(strconv.FormatFloat(numericPrefix(tok.Value), 'f', -1, 64), part)
	})
	if len(splices) == 0 {
		return data, 0
	}
	return applyTokenSplices(data, splices), len(splices)
}

// saturatedRun reports whether validation reads a number of the run tok as the
// largest integer or real, signed, veraPDF gives a number it cannot read
// ([compliance.SaturatedNumberNote]). The run is read as validation reads a
// content stream ([content.ParseStreamData]), where it may hold several
// numbers: "--5" is -9223372036854775807 then -5.
func saturatedRun(ctx context.Context, tok string) bool {
	ops, err := content.ParseStreamData(ctx, []byte(tok+" n"))
	if err != nil || len(ops) != 1 {
		return false
	}
	for _, operand := range ops[0].Operands {
		switch v := operand.(type) {
		case ast.PdfInt:
			if v == math.MaxInt64 || v == -math.MaxInt64 {
				return true
			}
		case ast.PdfReal:
			if math.Abs(float64(v)) == math.MaxFloat64 {
				return true
			}
		}
	}
	return false
}

// delimitedOperators counts the operators of data written against one of the
// delimiters ")", ">", "{", "}" and "%", which validation reads, as veraPDF
// does, as one operator ISO 32000-1 does not define ([content.ParseStreamData]),
// and whose part before the delimiter is an operator of Annex A: "re%" before
// a comment, or "Q)". It also counts, as strays, the operators validation reads
// from a stray ")", ">", "{" or "}" where an operator stands: ")", or ")Tj)".
//
// ISO 32000-1, 7.2.2, Table 2 makes those characters delimiters, so a viewer
// reads re and a comment, and the reading of a repair ([content.ForRepair])
// finds nothing to remove. Written back from that reading, the operator stands
// apart from the delimiter, the comment and a stray delimiter go, and veraPDF
// reads re. An operator whose part before the delimiter is no operator is not
// counted: the repair removes that part as it removes any undefined operator.
// A stray delimiter begins no object, so the reading of a repair drops it, a
// viewer draws nothing for it, and the stream written back from that reading
// holds none of it: ")Tj)" leaves the Tj a viewer reads.
//
// # Corpus evidence
//
//   - pdfjs calgray.pdf and calrgb.pdf: write "re" against a comment 60 and
//     1360 times, which veraPDF 1.30 reports under ISO 19005-1, 6.2.10 t1 and
//     ISO 19005-2, 6.2.2 t1, and which their PDF/A-1b and PDF/A-2b
//     remediations, default and aggressive, left in place. The outputs now
//     pass that test for veraPDF, and their extracted text is unchanged.
func delimitedOperators(ctx context.Context, data []byte) (delimited, strays int) {
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return 0, 0
	}
	for _, op := range ops {
		if content.IsOperator(op.Operator) {
			continue
		}
		switch i := strings.IndexAny(op.Operator, ")>{}%"); {
		case i == 0:
			strays++
		case i > 0 && content.IsOperator(op.Operator[:i]):
			delimited++
		}
	}
	return delimited, strays
}

// dropUndefinedOperators separates every run of operators written with no
// white space between them ([separateGluedOperators]), then blanks every
// undefined operator ([content.IsUndefined]) for [filterOps] to drop, BX and
// EX aside. It returns the operations, how many operators it separated, and
// how many it removed.
//
// An EI or an ID the reading of a repair returns stands outside every inline
// image, whose whole object it reads as one BI operation, so a viewer draws
// nothing for it and removing it changes no image.
//
// ISO 19005-2, 6.2.2 forbids an undefined operator even inside a BX/EX
// compatibility section, so one is removed wherever it stands. The unknown-ops
// fixer and the aggressive content rewrite both remove them through this, so a
// run a viewer reads as operators comes apart on either path rather than going
// whole on one of them.
func dropUndefinedOperators(ops []content.Operation) ([]content.Operation, int, int) {
	ops, separated := separateGluedOperators(ops)
	removed := 0
	for i, op := range ops {
		if op.Operator == "" || op.Operator == "BX" || op.Operator == "EX" {
			continue
		}
		if content.IsUndefined(op.Operator) {
			ops[i].Operator = ""
			removed++
		}
	}
	return ops, separated, removed
}

// separateGluedOperators replaces every operation whose operator is a run of
// operators written with no white space between them with one operation per
// operator, and returns the operations and how many operators it separated.
// The first operator takes the run's operands and the others take none, which
// is how a viewer that splits the run hands them out: no operand stands
// between them.
//
// ISO 32000-1, 7.2.2 reads "ETBT" as one keyword, and ISO 19005-2, 6.2.2
// forbids it as an operator Annex A does not define, but PDF.js reads ET then
// BT (see [content.SplitGluedOperators]), and so does litho's text
// extraction. Removing the run as an undefined operator removed the ET and BT
// a viewer draws with, and the text after them moved. Written back apart, the
// operators read the same under both readings. Only a run that splits into
// operators alone comes apart ([content.GluedOperators]): one that leaves
// anything else, such as "junk", which splits into j and the undefined unk,
// is removed whole as before.
//
// # Corpus evidence
//
//   - pdfjs sci-notation.pdf: writes "ETBT" between its two text objects.
//     Once text extraction read ET then BT, removing the run moved the second
//     line off the page, the PDF/A-2b content repairs were rolled back,
//     default and aggressive, and 6.2.2 t1 stayed in the output. The two
//     operators now come apart, the output passes that test for litho and
//     veraPDF, and both lines extract.
//   - pdfjs bug1130815.pdf: holds such runs, "ll" and "lm", in a damaged page
//     stream, and the violations its remediation leaves are unchanged.
//   - All corpora: no file's PDF/A-2b remediation leaves other violations
//     than before, default or aggressive, veraPDF files at their own profile.
func separateGluedOperators(ops []content.Operation) ([]content.Operation, int) {
	var out []content.Operation
	n := 0
	for i, op := range ops {
		var parts []string
		if op.Operator != "" && !content.IsOperator(op.Operator) {
			parts = content.GluedOperators(op.Operator)
		}
		if parts == nil {
			if out != nil {
				out = append(out, op)
			}
			continue
		}
		if out == nil {
			out = append(make([]content.Operation, 0, len(ops)+len(parts)), ops[:i]...)
		}
		out = append(out, content.Operation{Operator: parts[0], Operands: op.Operands})
		for _, part := range parts[1:] {
			out = append(out, content.Operation{Operator: part})
		}
		n += len(parts) - 1
	}
	if out == nil {
		return ops, 0
	}
	return out, n
}

// rewriteExponentNumbers returns data with every number written with an
// exponent, such as "1.5e1" or "1E-3", replaced by the number a viewer reads
// in it, the part before the exponent letter ([exponentMantissa], written by
// [plainNumber]), and how many it replaced. A stream holding no such number
// where an operand stands comes back unchanged with a count of 0.
//
// ISO 32000-1, 7.3.3 gives a number no exponent, so the token is no operand,
// and neither reference reader takes it as one. veraPDF ends the number at the
// exponent letter and the operator after it at the next digit: "1.5e1 w" is
// the number 1.5, an operator e that ISO 32000-1 does not define (ISO 19005-1,
// 6.2.10 t1; ISO 19005-2, 6.2.2 t1), and the number 1, which the w takes, as
// validation reads it ([content.ParseStreamData]). PDF.js ends the number
// at the letter too (Lexer.getNumber has no exponent), reads the rest as a
// command it does not know, and skips that command keeping the operands before
// it (EvaluatorPreprocessor.read), so the w it draws has the width 1.5, and
// "/F1 1e2 Tf" sets the font size 1.
//
// Remediation keeps what the viewer shows, so the token is rewritten as the
// number PDF.js reads, and the rest of it goes, as both readers treat it:
// PDF.js skips it, and veraPDF reports it as an undefined operator, whose
// removal the way other undefined operators are removed would take the number
// with it. Rewriting the token as the number it spells, 15 or 100, changed the
// page: the text after "/F1 1e2 Tf" drew at a hundred times the size PDF.js
// draws it at. The reading of a repair ([content.ForRepair]) keeps the token
// whole, reads "1.5e1" as 15 and cannot read "1e2" at all, a reading of its own
// this rewrite does not follow.
//
// Only a stream with a finding is rewritten: a number where an operand
// stands, outside any array or dictionary, which is the one validation reports,
// as the operator its exponent letter starts. The rest of such a stream's
// exponents go too, since veraPDF stops reading a stream at one inside an
// array or a dictionary; PDF.js reads such a number the same way there, the
// rest being a command an array or a dictionary drops. An inline image's
// dictionary is left as written, as it was when the token walk skipped the
// dictionary with the image's data.
//
// The rewrite splices the tokens in place, so the stream's other bytes, inline
// image data included, are carried across unchanged. A stream the lexer cannot
// read to its end is left alone.
//
// # Corpus evidence
//
//   - pdfjs sci-notation.pdf: the one corpus file with an exponent where an
//     operand stands, "/F1 1e2 Tf" after the stray keyword ETBT. Its PDF.js
//     text golden draws the second line at height 1 and width 6.5. The
//     rewrite to 100 drew it 594.6 wide, past the page edge, and both litho
//     and PDF.js lost its last character from the extracted text. The rewrite
//     writes "/F1 1 Tf": PDF.js reads the output of a PDF/A-1b, PDF/A-2b or
//     PDF/A-4 remediation, default or aggressive, exactly as its golden reads
//     the source, litho's text of the output matches that golden, and litho
//     and veraPDF report the same violations on it as before.
func rewriteExponentNumbers(ctx context.Context, data []byte, part int) ([]byte, int) {
	operands := 0
	splices, complete := contentTokenSplices(ctx, data, func(tok lexer.Token, at tokenPlace) (string, bool) {
		if tok.Type != lexer.TokenNumber || at.inlineImage || !strings.ContainsAny(tok.Value, "eE") {
			return "", false
		}
		text, ok := plainNumber(exponentMantissa(tok.Value), part)
		if ok && at.depth == 0 {
			operands++
		}
		return text, ok
	})
	if !complete || operands == 0 {
		return data, 0
	}
	return applyTokenSplices(data, splices), len(splices)
}

// exponentMantissa returns the part of a number token written with an
// exponent that stands before the exponent letter: "1.5" of "1.5e1" and "-1"
// of "-1E-3". It is the number PDF.js and veraPDF read in the token, both
// ending a number at the letter; see [rewriteExponentNumbers]. A token with no
// exponent letter comes back whole.
func exponentMantissa(tok string) string {
	if i := strings.IndexAny(tok, "eE"); i >= 0 {
		return tok[:i]
	}
	return tok
}

// rewriteNonFiniteNumbers returns data with every token that spells a number
// with no finite value where an operand stands ([nonFiniteNumber]), such as
// "inf" or "-inf", replaced by 0, and how many it replaced.
//
// ISO 32000-1, 7.3.3 writes a number as digits with an optional sign and
// decimal point, so these tokens are keywords, and ISO 19005-1, 6.2.10 t1 and
// ISO 19005-2/3/4, 6.2.2 t1 forbid them as operators Annex A does not define.
// veraPDF reports each one so and reads the operands before it as its own. A
// producer writes one where its formatter printed a floating-point value it
// could not bound: a Type 3 glyph with no marks, whose bounding box was
// accumulated from no point, reads "602 0" then the box (inf, inf, -inf, -inf)
// ahead of its d1.
//
// Each token is taken for the number it stands for, as
// [repairMalformedNumbers] takes a numeral that lost its digits, and written
// 0, a value inside every part's limits, so the operator after it keeps the
// operands the producer wrote for it. The other repairs at hand change what a
// viewer shows:
//
//   - Removing the token as an undefined operator takes the operands before it
//     too, so the d1 above keeps none of the six Table 113 requires, the glyph
//     width among them.
//   - [separateGluedOperators] reads "inf" as PDF.js does, i then n then f, and
//     writes three operators no producer meant ahead of the d1 a glyph starts
//     with (ISO 32000-1, 9.6.5).
//   - The part's largest real M, which [plainNumber] writes for a magnitude
//     past it, turns the empty box into (M, M, -M, -M), which a reader
//     normalises into a box covering the plane (ISO 32000-1, 7.9.5), and PDF.js
//     widens the font's bounding box to it.
//
// PDF.js reads the zero box as bogus bounds and skips the d1
// (TranslatedFont's Type 3 colour-operator pass), and draws the glyph as it
// drew the source's: with nothing, since it cannot read "-inf" as a number
// (Lexer.getNumber) and drops a Type 3 glyph at its first error
// (TranslatedFont.loadType3Data).
//
// Only a token outside any array or dictionary is rewritten, the one the
// undefined-operator tests report. The tokens are spliced in place, so every
// other byte of the stream, inline image data included, is carried across
// unchanged. A stream the lexer cannot read to its end has the tokens before
// the failure rewritten and the rest left as written.
//
// # Corpus evidence
//
//   - pdfjs issue5039.pdf: the space glyph of its Type 3 font is the glyph
//     above, and no other file holds such a token where the repair reads. Its
//     PDF/A-1b, PDF/A-2b and PDF/A-4 remediations, default or aggressive, no
//     longer leave the 4 undefined operators for litho or veraPDF. veraPDF's
//     glyph width finding on the glyph goes too (ISO 19005-1, 6.3.6 t1; ISO
//     19005-2, 6.2.11.5 t1; ISO 19005-4, 6.2.10.5 t1), since the d1 keeps the
//     width /Widths gives, and the text and images do not change.
func rewriteNonFiniteNumbers(ctx context.Context, data []byte) ([]byte, int) {
	splices, _ := contentTokenSplices(ctx, data, func(tok lexer.Token, at tokenPlace) (string, bool) {
		if tok.Type != lexer.TokenKeyword || at.depth != 0 || !nonFiniteNumber(tok.Value) {
			return "", false
		}
		return "0", true
	})
	if len(splices) == 0 {
		return data, 0
	}
	return applyTokenSplices(data, splices), len(splices)
}

// nonFiniteNumber reports whether tok spells a floating-point value with no
// finite magnitude the way a formatter prints one: "inf", "infinity" or "nan"
// in any case, after at most one sign, as C's printf ("inf", "-nan"), Java and
// JavaScript ("Infinity", "NaN") write them.
func nonFiniteNumber(tok string) bool {
	if len(tok) > 0 && (tok[0] == '+' || tok[0] == '-') {
		tok = tok[1:]
	}
	return strings.EqualFold(tok, "inf") || strings.EqualFold(tok, "infinity") || strings.EqualFold(tok, "nan")
}

// plainNumber writes the number tok spells the way ISO 32000-1, 7.3.3 spells
// a number, inside the limits of the PDF/A part: an integer when it is whole
// and within the integer range, and a real with a decimal point otherwise. ok
// is false when tok is not a number.
//
// A magnitude past the part's largest real becomes that real (ISO 19005-1,
// 6.1.12 t2; ISO 19005-2, 6.1.13 t2). Part 4 states no limit and takes the
// ISO 32000-1, Table C.1 one, which is also where a float64 overflow, a run
// of over 308 digits, lands. A nonzero magnitude closer to 0 than the smallest
// normal real of that table becomes 0, the only value ISO 19005-2, 6.1.13 t5
// accepts there; parts 1 and 4 state no such floor and take the same one,
// since no operand draws differently at that magnitude. A whole number past
// the integer range keeps its decimal point, since read back as an integer it
// would break ISO 19005-2, 6.1.13 t1.
func plainNumber(tok string, part int) (string, bool) {
	v, err := strconv.ParseFloat(tok, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return "", false
	}
	maxAbs := compliance.MaxRealAbs2
	if part == 1 {
		maxAbs = compliance.MaxRealAbs1
	}
	switch {
	case v > maxAbs:
		v = maxAbs
	case v < -maxAbs:
		v = -maxAbs
	case math.Abs(v) < compliance.MinRealAbs2:
		v = 0
	}
	if v == math.Trunc(v) && math.Abs(v) <= compliance.MaxIntegerValue {
		return strconv.FormatInt(int64(v), 10), true
	}
	s := writer.FormatReal(v)
	if !strings.ContainsRune(s, '.') {
		s += ".0"
	}
	return s, true
}

// repairMalformedNumbers folds tokens the content parser surfaced as operators,
// but which are really numeric literals that lost their digits, back into the
// operand list of the operator that follows. Returns how many were folded.
//
// ISO 32000-1, 7.3.3 writes a real as an optional sign, digits and an optional
// decimal point, so "-." is not a number and the parser cannot treat it as one.
// It becomes an operator, which splits the operand run around it, and stripping
// it as an undefined operator (ISO 19005-2, 6.2.2) takes that op's operands with
// it. The text matrix
//
//	-. -.002 .002 -. 59.64 760.026 Tm
//
// came out as "59.64 760.026 Tm": Tm needs six operands (Table 108) and had two,
// so no text matrix was ever set and the page's text stopped extracting. The
// premise that stripping an undefined operator is content-preserving holds only
// for a real operator; a mangled operand is not dead bytes, it is part of the
// next operator's arguments.
//
// The salvaged value is the token's longest valid numeric prefix, or zero when
// it has none: "-." is negative zero, which is the value a tolerant reader
// already shows, and keeping the operand count right matters more than the
// digit no producer wrote.
//
// # Corpus evidence
//
//   - pdfjs issue15604.pdf: carries that text matrix, and every one of the
//     page's 1027 characters stopped extracting.
func repairMalformedNumbers(ops []content.Operation) ([]content.Operation, int) {
	var pending []ast.PdfObject
	out := make([]content.Operation, 0, len(ops))
	repaired := 0

	for _, op := range ops {
		if malformedNumber(op.Operator) {
			pending = append(pending, op.Operands...)
			pending = append(pending, ast.PdfReal(numericPrefix(op.Operator)))
			repaired++
			continue
		}
		if len(pending) > 0 {
			merged := make([]ast.PdfObject, 0, len(pending)+len(op.Operands))
			merged = append(merged, pending...)
			merged = append(merged, op.Operands...)
			op.Operands = merged
			pending = nil
		}
		out = append(out, op)
	}
	// A trailing run with no operator to attach to invokes nothing, so it is
	// dropped rather than emitted as bare operands.
	return out, repaired
}

// malformedNumber reports whether tok is built only from the characters a
// numeric literal uses, which means the parser surfaced it as an operator only
// because it is not a well-formed number (ISO 32000-1, 7.3.3). A token made of
// digits alone would have parsed as a number and never reached here.
func malformedNumber(tok string) bool {
	if tok == "" {
		return false
	}
	for _, r := range tok {
		if (r < '0' || r > '9') && r != '+' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

// numericPrefix returns the value of the longest leading run of tok that parses
// as a PDF number, or zero when there is none.
func numericPrefix(tok string) float64 {
	end, seenDigit, seenDot := 0, false, false
	for i, r := range tok {
		switch {
		case r == '+' || r == '-':
			if i != 0 {
				return prefixValue(tok, end, seenDigit)
			}
		case r == '.':
			if seenDot {
				return prefixValue(tok, end, seenDigit)
			}
			seenDot = true
		case r >= '0' && r <= '9':
			seenDigit = true
		default:
			return prefixValue(tok, end, seenDigit)
		}
		end = i + 1
	}
	return prefixValue(tok, end, seenDigit)
}

// prefixValue parses tok[:end], treating a run with no digit as zero.
func prefixValue(tok string, end int, seenDigit bool) float64 {
	if !seenDigit {
		return 0
	}
	v, err := strconv.ParseFloat(tok[:end], 64)
	if err != nil {
		return 0
	}
	return v
}
