package compliance

import (
	"context"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// objectSweep is what one walk over every object the cross-reference table
// lists ([sweptXRefEntries]) finds for the checks that judge every object: the
// file specifications ([collectFileSpecs]), the metadata streams other objects
// reference ([xmpCollectMetadataStreams]), and the findings of the file
// structure checks that read each object ([structureFindings]).
//
// A validation run walks the objects once, in [resolveAllObjects], and hands
// each one to every check that needs it while it is in hand. Each check
// walking the table itself read every object once more: on a file larger than
// the object cache, every object was parsed again at each walk, about nine
// times in all. The findings are those each check's own walk made, in the same
// object-number order, so the violations and their order do not move.
//
// The walk runs on the goroutine that starts the check groups, before they
// start, and nothing writes the sweep afterwards: the groups only read it, and
// a caller that changes what it reads works on a copy.
type objectSweep struct {
	doc *document.Document

	// fileSpecs, when wanted, holds the file specifications the walk found,
	// in the order [collectFileSpecs] gathers them before it adds those of the
	// /EmbeddedFiles name tree.
	fileSpecs     []fileSpecEntry
	wantFileSpecs bool

	// metadata, when wanted, holds the metadata streams the walk found other
	// objects reference, in the order [xmpCollectMetadataRefs] found them,
	// leaving out the stream object metadataCatalog, the catalog's own packet
	// (-1 for none). metadataSeen holds them during the walk.
	metadata        []int
	metadataSeen    map[int]bool
	metadataCatalog int
	wantMetadata    bool

	// structure, when not nil, receives the findings of the file structure
	// checks.
	structure *structureFindings

	// complete is set when the walk visited every object, so that a walk ctx
	// cut short is never read as the whole document.
	complete bool
}

// structureFindings holds what the file structure checks that judge every
// object found on each, for profile. Each list is in object-number order and
// holds only the objects that fail.
type structureFindings struct {
	profile string
	part    int

	externalKeys       []objectFindings // see [checkStreamExternalKeys]
	lzw                []objectFindings // see [checkStreamLZW]
	nonStandardFilters []objectFindings // see [checkNonStandardFilters]
	numericLimits      []objectFindings // see [checkNumericLimits]
	streamLengths      []streamLength   // see [checkStreamLengths]
	type4              map[int][]Violation
}

// objectFindings is the violations one object fails.
type objectFindings struct {
	objNum     int
	violations []Violation
}

// streamLength is the /Length an uncompressed stream declares, when
// [declaredStreamLength] judges it.
type streamLength struct {
	objNum int
	length ast.PdfInt
}

// sweptObjectsKey is the context key a validation run hands its
// [objectSweep] to the check groups with.
type sweptObjectsKey struct{}

// withSweptObjects returns ctx carrying s, when s visited every object.
func withSweptObjects(ctx context.Context, s *objectSweep) context.Context {
	if !s.complete {
		return ctx
	}
	return context.WithValue(ctx, sweptObjectsKey{}, s)
}

// sweptObjectsOf returns the sweep ctx carries for doc, or nil.
func sweptObjectsOf(ctx context.Context, doc *document.Document) *objectSweep {
	s, _ := ctx.Value(sweptObjectsKey{}).(*objectSweep)
	if s == nil || s.doc != doc {
		return nil
	}
	return s
}

// newObjectSweep returns the sweep of a validation run of doc against
// profile, whose catalog is catalog. Every profile wants the file
// specifications. A PDF/A profile also wants the metadata streams and the file
// structure findings, which its groups read on every run; a PDF/UA profile
// reads the metadata streams on some documents only, and walks for them then
// ([xmpCollectMetadataStreams]).
func newObjectSweep(doc *document.Document, catalog ast.PdfDict, profile string) *objectSweep {
	s := &objectSweep{doc: doc, wantFileSpecs: true}
	if !IsPDFUA(profile) {
		s.wantMetadata = true
		s.metadataSeen = make(map[int]bool)
		s.metadataCatalog = catalogMetadataNum(catalog)
		s.structure = &structureFindings{profile: profile, part: PDFAPart(profile)}
	}
	return s
}

// catalogMetadataNum returns the object number of the catalog's own metadata
// stream, or -1 when its /Metadata is not a reference.
func catalogMetadataNum(catalog ast.PdfDict) int {
	if ref, ok := catalog["Metadata"].(ast.PdfIndirectRef); ok {
		return ref.ObjectNum
	}
	return -1
}

// walk resolves every object [sweptXRefEntries] yields, in object-number
// order, and visits each one that resolves. With checkFormat set it also
// format-checks each one, as [resolveAllObjects] does. It stops when ctx is
// done, leaving the sweep incomplete.
func (s *objectSweep) walk(ctx context.Context, checkFormat bool) int {
	doc := s.doc
	count := 0
	for objNum, entry := range sweptXRefEntries(doc) {
		if ctx.Err() != nil {
			return count
		}
		if entry.Free {
			continue
		}
		obj, err := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if checkFormat {
			doc.CheckObjectFormat(ctx, objNum)
		}
		count++
		if err != nil {
			// A resolution failure is tolerated: the checks that read the
			// object skip it, as their own walks did.
			doc.Logger.Debug("compliance: cannot resolve object in the object sweep", "object", objNum, "error", err)
			continue
		}
		s.visit(ctx, objNum, entry, obj)
	}
	if ctx.Err() == nil {
		s.complete = true
	}
	s.metadataSeen = nil
	return count
}

// visit hands object objNum, whose entry is entry and which resolved to obj,
// to every check the sweep collects for.
func (s *objectSweep) visit(ctx context.Context, objNum int, entry xref.XRefEntry, obj ast.PdfObject) {
	if s.wantFileSpecs {
		s.fileSpecs = append(s.fileSpecs, fileSpecsAt(ctx, s.doc, objNum, obj)...)
	}
	if s.wantMetadata && len(s.metadata) < maxXMPMetadataStreams {
		xmpCollectMetadataRefs(obj, s.metadataCatalog, s.metadataSeen, &s.metadata, 0)
	}
	if s.structure != nil {
		s.structure.visit(ctx, s.doc, objNum, entry, obj)
	}
}

// visit records what the file structure checks find on object objNum.
func (f *structureFindings) visit(ctx context.Context, doc *document.Document, objNum int, entry xref.XRefEntry, obj ast.PdfObject) {
	add := func(list *[]objectFindings, violations []Violation) {
		if len(violations) > 0 {
			*list = append(*list, objectFindings{objNum: objNum, violations: violations})
		}
	}
	if stream, ok := obj.(ast.PdfStream); ok && stream.Dict != nil {
		add(&f.externalKeys, streamExternalKeysAt(objNum, stream, f.profile))
		add(&f.lzw, streamLZWAt(ctx, doc, objNum, stream, f.profile))
		add(&f.nonStandardFilters, nonStandardFiltersAt(ctx, doc, objNum, stream, f.part, f.profile))
		if entry.Type == xref.XRefTypeNormal {
			if lengthObj, ok := stream.Dict["Length"]; ok {
				if length, judged := declaredStreamLength(ctx, doc, lengthObj); judged {
					f.streamLengths = append(f.streamLengths, streamLength{objNum: objNum, length: length})
				}
			}
		}
	}
	add(&f.numericLimits, numericLimitsAt(ctx, doc, objNum, obj, f.part, f.profile))
	if found := type4FunctionLimitsAt(ctx, doc, objNum, obj, f.part, f.profile); len(found) > 0 {
		if f.type4 == nil {
			f.type4 = make(map[int][]Violation)
		}
		f.type4[objNum] = found
	}
}

// sweptFileSpecs returns the file specifications the object sweep of the
// validation run ctx carries found in doc, or walks the objects for them when
// ctx carries none. The slice is the caller's.
func sweptFileSpecs(ctx context.Context, doc *document.Document) []fileSpecEntry {
	if s := sweptObjectsOf(ctx, doc); s != nil && s.wantFileSpecs {
		return slices.Clone(s.fileSpecs)
	}
	s := &objectSweep{doc: doc, wantFileSpecs: true}
	s.walk(ctx, false)
	return s.fileSpecs
}

// sweptMetadataStreams returns the metadata streams the objects of doc
// reference other than catalogMetaNum, in the order [xmpCollectMetadataRefs]
// finds them, from the object sweep ctx carries or from a walk of the objects
// when it carries none that left out the same catalog stream. The slice is
// the caller's.
func sweptMetadataStreams(ctx context.Context, doc *document.Document, catalogMetaNum int) []int {
	if s := sweptObjectsOf(ctx, doc); s != nil && s.wantMetadata && s.metadataCatalog == catalogMetaNum {
		return slices.Clone(s.metadata)
	}
	s := &objectSweep{doc: doc, wantMetadata: true, metadataSeen: make(map[int]bool), metadataCatalog: catalogMetaNum}
	s.walk(ctx, false)
	return s.metadata
}

// sweptStructureFindings returns the file structure findings for profile from
// the object sweep ctx carries for doc, or from a walk of the objects when it
// carries none for that profile. The caller only reads them.
func sweptStructureFindings(ctx context.Context, doc *document.Document, profile string) *structureFindings {
	if s := sweptObjectsOf(ctx, doc); s != nil && s.structure != nil && s.structure.profile == profile {
		return s.structure
	}
	s := &objectSweep{doc: doc, structure: &structureFindings{profile: profile, part: PDFAPart(profile)}}
	s.walk(ctx, false)
	return s.structure
}

// reachedFindings returns the violations of findings in order, each object's
// stated twice when veraPDF reaches it twice; see [cosReachedTwice].
func reachedFindings(twice map[int]bool, findings []objectFindings) []Violation {
	n := 0
	for _, f := range findings {
		n += len(f.violations)
	}
	results := make([]Violation, 0, n)
	for _, f := range findings {
		results = append(results, asReached(twice, f.objNum, f.violations)...)
	}
	return results
}
