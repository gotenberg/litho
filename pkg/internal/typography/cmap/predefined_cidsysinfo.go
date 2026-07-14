package cmap

// cidSystemInfoEntry is one (Ordering, Supplement) of a predefined CMap's
// CIDSystemInfo. Registry is always "Adobe".
type cidSystemInfoEntry struct {
	ordering   string
	supplement int
}

// predefinedCMapCIDSystemInfo maps each predefined CMap name (ISO 32000-1
// Table 118) to the CIDSystemInfo it is treated as declaring at each PDF
// specification generation, mirroring veraPDF's CharacterCollections table
// (org.verapdf.pd.font.cmap.CharacterCollections), which derives from Adobe's
// registered CMap resources. Registry is always "Adobe". Index 0 applies to the
// ISO 32000-1 era (PDF/A-1/2/3); the last index applies to ISO 32000-2
// (PDF/A-4). A few CMaps changed character collection across generations (for
// example UniKS-UTF16-H: Adobe-Korea1-2 in older specs, Adobe-KR-9 in newer),
// so both Ordering and Supplement are indexed rather than derived from the name.
//
//nolint:gochecknoglobals // static spec table (Adobe registered CMaps / veraPDF CharacterCollections)
var predefinedCMapCIDSystemInfo = map[string][]cidSystemInfoEntry{
	"83pv-RKSJ-H":      {{"Japan1", 1}, {"Japan1", 1}, {"Japan1", 7}},
	"90ms-RKSJ-H":      {{"Japan1", 2}, {"Japan1", 2}, {"Japan1", 7}},
	"90ms-RKSJ-V":      {{"Japan1", 2}, {"Japan1", 2}, {"Japan1", 7}},
	"90msp-RKSJ-H":     {{"Japan1", 2}, {"Japan1", 2}, {"Japan1", 7}},
	"90msp-RKSJ-V":     {{"Japan1", 2}, {"Japan1", 2}, {"Japan1", 7}},
	"90pv-RKSJ-H":      {{"Japan1", 1}, {"Japan1", 1}, {"Japan1", 7}},
	"Add-RKSJ-H":       {{"Japan1", 1}, {"Japan1", 1}, {"Japan1", 7}},
	"Add-RKSJ-V":       {{"Japan1", 1}, {"Japan1", 1}, {"Japan1", 7}},
	"B5pc-H":           {{"CNS1", 0}, {"CNS1", 0}, {"CNS1", 7}},
	"B5pc-V":           {{"CNS1", 0}, {"CNS1", 0}, {"CNS1", 7}},
	"CNS-EUC-H":        {{"CNS1", 0}, {"CNS1", 0}, {"CNS1", 7}},
	"CNS-EUC-V":        {{"CNS1", 0}, {"CNS1", 0}, {"CNS1", 7}},
	"ETen-B5-H":        {{"CNS1", 0}, {"CNS1", 0}, {"CNS1", 7}},
	"ETen-B5-V":        {{"CNS1", 0}, {"CNS1", 0}, {"CNS1", 7}},
	"ETenms-B5-H":      {{"CNS1", 0}, {"CNS1", 0}, {"CNS1", 7}},
	"ETenms-B5-V":      {{"CNS1", 0}, {"CNS1", 0}, {"CNS1", 7}},
	"EUC-H":            {{"Japan1", 1}, {"Japan1", 1}, {"Japan1", 7}},
	"EUC-V":            {{"Japan1", 1}, {"Japan1", 1}, {"Japan1", 7}},
	"Ext-RKSJ-H":       {{"Japan1", 2}, {"Japan1", 2}, {"Japan1", 7}},
	"Ext-RKSJ-V":       {{"Japan1", 2}, {"Japan1", 2}, {"Japan1", 7}},
	"GB-EUC-H":         {{"GB1", 0}, {"GB1", 0}, {"GB1", 5}},
	"GB-EUC-V":         {{"GB1", 0}, {"GB1", 0}, {"GB1", 5}},
	"GBK-EUC-H":        {{"GB1", 2}, {"GB1", 2}, {"GB1", 5}},
	"GBK-EUC-V":        {{"GB1", 2}, {"GB1", 2}, {"GB1", 5}},
	"GBK2K-H":          {{"GB1", 4}, {"GB1", 4}, {"GB1", 5}},
	"GBK2K-V":          {{"GB1", 4}, {"GB1", 4}, {"GB1", 5}},
	"GBKp-EUC-H":       {{"GB1", 2}, {"GB1", 2}, {"GB1", 5}},
	"GBKp-EUC-V":       {{"GB1", 2}, {"GB1", 2}, {"GB1", 5}},
	"GBpc-EUC-H":       {{"GB1", 0}, {"GB1", 0}, {"GB1", 5}},
	"GBpc-EUC-V":       {{"GB1", 0}, {"GB1", 0}, {"GB1", 5}},
	"H":                {{"Japan1", 1}, {"Japan1", 1}, {"Japan1", 7}},
	"HKscs-B5-H":       {{"CNS1", 3}, {"CNS1", 3}, {"CNS1", 7}},
	"HKscs-B5-V":       {{"CNS1", 3}, {"CNS1", 3}, {"CNS1", 7}},
	"Identity-H":       {{"Identity", 0}, {"Identity", 0}, {"Identity", 0}},
	"Identity-V":       {{"Identity", 0}, {"Identity", 0}, {"Identity", 0}},
	"KSC-EUC-H":        {{"Korea1", 0}, {"Korea1", 0}, {"KR", 9}},
	"KSC-EUC-V":        {{"Korea1", 0}, {"Korea1", 0}, {"KR", 9}},
	"KSCms-UHC-H":      {{"Korea1", 1}, {"Korea1", 1}, {"KR", 9}},
	"KSCms-UHC-HW-H":   {{"Korea1", 1}, {"Korea1", 1}, {"KR", 9}},
	"KSCms-UHC-HW-V":   {{"Korea1", 1}, {"Korea1", 1}, {"KR", 9}},
	"KSCms-UHC-V":      {{"Korea1", 1}, {"Korea1", 1}, {"KR", 9}},
	"KSCpc-EUC-H":      {{"Korea1", 0}, {"Korea1", 0}, {"KR", 9}},
	"UniCNS-UCS2-H":    {{"CNS1", 3}, {"CNS1", 3}, {"CNS1", 7}},
	"UniCNS-UCS2-V":    {{"CNS1", 3}, {"CNS1", 3}, {"CNS1", 7}},
	"UniCNS-UTF16-H":   {{"CNS1", 4}, {"CNS1", 7}},
	"UniCNS-UTF16-V":   {{"CNS1", 4}, {"CNS1", 7}},
	"UniGB-UCS2-H":     {{"GB1", 4}, {"GB1", 4}, {"GB1", 5}},
	"UniGB-UCS2-V":     {{"GB1", 4}, {"GB1", 4}, {"GB1", 5}},
	"UniGB-UTF16-H":    {{"GB1", 4}, {"GB1", 5}},
	"UniGB-UTF16-V":    {{"GB1", 4}, {"GB1", 5}},
	"UniJIS-UCS2-H":    {{"Japan1", 4}, {"Japan1", 4}, {"Japan1", 7}},
	"UniJIS-UCS2-HW-H": {{"Japan1", 4}, {"Japan1", 4}, {"Japan1", 7}},
	"UniJIS-UCS2-HW-V": {{"Japan1", 4}, {"Japan1", 4}, {"Japan1", 7}},
	"UniJIS-UCS2-V":    {{"Japan1", 4}, {"Japan1", 4}, {"Japan1", 7}},
	"UniJIS-UTF16-H":   {{"Japan1", 5}, {"Japan1", 7}},
	"UniJIS-UTF16-V":   {{"Japan1", 5}, {"Japan1", 7}},
	"UniKS-UCS2-H":     {{"Korea1", 1}, {"Korea1", 1}, {"KR", 9}},
	"UniKS-UCS2-V":     {{"Korea1", 1}, {"Korea1", 1}, {"KR", 9}},
	"UniKS-UTF16-H":    {{"Korea1", 2}, {"KR", 9}},
	"UniKS-UTF16-V":    {{"Korea1", 2}, {"KR", 9}},
	"V":                {{"Japan1", 1}, {"Japan1", 1}, {"Japan1", 7}},
}

