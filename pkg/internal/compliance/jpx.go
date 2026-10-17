package compliance

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
)

// jpxRules holds the 5 rule constants for a given PDF/A part.
type jpxRules struct{ t1, t2, t3, t4, t5 Rule }

func jpxRulesForPart(part int) jpxRules {
	switch part {
	case 2:
		return jpxRules{
			Rule_19005_2_6_2_8_3_t01, Rule_19005_2_6_2_8_3_t02,
			Rule_19005_2_6_2_8_3_t03, Rule_19005_2_6_2_8_3_t04,
			Rule_19005_2_6_2_8_3_t05,
		}
	case 3:
		return jpxRules{
			Rule_19005_3_6_2_8_3_t01, Rule_19005_3_6_2_8_3_t02,
			Rule_19005_3_6_2_8_3_t03, Rule_19005_3_6_2_8_3_t04,
			Rule_19005_3_6_2_8_3_t05,
		}
	case 4:
		return jpxRules{
			Rule_19005_4_6_2_7_3_t01, Rule_19005_4_6_2_7_3_t02,
			Rule_19005_4_6_2_7_3_t03, Rule_19005_4_6_2_7_3_t04,
			Rule_19005_4_6_2_7_3_t05,
		}
	}
	return jpxRules{}
}

// checkJPXImage validates JPEG2000 data stream constraints for an Image XObject
// with JPXDecode filter.
//
// # Corpus evidence
//
//   - pdfjs issue11004.pdf, issue12579.pdf: write [/ASCII85Decode /JPXDecode]
//     and [/FlateDecode /JPXDecode] respectively, and behind either one no
//     §6.2.8.3 violation was reported while the raw bytes were read.
func checkJPXImage(ctx context.Context, doc *document.Document, stream ast.PdfStream, name ast.PdfName, pageNum, part int) []Violation {
	rules := jpxRulesForPart(part)
	if rules.t1.Specification == "" {
		return nil
	}

	// The checks below read what JPXDecode consumes, so the chain is applied
	// up to it. Asking for the raw bytes yielded the codestream only when
	// JPXDecode was the whole chain, so a filter in front of it hid every
	// §6.2.8.3 violation.
	data, _, err := doc.DecodeStreamUpTo(ctx, stream, "JPXDecode")
	if err != nil || len(data) < 12 {
		return nil
	}

	if jpx.IsJP2(data) {
		// Per ISO 19005-2 §6.2.8.3, the colour-spec checks (t2/t3/t4) are
		// waived when the image XObject supplies its own /ColorSpace.
		return checkJP2Container(data, name, pageNum, rules, imageHasColorSpace(ctx, doc, stream.Dict), doc.Logger)
	}
	// Raw J2K codestream: FF 4F (SOC) FF 51 (SIZ).
	if data[0] == 0xFF && data[1] == 0x4F && data[2] == 0xFF && data[3] == 0x51 {
		return checkRawJ2K(data, name, pageNum, rules, doc.Logger)
	}

	return nil
}

// imageHasColorSpace reports whether an image XObject dict supplies a usable
// /ColorSpace. A present, non-null /ColorSpace entry defines the image's colour
// space, so the JP2 colour-specification boxes are not consulted.
func imageHasColorSpace(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	obj, ok := dict["ColorSpace"]
	if !ok {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return false
	}
	if _, isNull := resolved.(ast.PdfNull); isNull {
		return false
	}
	return resolved != nil
}

// selectedColr is the effective colour specification for a JP2 container.
// Per ISO 19005-2 §6.2.8.3, a conforming reader uses the colour spec whose
// APPROX field is 0x01; its METH and enumerated colour space are the effective
// ones. When no spec has APPROX 0x01 and there is exactly one spec, that sole
// spec is used; otherwise no effective colour space is defined (methSet false).
type selectedColr struct {
	methSet   bool
	meth      int
	enumCSSet bool
	enumCS    uint32
}

