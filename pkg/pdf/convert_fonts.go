package pdf

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/text"
)

// maxConvertFontIndexes bounds the font indexes [convertFonts] keeps, one per
// list of directories. Anti-DoS guard, not spec-mandated: an index holds up
// to 256 MiB of parsed fonts and font programs, so a caller cycling through
// directory lists must not pile them up. A service converts with one list, the system
// directories or its own, and rarely with more than two.
const maxConvertFontIndexes = 4

// errFontIndexAbandoned stands for an index whose build stopped without a
// result, a panic recovered further up: callers waiting on it build again.
var errFontIndexAbandoned = errors.New("font index build abandoned")

// convertFontIndex is one cached index, ready once its channel closes.
type convertFontIndex struct {
	ready chan struct{}
	fonts *text.Fonts
	err   error
}

// convertFontCache holds the font indexes of the conversions, keyed by directory
// list. Indexing a desktop's font directories takes seconds, which every
// call would otherwise pay again.
//
// The key keeps the order of the list rather than sorting it, because the
// order decides between faces that match equally: a per-user directory
// listed first wins over a system one, and two orders of one set are two
// indexes. Entries leave oldest first past [maxConvertFontIndexes].
var convertFontCache = struct {
	sync.RWMutex
	entries map[string]*convertFontIndex
	// order lists the keys oldest first.
	order []string
}{entries: make(map[string]*convertFontIndex)}

// convertFontKey returns the cache key of dirs: each cleaned, in order.
func convertFontKey(dirs []string) string {
	clean := make([]string, len(dirs))
	for i, d := range dirs {
		clean[i] = filepath.Clean(d)
	}
	return strings.Join(clean, "\x00")
}

// convertFonts returns the font index of dirs, built once and shared by every
// later call with the same list. Concurrent first calls build it once: the
// others wait for it, or for their ctx. A build that fails, which only ctx
// ending does, is not kept.
func convertFonts(ctx context.Context, logger *slog.Logger, dirs []string) (*text.Fonts, error) {
	key := convertFontKey(dirs)
	for {
		e, build := claimConvertFontIndex(key)
		if build {
			return buildConvertFontIndex(ctx, logger, key, dirs, e)
		}
		select {
		case <-e.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if e.err == nil {
			return e.fonts, nil
		}
		// The build failed for its own caller's ctx, or was abandoned, and
		// left the cache; this call's ctx decides whether to try again.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
}

// claimConvertFontIndex returns the entry of key and whether the caller must
// build it, which it must when the entry is new.
func claimConvertFontIndex(key string) (*convertFontIndex, bool) {
	convertFontCache.RLock()
	e := convertFontCache.entries[key]
	convertFontCache.RUnlock()
	if e != nil {
		return e, false
	}
	convertFontCache.Lock()
	defer convertFontCache.Unlock()
	if e := convertFontCache.entries[key]; e != nil {
		return e, false
	}
	e = &convertFontIndex{ready: make(chan struct{})}
	convertFontCache.entries[key] = e
	convertFontCache.order = append(convertFontCache.order, key)
	for len(convertFontCache.order) > maxConvertFontIndexes {
		delete(convertFontCache.entries, convertFontCache.order[0])
		convertFontCache.order = convertFontCache.order[1:]
	}
	return e, true
}

// buildConvertFontIndex indexes dirs into e, the entry of key, and releases
// the calls waiting on it. A failed or abandoned build leaves the cache, so
// the next call builds again.
func buildConvertFontIndex(ctx context.Context, logger *slog.Logger, key string, dirs []string, e *convertFontIndex) (_ *text.Fonts, err error) {
	e.err = errFontIndexAbandoned
	defer func() {
		if e.err != nil {
			dropConvertFontIndex(key, e)
		}
		close(e.ready)
	}()
	fonts, err := text.NewFonts(ctx, logger, dirs)
	if err != nil {
		e.err = err
		return nil, err
	}
	e.fonts, e.err = fonts, nil
	return fonts, nil
}

// dropConvertFontIndex removes e from the cache when it is still the entry of
// key.
func dropConvertFontIndex(key string, e *convertFontIndex) {
	convertFontCache.Lock()
	defer convertFontCache.Unlock()
	if convertFontCache.entries[key] != e {
		return
	}
	delete(convertFontCache.entries, key)
	if i := slices.Index(convertFontCache.order, key); i >= 0 {
		convertFontCache.order = slices.Delete(convertFontCache.order, i, i+1)
	}
}
