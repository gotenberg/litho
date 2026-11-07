// Package woff turns a web font file into the sfnt program it wraps: WOFF
// 1.0 (W3C Recommendation, 13 December 2012), whose tables are each
// compressed with zlib, and WOFF 2.0 (W3C Recommendation, 1 March 2018),
// whose tables share one Brotli stream and whose glyf, loca and hmtx tables
// may be transformed. [Decode] undoes the compression and the transforms
// and returns a TrueType or OpenType program the rest of litho reads like
// any font file.
//
// The metadata and private data blocks of both formats are ignored: they
// carry nothing a renderer reads. A WOFF 2.0 collection yields its first
// font only.
//
// Every length, count and offset in the file is untrusted: the decoders
// check each one against the file and against named caps on the
// uncompressed size, the table count and the points of a glyph before
// allocating, and never panic.
package woff
