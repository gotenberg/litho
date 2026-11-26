package compliance

import (
	"fmt"
	"testing"
)

// TestDeviceColorPage_ReadsTheContextTheCheckWrites pins that the page and
// family a device colour violation states round-trip through
// DeviceColorPage, at every PDF/A part, and that any other violation names no
// page.
func TestDeviceColorPage_ReadsTheContextTheCheckWrites(t *testing.T) {
	t.Parallel()

	for part := 1; part <= 4; part++ {
		for _, f := range deviceFamilies {
			v := Violation{Rule: f.rule(part), Context: fmt.Sprintf(deviceUseContext, 7, f.name, f.defaultName, intentPhrase(f.intent))}
			if page, ok := DeviceColorPage(v, f.name); !ok || page != 7 {
				t.Errorf("part %d, %s: DeviceColorPage = %d, %v, want 7, true", part, f.name, page, ok)
			}
			for _, other := range deviceFamilies {
				if other.name == f.name {
					continue
				}
				if _, ok := DeviceColorPage(v, other.name); ok {
					t.Errorf("part %d: a %s violation read as %s", part, f.name, other.name)
				}
			}
		}
	}
	group := Violation{Rule: deviceCMYKRule(2), Context: "page 2: transparency group blending colour space is DeviceCMYK without CMYK OutputIntent"}
	if _, ok := DeviceColorPage(group, "DeviceCMYK"); ok {
		t.Error("a transparency group's blending space read as page content")
	}
	if _, ok := DeviceColorPage(Violation{Context: "page 2: DeviceCMYK used without DefaultCMYK or CMYK OutputIntent"}, "DeviceCMYK"); ok {
		t.Error("a violation of another rule read as a device colour one")
	}
}
