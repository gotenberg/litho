package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// maxEndobjScanBytes bounds the window read while looking for an object's
// "endobj" keyword. It is an anti-DoS guard, not spec-mandated: the scan starts
// at an offset taken from the cross-reference table, so a crafted entry could
// otherwise point near the start of a huge file and have litho allocate and
// search all of it for every object. 16 KB is far larger than any real
// indirect object's header and payload preamble, and the window shrinks
// further whenever the next object's offset is nearer, so a conforming file
// never reaches the cap.
const maxEndobjScanBytes = 16384

// FindStartXRef scans the last 1024 bytes of the PDF for the startxref
// pointer and returns the byte offset it references.
func FindStartXRef(r io.ReaderAt, size int64) (int64, error) {
	bufSize := min(size, int64(1024))

	buf := make([]byte, bufSize)
	_, err := r.ReadAt(buf, size-bufSize)
	if err != nil {
		return 0, fmt.Errorf("reading tail bytes for startxref: %w", err)
	}

	idx := bytes.LastIndex(buf, []byte("startxref"))
	if idx == -1 {
		return 0, fmt.Errorf("startxref not found")
	}

	sub := buf[idx+len("startxref"):]
	// bytes.Reader actually implements io.ReaderAt
	br := bytes.NewReader(sub)
	lex := lexer.NewLexer(ioat.NewReader(br, int64(len(sub)), 0))
	tok, err := lex.NextToken()
	if err != nil {
		return 0, fmt.Errorf("scanning token after startxref: %w", err)
	}
	if tok.Type != lexer.TokenNumber {
		return 0, fmt.Errorf("expected offset after startxref, got %v", tok.Value)
	}

	offset, err := strconv.ParseInt(tok.Value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing startxref offset value %q: %w", tok.Value, err)
	}

	return offset, nil
}

