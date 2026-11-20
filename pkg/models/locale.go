package models

// Locale is a locale a conversion formats spreadsheet values under, as a
// BCP 47 tag: one of the Locale constants, each a locale LibreOffice
// ships, such as [LocaleEnUS], [LocaleFrFR] or [LocaleSrLatnRS]. The zero
// Locale means the default, en-US.
type Locale string
