package mutations

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/crypto"
)

// ComposePlacement draws one page of a [ComposeConfig] source on a composed
// page. The caller validates it: Compose refuses an index out of range rather
// than reading past a slice, and trusts every number.
type ComposePlacement struct {
	// Source indexes [ComposeConfig.Sources].
	Source int
	// Page is the 0-based index of the source page.
	Page int
	// TargetPage is the 0-based index of the composed page.
	TargetPage int
	// X and Y position the bottom-left corner of the source page as a
	// viewer shows it, in points.
	X, Y float64
	// Scale is the uniform scale factor, positive.
	Scale float64
	// Rotation is the counter-clockwise rotation in degrees around (X, Y).
	Rotation float64
}

// ComposeConfig describes a composed document for [Compose].
type ComposeConfig struct {
	// MediaBox is the /MediaBox of every page, [0 0 width height].
	MediaBox ast.PdfArray
	// Pages is the page count of the output, at least the highest
	// TargetPage plus one.
	Pages int
	// Sources are the documents the placements draw from, each once.
	Sources []MergeSource
	// Placements are drawn in order, each on its target page.
	Placements []ComposePlacement
	// Version is the lowest version the output declares; the sources' raise
	// it.
	Version string
	// XRefStream writes a cross-reference stream.
	XRefStream bool
	// Compact selects the compaction passes.
	Compact CompactOptions
	// Encrypt encrypts the output, nil for none.
	Encrypt *crypto.EncryptConfig
}

// Compose writes a new document of cfg.Pages pages, each on cfg.MediaBox, and
// draws each placement's source page on its target
// page as a form XObject (see [pageToFormXObject]), in order. A page no
// placement names is blank.
//
// Every object of the output comes from one allocator: the composed pages,
// their content and the page tree are new, and everything a form reaches is
// copied through one copier per source, so two placements of one source
// share the objects they both reach. A source page placed twice is one form.
// The output carries the optional content groups of the sources, as a merge
// does (see [mergeOptionalContent]), so a layer a source hides stays hidden.
// It carries nothing else of their catalogs, no annotation, and no trailer
// /Info.
func Compose(ctx context.Context, cfg ComposeConfig, dst io.Writer) error {
	if len(cfg.Sources) == 0 || len(cfg.Placements) == 0 {
		return fmt.Errorf("compose: nothing to place")
	}

	alloc := writer.NewObjAllocator(0)
	version := cfg.Version
	copiers := make([]*writer.Copier, len(cfg.Sources))
	for i, src := range cfg.Sources {
		// Compared as numbers, as a merge compares them.
		version = writer.AtLeastVersion(version, sourceVersion(src.Doc))
		copiers[i] = writer.NewCopier(src.Doc, alloc)
	}

	type formKey struct{ source, page int }
	forms := make(map[formKey]ast.PdfIndirectRef)
	contents := make([]strings.Builder, cfg.Pages)
	resources := make([]ast.PdfDict, cfg.Pages)

	for i, pl := range cfg.Placements {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("compose canceled: %w", err)
		}
		if pl.Source < 0 || pl.Source >= len(cfg.Sources) ||
			pl.Page < 0 || pl.Page >= len(cfg.Sources[pl.Source].Pages) ||
			pl.TargetPage < 0 || pl.TargetPage >= cfg.Pages {
			return fmt.Errorf("compose: placement %d names no page", i)
		}

		key := formKey{pl.Source, pl.Page}
		formRef, ok := forms[key]
		if !ok {
			src := cfg.Sources[pl.Source]
			var err error
			formRef, err = pageToFormXObject(ctx, src.Doc, src.Pages[pl.Page], copiers[pl.Source], alloc)
			if err != nil {
				return fmt.Errorf("placement %d: converting source page %d to a form xobject: %w", i, pl.Page+1, err)
			}
			forms[key] = formRef
		}

		name := ast.PdfName(fmt.Sprintf("Fm%d", formRef.ObjectNum))
		if resources[pl.TargetPage] == nil {
			resources[pl.TargetPage] = ast.PdfDict{}
		}
		resources[pl.TargetPage][name] = formRef

		// Not marked as an /Artifact, unlike a stamp (see
		// [applyStampToPages]): a stamp adds to a page that has content of
		// its own, and a placement is the composed page's content. Marked,
		// its text would vanish from text extraction, which strips
		// artifacts by default, and the page would read as empty.
		b := &contents[pl.TargetPage]
		b.WriteString("q ")
		b.WriteString(buildTransformMatrix(pl.X, pl.Y, pl.Scale, pl.Scale, pl.Rotation))
		fmt.Fprintf(b, "/%s Do Q\n", string(name))
	}

	pageRefs := make([]ast.PdfObject, cfg.Pages)
	for i := range cfg.Pages {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("compose canceled: %w", err)
		}
		// ISO 32000-1, 7.7.3.3, Table 30 requires /Resources of every page,
		// so a blank page states an empty dictionary.
		pageResources := ast.PdfDict{}
		page := ast.PdfDict{
			"Type":      ast.PdfName("Page"),
			"MediaBox":  append(ast.PdfArray(nil), cfg.MediaBox...),
			"Resources": pageResources,
		}
		if xobjects := resources[i]; xobjects != nil {
			pageResources["XObject"] = xobjects
			page["Contents"] = alloc.Alloc(ast.NewStream(ast.PdfDict{}, []byte(contents[i].String())))
		}
		pageRefs[i] = alloc.Alloc(page)
	}

	catalog := ast.PdfDict{
		"Type":  ast.PdfName("Catalog"),
		"Pages": allocPageTree(alloc, pageRefs),
	}
	mergeOptionalContent(ctx, cfg.Sources, copiers, catalog)
	catalogRef := alloc.Alloc(catalog)
	if err := completeRewrittenPageTree(ctx, alloc, catalogRef, &document.MediaBoxDefault{}, nil); err != nil {
		return err
	}

	if version == "" {
		version = "1.4"
	}
	trailer := ast.PdfDict{"Root": catalogRef}
	return finishRewrite(ctx, dst, cfg.Sources[0].Doc, alloc, trailer, version, cfg.XRefStream, cfg.Compact, cfg.Encrypt, "composed output")
}
