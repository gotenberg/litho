package text

import (
	"bytes"
	"sync"
	"testing"
)

// TestFace_ComposeEvictsPastTheByteCap locks the bound of the composed
// programs: past it the least recently used one leaves, the one just read
// stays, and a program that left is read again, equal to the first.
func TestFace_ComposeEvictsPastTheByteCap(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	f.maxComposed = 1
	bold := style()
	bold.Weight = 700
	regular, heavy := f.Match(style()), f.Match(bold)
	first, err := regular.Compose()
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := regular.Compose(); again != first { //nolint:errcheck // the first call succeeded on the same file
		t.Error("a cached program is read again")
	}
	if _, err := heavy.Compose(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	evicted, kept, held := regular.composed == nil, heavy.composed != nil, f.composedBytes
	f.mu.Unlock()
	if !evicted || !kept {
		t.Errorf("evicted %v, kept %v: want the older program out and the newer in", evicted, kept)
	}
	if held != int64(len(heavy.composed.Program)) {
		t.Errorf("cache holds %d bytes, want the last program's %d", held, len(heavy.composed.Program))
	}
	back, err := regular.Compose()
	if err != nil {
		t.Fatal(err)
	}
	if back == first || back.Key != first.Key || !bytes.Equal(back.Program, first.Program) {
		t.Error("a program read again differs, or was never evicted")
	}
}

// TestFace_ComposeConcurrent composes and lays out with the faces of one
// index from many goroutines, under a cap that evicts on every call, so
// the race detector sees the cache and the shaping kits shared.
func TestFace_ComposeConcurrent(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	f.maxComposed = 1
	f.maxLoaded = 1
	bold, italic := style(), style()
	bold.Weight = 700
	italic.Italic = true
	styles := []Style{style(), bold, italic}
	want := make([][]Line, len(styles))
	for i, s := range styles {
		want[i], _ = layout(t, f, "AB BA", s, 100, Collapse)
	}
	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for g := range 8 {
		wg.Go(func() {
			for i := range 30 {
				k := (g + i) % len(styles)
				fc := f.Match(styles[k])
				cf, err := fc.Compose()
				if err != nil || cf.Key != fc.Key() {
					errs <- "compose " + fc.Key()
				}
				lines, _, err := f.Layout(t.Context(), []Run{{Text: "AB BA", Style: styles[k]}}, 100, Options{})
				if err != nil || len(lines) != 1 || lines[0].Width != want[k][0].Width || lineText(lines[0]) != lineText(want[k][0]) {
					errs <- "layout " + fc.Key()
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
