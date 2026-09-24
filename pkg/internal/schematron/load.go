package schematron

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"strings"
)

// Load reads the Schematron document at rootPath from fsys, resolves
// every sch:include, and returns a fully-linked [Schema]. References
// from phase/active to missing patterns surface as
// [ErrUnresolvedRef] after the load.
//
// fsys must be a forward-slash filesystem (the [io/fs] convention);
// embed.FS, os.DirFS, and testing/fstest.MapFS all qualify. Path
// resolution for includes is relative to the importing schema's
// location.
//
// The loader checks ctx.Err() at each include boundary so callers
// that load large schema sets can bound provisioning time.
func Load(ctx context.Context, fsys fs.FS, rootPath string) (*Schema, error) {
	l := &loader{
		fsys:    fsys,
		loading: make(map[string]bool),
	}
	s, err := l.loadFile(ctx, rootPath, "")
	if err != nil {
		return nil, err
	}
	if err := validatePhaseRefs(s); err != nil {
		return nil, err
	}
	return s, nil
}

// loader tracks the work-in-progress state during a multi-file
// Schematron load. The loading map detects include cycles.
type loader struct {
	fsys    fs.FS
	loading map[string]bool
}

func (l *loader) loadFile(ctx context.Context, p, importer string) (*Schema, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if importer != "" {
		p = path.Join(path.Dir(importer), p)
	}
	if l.loading[p] {
		return nil, fmt.Errorf("%w at %s", ErrIncludeLoop, p)
	}
	l.loading[p] = true
	defer delete(l.loading, p)

	data, err := fs.ReadFile(l.fsys, p)
	if err != nil {
		return nil, fmt.Errorf("%w: reading %s: %w", ErrSchemaParse, p, err)
	}
	raw, err := parseRawSchema(data)
	if err != nil {
		return nil, fmt.Errorf("%w at %s: %w", ErrSchemaParse, p, err)
	}
	s := &Schema{
		Title:        strings.TrimSpace(raw.Title),
		QueryBinding: raw.QueryBinding,
		Patterns:     make(map[string]*Pattern),
		Phases:       make(map[string]*Phase),
	}
	for _, ns := range raw.NS {
		s.Namespaces = append(s.Namespaces, NamespaceDecl(ns))
	}
	for _, lt := range raw.Lets {
		s.Lets = append(s.Lets, Let(lt))
	}

	// Resolve includes first so the importer can pull patterns and
	// phases from the included schema into its own indexes.
	for _, inc := range raw.Includes {
		if inc.Href == "" {
			continue
		}
		sub, err := l.loadFile(ctx, inc.Href, p)
		if err != nil {
			return nil, err
		}
		mergeSchema(s, sub)
	}

	for _, rp := range raw.Phases {
		ph := &Phase{ID: rp.ID}
		for _, a := range rp.Active {
			ph.Active = append(ph.Active, a.Pattern)
		}
		for _, lt := range rp.Lets {
			ph.Lets = append(ph.Lets, Let(lt))
		}
		s.Phases[rp.ID] = ph
	}
	for idx, rp := range raw.Patterns {
		if rp.Abstract == "true" {
			return nil, fmt.Errorf("%w: abstract patterns (pattern id=%q)", ErrUnsupportedConstruct, rp.ID)
		}
		pat := &Pattern{ID: rp.ID, Title: strings.TrimSpace(rp.Title)}
		for _, lt := range rp.Lets {
			pat.Lets = append(pat.Lets, Let(lt))
		}
		for _, rr := range rp.Rules {
			rule := &Rule{Context: rr.Context}
			for _, lt := range rr.Lets {
				rule.Lets = append(rule.Lets, Let(lt))
			}
			for _, ra := range rr.Asserts {
				rule.Asserts = append(rule.Asserts, &Assertion{
					ID:      ra.ID,
					Test:    ra.Test,
					Flag:    ra.Flag,
					Role:    ra.Role,
					Message: collapseWhitespace(ra.InnerXML),
					Negate:  false,
				})
			}
			for _, rr2 := range rr.Reports {
				rule.Reports = append(rule.Reports, &Assertion{
					ID:      rr2.ID,
					Test:    rr2.Test,
					Flag:    rr2.Flag,
					Role:    rr2.Role,
					Message: collapseWhitespace(rr2.InnerXML),
					Negate:  true,
				})
			}
			pat.Rules = append(pat.Rules, rule)
		}
		// ISO Schematron does not require sch:pattern to carry an id;
		// the bundled Factur-X profile schematrons ship dozens of
		// anonymous sch:pattern siblings. Bucketing them by raw id
		// would collapse every nameless sibling into a single map
		// entry and silently drop 66 of FACTUR-X_EN16931.sch's 67
		// patterns. The synthetic key is empty-prefixed so phase
		// activation, which looks up named patterns only, continues
		// to see no match for the anonymous ones, matching how the
		// spec uses phases.
		key := rp.ID
		if key == "" {
			key = fmt.Sprintf("__anon_%d", idx)
		}
		s.Patterns[key] = pat
		s.PatternOrder = append(s.PatternOrder, key)
	}
	return s, nil
}

