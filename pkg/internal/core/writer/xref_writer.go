package writer

import (
	"fmt"
	"io"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// XRefRecord tracks the byte offset of a written object.
type XRefRecord struct {
	ObjNum     int
	Offset     int64
	Generation int
	Free       bool
}

// WriteXRefTable writes a traditional cross-reference table, trailer
// dictionary, and startxref pointer to w. Returns the byte offset of the
// "xref" keyword (for use in the startxref line) and the total bytes written.
func WriteXRefTable(w io.Writer, records []XRefRecord, trailer ast.PdfDict) (xrefOffset int64, written int64, err error) {
	// Sort records by object number.
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

	// Write "xref\n".
	n, err := io.WriteString(w, "xref\n")
	total += int64(n)
	if err != nil {
		return 0, total, err
	}

	// Write each subsection.
	for _, sub := range subs {
		header := fmt.Sprintf("%d %d\n", sub.start, len(sub.entries))
		n, err := io.WriteString(w, header)
		total += int64(n)
		if err != nil {
			return 0, total, err
		}

		for _, entry := range sub.entries {
			var line string
			if entry.Free {
				line = fmt.Sprintf("%010d %05d f \r\n", entry.Offset, entry.Generation)
			} else {
				line = fmt.Sprintf("%010d %05d n \r\n", entry.Offset, entry.Generation)
			}
			n, err := io.WriteString(w, line)
			total += int64(n)
			if err != nil {
				return 0, total, err
			}
		}
	}

	// Write trailer.
	n, err = io.WriteString(w, "trailer\n")
	total += int64(n)
	if err != nil {
		return 0, total, err
	}

	n2, err := Serialize(w, trailer)
	total += n2
	if err != nil {
		return 0, total, err
	}

	n, err = io.WriteString(w, "\n")
	total += int64(n)
	if err != nil {
		return 0, total, err
	}

	return 0, total, nil // xrefOffset is set by the caller from the write position
}

// WriteXRefStream writes a PDF 1.5+ cross-reference stream to w. The stream
// dict doubles as the trailer. Returns the total bytes written.
// The allocator is used to assign an object number to the xref stream itself.
func WriteXRefStream(w io.Writer, records []XRefRecord, trailer ast.PdfDict, allocator *ObjAllocator) (int64, error) {
	// Sort records by object number.
	sort.Slice(records, func(i, j int) bool {
		return records[i].ObjNum < records[j].ObjNum
	})

	// Determine field widths. Type is always 1 byte. Field2 and Field3
	// widths are determined by the maximum values.
	var maxOffset int64
	var maxGen int
	for _, rec := range records {
		if rec.Offset > maxOffset {
			maxOffset = rec.Offset
		}
		if rec.Generation > maxGen {
			maxGen = rec.Generation
		}
	}

	w1 := bytesNeeded(maxOffset)
	w2 := bytesNeeded(int64(maxGen))
	if w2 == 0 {
		w2 = 1
	}

	// Encode entries.
	entrySize := 1 + w1 + w2
	data := make([]byte, 0, len(records)*entrySize)
	for _, rec := range records {
		if rec.Free {
			data = append(data, 0) // type 0: free
		} else {
			data = append(data, 1) // type 1: normal
		}
		data = appendBigEndian(data, rec.Offset, w1)
		data = appendBigEndian(data, int64(rec.Generation), w2)
	}

	// Compress with FlateDecode.
	compressed, err := filter.ApplyEncodeFilter(data, "FlateDecode")
	if err != nil {
		return 0, fmt.Errorf("compressing xref stream: %w", err)
	}

	// Build the xref stream dict (which doubles as the trailer).
	// The xref stream itself gets an object number.
	xrefObjNum := allocator.NextObjNum()

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
	dict["Type"] = ast.PdfName("XRef")
	dict["W"] = ast.PdfArray{ast.PdfInt(1), ast.PdfInt(w1), ast.PdfInt(w2)}
	dict["Index"] = indexArr
	dict["Size"] = ast.PdfInt(xrefObjNum + 1)
	dict["Length"] = ast.PdfInt(len(compressed))
	dict["Filter"] = ast.PdfName("FlateDecode")

	stream := ast.NewStream(dict, compressed)
	return SerializeIndirect(w, xrefObjNum, stream)
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
