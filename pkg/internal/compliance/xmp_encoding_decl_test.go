package compliance

import "testing"

// utf16Bytes encodes s as UTF-16BE so a test can offer genuinely wide bytes.
func utf16Bytes(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for _, r := range s {
		out = append(out, byte(r>>8), byte(r))
	}
	return out
}

// TestXMPDetectEncoding_BytesBeatTheDeclaration covers a packet whose encoding
// declaration contradicts its own bytes. XML opens with "<?xml" or "<?xpacket",
// so a UTF-16 or UTF-32 packet necessarily interleaves NUL bytes through that
// syntax; a prefix carrying none is UTF-8 whatever it declares. Believing the
// declaration decoded a UTF-8 packet as UTF-16, which cost it its encoding
// verdict and its whole PDF/A identification, neither of which veraPDF reports.
func TestXMPDetectEncoding_BytesBeatTheDeclaration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "utf-8 bytes declaring utf-16",
			data: []byte(`<?xml version="1.0" encoding="UTF-16"?>` + "\n" + `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?><x:xmpmeta/>`),
			want: "UTF-8",
		},
		{
			name: "utf-8 bytes declaring utf-32",
			data: []byte(`<?xml version="1.0" encoding="UTF-32"?>` + "\n" + `<?xpacket begin=""?><x:xmpmeta/>`),
			want: "UTF-8",
		},
		{
			name: "genuinely wide bytes are still detected",
			data: utf16Bytes(`<?xml version="1.0" encoding="UTF-16"?><x:xmpmeta/>`),
			want: "UTF-16BE",
		},
		{
			name: "plain utf-8 with no declaration",
			data: []byte(`<?xpacket begin=""?><x:xmpmeta/>`),
			want: "UTF-8",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := xmpDetectEncoding(tc.data); got != tc.want {
				t.Errorf("xmpDetectEncoding = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestXMPCheckPacketHeader_ScopedToXpacket covers which processing instruction
// the ISO 19005-1 6.7.5 attribute rules judge. Their object is the xpacket
// header, so an XML declaration's own encoding= is not theirs to report;
// cutting at the first "?>" read the declaration as the header.
func TestXMPCheckPacketHeader_ScopedToXpacket(t *testing.T) {
	t.Parallel()

	const xpacketPI = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"`

	for _, tc := range []struct {
		name         string
		data         string
		wantBytes    bool
		wantEncoding bool
	}{
		{
			name:         "a declaration's encoding belongs to the declaration",
			data:         `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + xpacketPI + `?><x:xmpmeta/>`,
			wantBytes:    false,
			wantEncoding: false,
		},
		{
			name:         "encoding on the xpacket header still answers",
			data:         `<?xml version="1.0"?>` + "\n" + xpacketPI + ` encoding="UTF-8"?><x:xmpmeta/>`,
			wantBytes:    false,
			wantEncoding: true,
		},
		{
			name:         "bytes on the xpacket header still answers",
			data:         xpacketPI + ` bytes="2048"?><x:xmpmeta/>`,
			wantBytes:    true,
			wantEncoding: false,
		},
		{
			name:         "both on the xpacket header",
			data:         xpacketPI + ` bytes="2048" encoding="UTF-8"?><x:xmpmeta/>`,
			wantBytes:    true,
			wantEncoding: true,
		},
		{
			name:         "a clean header answers to neither",
			data:         xpacketPI + `?><x:xmpmeta/>`,
			wantBytes:    false,
			wantEncoding: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var results []Violation
			xmpCheckPacketHeader([]byte(tc.data), 1, &results)
			gotBytes := hasRule(results, Rule_19005_1_6_7_5_t01)
			gotEncoding := hasRule(results, Rule_19005_1_6_7_5_t02)
			if gotBytes != tc.wantBytes {
				t.Errorf("6.7.5 t1 fired = %v, want %v (%v)", gotBytes, tc.wantBytes, results)
			}
			if gotEncoding != tc.wantEncoding {
				t.Errorf("6.7.5 t2 fired = %v, want %v (%v)", gotEncoding, tc.wantEncoding, results)
			}
		})
	}
}
