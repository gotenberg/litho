package remediation

import (
	"context"
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// contentStreamContext is the phrase compliance's content-stream limit check
// names the stream holding a number with: "page N (stream content)" for the
// content of a page, and "object N (stream content)" for a stream it draws.
const contentStreamContext = "(stream content)"

// maxTranslationSplits caps the translations [splitTranslation] writes ahead
// of one cm operator. Anti-DoS guard, not spec-mandated: each one adds some 20
// bytes to the stream, and a translation past the cap, over 33 times the
// PDF/A-1 real limit, is clamped instead. The value covers every translation
// past the limit of a real document.
//
// # Corpus evidence
//
//   - All corpora: 7 files write a cm translation past the PDF/A-1 real
//     limit, 6064 of them, and the one taking the most splits takes 5, in
//     pdfjs issue19835.pdf.
const maxTranslationSplits = 32

// matchContentLimits matches the violations of an integer or real of a
// content stream past the implementation limits: ISO 19005-1, 6.1.12 t1 and
// t2, and ISO 19005-2/3, 6.1.13 t1, t2 and t5. A number compliance reads as
// the largest value veraPDF gives a number it cannot read is left to the
// undefined-operator repair ([matchUnknownOps]).
func matchContentLimits(v compliance.Violation) bool {
	switch v.Rule {
	case compliance.Rule_19005_1_6_1_12_t01, compliance.Rule_19005_1_6_1_12_t02,
		compliance.Rule_19005_2_6_1_13_t01, compliance.Rule_19005_2_6_1_13_t02, compliance.Rule_19005_2_6_1_13_t05,
		compliance.Rule_19005_3_6_1_13_t01, compliance.Rule_19005_3_6_1_13_t02, compliance.Rule_19005_3_6_1_13_t05:
	default:
		return false
	}
	return contextContains(v, contentStreamContext) && !contextContains(v, compliance.SaturatedNumberNote)
}

// contentLimitsTally counts what [clampContentNumbers] changed.
type contentLimitsTally struct {
	// clamped counts the numbers respelled inside the limits.
	clamped int
	// split counts the cm operators whose translation was written as
	// several translations inside the limits.
	split int
}

// fixContentLimits repairs the numbers past the implementation limits of every
// content stream a page draws, which [walkContentStreams] reaches: the page's
// own content, and the form XObjects, tiling patterns, Type 3 glyphs and
// annotation appearances it draws. See [clampContentNumbers].
//
// Clamping can shift a drawing coordinate, but the alternative is failing
// compliance; the spec caps the value regardless of intent. It is a
// content-tier repair, so the writer rolls it back when the output loses the
// text the source shows.
//
// # Corpus evidence
//
//   - All corpora: of the PDF/A-1b and PDF/A-2b remediations, default and
//     aggressive, of the 31 files that report a limit violation and hold a
//     number this repair or the one it replaces changes, veraPDF files at
//     their own profile, 40 change. Every one moves toward veraPDF: the
//     violations of the integer, real and denormal limits litho and veraPDF
//     find in the outputs go from 98396 to 0, 4 remediations that failed now
//     succeed, and every output that existed before keeps its extracted text.
//   - pdfjs 22060_A1_01_Plans.pdf: holds six reals past the PDF/A-1 limit in
//     tiling patterns /R21 and /R24, which the repair did not reach when it
//     read page content alone, and veraPDF failed each of them in the output
//     of its PDF/A-1b remediation.
//   - pdfjs tracemonkey.pdf and 10 files holding its content: hold 316 reals
//     past the PDF/A-1 limit in the streams their pages draw besides their
//     own content, and issue14724.pdf holds 45533, which their PDF/A-1b
//     remediations left for litho and veraPDF alike.
func fixContentLimits(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	part := compliance.PDFAPart(profile)
	var total contentLimitsTally
	if _, err := walkStreams(ctx, alloc, doc, "content-limits", func(ctx context.Context, data []byte, uncoloured bool) ([]byte, func()) {
		newData, t := clampContentNumbers(ctx, data, part, uncoloured)
		if t == (contentLimitsTally{}) {
			return nil, nil
		}
		return newData, func() {
			total.clamped += t.clamped
			total.split += t.split
		}
	}); err != nil {
		return nil, err
	}
	var fixes []string
	if total.clamped > 0 {
		fixes = append(fixes, fmt.Sprintf("clamped %d out-of-range number(s) in content streams", total.clamped))
	}
	if total.split > 0 {
		fixes = append(fixes, fmt.Sprintf("split the out-of-range translation of %d cm operator(s) in content streams", total.split))
	}
	return fixes, nil
}

// clampContentNumbers returns one content stream's decoded bytes with every
// number past the implementation limits of the part ([breaksNumberLimits])
// brought inside them, and what it changed.
//
// A cm operator whose translation breaks the PDF/A-1 real limit, with its
// other operands inside it, is written as translations inside the limit
// ahead of the cm, whose translation keeps the rest ([translationSplits]).
// The matrices compose to the one the producer wrote, so everything drawn
// after the cm stays where it was. Clamping the translation moved all of it,
// the text the page shows included.
//
// Any other number is respelled by [plainNumber]: a real past the largest
// real becomes that real, a denormal real becomes 0, and an integer past the
// integer range becomes the real of its value when the part admits it, the
// largest real otherwise.
//
// A number is respelled where compliance judges it ([judgedLimitBreakers]):
// an operand, an element of an array or a value of a dictionary, an inline
// image's dictionary included, that the operator taking it reads, as veraPDF
// reads it. The numbers are spliced in place ([contentTokenSplices]), so every
// other byte of the stream, inline image data included, is carried across
// unchanged. uncoloured reports the cell of an uncoloured tiling pattern,
// which starts with its colour operators ignored.
//
// The repair used to parse the page's own content, clamp its operands to the
// PDF/A-1 real limit and write the operations back. That left the numbers of
// arrays and dictionaries past the limits, and every stream a page draws
// besides its own content. It clamped an integer past 32767, which ISO
// 19005-1, 6.1.12 t1 admits up to 2147483647, and a real past 32767 under
// parts 2 and 3, which admit it up to 3.403e38. And it wrote an inline image
// back as its dictionary alone, with no ID, data or EI (ISO 32000-1, 8.9.7).
//
// A run that spells no number of ISO 32000-1, 7.3.3, such as "-", is left to
// the undefined-operator repair, which respells it as the number it begins
// with (see [matchUnknownOps]).
//
// # Corpus evidence
//
//   - pdfjs issue19835.pdf and pdfcpu pkg_testdata__read.go.pdf: clamping the
//     cm translations of their page content dropped the extracted text of
//     their PDF/A-1b remediation from 214888 to 35013 and from 78285 to 22868
//     bytes, and the remediation failed, default and aggressive. Split, their
//     5945 and 34 translations draw as written, and the outputs pass litho
//     and veraPDF 1.30.
//   - pdfjs issue19281.pdf: its forms place their drawings with 79 cm
//     translations past the limit. Clamped, the extracted text of its PDF/A-1b
//     remediation dropped from 2348730 to 759400 bytes, and the remediation
//     failed. Split, the text is unchanged, and the output no longer fails the
//     limit for litho or veraPDF, where it failed it 175 times when the repair
//     did not reach the forms.
func clampContentNumbers(ctx context.Context, data []byte, part int, uncoloured bool) ([]byte, contentLimitsTally) {
	splits, n := translationSplits(ctx, data, part)
	judged := judgedLimitBreakers(ctx, data, part, uncoloured)
	// Both walks offer the same tokens in the same order, so a token's
	// ordinal names it in both. The judged numbers are read as validation
	// reads them, and named by where they end.
	ordinal := -1
	splices, _ := contentTokenSplices(ctx, data, func(tok lexer.Token, at tokenPlace) (string, bool) {
		ordinal++
		if text, ok := splits[ordinal]; ok {
			return text, true
		}
		if tok.Type != lexer.TokenNumber || !breaksNumberLimits(tok.Value, part) || !judged[at.end] {
			return "", false
		}
		return plainNumber(tok.Value, part)
	})
	if len(splices) == 0 {
		return data, contentLimitsTally{}
	}
	// The splices of a split stand where an operand stands, which the walk
	// keeps whatever it reads, and the others are clamps.
	return applyTokenSplices(data, splices), contentLimitsTally{clamped: len(splices) - len(splits), split: n}
}

// limitBreaker is a number token [judgedLimitBreakers] holds until the
// operator that takes it: where it stands, and whether it is an integer.
type limitBreaker struct {
	at      tokenPlace
	integer bool
}

// judgedLimitBreakers returns, keyed by the offset just past their last byte,
// the numbers of data past the implementation limits of the part
// ([breaksNumberLimits]) that compliance judges: the ones the operator taking
// them reads, as [compliance.ContentOperandLink] states it, while the stream
// processes its colour operators as [compliance.ColourState] follows it.
// uncoloured reports that the stream starts with them ignored.
//
// The stream is read as validation reads it ([visitVeraPDFTokens]), which
// finds the operator c in "c047" and the number 047 after it, and gives a run
// of digits past the range of a double the value veraPDF reads in it. It is
// read whole, past the stray "]" where validation stops, as validation reads
// the stream once the repairs drop it.
//
// A number no operator reads, one an undefined operator takes among them,
// answers to no limit veraPDF states, so it is left as written. The undefined
// operator is the undefined-operator repair's ([matchUnknownOps]).
//
// # Corpus evidence
//
//   - pdfjs bug1130815.pdf: its damaged page gives three numbers past the
//     limits to undefined operators, such as ll, which the repair respelled
//     at PDF/A-1b and PDF/A-2b and now leaves. No other file's repair
//     changes. veraPDF 1.30 cannot open the file.
func judgedLimitBreakers(ctx context.Context, data []byte, part int, uncoloured bool) map[int]bool {
	var (
		judged map[int]bool
		// operands holds the numbers past the limits read since the last
		// operator, and image the ones of the inline image dictionary being
		// read, with imageMask and maskKey following its /IM entry.
		operands, image    []limitBreaker
		imageMask, maskKey bool
	)
	colour := compliance.NewColourState(uncoloured)
	mark := func(b limitBreaker) {
		if judged == nil {
			judged = make(map[int]bool)
		}
		judged[b.at.end] = true
	}
	// endImage settles the inline image whose dictionary was read: veraPDF
	// reads it only while the stream processes its colour operators, or as
	// an image mask.
	endImage := func() {
		if colour.ReadsInlineImage(imageMask) {
			for _, b := range image {
				mark(b)
			}
		}
		image, imageMask, maskKey = image[:0], false, false
	}
	visitVeraPDFTokens(ctx, data, func(tok lexer.Token, at tokenPlace) {
		breaks := tok.Type == lexer.TokenNumber && breaksNumberLimits(tok.Value, part)
		if at.inlineImage {
			if breaks {
				image = append(image, limitBreaker{at: at})
			}
			// ISO 32000-1, 8.9.7, Table 93 abbreviates /ImageMask as /IM, the
			// only spelling veraPDF reads.
			imageMask = imageMask || maskKey && tok.Type == lexer.TokenKeyword && tok.Value == "true"
			maskKey = at.depth == 1 && tok.Type == lexer.TokenName && tok.Value == "/IM"
			return
		}
		if len(image) > 0 || imageMask {
			endImage()
		}
		switch {
		case breaks:
			operands = append(operands, limitBreaker{at: at, integer: !strings.ContainsRune(tok.Value, '.')})
		case at.depth == 0 && isOperatorToken(tok):
			for _, b := range operands {
				if operandJudged(compliance.ContentOperandLink(tok.Value, b.at.operand, at.operand, colour.On()), b) {
					mark(b)
				}
			}
			operands = operands[:0]
			colour.Operator(tok.Value)
		}
	})
	if len(image) > 0 {
		endImage()
	}
	return judged
}

// operandJudged reports whether link, what the operator reads of an operand
// ([compliance.ContentOperandLink]), reaches b, a number standing in it.
func operandJudged(link compliance.OperandLink, b limitBreaker) bool {
	switch {
	case b.at.depth == 0:
		return link&compliance.LinkNumber != 0 || b.integer && link&compliance.LinkInteger != 0
	case b.at.dict:
		return link&compliance.LinkDict != 0
	}
	return link&compliance.LinkArray != 0 || b.at.depth == 1 && link&compliance.LinkArrayNumbers != 0
}

// cmOperands is the count of operands of a cm operator (ISO 32000-1, 8.4.4,
// Table 57).
const cmOperands = 6

// splitOperand is an operand [translationSplits] reads: the ordinal of its
// token among the tokens [contentTokenSplices] offers, and the token.
type splitOperand struct {
	ordinal int
	tok     lexer.Token
}

// translationSplits returns, keyed by the ordinal of the token among the
// tokens [contentTokenSplices] offers, the text that writes every cm operator
// of data whose translation breaks the PDF/A-1 real limit (ISO 19005-1, 6.1.12
// t2), with its other operands inside it, as translations inside the limit
// ahead of it, and how many such operators it found.
//
// cm concatenates its matrix with the current transformation matrix (ISO
// 32000-1, 8.4.4), so "1 0 0 1 tx ty cm a b c d e f cm" draws as
// "a b c d e+tx f+ty cm". Each translation written ahead moves by the limit,
// signed, and the cm keeps the rest, written with as many decimals as its
// operand, so the sum is the operand to the digit. A translation that takes
// more than [maxTranslationSplits] splits is left to the clamp, and so is a cm
// whose operands are not six numbers where an operand stands.
//
// Parts 2 and 3 admit a real up to 3.403e38, which no translation of a real
// document reaches, and an integer past their integer range is respelled as
// the real of its value, which draws the same.
func translationSplits(ctx context.Context, data []byte, part int) (map[int]string, int) {
	if part != 1 {
		return nil, 0
	}
	var splits map[int]string
	found := 0
	// operands holds the last cmOperands operands read since the last
	// operator, count how many were read, and nested whether one of them
	// was an array, a dictionary or an inline image.
	var operands []splitOperand
	count, nested := 0, false
	ordinal := -1
	contentTokenSplices(ctx, data, func(tok lexer.Token, at tokenPlace) (string, bool) {
		ordinal++
		switch {
		case at.depth > 0 || at.inlineImage:
			nested = true
		case tok.Type == lexer.TokenKeyword && tok.Value != "true" && tok.Value != "false" && tok.Value != "null":
			if tok.Value == "cm" && !nested && count == cmOperands {
				if texts, ok := splitTranslation(operands, part); ok {
					if splits == nil {
						splits = make(map[int]string)
					}
					maps.Copy(splits, texts)
					found++
				}
			}
			operands, count, nested = operands[:0], 0, false
		default:
			count++
			if len(operands) == cmOperands {
				operands = append(operands[:0], operands[1:]...)
			}
			operands = append(operands, splitOperand{ordinal: ordinal, tok: tok})
		}
		return "", false
	})
	return splits, found
}

// splitTranslation returns, keyed by ordinal, the texts that write the cm
// operator whose operands are operands as [translationSplits] describes, and
// false when it is no such operator.
func splitTranslation(operands []splitOperand, part int) (map[int]string, bool) {
	for k, o := range operands {
		if o.tok.Type != lexer.TokenNumber || (k < 4 && breaksNumberLimits(o.tok.Value, part)) {
			return nil, false
		}
	}
	e, f := operands[4], operands[5]
	ne, re, okE := translationPieces(e.tok.Value, part)
	nf, rf, okF := translationPieces(f.tok.Value, part)
	if !okE || !okF || (ne == 0 && nf == 0) {
		return nil, false
	}
	var prefix strings.Builder
	for k := range max(ne, nf) {
		fmt.Fprintf(&prefix, "1 0 0 1 %s %s cm ", limitPiece(e.tok.Value, k < ne), limitPiece(f.tok.Value, k < nf))
	}
	texts := map[int]string{operands[0].ordinal: prefix.String() + operands[0].tok.Value}
	if ne > 0 {
		texts[e.ordinal] = re
	}
	if nf > 0 {
		texts[f.ordinal] = rf
	}
	return texts, true
}

// translationPieces returns how many translations of the PDF/A-1 real limit,
// signed, the number token tok takes to come inside the limits of the part,
// and what is left of it written with as many decimals as tok: none for a
// number inside them, an integer PDF/A-1 admits up to 2147483647 among them.
// ok is false when tok takes more than [maxTranslationSplits] translations.
func translationPieces(tok string, part int) (int, string, bool) {
	if !breaksNumberLimits(tok, part) {
		return 0, tok, true
	}
	v, err := strconv.ParseFloat(tok, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, "", false
	}
	limit := compliance.MaxRealAbs1
	n := int(math.Ceil(math.Abs(v)/limit)) - 1
	if n > maxTranslationSplits {
		return 0, "", false
	}
	rest := v - math.Copysign(float64(n)*limit, v)
	decimals := 0
	if i := strings.IndexByte(tok, '.'); i >= 0 {
		decimals = len(tok) - i - 1
	}
	return n, strconv.FormatFloat(rest, 'f', decimals, 64), true
}

// limitPiece returns the translation one split of the number token tok moves
// by: the PDF/A-1 real limit with the sign of tok when move is set, and 0
// otherwise.
func limitPiece(tok string, move bool) string {
	if !move {
		return "0"
	}
	limit := strconv.FormatFloat(compliance.MaxRealAbs1, 'f', -1, 64)
	if strings.HasPrefix(tok, "-") {
		return "-" + limit
	}
	return limit
}

// breaksNumberLimits reports whether the number token tok, as ISO 32000-1,
// 7.3.3 spells it, breaks an implementation limit of the PDF/A part: an
// integer outside the range of ISO 19005-1, 6.1.12 t1 and ISO 19005-2/3,
// 6.1.13 t1, a real past the magnitude of ISO 19005-1, 6.1.12 t2 or ISO
// 19005-2/3, 6.1.13 t2, or a nonzero real closer to 0 than ISO 19005-2/3,
// 6.1.13 t5 admits. ISO 19005-4 states none of these limits.
//
// A token written with an exponent is left to [rewriteExponentNumbers]:
// validation reads the number before the exponent letter alone, as veraPDF
// does.
func breaksNumberLimits(tok string, part int) bool {
	if part < 1 || part > 3 || strings.ContainsAny(tok, "eE") {
		return false
	}
	if !strings.ContainsRune(tok, '.') {
		n, err := strconv.ParseInt(tok, 10, 64)
		return err != nil || n > compliance.MaxIntegerValue || n < -compliance.MaxIntegerValue-1
	}
	v, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		// Digits around one period fail ParseFloat only past the range of a
		// double, which is past every real limit.
		return true
	}
	if part == 1 {
		return math.Abs(v) > compliance.MaxRealAbs1
	}
	return math.Abs(v) > compliance.MaxRealAbs2 || (v != 0 && math.Abs(v) < compliance.MinRealAbs2)
}
