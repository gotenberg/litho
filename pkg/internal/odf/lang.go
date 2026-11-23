package odf

// Lang returns the language the text properties props state as a BCP 47
// tag: fo:language, with fo:country when it names one (ODF 1.3, Part 3,
// 20.204 fo:country, 20.211 fo:language). "" when they state none, or
// state zxx or none, a text in no language.
func Lang(props Props) string {
	lang := props.Get("fo:language")
	if lang == "" || lang == "zxx" || lang == "none" {
		return ""
	}
	if country := props.Get("fo:country"); country != "" && country != "none" {
		return lang + "-" + country
	}
	return lang
}
