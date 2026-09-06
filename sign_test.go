// Copyright (c) 2026, the go-macos authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file.

package appbundle

import (
	"os"
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

// standIn puts a program in place of codesign that says what the test wants it
// to say, so that the signing step is exercised where codesign does not exist
// -- which is every machine the coverage gate runs on.
func standIn(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no shell to write a stand-in with")
	}
	path := filepath.Join(t.TempDir(), "codesign")
	if err := writeFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("write the stand-in: %v", err)
	}
	was := codesignProgram
	codesignProgram = path
	t.Cleanup(func() { codesignProgram = was })
}

func TestWhatIsAskedOfCodesign(t *testing.T) {
	said := filepath.Join(t.TempDir(), "argv")
	standIn(t, `printf '%s\n' "$@" > `+said)
	b := built(t, "io.github.go-macos.thing", aProgram(""))
	if err := b.Sign(Signer{Identity: "Some Name", Entitlements: "/tmp/ent.plist"}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := os.ReadFile(said)
	if err != nil {
		t.Fatalf("read what was asked: %v", err)
	}
	want := "--force\n--sign\nSome Name\n--identifier\nio.github.go-macos.thing\n--entitlements\n/tmp/ent.plist\n" + b.Path + "\n"
	if string(got) != want {
		t.Errorf("codesign was asked\n%q\nwant\n%q", got, want)
	}
}

func TestAnIdentifierGivenBeatsTheBundlesOwn(t *testing.T) {
	said := filepath.Join(t.TempDir(), "argv")
	standIn(t, `printf '%s\n' "$@" > `+said)
	b := built(t, "io.github.go-macos.thing", aProgram(""))
	if err := b.Sign(Signer{Identity: "-", Identifier: "io.github.go-macos.other"}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, _ := os.ReadFile(said)
	if !strings.Contains(string(got), "io.github.go-macos.other") {
		t.Errorf("the given identifier was not used: %q", got)
	}
	if strings.Contains(string(got), "io.github.go-macos.thing") {
		t.Errorf("the bundle's own identifier was used as well: %q", got)
	}
}

// ⛔ codesign says what is wrong on stderr and nothing in the exit status, so
// dropping its output leaves "exit status 1" and a person with nothing to go on.
func TestWhatCodesignComplainedAboutIsKept(t *testing.T) {
	standIn(t, `echo "Warning: unable to build chain" >&2; exit 1`)
	b := built(t, "io.github.go-macos.thing", aProgram(""))
	err := b.Sign(Signer{Identity: "Nobody"})
	if err == nil {
		t.Fatal("a failing codesign was reported as success")
	}
	if !strings.Contains(err.Error(), "unable to build chain") {
		t.Errorf("the complaint was dropped: %q", err)
	}
}

func TestAFailureWithNothingToSayIsStillAFailure(t *testing.T) {
	standIn(t, `exit 3`)
	b := built(t, "io.github.go-macos.thing", aProgram(""))
	if err := b.Sign(Signer{Identity: "Nobody"}); err == nil {
		t.Fatal("a silent failure was reported as success")
	}
	if _, err := b.DesignatedRequirement(); err == nil {
		t.Fatal("a silent failure was reported as a requirement")
	}
}

func TestSigningABundleThatSaysNothingAboutItselfIsRefused(t *testing.T) {
	standIn(t, `exit 0`)
	b := Bundle{Path: t.TempDir(), Name: "Thing"}
	err := b.Sign(Signer{Identity: "-"})
	if err == nil {
		t.Fatal("a bundle with no Info.plist was signed under a guessed name")
	}
	if !strings.Contains(err.Error(), "Info.plist") {
		t.Errorf("the refusal should say where the name was looked for, got %q", err)
	}
}

func TestTheRequirementIsFoundHoweverCodesignPrefacesIt(t *testing.T) {
	const want = `identifier "io.github.go-macos.thing" and certificate root = H"abc"`
	for _, preface := range []string{"designated => ", "# designated => "} {
		standIn(t, `echo 'Executable=/somewhere' >&2; echo '`+preface+want+`'`)
		b := Bundle{Path: t.TempDir(), Name: "Thing"}
		got, err := b.DesignatedRequirement()
		if err != nil {
			t.Fatalf("%q: %v", preface, err)
		}
		if got != want {
			t.Errorf("%q: requirement = %q", preface, got)
		}
	}
}

func TestASignatureWithNoRequirementIsSaidToHaveNone(t *testing.T) {
	standIn(t, `echo 'Executable=/somewhere'`)
	b := Bundle{Path: t.TempDir(), Name: "Thing"}
	if _, err := b.DesignatedRequirement(); err == nil {
		t.Fatal("output with no requirement in it yielded one")
	}
}

func TestTheComplaintFromReadingARequirementIsKept(t *testing.T) {
	standIn(t, `echo "code object is not signed at all" >&2; exit 1`)
	b := Bundle{Path: t.TempDir(), Name: "Thing"}
	_, err := b.DesignatedRequirement()
	if err == nil {
		t.Fatal("an unsigned bundle reported a requirement")
	}
	if !strings.Contains(err.Error(), "not signed at all") {
		t.Errorf("the complaint was dropped: %q", err)
	}
}

// A plist that stops in the middle names nothing, and the half-read name must
// not become what the signature is made out to.
func TestAnInfoPlistCutShortNamesNothing(t *testing.T) {
	for _, plist := range []string{
		`<plist><dict><key>CFBundleName</key><string>Thing</string></dict></plist>`,
		`<plist><dict><key>CFBundleIdentifier</key>`,
		`<plist><dict><key>CFBundleIdentifier</key><string>io.github.go-macos.thing`,
	} {
		b := built(t, "io.github.go-macos.thing", aProgram(""))
		if err := writeFile(filepath.Join(b.Path, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
			t.Fatalf("rewrite Info.plist: %v", err)
		}
		if id, ok := b.identifier(); ok {
			t.Errorf("%q yielded the identifier %q", plist, id)
		}
	}
}