// parseXRefChain locates the startxref pointer and walks the chain of xref
// tables/streams (via /Prev links) to populate d.Index.
func (d *Document) parseXRefChain(ctx context.Context) error {
	currentOffset, err := FindStartXRef(d.r, d.size)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDamagedXRef, err)
	}

	// A /Prev chain must terminate (ISO 32000-1 §7.5.8). A cyclic or
	// self-referential /Prev, from a crafted or corrupt file, would otherwise
	// re-parse the same xref section forever and hang the reader. Track visited
	// offsets and stop when one repeats, mirroring ResolveObject's cycle guard;
	// the entries parsed so far are kept (lenient recovery). The set bounds the
	// walk to the file's distinct offsets, so it is not a fixed anti-DoS cap.
	visited := make(map[int64]struct{})

	// The section furthest into the file answers the /ID rule, whichever order
	// the /Prev chain visits the sections in: see xref.Index.HighestOffsetTrailer.
	highestOffset := int64(-1)

	for currentOffset != 0 {
		if _, seen := visited[currentOffset]; seen {
			d.Logger.Debug("xref chain: /Prev cycle detected, stopping walk",
				"offset", currentOffset)
			break
		}
		visited[currentOffset] = struct{}{}

		lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, currentOffset))
		tok, err := lex.NextToken()
		if err != nil {
			return fmt.Errorf("reading token at xref offset %d: %w", currentOffset, err)
		}

		var prev int64
		switch {
		case tok.Type == lexer.TokenKeyword && tok.Value == "xref":
			d.XRefTableOffsets = append(d.XRefTableOffsets, currentOffset)
			lex = lexer.NewLexer(ioat.NewReader(d.r, d.size, currentOffset))
			// Snapshot the object numbers that already belong to newer
			// xref sections so this section's /XRefStm cannot override
			// them. Entries added by this section's plain xref table or
			// /XRefStm remain free to be updated by the stream.
			preExisting := make(map[int]struct{}, len(d.xrefEntries))
			for objNum := range d.xrefEntries {
				preExisting[objNum] = struct{}{}
			}
			trailerDict, p, trailerViolations, err := xref.ParseXRefTable(ctx, lex, d.xrefEntries)
			if err != nil {
				return fmt.Errorf("parsing xref table at offset %d: %w", currentOffset, err)
			}
			prev = p
			d.Index.LastTrailer = trailerDict
			if currentOffset > highestOffset {
				highestOffset = currentOffset
				d.Index.HighestOffsetTrailer = trailerDict
			}
			if d.Index.Trailer == nil {
				d.Index.Trailer = trailerDict
				d.trailerFormatViolations = trailerViolations
			} else {
				d.trailerMergedKeys = append(d.trailerMergedKeys, mergeTrailerKeys(d.Index.Trailer, trailerDict)...)
			}

			// Hybrid-Reference (PDF 1.5): the trailer may contain /XRefStm
			// pointing to a compressed XRef stream whose entries supplement
			// the legacy plain-text table in the same section. The stream
			// overrides entries added by this section's plain table but
			// leaves entries from newer sections (already in preExisting)
			// untouched so incremental updates keep their latest offsets.
			if xRefStmObj, ok := trailerDict["XRefStm"]; ok {
				if xRefStmOffset, ok := xRefStmObj.(ast.PdfInt); ok {
					if err := d.parseHybridXRefStream(ctx, int64(xRefStmOffset), preExisting); err != nil {
						return fmt.Errorf("hybrid /XRefStm: %w", err)
					}
				}
			}
		case tok.Type == lexer.TokenNumber:
			lex = lexer.NewLexer(ioat.NewReader(d.r, d.size, currentOffset))
			p := parser.NewParser(lex)
			obj, err := p.ParseObject(ctx)
			if err != nil {
				return fmt.Errorf("parsing xref stream object at offset %d: %w", currentOffset, err)
			}
			stream, ok := obj.(ast.PdfStream)
			if !ok {
				return fmt.Errorf("expected xref stream, got %T", obj)
			}
			pVal, err := xref.ParseXRefStream(ctx, stream, d.xrefEntries, d.builtinFilters())
			if err != nil {
				return fmt.Errorf("parsing xref stream at offset %d: %w", currentOffset, err)
			}
			prev = pVal
			d.Index.LastTrailer = stream.Dict
			if currentOffset > highestOffset {
				highestOffset = currentOffset
				d.Index.HighestOffsetTrailer = stream.Dict
			}
			if d.Index.Trailer == nil {
				d.Index.Trailer = stream.Dict
			} else {
				d.trailerMergedKeys = append(d.trailerMergedKeys, mergeTrailerKeys(d.Index.Trailer, stream.Dict)...)
			}
		default:
			return fmt.Errorf("unexpected token at xref offset: %v", tok.Value)
		}

		// A revision's %%EOF follows its cross-reference section whichever
		// form that section takes, so record both. Recording after the switch
		// keeps a section that failed to parse out of the list.
		d.XRefSectionOffsets = append(d.XRefSectionOffsets, currentOffset)

		currentOffset = prev
	}

	return nil
}

// pruneUnrepresentableEntries drops cross-reference entries whose object number
// the file is too small to have defined.
//
// Object numbers reach the index from three untrusted places, none of which
// bounds them: a classic subsection header, an xref stream's /Index, and the
// raw-byte rebuild scan. Bogus entries are inert on read, so such a file opens
// everywhere, but every write derives its next object number from the highest
// one in the index, so one bogus entry sends every appended object past a
// number other readers accept.
//
// See [Document.objectNumberFits] for the bound and why the file size supplies
// it. Compliance reads benefit too: an entry no file could hold was inflating
// the object count checked against the ISO 32000-1, Annex C limits.
//
// # Corpus evidence
//
//   - pdfjs GHOSTSCRIPT-698804-1-fuzzed.pdf: carries the header
//     "00000004294967296 3" in a 627-byte file. litho appended
//     "4294967299 0 obj" with a matching /Size, an object number past 2^32 that
//     qpdf refuses outright, turning a file both poppler and pikepdf accept
//     into one pikepdf rejects.
func (d *Document) pruneUnrepresentableEntries() {
	for objNum := range d.xrefEntries {
		if d.objectNumberFits(objNum) {
			continue
		}
		delete(d.xrefEntries, objNum)
		d.Logger.Debug("dropping xref entry whose object number the file cannot define",
			slog.Int("object_number", objNum),
			slog.Int64("file_size", d.size),
		)
	}
}

