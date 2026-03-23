//go:build cgo

package filter

/*
#cgo pkg-config: libopenjp2
#include <openjpeg.h>
#include <stdlib.h>
#include <string.h>

// Silence all OpenJPEG log messages.
static void opj_quiet_callback(const char *msg, void *data) {
	(void)msg;
	(void)data;
}

// In-memory stream state for reading from a Go byte slice.
typedef struct {
	const unsigned char *data;
	OPJ_SIZE_T          size;
	OPJ_SIZE_T          offset;
} mem_stream_t;

static OPJ_SIZE_T mem_read(void *buf, OPJ_SIZE_T nb, void *user) {
	mem_stream_t *s = (mem_stream_t *)user;
	if (s->offset >= s->size) return (OPJ_SIZE_T)-1;
	OPJ_SIZE_T avail = s->size - s->offset;
	if (nb > avail) nb = avail;
	memcpy(buf, s->data + s->offset, nb);
	s->offset += nb;
	return nb;
}

static OPJ_OFF_T mem_skip(OPJ_OFF_T nb, void *user) {
	mem_stream_t *s = (mem_stream_t *)user;
	if (nb < 0) {
		if ((OPJ_SIZE_T)(-nb) > s->offset) {
			s->offset = 0;
		} else {
			s->offset -= (OPJ_SIZE_T)(-nb);
		}
	} else {
		s->offset += (OPJ_SIZE_T)nb;
		if (s->offset > s->size) s->offset = s->size;
	}
	return (OPJ_OFF_T)s->offset;
}

static OPJ_BOOL mem_seek(OPJ_OFF_T pos, void *user) {
	mem_stream_t *s = (mem_stream_t *)user;
	if (pos < 0 || (OPJ_SIZE_T)pos > s->size) return OPJ_FALSE;
	s->offset = (OPJ_SIZE_T)pos;
	return OPJ_TRUE;
}

// decodeJPX decodes JPEG 2000 data and writes interleaved pixel bytes into
// *out_buf (caller must free). Returns 0 on success, -1 on error.
// *out_len receives the byte length, *out_w / *out_h / *out_comps the image
// dimensions.
static int decodeJPX(const unsigned char *src, size_t src_len,
                     unsigned char **out_buf, size_t *out_len,
                     int *out_w, int *out_h, int *out_comps) {

	// Detect codec from the first bytes.
	OPJ_CODEC_FORMAT fmt = OPJ_CODEC_JP2;
	if (src_len >= 4 && src[0] == 0xFF && src[1] == 0x4F &&
	    src[2] == 0xFF && src[3] == 0x51) {
		fmt = OPJ_CODEC_J2K;
	}

	opj_codec_t *codec = opj_create_decompress(fmt);
	if (!codec) return -1;

	opj_set_info_handler(codec, opj_quiet_callback, NULL);
	opj_set_warning_handler(codec, opj_quiet_callback, NULL);
	opj_set_error_handler(codec, opj_quiet_callback, NULL);

	opj_dparameters_t params;
	opj_set_default_decoder_parameters(&params);
	if (!opj_setup_decoder(codec, &params)) {
		opj_destroy_codec(codec);
		return -1;
	}

	// Build in-memory stream.
	mem_stream_t *ms = (mem_stream_t *)malloc(sizeof(mem_stream_t));
	if (!ms) { opj_destroy_codec(codec); return -1; }
	ms->data   = src;
	ms->size   = (OPJ_SIZE_T)src_len;
	ms->offset = 0;

	opj_stream_t *stream = opj_stream_create(1024 * 1024, OPJ_TRUE);
	if (!stream) { free(ms); opj_destroy_codec(codec); return -1; }
	opj_stream_set_user_data(stream, ms, free);
	opj_stream_set_user_data_length(stream, (OPJ_UINT64)src_len);
	opj_stream_set_read_function(stream, mem_read);
	opj_stream_set_skip_function(stream, mem_skip);
	opj_stream_set_seek_function(stream, mem_seek);

	opj_image_t *image = NULL;
	if (!opj_read_header(stream, codec, &image)) {
		opj_stream_destroy(stream);
		opj_destroy_codec(codec);
		return -1;
	}
	if (!opj_decode(codec, stream, image)) {
		opj_image_destroy(image);
		opj_stream_destroy(stream);
		opj_destroy_codec(codec);
		return -1;
	}

	opj_stream_destroy(stream);
	opj_destroy_codec(codec);

	// Interleave components into a flat pixel buffer.
	int w = (int)(image->comps[0].w);
	int h = (int)(image->comps[0].h);
	int nc = (int)image->numcomps;
	size_t total = (size_t)w * (size_t)h * (size_t)nc;

	unsigned char *buf = (unsigned char *)malloc(total);
	if (!buf) { opj_image_destroy(image); return -1; }

	for (int y = 0; y < h; y++) {
		for (int x = 0; x < w; x++) {
			size_t px = ((size_t)y * (size_t)w + (size_t)x);
			for (int c = 0; c < nc; c++) {
				int v = image->comps[c].data[px];
				if (image->comps[c].sgnd) v += (1 << (image->comps[c].prec - 1));
				if (v < 0) v = 0;
				if (v > 255) v = 255;
				buf[px * (size_t)nc + (size_t)c] = (unsigned char)v;
			}
		}
	}

	opj_image_destroy(image);

	*out_buf   = buf;
	*out_len   = total;
	*out_w     = w;
	*out_h     = h;
	*out_comps = nc;
	return 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// decodeJPX decodes JPEG 2000 (JPX) data via libopenjp2 and returns raw
// interleaved pixel bytes.
func decodeJPX(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("jpx decode: empty input")
	}

	var (
		outBuf   *C.uchar
		outLen   C.size_t
		outW     C.int
		outH     C.int
		outComps C.int
	)

	rc := C.decodeJPX(
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.size_t(len(data)),
		&outBuf, &outLen,
		&outW, &outH, &outComps,
	)
	if rc != 0 {
		return nil, fmt.Errorf("jpx decode: libopenjp2 decoding failed")
	}
	defer C.free(unsafe.Pointer(outBuf))

	result := make([]byte, int(outLen))
	copy(result, unsafe.Slice((*byte)(unsafe.Pointer(outBuf)), int(outLen)))
	return result, nil
}
