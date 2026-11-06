package text

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

func FuzzLoadFontData(f *testing.F) {
	valid := testSans("Regular", 400, false, 100)
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Add(fontbuild.Collection(valid, testDigits()))
	f.Fuzz(func(t *testing.T, data []byte) {
		fonts := memoryFonts(map[string][]byte{"fuzz.ttf": data})
		if fonts.Len() == 0 {
			return
		}
		for _, fc := range fonts.faces {
			if _, err := fc.Compose(); err != nil {
				continue
			}
		}
		if _, _, err := fonts.Layout(context.Background(), []Run{{Text: "AB Cé D", Style: Style{Size: 10}}}, 20, Options{}); err != nil {
			t.Fatalf("Layout on a fuzzed face: %v", err)
		}
	})
}

// memoryFile is a font file held in memory.
type memoryFile struct {
	*bytes.Reader
}

// Close does nothing.
func (memoryFile) Close() error { return nil }

// memoryFonts indexes files held in memory, in lexical order of their
// names, as [NewFonts] indexes a directory.
func memoryFonts(files map[string][]byte) *Fonts {
	f := newIndex(nil, func(path string) (sfntFile, int64, error) {
		data := files[path]
		return memoryFile{bytes.NewReader(data)}, int64(len(data)), nil
	})
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	budget := maxFontIndexBytes
	for _, name := range names {
		f.indexFile(name, &budget)
	}
	return f
}