// objectNumberFits reports whether an object number is one a file could
// plausibly define, bounding it with [maxTrailerObjectCount].
//
// The bound is on the number itself, not on the file's length. An object number
// is a sparse identifier, not an index (ISO 32000-1, 7.3.10): a file may number
// five objects 8797 to 8801 and still be 8 kB, because the count of objects and
// the numbers they carry are independent. Bounding by byte length conflated the
// two and dropped every such object from [Document.HighestObjectNumber], so an
// incremental update allocated numbers already in use and wrote a /Size that
// cut the table short.
//
// A negative number is never valid. The cap stays as an anti-DoS guard so a
// corrupt xref cannot send allocation somewhere absurd; the entry count remains
// bounded by the file itself, since every entry costs bytes to list.
//
// # Corpus evidence
//
//   - pdfjs issue4573.pdf: 8474 bytes numbering objects up to 8801, for which
//     the byte-length bound answered 380. Rewriting the catalog at 8797 then
//     pushed the allocator to 8798 and the incremental trailer went out as
//     /Size 8798, which puts the page tree at 8798 and everything above it
//     past the end of the table. poppler read the file before litho touched it
//     and afterwards reported "Catalog dictionary does not contain a valid
//     Pages entry".
func (d *Document) objectNumberFits(objNum int) bool {
	if objNum < 0 {
		return false
	}
	return objNum <= maxTrailerObjectCount
}

// HighestObjectNumber returns the highest object number the file uses. A writer
// appending an incremental update allocates above it, so no new object can
// shadow one already in the bytes.
//
// The cross-reference index alone does not answer this, and the gap is ordinary
// rather than exotic: an xref stream need not list itself, so the object holding
// it is often absent from the very index it defines, and an allocation from the
// index alone lands straight over the file's /Type /XRef stream. A mislabelled
// subsection header opens the same gap wider, leaving defined objects looking
// free.
//
// The trailer /Size is one greater than the highest object number used (ISO
// 32000-1, Table 15), which covers exactly the objects the index missed. Taking
// the larger of the two is never worse than the index alone: a /Size that
// understates the file, which broken producers do emit, simply loses to the
// index.
//
// The file-size plausibility check that guards the index is deliberately NOT
// applied to /Size here. A file cannot DEFINE an object numbered above its byte
// count, which is why [Document.pruneUnrepresentableEntries] drops such entries,
// but it can REFERENCE one, and every caller of this method is choosing the next
// object number to WRITE. Rejecting such a /Size drops the estimate back to the
// index, and the update's own objects land on top of ones the file references.
//
// # Corpus evidence
//
//   - verapdf 6-1-4-t03-fail-a: defines objects 1-22 and indexes 1-21, and
//     litho appended its own object 22 straight over the file's /Type /XRef
//     stream.
//   - pdfjs bug1019475_1.pdf: lost object 586 the same way, to an appended
//     object written over its unlisted xref stream.
//   - pdfjs GHOSTSCRIPT-698804-1-fuzzed.pdf: a mislabelled subsection header
//     indexes only 0 and 1 while the file defines 1 through 6, leaving objects
//     2-4 looking free.
//   - pdfjs issue16091.pdf: the catalog names its page tree 156325 while the
//     trailer declares /Size 156342 in a 107 KB file, so the plausibility check
//     rejected /Size, the estimate fell back to the index, and the update's own
//     cross-reference stream was allocated at 156325, on top of the page tree
//     the catalog points at. Every reader then refused the output: poppler with
//     "Top-level pages object is wrong type (stream)" and qpdf with "unable to
//     find /Root dictionary", on a file both of them read before the edit.
func (d *Document) HighestObjectNumber() int {
	highest := 0
	// Under the read lock, because resolution registers objects it recovers by
	// scanning long after the index was built, on whichever goroutine missed.
	// For the same reason the number is bounded here as well as in the prune.
	d.mu.RLock()
	for objNum := range d.xrefEntries {
		if objNum > highest && d.objectNumberFits(objNum) {
			highest = objNum
		}
	}
	d.mu.RUnlock()
	if size, ok := d.trailerObjectCount(); ok && size-1 > highest {
		highest = size - 1
	}
	return highest
}

