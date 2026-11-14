package ods

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestSparklines_Read locks Calc's calcext:sparkline-groups: a drawing of
// each sparkline's cell drawing its range's values, and the row of that
// cell keeping its own height though no content follows it.
func TestSparklines_Read(t *testing.T) {
	t.Parallel()
	body := table("S", `<table:table-row><table:table-cell office:value-type="float" office:value="1"/><table:table-cell office:value-type="float" office:value="-1"/></table:table-row>`+
		`<table:table-row table:style-name="tall"><table:table-cell/></table:table-row>`+
		`<calcext:sparkline-groups><calcext:sparkline-group calcext:type="column" calcext:negative="true" calcext:color-series="#00ff00" calcext:color-negative="#ff0000">`+
		`<calcext:sparklines><calcext:sparkline calcext:cell-address="S.A2" calcext:data-range="S.A1:S.B1"/></calcext:sparklines></calcext:sparkline-group></calcext:sparkline-groups>`)
	auto := `<style:style style:name="tall" style:family="table-row"><style:table-row-properties style:row-height="2cm" style:use-optimal-row-height="false"/></style:style>`
	doc, _ := read(t, flat{auto: auto, body: body}.bytes())
	sh := doc.Sheets[0]
	if len(sh.Drawings) != 1 || sh.Drawings[0].From.Row != 1 || sh.Drawings[0].To.Row != 2 {
		t.Fatalf("drawings %+v", sh.Drawings)
	}
	tall := false
	for _, r := range sh.Rows {
		if r.Index == 1 && r.Height > 50 {
			tall = true
		}
	}
	if !tall {
		t.Errorf("the sparkline's row lost its height: %+v", sh.Rows)
	}
	var fills []typeset.Color
	for _, f := range sh.Drawings[0].Picture.Vector(t.Context(), typeset.Rect{Width: 40, Height: 40}) {
		if p, ok := f.(*typeset.Path); ok && p.Fill != nil {
			fills = append(fills, p.Fill.Color)
		}
	}
	if len(fills) != 2 || fills[0] != (typeset.Color{G: 1, A: 1}) || fills[1] != (typeset.Color{R: 1, A: 1}) {
		t.Errorf("columns %v", fills)
	}
}
