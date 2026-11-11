package sheet

// MaxCells caps the cells one workbook reads, all sheets together, with a
// value or a visible style, repetitions expanded: 1,000,000. It is an
// anti-DoS guard, not spec-mandated: a repeated row or cell is one element
// of the file and can ask for a million copies, and a million cells print
// on some twenty thousand pages, past any page cap a conversion runs with.
// Every reader (CSV, SpreadsheetML, OpenDocument) applies it.
const MaxCells = 1_000_000

// CellCost is what one cell costs the conversion's memory budget, charged
// when it is read: the cell of the model, and the structure element,
// fragments and plan entries the pages build from it, which live until
// the document is written. Its glyph runs are charged apart, when they are
// laid out. Charging the rest when the cell is read lets a workbook the
// budget cannot lay out fail while it is read, before any page is planned.
//
// # Corpus evidence
//
//   - probe, csv: 100,000 cells of numbers and short words, ten a row,
//     peak at 311 MiB of Go heap, live and garbage together, about 3.2 KiB
//     a cell, while the account holds 177 MiB.
//   - probe, csv: 1,000,000 cells are refused a third of a second in,
//     while read, where they took 62 seconds and 2.4 GiB before the
//     composition refused them.
const CellCost = 1536
