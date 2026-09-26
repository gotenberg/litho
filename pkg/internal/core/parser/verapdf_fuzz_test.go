package parser

import (
	"context"
	"testing"
)

// FuzzVeraPDFContainers feeds arbitrary content-stream bytes to a parser that
// notes the containers veraPDF gives up on ([Parser.WithVeraPDFContainers]),
// through ParseObject and ParseInlineImageHeader, and asserts it never panics
// and that a parser without it never reports one.
func FuzzVeraPDFContainers(f *testing.F) {
	for _, s := range []string{
		"[1 yy] [1e2] [(a) - (b)] [1 0 R] <</A 1 2 /B>> <</A 1 2 /B 3>> <</A xx>> <</A>> <<xx /A 1>>",
		"/W 1 /H 1 /Foo 1.5e1 ID \x80 EI ",
		"<</A endobj /B stream>>",
		"<</A 1 2",
	} {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}
		for _, containers := range []bool{true, false} {
			p := veraPDFParser(string(data), containers)
			for range 1000 {
				if _, err := p.ParseObject(context.Background()); err != nil {
					break
				}
			}
			if !containers && p.Abandoned() {
				t.Fatal("a parser without WithVeraPDFContainers reports a container veraPDF gives up on")
			}
			// Crash-freedom only: damaged input fails by design.
			p = veraPDFParser(string(data), containers)
			_, _ = p.ParseInlineImageHeader(context.Background())
		}
	})
}
