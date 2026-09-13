package compliance

import (
	"testing"
)

// TestXMPStructTypeFields2005 pins the ResourceRef and ResourceEvent members to
// the 2005 XMP Specification, the edition ISO 19005-2 normatively references.
//
// litho already judges property names against that edition: ISO 19005-2,
// 6.6.2.3.1 t1 reports a property "not predefined in XMP 2005". The structured
// field sets had been left comprehensive, so a file writing a member Adobe
// added later passed t2. pdfjs issue15220.pdf writes stRef:originalDocumentID
// inside xmpMM:DerivedFrom and stEvt:changed inside xmpMM:History, and veraPDF
// reports both under 6.6.2.3.1 t2.
//
// Every name below was confirmed against veraPDF one field per fixture: the
// accepted ones pass its t2 and the rejected ones fail it.
func TestXMPStructTypeFields2005(t *testing.T) {
	t.Parallel()

	const (
		resourceEvent = "http://ns.adobe.com/xap/1.0/sType/ResourceEvent#"
		resourceRef   = "http://ns.adobe.com/xap/1.0/sType/ResourceRef#"
	)

	cases := []struct {
		ns     string
		member string
		want   bool
	}{
		{resourceEvent, "action", true},
		{resourceEvent, "instanceID", true},
		{resourceEvent, "parameters", true},
		{resourceEvent, "softwareAgent", true},
		{resourceEvent, "when", true},
		{resourceEvent, "changed", false},

		{resourceRef, "instanceID", true},
		{resourceRef, "documentID", true},
		{resourceRef, "versionID", true},
		{resourceRef, "renditionClass", true},
		{resourceRef, "renditionParams", true},
		{resourceRef, "manager", true},
		{resourceRef, "managerVariant", true},
		{resourceRef, "manageTo", true},
		{resourceRef, "manageUI", true},
		{resourceRef, "originalDocumentID", false},
		{resourceRef, "filePath", false},
		{resourceRef, "fromPart", false},
		{resourceRef, "toPart", false},
		{resourceRef, "lastModifyDate", false},
		{resourceRef, "maskMarkers", false},
		{resourceRef, "partMapping", false},
		{resourceRef, "alternatePaths", false},
		{resourceRef, "placedXResolution", false},
		{resourceRef, "placedYResolution", false},
		{resourceRef, "placedResolutionUnit", false},
	}

	for _, tc := range cases {
		name := tc.ns[len("http://ns.adobe.com/xap/1.0/sType/"):] + tc.member
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := xmpStructTypeFields[tc.ns][tc.member]; got != tc.want {
				t.Errorf("member %q of %s = %v, want %v", tc.member, tc.ns, got, tc.want)
			}
		})
	}
}
