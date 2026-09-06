// Copyright (c) 2026, the go-macos authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file.

package appbundle

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// built assembles a bundle around a program whose bytes are what the caller
// says, so that two bundles can be made to differ in the only way that matters
// here: their executable.
func built(t *testing.T, id string, program []byte) Bundle {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "prog")
	if err := writeFile(exe, program, 0o755); err != nil {
		t.Fatalf("write the program: %v", err)
	}
	b, err := Build(Spec{Dir: t.TempDir(), Name: "Thing", Identifier: id, Executable: exe})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return b
}

// aProgram is a shell script, because codesign will sign one and it can be made
// to differ by a byte without a compiler.
func aProgram(tail string) []byte { return []byte("#!/bin/sh\nexit 0\n# " + tail + "\n") }

func needCodesign(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("codesign is a macOS program")
	}
	if _, err := exec.LookPath("codesign"); err != nil {
		t.Skip("no codesign on this machine")
	}
}

// ⛔ THE FACT THE WHOLE FILE EXISTS FOR. An ad-hoc signature's designated
// requirement is the hash of the binary, so two builds that differ at all are
// two different applications to macOS -- and every privacy permission granted
// to one of them means nothing for the other.
func TestAdHocMakesADifferentApplicationOutOfEveryBuild(t *testing.T) {
	needCodesign(t)
	first := built(t, "io.github.go-macos.thing", aProgram("first"))
	second := built(t, "io.github.go-macos.thing", aProgram("second"))
	for _, b := range []Bundle{first, second} {
		if err := b.Sign(Signer{Identity: "-"}); err != nil {
			t.Fatalf("sign ad hoc: %v", err)
		}
	}
	a, err := first.DesignatedRequirement()
	if err != nil {
		t.Fatalf("read the first requirement: %v", err)
	}
	c, err := second.DesignatedRequirement()
	if err != nil {
		t.Fatalf("read the second requirement: %v", err)
	}
	if !strings.Contains(a, "cdhash") {
		t.Errorf("an ad-hoc requirement should be a cdhash, got %q", a)
	}
	if a == c {
		t.Errorf("two different programs signed ad hoc share a requirement %q;\n"+
			"if that is now true, an ad-hoc build keeps its permissions and the "+
			"reason to sign with a certificate is gone", a)
	}
	// And the identifier, which the caller went to the trouble of setting, is
	// not in it at all. That is why it cannot survive: nothing in the rule says
	// which program this is, only which bytes.
	if strings.Contains(a, "io.github.go-macos.thing") {
		t.Errorf("an ad-hoc requirement names the identifier now: %q", a)
	}
}

// A certificate is what makes the requirement outlive a rebuild. There is no
// signing certificate on a CI runner, so this runs where one has been made and
// skips where none has -- the fact it establishes was measured by hand on a
// machine that has one:
//
//	identifier "io.github.go-xrkit.xrdesk" and certificate root = H"63463ed1..."
//
// identical for two genuinely different binaries.
func TestACertificateMakesTheRequirementSurviveARebuild(t *testing.T) {
	needCodesign(t)
	identity := signingIdentity(t)
	first := built(t, "io.github.go-macos.thing", aProgram("first"))
	second := built(t, "io.github.go-macos.thing", aProgram("second"))
	for _, b := range []Bundle{first, second} {
		if err := b.Sign(Signer{Identity: identity}); err != nil {
			t.Fatalf("sign with %q: %v", identity, err)
		}
	}
	a, err := first.DesignatedRequirement()
	if err != nil {
		t.Fatalf("read the first requirement: %v", err)
	}
	c, err := second.DesignatedRequirement()
	if err != nil {
		t.Fatalf("read the second requirement: %v", err)
	}
	if a != c {
		t.Errorf("two builds signed with the same certificate disagree:\n %q\n %q", a, c)
	}
	if !strings.Contains(a, `identifier "io.github.go-macos.thing"`) {
		t.Errorf("the requirement should name the bundle, got %q", a)
	}
}

// signingIdentity finds any signing certificate on this machine, or skips.
//
// It takes whichever one is there rather than a name written down here,
// because the certificate is a local, per-machine thing: a name that works on
// one Mac is an unexplained skip on every other.
func signingIdentity(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("security", "find-identity", "-p", "codesigning").Output()
	if err != nil {
		t.Skip("cannot ask for signing identities")
	}
	// Lines look like:  1) DEADBEEF... "Some Name" (CSSMERR_TP_NOT_TRUSTED)
	// The untrusted note is expected for a self-signed certificate and does not
	// stop it signing, so it is not a reason to pass this one over.
	for line := range strings.Lines(string(out)) {
		_, after, ok := strings.Cut(line, `"`)
		if !ok {
			continue
		}
		name, _, ok := strings.Cut(after, `"`)
		if ok && name != "" {
			return name
		}
	}
	t.Skip("no signing certificate on this machine")
	return ""
}

func TestSigningWithNoIdentityIsRefusedRatherThanGuessed(t *testing.T) {
	b := Bundle{Path: t.TempDir(), Name: "Thing"}
	err := b.Sign(Signer{})
	if err == nil {
		t.Fatal("signing with no identity was accepted")
	}
	if !strings.Contains(err.Error(), "ad hoc") {
		t.Errorf("the refusal should say what to pass instead, got %q", err)
	}
}

func TestTheIdentifierComesFromTheBundleWhenNoneIsGiven(t *testing.T) {
	b := built(t, "io.github.go-macos.thing", aProgram(""))
	got, ok := b.identifier()
	if !ok {
		t.Fatal("the assembled bundle has no identifier")
	}
	if got != "io.github.go-macos.thing" {
		t.Errorf("identifier = %q", got)
	}
}

// An identifier with an ampersand in it is unlikely and legal, and the plist it
// was written into escaped it. Reading it back has to undo exactly that, or the
// signature would be made out to a name the bundle does not have.
func TestTheIdentifierIsReadBackUnescaped(t *testing.T) {
	for _, id := range []string{
		`io.github.go-macos.bed&breakfast`,
		`io.github.go-macos.<angle>`,
		`io.github.go-macos.q"uote`,
		`io.github.go-macos.&amp;lt;`,
	} {
		b := built(t, id, aProgram(""))
		got, ok := b.identifier()
		if !ok {
			t.Fatalf("%q: no identifier", id)
		}
		if got != id {
			t.Errorf("identifier = %q, want %q", got, id)
		}
	}
}

func TestABundleWithNoInfoPlistHasNoIdentifier(t *testing.T) {
	b := Bundle{Path: t.TempDir(), Name: "Thing"}
	if _, ok := b.identifier(); ok {
		t.Error("a directory that is not a bundle reported an identifier")
	}
}
