package schematron

// SchematronNamespace is the canonical ISO Schematron namespace URI.
const SchematronNamespace = "http://purl.oclc.org/dsdl/schematron"

// Schema is the loaded representation of one sch:schema document plus
// every schema reachable through sch:include during the load. Values
// must be treated as immutable after [Load] returns.
type Schema struct {
	// Title is the value of the optional sch:title element. Empty
	// when the schema omits it.
	Title string
	// QueryBinding records the queryBinding attribute on sch:schema.
	// EN 16931 and XRechnung use "xslt2".
	// TODO: enforce "xslt2" at load; nothing checks the value yet.
	QueryBinding string
	// Namespaces collects every sch:ns child in source order.
	// Evaluation uses these to resolve XPath prefixes.
	Namespaces []NamespaceDecl
	// Patterns indexes top-level sch:pattern declarations by their
	// id. Pattern order is preserved in PatternOrder so callers that
	// run "all patterns" semantics walk them in the same order the
	// schema author wrote them.
	Patterns     map[string]*Pattern
	PatternOrder []string
	// Phases indexes sch:phase declarations by their id. Empty when
	// the schema declares no phases (the validator then runs every
	// pattern).
	Phases map[string]*Phase
	// Lets carries top-level sch:let bindings that apply to every
	// rule in the schema. Rule-scoped lets live on the [Rule] itself.
	Lets []Let
}

// NamespaceDecl binds a prefix to a namespace URI for XPath
// evaluation. Source order is preserved; downstream code that
// duplicates a prefix sees the last binding win.
type NamespaceDecl struct {
	Prefix string
	URI    string
}

// Phase names a set of active patterns. EN 16931 ships a single
// phase that activates every pattern; XRechnung ships multiple so
// callers can validate only the German-specific subset.
type Phase struct {
	ID string
	// Active lists pattern ids the phase enables, in source order.
	Active []string
	// Lets carries phase-scoped sch:let bindings. Evaluation merges
	// these with the schema-level Lets when the phase is selected.
	Lets []Let
}

// Pattern groups related rules. Patterns can be nested inside phases
// for staged validation; abstract patterns (with attribute
// abstract="true") are out of scope per the documented subset.
type Pattern struct {
	ID    string
	Title string
	// Rules walks the pattern's sch:rule children in source order.
	Rules []*Rule
	// Lets carries pattern-scoped sch:let declarations. They
	// evaluate against the document root before any rule in the
	// pattern runs and the resulting literals substitute into the
	// rule contexts and assertion tests.
	Lets []Let
}

// Rule binds a context XPath to a list of assertions. Every assert
// fires when its test evaluates to false on a context match; every
// report fires when its test evaluates to true.
type Rule struct {
	// Context is the raw XPath expression off the sch:rule
	// context attribute, kept as text; [Validate] compiles it via
	// antchfx/xpath.
	Context string
	// Asserts and Reports are kept separate so evaluation can apply
	// the inverted semantics without re-checking element kind.
	Asserts []*Assertion
	Reports []*Assertion
	// Lets carries rule-scoped sch:let bindings. Evaluation
	// resolves these on each context match before running the
	// assertions and reports.
	Lets []Let
}

// Assertion captures one sch:assert or sch:report. The Negate flag
// flips the boolean interpretation so the evaluator can dispatch
// uniformly: an assertion fires when its evaluated value differs
// from the not-firing convention. Concretely:
//
//   - sch:assert fires when test() == false (Negate=false).
//   - sch:report fires when test() == true  (Negate=true).
type Assertion struct {
	ID      string
	Test    string
	Flag    string
	Role    string
	Message string
	Negate  bool
}

// Let is a sch:let binding: a name plus an XPath expression. The
// expression is kept as text; [Validate] substitutes it into the
// expressions that reference it before compilation.
type Let struct {
	Name  string
	Value string
}
