package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// matchLZW claims the LZWDecode ban in every spelling the parts give it.
//
// One requirement, four clause numbers: ISO 19005-1 spells it 6.1.10, ISO
// 19005-2 and 19005-3 spell the stream-filter half 6.1.7.2, and ISO 19005-4
// spells it 6.1.6.2. Only 6.1.10 was claimed, so a PDF/A-2, -3 or -4 target
// kept an LZWDecode stream that [fixLZW] already knew how to re-encode.
//
// Exact equality, never clausePrefix: "6.1.7" would swallow 6.1.7.1, the
// unrelated stream-length and stream-keyword-EOL rule that the full rewrite
// handles.
//
// 6.1.7.2 and 6.1.6.2 forbid more than LZWDecode: a filter absent from ISO
// 32000 Table 6, and a Crypt filter whose decode parameters name anything but
// Identity. [fixLZW] now also repairs the first of those in its narrowest form,
// a Table 6 name misspelled by CASE ([fixFilterNameCase]); a Crypt filter with
// non-Identity parameters is still unrepaired and stays in the residual rather
// than being silenced.
//
// The inline-image spellings stay outside this matcher's reach on purpose:
// they name the Inline Image /F key, and rewriting a BI...ID...EI sequence is
// beyond what re-encoding stream objects can do. Every part has one, and they
// collide with the stream-filter spellings rather than sitting clear of them:
// ISO 19005-1 splits 6.1.10 by TEST NUMBER (t01 the stream filter, t02 the
// inline image, carrying the SAME description word for word), parts 2 and 3
// give 6.1.10 entirely to the inline image while their stream filter is
// 6.1.7.2, and part 4 calls it 6.1.9.
//
// So matching the clause alone claimed three rules this fixer documents itself
// as unable to repair, and matching Specification and Clause together would
// still claim ISO 19005-1 t02. A clause number is not a rule id: the identity
// is Specification, Clause and TestNumber together, which is why this is an
// explicit allow-list rather than a set of literals. Comparing descriptions
// would not separate part 1's two either.
func matchLZW(v compliance.Violation) bool {
	spec, clause, test := v.Rule.Specification, v.Rule.Clause, v.Rule.TestNumber
	switch {
	case spec == "ISO_19005_1" && clause == "6.1.10" && test == 1:
		return true
	case spec == "ISO_19005_2" && clause == "6.1.7.2" && test == 1:
		return true
	case spec == "ISO_19005_3" && clause == "6.1.7.2" && test == 1:
		return true
	case spec == "ISO_19005_4" && clause == "6.1.6.2" && test == 1:
		return true
	}
	return false
}

// fixLZW re-encodes every LZWDecode stream as FlateDecode, and normalises a
// stream /Filter name that differs from a Table 6 spelling only by case.
//
// Both repairs answer the same clause. The LZW half rewrites the bytes; the
// case half rewrites only the name, and only when the bytes prove it right.
func fixLZW(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	var count int
	renamed := make(map[int]string)

	for objNum, entry := range doc.Index.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Free {
			continue
		}

		ref := ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation}
		resolved, err := doc.Resolve(ctx, ref)
		if err != nil {
			continue
		}

		stream, ok := resolved.(ast.PdfStream)
		if !ok || stream.Dict == nil {
			continue
		}

		filterObj, ok := stream.Dict["Filter"]
		if !ok {
			continue
		}

		isLZW := false
		switch f := filterObj.(type) {
		case ast.PdfName:
			isLZW = f == "LZWDecode" || f == "LZW"
		case ast.PdfArray:
			if len(f) == 1 {
				if name, ok := f[0].(ast.PdfName); ok {
					isLZW = name == "LZWDecode" || name == "LZW"
				}
			}
		}

		if !isLZW {
			if desc := fixFilterNameCase(ctx, alloc, doc, objNum, stream, filterObj); desc != "" {
				renamed[objNum] = desc
			}
			continue
		}

		decoded, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			continue
		}

		var compressed bytes.Buffer
		w := zlib.NewWriter(&compressed)
		if _, err := w.Write(decoded); err != nil {
			continue
		}
		if err := w.Close(); err != nil {
			continue
		}

		newDict := make(ast.PdfDict, len(stream.Dict))
		maps.Copy(newDict, stream.Dict)
		newDict["Filter"] = ast.PdfName("FlateDecode")
		delete(newDict, "DecodeParms")
		newDict["Length"] = ast.PdfInt(compressed.Len())

		alloc.AllocAt(objNum, ast.NewStream(newDict, compressed.Bytes()))
		count++
	}

	var fixes []string
	if count > 0 {
		fixes = append(fixes, fmt.Sprintf("re-encoded %d LZW streams as FlateDecode", count))
	}
	// Sorted: doc.Index.Entries is a map, and identical input must produce
	// identical output.
	for _, objNum := range slices.Sorted(maps.Keys(renamed)) {
		fixes = append(fixes, renamed[objNum])
	}
	return fixes, nil
}

