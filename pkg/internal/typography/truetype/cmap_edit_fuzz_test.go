package truetype

import (
	"context"
	"testing"
)

// FuzzRewriteCmap feeds arbitrary bytes to [RewriteCmap], which rewrites the
// cmap of a font program a remediation caller supplies. The contract is
// crash-freedom, and a rewritten program rewrites again.
func FuzzRewriteCmap(f *testing.F) {
	valid := subsetSource()
	f.Add(valid, uint16(3), uint16(1))
	f.Add(valid[:len(valid)/2], uint16(3), uint16(0))
	f.Add(richSfnt(), uint16(3), uint16(1))
	f.Fuzz(func(t *testing.T, data []byte, platform, encoding uint16) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()
		from := CmapSubtable{PlatformID: platform, EncodingID: encoding}
		aliases := []CmapAlias{{Target: from, From: from}, {Target: CmapSubtable{PlatformID: 1, EncodingID: 0}, From: from}}
		out, err := RewriteCmap(ctx, data, aliases)
		if err != nil {
			return
		}
		again := []CmapAlias{{Target: CmapSubtable{PlatformID: 3, EncodingID: 1}, From: CmapSubtable{PlatformID: 1, EncodingID: 0}}}
		if _, err := RewriteCmap(ctx, out, again); err != nil {
			t.Errorf("rewriting a rewritten program: %v", err)
		}
	})
}
