package recolor

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// sessionColors is the [Colors] a session converts its images through: the
// session's own plans, so an image and a colour operator in one space convert
// alike, and a kept spot colour is written once for both.
//
// It reads no page dictionary: the page's own /OutputIntents (ISO 32000-2,
// 14.11.5) do not change what a device space means here, as they do not for
// the colour operators, so a page carrying them converts through the
// document's reading of its device spaces ([Options.DeviceCMYK] and its
// siblings). Nor does it read an image's own /Intent: the conversion converts
// every colour under [Options.Intent].
//
// TODO: [recolour] read a page's /OutputIntents for its device spaces, in the
// content pass and here alike, as the renderer does; no corpus file states
// them.
type sessionColors struct {
	s *session
}

// Converter returns the session's conversion of the colour space space, read
// against resources, whose default colour spaces stand in for a device space
// (ISO 32000-1, 8.6.5.6). A space that already is the target converts as the
// identity.
func (c sessionColors) Converter(ctx context.Context, space ast.PdfObject, resources, _ ast.PdfDict, _ ast.PdfName) (Converter, error) {
	p := c.s.planDefinition(ctx, space, resources)
	switch p.kind {
	case planConvert:
		return p.conv, nil
	case planIdentity:
		if !p.fixed && p.n == c.s.t.n {
			return identity{}, nil
		}
	case planUnknown:
		return nil, p.err
	case planKeep, planPattern:
	}
	return nil, fmt.Errorf("%w: a space whose colour values stay has no conversion to the target", ErrUnsupportedSpace)
}

// SpotSpace returns the Separation or DeviceN space it is given as the
// session keeps it: its colorants and tints as they are, its alternate the
// target and its tint transform a sampled function into it (ISO 32000-1,
// 8.6.6.4 and 8.6.6.5). A space written as a reference is rewritten at its number, once,
// and comes back as the reference; a direct one comes back as the rewritten
// array the plan holds, the same array each time. A spot colour whose
// alternate already is the target comes back as it is.
func (c sessionColors) SpotSpace(ctx context.Context, space ast.PdfObject, _, _ ast.PdfDict) (ast.PdfObject, error) {
	p := c.s.planDefinition(ctx, space, nil)
	switch {
	case p.kind == planUnknown:
		return nil, p.err
	case p.kind == planIdentity && p.spot:
		return space, nil
	case p.kind == planKeep && p.spot && p.def != nil:
		if _, isRef := space.(ast.PdfIndirectRef); isRef {
			if _, _, err := c.s.rewriteDefinition(ctx, space); err != nil {
				return nil, err
			}
			return space, nil
		}
		return p.def, nil
	}
	return nil, fmt.Errorf("%w: no spot colour the conversion keeps", ErrUnsupportedSpace)
}