// selectColr picks the effective colour specification the way veraPDF does
// (veraPDF-parser, JPEG2000.parseHeader): METH from the first box with APPROX
// 0x01, EnumCS from the first such box of METH 1, and the sole box when none
// has APPROX 0x01. This is compliance's reading on purpose, not the decoder's:
// [jpx.Image.Colours] ranks the boxes by PREC, then APPROX, as ISO 32000-1,
// 7.4.9 has a PDF reader try them, and drops a method it cannot read, while
// ISO 19005-2, 6.2.8.3 names the APPROX 0x01 box and judges every METH.
func selectColr(boxes []jpx.ColourSpec) selectedColr {
	var s selectedColr
	anyApprox := false
	for _, c := range boxes {
		if c.Approximation != 0x01 {
			continue
		}
		anyApprox = true
		if !s.methSet {
			s.methSet = true
			s.meth = c.Method
		}
		// An enumerated colour space exists only for METH 0x01.
		if c.Method == 0x01 && !s.enumCSSet {
			s.enumCSSet = true
			s.enumCS = c.EnumCS
		}
	}
	if anyApprox {
		return s
	}
	if len(boxes) == 1 {
		s.methSet = true
		s.meth = boxes[0].Method
		if boxes[0].Method == 0x01 {
			s.enumCSSet = true
			s.enumCS = boxes[0].EnumCS
		}
	}
	return s
}

// checkJP2Container judges the JP2 Header box of a JP2 file.
//
// The boxes are read with [jpx.ReadBoxes], and a malformed box header ends the
// walk without discarding the boxes before it: the rules judge what the
// header states, so a file damaged after its header is still judged on it.
// The first JP2 Header box is judged wherever it sits among the top-level
// boxes, and a Colour Specification box is counted whatever its method, since
// t2 and t3 judge the count and the METH of every box (ISO 19005-2,
// 6.2.8.3), where a decoder skips a method it does not know (T.800, Table
// I.9). An XLBox of 0, which T.800, I.4 does not define, ends the walk as any
// other malformed length does.
//
// # Corpus evidence
//
//   - All corpora: the 341 JPXDecode streams in 55 files judge identically,
//     at every PDF/A-2, -3 and -4 profile, under this walk and under the
//     compliance-only walk it replaced.
func checkJP2Container(data []byte, name ast.PdfName, pageNum int, rules jpxRules, hasColorSpace bool, logger *slog.Logger) []Violation {
	boxes, err := jpx.ReadBoxes(data)
	if err != nil {
		logger.Debug("compliance.jpx: JP2 box walk stopped", "xobject", name, "page", pageNum, "error", err)
	}

	var (
		jp2h      []byte
		foundJP2H bool
	)
	for _, b := range boxes {
		if b.Type == jpx.BoxHeader {
			jp2h, foundJP2H = b.Body, true
			break
		}
	}
	if !foundJP2H {
		return nil
	}

	inner, err := jpx.ReadBoxes(jp2h)
	if err != nil {
		logger.Debug("compliance.jpx: JP2 Header box walk stopped", "xobject", name, "page", pageNum, "error", err)
	}

	var (
		numComponents int
		ihdrBPC       byte
		bpccSsiz      []byte
		colrBoxes     []jpx.ColourSpec
	)
	for _, b := range inner {
		switch b.Type {
		case jpx.BoxImageHeader:
			// A short Image Header box states nothing; the last readable one
			// stands.
			if h, err := jpx.ReadImageHeader(b.Body); err == nil {
				numComponents = h.Components
				ihdrBPC = h.BPC
			}
		case jpx.BoxBitsPerComponent:
			bpccSsiz = b.Body
		case jpx.BoxColour:
			// A box too short for METH, PREC and APPROX states no colour
			// specification and is not counted.
			if c, err := jpx.ReadColourSpec(b.Body); err == nil {
				colrBoxes = append(colrBoxes, c)
			}
		}
	}

	var results []Violation

	// t1: Number of colour channels must be 1, 3, or 4.
	if numComponents != 1 && numComponents != 3 && numComponents != 4 {
		results = append(results, failf(rules.t1,
			"page %d, XObject %s: JPEG2000 has %d colour channels", pageNum, name, numComponents)...)
	}

	// The colour-spec rules (t2/t3/t4) defer to an explicit image /ColorSpace.
	approxCount := 0
	for _, c := range colrBoxes {
		if c.Approximation == 0x01 {
			approxCount++
		}
	}
	sel := selectColr(colrBoxes)

	// t2: there must be exactly one colour spec, or exactly one with APPROX=0x01.
	if !hasColorSpace && len(colrBoxes) != 1 && approxCount != 1 {
		results = append(results, failf(rules.t2,
			"page %d, XObject %s: %d colr specs, %d with APPROX=0x01 (need exactly 1 spec or 1 with APPROX=0x01)",
			pageNum, name, len(colrBoxes), approxCount)...)
	}

	// t3: the effective colour method (the APPROX=0x01 spec, or the sole spec)
	// must be 0x01, 0x02, or 0x03. An undefined effective method also fails.
	if !hasColorSpace && (!sel.methSet || (sel.meth != 0x01 && sel.meth != 0x02 && sel.meth != 0x03)) {
		methDesc := "none"
		if sel.methSet {
			methDesc = fmt.Sprintf("0x%02X", sel.meth)
		}
		results = append(results, failf(rules.t3,
			"page %d, XObject %s: effective colr METH=%s", pageNum, name, methDesc)...)
	}

	// t4: the effective enumerated colour space must not be 19 (CIEJab).
	if !hasColorSpace && sel.enumCSSet && sel.enumCS == jpx.ColourCIEJab {
		results = append(results, failf(rules.t4,
			"page %d, XObject %s: CIEJab colour space", pageNum, name)...)
	}

	// t5: Bit depth 1-38, all channels same bit depth.
	if len(bpccSsiz) > 0 {
		checkBitDepths(&results, bpccSsiz, name, pageNum, rules.t5)
	} else if ihdrBPC != 0xFF {
		bd := int(ihdrBPC&0x7F) + 1
		if bd < 1 || bd > 38 {
			results = append(results, failf(rules.t5,
				"page %d, XObject %s: bit depth %d out of range 1-38", pageNum, name, bd)...)
		}
	}

	return results
}

