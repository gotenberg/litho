package text

import (
	"fmt"
	"log/slog"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
)

// loadFont returns the parsed font of fc, from the cache or parsed from its
// file. A face that failed to load fails again without reading its file, so
// every call agrees. The file is read without holding f.mu.
func (f *Fonts) loadFont(fc *Face) (*font.Font, error) {
	f.mu.Lock()
	if fc.loadErr != nil {
		err := fc.loadErr
		f.mu.Unlock()
		return nil, err
	}
	if fc.font != nil {
		f.touchLocked(fc)
		ft := fc.font
		f.mu.Unlock()
		return ft, nil
	}
	f.mu.Unlock()

	ft, err := fc.parseFont()

	f.mu.Lock()
	defer f.mu.Unlock()
	if fc.font != nil {
		// Another call loaded it meanwhile; both parsed the same bytes.
		f.touchLocked(fc)
		return fc.font, nil
	}
	if err != nil {
		if fc.loadErr == nil {
			fc.loadErr = fmt.Errorf("loading face %s: %w", fc.Key(), err)
			f.logger.Debug("font files: a face failed to load", slog.String("face", fc.Key()), slog.Any("error", err))
		}
		return nil, fc.loadErr
	}
	fc.font = ft
	f.pushFrontLocked(fc)
	f.loaded++
	f.loadedBytes += fc.size
	for f.lruTail != fc && (f.loaded > f.maxLoaded || f.loadedBytes > f.maxLoadedBytes) {
		f.evictLocked(f.lruTail)
	}
	return ft, nil
}

// touchLocked moves fc to the front of the cache. f.mu is held.
func (f *Fonts) touchLocked(fc *Face) {
	if f.lruHead == fc {
		return
	}
	f.unlinkLocked(fc)
	f.pushFrontLocked(fc)
}

// pushFrontLocked links fc at the front of the cache. f.mu is held.
func (f *Fonts) pushFrontLocked(fc *Face) {
	fc.prev, fc.next = nil, f.lruHead
	if f.lruHead != nil {
		f.lruHead.prev = fc
	}
	f.lruHead = fc
	if f.lruTail == nil {
		f.lruTail = fc
	}
}

// unlinkLocked removes fc from the cache list. f.mu is held.
func (f *Fonts) unlinkLocked(fc *Face) {
	if fc.prev != nil {
		fc.prev.next = fc.next
	} else {
		f.lruHead = fc.next
	}
	if fc.next != nil {
		fc.next.prev = fc.prev
	} else {
		f.lruTail = fc.prev
	}
	fc.prev, fc.next = nil, nil
}

// evictLocked drops the parsed font of fc. A layout that still holds it
// keeps using it. f.mu is held.
func (f *Fonts) evictLocked(fc *Face) {
	f.unlinkLocked(fc)
	fc.font = nil
	f.loaded--
	f.loadedBytes -= fc.size
}

// parseFont parses the face from its file with go-text, after checking its
// table directory and the tables go-text would panic on again, since the
// file may have changed since it was indexed.
func (fc *Face) parseFont() (*font.Font, error) {
	file, size, err := fc.src.open(fc.src.path)
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck // read-only file: Close reports nothing a read did not
	if size > maxFontCollectionBytes || (!fc.src.collection && size > maxFontFileBytes) {
		return nil, fmt.Errorf("file of %d bytes past the size bound", size)
	}
	records, err := readTableDirectory(file, size, fc.dir)
	if err != nil {
		return nil, err
	}
	if _, err := checkProgram(records); err != nil {
		return nil, err
	}
	loaders, err := ot.NewLoaders(file)
	if err != nil {
		return nil, err
	}
	if fc.index >= len(loaders) {
		return nil, fmt.Errorf("face %d of a file of %d faces", fc.index, len(loaders))
	}
	ld := loaders[fc.index]
	tables := make(map[string][]byte, 5)
	for _, t := range []struct {
		tag   string
		limit uint32
	}{{"cmap", maxCmapTableBytes}, {"maxp", maxSmallTableBytes}, {"loca", maxLocaTableBytes}, {"hhea", maxSmallTableBytes}, {"vhea", maxSmallTableBytes}} {
		if tables[t.tag], err = readTable(ld, records, t.tag, t.limit); err != nil {
			return nil, err
		}
	}
	head, err := readHead(ld, records)
	if err != nil {
		return nil, err
	}
	glyf, _ := findRecord(records, "glyf")
	if err := checkGoTextSafe(goTextTables{
		cmap: tables["cmap"], head: head, maxp: tables["maxp"], loca: tables["loca"],
		hhea: tables["hhea"], vhea: tables["vhea"], glyfLen: int64(glyf.Length),
	}); err != nil {
		return nil, err
	}
	return font.NewFont(ld)
}
