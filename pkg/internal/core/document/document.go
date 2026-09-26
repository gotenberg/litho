package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// Document represents a parsed PDF document. It holds a reference to the
// underlying data stream, the cross-reference index, and a cache of
// resolved objects to avoid re-parsing.
//
// Concurrent use. Document is safe for concurrent use by multiple
// goroutines after construction completes. All mutable caches
// (objectCache, fontCache, cmapCache, objStmCache, fontProgCache, colorCache),
// the cross-reference entries, which a resolution that misses them rewrites
// (see [Document.XRefEntries]), and the compliance scratch state
// (ComplianceMode, formatViolations) are protected by internal locks. The format-violation record is keyed by object number
// and written once per object, so concurrent resolutions of the same
// object contribute one entry, not one per goroutine.
// Concurrent read paths ([Document.Resolve], [ResolveDict], [ResolveArray], etc.)
// run in parallel through a read-write mutex. Concurrent validation
// runs ([pdf.Reader.ValidatePDFA] / [pdf.Reader.ValidatePDFUA] on the
// same underlying Document) are serialized through a dedicated run
// mutex so that one validator cannot observe another's transient
// compliance-mode state.
//
// The [Document.Authenticate] method must be called before concurrent
// use begins; it is part of the setup phase in [NewDocument] and
// [pdf.Open] and is not safe to call in parallel with reads.
//
// The resource budgets ([Document.Limits]) are set by [NewDocument] and
// only read afterwards, so they need no lock.
type Document struct {
	r                io.ReaderAt
	size             int64
	Index            xref.Index
	xrefEntries      map[int]xref.XRefEntry // cross-reference entries by object number, see [Document.XRefEntries]
	objectCache      map[int]ast.PdfObject
	fontCache        map[int]any
	cmapCache        map[int]any         // parsed CMap by ToUnicode stream object number
	objStmCache      map[int]*objStmData // parsed object streams by ObjStm object number
	objStmCacheBytes int                 // decoded bytes held in objStmCache, bounded by limits.Limits.MaxObjStmCacheBytes
	fontProgCache    map[int][]byte      // decoded font programs by FontFile stream object number
	fontProgBytes    int                 // decoded bytes held in fontProgCache, bounded by limits.Limits.MaxFontProgCacheBytes
	colorCache       map[[2]int]any      // colour transforms by profile object number and rendering intent
	colorCacheBytes  int                 // bytes held in colorCache, bounded by limits.Limits.MaxColorCacheBytes
	mu               sync.RWMutex        // protects objectCache, fontCache, cmapCache, objStmCache, fontProgCache, colorCache, formatViolations and xrefEntries once construction completes
	runMu            sync.Mutex          // serializes compliance-engine validation runs
	xrefAugment      sync.Once           // guards the one-shot raw-bytes augmentation of xrefEntries
	xrefAugmented    bool                // the augmentation added or overrode an entry; protected by mu
	xrefAdded        map[int]bool        // object numbers the augmentation added, absent from the index as opened; protected by mu
	maxCacheSize     int                 // evict all objects when cache exceeds this size
	limits           limits.Limits       // resource budgets, set by NewDocument, read-only after
	Crypto           *crypto.Handler
	streamCrypt      ast.PdfName // the /StmF crypt filter Crypto decrypts streams with, set by NewDocument, read-only after
	OCConfig         *OCConfig
	IsEncrypted      bool
	Authenticated    bool

	// Logger is the structured logger used for debug output. It is never nil;
	// when the caller does not supply a logger, a no-op logger that discards
	// all output is used.
	Logger *slog.Logger

	// Linearization holds the linearization parameters if the document is
	// linearized (fast web view). Nil for non-linearized documents.
	Linearization *LinearizationParams

	// HintTable holds the per-page offset entries decoded from the hint
	// stream of a linearized PDF. Nil for non-linearized documents.
	HintTable []PageOffsetEntry

	// Rebuilt is true when the cross-reference table was reconstructed
	// by scanning the file for object headers because the original xref
	// data was damaged or missing.
	Rebuilt bool

	// TrailerSynthesized is true when no trailer dictionary could be recovered
	// from the file and [Document.Index].Trailer holds one litho made up instead. Its
	// entries are litho's own, derived from whatever the raw scan found, so a
	// caller that reports on what the file contains must not read them: a
	// 219-byte damaged file yields /Size 2147483648 from a bogus object number
	// the scan picked up, which is a value the file does not contain.
	TrailerSynthesized bool

	// HeaderOffset is the physical byte position of the %PDF- header,
	// non-zero when junk precedes it. Readers disagree on the offset
	// base for such files: litho resolves xref offsets from the physical
	// file start, while Acrobat and veraPDF resolve them relative to the
	// header. Writers must not extend such files incrementally, since
	// any appended section is wrong for one of the two conventions.
	HeaderOffset int64

	// Version is the PDF version string extracted from the %PDF-X.Y header
	// (e.g., "1.4", "1.7", "2.0").
	Version string

	// complianceMode enables parser-level format violation collection.
	// When true, parsers created during object resolution collect hex
	// string format issues and other structural violations without
	// failing the parse. Accessed atomically so [Document.Resolve] can check it
	// without blocking on a mutex on every call. Toggled by
	// [Document.SetComplianceMode].
	complianceMode atomic.Bool

	// formatViolations records the parser-level format violations found on
	// each object, keyed by object number, by a parse in compliance mode or
	// by [Document.CheckObjectFormat]. Recording is idempotent per object (see
	// [Document.RecordFormatViolations]), so the set is a function of the
	// input bytes rather than of how often an object happened to be parsed.
	// Access is serialized by d.mu.
	//
	// The key space is the set of object numbers that carry an in-use xref
	// entry, uncompressed or held in an object stream, so the map holds at
	// most len(xrefEntries) keys: an object number absent from the index
	// fails resolution before anything is recorded. That is the same bound
	// the index itself already lives under (an xref entry costs bytes in the
	// file), so the store needs no cap of its own.
	formatViolations map[int][]error

	// integerOverflows records, per object number, the integer literals past
	// the 64-bit range that the object's parse read as reals (see
	// [parser.Parser.IntegerOverflows]). It is recorded in every mode, the
	// first recording for an object wins, and only objects that hold one get
	// a key, so it lives under the same bound as formatViolations. Access is
	// serialized by d.mu.
	integerOverflows map[int][]float64

	// objectStarts lists, by ascending offset, where the index places each
	// in-use uncompressed object once construction has read it from the table
	// or rebuilt it, before any resolution can augment it from a raw-byte scan.
	// [Document.validateEndobjFormat] reads it to bound its scan. Written once
	// during construction and read-only thereafter.
	objectStarts []objectStart

	// declaredStarts is objectStarts when the index is the cross-reference
	// table the file declares, and empty for a rebuilt index, which has no
	// table behind it. [Document.objectEnd] reads it to bound a parse. Written
	// once during construction and read-only thereafter.
	declaredStarts []objectStart

	// XRefTableOffsets records the byte offsets of traditional xref tables
	// encountered during parsing. Written once during construction and
	// read-only thereafter.
	XRefTableOffsets []int64

	// XRefSectionOffsets records the byte offset of every cross-reference
	// section the /Prev chain visits, whether that section is a traditional
	// xref table or a cross-reference stream (ISO 32000-1, 7.5.8). Only
	// sections that parsed are recorded. Written once during construction
	// and read-only thereafter.
	//
	// [Document.XRefTableOffsets] lists the traditional tables alone, because
	// its readers parse the raw bytes that follow the xref keyword. Anything
	// that only needs to know where a revision ends wants this field: a file
	// written with cross-reference streams has no traditional table at all.
	XRefSectionOffsets []int64

	// Filters decodes the document's streams: it holds the registered filter
	// plugins, if any, and bounds every decode by the document's budgets.
	// [NewDocument] sets it with no plugin, and pdf.Open replaces it once,
	// before concurrent use begins, when plugins are registered; read-only
	// after. A nil Filters consults no plugin and decodes under the default
	// budgets.
	Filters *filter.Decoder

	// CMYKProfile is the ICC profile DeviceCMYK colours are read in when the
	// document states no output intent for them. Nil selects litho's
	// built-in profile. Set once during construction, then read-only.
	CMYKProfile []byte
}

