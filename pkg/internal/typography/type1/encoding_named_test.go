package type1

import "testing"

// TestNamedCodes pins which codes a built-in encoding vector names: every
// entry a PostScript interpreter runs, whatever its spacing or radix, names
// its code unless the name is .notdef, and a program that refers to a named
// encoding, or writes no entry, gives no answer.
func TestNamedCodes(t *testing.T) {
	t.Parallel()

	named, ok := NamedCodes([]byte("/Encoding 256 array\n0 1 255 {1 index exch /.notdef put} for\n" +
		"dup 65 /A put\ndup 32/space put\ndup 8#102 /B put\ndup 67 /.notdef put\nreadonly def\ncurrentfile eexec\n"))
	if !ok {
		t.Fatal("NamedCodes ok = false on an explicit vector")
	}
	for code, want := range map[int]bool{65: true, 32: true, 66: true, 67: false, 68: false} {
		if named[code] != want {
			t.Errorf("named[%d] = %v, want %v", code, named[code], want)
		}
	}

	for _, data := range []string{
		"/Encoding StandardEncoding def\ncurrentfile eexec\n",
		"/FontName /X def\ncurrentfile eexec\ndup 65 /A put\n",
		"",
	} {
		if _, ok := NamedCodes([]byte(data)); ok {
			t.Errorf("NamedCodes(%q) ok = true, want false", data)
		}
	}
}
