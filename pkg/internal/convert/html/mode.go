package html

import (
	"strings"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// documentMode returns the mode of the HTML document src (HTML Standard,
// 13.2.6.4.1, the "initial" insertion mode): the DOCTYPE token that comes
// before anything but white space and comments decides it, and a document
// whose first token is anything else is in quirks mode. A frame's srcdoc
// document is never parsed through it: it is in no-quirks mode.
func documentMode(src string) css.DocumentMode {
	z := dom.NewTokenizer(strings.NewReader(src))
	for {
		switch z.Next() {
		case dom.CommentToken:
		case dom.TextToken:
			if strings.Trim(string(z.Raw()), "\t\n\f\r ") != "" {
				return css.Quirks
			}
		case dom.DoctypeToken:
			return doctypeMode(string(z.Raw()))
		case dom.ErrorToken, dom.StartTagToken, dom.EndTagToken, dom.SelfClosingTagToken:
			return css.Quirks
		}
	}
}

// doctype is a DOCTYPE token (HTML Standard, 13.2.5.53 to 13.2.5.68): its
// name, its public and system identifiers with whether each is present,
// and its force-quirks flag.
type doctype struct {
	name, public, system string
	hasPublic, hasSystem bool
	forceQuirks          bool
}

// readDoctype tokenizes raw, the source of a DOCTYPE token from "<!" to
// its closing ">" or the end of the input, by the DOCTYPE states of the
// HTML Standard's tokenizer (13.2.5.53 to 13.2.5.68).
func readDoctype(raw string) doctype {
	var d doctype
	s := raw[min(len(raw), len("<!DOCTYPE")):]
	i := 0
	ws := func() {
		for i < len(s) && isHTMLSpace(s[i]) {
			i++
		}
	}
	// quoted reads an identifier from the quote at i, reporting whether
	// its closing quote was found: a ">" or the end of the input ends it
	// abruptly, which sets the force-quirks flag.
	quoted := func() (string, bool) {
		q := s[i]
		i++
		j := strings.IndexByte(s[i:], q)
		k := strings.IndexByte(s[i:], '>')
		if j < 0 || k >= 0 && k < j {
			end := len(s)
			if k >= 0 {
				end = i + k
			}
			id := s[i:end]
			i = len(s)
			return id, false
		}
		id := s[i : i+j]
		i += j + 1
		return id, true
	}
	ws()
	if i >= len(s) || s[i] == '>' {
		// A missing name: 13.2.5.54.
		d.forceQuirks = true
		return d
	}
	start := i
	for i < len(s) && !isHTMLSpace(s[i]) && s[i] != '>' {
		i++
	}
	d.name = strings.ToLower(strings.ReplaceAll(s[start:i], "\x00", "�"))
	ws()
	if i >= len(s) {
		d.forceQuirks = true
		return d
	}
	if s[i] == '>' {
		return d
	}
	keyword := strings.ToLower(s[i:min(len(s), i+6)])
	if keyword != "public" && keyword != "system" {
		// 13.2.5.56: anything else after the name is bogus.
		d.forceQuirks = true
		return d
	}
	i += 6
	if keyword == "public" {
		ws()
		if i >= len(s) || s[i] != '"' && s[i] != '\'' {
			d.forceQuirks = true
			return d
		}
		id, closed := quoted()
		d.public, d.hasPublic = id, true
		if !closed {
			d.forceQuirks = true
			return d
		}
		ws()
		if i >= len(s) {
			d.forceQuirks = true
			return d
		}
		if s[i] == '>' {
			return d
		}
		if s[i] != '"' && s[i] != '\'' {
			d.forceQuirks = true
			return d
		}
	} else {
		ws()
		if i >= len(s) || s[i] != '"' && s[i] != '\'' {
			d.forceQuirks = true
			return d
		}
	}
	id, closed := quoted()
	d.system, d.hasSystem = id, true
	if !closed {
		d.forceQuirks = true
		return d
	}
	ws()
	if i >= len(s) {
		d.forceQuirks = true
	}
	// Anything else after the system identifier is bogus, which does
	// not set the flag (13.2.5.67).
	return d
}

// doctypeMode returns the mode the DOCTYPE token of source raw sets
// (HTML Standard, 13.2.6.4.1), its identifiers compared ASCII
// case-insensitively.
func doctypeMode(raw string) css.DocumentMode {
	d := readDoctype(raw)
	public, system := strings.ToLower(d.public), strings.ToLower(d.system)
	switch {
	case d.forceQuirks || d.name != "html":
		return css.Quirks
	case d.hasPublic && (public == "-//w3o//dtd w3 html strict 3.0//en//" || public == "-/w3c/dtd html 4.0 transitional/en" || public == "html"):
		return css.Quirks
	case d.hasSystem && system == "http://www.ibm.com/data/dtd/v11/ibmxhtml1-transitional.dtd":
		return css.Quirks
	}
	if d.hasPublic {
		for _, p := range quirksPublicPrefixes {
			if strings.HasPrefix(public, p) {
				return css.Quirks
			}
		}
		html401 := strings.HasPrefix(public, "-//w3c//dtd html 4.01 frameset//") || strings.HasPrefix(public, "-//w3c//dtd html 4.01 transitional//")
		if html401 && system == "" {
			return css.Quirks
		}
		if html401 || strings.HasPrefix(public, "-//w3c//dtd xhtml 1.0 frameset//") || strings.HasPrefix(public, "-//w3c//dtd xhtml 1.0 transitional//") {
			return css.LimitedQuirks
		}
	}
	return css.NoQuirks
}

// quirksPublicPrefixes are the starts of the public identifiers that set
// quirks mode (HTML Standard, 13.2.6.4.1), in lower case.
var quirksPublicPrefixes = []string{
	"+//silmaril//dtd html pro v0r11 19970101//",
	"-//as//dtd html 3.0 aswedit + extensions//",
	"-//advasoft ltd//dtd html 3.0 aswedit + extensions//",
	"-//ietf//dtd html 2.0 level 1//",
	"-//ietf//dtd html 2.0 level 2//",
	"-//ietf//dtd html 2.0 strict level 1//",
	"-//ietf//dtd html 2.0 strict level 2//",
	"-//ietf//dtd html 2.0 strict//",
	"-//ietf//dtd html 2.0//",
	"-//ietf//dtd html 2.1e//",
	"-//ietf//dtd html 3.0//",
	"-//ietf//dtd html 3.2 final//",
	"-//ietf//dtd html 3.2//",
	"-//ietf//dtd html 3//",
	"-//ietf//dtd html level 0//",
	"-//ietf//dtd html level 1//",
	"-//ietf//dtd html level 2//",
	"-//ietf//dtd html level 3//",
	"-//ietf//dtd html strict level 0//",
	"-//ietf//dtd html strict level 1//",
	"-//ietf//dtd html strict level 2//",
	"-//ietf//dtd html strict level 3//",
	"-//ietf//dtd html strict//",
	"-//ietf//dtd html//",
	"-//metrius//dtd metrius presentational//",
	"-//microsoft//dtd internet explorer 2.0 html strict//",
	"-//microsoft//dtd internet explorer 2.0 html//",
	"-//microsoft//dtd internet explorer 2.0 tables//",
	"-//microsoft//dtd internet explorer 3.0 html strict//",
	"-//microsoft//dtd internet explorer 3.0 html//",
	"-//microsoft//dtd internet explorer 3.0 tables//",
	"-//netscape comm. corp.//dtd html//",
	"-//netscape comm. corp.//dtd strict html//",
	"-//o'reilly and associates//dtd html 2.0//",
	"-//o'reilly and associates//dtd html extended 1.0//",
	"-//o'reilly and associates//dtd html extended relaxed 1.0//",
	"-//sq//dtd html 2.0 hotmetal + extensions//",
	"-//softquad software//dtd hotmetal pro 6.0::19990601::extensions to html 4.0//",
	"-//softquad//dtd hotmetal pro 4.0::19971010::extensions to html 4.0//",
	"-//spyglass//dtd html 2.0 extended//",
	"-//sun microsystems corp.//dtd hotjava html//",
	"-//sun microsystems corp.//dtd hotjava strict html//",
	"-//w3c//dtd html 3 1995-03-24//",
	"-//w3c//dtd html 3.2 draft//",
	"-//w3c//dtd html 3.2 final//",
	"-//w3c//dtd html 3.2//",
	"-//w3c//dtd html 3.2s draft//",
	"-//w3c//dtd html 4.0 frameset//",
	"-//w3c//dtd html 4.0 transitional//",
	"-//w3c//dtd html experimental 19960712//",
	"-//w3c//dtd html experimental 970421//",
	"-//w3c//dtd w3 html//",
	"-//w3o//dtd w3 html 3.0//",
	"-//webtechs//dtd mozilla html 2.0//",
	"-//webtechs//dtd mozilla html//",
}
