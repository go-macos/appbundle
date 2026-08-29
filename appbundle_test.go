// Copyright (c) 2026, the go-macos authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file.

package appbundle

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAPathSaysWhetherItIsInsideAnApplication covers the question a program
// asks about itself. It is a question about a path, so it is answered the same
// way on every platform — which is what lets a build assemble a bundle it
// cannot run.
func TestAPathSaysWhetherItIsInsideAnApplication(t *testing.T) {
	in := filepath.Join("/Applications", "godl.app", "Contents", "MacOS", "godl")
	b, ok := Of(in)
	if !ok {
		t.Fatalf("Of(%q) found no bundle", in)
	}
	if b.Name != "godl" {
		t.Errorf("Name = %q", b.Name)
	}
	if b.Path != filepath.Join("/Applications", "godl.app") {
		t.Errorf("Path = %q", b.Path)
	}

	for _, out := range []string{
		"/usr/local/bin/godl",
		"/Applications/godl.app/Contents/Resources/godl",
		"/Applications/godl.app/godl",
		"godl",
		"",
	} {
		if _, ok := Of(out); ok {
			t.Errorf("Of(%q) claimed a bundle", out)
		}
	}
}

// TestWhatThisProcessIsIn covers the same question asked of the running
// program, including the machine refusing to say where it is.
func TestWhatThisProcessIsIn(t *testing.T) {
	saved := executable
	t.Cleanup(func() { executable = saved })

	executable = func() (string, error) {
		return filepath.Join("/Applications", "godl.app", "Contents", "MacOS", "godl"), nil
	}
	if b, ok := Running(); !ok || b.Name != "godl" {
		t.Errorf("Running = %+v, %v", b, ok)
	}

	// A test binary is not in a bundle, which is the ordinary answer.
	executable = func() (string, error) { return "/usr/local/bin/godl", nil }
	if _, ok := Running(); ok {
		t.Error("a program outside a bundle reported being in one")
	}

	// And a machine that will not say is not a bundle either: better to be
	// a command-line program than to act like an application on a guess.
	executable = func() (string, error) { return "", errors.New("no such thing") }
	if _, ok := Running(); ok {
		t.Error("a program that cannot locate itself reported being in a bundle")
	}
}

// TestAnAssembledBundleHoldsWhatMacOSReads covers the layout and the keys.
// Every one of them is read by the system rather than by us, so a missing one
// is not a failure anybody sees — it is an application that behaves oddly.
func TestAnAssembledBundleHoldsWhatMacOSReads(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "built")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\ntrue\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	b, err := Build(Spec{
		Dir: dir, Name: "godl", Identifier: "io.example.godl",
		Version: "1.2.3", Executable: exe, Accessory: true,
		MinimumSystem: "11.0",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := Of(filepath.Join(b.Path, "Contents", "MacOS", "godl")); !ok {
		t.Error("what was assembled is not recognised as a bundle")
	}

	// The executable is copied, and copied runnable: a bundle whose program
	// is not executable is an application that will not start.
	fi, err := os.Stat(filepath.Join(b.Path, "Contents", "MacOS", "godl"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("the executable went in with mode %v", fi.Mode().Perm())
	}

	raw, err := os.ReadFile(filepath.Join(b.Path, "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<key>CFBundleIdentifier</key>", "io.example.godl",
		"<key>CFBundleExecutable</key>", "godl",
		"<key>CFBundlePackageType</key>", "APPL",
		"<key>CFBundleShortVersionString</key>", "1.2.3",
		"<key>LSMinimumSystemVersion</key>", "11.0",
		"<key>LSUIElement</key>", "<true/>",
		"NSHighResolutionCapable",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("Info.plist does not say %q:\n%s", want, raw)
		}
	}
}

// TestABundleThatIsNotAnAccessoryKeepsItsDockTile covers the default: not
// every bundled program is a menu-bar accessory, and LSUIElement present and
// false is not the same as absent.
func TestABundleThatIsNotAnAccessoryKeepsItsDockTile(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "built")
	if err := os.WriteFile(exe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Build(Spec{Dir: dir, Name: "app", Identifier: "io.example.app", Executable: exe})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(b.Path, "Contents", "Info.plist"))
	if strings.Contains(string(raw), "LSUIElement") {
		t.Error("an ordinary application was made an accessory")
	}
	// A version nobody gave still has to be a version, since the system
	// compares it.
	if !strings.Contains(string(raw), "0.0.0") {
		t.Errorf("no version was written:\n%s", raw)
	}
	if strings.Contains(string(raw), "LSMinimumSystemVersion") {
		t.Error("a floor nobody asked for was invented")
	}
}

// TestABundleThatCannotBeAssembledSaysWhy covers refusing to write something
// half-formed. A bundle missing an identifier is one macOS treats as another
// copy of whatever else has none.
func TestABundleThatCannotBeAssembledSaysWhy(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "built")
	if err := os.WriteFile(exe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	good := Spec{Dir: dir, Name: "app", Identifier: "io.example.app", Executable: exe}

	cases := map[string]func(*Spec){
		"no name":        func(s *Spec) { s.Name = "" },
		"no identifier":  func(s *Spec) { s.Identifier = "" },
		"nothing to run": func(s *Spec) { s.Executable = "" },
		"nothing there":  func(s *Spec) { s.Executable = filepath.Join(dir, "never-built") },
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			s := good
			spoil(&s)
			if _, err := Build(s); err == nil {
				t.Error("a bundle was assembled anyway")
			}
		})
	}

	t.Run("nowhere to put it", func(t *testing.T) {
		s := good
		s.Dir = filepath.Join(exe, "under-a-file")
		if _, err := Build(s); err == nil {
			t.Error("a bundle was assembled under a file")
		}
	})
}

