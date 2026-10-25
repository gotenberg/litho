package icc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// FuzzLink feeds arbitrary data to the profile reader and connects what it
// reads to the sRGB profile both ways, and as a DeviceLink, under every
// intent with and without black point compensation, converting colours
// through each link it builds. The contract is crash-freedom: every input
// either converts or returns an error carrying a sentinel.
func FuzzLink(f *testing.F) {
	seeds := [][]byte{rgbProfile(), greyProfile(), cmykProfile(), extendedProfile(), deviceLinkProfile(), sixColourProfile(), v4RGBDisplayProfile(), CMYKPrinterProfile()}
	for _, name := range []string{"v4-cmyk-lut.icc", "v4-rgb-lut-xyz.icc"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatal(err)
		}
		seeds = append(seeds, data)
	}
	for _, p := range seeds {
		f.Add(p)
		f.Add(p[:len(p)/2])
	}
	srgb, err := Parse(SRGBProfile())
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Parse(data)
		if err != nil {
			return
		}
		convert := func(l *Link, err error) {
			if err != nil {
				if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrLimit) {
					t.Fatalf("error without a sentinel: %v", err)
				}
				return
			}
			in, out := make([]float64, l.InputChannels()), make([]float64, l.OutputChannels())
			for v := range 3 {
				for i := range in {
					in[i] = float64(v) / 2
				}
				l.Convert(in, out)
			}
		}
		convert(NewLink(p, nil, TransformOptions{}))
		for intent := range Intent(4) {
			for _, bpc := range []bool{false, true} {
				opts := TransformOptions{Intent: intent, BlackPointCompensation: bpc}
				convert(NewLink(p, srgb, opts))
				convert(NewLink(srgb, p, opts))
			}
		}
	})
}

// FuzzProfile feeds arbitrary data to the profile reader and, when it reads
// and builds a transform, under every intent and black point compensation
// choice, converts colours through it to sRGB and to CIELAB, and asks whether
// it is neutral. The contract is crash-freedom: every input either converts
// or returns an error carrying a sentinel.
func FuzzProfile(f *testing.F) {
	for _, p := range [][]byte{rgbProfile(), greyProfile(), cmykProfile(), extendedProfile()} {
		f.Add(p)
		f.Add(p[:len(p)/2])
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		sentinel := func(err error) {
			if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrLimit) {
				t.Fatalf("error without a sentinel: %v", err)
			}
		}
		p, err := Parse(data)
		if err != nil {
			sentinel(err)
			return
		}
		for intent := range Intent(4) {
			// BPCOn builds what BPCDefault builds.
			for _, bpc := range []BPC{BPCDefault, BPCOff} {
				tr, err := p.NewTransform(intent.WithBPC(bpc))
				if err != nil {
					sentinel(err)
					continue
				}
				in := make([]byte, tr.Components())
				for v := range 4 {
					for i := range in {
						in[i] = byte(v * 85)
					}
					tr.Convert(in)
					tr.Lab(in)
				}
				tr.Neutral()
			}
		}
	})
}

// FuzzToV2 feeds arbitrary data to the version 2 re-encoder. The contract is
// crash-freedom and a well-formed result: every input either converts to a
// profile [Parse] reads as version 2, or returns an error carrying a sentinel.
func FuzzToV2(f *testing.F) {
	for _, p := range [][]byte{sGrayV4(), rgbProfile(), extendedProfile()} {
		f.Add(p)
		f.Add(p[:len(p)/2])
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := ToV2(data)
		if err != nil {
			if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrLimit) {
				t.Fatalf("error without a sentinel: %v", err)
			}
			return
		}
		p, err := Parse(out)
		if err != nil {
			t.Fatalf("converted profile does not parse: %v", err)
		}
		if p.Version != 2 {
			t.Fatalf("converted profile is version %d", p.Version)
		}
	})
}
