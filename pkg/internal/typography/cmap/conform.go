package cmap

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"regexp"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// maxBfRangeEntriesPerGroup is the entry limit of one beginbfrange group:
// Adobe Technical Note #5411, 1.4.1, "the number of entries in each group of
// mappings cannot exceed 100".
const maxBfRangeEntriesPerGroup = 100

// maxRewriteCodeBytes bounds the source-code width [ConformBfRanges] rewrites,
// so a code fits the uint64 its arithmetic uses. A wider code is left as
// written.
const maxRewriteCodeBytes = 8

// bfrangeGroup matches one "N beginbfrange ... endbfrange" group. Submatch 1
// is its body.
var bfrangeGroup = regexp.MustCompile(`\d+\s+beginbfrange([\s\S]*?)endbfrange`)

// bfRangeEntry is one bfrange entry: a source range and either a string
// destination incremented per code or an array holding one destination per
// code.
type bfRangeEntry struct {
	lo, hi  uint64
	width   int
	isArray bool
	dst     []byte
	dstArr  [][]byte
}

// ConformBfRanges rewrites the bfrange entries of a ToUnicode CMap whose
// source codes cross a first-byte boundary, which Adobe Technical Note #5411,
// 1.4.1 rules invalid, into entries that do not. Each such entry is split
// where its source code enters a new first-byte block and where its
// destination's last byte would pass 255 (ISO 32000-2, 9.10.3), and groups
// keep to 100 entries. Groups that need no change keep their bytes.
//
// It returns the rewrite and true only when the result maps exactly the codes
// and values [CMap.Mapping] holds for data and leaves nothing out of
// [CMap.StrictMapping]. Otherwise it returns data and false.
func ConformBfRanges(ctx context.Context, data []byte) ([]byte, bool, error) {
	orig, err := Parse(ctx, data)
	if err != nil {
		return data, false, fmt.Errorf("parsing ToUnicode CMap: %w", err)
	}
	if orig.strict == nil {
		return data, false, nil
	}

	var out bytes.Buffer
	last, changed := 0, false
	for _, m := range bfrangeGroup.FindAllSubmatchIndex(data, -1) {
		if err := ctx.Err(); err != nil {
			return data, false, err
		}
		entries, ok := parseBfRangeEntries(ctx, data[m[2]:m[3]])
		if !ok {
			continue
		}
		split, ok := conformEntries(entries)
		if !ok {
			continue
		}
		out.Write(data[last:m[0]])
		writeBfRangeGroups(&out, split)
		last = m[1]
		changed = true
	}
	if !changed {
		return data, false, nil
	}
	out.Write(data[last:])
	rewritten := out.Bytes()

	// The rewrite is only safe when a lenient reader sees no difference and a
	// strict one finds nothing left to drop. A group this function could not
	// read whole fails the second test and keeps the whole CMap as written.
	got, err := Parse(ctx, rewritten)
	if err != nil || got.strict != nil || !maps.Equal(got.Mapping, orig.Mapping) {
		return data, false, nil //nolint:nilerr // A rewrite that does not round-trip keeps the CMap as written.
	}
	return rewritten, true, nil
}

// parseBfRangeEntries reads the body of one bfrange group. It reports false
// for anything but well-formed entries, so a group it cannot read whole is
// left as written.
func parseBfRangeEntries(ctx context.Context, body []byte) ([]bfRangeEntry, bool) {
	p := parser.NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(body), int64(len(body)), 0)))
	var out []bfRangeEntry
	for {
		if ctx.Err() != nil {
			return nil, false
		}
		tok, err := p.Peek(0)
		if err != nil {
			return nil, false
		}
		if tok.Type == lexer.TokenEOF {
			return out, true
		}
		var objs [3]ast.PdfObject
		for i := range objs {
			if objs[i], err = p.ParseObject(ctx); err != nil {
				return nil, false
			}
		}
		lo, ok1 := objs[0].(ast.PdfString)
		hi, ok2 := objs[1].(ast.PdfString)
		if !ok1 || !ok2 || len(lo) == 0 || len(lo) != len(hi) || len(lo) > maxRewriteCodeBytes {
			return nil, false
		}
		e := bfRangeEntry{lo: codeValue([]byte(lo)), hi: codeValue([]byte(hi)), width: len(lo)}
		switch d := objs[2].(type) {
		case ast.PdfString:
			e.dst = []byte(d)
		case ast.PdfArray:
			e.isArray = true
			e.dstArr = make([][]byte, 0, len(d))
			for _, el := range d {
				s, ok := el.(ast.PdfString)
				if !ok {
					return nil, false
				}
				e.dstArr = append(e.dstArr, []byte(s))
			}
		default:
			return nil, false
		}
		out = append(out, e)
	}
}

