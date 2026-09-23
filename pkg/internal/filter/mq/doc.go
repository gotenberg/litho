// Package mq implements the MQ adaptive binary arithmetic decoder defined by
// ITU-T T.88, Annex E. Each call to [Decoder.Decode] returns one binary
// decision and adapts the probability state of the [Context] it was coded in.
//
// ITU-T T.800 (JPEG 2000), Annex C defines the same decoder: its Table C.2
// matches T.88, Table E.1 row for row, and its BYTEIN treats a 0xFF followed
// by a byte above 0x8F as a marker in the same way.
package mq
