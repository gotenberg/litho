package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// zipBlock is the size of the ranges a remote archive is read in: the
// central directory and an entry each take a few requests rather than one
// per small read the zip reader makes.
const zipBlock = 1 << 20

// maxZipBlocks bounds the blocks one remote archive keeps in memory.
// Anti-DoS guard, not a property of any archive: a document read
// sequentially needs the block it is in and the next.
const maxZipBlocks = 64

// zipCache opens each remote archive once, its central directory read
// through HTTP range requests, and reads entries out of it. Safe for
// concurrent use.
type zipCache struct {
	client *http.Client
	mu     sync.Mutex
	zips   map[string]*remoteZip
}

// remoteZip is one opened remote archive.
type remoteZip struct {
	once  sync.Once
	r     *zip.Reader
	err   error
	files map[string]*zip.File
}

func newZipCache(client *http.Client) *zipCache {
	return &zipCache{client: client, zips: make(map[string]*remoteZip)}
}

// open returns the remote archive at u, its central directory read once.
func (c *zipCache) open(ctx context.Context, u string) (*remoteZip, error) {
	c.mu.Lock()
	z, ok := c.zips[u]
	if !ok {
		z = &remoteZip{}
		c.zips[u] = z
	}
	c.mu.Unlock()
	z.once.Do(func() {
		ra, size, err := newRangeReaderAt(ctx, c.client, u)
		if err != nil {
			z.err = err
			return
		}
		if z.r, z.err = zip.NewReader(ra, size); z.err != nil {
			z.err = fmt.Errorf("open %s: %w", u, z.err)
			return
		}
		z.files = make(map[string]*zip.File, len(z.r.File))
		for _, f := range z.r.File {
			z.files[f.Name] = f
		}
	})
	return z, z.err
}

// list returns the entries of the archive at u.
func (c *zipCache) list(ctx context.Context, u string) ([]*zip.File, error) {
	z, err := c.open(ctx, u)
	if err != nil {
		return nil, err
	}
	return z.r.File, nil
}

// read returns the bytes of entry in the archive at u.
func (c *zipCache) read(ctx context.Context, u, entry string) ([]byte, error) {
	z, err := c.open(ctx, u)
	if err != nil {
		return nil, err
	}
	f, ok := z.files[entry]
	if !ok {
		return nil, fmt.Errorf("%w: %s holds no entry %s", errPermanent, u, entry)
	}
	if f.UncompressedSize64 > maxFetchBytes {
		return nil, fmt.Errorf("%w: %s in %s is larger than %d bytes", errPermanent, entry, u, maxFetchBytes)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s in %s: %w", entry, u, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxFetchBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s in %s: %w", entry, u, err)
	}
	return data, nil
}

// rangeReaderAt reads a remote file through HTTP range requests, a block at
// a time, keeping the last blocks read. Safe for concurrent use.
type rangeReaderAt struct {
	ctx    context.Context //nolint:containedctx // zip.Reader's ReaderAt takes no context; the fetch's bounds every read
	client *http.Client
	url    string
	size   int64
	mu     sync.Mutex
	blocks map[int64][]byte
	order  []int64
}

// newRangeReaderAt returns a reader of the remote file at u and its size,
// which a HEAD request reads.
func newRangeReaderAt(ctx context.Context, client *http.Client, u string) (*rangeReaderAt, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, http.NoBody)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", errPermanent, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("HEAD %s: %w", u, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("HEAD %s: %s", u, resp.Status)
	}
	size, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil || size <= 0 {
		return nil, 0, fmt.Errorf("HEAD %s: no content length", u)
	}
	if !strings.Contains(resp.Header.Get("Accept-Ranges"), "bytes") {
		return nil, 0, fmt.Errorf("%w: %s does not serve ranges", errPermanent, u)
	}
	return &rangeReaderAt{ctx: ctx, client: client, url: u, size: size, blocks: make(map[int64][]byte)}, size, nil
}

// ReadAt implements io.ReaderAt.
func (r *rangeReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	n := 0
	for n < len(p) {
		at := off + int64(n)
		if at >= r.size {
			return n, io.EOF
		}
		block, err := r.block(at / zipBlock)
		if err != nil {
			return n, err
		}
		n += copy(p[n:], block[at%zipBlock:])
	}
	return n, nil
}

// block returns block i, from the cache or a range request.
func (r *rangeReaderAt) block(i int64) ([]byte, error) {
	r.mu.Lock()
	b, ok := r.blocks[i]
	r.mu.Unlock()
	if ok {
		return b, nil
	}
	start := i * zipBlock
	n := min(int64(zipBlock), r.size-start)
	var err error
	for range fetchRetries {
		if b, err = httpRange(r.ctx, r.client, r.url, start, n); err == nil || errors.Is(err, errPermanent) || r.ctx.Err() != nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	if int64(len(b)) != n {
		return nil, fmt.Errorf("%s: range %d-%d answered %d bytes", r.url, start, start+n-1, len(b))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.blocks[i]; !ok {
		r.blocks[i] = b
		r.order = append(r.order, i)
		if len(r.order) > maxZipBlocks {
			delete(r.blocks, r.order[0])
			r.order = r.order[1:]
		}
	}
	return b, nil
}