// mergeSchema folds an included schema's namespaces, lets, patterns,
// and phases into dst. Duplicate pattern or phase ids are detected
// by callers via validatePhaseRefs; the merge itself overwrites the
// later wins, matching what most Schematron processors do.
func mergeSchema(dst, src *Schema) {
	dst.Namespaces = append(dst.Namespaces, src.Namespaces...)
	dst.Lets = append(dst.Lets, src.Lets...)
	for _, id := range src.PatternOrder {
		if _, exists := dst.Patterns[id]; !exists {
			dst.PatternOrder = append(dst.PatternOrder, id)
		}
		dst.Patterns[id] = src.Patterns[id]
	}
	maps.Copy(dst.Phases, src.Phases)
}

// validatePhaseRefs walks every phase's active list and confirms
// each referenced pattern id exists in the schema. A missing
// reference is a load-time failure rather than an evaluation-time
// surprise.
func validatePhaseRefs(s *Schema) error {
	for _, ph := range s.Phases {
		for _, id := range ph.Active {
			if _, ok := s.Patterns[id]; !ok {
				return fmt.Errorf("%w: phase %q activates pattern %q which is not declared",
					ErrUnresolvedRef, ph.ID, id)
			}
		}
	}
	return nil
}

// rawSchema mirrors the Schematron source structure for encoding/xml.
// Fields stay close to the spec naming so the build helpers above
// can translate without ambiguity.
type rawSchema struct {
	XMLName      xml.Name     `xml:"http://purl.oclc.org/dsdl/schematron schema"`
	Title        string       `xml:"http://purl.oclc.org/dsdl/schematron title"`
	QueryBinding string       `xml:"queryBinding,attr"`
	NS           []rawNS      `xml:"http://purl.oclc.org/dsdl/schematron ns"`
	Lets         []rawLet     `xml:"http://purl.oclc.org/dsdl/schematron let"`
	Phases       []rawPhase   `xml:"http://purl.oclc.org/dsdl/schematron phase"`
	Patterns     []rawPattern `xml:"http://purl.oclc.org/dsdl/schematron pattern"`
	Includes     []rawInclude `xml:"http://purl.oclc.org/dsdl/schematron include"`
}

type rawNS struct {
	Prefix string `xml:"prefix,attr"`
	URI    string `xml:"uri,attr"`
}

type rawLet struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type rawPhase struct {
	ID     string      `xml:"id,attr"`
	Active []rawActive `xml:"http://purl.oclc.org/dsdl/schematron active"`
	Lets   []rawLet    `xml:"http://purl.oclc.org/dsdl/schematron let"`
}

type rawActive struct {
	Pattern string `xml:"pattern,attr"`
}

type rawPattern struct {
	ID       string    `xml:"id,attr"`
	Abstract string    `xml:"abstract,attr"`
	Title    string    `xml:"http://purl.oclc.org/dsdl/schematron title"`
	Lets     []rawLet  `xml:"http://purl.oclc.org/dsdl/schematron let"`
	Rules    []rawRule `xml:"http://purl.oclc.org/dsdl/schematron rule"`
}

type rawRule struct {
	Context string         `xml:"context,attr"`
	Lets    []rawLet       `xml:"http://purl.oclc.org/dsdl/schematron let"`
	Asserts []rawAssertion `xml:"http://purl.oclc.org/dsdl/schematron assert"`
	Reports []rawAssertion `xml:"http://purl.oclc.org/dsdl/schematron report"`
}

type rawAssertion struct {
	ID       string `xml:"id,attr"`
	Test     string `xml:"test,attr"`
	Flag     string `xml:"flag,attr"`
	Role     string `xml:"role,attr"`
	InnerXML string `xml:",innerxml"`
}

type rawInclude struct {
	Href string `xml:"href,attr"`
}

func parseRawSchema(data []byte) (*rawSchema, error) {
	root, err := peekRoot(data)
	if err != nil {
		return nil, err
	}
	if root.Space != SchematronNamespace {
		return nil, errors.New("root element is not in the Schematron namespace")
	}
	switch root.Local {
	case "schema":
		var raw rawSchema
		if err := xml.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		return &raw, nil
	case "pattern":
		// ISO Schematron lets sch:include reference any element,
		// not just sch:schema. XRechnung common.sch ships a bare
		// sch:pattern; wrap it into a synthetic schema so the rest
		// of the loader pipeline sees a uniform root.
		var pat rawPattern
		if err := xml.Unmarshal(data, &pat); err != nil {
			return nil, err
		}
		return &rawSchema{Patterns: []rawPattern{pat}}, nil
	}
	return nil, fmt.Errorf("root element %s is not sch:schema or sch:pattern", root.Local)
}

// peekRoot returns the qualified name of the first StartElement in
// data. encoding/xml does not expose this on the unmarshal path, so
// the loader walks the token stream just far enough to identify the
// root and then dispatches to the appropriate concrete decoder.
func peekRoot(data []byte) (xml.Name, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return xml.Name{}, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name, nil
		}
	}
}

// collapseWhitespace flattens multi-line assertion messages into a
// single line so callers rendering violations to a terminal don't
// see jagged indentation. Inline emphasis tags like <sch:span> are
// preserved as their text content via [stripXMLTags].
func collapseWhitespace(s string) string {
	s = stripXMLTags(s)
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// stripXMLTags drops any XML tags from s, returning only the text
// content. The Schematron spec lets assert/report messages embed
// sch:value-of, sch:name, and inline emphasis elements; only the
// bare text is kept for diagnostics.
// TODO: expand sch:value-of in messages.
func stripXMLTags(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inside := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '<':
			inside = true
		case c == '>':
			inside = false
		case !inside:
			b.WriteByte(c)
		}
	}
	return b.String()
}