// ObjectGeneration returns the generation number the file gives an in-use
// object, or 0 when it defines no such object.
//
// A replacement written by an incremental update must carry it: an indirect
// reference names an object by number AND generation (ISO 32000-1, 7.3.10), so
// rewriting "3 1 obj" as "3 0 obj" leaves every "3 1 R" in the file pointing at
// an object that no longer answers. litho could not see this in its own output
// because its index is keyed by object number alone, which is deliberate
// leniency on read.
//
// A free entry reports 0. Its generation is the one to use if the number is
// ever reused (ISO 32000-1, 7.5.4), not the identity of a live object, and
// nothing references a free object.
//
// # Corpus evidence
//
//   - pdfjs bug1393476.pdf: came out of "litho rotate" reporting the new angle
//     to litho and none at all to poppler or pikepdf.
func (d *Document) ObjectGeneration(objNum int) int {
	entry, ok := d.XRefEntry(objNum)
	if !ok || entry.Free {
		return 0
	}
	return entry.Generation
}

// DefinesObject reports whether the cross-reference table lists objNum as a
// live object, whatever resolving it later yields.
//
// It separates an object the file never defines, which ISO 32000-1, 7.3.9 makes
// equivalent to null, from one the file does define but litho failed to load.
// The rewrite needs that distinction: the first has nothing to lose, the second
// loses whatever the object held. Free entries report false, since nothing
// references a free object.
func (d *Document) DefinesObject(objNum int) bool {
	entry, ok := d.XRefEntry(objNum)
	return ok && !entry.Free
}

// maxTrailerObjectCount bounds how far a trailer /Size may push the next object
// number litho allocates. This is an anti-DoS guard, not a spec limit: /Size is
// file-derived and so untrusted, and a corrupt one must not send object numbers
// somewhere absurd. 2^23 is orders of magnitude above the object count of any
// real document while still leaving room above any number a file could
// plausibly reference.
//
// # Corpus evidence
//
//   - All corpora: the largest fixture holds ~10^5 objects.
const maxTrailerObjectCount = 1 << 23

// trailerObjectCount returns the trailer /Size when it is a usable object count.
//
// Bounded by [maxTrailerObjectCount] rather than by the file size: the caller
// picks the next object number to write, and a file may reference an object
// number larger than its own byte count even though it cannot define one. See
// [Document.HighestObjectNumber].
func (d *Document) trailerObjectCount() (int, bool) {
	sizeObj, ok := d.Index.Trailer["Size"]
	if !ok {
		return 0, false
	}
	size, ok := sizeObj.(ast.PdfInt)
	if !ok || size <= 0 || size > maxTrailerObjectCount {
		return 0, false
	}
	// Bounded above by d.size, an int64 file size, so the conversion cannot
	// overflow an int on any platform litho builds for.
	return int(size), true
}

// mergeTrailerKeys copies /Root and /Info from an older trailer into the
// primary trailer when they are missing. This handles linearized PDFs where
// these keys may only appear in the linearization hint trailer.
// /ID and /Encrypt are NOT merged because their presence/absence in the
// primary trailer is compliance-significant (PDF/A 6.1.3). It returns the keys
// it copied, which [Document.TrailerAsWritten] leaves out.
func mergeTrailerKeys(dst, src ast.PdfDict) []ast.PdfName {
	var copied []ast.PdfName
	for _, k := range []ast.PdfName{"Root", "Info"} {
		if _, exists := dst[k]; !exists {
			if v, ok := src[k]; ok {
				dst[k] = v
				copied = append(copied, k)
			}
		}
	}
	return copied
}

// parseHybridXRefStream parses a compressed XRef stream embedded in a
// hybrid-reference PDF. Entries from the stream override entries in
// the legacy plain-text table from the same xref section so readers
// that understand compressed object streams always see the complete
// picture. Entries in skipLocked (object numbers registered by newer
// xref sections before this section was parsed) stay untouched so
// incremental updates keep their latest offsets.
func (d *Document) parseHybridXRefStream(ctx context.Context, offset int64, skipLocked map[int]struct{}) error {
	lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, offset))
	p := parser.NewParser(lex)
	obj, err := p.ParseObject(ctx)
	if err != nil {
		return fmt.Errorf("parsing hybrid xref stream object at offset %d: %w", offset, err)
	}

	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return fmt.Errorf("expected xref stream at /XRefStm offset %d, got %T", offset, obj)
	}

	hybridEntries := make(map[int]xref.XRefEntry)
	if _, err := xref.ParseXRefStream(ctx, stream, hybridEntries, d.builtinFilters()); err != nil {
		return fmt.Errorf("parsing hybrid xref stream entries at offset %d: %w", offset, err)
	}

	for objNum, entry := range hybridEntries {
		if _, locked := skipLocked[objNum]; locked {
			continue
		}
		d.xrefEntries[objNum] = entry
	}

	return nil
}

