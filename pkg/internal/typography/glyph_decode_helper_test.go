package typography

// decodeNextChar is [decodeNextCode] without the stand-in report, the reading
// the decoding tests assert.
func decodeNextChar(font *Font, data []byte) (char string, n int, skip bool) {
	char, n, skip, _ = decodeNextCode(font, data)
	return char, n, skip
}
