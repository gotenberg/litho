package mutations

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// shiftedOriginStampDoc builds a one-page document whose MediaBox does NOT
// start at the origin, so [applyStampToPages] takes its stampOriginShift
// branch and allocates a second, translated copy of the stamp content.
//
// stampDoc's page starts at 0 0, which only ever exercises the shared stamp
// stream. The shifted copy interpolates the same operator string a second
// time, so a repair applied to one materialisation and not the other would be
// correct on ordinary pages and silently wrong here.
func shiftedOriginStampDoc(t *testing.T) (context.Context, *document.Document, []*document.Page, *writer.ObjAllocator) {
	t.Helper()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [10 20 210 220] /Contents 4 0 R >>",
		"<< /Length 0 >>\nstream\n\nendstream",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	return ctx, doc, pages, writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
}

// TestStampText_ContentIsMarkedAsArtifact pins the marking a stamp owes a
// tagged document.
//
// ISO 14289-1 §7.1 test 3 requires page content to be tagged as real content
// or marked as an artifact, and ISO 32000-1 §14.8.2.2 names watermarks as
// artifacts. A stamp is appended after the structure tree exists, so it can
// never be tagged: declaring it an artifact is the only correct outcome.
//
// Measured before this was pinned: stamping an autotagged page took PDF/UA-1
// from 4 violations to 7.
//
// The bare /Artifact name is what the validator recognises. classifyMarker in
// compliance/accessibility_content.go returns markerArtifact on operand 0 being
// the name Artifact and requires no property list, and graphics/execute.go
// agrees, so no subtype dictionary is needed for either the checker or the
// extractor to see it.
func TestStampText_ContentIsMarkedAsArtifact(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := stampDoc(t)
	if err := StampText(ctx, alloc, doc, pages, TextStampOptions{
		Text:     "PlainStamp",
		FontSize: 12,
	}, nil); err != nil {
		t.Fatalf("StampText: %v", err)
	}

	content := stampedContent(t, ctx, alloc)
	if !strings.Contains(content, "(PlainStamp) Tj") {
		t.Fatalf("precondition: the stamp was not drawn at all: %q", content)
	}
	if !strings.Contains(content, "/Artifact BMC") {
		t.Errorf("stamp content is not marked as an artifact: %q", content)
	}
	if !strings.Contains(content, "EMC") {
		t.Errorf("stamp content opens a marked-content sequence it never closes: %q", content)
	}
}

// TestStampImage_ContentIsMarkedAsArtifact covers a second builder.
//
// All three stamp kinds funnel through applyStampToPages, so marking there
// covers them by construction. This asserts it rather than assuming it: an
// image stamp emits a Do, not a Tj, and takes none of the text builder's path.
func TestStampImage_ContentIsMarkedAsArtifact(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := stampDoc(t)
	if err := StampImage(ctx, alloc, doc, pages, ImageStampOptions{
		Data:     minimalPNG(),
		MimeType: "image/png",
		X:        10,
		Y:        10,
		Width:    20,
		Height:   20,
	}, nil); err != nil {
		t.Fatalf("StampImage: %v", err)
	}

	content := stampedContent(t, ctx, alloc)
	if !strings.Contains(content, " Do") {
		t.Fatalf("precondition: the image stamp was not drawn: %q", content)
	}
	if !strings.Contains(content, "/Artifact BMC") {
		t.Errorf("image stamp content is not marked as an artifact: %q", content)
	}
}

// TestStampOnAShiftedOriginPageIsAlsoMarked covers the second place the
// operator string is materialised.
//
// applyStampToPages builds one shared stamp stream, and for a page whose box
// does not start at the origin it allocates a translated COPY that interpolates
// the same string again. A repair written at one of the two sites passes on an
// ordinary page and fails only here, which is the least likely case to be
// noticed by hand.
func TestStampOnAShiftedOriginPageIsAlsoMarked(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := shiftedOriginStampDoc(t)
	if err := StampText(ctx, alloc, doc, pages, TextStampOptions{
		Text:     "ShiftedStamp",
		FontSize: 12,
	}, nil); err != nil {
		t.Fatalf("StampText: %v", err)
	}

	content := stampedContent(t, ctx, alloc)
	// Precondition, not decoration: if the shift ever stopped triggering, every
	// assertion below would pass while testing the ordinary path twice. The
	// translated copy is the only thing that emits a bare "q 1 0 0 1 " prefix,
	// because a stamp with no rotation writes its position through Td instead.
	if !strings.Contains(content, "q 1 0 0 1 ") {
		t.Fatalf("precondition: the shifted-origin copy was not built, so this tests nothing new: %q", content)
	}
	if !strings.Contains(content, "/Artifact BMC") {
		t.Errorf("the translated stamp copy is not marked as an artifact: %q", content)
	}
}
