package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gotenberg/litho/pkg/pdf"
)

// textFontFlags are the --font-dir and --system-fonts flags of the commands
// that draw a line of text with a standard font: flatten-form, for the values
// it draws into the page, and stamp-text. A standard font writes
// WinAnsiEncoding only, so a line outside it, Cyrillic or Chinese for
// instance, takes a program these flags offer that covers it.
type textFontFlags struct {
	dir    *string
	system *bool
}

// registerTextFontFlags declares the flags on fs.
func registerTextFontFlags(fs *flag.FlagSet) textFontFlags {
	return textFontFlags{
		dir: fs.String("font-dir", "",
			"Directory of TrueType and OpenType font files, searched recursively, to draw the text the standard fonts cannot: a line outside WinAnsiEncoding takes the first file of the font's family that covers it, then any that does"),
		system: fs.Bool("system-fonts", false,
			"Also search the operating system's font directories, after --font-dir, for the text the standard fonts cannot draw; the output then depends on the fonts installed"),
	}
}

// source returns the fonts the flags offer: none when --font-dir is empty and
// --system-fonts unset, or an error when --font-dir names no readable
// directory.
func (f textFontFlags) source() (pdf.FontSource, error) {
	var sources []pdf.FontSource
	if *f.dir != "" {
		info, err := os.Stat(*f.dir)
		if err != nil {
			return pdf.FontSource{}, fmt.Errorf("--font-dir: %w", err)
		}
		if !info.IsDir() {
			return pdf.FontSource{}, fmt.Errorf("--font-dir: %s is not a directory", *f.dir)
		}
		sources = append(sources, pdf.NewFontFileSource(os.DirFS(*f.dir)))
	}
	if *f.system {
		sources = append(sources, pdf.NewSystemFontSource())
	}
	return pdf.JoinFontSources(sources...), nil
}
