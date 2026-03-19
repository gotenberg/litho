package graphics

import (
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// evaluateOCMD evaluates an inline Optional Content Membership Dictionary and
// returns true if the content should be visible. It defaults to visible for
// malformed dictionaries or unknown layer names.
func (m *Machine) evaluateOCMD(dict ast.PdfDict) bool {
	ocgsObj, ok := dict["OCGs"]
	if !ok {
		return true
	}

	// Collect layer names from /OCGs (single name or array).
	var names []string
	switch v := ocgsObj.(type) {
	case ast.PdfName:
		names = append(names, string(v))
	case ast.PdfArray:
		for _, item := range v {
			if n, ok := item.(ast.PdfName); ok {
				names = append(names, string(n))
			}
		}
	}

	if len(names) == 0 {
		return true
	}

	// Determine the policy; default is AnyOn.
	policy := ast.PdfName("AnyOn")
	if p, ok := dict["P"]; ok {
		if pn, ok := p.(ast.PdfName); ok {
			policy = pn
		}
	}

	switch policy {
	case "AllOn":
		for _, name := range names {
			visible, exists := m.ActiveOCs[name]
			if !exists {
				continue
			}
			if !visible {
				return false
			}
		}
		return true

	case "AnyOff":
		for _, name := range names {
			visible, exists := m.ActiveOCs[name]
			if !exists {
				continue
			}
			if !visible {
				return true
			}
		}
		return false

	case "AllOff":
		for _, name := range names {
			visible, exists := m.ActiveOCs[name]
			if !exists {
				continue
			}
			if visible {
				return false
			}
		}
		return true

	default: // AnyOn
		for _, name := range names {
			visible, exists := m.ActiveOCs[name]
			if !exists {
				continue
			}
			if visible {
				return true
			}
		}
		return false
	}
}
