// Package dct decodes the JPEG data of a DCTDecode stream (ISO 32000-1,
// 7.4.8): baseline and progressive Huffman-coded frames of ITU-T T.81, with
// the colour transforms of Adobe Technical Note #5116 that ISO 32000-1
// defers to.
//
// It tolerates damage that other readers render through, where a strict
// decoder refuses the whole image: scan data that ends early, a component
// identifier repeated in the frame, a height given by a DNL marker.
//
// [OptimizeHuffman] re-codes a sequential JPEG losslessly with optimal
// Huffman tables, and [Encode] writes samples as a new baseline JPEG.
package dct
