package mutations

import (
	"testing"
)

// imageObjects returns a one-page document whose page paints an image XObject
// (object 7) whose data is imageData under imageDict, with a soft mask
// (object 8) whose samples spell SECRET too, and a thumbnail (object 9) that
// does as well. pageExtra and catalogExtra are spliced into the page and
// catalog dictionaries.
func imageObjects(imageDict string, imageData []byte, pageExtra, catalogExtra string) map[int]string {
	objs := copiesObjects(catalogExtra)
	objs[3] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
		"/Resources << /XObject << /Im 7 0 R >> >> /Thumb 9 0 R " + pageExtra + " >>"
	objs[4] = streamObject("", []byte("q 60 0 0 10 100 100 cm /Im Do Q"))
	objs[7] = streamObject(imageDict+" /SMask 8 0 R", imageData)
	gray := "/Subtype /Image /Width 6 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8"
	objs[8] = streamObject("/Type /XObject "+gray, []byte("SECRET"))
	objs[9] = streamObject(gray, []byte("SECRET"))
	return objs
}

// rawImageDict is an uncompressed six-sample grey image, the data "SECRET".
const rawImageDict = "/Type /XObject /Subtype /Image /Width 6 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8"

// TestScanOutputForTokens_SkipsImageSamples pins that the data of an image
// XObject is not compared: it is samples (ISO 32000-1, 8.9.5), whatever codec
// stores them, and text extraction never reads it (9.10.2). An image codec's
// data the scan cannot decode is not reported unreadable either, since it is
// not read.
func TestScanOutputForTokens_SkipsImageSamples(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		dict string
		data []byte
	}{
		"raw samples": {dict: rawImageDict, data: []byte("SECRET")},
		"dct":         {dict: rawImageDict + " /Filter /DCTDecode", data: []byte("\xff\xd8\xff\xfe\x00\x08SECRET\xff\xd9")},
		"jpx":         {dict: "/Subtype /Image /Filter /JPXDecode", data: []byte("not a codestream: SECRET")},
		"ccitt":       {dict: rawImageDict + " /Filter /CCITTFaxDecode", data: []byte("SECRET")},
		"jbig2":       {dict: rawImageDict + " /Filter /JBIG2Decode", data: []byte("SECRET")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if res := scanFor(t, copiesDoc(t, imageObjects(tc.dict, tc.data, "", ""), nil, ""), "SECRET"); !res.Clean() {
				t.Errorf("image samples were compared: %+v", res)
			}
		})
	}

	// The control: the same document with the word drawn by the page's
	// content is not clean, so the results above do not come from a scan
	// that reads nothing.
	objs := imageObjects(rawImageDict, []byte("SECRET"), "", "")
	objs[4] = streamObject("", []byte("q 60 0 0 10 100 100 cm /Im Do Q BT /F1 12 Tf (SECRET) Tj ET"))
	if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !hasHit(res, 4, "stream") || len(res.Hits) != 1 {
		t.Errorf("want exactly the content stream's copy, got %+v", res)
	}
}

// TestScanOutputForTokens_ReadsAnImagesStoredTail pins that the bytes stored
// past an image's first filter's end are still compared: they are no samples,
// and a reader of the file's bytes finds them.
func TestScanOutputForTokens_ReadsAnImagesStoredTail(t *testing.T) {
	t.Parallel()
	flate, err := flateCompress([]byte("\x00\x10\x20\x30\x40\x50"))
	if err != nil {
		t.Fatal(err)
	}
	objs := imageObjects(rawImageDict+" /Filter /FlateDecode", append(flate, []byte("SECRET")...), "", "")
	if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !hasHit(res, 7, "stream") {
		t.Errorf("the tail past the image's data was not compared: %+v", res)
	}
}

// TestScanOutputForTokens_ReadsAnImageAlsoReadAsBytes pins that the skip does
// not hide a stream a reader reads as bytes whatever its dictionary says: a
// page's contents, an embedded file, a metadata stream, a JavaScript action
// and an XFA form, and a stream whose /Type contradicts its /Subtype /Image.
// The walk ranges dictionaries, so repeating the scan meets both orders.
func TestScanOutputForTokens_ReadsAnImageAlsoReadAsBytes(t *testing.T) {
	t.Parallel()
	data := []byte("SECRET")
	cases := map[string]map[int]string{
		"page contents": func() map[int]string {
			objs := imageObjects(rawImageDict, data, "", "")
			objs[2] = "<< /Type /Pages /Kids [3 0 R 10 0 R] /Count 2 >>"
			objs[10] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R >>"
			return objs
		}(),
		"embedded file": imageObjects(rawImageDict, data, "",
			"/AF [<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 7 0 R >> >>]"),
		"metadata":   imageObjects(rawImageDict, data, "/Metadata 7 0 R", ""),
		"javascript": imageObjects(rawImageDict, data, "", "/OpenAction << /S /JavaScript /JS 7 0 R >>"),
		"xfa array":  imageObjects(rawImageDict, data, "", "/AcroForm << /Fields [] /XFA [(template) 7 0 R] >>"),
		"xfa stream": imageObjects(rawImageDict, data, "", "/AcroForm << /Fields [] /XFA 7 0 R >>"),
		"contradictory type": imageObjects(
			"/Type /EmbeddedFile /Subtype /Image /Width 6 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", data, "", ""),
	}
	for name, objs := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := copiesDoc(t, objs, nil, "")
			for range 20 {
				if res := scanFor(t, doc, "SECRET"); !hasHit(res, 7, "stream") {
					t.Fatalf("an image also read as bytes was skipped: %+v", res)
				}
			}
		})
	}
}