// ComplianceMode reports whether parser-level format violation
// collection is enabled. Reads are lock-free via an atomic load so they
// can run on the hot [Document.Resolve] path without contention.
func (d *Document) ComplianceMode() bool {
	return d.complianceMode.Load()
}

// SetComplianceMode toggles parser-level format violation collection.
// The compliance engine turns it on at the start of a validation run
// and off at the end (through its run mutex), so cross-goroutine
// observers of [Document.ComplianceMode] may see transient true values
// only during a concurrent validation.
func (d *Document) SetComplianceMode(enabled bool) {
	d.complianceMode.Store(enabled)
}

// RecordFormatViolations records the format violations found while parsing
// object objNum. The first recording for an object wins: a later call for
// the same object is dropped, including when it carries a different list.
// An object is parsed more than once whenever its parse fails (a failed
// parse is never cached), whenever its cache entry is evicted, and whenever
// two goroutines miss the cache for it at the same time, so counting per
// parse made the reported violation set depend on scheduling. Counting per
// object makes it a function of the input bytes.
//
// Recording an empty list still marks the object as visited, so a later
// re-parse cannot add anything either.
//
// RecordFormatViolations is safe for concurrent use.
func (d *Document) RecordFormatViolations(objNum int, violations ...error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.formatViolations == nil {
		d.formatViolations = make(map[int][]error)
	}
	if _, ok := d.formatViolations[objNum]; ok {
		return
	}
	d.formatViolations[objNum] = slices.Clone(violations)
}

// FormatViolations returns the recorded format violations ordered by object
// number, then by the order in which they were found within that object.
// The order never depends on map iteration or on the order the objects were
// resolved in (CONTRIBUTING "Deterministic output").
//
// The result is a fresh slice, so callers may hold it while resolution
// continues. FormatViolations is safe for concurrent use.
func (d *Document) FormatViolations() []error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.formatViolationsLocked()
}

// ObjectFormatViolations returns the format violations recorded for object
// objNum, in the order they were found, or nil when none were. The first
// recording for an object wins (see [Document.RecordFormatViolations]), so
// once the object has been parsed in compliance mode the list no longer
// changes. The result is a fresh slice. ObjectFormatViolations is safe for
// concurrent use.
func (d *Document) ObjectFormatViolations(objNum int) []error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return slices.Clone(d.formatViolations[objNum])
}

// recordIntegerOverflows records the integer literals past the 64-bit range
// that the parse of object objNum read as reals. The first recording for an
// object wins, as in [Document.RecordFormatViolations], so the record is a
// function of the input bytes. An empty list records nothing.
func (d *Document) recordIntegerOverflows(objNum int, values []float64) {
	if len(values) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.integerOverflows == nil {
		d.integerOverflows = make(map[int][]float64)
	}
	if _, ok := d.integerOverflows[objNum]; ok {
		return
	}
	d.integerOverflows[objNum] = slices.Clone(values)
}

// IntegerOverflows returns the integer literals past the 64-bit range that the
// parse of object objNum read as reals, in parse order, or nil when it held
// none or has not been parsed. The result is a fresh slice. IntegerOverflows
// is safe for concurrent use.
func (d *Document) IntegerOverflows(objNum int) []float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return slices.Clone(d.integerOverflows[objNum])
}

// ResetFormatViolations drops every recorded format violation and returns
// the previous contents in [Document.FormatViolations] order.
//
// Recording is repopulated by parsing, so reset the record together with
// the object cache: an object still in the cache is parsed again only by
// [Document.CheckObjectFormat], one extra parse per cached object.
// [Document.ClearCaches] and [Document.Authenticate] reset both at once for
// that reason. A validation pass does not reset on its own, since the next
// pass over the same Document would then parse every cached object a second
// time to record the same findings.
func (d *Document) ResetFormatViolations() []error {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := d.formatViolationsLocked()
	d.formatViolations = nil
	return out
}

// formatViolationsLocked returns the ordered violations. The caller must
// hold d.mu.
func (d *Document) formatViolationsLocked() []error {
	out := make([]error, 0, len(d.formatViolations))
	for _, objNum := range slices.Sorted(maps.Keys(d.formatViolations)) {
		out = append(out, d.formatViolations[objNum]...)
	}
	return out
}

// LockValidationRun acquires the document's validation run mutex and
// returns a release function. Callers that kick off a compliance
// validation pass use this to serialize runs on the same Document so
// that transient per-run state (ComplianceMode, FormatViolations) is
// not corrupted by a concurrent second validator.
func (d *Document) LockValidationRun() func() {
	d.runMu.Lock()
	return d.runMu.Unlock
}