// ValidateObjectHeaderFormat performs raw-byte validation of the indirect
// object header at offset, reporting the violations found in byte order.
//
// Resolution validates a header only when it misses the cache, so an object
// already resolved during open escapes the check. Callers that need every
// object judged, rather than whichever ones happened to be resolved, iterate
// the cross-reference entries and call this directly. See
// [Document.ValidateXRefTableFormat] for the same arrangement on xref tables.
func (d *Document) ValidateObjectHeaderFormat(offset int64, objNum int) []error {
	return d.validateObjectHeaderFormat(offset, objNum)
}

// isPDFWhiteSpace reports whether c is one of the six white-space characters
// ISO 32000-1, Table 1 defines. The object header rule (ISO 19005-2 6.1.9 t1,
// ISO 19005-4 6.1.8 t1) asks for "a single white-space character" between the
// object number, the generation number and the obj keyword, not for a space.
//
// # Corpus evidence
//
//   - pdfjs colors.pdf: writes every header as "3\n0\nobj", which veraPDF
//     accepts and litho reported twice per object.
func isPDFWhiteSpace(c byte) bool {
	switch c {
	case 0x00, '\t', '\n', '\f', '\r', ' ':
		return true
	}
	return false
}

// validateObjectHeaderFormat performs raw-byte validation of an indirect object
// header "N G obj". The spec requires: object number and generation number
// separated by a single white-space character, generation number and obj
// keyword separated by a single white-space character, obj preceded by EOL,
// endobj preceded by EOL. It returns the violations found, in byte order.
//
// The findings are returned rather than recorded on the document so the
// caller records them once for the object, whatever the parse that follows
// them does.
func (d *Document) validateObjectHeaderFormat(offset int64, objNum int) []error {
	// Read enough bytes to cover the header: "NNNNNNNNN GGGGG obj\n" ≈ 25 bytes.
	var buf [40]byte
	n, _ := d.r.ReadAt(buf[:], offset)
	if n < 5 { //nolint:mnd // need at least "N G obj"
		return nil
	}
	data := buf[:n]

	var violations []error

	// Parse: <digits> <separator> <digits> <separator> "obj" <EOL>
	pos := 0

	// Object number: digits.
	for pos < len(data) && data[pos] >= '0' && data[pos] <= '9' {
		pos++
	}
	if pos == 0 || pos >= len(data) {
		return nil // Can't parse, skip.
	}

	// Separator between object number and generation: one white-space character.
	if !isPDFWhiteSpace(data[pos]) {
		return append(violations,
			parser.NewFormatViolation(ErrObjectHeaderFormat, fmt.Sprintf("compliance: obj header %d: object number not followed by a white-space character (0x%02X)", objNum, data[pos])))
	}
	wsCount := 0
	for pos < len(data) && isPDFWhiteSpace(data[pos]) {
		wsCount++
		pos++
	}
	if wsCount > 1 {
		violations = append(violations,
			parser.NewFormatViolation(ErrObjectHeaderFormat, fmt.Sprintf("compliance: obj header %d: %d white-space characters between object number and generation (must be 1)", objNum, wsCount)))
	}

	// Generation number: digits.
	genStart := pos
	for pos < len(data) && data[pos] >= '0' && data[pos] <= '9' {
		pos++
	}
	if pos == genStart || pos >= len(data) {
		return violations
	}

	// Separator between generation and "obj": one white-space character.
	if !isPDFWhiteSpace(data[pos]) {
		return append(violations,
			parser.NewFormatViolation(ErrObjectHeaderFormat, fmt.Sprintf("compliance: obj header %d: generation not followed by a white-space character (0x%02X)", objNum, data[pos])))
	}
	wsCount = 0
	for pos < len(data) && isPDFWhiteSpace(data[pos]) {
		wsCount++
		pos++
	}
	if wsCount > 1 {
		violations = append(violations,
			parser.NewFormatViolation(ErrObjectHeaderFormat, fmt.Sprintf("compliance: obj header %d: %d white-space characters between generation and obj keyword (must be 1)", objNum, wsCount)))
	}

	// "obj" keyword.
	if pos+3 > len(data) || string(data[pos:pos+3]) != "obj" {
		return violations // Not a valid object header, skip.
	}
	pos += 3

	// After "obj": must be followed by EOL marker (LF, CR, or CR+LF).
	if pos < len(data) {
		ch := data[pos]
		if ch != '\n' && ch != '\r' {
			violations = append(violations,
				parser.NewFormatViolation(ErrObjectHeaderFormat, fmt.Sprintf("compliance: obj header %d: obj keyword not followed by EOL marker (0x%02X)", objNum, ch)))
		}
	}

	// Validate endobj: scan forward to find "endobj" and check it's
	// preceded by an EOL marker (LF or CR).
	return append(violations, d.validateEndobjFormat(offset, objNum)...)
}

