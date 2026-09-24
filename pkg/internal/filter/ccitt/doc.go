// Package ccitt decodes the CCITT facsimile data of a CCITTFaxDecode stream
// (ISO 32000-1, 7.4.6): Group 3 one-dimensional and mixed coding (ITU-T T.4)
// and Group 4 two-dimensional coding (ITU-T T.6), under the parameters of
// ISO 32000-1, Table 11.
//
// Decoded rows hold one bit per pixel, 0 for black unless BlackIs1 says
// otherwise, each row padded to a whole byte.
package ccitt