// checkRawJ2K judges a bare codestream by its SIZ marker segment.
//
// The SIZ parameters are read from where T.800, A.5.1 puts them, right after
// SOC, the SIZ marker and Lsiz, and are not bounded by Lsiz: Csiz and the
// component entries the rules judge are the ones the codestream states, and a
// wrong Lsiz does not change them.
func checkRawJ2K(data []byte, name ast.PdfName, pageNum int, rules jpxRules, logger *slog.Logger) []Violation {
	const sizParams = 6 // SOC, the SIZ marker and Lsiz.
	if len(data) < sizParams {
		return nil
	}
	siz, err := jpx.ReadSIZ(data[sizParams:])
	if err != nil || len(siz.Components) == 0 {
		// A codestream whose SIZ cannot be read states no channels to judge.
		logger.Debug("compliance.jpx: cannot read the J2K SIZ marker segment", "xobject", name, "page", pageNum, "error", err)
		return nil
	}
	numComponents := len(siz.Components)
	ssiz := make([]byte, numComponents)
	for i, c := range siz.Components {
		ssiz[i] = c.Ssiz
	}

	var results []Violation

	// t1: channels must be 1, 3, or 4.
	if numComponents != 1 && numComponents != 3 && numComponents != 4 {
		results = append(results, failf(rules.t1,
			"page %d, XObject %s: JPEG2000 has %d colour channels", pageNum, name, numComponents)...)
	}

	// t5: bit depth.
	checkBitDepths(&results, ssiz, name, pageNum, rules.t5)

	return results
}

// checkBitDepths validates t5: all channels must have bit depth 1-38 and
// all channels must have the same bit depth.
func checkBitDepths(results *[]Violation, ssiz []byte, name ast.PdfName, pageNum int, rule Rule) {
	if len(ssiz) == 0 {
		return
	}
	firstBD := int(ssiz[0]&0x7F) + 1
	for _, s := range ssiz {
		bd := int(s&0x7F) + 1
		if bd < 1 || bd > 38 {
			*results = append(*results, failf(rule,
				"page %d, XObject %s: bit depth %d out of range 1-38", pageNum, name, bd)...)
			return
		}
		if bd != firstBD {
			*results = append(*results, failf(rule,
				"page %d, XObject %s: inconsistent bit depths (%d vs %d)", pageNum, name, firstBD, bd)...)
			return
		}
	}
}

// hasJPXFilter reports whether a stream dict has /Filter containing JPXDecode.
func hasJPXFilter(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	filterObj, ok := dict["Filter"]
	if !ok {
		return false
	}
	name, err := document.ResolveName(ctx, doc, filterObj)
	if err == nil {
		return name == "JPXDecode"
	}
	arr, err := document.ResolveArray(ctx, doc, filterObj)
	if err != nil {
		return false
	}
	for _, elem := range arr {
		n, _ := document.ResolveName(ctx, doc, elem)
		if n == "JPXDecode" {
			return true
		}
	}
	return false
}