// canonicalStreamFilters lists the stream filter names ISO 32000-2, Table 6
// defines, which ISO 19005-2/3 §6.1.7.2 and ISO 19005-4 §6.1.6.2 restrict a
// conforming file to. It mirrors compliance's standardFilters, which is the
// authority for the verdict; this copy supplies only the canonical SPELLING to
// repair towards, so the two must stay in sync.
//
//nolint:gochecknoglobals // static spec table, mirrors compliance.standardFilters
var canonicalStreamFilters = []ast.PdfName{
	"ASCIIHexDecode",
	"ASCII85Decode",
	"LZWDecode",
	"FlateDecode",
	"RunLengthDecode",
	"CCITTFaxDecode",
	"JBIG2Decode",
	"DCTDecode",
	"JPXDecode",
	"Crypt",
}

// canonicalFilterSpelling returns the Table 6 name that differs from name only
// by case. A name already spelled canonically yields false: there is nothing to
// repair, which is what keeps this away from the LZWDecode streams the caller
// re-encodes instead.
//
// PDF names are case-sensitive (ISO 32000-1, 7.3.5), so /Flatedecode is not
// /FlateDecode. It is "a filter not listed in Table 6", exactly what the clause
// forbids, and compliance's own comment names this case as the example.
func canonicalFilterSpelling(name ast.PdfName) (ast.PdfName, bool) {
	for _, want := range canonicalStreamFilters {
		if name == want {
			return "", false
		}
		if strings.EqualFold(string(name), string(want)) {
			return want, true
		}
	}
	return "", false
}

// fixFilterNameCase rewrites a stream /Filter whose name differs from a Table 6
// spelling only by case, but ONLY when the stream actually decodes under the
// canonical name. The decode is the proof; the name alone is not.
//
// The check must be [filter.DecodeStreamChecked], never a nil error from
// DecodeStream. Decoding is best-effort: an unsupported filter is SKIPPED and
// its bytes pass through unchanged, so DecodeStream reports success having
// decoded nothing.
//
// Only a single-name /Filter is repaired. An array would need each element
// verified in chain order, and no real file exercises that code.
//
// # Corpus evidence
//
//   - verapdf 6-1-6-2-t01-fail-b: on object 11, DecodeStream returned err=nil
//     for /Flatedecode with the raw 37 bytes untouched, byte for byte.
//   - All corpora: no file carries a case-misspelled name inside a /Filter
//     array, so the gate could not falsify an array repair.
func fixFilterNameCase(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, objNum int, stream ast.PdfStream, filterObj ast.PdfObject) string {
	name, ok := filterObj.(ast.PdfName)
	if !ok {
		return ""
	}
	canonical, ok := canonicalFilterSpelling(name)
	if !ok {
		return ""
	}

	newDict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(newDict, stream.Dict)
	newDict["Filter"] = canonical

	// The raw bytes carry across unchanged, so /Length stays correct.
	candidate := ast.NewLazyStream(newDict, stream.Bytes)
	// The built-in decoders under the document's budgets: the proof is that
	// litho decodes the canonical name, not that a plugin claims it.
	if _, incomplete, err := filter.NewDecoder(nil, doc.Limits()).DecodeStreamChecked(ctx, candidate); err != nil || incomplete {
		return ""
	}

	alloc.AllocAt(objNum, candidate)
	return fmt.Sprintf("renamed non-standard filter /%s to /%s on object %d", name, canonical, objNum)
}
