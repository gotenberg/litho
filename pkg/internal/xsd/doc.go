// Package xsd provides a minimal pure-Go XML Schema (XSD) engine
// scoped to the subset of constructs the UN/CEFACT Cross-Industry
// Invoice D16B schemas use. It is not a general-purpose XSD 1.0
// validator: substitution groups, identity constraints (xs:key /
// xs:keyref / xs:unique), xs:redefine, and complex-type derivation by
// restriction are intentionally out of scope because CII does not
// rely on them.
//
// PR1 of the engine ships only the schema loader and the in-memory
// type model. Validation, facet enforcement, attribute checks, and
// CII bundling arrive in follow-up PRs; see FACTUR-X.md for the
// roadmap.
//
// Concurrency: a [Schema] is immutable once [Load] returns. Concurrent
// readers are safe. Mutating a Schema after Load is undefined.
package xsd