// DecodeStream decodes a [ast.PdfStream] through the document's filter pipeline.
// Context is checked between decompression chunks to allow cancellation.
func (d *Document) DecodeStream(ctx context.Context, stream ast.PdfStream) ([]byte, error) {
	data, _, err := d.DecodeStreamReport(ctx, stream)
	return data, err
}

// DecodeStreamReport is [Document.DecodeStream] that also reports which
// filters left the returned bytes short of the whole decoded stream, with the
// contract of [filter.Decoder.DecodeStreamReport].
func (d *Document) DecodeStreamReport(ctx context.Context, stream ast.PdfStream) ([]byte, filter.DecodeReport, error) {
	stream = d.resolveStreamFilterKeys(ctx, stream)
	// A nil d.Filters consults no plugin, so one call covers both paths and
	// a skipped filter is logged whether or not plugins are registered.
	data, report, err := d.Filters.DecodeStreamReport(ctx, stream)
	if report.Incomplete() {
		// The bytes are only partially decoded: a skipped filter left them
		// filter-encoded, or a filter met damaged data and handed on what it
		// decoded before the damage. Surface it at Debug rather than silently
		// returning them as the whole stream.
		d.Logger.Debug("stream only partially decoded",
			slog.Any("skipped", report.Skipped), slog.Any("damaged", report.Damaged))
	}
	return data, report, err
}

// DecodeStreamUpTo decodes stream through its filter chain, stopping before
// the first stop filter, and returns the bytes that filter itself receives.
// found reports whether stop appears in the chain. See
// [filter.DecodeStreamUpTo] for why an image-format check reads its filter's
// input rather than the raw or fully decoded bytes.
//
// The filters this skips past are the built-in ones (a JPEG2000 or JBIG2
// codestream is what a chain ends with, never what it passes through), so the
// document's plugins are not consulted and the result does not vary with
// which optional plugins are registered. The document's budgets still apply.
func (d *Document) DecodeStreamUpTo(ctx context.Context, stream ast.PdfStream, stop ast.PdfName) ([]byte, bool, error) {
	stream = d.resolveStreamFilterKeys(ctx, stream)
	return d.Filters.DecodeStreamUpTo(ctx, stream, stop)
}

// NewStreamReader returns a lazy [io.ReadCloser] using the document's filter decoder.
func (d *Document) NewStreamReader(ctx context.Context, stream ast.PdfStream) (io.ReadCloser, error) {
	stream = d.resolveStreamFilterKeys(ctx, stream)
	return d.Filters.NewStreamReader(ctx, stream)
}

// NewStreamReaderChecked is [Document.NewStreamReader] that fails, with
// [filter.ErrUnsupportedFilter], on a filter litho cannot decode instead of
// handing its input on still encoded. [Document.UndecodableFilters] lists a
// filter of every stream it refuses, and it refuses every listed stream,
// unless the data fails decoding first, damaged or past a decoding limit. See
// [filter.Decoder.NewStreamReaderChecked].
func (d *Document) NewStreamReaderChecked(ctx context.Context, stream ast.PdfStream) (io.ReadCloser, error) {
	stream = d.resolveStreamFilterKeys(ctx, stream)
	return d.Filters.NewStreamReaderChecked(ctx, stream)
}

// builtinFilters returns a decoder that consults no plugin and decodes under
// the document's budgets. The cross-reference and object streams decode
// through it: they are read during construction, before pdf.Open registers
// any plugin, and a plugin must not decide how an object stream read later
// resolves the objects it holds.
func (d *Document) builtinFilters() *filter.Decoder {
	return filter.NewDecoder(nil, d.Limits())
}

// resolveStreamFilterKeys returns stream with its /Filter and /DecodeParms
// entries resolved to direct objects, copying the dictionary only when a
// reference is actually found.
//
// Per ISO 32000-1, 7.3.10, any value may be written as an indirect reference
// unless a table says otherwise, and Table 5 places no such restriction on
// either key. The filter pipeline sees only the dictionary and cannot resolve
// anything, so a stream whose /Filter is a reference decoded to nothing.
//
// The JBIG2Globals stream of a JBIG2Decode parameter dictionary is resolved
// the same way: ISO 32000-1, Table 12 makes it a stream, so it is always an
// indirect reference, and the JBIG2Decode filter reads the global segments
// from it. So is the /ColorSpace of a JPXDecode image, which decides how that
// filter reads the JPEG 2000 data (7.4.9). A Crypt filter naming the crypt
// filter the security handler already applied reads as Identity (see
// [Document.appliedCryptParms]).
//
// # Corpus evidence
//
//   - pdfjs bug1727053.pdf: writes /Filter 3 0 R for its embedded font
//     program, which left the whole 751 KB TrueType program unreadable.
func (d *Document) resolveStreamFilterKeys(ctx context.Context, stream ast.PdfStream) ast.PdfStream {
	stream = d.resolveFilterKeys(ctx, stream)
	if stream.Dict == nil {
		return stream
	}
	parms, parmsChanged := d.resolveJBIG2Globals(ctx, stream.Dict["DecodeParms"])
	if crypt, changed := d.appliedCryptParms(stream.Dict["Filter"], parms); changed {
		parms, parmsChanged = crypt, true
	}
	cs, csChanged := d.resolveJPXColorSpace(ctx, stream.Dict)
	if !parmsChanged && !csChanged {
		return stream
	}
	resolvedDict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(resolvedDict, stream.Dict)
	if parmsChanged {
		resolvedDict["DecodeParms"] = parms
	}
	if csChanged {
		resolvedDict["ColorSpace"] = cs
	}
	stream.Dict = resolvedDict
	return stream
}