// TestABundleHalfWrittenIsReported covers the three ways the disk can stop an
// assembly partway. None can be arranged on a working machine, and a bundle
// that is half there is worse than one that is not: the system will launch it.
func TestABundleHalfWrittenIsReported(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "built")
	if err := os.WriteFile(exe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Dir: dir, Name: "app", Identifier: "io.example.app", Executable: exe}
	full := errors.New("the disk is full")

	cases := map[string]func(){
		"no directory to put it in": func() {
			mkdirAll = func(string, os.FileMode) error { return full }
		},
		"the executable will not go in": func() {
			writeFile = func(p string, _ []byte, _ os.FileMode) error {
				if strings.HasSuffix(p, "app") {
					return full
				}
				return nil
			}
		},
		"what it claims to be will not go in": func() {
			writeFile = func(p string, _ []byte, _ os.FileMode) error {
				if strings.HasSuffix(p, "Info.plist") {
					return full
				}
				return nil
			}
		},
	}
	for name, stage := range cases {
		t.Run(name, func(t *testing.T) {
			savedMk, savedWr := mkdirAll, writeFile
			t.Cleanup(func() { mkdirAll, writeFile = savedMk, savedWr })
			stage()
			if _, err := Build(spec); err == nil {
				t.Error("a bundle was reported assembled")
			}
		})
	}
}

// TestAnApplicationCarriesItsIconAndItsKind covers the two files nothing reads
// back: PkgInfo, which the Finder has read since before Info.plist existed, and
// the icon, whose absence is an application drawn as a blank page everywhere it
// appears.
func TestAnApplicationCarriesItsIconAndItsKind(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "built")
	if err := os.WriteFile(exe, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	b, err := Build(Spec{
		Dir: dir, Name: "godl", Identifier: "io.example.godl",
		Executable: exe, Icon: []byte("icns bytes"),
	})
	if err != nil {
		t.Fatal(err)
	}
	kind, err := os.ReadFile(filepath.Join(b.Path, "Contents", "PkgInfo"))
	if err != nil || string(kind) != "APPL????" {
		t.Errorf("PkgInfo = %q, %v", kind, err)
	}
	icon, err := os.ReadFile(filepath.Join(b.Path, "Contents", "Resources", "godl.icns"))
	if err != nil || string(icon) != "icns bytes" {
		t.Errorf("the icon = %q, %v", icon, err)
	}
	plist, _ := os.ReadFile(filepath.Join(b.Path, "Contents", "Info.plist"))
	if !strings.Contains(string(plist), "<key>CFBundleIconFile</key>") {
		t.Error("the bundle carries an icon it never mentions")
	}

	// And an application with no icon says nothing about one, rather than
	// naming a file that is not there.
	b2, err := Build(Spec{Dir: dir, Name: "plain", Identifier: "io.example.plain", Executable: exe})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b2.Path, "Contents", "Resources")); !os.IsNotExist(err) {
		t.Error("an application with no icon was given somewhere to keep one")
	}
	plist2, _ := os.ReadFile(filepath.Join(b2.Path, "Contents", "Info.plist"))
	if strings.Contains(string(plist2), "CFBundleIconFile") {
		t.Error("an application with no icon names one anyway")
	}
}

// TestTheLaterFilesCanFailToo covers the writes added after Info.plist. Each
// leaves a bundle the system will still launch, which is what makes a partial
// assembly worth refusing rather than reporting.
func TestTheLaterFilesCanFailToo(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "built")
	if err := os.WriteFile(exe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Dir: dir, Name: "app", Identifier: "io.example.app",
		Executable: exe, Icon: []byte("icns")}
	full := errors.New("the disk is full")

	failOn := func(part string) func() {
		return func() {
			writeFile = func(p string, b []byte, m os.FileMode) error {
				if strings.Contains(p, part) {
					return full
				}
				return os.WriteFile(p, b, m)
			}
		}
	}
	cases := map[string]func(){
		"what kind of thing it is": failOn("PkgInfo"),
		"the icon itself":          failOn(".icns"),
		"somewhere to keep it": func() {
			calls := 0
			mkdirAll = func(p string, m os.FileMode) error {
				calls++
				if calls == 1 {
					return os.MkdirAll(p, m) // Contents/MacOS
				}
				return full // Contents/Resources
			}
		},
	}
	for name, stage := range cases {
		t.Run(name, func(t *testing.T) {
			savedMk, savedWr := mkdirAll, writeFile
			t.Cleanup(func() { mkdirAll, writeFile = savedMk, savedWr })
			stage()
			if _, err := Build(spec); err == nil {
				t.Error("a bundle missing one of its files was reported assembled")
			}
		})
	}
}
