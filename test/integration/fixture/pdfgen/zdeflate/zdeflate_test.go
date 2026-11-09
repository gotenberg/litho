package zdeflate

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The input generators mirror the Python script that wrote
// testdata/vectors.txt (lines: name level length sha256), so only hashes of
// Python's zlib.compress output are stored.

func rnd(n int, seed uint32) []byte {
	x := seed
	out := make([]byte, n)
	for i := range out {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		out[i] = byte(x >> 24)
	}
	return out
}

func lowent(n int, seed uint32) []byte {
	out := rnd(n, seed)
	for i, b := range out {
		out[i] = 97 + b&3
	}
	return out
}

var loremText = []byte("Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod " +
	"tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, " +
	"quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat.\n")

func mixed() []byte {
	var out []byte
	for i := range 40 {
		out = append(out, bytes.Repeat(loremText, i%5+1)...)
		out = append(out, rnd(1000+137*i, uint32(i+1))...)
	}
	return out
}

func objs(n int) []byte {
	var out []byte
	for i := 0; len(out) < n; i++ {
		out = fmt.Appendf(out, "%d 0 obj\n<< /Type /Page /N %d /MediaBox [0 0 612 792] >>\nendobj\n", i, i*7%13)
	}
	return out[:n]
}

func input(name string) ([]byte, bool) {
	switch name {
	case "empty":
		return []byte{}, true
	case "one":
		return []byte("a"), true
	case "twelve":
		return []byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0}, true
	case "text":
		return loremText, true
	case "fox":
		return bytes.Repeat([]byte("The quick brown fox "), 5000), true
	case "zeros100k":
		return make([]byte, 100000), true
	case "mixed":
		return mixed(), true
	case "objs300k":
		return objs(300000), true
	case "lowent200k":
		return lowent(200000, 99), true
	}
	if v, ok := strings.CutPrefix(name, "rnd"); ok {
		n, err := strconv.Atoi(v)
		return rnd(n, uint32(n)), err == nil
	}
	if v, ok := strings.CutPrefix(name, "objs"); ok {
		n, err := strconv.Atoi(v)
		return objs(n), err == nil
	}
	return nil, false
}

func TestCompressMatchesPythonZlib(t *testing.T) {
	f, err := os.Open("testdata/vectors.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	cache := map[string][]byte{}
	sc := bufio.NewScanner(f)
	count := 0
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 4 {
			t.Fatalf("malformed vector line %q", sc.Text())
		}
		name := fields[0]
		level, err1 := strconv.Atoi(fields[1])
		wantLen, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil {
			t.Fatalf("malformed vector line %q", sc.Text())
		}
		data, ok := cache[name]
		if !ok {
			data, ok = input(name)
			if !ok {
				t.Fatalf("unknown input %q", name)
			}
			cache[name] = data
		}
		got := Compress(data, level)
		sum := sha256.Sum256(got)
		if len(got) != wantLen || hex.EncodeToString(sum[:]) != fields[3] {
			t.Errorf("%s level %d: got %d bytes sha256 %x, want %d bytes sha256 %s",
				name, level, len(got), sum, wantLen, fields[3])
		}

		r, err := zlib.NewReader(bytes.NewReader(got))
		if err != nil {
			t.Errorf("%s level %d: zlib.NewReader: %v", name, level, err)
			continue
		}
		back, err := io.ReadAll(r)
		if err != nil || !bytes.Equal(back, data) {
			t.Errorf("%s level %d: round trip failed: %v", name, level, err)
		}
		count++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("no vectors")
	}
	t.Logf("%d vectors", count)
}

func TestCompressDefaultIsLevel6(t *testing.T) {
	data := objs(5000)
	if !bytes.Equal(CompressDefault(data), Compress(data, 6)) || !bytes.Equal(Compress(data, -1), Compress(data, 6)) {
		t.Fatal("CompressDefault, level -1 and level 6 differ")
	}
}