// appliedCryptParms returns /DecodeParms with the parameters of a Crypt filter
// that names the document's default stream crypt filter rewritten to name
// Identity, and reports whether it rewrote any. filterObj and parms are the
// stream's resolved /Filter and /DecodeParms.
//
// ISO 32000-1, 7.4.10 has the Crypt filter decrypt with the crypt filter its
// /Name gives, and 7.6.5 makes /StmF the one every other stream is decrypted
// with. litho's security handler decrypts every stream whose Crypt filter is
// not Identity with /StmF as it reads the stream, so for one naming /StmF the
// decryption is done and what remains for the filter is Identity's work. The
// filter pipeline, which sees only the dictionary, would otherwise refuse the
// name as a crypt filter it cannot apply. A Crypt filter naming any other crypt
// filter was decrypted with the wrong one, and is left for the pipeline to
// refuse.
//
// # Corpus evidence
//
//   - All corpora: the 6 streams behind a Crypt filter all take Identity,
//     named or by default, so the rewrite applies to none.
func (d *Document) appliedCryptParms(filterObj, parms ast.PdfObject) (ast.PdfObject, bool) {
	if d.streamCrypt == "" || d.streamCrypt == "Identity" {
		return parms, false
	}
	// applied returns the parameters p rewritten to Identity when they name
	// the default stream crypt filter.
	applied := func(p ast.PdfObject) (ast.PdfDict, bool) {
		dict, isDict := p.(ast.PdfDict)
		if name, _ := dict["Name"].(ast.PdfName); !isDict || name != d.streamCrypt {
			return nil, false
		}
		out := maps.Clone(dict)
		out["Name"] = ast.PdfName("Identity")
		return out, true
	}
	switch f := filterObj.(type) {
	case ast.PdfName:
		if f == "Crypt" {
			if out, ok := applied(parms); ok {
				return out, true
			}
		}
	case ast.PdfArray:
		arr, isArray := parms.(ast.PdfArray)
		if !isArray {
			// A single dictionary under a filter array is the first filter's
			// (see filter.extractDecodeParms), and 7.4.10 puts Crypt first.
			if len(f) > 0 && f[0] == ast.PdfName("Crypt") {
				if out, ok := applied(parms); ok {
					return out, true
				}
			}
			return parms, false
		}
		var out ast.PdfArray
		for i, item := range f {
			if item != ast.PdfName("Crypt") || i >= len(arr) {
				continue
			}
			rewritten, ok := applied(arr[i])
			if !ok {
				continue
			}
			if out == nil {
				out = slices.Clone(arr)
			}
			out[i] = rewritten
		}
		if out != nil {
			return out, true
		}
	}
	return parms, false
}

// resolveJPXColorSpace resolves the /ColorSpace of a stream decoded by
// JPXDecode far enough for the filter to tell an Indexed space from any other:
// an indirect value, and an indirect family name heading an array (ISO
// 32000-1, 8.6.3). Under an Indexed space the JPEG 2000 samples are palette
// indices, so the filter must not look them up in the JPEG 2000 palette
// (7.4.9). Other streams, and a value that will not resolve, are left alone.
func (d *Document) resolveJPXColorSpace(ctx context.Context, dict ast.PdfDict) (ast.PdfObject, bool) {
	if !chainHas(dict["Filter"], "JPXDecode") {
		return nil, false
	}
	cs, changed := dict["ColorSpace"], false
	if ref, isRef := cs.(ast.PdfIndirectRef); isRef {
		obj, err := d.Resolve(ctx, ref)
		if err != nil {
			d.Logger.Debug("cannot resolve the ColorSpace of a JPXDecode image", "error", err)
			return nil, false
		}
		cs, changed = obj, true
	}
	if arr, isArray := cs.(ast.PdfArray); isArray && len(arr) > 0 {
		if ref, isRef := arr[0].(ast.PdfIndirectRef); isRef {
			family, err := d.Resolve(ctx, ref)
			if err != nil {
				d.Logger.Debug("cannot resolve the ColorSpace family of a JPXDecode image", "error", err)
				return nil, false
			}
			arr = slices.Clone(arr)
			arr[0] = family
			cs, changed = arr, true
		}
	}
	return cs, changed
}

// chainHas reports whether a resolved /Filter value names filter.
func chainHas(value ast.PdfObject, name ast.PdfName) bool {
	switch v := value.(type) {
	case ast.PdfName:
		return v == name
	case ast.PdfArray:
		for _, item := range v {
			if n, ok := item.(ast.PdfName); ok && n == name {
				return true
			}
		}
	}
	return false
}

// resolveJBIG2Globals resolves the JBIG2Globals entry of a /DecodeParms value,
// a dictionary or an array of them, to the stream it refers to, with that
// stream's own filter keys resolved. The globals stream is not searched for a
// JBIG2Globals of its own, so a stream naming itself cannot recurse.
func (d *Document) resolveJBIG2Globals(ctx context.Context, parms ast.PdfObject) (ast.PdfObject, bool) {
	resolveDict := func(dict ast.PdfDict) (ast.PdfDict, bool) {
		ref, isRef := dict["JBIG2Globals"].(ast.PdfIndirectRef)
		if !isRef {
			return dict, false
		}
		obj, err := d.Resolve(ctx, ref)
		if err != nil {
			d.Logger.Debug("cannot resolve JBIG2Globals", "error", err)
			return dict, false
		}
		globals, isStream := obj.(ast.PdfStream)
		if !isStream {
			return dict, false
		}
		out := maps.Clone(dict)
		out["JBIG2Globals"] = d.resolveFilterKeys(ctx, globals)
		return out, true
	}
	switch v := parms.(type) {
	case ast.PdfDict:
		return resolveDict(v)
	case ast.PdfArray:
		var out ast.PdfArray
		for i, item := range v {
			dict, isDict := item.(ast.PdfDict)
			if !isDict {
				continue
			}
			resolved, changed := resolveDict(dict)
			if !changed {
				continue
			}
			if out == nil {
				out = slices.Clone(v)
			}
			out[i] = resolved
		}
		if out != nil {
			return out, true
		}
	}
	return parms, false
}

// resolveFilterKeys resolves a stream's /Filter and /DecodeParms entries, as
// [Document.resolveStreamFilterKeys] describes, without the JBIG2Globals step.
func (d *Document) resolveFilterKeys(ctx context.Context, stream ast.PdfStream) ast.PdfStream {
	if stream.Dict == nil {
		return stream
	}
	var resolvedDict ast.PdfDict
	for _, key := range []ast.PdfName{"Filter", "DecodeParms"} {
		value, ok := stream.Dict[key]
		if !ok {
			continue
		}
		direct, changed := d.resolveFilterValue(ctx, value)
		if !changed {
			continue
		}
		if resolvedDict == nil {
			resolvedDict = make(ast.PdfDict, len(stream.Dict))
			maps.Copy(resolvedDict, stream.Dict)
		}
		resolvedDict[key] = direct
	}
	if resolvedDict == nil {
		return stream
	}
	stream.Dict = resolvedDict
	return stream
}

