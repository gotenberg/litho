package writer

import (
	"context"
	"fmt"
	"io"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// XRefRecord tracks where a written object lives.
//
// An object is either written directly, at Offset, or packed inside an object
// stream, in which case Compressed is set and ObjStmNum/ObjStmIndex name the
// containing stream and the object's position in it (ISO 32000-1, 7.5.8.3,
// type 2). Only a cross-reference STREAM can express the packed form: a
// traditional table has no type field, so [WriteXRefTable] cannot represent it.
type XRefRecord struct {
	ObjNum     int
	Offset     int64
	Generation int
	Free       bool
	// Compressed marks an object held inside an object stream. Offset and
	// Generation are ignored for such an entry; a compressed object always
	// has generation 0 (ISO 32000-1, 7.5.7).
	Compressed  bool
	ObjStmNum   int
	ObjStmIndex int
}

// WriteXRefTable writes a traditional cross-reference table and its trailer
// to w: the "xref" keyword line, one subsection per run of consecutive object
// numbers in records (a header line, then one 20-byte entry per record), the
// "trailer" keyword line, and the serialized trailer dictionary followed by a
// newline. It sorts records by object number in place.
//
// It writes neither the startxref pointer nor the %%EOF marker: the caller
// takes the table's offset from its own write position before the call and
// writes both after it. Returns the bytes written, including those of a
// partial write that ends in an error.
func WriteXRefTable(ctx context.Context, w io.Writer, records []XRefRecord, trailer ast.PdfDict) (written int64, err error) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].ObjNum < records[j].ObjNum
	})

	// Build subsections: groups of consecutive object numbers.
	type subsection struct {
		start   int
		entries []XRefRecord
	}
	var subs []subsection

	for _, rec := range records {
		if len(subs) == 0 || rec.ObjNum != subs[len(subs)-1].start+len(subs[len(subs)-1].entries) {
			subs = append(subs, subsection{start: rec.ObjNum, entries: []XRefRecord{rec}})
		} else {
			subs[len(subs)-1].entries = append(subs[len(subs)-1].entries, rec)
		}
	}

	var total int64

	n, err := io.WriteString(w, "xref\n")
	total += int64(n)
	if err != nil {
		return total, err
	}

	// Write each subsection.
	for _, sub := range subs {
		header := fmt.Sprintf("%d %d\n", sub.start, len(sub.entries))
		n, err := io.WriteString(w, header)
		total += int64(n)
		if err != nil {
			return total, err
		}

		for _, entry := range sub.entries {
			if err := ctx.Err(); err != nil {
				return total, fmt.Errorf("writing xref entry for object %d: %w", entry.ObjNum, err)
			}
			// Per ISO 32000-1, 7.5.4, an entry is exactly 20 bytes including its
			// two-byte end-of-line. With CRLF the type keyword is followed by the
			// EOL directly; the "n <CR><LF>" spelling is 21 bytes and desynchronises
			// strict readers from the second entry onward.
			var line string
			if entry.Free {
				line = fmt.Sprintf("%010d %05d f\r\n", entry.Offset, entry.Generation)
			} else {
				line = fmt.Sprintf("%010d %05d n\r\n", entry.Offset, entry.Generation)
			}
			n, err := io.WriteString(w, line)
			total += int64(n)
			if err != nil {
				return total, err
			}
		}
	}

	// Write trailer.
	n, err = io.WriteString(w, "trailer\n")
	total += int64(n)
	if err != nil {
		return total, err
	}

	n2, err := Serialize(ctx, w, trailer)
	total += n2
	if err != nil {
		return total, err
	}

	n, err = io.WriteString(w, "\n")
	total += int64(n)
	if err != nil {
		return total, err
	}

	return total, nil
}

