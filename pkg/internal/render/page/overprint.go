package page

import (
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
)

// processColorants maps the process colorant names of a CMYK device to their
// components (ISO 32000-1, 8.6.6.4: a Separation or DeviceN colorant may name
// a process colorant).
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1, 8.6.6.4
var processColorants = map[ast.PdfName]uint8{"Cyan": 1, "Magenta": 2, "Yellow": 4, "Black": 8}

// allProcess has a bit for each CMYK component.
const allProcess = 0x0f

// overprintKeep returns the components of the CMYK group being painted that a
// painting in colour c leaves as they are when [Options.Overprint] simulates
// overprinting (ISO 32000-1, 8.6.7): with the overprint parameter of the
// painting set, a DeviceCMYK colour under overprint mode 1 leaves those its
// zero components name, and a Separation or DeviceN colour of process
// colorants those it does not name. It returns 0, every component painted,
// for every other painting: a colour of another space reaches the device
// converted to all four process colorants, and a colour that does not keep
// its components through to the group, under a transfer function or a
// default colour space, cannot say which are zero.
//
// A spot colorant has no plane of its own here: a colour naming one paints
// through its alternate and knocks out, where poppler's overprint preview
// keeps up to four spot planes apart.
//
// # Corpus evidence
//
//   - All corpora: 296 pages of the 191 files that paint under overprint,
//     each drawn at 36 dpi as it is and with every /OP and /op set false.
//     What overprinting changes by more than 16 levels: 617651 pixels with
//     poppler 26.09.0 -overprint, 650048 with this simulation, 569633 of them
//     the same pixels (92% of poppler's), every one changed the same way.
//     Poppler changes 47 pages, this simulation 36 of them; the other 11
//     paint Separation and DeviceN spot colours, which poppler keeps in
//     planes of their own, or change fewer than 0.7% of their pixels.
//   - pdfjs, govdocs: poppler -overprint converts its CMYK planes to RGB
//     by its own formula, so a page it draws with -overprint differs from
//     the page it draws without wherever DeviceCMYK paints; only the change
//     overprinting makes compares.
func (r *renderer) overprintKeep(s *scope, st *graphics.State, c graphics.Color, stroke bool) uint8 {
	if !r.overprint {
		return 0
	}
	on := st.FillOverprint
	if stroke {
		on = st.StrokeOverprint
	}
	sp := r.top().group.Space()
	if !on || sp.Kind() != blend.CMYK || sp.Key() != graphics.FamilyDeviceCMYK {
		return 0
	}
	if _, ok := r.nativeColor(s, c, sp); !ok {
		return 0
	}
	switch c.Space.Family {
	case graphics.FamilyDeviceCMYK:
		// 8.6.7: nonzero overprint mode applies to DeviceCMYK alone.
		if st.OverprintMode != 1 || len(c.Components) != 4 {
			return 0
		}
		var keep uint8
		for k, v := range c.Components {
			if v == 0 {
				keep |= 1 << k
			}
		}
		return keep
	case graphics.FamilySeparation, graphics.FamilyDeviceN:
		painted, ok := r.processPainted(c.Space.Object)
		if !ok {
			return 0
		}
		return allProcess &^ painted
	}
	return 0
}

// processPainted returns the CMYK components the colorants of the Separation
// or DeviceN space def name, false when one names a spot colorant or All,
// which paints every colorant (8.6.6.4). None paints nothing.
func (r *renderer) processPainted(def ast.PdfObject) (uint8, bool) {
	arr, err := document.ResolveArray(r.ctx, r.doc, def)
	if err != nil || len(arr) < 2 {
		return 0, false
	}
	names := []ast.PdfObject{arr[1]}
	if family, _ := document.ResolveName(r.ctx, r.doc, arr[0]); family == graphics.FamilyDeviceN {
		list, err := document.ResolveArray(r.ctx, r.doc, arr[1])
		if err != nil {
			return 0, false
		}
		names = list
	}
	var painted uint8
	for _, o := range names {
		name, err := document.ResolveName(r.ctx, r.doc, o)
		if err != nil {
			return 0, false
		}
		if name == "None" {
			continue
		}
		bit, ok := processColorants[name]
		if !ok {
			return 0, false
		}
		painted |= bit
	}
	return painted, true
}