// resolveFilterValue resolves an indirect /Filter or /DecodeParms value, and
// any indirect element of an array reached that way. Reports whether anything
// changed so the caller can leave the stream untouched in the common case. A
// reference that will not resolve is left alone for the pipeline to reject.
func (d *Document) resolveFilterValue(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, bool) {
	changed := false
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		direct, err := d.Resolve(ctx, ref)
		if err != nil {
			d.Logger.Debug("cannot resolve indirect stream filter entry", "error", err)
			return obj, false
		}
		obj, changed = direct, true
	}
	arr, isArray := obj.(ast.PdfArray)
	if !isArray {
		return obj, changed
	}
	// An array may name its filters through references of its own.
	var out ast.PdfArray
	for i, item := range arr {
		ref, isRef := item.(ast.PdfIndirectRef)
		if !isRef {
			continue
		}
		direct, err := d.Resolve(ctx, ref)
		if err != nil {
			continue
		}
		if out == nil {
			out = slices.Clone(arr)
		}
		out[i] = direct
	}
	if out == nil {
		return obj, changed
	}
	return out, true
}

// OpenFunc returns a lazy Open closure for a PDF stream. The returned
// function decodes the stream through the filter pipeline on demand.
// Use this to populate Open fields on model types ([models.ImageMeta],
// [models.EmbeddedFile], etc.) without loading stream data at parse time.
func (d *Document) OpenFunc(stream ast.PdfStream) func(ctx context.Context) (io.ReadCloser, error) {
	return func(ctx context.Context) (io.ReadCloser, error) {
		return d.NewStreamReader(ctx, stream)
	}
}

// NopLogger returns a logger that discards all output.
func NopLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// headerScanLimit is the byte window scanned for the "%PDF-" header
// at the start of a file. ISO 32000 places the header on the first
// line, but Acrobat tolerates a leading prefix (BOM, shebang line,
// HTTP-style preamble, explanatory comment) and so does litho. xref
// offsets remain absolute from byte 0 of the file, which is why no
// offset adjustment is needed downstream.
const headerScanLimit = 1024

// headerSearchLimit bounds the search for a "%PDF-" header that sits beyond
// [headerScanLimit]. It is an anti-DoS guard, not spec-mandated: the search only
// runs for a file that already proved it ends like a PDF, and 16 MiB covers any
// plausible prefix while keeping the read bounded for a crafted input.
const headerSearchLimit = 16 << 20

// headerExistsBeyondScanLimit reports whether the file carries a "%PDF-" header
// further in than [headerScanLimit], which is the case litho refuses: such a
// document's body is too far from the start to cache safely, and it is not the
// same thing as a file that states no header at all.
func headerExistsBeyondScanLimit(r io.ReaderAt, size int64) bool {
	limit := min(size, headerSearchLimit)
	if limit <= headerScanLimit {
		return false
	}

	buf := make([]byte, limit)
	n, err := r.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		return false
	}
	return bytes.Contains(buf[:n], []byte("%PDF-"))
}

// Option configures a [Document] as [NewDocument] builds it.
type Option func(*options)

// options holds what the Options of one NewDocument call set.
type options struct {
	limits limits.Limits
}

// WithLimits sets the resource budgets the document is read under. A zero
// budget reads as its default. The caller validates lim first
// ([limits.Limits.Validate]): NewDocument trusts it.
func WithLimits(lim limits.Limits) Option {
	return func(o *options) {
		o.limits = lim
	}
}

// Limits returns the resource budgets the document is read under, every
// one filled in. A nil or zero-value Document has the defaults.
func (d *Document) Limits() limits.Limits {
	if d == nil {
		return limits.Default()
	}
	return d.limits.WithDefaults()
}

