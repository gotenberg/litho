package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// misrootedOCPDF assembles a one-page document whose catalog is object 2 and
// declares one optional-content group turned off, while the trailer /Root names
// rootObjNum. Pointing /Root at the /Info dictionary in object 1 reproduces a
// damaged incremental update; pointing it at object 2 is the control.
func misrootedOCPDF(rootObjNum int) []byte {
	stream := "/OC /MC0 BDC\nBT ET\nEMC\n"
	objs := []string{
		"<< /Producer (Aspose) >>",
		"<< /Type /Catalog /Pages 3 0 R /OCProperties << /OCGs [5 0 R] /D << /OFF [5 0 R] >> >> >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 3 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Properties << /MC0 5 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /OCG /Name (Layer1) >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %d 0 R /Info 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, rootObjNum, start)
	return buf.Bytes()
}

// TestOCConfigFollowsTheRecoveredCatalog pins that the optional-content
// configuration is read from the catalog rather than from whatever object a
// damaged trailer /Root names.
//
// Per ISO 32000-1 §7.5.5 Table 15 /Root shall be the catalog, so a file naming
// something else is malformed and [Document.CatalogRef] recovers from it.
// Reading the raw /Root left On, Off and BaseState empty, so every group fell
// back to visible: a layer the document turns off was reported as on to every
// consumer that honours visibility.
func TestOCConfigFollowsTheRecoveredCatalog(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		rootObj int
	}{
		{"control: /Root names the catalog", 2},
		{"/Root names the /Info dictionary", 1},
	} {
		data := misrootedOCPDF(tc.rootObj)
		doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%s: NewDocument: %v", tc.name, err)
		}
		if doc.OCConfig == nil {
			t.Fatalf("%s: OCConfig is nil", tc.name)
		}
		if !doc.OCConfig.Off[5] {
			t.Errorf("%s: OCG 5 not recorded as off; Off=%v BaseState=%q",
				tc.name, doc.OCConfig.Off, doc.OCConfig.BaseState)
		}
		if doc.OCConfig.BaseState != "ON" {
			t.Errorf("%s: BaseState = %q, want \"ON\"", tc.name, doc.OCConfig.BaseState)
		}
	}
}
