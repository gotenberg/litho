//go:build cgo

package filter

/*
#cgo pkg-config: jbig2dec
#include <jbig2.h>
#include <stdlib.h>
#include <string.h>

// decodeJBIG2C decodes a JBIG2 stream, optionally preceded by global segment
// data. The decoded 1-bpp bitmap is returned as packed bytes (MSB-first),
// one bit per pixel, with rows padded to byte boundaries.
// Returns 0 on success, -1 on error.
static int decodeJBIG2C(const unsigned char *globals, size_t globals_len,
                        const unsigned char *data,    size_t data_len,
                        unsigned char **out_buf, size_t *out_len,
                        int *out_w, int *out_h) {

	Jbig2Ctx *ctx = jbig2_ctx_new(NULL, JBIG2_OPTIONS_EMBEDDED, NULL, NULL, NULL);
	if (!ctx) return -1;

	// Feed global segments first if present.
	if (globals && globals_len > 0) {
		Jbig2GlobalCtx *gctx = NULL;
		if (jbig2_data_in(ctx, globals, globals_len) < 0) {
			jbig2_ctx_free(ctx);
			return -1;
		}
		gctx = jbig2_make_global_ctx(ctx);
		if (!gctx) {
			jbig2_ctx_free(ctx);
			return -1;
		}
		// Create a new context with the global segments.
		ctx = jbig2_ctx_new(NULL, JBIG2_OPTIONS_EMBEDDED, gctx, NULL, NULL);
		if (!ctx) {
			jbig2_global_ctx_free(gctx);
			return -1;
		}
		// gctx is owned by ctx now — freed when ctx is freed.
	}

	if (jbig2_data_in(ctx, data, data_len) < 0) {
		jbig2_ctx_free(ctx);
		return -1;
	}
	if (jbig2_complete_page(ctx) < 0) {
		jbig2_ctx_free(ctx);
		return -1;
	}

	Jbig2Image *page = jbig2_page_out(ctx);
	if (!page) {
		jbig2_ctx_free(ctx);
		return -1;
	}

	int w = page->width;
	int h = page->height;
	int stride = page->stride;
	size_t total = (size_t)stride * (size_t)h;

	unsigned char *buf = (unsigned char *)malloc(total);
	if (!buf) {
		jbig2_release_page(ctx, page);
		jbig2_ctx_free(ctx);
		return -1;
	}
	memcpy(buf, page->data, total);

	jbig2_release_page(ctx, page);
	jbig2_ctx_free(ctx);

	*out_buf = buf;
	*out_len = total;
	*out_w   = w;
	*out_h   = h;
	return 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// decodeJBIG2 decodes JBIG2 data via libjbig2dec. The DecodeParms dict may
// contain a /JBIG2Globals stream that is fed to the decoder before the
// page-specific data.
func decodeJBIG2(data []byte, parms ast.PdfDict) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("jbig2 decode: empty input")
	}

	// Extract /JBIG2Globals from DecodeParms if present.
	var globals []byte
	if parms != nil {
		if globalsObj, ok := parms["JBIG2Globals"]; ok {
			if globalsStream, ok := globalsObj.(ast.PdfStream); ok {
				decoded, err := DecodeStream(globalsStream)
				if err != nil {
					return nil, fmt.Errorf("decoding JBIG2Globals: %w", err)
				}
				globals = decoded
			}
		}
	}

	var (
		globalsPtr *C.uchar
		globalsLen C.size_t
	)
	if len(globals) > 0 {
		globalsPtr = (*C.uchar)(unsafe.Pointer(&globals[0]))
		globalsLen = C.size_t(len(globals))
	}

	var (
		outBuf *C.uchar
		outLen C.size_t
		outW   C.int
		outH   C.int
	)

	rc := C.decodeJBIG2C(
		globalsPtr, globalsLen,
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.size_t(len(data)),
		&outBuf, &outLen,
		&outW, &outH,
	)
	if rc != 0 {
		return nil, fmt.Errorf("jbig2 decode: libjbig2dec decoding failed")
	}
	defer C.free(unsafe.Pointer(outBuf))

	result := make([]byte, int(outLen))
	copy(result, unsafe.Slice((*byte)(unsafe.Pointer(outBuf)), int(outLen)))
	return result, nil
}