// NewDocument creates a new [Document] by parsing the XRef chains.
// If logger is nil, a no-op logger is used. The object cache holds at most
// [limits.Limits.MaxCachedObjects] entries of the budgets [WithLimits] sets.
//
// # Corpus evidence
//
//   - pdfjs bug1606566.pdf: has a complete xref, trailer and startxref and
//     simply lacks the "%PDF-" line. pdf.js, poppler ("May not be a PDF file
//     (continuing anyway)") and pikepdf all open it, which is why a headerless
//     file that ends like a PDF is read.
//   - pdfjs issue5909_original.pdf: its final startxref points into a
//     superseded revision, and the rebuild left all 43 objects cached as
//     ciphertext, so `litho metadata` reported the encrypted bytes as the
//     /Author and `litho text` found nothing in a document poppler reads. A
//     full rewrite then dropped /Encrypt while copying those bytes verbatim,
//     turning a readable file into an unreadable one and reporting success.
//     The caches are reset once the crypto handler exists for that reason.
func NewDocument(ctx context.Context, r io.ReaderAt, size int64, logger *slog.Logger, opts ...Option) (*Document, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	lim := o.limits.WithDefaults()

	// Validate PDF Header. Scan up to headerScanLimit bytes so files
	// with a leading prefix (UTF-8 BOM, explanatory comment) are still
	// accepted.
	headerBuf := make([]byte, headerScanLimit)
	n, err := r.ReadAt(headerBuf, 0)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read file header: %w", err)
	}
	if n == 0 {
		return nil, ErrMissingHeader
	}
	headerBuf = headerBuf[:n]
	pdfIdx := bytes.Index(headerBuf, []byte("%PDF-"))

	// A file with no header at all is still read when it ends like a PDF.
	// ISO 32000-1, 7.5.2 requires the header, so such a file is invalid, but
	// every other reader accepts it: pdf.js, poppler and pikepdf all open a
	// file that has a complete xref, trailer and startxref and lacks only the
	// "%PDF-" line.
	//
	// A header merely sitting beyond headerScanLimit is a different case and
	// stays rejected: its body is too far from the start to cache safely, and
	// treating it as headerless would record a header offset of 0 for a file
	// that has one.
	if pdfIdx == -1 {
		// The tail probe runs first because it is bounded and cheap. Input that
		// is not a PDF at all fails here, so it never reaches the scan below.
		if _, xrefErr := FindStartXRef(r, size); xrefErr != nil {
			return nil, ErrMissingHeader
		}
		if headerExistsBeyondScanLimit(r, size) {
			return nil, ErrMissingHeader
		}
	}

	// Extract version string (e.g., "1.4", "2.0"). A headerless file states no
	// version, which is what pdf.js reports for it as well.
	version := ""
	headerOffset := int64(0)
	if pdfIdx >= 0 {
		headerOffset = int64(pdfIdx)
		version = headerVersion(headerBuf[pdfIdx+len("%PDF-"):])
	}

	if logger == nil {
		logger = NopLogger()
	}

	doc := &Document{
		r:            r,
		size:         size,
		xrefEntries:  make(map[int]xref.XRefEntry),
		objectCache:  make(map[int]ast.PdfObject),
		fontCache:    make(map[int]any),
		cmapCache:    make(map[int]any),
		maxCacheSize: lim.MaxCachedObjects,
		limits:       lim,
		Filters:      filter.NewDecoder(nil, lim),
		Logger:       logger,
		Version:      version,
		HeaderOffset: headerOffset,
	}

	// Probe for linearization before seeking to the end of the file. The probe
	// starts at the header, not at byte 0: a file carrying a leading prefix
	// still declares its linearization dictionary as the first object after
	// %PDF-, and reading from 0 finds the prefix instead.
	linParams := probeLinearization(r, size, doc.HeaderOffset)
	if linParams != nil {
		doc.Linearization = linParams
		hintEntries, err := parseHintStream(r, size, linParams, lim.MaxStreamBytes)
		if err == nil && len(hintEntries) > 0 {
			doc.HintTable = hintEntries
		}
	}

	if err := doc.parseXRefChain(ctx); err != nil {
		doc.Logger.Debug("xref chain parse failed, attempting rebuild",
			slog.String("error", err.Error()),
		)
		// Fallback: brute-force rebuild by scanning for object headers.
		doc.xrefEntries = make(map[int]xref.XRefEntry)
		doc.Index.Trailer = nil
		if rebuildErr := doc.rebuildXRef(ctx); rebuildErr != nil {
			return nil, fmt.Errorf("%w: %v (rebuild also failed: %v)", ErrDamagedXRef, err, rebuildErr)
		}
		doc.Rebuilt = true
	}

	// Verify the catalog /Root is reachable. Broken xref chains from
	// incremental updates can parse without error but miss the catalog
	// object. If /Root can't be resolved, rebuild the xref from scratch.
	// Also rebuild when the parse produced zero entries: some producers
	// emit a syntactically valid but empty xref table with startxref
	// pointing at offset 0, which yields no objects. The rebuild path
	// discovers them by scanning raw bytes.
	if !doc.Rebuilt {
		shouldRebuild := len(doc.xrefEntries) == 0
		if !shouldRebuild {
			if rootRef, ok := doc.Index.Trailer["Root"]; ok {
				if ref, ok := rootRef.(ast.PdfIndirectRef); ok {
					if _, exists := doc.xrefEntries[ref.ObjectNum]; !exists {
						shouldRebuild = true
					}
				}
			}
		}
		if shouldRebuild {
			doc.Logger.Debug("xref entries incomplete, rebuilding")
			doc.xrefEntries = make(map[int]xref.XRefEntry)
			doc.Index.Trailer = nil
			if rebuildErr := doc.rebuildXRef(ctx); rebuildErr == nil {
				doc.Rebuilt = true
			}
		}
	}

	// A raw-scan rebuild only recovers uncompressed "N G obj" headers; objects
	// held inside object streams (ISO 32000-1 §7.5.7), which can include a
	// catalog or page-tree node a modern producer compressed, are invisible to
	// it. Register each recovered object stream's members so ResolveCatalog
	// can still find a compressed /Type /Catalog (otherwise the whole page tree
	// is lost). Runs only after a rebuild, when the fully-constructed document
	// can resolve the object streams it found.
	if doc.Rebuilt {
		doc.expandObjectStreamsForRebuild(ctx, doc.xrefEntries)
	}

	// Runs once the index is final, whichever path filled it, because all of
	// them take object numbers straight from the file.
	doc.pruneUnrepresentableEntries()

	doc.objectStarts = declaredObjectStarts(doc.xrefEntries)
	if !doc.Rebuilt {
		doc.declaredStarts = doc.objectStarts
	}

	doc.Logger.Debug("xref parsed",
		slog.Int("entries", len(doc.xrefEntries)),
		slog.Bool("rebuilt", doc.Rebuilt),
		slog.Bool("linearized", doc.Linearization != nil),
	)

	// Initialize Crypto if Encrypt dictionary is present. An /Encrypt written as
	// the null object is absent (ISO 32000-1, 7.3.7), so the file is not
	// encrypted: veraPDF opens it as such. A reference to a null object still
	// fails below, as veraPDF also refuses that file.
	encObj, ok := doc.Index.Trailer["Encrypt"]
	if _, isNull := encObj.(ast.PdfNull); isNull {
		ok = false
	}
	if ok {
		var encDict ast.PdfDict
		if ref, ok := encObj.(ast.PdfIndirectRef); ok {
			// Resolving without Crypto is safe because Encrypt dict values are never encrypted
			obj, err := doc.Resolve(ctx, ref)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve /Encrypt reference: %w", err)
			}
			encDict, ok = obj.(ast.PdfDict)
			if !ok {
				return nil, fmt.Errorf("resolved /Encrypt is not a dictionary")
			}
		} else {
			encDict, ok = encObj.(ast.PdfDict)
			if !ok {
				return nil, fmt.Errorf("/Encrypt is not a dictionary")
			}
		}

		doc.IsEncrypted = true
		encEntries := doc.resolveEncryptEntries(ctx, encDict)
		handler, err := crypto.NewHandler(encEntries, doc.Index.Trailer)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize crypto handler: %w", err)
		}
		doc.Crypto = handler
		// ISO 32000-1, Table 20: crypt filters, and so /StmF, exist from V 4.
		if stmF, isName := encEntries["StmF"].(ast.PdfName); isName && handler.V >= 4 {
			doc.streamCrypt = stmF
		}
		doc.Authenticated = handler.Authenticated

		// Anything parsed before this point was parsed without the decryption
		// function, so its strings and streams are still ciphertext. Recovery
		// does exactly that: a raw-scan rebuild resolves every object it found
		// looking for object streams, which runs above and caches all of them.
		// Left in the cache, that ciphertext is reported as the document's
		// metadata and text, and a full rewrite copies it verbatim after
		// dropping /Encrypt, turning a readable file into an unreadable one.
		//
		// Document.Authenticate already clears the caches for this reason,
		// but a document that opens on the empty user password authenticates
		// inside crypto.NewHandler and never calls it.
		doc.resetParsedObjectCaches()

		doc.Logger.Debug("encryption detected",
			slog.Int("V", handler.V),
			slog.Int("key_length", handler.Length),
			slog.String("cfm", handler.CFM),
			slog.Bool("authenticated_empty_password", handler.Authenticated),
		)
	}

	doc.initOCConfig(ctx)

	// Per PDF spec, the catalog /Version key overrides the header version
	// when present (allows incremental updates to bump the version).
	doc.initCatalogVersion(ctx)

	doc.Logger.Debug("document opened",
		slog.Int64("size_bytes", size),
		slog.String("version", doc.Version),
		slog.Bool("encrypted", doc.IsEncrypted),
	)

	return doc, nil
}

