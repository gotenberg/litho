// Package mq implements the MQ adaptive binary arithmetic decoder defined by
// ITU-T T.88, Annex E. Each call to [Decoder.Decode] returns one binary
// decision and adapts the probability state of the [Context] it was coded in.
package mq
