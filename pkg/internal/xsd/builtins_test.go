package xsd

import "testing"

// builtin returns a *BuiltinType for the given XS-namespace local name.
func builtin(local string) *BuiltinType {
	return &BuiltinType{Name: QName{Namespace: XSNamespace, Local: local}}
}

func TestValidateLexical_StringFamily(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		value   string
		wantErr bool
	}{
		{"string accepts anything", "string", "with\ttab", false},
		{"normalizedString rejects tab", "normalizedString", "with\ttab", true},
		{"normalizedString rejects newline", "normalizedString", "line1\nline2", true},
		{"normalizedString accepts space", "normalizedString", "hello world", false},
		{"token rejects leading space", "token", " hello", true},
		{"token rejects trailing space", "token", "hello ", true},
		{"token rejects double space", "token", "hello  world", true},
		{"token accepts single space", "token", "hello world", false},
		{"anyURI accepts empty", "anyURI", "", false},
		{"anyURI accepts arbitrary", "anyURI", "http://example.com/x", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateLexical(tc.value, builtin(tc.base))
			if tc.wantErr && got == nil {
				t.Errorf("got nil, want lexical error")
			}
			if !tc.wantErr && got != nil {
				t.Errorf("got %v, want nil", got)
			}
		})
	}
}

func TestValidateLexical_Numerics(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		value   string
		wantErr bool
	}{
		{"decimal accepts integer", "decimal", "42", false},
		{"decimal accepts negative", "decimal", "-1.5", false},
		{"decimal rejects letters", "decimal", "abc", true},
		{"integer accepts whole", "integer", "100", false},
		{"integer rejects fraction", "integer", "1.5", true},
		{"nonNegativeInteger rejects -1", "nonNegativeInteger", "-1", true},
		{"nonNegativeInteger accepts 0", "nonNegativeInteger", "0", false},
		{"positiveInteger rejects 0", "positiveInteger", "0", true},
		{"positiveInteger accepts 1", "positiveInteger", "1", false},
		{"int respects 32-bit upper bound", "int", "2147483648", true},
		{"int accepts 2^31-1", "int", "2147483647", false},
		{"byte rejects 200", "byte", "200", true},
		{"unsignedByte accepts 200", "unsignedByte", "200", false},
		{"boolean accepts 'true'", "boolean", "true", false},
		{"boolean accepts '0'", "boolean", "0", false},
		{"boolean rejects 'yes'", "boolean", "yes", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateLexical(tc.value, builtin(tc.base))
			if tc.wantErr && got == nil {
				t.Errorf("got nil, want lexical error")
			}
			if !tc.wantErr && got != nil {
				t.Errorf("got %v, want nil", got)
			}
		})
	}
}

func TestValidateLexical_DateTimes(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		value   string
		wantErr bool
	}{
		{"date accepts CCYY-MM-DD", "date", "2026-05-17", false},
		{"date rejects short", "date", "26-05-17", true},
		{"date rejects bad month", "date", "2026-13-01", true},
		{"date accepts with Z", "date", "2026-05-17Z", false},
		{"date accepts with offset", "date", "2026-05-17+01:00", false},
		{"dateTime accepts full", "dateTime", "2026-05-17T12:00:00", false},
		{"dateTime accepts with offset", "dateTime", "2026-05-17T12:00:00+01:00", false},
		{"dateTime rejects no T", "dateTime", "2026-05-17 12:00:00", true},
		{"time accepts HH:MM:SS", "time", "12:00:00", false},
		{"time rejects bad shape", "time", "12-00-00", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateLexical(tc.value, builtin(tc.base))
			if tc.wantErr && got == nil {
				t.Errorf("got nil, want lexical error")
			}
			if !tc.wantErr && got != nil {
				t.Errorf("got %v, want nil", got)
			}
		})
	}
}

func TestValidateLexical_UnknownReturnsNil(t *testing.T) {
	// Built-ins outside the explicit dispatch list fall through to
	// permissive acceptance so the link pass can still resolve refs
	// without forcing the validator to handle every primitive.
	got := validateLexical("anything", builtin("duration"))
	if got != nil {
		t.Errorf("got %v, want nil for permissive built-in", got)
	}
}
