// Package imagefile reads image files: it tells their format from their
// bytes, reads what a file states of its pictures (size, resolution,
// orientation, colour profile, description) and turns each picture into a
// [typeset.ImageSource] that keeps the file's samples: a JPEG or a JPEG
// 2000 file as it is, every other format decoded and written losslessly at
// its own depth, an indexed picture indexed.
//
// The HTML converter reads its pictures' orientation through it, and
// decodes them through [Decode], which guards the decoders it wraps.
//
// Every size, count and offset a file states is bounded before it is used:
// a picture against the conversion's pixel budget
// (models.Limits.MaxImagePixels) before it is decoded, a profile or a text
// against the caps of this package, a file's pictures against
// [MaxPictures].
package imagefile
