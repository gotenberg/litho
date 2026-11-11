package xmp

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestEscape(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`a & b < c > d`: "a &amp; b &lt; c &gt; d",
		`"it's"`:        "&quot;it&apos;s&quot;",
		"one\r\ntwo":    "one&#xD;\ntwo",
		"bell\x07tab\t": "belltab\t",
		"del\x7f":       "del",
	} {
		if got := Escape(in); got != want {
			t.Errorf("Escape(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPacketIsWellFormed parses the packet back and finds each value once,
// escaped markup included.
func TestPacketIsWellFormed(t *testing.T) {
	t.Parallel()
	info := Info{Title: "A <b> & \"c\"", Author: "Ann", Subject: "S", Keywords: "k1, k2", Creator: "conv", Producer: "litho", Lang: "fr-FR"}
	p := Packet(info)
	if !bytes.HasPrefix(p, []byte("<?xpacket begin=")) || !bytes.HasSuffix(p, []byte(`<?xpacket end="r"?>`)) {
		t.Errorf("packet wrapper missing: %q", p)
	}
	dec := xml.NewDecoder(bytes.NewReader(p))
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("packet is not well formed: %v", err)
		}
		if cd, ok := tok.(xml.CharData); ok {
			text.Write(cd)
		}
	}
	for _, v := range []string{info.Title, info.Author, info.Subject, info.Keywords, info.Creator, info.Producer, info.Lang} {
		if strings.Count(text.String(), v) != 1 {
			t.Errorf("value %q found %d times", v, strings.Count(text.String(), v))
		}
	}
	if bytes.Contains(Packet(Info{}), []byte("dc:title")) {
		t.Error("an empty title writes dc:title")
	}
}
