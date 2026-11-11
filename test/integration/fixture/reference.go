package fixture

import (
	"errors"
	"os"
)

// Reference is what a source's options say of the reference it is judged
// against.
type Reference struct {
	// Of is the fixture the source's reference_of names, whose reference
	// stands for the source's own; "" when it names none.
	Of string
	// LithoOptions are the litho-only keys the source's conversion applies
	// ([Source.LithoOptions], [Office.LithoOptions]). No reference engine
	// takes them, so a source that sets one and names no Of has no
	// reference to be judged against.
	LithoOptions []string
}

// Unreferenced reports whether the source sets a litho-only option and
// borrows no reference: no engine can print what its conversion prints.
func (r Reference) Unreferenced() bool {
	return r.Of == "" && len(r.LithoOptions) > 0
}

// LoadReference reads what the source at path says of its reference, for
// either kind of source: a directory holding index.html or index.md reads
// as an HTML or Markdown source ([Load]), any other path as an Office source
// ([LoadOffice]), which a document file is, without options. It fails as
// those do.
func LoadReference(path string) (Reference, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		src, err := Load(path)
		switch {
		case err == nil:
			return Reference{Of: src.Options.ReferenceOf, LithoOptions: src.LithoOptions()}, nil
		case !errors.Is(err, ErrNoDocument):
			return Reference{}, err
		}
	}
	src, err := LoadOffice(path)
	if err != nil {
		return Reference{}, err
	}
	return Reference{Of: src.Options.ReferenceOf, LithoOptions: src.LithoOptions()}, nil
}
