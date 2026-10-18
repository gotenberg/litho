// Package saslprep implements SASLprep (RFC 4013), the stringprep (RFC 3454)
// profile ISO 32000-2, 7.6.4.3.3 applies to an AES-256 password before the
// password is encoded as UTF-8 and truncated to 127 bytes.
//
// The tables are generated from RFC 3454 by cmd/gen-saslprep. Normalization
// form KC is golang.org/x/text/unicode/norm's, which follows the current
// Unicode version rather than the Unicode 3.2 that RFC 4013 names (see
// [Query]).
//
// Every function is pure and safe for concurrent use.
package saslprep
