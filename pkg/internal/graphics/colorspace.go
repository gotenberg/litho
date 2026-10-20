package graphics

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// DescribeColorSpace reads the colour space definition obj, a family name or
// an array whose first element is the family (ISO 32000-1, 8.6.3), as a cs
// operator selecting it through a resource would: the family, the component
// count and the resolved definition. It is for a definition that names no
// resource, such as the /ColorSpace of a shading (8.7.4.5, Table 78) or of an
// image (8.9.5, Table 89). It returns an error when obj resolves to nothing
// that names a space, or to a family Table 62 does not list.
func DescribeColorSpace(ctx context.Context, doc *document.Document, obj ast.PdfObject) (ColorSpace, error) {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return ColorSpace{}, fmt.Errorf("resolving colour space: %w", err)
	}
	m := &Machine{Doc: doc}
	cs := m.describeColorSpace(ctx, resolved, true)
	if cs.Family == "" {
		return ColorSpace{}, fmt.Errorf("colour space %v names no family of ISO 32000-1, 8.6.3, Table 62", resolved)
	}
	return cs, nil
}
