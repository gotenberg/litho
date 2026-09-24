// Package xsd provides a minimal pure-Go XML Schema (XSD) engine
// scoped to the subset of constructs the UN/CEFACT Cross-Industry
// Invoice D16B schemas use. It is not a general-purpose XSD 1.0
// validator: substitution groups, identity constraints (xs:key /
// xs:keyref / xs:unique), xs:redefine, and complex-type derivation by
// restriction are intentionally out of scope because CII does not
// rely on them.
//
// [Load] builds the in-memory type model from a schema tree,
// [Validate] checks an instance document against a [Schema], and
// [ValidateValue] enforces one simple type's lexical space and facets.
// The compliance package bundles the CII schemas and runs them on
// Factur-X payloads.
//
// Concurrency: a [Schema] is immutable once [Load] returns. Concurrent
// readers are safe. Mutating a Schema after Load is undefined.
package xsd