// validateEndobjFormat scans the object body starting at offset to find
// "endobj" and validates it is preceded by an EOL marker. It returns the
// violations found.
//
// The scan stops at the next object the index placed when the document was
// opened ([Document.objectStarts]), so "endobj" literals embedded in a later
// object's strings cannot be found. The raw-byte augmentation can move and add
// entries after that, at whichever lookup first misses the index, and a window
// taken from the live index made the verdict on an object depend on whether
// some earlier resolution had run it: a table the augmentation repairs can
// place an object inside the body of the one before it, and the scan of that
// one then ended short of its own endobj until the augmentation moved the
// entry. An offset that misses the header of the object listed there is
// stepped over instead, as [Document.objectEnd] steps over it, and
// [nextObjectHeader] still ends the window at the header the object really
// has.
//
// # Corpus evidence
//
//   - pdfjs issue15096.pdf: supersedes object 264, whose abandoned copy ends
//     "endobj " and was reported as object 263's until the window stopped at
//     the next object header.
//   - All corpora: forcing the augmentation once a file is open moves 5
//     objects and adds 465 across 470 files, and every object judged at the
//     same offset before and after it reports the same findings, whether the
//     window comes from the live index or the opening one. No PDF/A-1b or
//     PDF/A-2b violation moves.
func (d *Document) validateEndobjFormat(offset int64, objNum int) []error {
	// A binary search over the ascending offsets keeps this O(log N) per
	// object; scanning every entry here would be O(N^2) across a full pass on
	// a file with many objects.
	scanLimit := int64(maxEndobjScanBytes)
	if next, ok := d.nextObjectStart(d.objectStarts, offset); ok && next-offset < scanLimit {
		scanLimit = next - offset
	}
	buf := make([]byte, scanLimit)
	n, _ := d.r.ReadAt(buf, offset)
	if n < len("endobj")+4 { //nolint:mnd // too short to hold "endobj" plus its EOL marker
		return nil
	}
	data := buf[:n]

	// The cap above is the next offset the cross-reference table lists, which
	// an incremental update can leave far past this object: a superseded
	// definition keeps its bytes in the file but loses its xref entry, so the
	// window runs on through it. Stop at the next object header instead, so the
	// backward scan below cannot reach a dead object's "endobj" and report it
	// against this one.
	if hdr := nextObjectHeader(data); hdr > 0 {
		data = data[:hdr]
	}

	// Scan backward for "endobj". Since `data` stops at the start of
	// the next object the last endobj in the buffer belongs to this
	// object, not a neighbour; embedded string occurrences earlier in
	// the body are skipped over.
	endobjIdx := -1
	for i := len(data) - 6; i >= 0; i-- { //nolint:mnd // 6 = len("endobj"), the keyword matched just below.
		if string(data[i:i+6]) == "endobj" {
			endobjIdx = i
			break
		}
	}
	if endobjIdx < 1 {
		return nil // Not found or at position 0.
	}

	var violations []error

	// Check that endobj is preceded by EOL (LF or CR).
	prevByte := data[endobjIdx-1] //nolint:gosec // endobjIdx >= 2 guaranteed by the check above
	if prevByte != '\n' && prevByte != '\r' {
		violations = append(violations,
			parser.NewFormatViolation(ErrObjectHeaderFormat, fmt.Sprintf("compliance: obj %d: endobj not preceded by EOL marker (0x%02X)", objNum, prevByte)))
	}

	// Check that endobj is followed by EOL (LF or CR) or EOF.
	afterIdx := endobjIdx + 6 //nolint:mnd // 6 = len("endobj"): step past the keyword just matched.
	if afterIdx < len(data) {
		afterByte := data[afterIdx]
		if afterByte != '\n' && afterByte != '\r' {
			violations = append(violations,
				parser.NewFormatViolation(ErrObjectHeaderFormat, fmt.Sprintf("compliance: obj %d: endobj not followed by EOL marker (0x%02X)", objNum, afterByte)))
		}
	}

	return violations
}

