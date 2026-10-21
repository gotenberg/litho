package pdf

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"runtime"

	"github.com/gotenberg/litho/pkg/internal/render/glyph"
)

// WithRenderFontProvider draws the fonts a document shows without embedding
// them with the programs p supplies, as [WithFontProvider] supplies them to a
// remediation. litho bundles no font program: without a provider, such a
// font draws each glyph as a hollow box as wide as the width the font
// declares.
//
// [Reader.RenderPage] asks p for each such font by its /BaseFont; for a
// CIDFont of an Adobe CJK character collection (Adobe-GB1, -CNS1, -Japan1,
// -Korea1), then by the PostScript names of the Noto Sans CJK and Source Han
// Sans Regular faces of the collection's region (NotoSansJP-Regular,
// NotoSansCJKjp-Regular, SourceHanSansJP-Regular for Adobe-Japan1); then by the
// standard 14 font whose metrics stand in for it (ISO 32000-1, 9.6.2.2),
// chosen by name or by its descriptor's /Flags, and draws with the first
// program that parses: a TrueType or OpenType program, a bare CFF program or
// a Type 1 program. [FontRequest.Codes] is empty. A glyph the program lacks
// draws a box, a code the font's encoding names no glyph for draws nothing,
// and a glyph whose advance differs from the declared width is
// scaled to it, so text keeps the positions the document gives it. An error
// p returns is logged at Debug and the font draws boxes.
//
// Repeated, the option adds providers, asked in order. The page drawn then
// depends on the programs p supplies. p must be safe for concurrent use when
// RenderPage runs concurrently.
func WithRenderFontProvider(p FontProvider) RenderOption {
	return func(o *renderOpts) {
		if p != nil {
			o.fonts = append(o.fonts, p)
		}
	}
}

// WithRenderFontFiles draws the fonts a document shows without embedding
// them with programs read from the font files of fsys, matched as
// [WithFontFiles] matches them: by PostScript name, by family and style, or
// by a metric-compatible family (Nimbus Sans or Arial for Helvetica), so a
// substitute draws with the advances the document declares. A font no file
// serves draws boxes ([WithRenderFontProvider]).
//
// The files are indexed once, at the first font a render asks for, and the
// index is kept for every render the option value serves: reuse the option
// across calls to index once. An error walking or reading fsys is logged at
// Debug and the font draws boxes.
func WithRenderFontFiles(fsys fs.FS) RenderOption {
	return WithRenderFontProvider(newFontFiles(fsys))
}

// WithRenderSystemFonts draws the fonts a document shows without embedding
// them with programs read from the operating system's font directories, the
// directories [WithSystemFonts] searches, matched as [WithRenderFontFiles]
// matches them, after every provider [WithRenderFontProvider] and
// [WithRenderFontFiles] registered. The page drawn then depends on the fonts
// installed where litho runs, which is why the option is off by default.
//
// The directories are indexed once, at the first font a render asks for, and
// the index is kept for every render the option value serves.
func WithRenderSystemFonts() RenderOption {
	home, err := os.UserHomeDir()
	if err != nil {
		// No home directory leaves the per-user directories out; the
		// system-wide ones still serve.
		home = ""
	}
	p := newSystemFonts(systemFontDirs(runtime.GOOS, os.Getenv, home))
	return func(o *renderOpts) {
		o.systemFonts = p
	}
}

// renderFontProvider adapts providers to the glyph loader: each is asked in
// turn, and the programs they offer are the font's candidates, in provider
// order. The providers of [WithRenderFontFiles] and [WithRenderSystemFonts]
// offer every face that serves the font, best first, read when the loader
// reaches it. A provider's error ends the request: the font then draws
// placeholders, and the glyph cache asks again at the next font load.
func renderFontProvider(logger *slog.Logger, providers []FontProvider) glyph.Provider {
	if len(providers) == 0 {
		return nil
	}
	return func(ctx context.Context, req glyph.Request) ([]glyph.Candidate, error) {
		out := FontRequest{
			BaseFont: req.BaseFont,
			Subtype:  req.Subtype,
			Flags:    req.Flags,
			Encoding: req.Encoding,
			Ordering: req.Ordering,
		}
		var candidates []glyph.Candidate
		for _, p := range providers {
			if files, ok := p.(*fontFiles); ok {
				offered, err := files.candidates(ctx, logger, out)
				if err != nil {
					return nil, err
				}
				for _, c := range offered {
					candidates = append(candidates, glyph.Candidate(c))
				}
				continue
			}
			program, err := p.FontProgram(ctx, out)
			if err != nil {
				return nil, err
			}
			if program != nil {
				candidates = append(candidates, func(context.Context) ([]byte, error) { return program, nil })
			}
		}
		return candidates, nil
	}
}
