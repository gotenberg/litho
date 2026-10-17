package document

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// FuzzNewDocument drives whole-document construction from crafted bytes.
//
// Nothing in the repository did this. FuzzHeaderVersion reads only the header
// string, FuzzLexer and FuzzParser build their own lexers over a byte slice,
// and FuzzParseXRefStream takes one already-extracted stream, so the assembly
// that wires them together was reached by no target at all: the linearization
// probe, the hint-stream reader, the xref chain walk and the brute-force
// rebuild fallback. [NewDocument] runs probeLinearization and parseHintStream
// EAGERLY, before parseXRefChain, so those paths are reachable from the first
// bytes of a file.
//
// The 64 KiB cap matches FuzzLexer and FuzzParser, the other two targets that
// consume whole-file bytes. It is a throughput bound, not a safety one:
// rebuildXRef scans the input in overlapping chunks, so cost grows with the
// input while the code paths do not.
//
// # Corpus evidence
//
//   - pdfjs bug1606566.pdf: the headerless shape NewDocument accepts, which
//     makes the headerless seed a supported shape rather than an exotic one.
func FuzzNewDocument(f *testing.F) {
	// A complete tiny document: header, one object, xref table, trailer.
	valid := "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog >>\nendobj\n" +
		"xref\n0 2\n" +
		"0000000000 65535 f \n" +
		"0000000009 00000 n \n" +
		"trailer\n<< /Size 2 /Root 1 0 R >>\n" +
		"startxref\n44\n%%EOF\n"

	for _, seed := range []string{
		valid,
		// No %PDF- header at all. NewDocument accepts this when the tail still
		// parses, so it is a supported shape rather than an exotic one.
		strings.TrimPrefix(valid, "%PDF-1.7\n"),
		// A header pushed beyond headerScanLimit (1024), which must stay
		// rejected rather than being treated as headerless.
		strings.Repeat(" ", 1100) + valid,
		// startxref pointing nowhere, which forces the rebuild fallback.
		strings.Replace(valid, "startxref\n44", "startxref\n999999", 1),
		// A linearization dictionary, so probeLinearization has something to
		// find before the xref chain is walked. /L is omitted deliberately:
		// probeLinearization rejects a stated /L that does not equal
		// fileSize-headerOffset, so a guessed one makes the seed inert.
		"%PDF-1.7\n1 0 obj\n<< /Linearized 1 /H [ 20 30 ] /O 2 /E 90 /N 1 /T 80 >>\nendobj\n" +
			"trailer\n<< /Size 2 >>\nstartxref\n9\n%%EOF\n",
		// Truncated mid-object, and truncated mid-xref.
		"%PDF-1.4\n1 0 obj\n<< /Type",
		"%PDF-1.4\nxref\n0 2\n0000000000 65535 f \n",
		"%PDF-1.4\n",
		"",
		"not a pdf at all",
	} {
		f.Add([]byte(seed))
	}

	// Built rather than written out, because its /H offsets are not known
	// until the bytes are assembled.
	f.Add(linearizedWithHintStream())

	// A trailer /Encrypt naming a null object, and one naming an object that
	// does not parse (the pdfjs PDFBOX-4352-0.pdf shape): both open
	// unencrypted (see resolveEncryptDict).
	f.Add(nullEncryptPDF("4 0 R"))
	f.Add(bytes.Replace(nullEncryptPDF("4 0 R"), []byte("4 0 obj\nnull\n"), []byte("4 0 obj\nE< >\n"), 1))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}

		doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			if doc != nil {
				t.Fatalf("NewDocument returned a document alongside error %v", err)
			}
			return
		}
		if doc == nil {
			t.Fatal("NewDocument returned a nil document and a nil error")
		}

		// A stated version must be major.minor. The same invariant
		// FuzzHeaderVersion pins on headerVersion in isolation, asserted here
		// through the whole-document path that actually stores it: the value
		// is compared against profile requirements, and a caller cannot tell
		// a half-parsed string from a real answer.
		if doc.Version != "" {
			major, minor, found := strings.Cut(doc.Version, ".")
			if !found || major == "" || minor == "" {
				t.Fatalf("Version = %q, which is not major.minor", doc.Version)
			}
		}

		// The header offset indexes into the input the caller supplied.
		if doc.HeaderOffset < 0 || doc.HeaderOffset > int64(len(data)) {
			t.Fatalf("HeaderOffset = %d, outside a %d-byte input", doc.HeaderOffset, len(data))
		}

		// An xref entry's offset is a position in the same input. A negative
		// one indexes backwards from every read site that uses it.
		for num, entry := range doc.xrefEntries {
			if entry.Offset < 0 {
				t.Fatalf("object %d has negative xref offset %d", num, entry.Offset)
			}
		}
	})
}

