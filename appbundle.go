// Copyright (c) 2026, the go-macos authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file.

// Package appbundle is the .app directory a macOS program lives in: whether
// the running process is inside one, and how to assemble one around an
// executable.
//
// A bare executable is not an application on this system. AppKit reads what a
// program is from the bundle around it, so a program that wants a menu-bar
// item, a dock tile, a name in the menu bar, notification permission or a
// place in Login Items has to be in one. Asked for from outside a bundle, a
// status item is asked for by nobody: it never appears, and the process ends
// without complaining.
//
// Everything here is path and file work — no AppKit, no cgo — so it builds and
// is tested on every platform, which is what makes a bundle assembler useful
// in a cross-compiling build.
package appbundle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Bundle is a .app directory.
type Bundle struct {
	// Path is the .app directory itself.
	Path string
	// Name is what it is called, without the .app.
	Name string
}

// suffix is the layout every bundle has: the executable sits three levels
// down, and that shape is what identifies one from a path alone.
const suffix = ".app" + string(filepath.Separator) + "Contents" + string(filepath.Separator) + "MacOS"

// Of reports the bundle an executable path sits in, if it sits in one.
//
// It is a question about a path, not about the machine, so it answers the same
// way everywhere: a build on another platform can reason about a bundle it is
// assembling.
func Of(exePath string) (Bundle, bool) {
	dir := filepath.Dir(filepath.Clean(exePath))
	if !strings.HasSuffix(dir, suffix) {
		return Bundle{}, false
	}
	// Whatever carries the suffix has the segments above it, so there is
	// always something left to name: no guard here for an empty root.
	app := filepath.Dir(filepath.Dir(dir))
	return Bundle{Path: app, Name: strings.TrimSuffix(filepath.Base(app), ".app")}, true
}

// executable is a seam: the one call here that can fail for reasons of the
// machine rather than of the path.
var (
	executable = os.Executable
	mkdirAll   = os.MkdirAll
	writeFile  = os.WriteFile
)

// Running reports the bundle this process is in, if it is in one.
func Running() (Bundle, bool) {
	exe, err := executable()
	if err != nil {
		return Bundle{}, false
	}
	return Of(exe)
}

// Spec is what to assemble.
type Spec struct {
	// Dir is where the .app is written.
	Dir string
	// Name is the application's name, and the .app directory's.
	Name string
	// Identifier is the bundle identifier, in reverse-DNS form.
	Identifier string
	// Version is what the application reports as its version.
	Version string
	// Executable is the built program to put inside. It is copied, so the
	// caller keeps whatever it built.
	Executable string
	// Accessory asks for LSUIElement: a program with a menu-bar item and no
	// dock tile and no menu of its own, which is what a status-item
	// application is.
	Accessory bool
	// MinimumSystem is the oldest macOS this claims to run on. Empty leaves
	// the key out rather than inventing a floor.
	MinimumSystem string
}

// Build assembles the bundle and reports where it put it.
//
// It replaces whatever was there: a bundle assembled over the top of an older
// one keeps files nothing refers to any more, and those are the ones that go
// stale without anybody noticing.
func Build(s Spec) (Bundle, error) {
	switch {
	case s.Name == "":
		return Bundle{}, errors.New("appbundle: an application with no name")
	case s.Identifier == "":
		return Bundle{}, errors.New("appbundle: an application with no identifier")
	case s.Executable == "":
		return Bundle{}, errors.New("appbundle: an application with nothing to run")
	}
	exe, err := os.ReadFile(s.Executable)
	if err != nil {
		return Bundle{}, fmt.Errorf("appbundle: read %s: %w", s.Executable, err)
	}

	app := filepath.Join(s.Dir, s.Name+".app")
	if err := os.RemoveAll(app); err != nil {
		return Bundle{}, fmt.Errorf("appbundle: clear %s: %w", app, err)
	}
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := mkdirAll(macos, 0o755); err != nil {
		return Bundle{}, fmt.Errorf("appbundle: create %s: %w", macos, err)
	}
	if err := writeFile(filepath.Join(macos, s.Name), exe, 0o755); err != nil {
		return Bundle{}, fmt.Errorf("appbundle: write the executable: %w", err)
	}
	if err := writeFile(filepath.Join(app, "Contents", "Info.plist"), []byte(s.plist()), 0o644); err != nil {
		return Bundle{}, fmt.Errorf("appbundle: write Info.plist: %w", err)
	}
	return Bundle{Path: app, Name: s.Name}, nil
}

// plist is what the application claims to be.
func (s Spec) plist() string {
	version := s.Version
	if version == "" {
		version = "0.0.0"
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	pair := func(k, v string) {
		fmt.Fprintf(&b, "\t<key>%s</key>\n\t<string>%s</string>\n", k, v)
	}
	pair("CFBundleName", s.Name)
	pair("CFBundleDisplayName", s.Name)
	pair("CFBundleExecutable", s.Name)
	pair("CFBundleIdentifier", s.Identifier)
	pair("CFBundlePackageType", "APPL")
	pair("CFBundleInfoDictionaryVersion", "6.0")
	pair("CFBundleShortVersionString", version)
	pair("CFBundleVersion", version)
	if s.MinimumSystem != "" {
		pair("LSMinimumSystemVersion", s.MinimumSystem)
	}
	if s.Accessory {
		b.WriteString("\t<key>LSUIElement</key>\n\t<true/>\n")
	}
	b.WriteString("\t<key>NSHighResolutionCapable</key>\n\t<true/>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}
