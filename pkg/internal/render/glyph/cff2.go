package glyph

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// parseCFF2Sfnt reads an OpenType program whose outlines are a CFF2 table
// (OpenType specification, CFF2 table): its default instance, and the hmtx
// advances its charstrings do not state ('hmtx' and HVAR tables).
//
// ISO 32000-2, 9.9, Table 126 lets /FontFile3 /OpenType carry such a program.
// veraPDF does not count a CFF2 table as the CFF program a Type 1 or
// CIDFontType0 font embeds, and compliance follows it, but a viewer draws it:
// poppler 26.09.0 draws all five CFF2 fonts of litho fontcff2_2b.pdf.
func parseCFF2Sfnt(data []byte, tables *sfntTables) (program, error) {
	table, err := truetype.CFF2Table(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadProgram, err)
	}
	f, err := cff.ParseCFF2(table)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadProgram, err)
	}
	// Metrics that will not read leave every advance 0 rather than refusing
	// the outlines the program holds: only a substitute's width scaling reads
	// the advance, and it skips a glyph whose advance is 0.
	hmtx, err := truetype.ReadHMetrics(data)
	if err != nil {
		hmtx = truetype.HMetrics{}
	}
	return &cffProgram{font: f, tables: tables, hmtx: &hmtx, n: len(data)}, nil
}