// WriteXRefStream writes a PDF 1.5+ cross-reference stream to w. The stream
// dict doubles as the trailer. Returns the total bytes written.
// The xref stream itself takes the allocator's next object number.
func WriteXRefStream(ctx context.Context, w io.Writer, records []XRefRecord, trailer ast.PdfDict, allocator *ObjAllocator, selfOffset int64) (int64, error) {
	// The cross-reference stream is itself an indirect object and must
	// carry an entry for its own object number (ISO 32000-1 §7.5.8.2);
	// otherwise a reader following /Index cannot locate it. Allocate the
	// number up front and record a type-1 entry at selfOffset, the write
	// position where SerializeIndirect emits the stream below. The xref
	// stream is the last object written, so selfOffset is fixed
	// regardless of the field widths chosen for the table, and including
	// it here also sizes the offset field wide enough to hold it.
	xrefObjNum := allocator.NextObjNum()
	records = append(records, XRefRecord{ObjNum: xrefObjNum, Offset: selfOffset})

	sort.Slice(records, func(i, j int) bool {
		return records[i].ObjNum < records[j].ObjNum
	})

	// Determine field widths. Type is always 1 byte. Field 2 holds an offset
	// for a type-1 entry and the containing stream's object number for a
	// type-2 one; field 3 holds a generation or the index within that stream
	// (ISO 32000-1, Table 18). Both fields are shared across entry types, so
	// each width must cover the maximum of every value that lands in it: a
	// width taken from offsets alone truncates a larger object-stream number,
	// and one taken from generations alone truncates an index above 255.
	var maxField2, maxField3 int64
	for _, rec := range records {
		f2, f3 := rec.Offset, int64(rec.Generation)
		if rec.Compressed {
			f2, f3 = int64(rec.ObjStmNum), int64(rec.ObjStmIndex)
		}
		if f2 > maxField2 {
			maxField2 = f2
		}
		if f3 > maxField3 {
			maxField3 = f3
		}
	}

	w1 := bytesNeeded(maxField2)
	w2 := bytesNeeded(maxField3)
	if w2 == 0 {
		w2 = 1
	}

	// Encode entries.
	entrySize := 1 + w1 + w2
	data := make([]byte, 0, len(records)*entrySize)
	for _, rec := range records {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("encoding xref entry for object %d: %w", rec.ObjNum, err)
		}
		switch {
		case rec.Free:
			data = append(data, 0) // type 0: free
			data = appendBigEndian(data, rec.Offset, w1)
			data = appendBigEndian(data, int64(rec.Generation), w2)
		case rec.Compressed:
			// Type 2: field 2 is the object stream's number, field 3 the
			// index within it (ISO 32000-1, 7.5.8.3).
			data = append(data, 2)
			data = appendBigEndian(data, int64(rec.ObjStmNum), w1)
			data = appendBigEndian(data, int64(rec.ObjStmIndex), w2)
		default:
			data = append(data, 1) // type 1: normal
			data = appendBigEndian(data, rec.Offset, w1)
			data = appendBigEndian(data, int64(rec.Generation), w2)
		}
	}

	compressed, err := filter.ApplyEncodeFilter(data, "FlateDecode")
	if err != nil {
		return 0, fmt.Errorf("compressing xref stream: %w", err)
	}

	// Build the xref stream dict (which doubles as the trailer).
	// Build Index array: subsections of consecutive object numbers.
	var indexArr ast.PdfArray
	i := 0
	for i < len(records) {
		start := records[i].ObjNum
		count := 1
		for i+count < len(records) && records[i+count].ObjNum == start+count {
			count++
		}
		indexArr = append(indexArr, ast.PdfInt(start), ast.PdfInt(count))
		i += count
	}

	dict := make(ast.PdfDict, len(trailer)+5)
	maps.Copy(dict, trailer)
	// The entry data is written as plain FlateDecode without a PNG predictor.
	// An inherited /DecodeParms (e.g. a predictor and /Columns copied from the
	// source xref stream during an incremental update) would make a strict
	// parser mis-decode the raw rows, so drop it.
	delete(dict, "DecodeParms")
	dict["Type"] = ast.PdfName("XRef")
	dict["W"] = ast.PdfArray{ast.PdfInt(1), ast.PdfInt(w1), ast.PdfInt(w2)}
	dict["Index"] = indexArr
	dict["Size"] = ast.PdfInt(xrefObjNum + 1)
	dict["Length"] = ast.PdfInt(len(compressed))
	dict["Filter"] = ast.PdfName("FlateDecode")

	stream := ast.NewStream(dict, compressed)
	return SerializeIndirect(ctx, w, xrefObjNum, 0, stream)
}

// bytesNeeded returns the minimum number of bytes to represent val in
// big-endian format.
func bytesNeeded(val int64) int {
	if val <= 0 {
		return 1
	}
	n := 0
	for v := val; v > 0; v >>= 8 {
		n++
	}
	return n
}

// appendBigEndian appends val as a big-endian integer of exactly width bytes.
func appendBigEndian(buf []byte, val int64, width int) []byte {
	for i := width - 1; i >= 0; i-- {
		buf = append(buf, byte(val>>(i*8))) //nolint:gosec // val is a PDF offset, always fits in the allocated width
	}
	return buf
}
