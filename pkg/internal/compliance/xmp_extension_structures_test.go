package compliance

import "testing"

// TestXMPExtensionPredefinedStructures pins the value of an extension property
// declared with a structure type the part predefines, each case measured on
// veraPDF 1.30.2 with a crafted file at PDF/A-1b and PDF/A-2b.
//
// veraPDF registers each predefined structure with its namespace and field
// types (ValidatorsContainerCreator, from XMPConstants) and validates a value
// against it field by field (StructuredTypeValidator): a structure whose every
// field is one of the type's, in the type's namespace, and matches its field
// type. A closed choice is read as its base type, since veraPDF validates
// PDF/A with isClosedChoiceCheck false. Font, Colorant, Marker and Time are
// predefined at parts 2 and 3 only.
func TestXMPExtensionPredefinedStructures(t *testing.T) {
	t.Parallel()

	one := func(name, typ, value string, want1b, want2b string) extScopeCase {
		return extScopeCase{
			name:    name,
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", typ}}),
			props:   `<ex:pa rdf:parseType="Resource">` + value + `</ex:pa>`,
			want1b:  want1b,
			want2b:  want2b,
		}
	}
	const (
		t3  = "6.7.9-t03:1"
		t2  = "6.6.2.3.1-t02:1"
		p1  = "6.7.8-t09:1,6.7.9-t02:1,6.7.9-t03:1"
		dim = `<stDim:w>1.5</stDim:w><stDim:h>2</stDim:h><stDim:unit>inch</stDim:unit>`
	)
	runExtScopeCases(t, []extScopeCase{
		one("Dimensions with its fields", "Dimensions", dim, "", ""),
		one("Dimensions with a word for a Real", "Dimensions", `<stDim:w>abc</stDim:w>`, t3, t2),
		one("Dimensions with a field it lacks", "Dimensions", `<stDim:depth>1</stDim:depth>`, t3, t2),
		one("Dimensions with a field of another namespace", "Dimensions", `<stFnt:w>1</stFnt:w>`, t3, t2),
		one("Dimensions with no field", "Dimensions", "", "", ""),
		one("Font with its fields", "Font", `<stFnt:fontName>A</stFnt:fontName><stFnt:composite>True</stFnt:composite>`+
			`<stFnt:childFontFiles><rdf:Seq><rdf:li>a</rdf:li></rdf:Seq></stFnt:childFontFiles>`, p1, ""),
		one("Font with a word for a Boolean", "Font", `<stFnt:composite>yes</stFnt:composite>`, p1, t2),
		one("Font with a bag for a seq", "Font", `<stFnt:childFontFiles><rdf:Bag><rdf:li>a</rdf:li></rdf:Bag></stFnt:childFontFiles>`, p1, t2),
		one("ResourceRef with a later member", "ResourceRef", `<stRef:filePath>a</stRef:filePath>`, t3, t2),
		one("ResourceRef with its member", "ResourceRef", `<stRef:documentID>uuid:1</stRef:documentID>`, "", ""),
		one("ResourceEvent with a word for a Date", "ResourceEvent", `<stEvt:when>notadate</stEvt:when>`, t3, t2),
		one("Thumbnail with a closed-choice value outside it", "Thumbnail", `<xmpGImg:format>PNG</xmpGImg:format>`, "", ""),
		one("Thumbnail with a word for an Integer", "Thumbnail", `<xmpGImg:width>x</xmpGImg:width>`, t3, t2),
		one("Colorant with a closed-choice value outside it", "Colorant", `<xmpG:mode>XYZ</xmpG:mode>`, p1, ""),
		one("Colorant with a word for a Real", "Colorant", `<xmpG:cyan>abc</xmpG:cyan>`, p1, t2),
		one("Version whose event holds a word for a Date", "Version",
			`<stVer:event rdf:parseType="Resource"><stEvt:when>bad</stEvt:when></stVer:event>`, t3, t2),
		one("Marker with a structure for an any field", "Marker",
			`<xmpDM:startTime rdf:parseType="Resource"><xmpDM:foo>1</xmpDM:foo></xmpDM:startTime>`, p1, ""),
		one("Time with a word for an Integer", "Time", `<xmpDM:value>abc</xmpDM:value>`, p1, t2),
	})
}

// TestXMPPredefinedStructFieldsAgree pins that the predefined structure types
// an extension value is judged against hold the members [xmpStructTypeFields]
// audits a predefined property's structure against, for every namespace both
// cover: the two tables mirror the same veraPDF 1.30.2 structures.
func TestXMPPredefinedStructFieldsAgree(t *testing.T) {
	t.Parallel()

	for ns, members := range xmpStructTypeFields {
		var fields map[string]string
		for _, table := range []map[string]xmpExtType{xmpPredefinedStructTypes1, xmpPredefinedStructTypes23} {
			for _, st := range table {
				if st.NS == ns {
					fields = st.fields
				}
			}
		}
		if fields == nil {
			t.Errorf("%s: no predefined structure type holds the namespace", ns)
			continue
		}
		if len(fields) != len(members) {
			t.Errorf("%s: %d fields, %d members", ns, len(fields), len(members))
		}
		for name := range members {
			if _, ok := fields[name]; !ok {
				t.Errorf("%s: member %s is no field of the predefined type", ns, name)
			}
		}
	}
}
