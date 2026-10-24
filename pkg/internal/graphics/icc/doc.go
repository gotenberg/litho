// Package icc reads ICC colour profiles (ICC.1:2010, and version 2 profiles)
// and converts colours with them.
//
// A [Transform] converts colours of a profile's grey, RGB or CMYK data colour
// space to sRGB, through the profile's lookup tables or its matrix and tone
// curves, with the profile's black point mapped onto sRGB's under every
// intent but absolute colorimetric, unless the graphics state's
// /UseBlackPtComp is /OFF ([BPC]): the reading image decoding and rendering
// use.
//
// A [Link] converts colours from one profile's data colour space to another's
// through the profile connection space, or through a DeviceLink profile: the
// AToB and BToA tables of every table type, inverted matrix and tone curve
// output, the four rendering intents, black point compensation, and Lab and
// n-colour data spaces where the tables allow.
package icc
