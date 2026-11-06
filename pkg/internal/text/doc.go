// Package text shapes and lays out text for the converters: styled runs in,
// lines of positioned glyph runs ([compose.Text]) out.
//
// It wraps go-text/typesetting (Unlicense OR BSD-3-Clause), the pure-Go
// port of HarfBuzz, and nothing else in litho draws text.
//
// # Pipeline
//
//   - [NewFonts] indexes the font files under a set of directories once,
//     reading only the tables a face's footprint needs: names, aspect,
//     vertical metrics and character coverage. [Fonts.Match] resolves a
//     [Style] to a face the way fontconfig does: exact family, then
//     metric-compatible aliases, then generic preference lists, then the
//     closest aspect. [Fonts.Layout] falls back per character to a face
//     that covers it, and parses the program of a face on first use into a
//     bounded cache.
//   - [Fonts.Layout] normalizes white space ([Whitespace]), splits the text
//     into paragraphs at forced breaks, resolves each paragraph's direction
//     from its first strong character (UAX #9, P2 and P3), itemizes it by
//     direction, script, style and face, shapes each item with HarfBuzz and
//     breaks the result into lines at UAX #14 opportunities, falling back to
//     grapheme boundaries (UAX #29) for a word wider than the line.
//
// # Coordinates
//
// Lengths are points, y grows downward, as in [compose]. A [Line] places its
// runs relative to its left baseline point.
//
// # Concurrency
//
// A [Fonts] is safe for concurrent use once [NewFonts] returns. Every
// [Fonts.Layout] call shapes with its own HarfBuzz state and go-text faces,
// so concurrent calls share only the read-only index and a mutex-guarded
// cache of parsed fonts, which go-text never mutates.
//
// # Determinism
//
// For a given set of font files, the same [Fonts.Match] and [Fonts.Layout]
// inputs produce identical results, whatever the order of concurrent calls
// and whichever fonts the cache holds. No map iteration decides an order.
package text
