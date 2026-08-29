// Copyright (c) 2026, the go-macos authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file.

package appbundle

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/png"
	"sort"
)

// iconTypes is the four-character code macOS reads each size under. A .icns is
// a container of complete PNGs, one per size, each announced by the code for
// the size it is: put a 256-pixel image under the code for 512 and the system
// believes the code, then draws it wrong.
var iconTypes = map[int]string{
	16:   "icp4",
	32:   "icp5",
	64:   "icp6",
	128:  "ic07",
	256:  "ic08",
	512:  "ic09",
	1024: "ic10",
}

// ICNS packs PNGs into the icon file macOS reads, replacing a shell out to
// iconutil and sips.
//
// Each image goes in whole, under the code for its own size, so the caller
// chooses which sizes to ship: the system picks the nearest it has, and one
// good large image beats seven resampled from it.
func ICNS(images ...[]byte) ([]byte, error) {
	if len(images) == 0 {
		return nil, fmt.Errorf("appbundle: an icon with no images in it")
	}
	type entry struct {
		code string
		data []byte
		size int
	}
	var entries []entry
	seen := map[int]bool{}
	for i, img := range images {
		cfg, err := png.DecodeConfig(bytes.NewReader(img))
		if err != nil {
			return nil, fmt.Errorf("appbundle: image %d is not a PNG: %w", i, err)
		}
		if cfg.Width != cfg.Height {
			return nil, fmt.Errorf("appbundle: image %d is %dx%d, and an icon is square",
				i, cfg.Width, cfg.Height)
		}
		code, ok := iconTypes[cfg.Width]
		if !ok {
			return nil, fmt.Errorf("appbundle: %dx%d is not a size macOS reads", cfg.Width, cfg.Width)
		}
		if seen[cfg.Width] {
			return nil, fmt.Errorf("appbundle: two images of %dx%d, and only one can be read",
				cfg.Width, cfg.Width)
		}
		seen[cfg.Width] = true
		entries = append(entries, entry{code: code, data: img, size: cfg.Width})
	}
	// Smallest first, which is the order the tools write and the order
	// anybody comparing two files by eye will expect.
	sort.Slice(entries, func(i, j int) bool { return entries[i].size < entries[j].size })

	body := &bytes.Buffer{}
	for _, e := range entries {
		body.WriteString(e.code)
		// The length a chunk announces includes the eight bytes announcing
		// it, which is the one thing a reader of this format gets wrong.
		_ = binary.Write(body, binary.BigEndian, uint32(len(e.data)+8))
		body.Write(e.data)
	}
	out := &bytes.Buffer{}
	out.WriteString("icns")
	_ = binary.Write(out, binary.BigEndian, uint32(body.Len()+8))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}