// PredefinedCMapCIDSystemInfo returns the CIDSystemInfo (Registry, Ordering,
// Supplement) that a predefined CMap named per ISO 32000-1 Table 118 is treated
// as declaring, for the purpose of the Type 0 font CIDSystemInfo compatibility
// check (ISO 19005-2 §6.2.11.3.1). Registry is always "Adobe". pdf20 selects the
// ISO 32000-2 generation of the table (PDF/A-4) versus the ISO 32000-1 era
// (PDF/A-1/2/3), matching veraPDF's flavour-dependent CharacterCollections
// lookup. ok is false when name is not a predefined encoding CMap (including the
// Identity CMaps, whose CIDSystemInfo the caller handles separately, and
// CIDSystemInfo-shaped names such as "Adobe-Japan1-2" that are not CMap names).
func PredefinedCMapCIDSystemInfo(name string, pdf20 bool) (registry, ordering string, supplement int, ok bool) {
	// Identity-H/V are exempt from the CIDSystemInfo compatibility rule and are
	// resolved by the caller before this lookup, so do not report them here even
	// though veraPDF's table lists them.
	if name == "Identity-H" || name == "Identity-V" {
		return "", "", 0, false
	}
	entries, found := predefinedCMapCIDSystemInfo[name]
	if !found || len(entries) == 0 {
		return "", "", 0, false
	}
	idx := 0
	if pdf20 {
		idx = len(entries) - 1
	}
	e := entries[idx]
	return "Adobe", e.ordering, e.supplement, true
}
