// Package jbig2 decodes JBIG2 bi-level image data (ITU-T T.88 | ISO/IEC 14492)
// in the embedded organisation that ISO 32000-1, 7.4.7 requires of a
// JBIG2Decode stream: the segments of one page, optionally preceded by global
// segments held in a separate stream.
//
// Decoded bitmaps keep the T.88 values: a 1 pixel is foreground, which T.88,
// 5.6 NOTE 1 gives as the typical reading of black.
package jbig2
