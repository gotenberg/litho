package pdf

import (
	"context"
	"io/fs"
	"os"
	"runtime"

	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// FontSource offers the font programs that draw text the standard 14 fonts
// cannot. A standard font writes its text in WinAnsiEncoding (ISO 32000-1,
// Annex D.2), which covers Western European text and nothing else. For a
// line outside it, [Writer.FlattenForm] with [WithFlattenFonts] and
// [Writer.StampText] with [TextStamp.Fonts] take the first program the source
// offers that has a glyph for every character of the line, matched to the
// standard font's family and style first, then any other, subset it to the
// glyphs the line uses, and embed it as a Type0 font with the Identity-H
// encoding and a ToUnicode CMap, so the text extracts and searches. A
// TrueType program is embedded with its glyf outlines, an OpenType program
// with CFF outlines as a CIDFontType0 over its CFF table (ISO 32000-1, 9.9).
//
// The letters are drawn one glyph per character. A script whose letters join
// or reorder, such as Arabic or Devanagari, comes out unshaped.
//
// The zero value offers nothing. The licence to embed a program is the
// caller's. A source shared by concurrent writes is safe to share.
type FontSource struct {
	source mutations.TextFontSource
}

// NewSystemFontSource offers the fonts installed on the system, read from the
// directories [WithSystemFonts] reads, indexed once on first use. The output
// then depends on the fonts installed where litho runs.
func NewSystemFontSource() FontSource {
	home, err := os.UserHomeDir()
	if err != nil {
		// No home directory leaves the per-user directories out; the
		// system-wide ones still serve.
		home = ""
	}
	return newFontFilesSource(newSystemFonts(systemFontDirs(runtime.GOOS, os.Getenv, home)))
}

// NewFontFileSource offers the TrueType and OpenType files of fsys, searched
// recursively as [WithFontFiles] searches them, indexed once on first use.
func NewFontFileSource(fsys fs.FS) FontSource {
	return newFontFilesSource(newFontFiles(fsys))
}

// NewFontProviderSource offers the one program p returns for a line, asked
// with the standard font's name as the request's BaseFont, TrueType as its
// Subtype and the line's characters as its Codes.
func NewFontProviderSource(p FontProvider) FontSource {
	if p == nil {
		return FontSource{}
	}
	return FontSource{source: func(ctx context.Context, req mutations.TextFontRequest) ([]mutations.TextFontCandidate, error) {
		program, err := p.FontProgram(ctx, textFontRequest(req))
		if err != nil || program == nil {
			return nil, err
		}
		return []mutations.TextFontCandidate{func(context.Context) ([]byte, error) { return program, nil }}, nil
	}}
}

// newFontFilesSource offers every face files indexes: the faces that serve
// the standard font first, as [WithFontFiles] ranks them, then the rest in
// index order, so a line no face of the family covers still finds one.
func newFontFilesSource(files *fontFiles) FontSource {
	return FontSource{source: func(ctx context.Context, req mutations.TextFontRequest) ([]mutations.TextFontCandidate, error) {
		logger := orDiscard(nil)
		indexed, err := files.index(ctx, logger)
		if err != nil {
			return nil, err
		}
		ranked := matchFontFiles(indexed, textFontRequest(req))
		offered := make(map[*fontFile]bool, len(ranked))
		out := make([]mutations.TextFontCandidate, 0, len(indexed))
		read := func(file *fontFile) mutations.TextFontCandidate {
			return func(context.Context) ([]byte, error) { return files.load(logger, file) }
		}
		for _, file := range ranked {
			offered[file] = true
			out = append(out, read(file))
		}
		for i := range indexed {
			if file := &indexed[i]; !offered[file] {
				out = append(out, read(file))
			}
		}
		return out, nil
	}}
}

// textFontRequest translates the request of a line of text into the one a
// [FontProvider] answers.
func textFontRequest(req mutations.TextFontRequest) FontRequest {
	base := req.StandardFont
	if base == "" {
		base = "Helvetica"
	}
	out := FontRequest{BaseFont: base, Subtype: "TrueType"}
	for _, r := range req.Text {
		out.Codes = append(out.Codes, FontCode{Code: uint32(r), Unicode: r}) //nolint:gosec // a rune is at most 0x10FFFF
	}
	return out
}

// FlattenFormOption tunes [Writer.FlattenForm].
type FlattenFormOption func(*flattenFormOpts)

type flattenFormOpts struct {
	fonts FontSource
}

// WithFlattenFonts offers the fonts that draw a field's value when the form's
// own font cannot: a value outside WinAnsiEncoding. Without it, such a value
// fails [Writer.WriteTo] with [ErrFlattenIncomplete] wrapping
// [ErrNoFontCovers].
func WithFlattenFonts(src FontSource) FlattenFormOption {
	return func(o *flattenFormOpts) {
		o.fonts = src
	}
}

// JoinFontSources offers what each source offers, in that order: the
// programs of the first, then the second's, and so on. A zero source offers
// nothing and is skipped.
func JoinFontSources(sources ...FontSource) FontSource {
	var live []mutations.TextFontSource
	for _, s := range sources {
		if s.source != nil {
			live = append(live, s.source)
		}
	}
	switch len(live) {
	case 0:
		return FontSource{}
	case 1:
		return FontSource{source: live[0]}
	}
	return FontSource{source: func(ctx context.Context, req mutations.TextFontRequest) ([]mutations.TextFontCandidate, error) {
		var out []mutations.TextFontCandidate
		for _, s := range live {
			offered, err := s(ctx, req)
			if err != nil {
				return nil, err
			}
			out = append(out, offered...)
		}
		return out, nil
	}}
}