// codeValue reads a big-endian character code.
func codeValue(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

// conformEntries splits every entry that crosses a first-byte boundary. It
// reports false when no entry needed it, or when one spans more codes than
// [Parse] expands, since a rewrite could then not match the parse.
func conformEntries(entries []bfRangeEntry) ([]bfRangeEntry, bool) {
	out := make([]bfRangeEntry, 0, len(entries))
	split := false
	for _, e := range entries {
		// A reversed range maps nothing, and one inside a single first-byte
		// block already conforms: both are kept as written.
		if e.lo > e.hi || e.lo>>8 == e.hi>>8 {
			out = append(out, e)
			continue
		}
		if e.hi-e.lo >= maxRangeEntries {
			return nil, false
		}
		split = true
		out = appendConformingRuns(out, e)
	}
	return out, split
}

// appendConformingRuns appends e split into runs that each stay within one
// first-byte block and keep the destination's last byte from passing 255.
// Every run starts from the destination [Parse] reaches at that code, so the
// runs map exactly what e does.
func appendConformingRuns(out []bfRangeEntry, e bfRangeEntry) []bfRangeEntry {
	count := int(e.hi-e.lo) + 1 //nolint:gosec // e.hi-e.lo < maxRangeEntries, checked by conformEntries.
	dst := bytes.Clone(e.dst)
	start, startDst := 0, bytes.Clone(dst)
	flush := func(end int) {
		run := bfRangeEntry{lo: e.lo + uint64(start), width: e.width, isArray: e.isArray} //nolint:gosec // 0 <= start < count.
		if e.isArray {
			// Parse maps a code only while the array still holds a
			// destination for it, so the run ends where the array does.
			if start >= len(e.dstArr) {
				return
			}
			run.dstArr = e.dstArr[start:min(end, len(e.dstArr))]
			run.hi = run.lo + uint64(len(run.dstArr)) - 1
		} else {
			run.dst = startDst
			run.hi = e.lo + uint64(end) - 1 //nolint:gosec // 0 < end <= count.
		}
		out = append(out, run)
	}
	for i := 1; i < count; i++ {
		if !e.isArray {
			incrementBytes(dst)
		}
		newBlock := (e.lo+uint64(i))&0xFF == 0                           //nolint:gosec // 0 < i < count.
		dstWrapped := !e.isArray && len(dst) > 0 && dst[len(dst)-1] == 0 // the last byte just carried
		if newBlock || dstWrapped {
			flush(i)
			start, startDst = i, bytes.Clone(dst)
		}
	}
	flush(count)
	return out
}

// writeBfRangeGroups writes entries as bfrange groups of at most
// [maxBfRangeEntriesPerGroup] entries.
func writeBfRangeGroups(w *bytes.Buffer, entries []bfRangeEntry) {
	for i := 0; i < len(entries); i += maxBfRangeEntriesPerGroup {
		chunk := entries[i:min(i+maxBfRangeEntriesPerGroup, len(entries))]
		if i > 0 {
			w.WriteByte('\n')
		}
		fmt.Fprintf(w, "%d beginbfrange\n", len(chunk))
		for _, e := range chunk {
			fmt.Fprintf(w, "<%0*X> <%0*X> ", 2*e.width, e.lo, 2*e.width, e.hi)
			if !e.isArray {
				fmt.Fprintf(w, "<%X>\n", e.dst)
				continue
			}
			w.WriteByte('[')
			for j, v := range e.dstArr {
				if j > 0 {
					w.WriteByte(' ')
				}
				fmt.Fprintf(w, "<%X>", v)
			}
			w.WriteString("]\n")
		}
		w.WriteString("endbfrange")
	}
}
