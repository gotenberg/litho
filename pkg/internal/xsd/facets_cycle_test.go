package xsd

import (
	"context"
	"testing"
	"testing/fstest"
)

// A simpleType whose restriction base names itself. The loader resolves the
// base through Schema.Types and never checks that the chain terminates, so
// nothing rejects this at load time.
const selfRestrictingXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" elementFormDefault="qualified">
  <xs:simpleType name="Loop">
    <xs:restriction base="Loop"><xs:maxLength value="5"/></xs:restriction>
  </xs:simpleType>
  <xs:element name="Root" type="Loop"/>
</xs:schema>`

// Two simpleTypes restricting each other. A self-reference might plausibly be
// caught by an identity check; a two-step cycle would not, so both shapes are
// pinned.
const mutuallyRestrictingXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" elementFormDefault="qualified">
  <xs:simpleType name="A"><xs:restriction base="B"><xs:maxLength value="5"/></xs:restriction></xs:simpleType>
  <xs:simpleType name="B"><xs:restriction base="A"><xs:maxLength value="7"/></xs:restriction></xs:simpleType>
  <xs:element name="Root" type="A"/>
</xs:schema>`

// TestValidate_CyclicSimpleTypeBaseTerminates pins that a base chain returning
// to itself cannot exhaust the goroutine stack.
//
// Measured before the bound landed: both schemas LOAD without error, and
// Validate then died with "fatal error: stack overflow" after the runtime grew
// the stack past its 1 GB limit. That is not a panic the pkg/pdf recover
// boundary can convert into an error, it kills the process, so the guard has to
// live in the walk itself.
//
// A crashing process cannot be asserted on from inside the same test binary,
// which is why this test states the POST-fix contract: Validate returns.
func TestValidate_CyclicSimpleTypeBaseTerminates(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		schema string
	}{
		{"self restriction", selfRestrictingXSD},
		{"mutual restriction", mutuallyRestrictingXSD},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fsys := fstest.MapFS{"s.xsd": &fstest.MapFile{Data: []byte(tc.schema)}}
			schema, err := Load(context.Background(), fsys, "s.xsd")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			// The value is longer than the maxLength the cycle declares, so
			// the facet walk runs to completion rather than short-circuiting.
			if _, err := Validate(context.Background(), schema, []byte(`<Root xmlns="urn:t">far too long to pass</Root>`)); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

// TestCollectAllFacets_CyclicChainStillCollects pins the truncation contract
// directly on the walk: it returns the facets it gathered rather than looping,
// and reports no built-in because a cyclic chain never bottoms out on one.
func TestCollectAllFacets_CyclicChainStillCollects(t *testing.T) {
	t.Parallel()

	a := &SimpleType{Name: QName{Namespace: "urn:t", Local: "A"}, Facets: []Facet{MaxLength{Value: 5}}}
	b := &SimpleType{Name: QName{Namespace: "urn:t", Local: "B"}, Facets: []Facet{MaxLength{Value: 7}}}
	a.Base = b
	b.Base = a

	steps, base, _ := walkDerivation(a)
	if base != nil {
		t.Errorf("base = %v, want nil: a cyclic chain reaches no built-in", base)
	}
	if len(steps) == 0 {
		t.Error("steps are empty: the walk should return what it gathered before the bound")
	}
	if len(steps) > maxTypeDerivationDepth {
		t.Errorf("steps = %d, want at most %d: the walk is not bounded", len(steps), maxTypeDerivationDepth)
	}
}

// TestCollectAllFacets_AcyclicOrderUnchanged is the control. The bound must not
// alter the base-first ordering the facet semantics depend on, and a test that
// only proves termination would pass even if the rewrite reversed the chain.
func TestCollectAllFacets_AcyclicOrderUnchanged(t *testing.T) {
	t.Parallel()

	builtin := &BuiltinType{Name: QName{Namespace: XSNamespace, Local: "string"}}
	upstream := &SimpleType{Name: QName{Local: "Upstream"}, Base: builtin, Facets: []Facet{MaxLength{Value: 9}}}
	derived := &SimpleType{Name: QName{Local: "Derived"}, Base: upstream, Facets: []Facet{MaxLength{Value: 3}}}

	steps, base, _ := walkDerivation(derived)
	if base != builtin {
		t.Fatalf("base = %v, want the built-in the chain bottoms out on", base)
	}
	if len(steps) != 2 || len(steps[0]) != 1 || len(steps[1]) != 1 {
		t.Fatalf("steps = %#v, want two steps of one facet each", steps)
	}
	first, ok := steps[0][0].(MaxLength)
	if !ok || first.Value != 9 {
		t.Errorf("steps[0] = %#v, want the UPSTREAM MaxLength(9): base-first order was lost", steps[0])
	}
	last, ok := steps[1][0].(MaxLength)
	if !ok || last.Value != 3 {
		t.Errorf("steps[1] = %#v, want the DERIVED MaxLength(3)", steps[1])
	}
}
