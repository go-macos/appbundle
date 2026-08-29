// Copyright (c) 2026, the go-macos authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file.

package appbundle

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// square is a PNG of the given size, so a test can talk about sizes rather
// than about bytes.
func square(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 1, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// read takes the container apart the way macOS does, so the test is about what
// was written rather than about what the writer meant to write.
func read(t *testing.T, icns []byte) map[string][]byte {
	t.Helper()
	if got := string(icns[:4]); got != "icns" {
		t.Fatalf("the file begins %q", got)
	}
	if n := binary.BigEndian.Uint32(icns[4:8]); int(n) != len(icns) {
		t.Fatalf("the file says it is %d bytes and is %d", n, len(icns))
	}
	out := map[string][]byte{}
	for p := 8; p < len(icns); {
		code := string(icns[p : p+4])
		n := int(binary.BigEndian.Uint32(icns[p+4 : p+8]))
		if n < 8 || p+n > len(icns) {
			t.Fatalf("chunk %q says it is %d bytes", code, n)
		}
		out[code] = icns[p+8 : p+n]
		p += n
	}
	return out
}

// TestAnIconHoldsEveryImageWhole covers the container: each PNG goes in
// unchanged, under the code for its own size, and comes back out byte for
// byte. Nothing here resamples anything — that is what the caller chose.
func TestAnIconHoldsEveryImageWhole(t *testing.T) {
	small, large := square(t, 32, 32), square(t, 512, 512)
	// Given large first, to show the order written is not the order given.
	icns, err := ICNS(large, small)
	if err != nil {
		t.Fatal(err)
	}
	chunks := read(t, icns)
	if len(chunks) != 2 {
		t.Fatalf("the icon holds %d images", len(chunks))
	}
	if !bytes.Equal(chunks["icp5"], small) {
		t.Error("the 32-pixel image did not come back whole")
	}
	if !bytes.Equal(chunks["ic09"], large) {
		t.Error("the 512-pixel image did not come back whole")
	}
	// Smallest first, which is what the tools write.
	if bytes.Index(icns, []byte("icp5")) > bytes.Index(icns, []byte("ic09")) {
		t.Error("the images are not in size order")
	}
}

// TestEverySizeMacOSReadsIsOffered covers the mapping. A size under the wrong
// code is not a failure anybody sees: the system believes the code and draws
// the icon wrong.
func TestEverySizeMacOSReadsIsOffered(t *testing.T) {
	for size, want := range iconTypes {
		icns, err := ICNS(square(t, size, size))
		if err != nil {
			t.Fatalf("%d: %v", size, err)
		}
		if _, ok := read(t, icns)[want]; !ok {
			t.Errorf("%dx%d did not go in as %q", size, size, want)
		}
	}
}

// TestAnIconThatCannotBeMadeSaysWhy covers everything refused. Each of these
// would produce a file macOS reads and draws wrongly rather than one it
// rejects, which is why they are refused here instead.
func TestAnIconThatCannotBeMadeSaysWhy(t *testing.T) {
	cases := map[string][][]byte{
		"nothing to put in it": nil,
		"not a PNG at all":     {[]byte("this is not a PNG")},
		"not square":           {square(t, 32, 64)},
		"a size nobody reads":  {square(t, 33, 33)},
		"the same size twice":  {square(t, 32, 32), square(t, 32, 32)},
	}
	for name, images := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ICNS(images...); err == nil {
				t.Error("an icon was made anyway")
			}
		})
	}
}