// nextObjectHeader returns the offset of the first indirect-object header
// ("N G obj" on its own line) that starts after the one at the beginning of
// data, or -1 when data holds only one object.
//
// Per ISO 32000-1, 7.3.10 the header is two non-negative integers and the obj
// keyword, and 7.5.5 puts an EOL marker before the object number, which is what
// makes the pattern specific enough to look for in bytes that may include
// stream data. Only a header preceded by "endobj" and an EOL is accepted, so a
// digit sequence inside a stream cannot end the window early.
func nextObjectHeader(data []byte) int {
	const keyword = "endobj"
	for i := 0; ; {
		next := bytes.Index(data[i:], []byte(keyword))
		if next < 0 {
			return -1
		}
		pos := i + next + len(keyword)
		i = pos
		// Step over the end-of-line (and any spaces a lax producer wrote)
		// separating this object from the next.
		j := pos
		for j < len(data) && (data[j] == '\r' || data[j] == '\n' || data[j] == ' ' || data[j] == '\t') {
			j++
		}
		if j == pos || j >= len(data) {
			continue // No separator, so nothing can start here.
		}
		if isObjectHeaderAt(data[j:]) {
			return j
		}
	}
}

// isObjectHeaderAt reports whether data begins with "N G obj".
func isObjectHeaderAt(data []byte) bool {
	digits := func(p int) int {
		start := p
		for p < len(data) && data[p] >= '0' && data[p] <= '9' {
			p++
		}
		if p == start {
			return -1
		}
		return p
	}
	spaces := func(p int) int {
		start := p
		for p < len(data) && (data[p] == ' ' || data[p] == '\r' || data[p] == '\n' || data[p] == '\t') {
			p++
		}
		if p == start {
			return -1
		}
		return p
	}
	p := digits(0)
	if p < 0 {
		return false
	}
	if p = spaces(p); p < 0 {
		return false
	}
	if p = digits(p); p < 0 {
		return false
	}
	if p = spaces(p); p < 0 {
		return false
	}
	return bytes.HasPrefix(data[p:], []byte("obj"))
}

