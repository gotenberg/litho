// Package zdeflate is a pure-Go port of the C zlib 1.2.12 deflate compressor.
//
// It exists for byte compatibility. The fixture generators in pdfgen must
// reproduce, byte for byte, PDF files that Python wrote with
// zlib.compress. Go's compress/flate produces valid but different streams, so
// this package ports deflate.c and trees.c from zlib 1.2.12 faithfully:
// configuration table, hash chains, lazy matching, the sym_buf block flush
// rule, the stored/fixed/dynamic block choice and the 16-bit bit buffer.
//
// [Compress] also replays CPython 3.9's zlib_compress_impl call pattern: one
// deflate(Z_FINISH) call per output buffer, the buffer starting at 16 KiB and
// doubling when full. The schedule only changes the bytes at level 0, where
// zlib sizes stored blocks by the free output space.
//
// Only the zlib wrapper, windowBits 15, memLevel 8, Z_DEFAULT_STRATEGY and
// inputs below 4 GiB are supported.
package zdeflate
