package document

import (
	"maps"
	"reflect"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// Fork returns a [Document] over the same bytes whose mutable state is its
// own: the trailer dictionaries, the cross-reference entries, the caches, the
// compliance scratch state and the logger. A writer edits a fork, so what it
// stages in the trailer, what it edits in a cached object, the caches it clears
// and the logger it swaps in never reach the Document other goroutines read.
//
// The fork starts from the index d holds when Fork runs, the entries a raw-byte
// augmentation added or moved included, and with empty caches: an object
// resolved through d and then edited in place by a writer would otherwise be
// edited in d as well. Each trailer dictionary is copied, keeping the aliasing
// between them, so a trailer that is both [xref.Index] Trailer and LastTrailer
// in d is one dictionary in the fork too. The copy is shallow: code editing a
// trailer replaces an entry, never an object an entry holds.
//
// What d fixed at construction and only reads afterwards is shared: the
// underlying [io.ReaderAt], the security handler, which authentication
// completed, the filter decoder, the resource budgets, the linearization
// parameters, the optional-content configuration and the offset tables. So are
// the objects [Document.PromoteDirectKids] staged, which the fork reads as d
// reads them.
//
// Fork is safe to call concurrently with every read of d, and the fork
// honours the same concurrency contract as d.
func (d *Document) Fork() *Document {
	f := &Document{
		r:                       d.r,
		size:                    d.size,
		objectCache:             make(map[int]ast.PdfObject),
		fontCache:               make(map[int]any),
		cmapCache:               make(map[int]any),
		maxCacheSize:            d.maxCacheSize,
		limits:                  d.limits,
		Crypto:                  d.Crypto,
		streamCrypt:             d.streamCrypt,
		OCConfig:                d.OCConfig,
		IsEncrypted:             d.IsEncrypted,
		Authenticated:           d.Authenticated,
		Logger:                  d.Logger,
		Linearization:           d.Linearization,
		HintTable:               d.HintTable,
		Rebuilt:                 d.Rebuilt,
		TrailerSynthesized:      d.TrailerSynthesized,
		HeaderOffset:            d.HeaderOffset,
		Version:                 d.Version,
		HeaderVersion:           d.HeaderVersion,
		trailerFormatViolations: d.trailerFormatViolations,
		trailerMergedKeys:       d.trailerMergedKeys,
		trailerStream:           d.trailerStream,
		objectStarts:            d.objectStarts,
		declaredStarts:          d.declaredStarts,
		XRefTableOffsets:        d.XRefTableOffsets,
		XRefSectionOffsets:      d.XRefSectionOffsets,
		Filters:                 d.Filters,
		CMYKProfile:             d.CMYKProfile,
		Index:                   forkIndex(d.Index),
	}

	// A completed augmentation stores done after it releases d.mu, so done is
	// read before the entries: a fork that sees done also sees what the run
	// wrote. One that copies a run's entries without seeing done runs the
	// augmentation again, which finds nothing left to add or move.
	done := d.xrefAugment.done.Load()
	d.mu.RLock()
	f.xrefEntries = maps.Clone(d.xrefEntries)
	f.xrefAdded = maps.Clone(d.xrefAdded)
	f.xrefAugmented = d.xrefAugmented
	d.mu.RUnlock()
	if f.xrefEntries == nil {
		f.xrefEntries = make(map[int]xref.XRefEntry)
	}
	if done {
		f.xrefAugment.done.Store(true)
	}
	// The fork reads the page tree d reads, promoted kids included. The map
	// is never modified once stored, so both documents share it.
	f.promoted.Store(d.promoted.Load())
	f.promotedDone.Store(d.promotedDone.Load())
	return f
}

// forkIndex copies the trailer dictionaries of idx, each distinct dictionary
// once, so two fields naming one dictionary in idx name one copy. A file with
// one cross-reference section has one trailer, which the parse stores in all
// three fields, and an edit of the fork's Trailer has to reach its LastTrailer
// as an edit of d's reached d's.
func forkIndex(idx xref.Index) xref.Index {
	var originals, copies []ast.PdfDict
	clone := func(d ast.PdfDict) ast.PdfDict {
		if d == nil {
			return nil
		}
		for i, o := range originals {
			if reflect.ValueOf(o).UnsafePointer() == reflect.ValueOf(d).UnsafePointer() {
				return copies[i]
			}
		}
		c := maps.Clone(d)
		originals = append(originals, d)
		copies = append(copies, c)
		return c
	}
	return xref.Index{
		Trailer:              clone(idx.Trailer),
		LastTrailer:          clone(idx.LastTrailer),
		HighestOffsetTrailer: clone(idx.HighestOffsetTrailer),
	}
}
