package compliance

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
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
func checkJPXImage(ctx context.Context, doc *document.Document, stream ast.PdfStream, name ast.PdfName, pageNum, part int) []Violation {
	rules := jpxRulesForPart(part)
	if rules.t1.Specification == "" {
		return nil
	}

	data, err := stream.Bytes(ctx)
	if err != nil || len(data) < 12 {
		return nil
	}

	if isJP2Container(data) {
		// Per ISO 19005-2 §6.2.8.3, the colour-spec checks (t2/t3/t4) are
		// waived when the image XObject supplies its own /ColorSpace.
		return checkJP2Container(data, name, pageNum, rules, imageHasColorSpace(ctx, doc, stream.Dict))
	}
	// Raw J2K codestream: FF 4F (SOC) FF 51 (SIZ).
	if data[0] == 0xFF && data[1] == 0x4F && data[2] == 0xFF && data[3] == 0x51 {
		return checkRawJ2K(data, name, pageNum, rules)
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

// isJP2Container checks for the JP2 signature box: 00 00 00 0C 6A 50 20 20 0D 0A 87 0A.
func isJP2Container(data []byte) bool {
	return len(data) >= 12 &&
		data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x00 && data[3] == 0x0C &&
		data[4] == 0x6A && data[5] == 0x50 && data[6] == 0x20 && data[7] == 0x20 &&
		data[8] == 0x0D && data[9] == 0x0A && data[10] == 0x87 && data[11] == 0x0A
}

// JP2 box type constants.
const (
	boxTypeJP2H = 0x6A703268 // 'jp2h'
	boxTypeIHDR = 0x69686472 // 'ihdr'
	boxTypeCOLR = 0x636F6C72 // 'colr'
	boxTypeBPCC = 0x62706363 // 'bpcc'
)

type jp2Box struct {
	boxType uint32
	payload []byte
}

func parseJP2Boxes(data []byte) []jp2Box {
	var boxes []jp2Box
	off := 0
	for off+8 <= len(data) {
		length := int(binary.BigEndian.Uint32(data[off : off+4]))
		btype := binary.BigEndian.Uint32(data[off+4 : off+8])

		headerSize := 8
		if length == 1 && off+16 <= len(data) {
			// Extended length.
			length = int(binary.BigEndian.Uint64(data[off+8 : off+16])) //nolint:gosec // G115: JP2 box lengths are bounded by file size.
			headerSize = 16
		}
		if length == 0 {
			// Box extends to end of data.
			boxes = append(boxes, jp2Box{boxType: btype, payload: data[off+headerSize:]})
			break
		}
		if length < headerSize || off+length > len(data) {
			break // Truncated or invalid.
		}
		boxes = append(boxes, jp2Box{boxType: btype, payload: data[off+headerSize : off+length]})
		off += length
	}
	return boxes
}

// colrInfo holds parsed data from a colr box.
type colrInfo struct {
	meth   byte
	approx byte
	enumCS uint32
}

func parseCOLR(payload []byte) (colrInfo, bool) {
	if len(payload) < 3 {
		return colrInfo{}, false
	}
	ci := colrInfo{meth: payload[0], approx: payload[2]}
	if ci.meth == 0x01 && len(payload) >= 7 {
		ci.enumCS = binary.BigEndian.Uint32(payload[3:7])
	}
	return ci, true
}

// selectedColr is the effective colour specification for a JP2 container.
// Per ISO 19005-2 §6.2.8.3, a conforming reader uses the colour spec whose
// APPROX field is 0x01; its METH and enumerated colour space are the effective
// ones. When no spec has APPROX 0x01 and there is exactly one spec, that sole
// spec is used; otherwise no effective colour space is defined (methSet false).
type selectedColr struct {
	methSet   bool
	meth      byte
	enumCSSet bool
	enumCS    uint32
}

func selectColr(boxes []colrInfo) selectedColr {
	var s selectedColr
	anyApprox := false
	for _, c := range boxes {
		if c.approx != 0x01 {
			continue
		}
		anyApprox = true
		if !s.methSet {
			s.methSet = true
			s.meth = c.meth
		}
		// An enumerated colour space exists only for METH 0x01.
		if c.meth == 0x01 && !s.enumCSSet {
			s.enumCSSet = true
			s.enumCS = c.enumCS
		}
	}
	if anyApprox {
		return s
	}
	if len(boxes) == 1 {
		s.methSet = true
		s.meth = boxes[0].meth
		if boxes[0].meth == 0x01 {
			s.enumCSSet = true
			s.enumCS = boxes[0].enumCS
		}
	}
	return s
}

func checkJP2Container(data []byte, name ast.PdfName, pageNum int, rules jpxRules, hasColorSpace bool) []Violation {
	boxes := parseJP2Boxes(data)

	// Find jp2h superbox.
	var jp2hPayload []byte
	for _, b := range boxes {
		if b.boxType == boxTypeJP2H {
			jp2hPayload = b.payload
			break
		}
	}
	if jp2hPayload == nil {
		return nil
	}

	inner := parseJP2Boxes(jp2hPayload)

	// Parse ihdr box.
	var numComponents int
	var ihdrBPC byte
	var bpccSsiz []byte
	for _, b := range inner {
		switch b.boxType {
		case boxTypeIHDR:
			if len(b.payload) >= 14 {
				numComponents = int(binary.BigEndian.Uint16(b.payload[8:10]))
				ihdrBPC = b.payload[10]
			}
		case boxTypeBPCC:
			bpccSsiz = b.payload
		}
	}

	// Parse all colr boxes.
	var colrBoxes []colrInfo
	for _, b := range inner {
		if b.boxType == boxTypeCOLR {
			if ci, ok := parseCOLR(b.payload); ok {
				colrBoxes = append(colrBoxes, ci)
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
		if c.approx == 0x01 {
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
	if !hasColorSpace && sel.enumCSSet && sel.enumCS == 19 {
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

func checkRawJ2K(data []byte, name ast.PdfName, pageNum int, rules jpxRules) []Violation {
	numComponents, ssiz, err := parseJ2KSIZMarker(data)
	if err != nil {
		// Pure-data helper: no *document.Document in scope, so the
		// parse-failure log goes to the package-level slog rather than
		// the document logger. Malformed JPX codestreams produce a
		// silent skip; a higher-level check will emit a rule violation
		// if a JPX image is expected to parse.
		slog.Debug("compliance.jpx: cannot parse J2K SIZ marker", "error", err)
		return nil
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

// parseJ2KSIZMarker parses the SIZ marker from a raw J2K codestream.
// Layout: [FF 4F] SOC [FF 51] SIZ marker
//
//	Lsiz(2) Rsiz(2) Xsiz(4) Ysiz(4) XOsiz(4) YOsiz(4)
//	XTsiz(4) YTsiz(4) XTOsiz(4) YTOsiz(4) Csiz(2)
//	then per component: Ssiz(1) XRsiz(1) YRsiz(1)
func parseJ2KSIZMarker(data []byte) (int, []byte, error) {
	// SIZ marker starts at offset 2 (after SOC).
	// Csiz at offset 2+2+2+2+4*8 = 40 from data start? No:
	// offset 0: FF 4F (SOC)
	// offset 2: FF 51 (SIZ marker)
	// offset 4: Lsiz (2 bytes)
	// offset 6: Rsiz (2 bytes)
	// offset 8: Xsiz (4 bytes)
	// ...
	// offset 38: Csiz (2 bytes) = 4 + 2 + 2 + 4*8 = 4+2+2+32 = 40... let me recount:
	// After FF 51: Lsiz(2), Rsiz(2), Xsiz(4), Ysiz(4), XOsiz(4), YOsiz(4),
	//              XTsiz(4), YTsiz(4), XTOsiz(4), YTOsiz(4), Csiz(2)
	// = 2+2+4+4+4+4+4+4+4+4+2 = 38 bytes from start of Lsiz
	// The Lsiz field is at data[4], so Csiz is at data[4+2+2+4*8] = data[4+36] = data[40]
	const csizOffset = 40
	if len(data) < csizOffset+2 {
		return 0, nil, errTruncated
	}
	csiz := int(binary.BigEndian.Uint16(data[csizOffset : csizOffset+2]))
	compStart := csizOffset + 2
	needed := compStart + csiz*3
	if len(data) < needed || csiz == 0 {
		return 0, nil, errTruncated
	}
	ssiz := make([]byte, csiz)
	for i := range csiz {
		ssiz[i] = data[compStart+i*3]
	}
	return csiz, ssiz, nil
}

var errTruncated = &truncatedError{}

type truncatedError struct{}

func (e *truncatedError) Error() string { return "truncated JPEG2000 data" }

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
