package xsd

import "testing"

func TestQName_String(t *testing.T) {
	tests := []struct {
		name string
		qn   QName
		want string
	}{
		{"namespaced", QName{Namespace: "urn:example", Local: "Foo"}, "{urn:example}Foo"},
		{"no namespace", QName{Local: "Foo"}, "Foo"},
		{"zero value", QName{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.qn.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestQName_IsZero(t *testing.T) {
	if !(QName{}).IsZero() {
		t.Error("zero-value QName.IsZero() = false, want true")
	}
	if (QName{Local: "X"}).IsZero() {
		t.Error("QName{Local: X}.IsZero() = true, want false")
	}
	if (QName{Namespace: "urn:y"}).IsZero() {
		t.Error("QName{Namespace: urn:y}.IsZero() = true, want false")
	}
}

func TestBuiltinTypeRegistry_CoversCommonXSPrimitives(t *testing.T) {
	registry := newBuiltinTypeRegistry()
	// Spot-check the types the CII subset relies on heavily; a full
	// list comparison would couple this test to the builtinNames slice
	// and discourage harmless additions.
	for _, local := range []string{"string", "decimal", "date", "int", "boolean", "anyURI"} {
		qn := QName{Namespace: XSNamespace, Local: local}
		t.Run(local, func(t *testing.T) {
			tt, ok := registry[qn]
			if !ok {
				t.Fatalf("registry missing xs:%s", local)
			}
			bt, ok := tt.(*BuiltinType)
			if !ok {
				t.Fatalf("xs:%s: got %T, want *BuiltinType", local, tt)
			}
			if bt.Name != qn {
				t.Errorf("xs:%s name: got %v, want %v", local, bt.Name, qn)
			}
			if bt.QName() != qn {
				t.Errorf("xs:%s QName(): got %v, want %v", local, bt.QName(), qn)
			}
		})
	}
}

func TestIsBuiltin(t *testing.T) {
	if !IsBuiltin(QName{Namespace: XSNamespace, Local: "string"}) {
		t.Error("IsBuiltin(xs:string) = false, want true")
	}
	if IsBuiltin(QName{Namespace: XSNamespace, Local: "notABuiltin"}) {
		t.Error("IsBuiltin(xs:notABuiltin) = true, want false")
	}
	if IsBuiltin(QName{Namespace: "urn:other", Local: "string"}) {
		t.Error("IsBuiltin({urn:other}string) = true, want false")
	}
}

func TestSimpleType_QName(t *testing.T) {
	st := &SimpleType{Name: QName{Namespace: "urn:t", Local: "Foo"}}
	if st.QName() != st.Name {
		t.Errorf("QName(): got %v, want %v", st.QName(), st.Name)
	}
}

func TestComplexType_QName(t *testing.T) {
	ct := &ComplexType{Name: QName{Namespace: "urn:t", Local: "Bar"}}
	if ct.QName() != ct.Name {
		t.Errorf("QName(): got %v, want %v", ct.QName(), ct.Name)
	}
}