// linearizedWithHintStream builds a file whose hint stream actually decodes,
// reaching decompressFlate, parsePageOffsetTable and newBitReader.
//
// Mutation cannot invent this. The chain has four gates in series:
// probeLinearization needs /Linearized and rejects a stated /L that is not
// exactly fileSize-headerOffset (so /L is omitted); parseHintStream needs
// /H [offset length] both above zero and a real stream object at that offset;
// decodeHintStreamData needs the filter to be FlateDecode; and
// parsePageOffsetTable needs a 36-byte decompressed header and a /N no larger
// than size/20. A random mutation will not produce a valid zlib member, which
// is why the seeds reached parseHintStream and stopped there.
func linearizedWithHintStream() []byte {
	// 64 bytes, NOT the 28 the length guard accepts. The 13 header items sum
	// to 288 bits = 36 bytes, so a 28..35 byte payload passes
	// parsePageOffsetTable's `len(data) < 28` check and then always fails
	// as truncated. Measured: a 28-byte payload returned "hint stream header
	// truncated at offset 28". All-zero items make every per-page array zero
	// bits wide, so 36 suffices and 64 leaves headroom.
	var comp bytes.Buffer
	zw := zlib.NewWriter(&comp)
	_, _ = zw.Write(make([]byte, 64))
	_ = zw.Close()

	// Fixed-width placeholders so patching them cannot shift any offset.
	const placeholder = "0000000000"

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	buf.WriteString("1 0 obj\n<< /Linearized 1 /N 1 /O 3 /E 90 /T 80 /H [ " +
		placeholder + " " + placeholder + " ] >>\nendobj\n")

	hintOff := buf.Len()
	buf.WriteString("2 0 obj\n<< /Length " + strconv.Itoa(comp.Len()) +
		" /Filter /FlateDecode >>\nstream\n")
	buf.Write(comp.Bytes())
	buf.WriteString("\nendstream\nendobj\n")
	hintLen := buf.Len() - hintOff

	buf.WriteString("trailer\n<< /Size 3 >>\nstartxref\n9\n%%EOF\n")

	out := buf.Bytes()
	first := bytes.Index(out, []byte(placeholder))
	copy(out[first:], fmt.Appendf(nil, "%010d", hintOff))
	second := bytes.Index(out[first+len(placeholder):], []byte(placeholder)) + first + len(placeholder)
	copy(out[second:], fmt.Appendf(nil, "%010d", hintLen))
	return out
}

// TestLinearizedSeedReachesTheHintTable asserts the seed above reaches the
// readers it was built for, rather than inferring it from a coverage number.
//
// A seed that is silently rejected leaves the target inert while looking well
// seeded: the earlier version of this file stated /L 100, which
// probeLinearization refused on every run, and the three readers below stayed
// at 0.0% while the seed list looked complete.
func TestLinearizedSeedReachesTheHintTable(t *testing.T) {
	t.Parallel()

	data := linearizedWithHintStream()

	params := probeLinearization(bytes.NewReader(data), int64(len(data)), 0)
	if params == nil {
		t.Fatal("probeLinearization returned nil: the seed is not recognised as linearized")
	}
	if params.HintStreamOffset <= 0 || params.HintStreamLength <= 0 {
		t.Fatalf("/H = [%d %d], both must exceed zero to reach parseHintStream",
			params.HintStreamOffset, params.HintStreamLength)
	}

	// parseHintStream runs the whole chain: stream parse, FlateDecode, then
	// parsePageOffsetTable over the decompressed bytes.
	if _, err := parseHintStream(bytes.NewReader(data), int64(len(data)), params, limits.DefaultMaxStreamBytes); err != nil {
		t.Fatalf("parseHintStream: %v", err)
	}
}