// Authenticate attempts to authenticate an encrypted document with the given password.
// Authenticate must be called during setup, before the [Document] is used concurrently.
func (d *Document) Authenticate(password string) error {
	if !d.IsEncrypted || d.Crypto == nil {
		return nil
	}
	if err := d.Crypto.Authenticate(password); err != nil {
		return fmt.Errorf("invalid password or unsupported encryption: %w", err)
	}
	d.Authenticated = true
	d.resetParsedObjectCaches()
	return nil
}

// resetParsedObjectCaches drops everything parsed so far so the next resolution
// re-reads it through the decryption function now in force.
//
// Objects cached before the crypto handler existed hold ciphertext, and nothing
// downstream can tell the difference: a resolve is a cache hit and the encrypted
// bytes are returned as the object's value. The format-violation record is keyed
// to those same parses, so it goes with them; whatever is re-parsed re-records.
func (d *Document) resetParsedObjectCaches() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.objectCache = make(map[int]ast.PdfObject)
	d.objStmCache = nil
	d.objStmCacheBytes = 0
	d.formatViolations = nil
}

// headerVersion reads the version number that follows "%PDF-" in buf.
//
// Per ISO 32000-1, 7.5.2 the header is "%PDF-1.N", so the version is digits, a
// period, then digits, and the line may carry anything after that: the header
// is a comment and the rest of the line is not part of the number. Reading to
// the end of the line turns a header such as "%PDF-1.3%" into version "1.3%",
// which is not a version and compares against nothing. Poppler reads 1.3 there
// and litho agrees.
//
// A header with no complete number ("%PDF-1.") yields the empty string, the
// same answer a headerless file gives.
//
// # Corpus evidence
//
//   - pdfjs issue15033.pdf: the header is "%PDF-1.3%", and reading to the end
//     of the line reported version "1.3%" where poppler reads 1.3.
//   - pdfjs issue9105_other.pdf: the header is "%PDF-1." with no complete
//     number.
func headerVersion(buf []byte) string {
	i := 0
	for i < len(buf) && buf[i] >= '0' && buf[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(buf) || buf[i] != '.' {
		return ""
	}
	j := i + 1
	for j < len(buf) && buf[j] >= '0' && buf[j] <= '9' {
		j++
	}
	if j == i+1 {
		return ""
	}
	return string(buf[:j])
}

// initCatalogVersion checks the catalog /Version key and overrides the header
// version with it.
//
// Per ISO 32000-1, 7.5.2 the catalog /Version, if present, shall be used
// instead of the version in the file header, whichever is later: Table 28 only
// describes when a producer writes the entry. pdf.js reads it the same way,
// with "return this.catalog.version || this.#version" in core/document.js.
//
// A /Version that is not a version number is ignored rather than reported.
// pdf.js validates it against the same shape and warns, and a caller comparing
// the reported version against a profile requirement cannot do anything with a
// name like /Nonsense.
//
// # Corpus evidence
//
//   - pdfjs PDFJS-9279-reduced.pdf: the header says 1.7 and the catalog says
//     /1.4, and its metadata golden expects version 1.4.
func (d *Document) initCatalogVersion(ctx context.Context) {
	// Resolved through ResolveCatalog: per ISO 32000-1 §7.5.5 Table 15
	// /Root shall be the catalog, and reading it raw made a file whose damaged
	// incremental update points elsewhere keep its header version, so a catalog
	// declaring /Version /2.0 was reported as 1.4.
	resolved, err := ResolveCatalog(ctx, d)
	if err != nil {
		d.Logger.Debug("resolving catalog for version check", slog.Any("err", err))
		return
	}
	if resolved == nil || resolved.Dict == nil {
		return
	}
	catalog := resolved.Dict
	vObj, ok := catalog["Version"]
	if !ok {
		return
	}
	vName, ok := vObj.(ast.PdfName)
	if !ok {
		return
	}
	if !isPDFVersion(string(vName)) {
		d.Logger.Debug("catalog /Version is not a version number", slog.String("value", string(vName)))
		return
	}
	d.Version = string(vName)
}

// isPDFVersion reports whether v has the "major.minor" shape ISO 32000-1, 7.5.2
// gives the header version and Table 28 gives the catalog entry: digits, a
// period, digits, and nothing else.
func isPDFVersion(v string) bool {
	major, minor, found := strings.Cut(v, ".")
	if !found || major == "" || minor == "" {
		return false
	}
	for _, part := range []string{major, minor} {
		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// IsLinearized reports whether the document has a linearization dictionary.
func (d *Document) IsLinearized() bool {
	return d.Linearization != nil
}

// ClearCaches releases the object cache and font cache to free memory.
// Caches are transparently rebuilt on the next access. The
// format-violation record is dropped with them, since re-parsing an object
// is what records it again (see [Document.ResetFormatViolations]).
//
// ClearCaches must not be called concurrently with other [Document] methods.
func (d *Document) ClearCaches() {
	d.mu.Lock()
	d.objectCache = make(map[int]ast.PdfObject)
	d.fontCache = make(map[int]any)
	d.cmapCache = make(map[int]any)
	d.objStmCache = nil
	d.objStmCacheBytes = 0
	d.fontProgCache = nil
	d.fontProgBytes = 0
	d.colorCache = nil
	d.colorCacheBytes = 0
	d.formatViolations = nil
	d.integerOverflows = nil
	d.mu.Unlock()
}

// CachedFontProgram returns the decoded font program for the given FontFile
// stream object number. Returns nil, false if not cached.
//
// The returned slice is the cached buffer itself, shared by every caller, so
// callers must treat it as read-only. Font programs are parsed, never edited,
// and copying them per call would undo the decode this cache exists to avoid.
func (d *Document) CachedFontProgram(objNum int) ([]byte, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	data, ok := d.fontProgCache[objNum]
	return data, ok
}

// SetCachedFontProgram stores a decoded font program keyed by its FontFile
// stream object number, bounding the total cached bytes at
// [limits.Limits.MaxFontProgCacheBytes] of the document's budgets with an
// evict-all policy.
//
// Font programs have strong locality: one embedded font is re-read by every
// per-page font check, which is exactly what makes the cache pay. A single
// program larger than the bound is not cached, so it cannot force repeated
// evictions.
func (d *Document) SetCachedFontProgram(objNum int, data []byte) {
	limit := d.Limits().MaxFontProgCacheBytes()
	if int64(len(data)) > limit {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fontProgCache == nil {
		d.fontProgCache = make(map[int][]byte)
	}
	if _, ok := d.fontProgCache[objNum]; ok {
		return
	}
	if int64(d.fontProgBytes)+int64(len(data)) > limit {
		d.fontProgCache = make(map[int][]byte)
		d.fontProgBytes = 0
	}
	d.fontProgCache[objNum] = data
	d.fontProgBytes += len(data)
}

// CachedColorTransform returns the colour transform cached for the profile
// stream object number and rendering intent. A nil value records a profile
// that could not be used.
func (d *Document) CachedColorTransform(objNum, intent int) (any, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	t, ok := d.colorCache[[2]int{objNum, intent}]
	return t, ok
}

// SetCachedColorTransform stores a colour transform of size bytes keyed by
// its profile stream object number and rendering intent, bounding the total
// bytes at [limits.Limits.MaxColorCacheBytes] of the document's budgets with
// an evict-all policy. A transform larger than the bound is not cached, so it
// cannot force repeated evictions.
func (d *Document) SetCachedColorTransform(objNum, intent int, t any, size int) {
	limit := d.Limits().MaxColorCacheBytes()
	if int64(size) > limit {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	key := [2]int{objNum, intent}
	if _, ok := d.colorCache[key]; ok {
		return
	}
	if d.colorCache == nil || int64(d.colorCacheBytes)+int64(size) > limit {
		d.colorCache = make(map[[2]int]any)
		d.colorCacheBytes = 0
	}
	d.colorCache[key] = t
	d.colorCacheBytes += size
}

// CachedCMap returns a previously cached parsed CMap for the given stream
// object number. Returns nil, false if not cached.
func (d *Document) CachedCMap(objNum int) (any, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	c, ok := d.cmapCache[objNum]
	return c, ok
}

// SetCachedCMap stores a parsed CMap keyed by its stream object number.
func (d *Document) SetCachedCMap(objNum int, cm any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.cmapCache[objNum]; !ok {
		d.cmapCache[objNum] = cm
	}
}

// CachedFont returns a previously cached font for the given object number.
// The second return value reports whether the font was found in the cache.
func (d *Document) CachedFont(objNum int) (any, bool) {
	d.mu.RLock()
	f, ok := d.fontCache[objNum]
	d.mu.RUnlock()
	return f, ok
}

// SetCachedFont stores a resolved font in the cache, keyed by object number.
// If the font is already cached, the existing entry is preserved.
func (d *Document) SetCachedFont(objNum int, font any) {
	d.mu.Lock()
	if _, ok := d.fontCache[objNum]; !ok {
		d.fontCache[objNum] = font
	}
	d.mu.Unlock()
}

// FileSize returns the total size of the underlying PDF file in bytes.
func (d *Document) FileSize() int64 {
	return d.size
}

// RawReader returns the underlying [io.ReaderAt] for low-level byte access.
func (d *Document) RawReader() io.ReaderAt {
	return d.r
}

// SetRawReader sets the underlying [io.ReaderAt]. This is intended for test
// code that needs to construct a [Document] with raw file bytes without
// going through the full parsing pipeline.
func (d *Document) SetRawReader(r io.ReaderAt) {
	d.r = r
}

// ReadFileBytes reads a specific chunk of raw bytes directly from the underlying file.
func (d *Document) ReadFileBytes(ctx context.Context, offset int64, length int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A length taken from the file can be negative, and make panics on one.
	if length < 0 {
		return nil, fmt.Errorf("reading %d bytes at offset %d: negative length", length, offset)
	}
	buf := make([]byte, length)
	_, err := d.r.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf, nil
}

// resolveEncryptEntries returns the encryption dictionary with its indirect
// entries replaced by the objects they name, including the crypt filter
// dictionaries nested inside /CF.
//
// [crypto.NewHandler] is given a dictionary and no way to resolve anything, so
// every entry it reads must already be a value. ISO 32000-1, 7.3.10 lets any
// object be written indirectly, and an AES document that writes /CF that way
// left the handler unable to see a crypt filter at all: it fell back to the V2
// default and decrypted the whole file with RC4 (Table 25). Authentication still
// succeeded, because /O and /U do not depend on the cipher, so litho reported an
// authenticated document and returned every string and stream as garbage.
//
// Resolving here is safe for the same reason the caller resolves /Encrypt
// itself: the values in an encryption dictionary are never encrypted (7.6.2),
// and doc.Crypto is not yet set, so no decryption is applied to them.
func (d *Document) resolveEncryptEntries(ctx context.Context, encDict ast.PdfDict) ast.PdfDict {
	resolved := make(ast.PdfDict, len(encDict))
	for key, value := range encDict {
		resolved[key] = d.resolveEncryptValue(ctx, value)
	}

	// /CF holds one dictionary per crypt filter name (Table 25), and each of
	// those may itself be indirect.
	filters, ok := resolved["CF"].(ast.PdfDict)
	if !ok {
		return resolved
	}
	newFilters := make(ast.PdfDict, len(filters))
	for name, filter := range filters {
		newFilters[name] = d.resolveEncryptValue(ctx, filter)
	}
	resolved["CF"] = newFilters
	return resolved
}

// resolveEncryptValue resolves one encryption-dictionary entry, leaving it
// untouched when it is already a value or cannot be read.
func (d *Document) resolveEncryptValue(ctx context.Context, value ast.PdfObject) ast.PdfObject {
	ref, ok := value.(ast.PdfIndirectRef)
	if !ok {
		return value
	}
	obj, err := d.Resolve(ctx, ref)
	if err != nil {
		d.Logger.Debug("resolving an encryption dictionary entry failed",
			slog.Int("object", ref.ObjectNum),
			slog.Any("err", err),
		)
		return value
	}
	return obj
}
