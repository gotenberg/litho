package compliance

import "testing"

// indexedOver wraps a DeviceN colour-space array in an /Indexed space with one
// palette entry (ISO 32000-1, 8.6.6.3: [/Indexed base hival lookup]). components
// sizes the lookup string, which holds one byte per base component per entry.
func indexedOver(deviceN string, components int) string {
	lookup := ""
	for range components {
		lookup += "00"
	}
	return "[/Indexed " + deviceN + " 0 <" + lookup + ">]"
}

// TestDeviceNColorants_IndexedBase pins that a DeviceN reached as the base of
// an /Indexed colour space is judged for ISO 19005-2 §6.2.4.4 t1.
//
// ISO 32000-1, 8.6.6.3 makes an /Indexed space's base a colour space in its own
// right, and veraPDF models it that way, anchoring the failure at the base
// DeviceN. litho tested only the head of the outer array at every call site, so
// an /Indexed wrapper hid the DeviceN completely.
//
// The declared-colorants arm is the control that matters: /Indexed over
// /DeviceN [/Red /Green /Blue] whose /Colorants declares all three passes.
// Following the base must not turn that into a report, so a fixture proving
// only that the rule can fire would not be enough.
//
// # Corpus evidence
//
//   - pdfjs issue9940.pdf: veraPDF anchors the failure at
//     imageCS[0](52 0 obj PDIndexed)/base[0](51 0 obj PDDeviceN), whose
//     /DeviceN names /IBM with no /Colorants entry for it, and the file went
//     unreported while the /Indexed wrapper hid it. The undeclared-colorants
//     case takes this shape.
//   - verapdf PDF_A-4__veraPDF_test_suite_6-2-4-5-t01-pass-f.pdf: carries
//     exactly the declared-colorants shape, and veraPDF passes it with real
//     verdicts.
func TestDeviceNColorants_IndexedBase(t *testing.T) {
	t.Parallel()

	const undeclared = "[/DeviceN [/Red /Green /Blue] /DeviceRGB 6 0 R]"
	const declared = "[/DeviceN [/Red /Green /Blue] /DeviceRGB 6 0 R " +
		"<< /Colorants << /Red [/Separation /Red /DeviceRGB 6 0 R] " +
		"/Green [/Separation /Green /DeviceRGB 6 0 R] " +
		"/Blue [/Separation /Blue /DeviceRGB 6 0 R] >> >>]"
	const noneOnly = "[/DeviceN [/None /None] /DeviceRGB 6 0 R]"

	tests := []struct {
		name string
		cs   string
		want bool
	}{
		{
			// The wrapper hid an undeclared spot colorant.
			name: "an /Indexed base with undeclared colorants fires",
			cs:   indexedOver(undeclared, 3),
			want: true,
		},
		{
			// Following the base must not invent a report.
			name: "an /Indexed base whose colorants are declared stays silent",
			cs:   indexedOver(declared, 3),
			want: false,
		},
		{
			// ISO 32000-1, 8.6.6.5: None never paints, so it names no ink.
			name: "an /Indexed base naming only None stays silent",
			cs:   indexedOver(noneOnly, 2),
			want: false,
		},
		{
			// The pre-existing direct path must keep working unchanged.
			name: "a bare DeviceN still fires without any /Indexed wrapper",
			cs:   undeclared,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run("resource ColorSpace: "+tt.name, func(t *testing.T) {
			t.Parallel()
			if got := firesRule2b(t, deviceNResourcePDF(tt.cs), "6.2.4.4", 1); got != tt.want {
				t.Errorf("§6.2.4.4 t1 fired = %v, want %v", got, tt.want)
			}
		})
		t.Run("image XObject: "+tt.name, func(t *testing.T) {
			t.Parallel()
			if got := firesRule2b(t, deviceNImagePDF(tt.cs), "6.2.4.4", 1); got != tt.want {
				t.Errorf("§6.2.4.4 t1 fired = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("a nested /Indexed chain is followed", func(t *testing.T) {
		t.Parallel()
		// ISO 32000-1, 8.6.6.3 forbids an /Indexed base from being another
		// /Indexed space, so this is malformed input rather than a real shape.
		// It is here because the colour link walk follows the chain and must
		// stay bounded (see [maxColorSpaceNesting]): the assertion is that
		// litho neither hangs nor misses it.
		nested := "[/Indexed " + indexedOver(undeclared, 3) + " 0 <00>]"
		if !firesRule2b(t, deviceNResourcePDF(nested), "6.2.4.4", 1) {
			t.Error("a DeviceN under nested /Indexed spaces must still be judged")
		}
	})
}
