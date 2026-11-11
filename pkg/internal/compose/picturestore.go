package compose

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// DefaultPictureHold is the compressed picture bytes a [PictureStore]
// keeps in memory when its hold is zero: 8 MiB. Memory guard, not
// spec-mandated: a document with a few photographs, a 12-megapixel JPEG
// being 3 to 6 MiB, makes no temp file, and past it a document's pictures
// cost memory one at a time, as they are encoded or written.
const DefaultPictureHold = 8 << 20

// PictureStore holds the compressed bytes of one conversion's pictures,
// the JPEG files and Flate samples of every [ImageSource] [EncodeImage]
// and [JPEGImage] make under a context that carries it: the first bytes
// up to its hold in memory, the rest in a temp file read back when the
// picture is decoded or written. A picture's bytes are the same wherever
// they are kept, so the output is too.
//
// A PictureStore is safe for concurrent use. [PictureStore.Close] removes
// its file: the sources it holds cannot be read after it.
type PictureStore struct {
	spool *writer.Spool
	mu    sync.Mutex
	hold  int64
	held  int64
}

// pictureStoreKey addresses the PictureStore [WithPictureStore] installs
// on a context.
type pictureStoreKey struct{}

// WithPictureStore returns a context carrying a new PictureStore whose file
// goes in dir, the operating system's temporary directory when dir is
// empty, and which keeps hold bytes in memory: [DefaultPictureHold] when
// hold is zero, every byte when hold is negative. The caller closes the
// store once the document it built is written, on every path.
func WithPictureStore(ctx context.Context, dir string, hold int64) (context.Context, *PictureStore) {
	if hold == 0 {
		hold = DefaultPictureHold
	}
	s := &PictureStore{spool: writer.NewSpool(dir), hold: hold}
	return context.WithValue(ctx, pictureStoreKey{}, s), s
}

// pictureStoreFrom returns the PictureStore ctx carries, nil for none.
func pictureStoreFrom(ctx context.Context) *PictureStore {
	s, _ := ctx.Value(pictureStoreKey{}).(*PictureStore) //nolint:errcheck // a type assertion, nil when absent
	return s
}

// Close removes the store's file, if it made one.
func (s *PictureStore) Close() error {
	if s == nil {
		return nil
	}
	if err := s.spool.Close(); err != nil {
		return fmt.Errorf("closing the picture store: %w", err)
	}
	return nil
}

// keep decides where src's bytes live: in memory, charged to ctx's budget
// as the document holds them until it is written, while the store's hold
// allows, else in the store's file. Without a store they stay in memory.
func keep(ctx context.Context, src *ImageSource) error {
	n := int64(len(src.JPEG)) + int64(len(src.Samples)) + int64(len(src.Alpha))
	s := pictureStoreFrom(ctx)
	if s == nil || !s.spills(n) {
		if err := limits.BudgetFrom(ctx).ChargeUnits(n, 1, "encoded pictures"); err != nil {
			return fmt.Errorf("keeping the picture: %w", err)
		}
		return nil
	}
	st := &storedPicture{}
	for _, b := range []struct {
		data []byte
		dst  *writer.Blob
	}{{src.JPEG, &st.jpeg}, {src.Samples, &st.samples}, {src.Alpha, &st.alpha}} {
		if len(b.data) == 0 {
			continue
		}
		blob, err := s.spool.Put(b.data)
		if err != nil {
			return fmt.Errorf("keeping the picture: %w", err)
		}
		*b.dst = blob
	}
	src.stored = st
	src.JPEG, src.Samples, src.Alpha = nil, nil, nil
	return nil
}

// spills reports whether n more bytes go to the file: once the bytes held
// in memory would pass the hold, every later picture goes there.
func (s *PictureStore) spills(n int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hold < 0 || s.held+n <= s.hold {
		s.held += n
		return false
	}
	s.held = s.hold
	return true
}

// storedPicture is where the bytes of an [ImageSource] kept in a
// [PictureStore]'s file are: its JPEG file, or its samples and alpha.
type storedPicture struct {
	jpeg, samples, alpha writer.Blob
}

// IsJPEG reports whether s is a JPEG file written as it is, wherever its
// bytes are kept.
func (s *ImageSource) IsJPEG() bool {
	return len(s.JPEG) > 0 || s.stored != nil && s.stored.jpeg.Len() > 0
}

// hasSamples reports whether s holds compressed samples.
func (s *ImageSource) hasSamples() bool {
	return len(s.Samples) > 0 || s.stored != nil && s.stored.samples.Len() > 0
}

// hasAlpha reports whether s holds a compressed alpha.
func (s *ImageSource) hasAlpha() bool {
	return s.Alpha != nil || s.stored != nil && s.stored.alpha.Len() > 0
}

// jpegReader, samplesReader and alphaReader read s's bytes from memory or
// from the store's file.
func (s *ImageSource) jpegReader() io.Reader {
	if s.stored != nil {
		return s.stored.jpeg.Reader()
	}
	return bytes.NewReader(s.JPEG)
}

func (s *ImageSource) samplesReader() io.Reader {
	if s.stored != nil {
		return s.stored.samples.Reader()
	}
	return bytes.NewReader(s.Samples)
}

func (s *ImageSource) alphaReader() io.Reader {
	if s.stored != nil {
		return s.stored.alpha.Reader()
	}
	return bytes.NewReader(s.Alpha)
}

// loader returns what a stream of the bytes data or blob reads: data from
// memory when s keeps it there, else the blob from the store's file.
func (s *ImageSource) loader(data []byte, blob func(*storedPicture) writer.Blob) func(context.Context) ([]byte, error) {
	if s.stored == nil {
		return func(context.Context) ([]byte, error) { return data, nil }
	}
	return blob(s.stored).Bytes
}
