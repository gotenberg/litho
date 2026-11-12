package ooxml

import (
	"bytes"
	"testing"
)

func FuzzDeobfuscateFont(f *testing.F) {
	valid := bytes.Repeat([]byte{0xA5}, 64)
	f.Add(valid, "{4C1A2B3D-5E6F-4071-8293-A4B5C6D7E8F9}")
	f.Add(valid[:16], "{4C1A2B3D-5E6F-4071-8293-A4B5C6D7E8F9}")
	f.Fuzz(func(t *testing.T, data []byte, key string) {
		before := bytes.Clone(data)
		if err := DeobfuscateFont(data, key); err != nil {
			if !bytes.Equal(data, before) {
				t.Fatal("a refused part changed")
			}
			return
		}
		if len(data) != len(before) || !bytes.Equal(data[32:], before[32:]) {
			t.Fatal("deobfuscation changed the program past its head")
		}
	})
}