// ValidateXRefTableFormat performs raw-byte validation of a traditional xref
// table's format and returns the violations found, in byte order. It reads
// directly from the underlying io.ReaderAt to check byte-level formatting
// that the lenient lexer-based parser cannot detect. Called by the compliance
// engine after ComplianceMode is set.
//
// The findings belong to a table, not to an object, so they are returned to
// the caller instead of joining the per-object record. Handing them back also
// spares the caller from reading them back out of shared state that a
// concurrent resolution is still writing.
func (d *Document) ValidateXRefTableFormat(offset int64) []error {
	// Read a generous chunk from the xref table start.
	// Most xref tables fit in 64KB, and only the header and first few
	// entries need validating to detect format issues.
	const maxRead = 65536
	buf := make([]byte, maxRead)
	n, _ := d.r.ReadAt(buf, offset)
	if n < 5 { //nolint:mnd // "xref" + at least 1 byte
		return nil
	}
	buf = buf[:n]

	// Must start with "xref".
	if !bytes.HasPrefix(buf, []byte("xref")) {
		return nil
	}
	pos := 4

	var violations []error

	// After "xref": must be a single EOL marker (\n or \r\n).
	if pos >= len(buf) {
		return nil
	}
	switch buf[pos] {
	case '\r':
		pos++
		if pos < len(buf) && buf[pos] == '\n' {
			pos++
		}
		// \r alone is acceptable as EOL here.
	case '\n':
		pos++
	default:
		return append(violations,
			parser.NewFormatViolation(ErrXRefEOLMarkers, "compliance: xref keyword not followed by EOL"))
	}

	// The single EOL marker must be followed by the subsection header, whose
	// first byte is a digit. Anything else breaks the single EOL marker between
	// the keyword and the header (ISO 19005-1, 6.1.4 t2; ISO 19005-2, 6.1.4
	// t2), not the spacing inside the header (ISO 19005-1, 6.1.4 t1): veraPDF
	// reads one EOL marker after the keyword and fails xrefEOLMarkersComplyPDFA
	// when no digit follows it (PDFParser.parseXrefTable). Measured with
	// veraPDF 1.30 at PDF/A-1b and PDF/A-2b: a second EOL marker, a space or a
	// tab, a form feed or a NUL after the marker each fail that test, and so
	// does a table that holds no subsection at all, its trailer keyword
	// following the marker. The test is one verdict on the bytes between the
	// keyword and the header, so it is stated once.
	//
	// Whatever white space stands there is skipped: leaving pos on it makes
	// the subsection header scan below read no digits and stop, so this defect
	// would mask a defect in the header that follows.
	if pos < len(buf) && (buf[pos] < '0' || buf[pos] > '9') {
		msg := "compliance: xref keyword not followed by a subsection header"
		switch {
		case buf[pos] == '\n' || buf[pos] == '\r':
			msg = "compliance: xref keyword and subsection header separated by multiple EOL markers"
		case isPDFWhiteSpace(buf[pos]):
			msg = "compliance: xref keyword and subsection header separated by extra whitespace"
		}
		violations = append(violations, parser.NewFormatViolation(ErrXRefEOLMarkers, msg))
		for pos < len(buf) && isPDFWhiteSpace(buf[pos]) {
			pos++
		}
	}

	// Parse subsection headers and validate format.
	for pos < len(buf) {
		// Check for "trailer" keyword, end of xref table.
		if pos+7 <= len(buf) && string(buf[pos:pos+7]) == "trailer" {
			break
		}

		// Subsection header: <digits> <SP> <digits> <EOL>
		// The start object number and count must be separated by a single SPACE (0x20).
		headerStart := pos

		// Read start object number (digits).
		for pos < len(buf) && buf[pos] >= '0' && buf[pos] <= '9' {
			pos++
		}
		if pos == headerStart {
			break // Not a digit, likely end of table or malformed.
		}

		// Must be exactly one space (0x20).
		if pos >= len(buf) {
			break
		}
		if buf[pos] != ' ' {
			violations = append(violations,
				parser.NewFormatViolation(ErrXRefSubsectionSpacing, fmt.Sprintf("compliance: xref subsection header separator is not a single space (0x%02X)", buf[pos])))
			break
		}
		sepCount := 0
		for pos < len(buf) && buf[pos] == ' ' {
			sepCount++
			pos++
		}
		if sepCount > 1 {
			violations = append(violations,
				parser.NewFormatViolation(ErrXRefSubsectionSpacing, fmt.Sprintf("compliance: xref subsection header has %d spaces between start and count (must be 1)", sepCount)))
		}

		// Read count (digits).
		countStart := pos
		for pos < len(buf) && buf[pos] >= '0' && buf[pos] <= '9' {
			pos++
		}
		if pos == countStart {
			break
		}
		count, err := strconv.Atoi(string(buf[countStart:pos]))
		if err != nil || count < 0 {
			break
		}

		// Skip EOL after header.
		if pos < len(buf) && buf[pos] == '\r' {
			pos++
			if pos < len(buf) && buf[pos] == '\n' {
				pos++
			}
		} else if pos < len(buf) && buf[pos] == '\n' {
			pos++
		}

		// Skip past the entries (each is 20 bytes).
		// Validate individual entries would be ideal but keeping it simple.
		// the subsection header format check catches the most common violations.
		entryBytes := count * 20 //nolint:mnd // 20 bytes per xref entry
		if pos+entryBytes > len(buf) {
			break // Not enough data to skip all entries.
		}
		pos += entryBytes
	}

	return violations
}
