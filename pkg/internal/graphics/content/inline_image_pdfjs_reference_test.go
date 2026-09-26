package content

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// referencePDFJSInlineImageEnd is the reading of [pdfjsImages.end] with
// nothing kept from one image to the next: every scan reads the stream from
// the data of its image, and every check lexes its window afresh. It is the
// oracle [FuzzPDFJSImagesMatchReference] holds the reader to.
func referencePDFJSInlineImageEnd(data []byte, idEnd int, dict ast.PdfDict) (int, bool) {
	start := min(idEnd+1, len(data))
	switch pdfjsFirstFilter(dict) {
	case "DCT", "DCTDecode":
		if end, ok := referencePDFJSDCTEnd(data, start); ok {
			return end, false
		}
	case "A85", "ASCII85Decode":
		if end, ok := pdfjsASCII85End(data, start); ok {
			return end, false
		}
	case "AHx", "ASCIIHexDecode":
		if gt := bytes.IndexByte(data[start:], '>'); gt >= 0 {
			return pdfjsSkipEI(data, start+gt+1), false
		}
	}
	return referencePDFJSDefaultEnd(data, start)
}

// referencePDFJSDefaultEnd is Parser.findDefaultInlineStreamEnd read byte
// by byte from start.
func referencePDFJSDefaultEnd(data []byte, start int) (int, bool) {
	state := 0
	lastEI := -1
	for i := start; i < len(data); {
		c := data[i]
		i++
		switch state {
		case 0:
			if c == 'E' {
				state = 1
			}
		case 1:
			state = 0
			if c == 'I' {
				state = 2
			}
		default:
			state = 0
			if c != ' ' && c != '\n' && c != '\r' {
				continue
			}
			lastEI = i
			if i == len(data) {
				return i, false
			}
			if !pdfjsASCII(data[i:min(i+pdfjsASCIIWindow, len(data))]) {
				continue
			}
			switch referencePDFJSCommandAfter(data[i:min(i+pdfjsCommandWindow, len(data))]) {
			case pdfjsOperator:
				return i, false
			case pdfjsThrows:
				return i, true
			case pdfjsNoOperator:
			}
		}
	}
	if lastEI >= 0 {
		return lastEI, false
	}
	return len(data), false
}

// referencePDFJSCommandAfter lexes window from its first byte to its end.
func referencePDFJSCommandAfter(window []byte) pdfjsCheck {
	lx := pdfjsLexer{b: window}
	operands := 0
	for {
		at := lx.i
		kind, cmd := pdfjsEOF, []byte(nil)
		if lx.skip() {
			kind, cmd = lx.token()
		}
		switch kind {
		case pdfjsEOF:
			return pdfjsNoOperator
		case pdfjsError:
			return pdfjsThrows
		case pdfjsCommand:
			args, ok := pdfjsOperands[string(cmd)]
			if !ok {
				return pdfjsNoOperator
			}
			if args.variable && operands <= args.n || !args.variable && operands == args.n {
				return pdfjsOperator
			}
			operands = 0
		case pdfjsValue:
			operands++
		}
		if lx.i <= at {
			return pdfjsNoOperator
		}
	}
}

// referencePDFJSDCTEnd is Parser.findDCTDecodeInlineStreamEnd read byte by
// byte from start.
func referencePDFJSDCTEnd(data []byte, start int) (int, bool) {
	for i := start; i < len(data); {
		b := data[i]
		i++
		if b != 0xff || i == len(data) {
			continue
		}
		marker := data[i]
		i++
		switch {
		case marker == 0x00:
		case marker == 0xff:
			i--
		case marker == 0xd9:
			return pdfjsSkipEI(data, i), true
		case pdfjsDCTSegment(marker):
			if i == len(data) {
				return 0, false
			}
			if i+1 == len(data) {
				i--
				continue
			}
			if length := int(data[i])<<8 | int(data[i+1]); length > 2 {
				i += length
			}
		}
	}
	return 0, false
}

// FuzzPDFJSImagesMatchReference asserts that [pdfjsImages], which keeps what
// its scans learnt of a stream, ends the data of every inline image where
// [referencePDFJSInlineImageEnd] does, and stops the reading where it stops
// it, whatever images it read before and in whatever order. The bytes are a
// content stream's, entirely file-controlled.
func FuzzPDFJSImagesMatchReference(f *testing.F) {
	f.Add([]byte("ID xEI 1 Td EI Q\nID \xff\xe0\x00\x04\xff\xd9 EI\nID EEI zz ID ~ EI\nID > EI"), uint64(0x0123456789abcdef))
	f.Add([]byte("ID x EI EI EI (a EI [ EI\n1 2 EI\r\x80\x81 EI BDC EI -5 EI % c\nEI BT"), uint64(0xfedcba9876543210))
	// Truncated: a DCT segment, a string and a sign cut short.
	f.Add([]byte("ID \xff\xc0\x00"), uint64(1))
	f.Add([]byte("ID x EI (a\\1 EI -"), uint64(2))

	filters := []ast.PdfDict{{}, {"F": ast.PdfName("DCT")}, {"F": ast.PdfName("A85")}, {"Filter": ast.PdfArray{ast.PdfName("AHx")}}}
	f.Fuzz(func(t *testing.T, data []byte, seed uint64) {
		if len(data) > maxFuzzInput {
			return
		}
		ctx := context.Background()
		// Every ID of the stream, in order, then the offsets the seed draws,
		// in whatever order it draws them.
		var ids []int
		for i := 0; ; {
			k := bytes.Index(data[i:], []byte("ID"))
			if k < 0 {
				break
			}
			ids = append(ids, i+k+2)
			i += k + 2
		}
		for range 8 {
			seed = seed*6364136223846793005 + 1442695040888963407
			ids = append(ids, int(seed>>33)%(len(data)+1))
		}
		r := newPDFJSImages(data)
		for k, idEnd := range ids {
			dict := filters[(k+int(seed>>60))%len(filters)]
			got, gotStops := r.end(ctx, idEnd, dict)
			want, wantStops := referencePDFJSInlineImageEnd(data, idEnd, dict)
			if got != want || gotStops != wantStops {
				t.Fatalf("end(%d, %v) = %d, %t, want %d, %t", idEnd, dict, got, gotStops, want, wantStops)
			}
		}
	})
}
