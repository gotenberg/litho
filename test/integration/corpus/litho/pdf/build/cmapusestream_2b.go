package build

func init() { register("cmapusestream_2b", cmapusestream2b) }

// cmapusestream2b builds cmapusestream_2b.pdf: an encoding CMap built on an embedded base.
//
// A /UseCMap entry in an embedded encoding CMap's stream dictionary (ISO 32000-1,
// 9.7.5.3, Table 120) names the CMap it builds on, here another embedded CMap
// stream. veraPDF's PDCMap.getCMapFile adds that base once the program has run,
// so CMap.toCID reads a code the program leaves unmapped through it. The corpus
// files whose encoding CMaps carry /UseCMap show no code their base supplies, so
// no count reached this.
//
// | Font | Encoding CMap                        | Base                  | Shows     | CIDs |
// | ---- | ------------------------------------ | --------------------- | --------- | ---- |
// | S1   | cidchar 0041 to 5, `/UseCMap` a base | cidchar 0042 to 4     | 0041 0042 | 5, 4 |
//
// Both CIDs select a glyph, so the glyph rules stay silent: a reader that left
// 0042 unmapped would send it to CID 0, absent and .notdef. The base is a stream
// whose name is no predefined CMap, which ISO 19005-2, 6.2.11.3.3 t3 fails.
//
// Every count was confirmed with live veraPDF 1.30.2. See ../FIXTURES.md for the
// authoritative expected firing set, and _encodingcmap for the font program.
func cmapusestream2b() ([]byte, error) {
	f := newECFixture()
	base := f.cmap("LithoS1Base", "1 begincidchar\n<0042> 4\nendcidchar\n", ecCMapOpts{})
	f.font("S1", "LithoUseCMapStream", f.cmap("LithoS1", "1 begincidchar\n<0041> 5\nendcidchar\n", ecCMapOpts{UseCMap: base}),
		"00410042", nil, nil)
	return f.save()
}
