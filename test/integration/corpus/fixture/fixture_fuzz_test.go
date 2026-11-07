package fixture

import "testing"

// FuzzParseOptions checks that no options file makes ParseOptions panic, and
// that what it accepts maps to options without panicking either.
func FuzzParseOptions(f *testing.F) {
	f.Add([]byte(`{"paperWidth": 8.5, "landscape": true, "scale": 0.75, "nativePages": "2-3", "emulatedMediaFeatures": {"prefers-color-scheme": "dark"}}`))
	f.Add([]byte(`{"paperWidth": 8.`))
	f.Fuzz(func(t *testing.T, data []byte) {
		o, err := ParseOptions(data)
		if err != nil {
			return
		}
		s := &Source{Options: o}
		s.Paper()
		s.Margins()
		s.MediaType()
		s.PreferCSSPageSize()
		s.PrintOptions()
	})
}

// FuzzParseOfficeOptions checks that no Office options file makes
// ParseOfficeOptions panic, and that what it accepts maps to options without
// panicking either.
func FuzzParseOfficeOptions(f *testing.F) {
	f.Add([]byte(`{"password": "foo", "landscape": true, "nativePages": "2-3", "nativeBookmarks": false, "paperWidth": 4, "paperHeight": 6}`))
	f.Add([]byte(`{"paperWidth": 4`))
	f.Fuzz(func(t *testing.T, data []byte) {
		o, err := ParseOfficeOptions(data)
		if err != nil {
			return
		}
		s := &Office{Options: o}
		s.ConvertOptions()
	})
}
