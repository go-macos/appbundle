// Copyright (c) 2026, the go-macos authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file.

package appbundle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Signer is how a bundle is code-signed, and the reason to bother is that a
// privacy permission is granted to a CODE IDENTITY rather than to a path.
//
// ⛔ AN AD-HOC SIGNATURE IS A NEW APPLICATION EVERY TIME THE SOURCE CHANGES.
// A Go binary comes out of the linker signed ad hoc, and an ad-hoc signature's
// designated requirement is the hash of the binary itself:
//
//	designated => cdhash H"f310919066069e55c6164a1d02e4fc0fc7f11d43"
//
// The bundle identifier is not even in it. So every grant the person made --
// screen recording, the camera, the microphone, Accessibility -- is attached to
// that one build and is gone the moment anything is recompiled. The program
// then fails at exactly the place it worked yesterday, and the failure looks
// like a bug in the program rather than a permission that quietly stopped
// applying. Rebuilding the identical source is fine, because Go builds
// reproducibly; it is CHANGING it that costs the grant, which is to say every
// build that was worth making.
//
// Signed with a certificate the requirement names the identifier and the
// certificate instead, and is the same for every build there will ever be:
//
//	designated => identifier "io.github.example.app" and certificate root = H"63463ed1..."
//
// The certificate does not have to be Apple's. A self-signed one made locally
// is untrusted for Gatekeeper and entirely sufficient here, because what is
// being asked of it is not "did Apple vouch for this" but "is this the same
// program the person said yes to".
type Signer struct {
	// Identity names the signing certificate, the way `security find-identity
	// -p codesigning` prints it -- a name like "Acme Dev", or its SHA-1.
	//
	// "-" signs ad hoc, which is what the linker already did, and which does
	// NOT give a stable identity. It is here because it is occasionally what
	// somebody wants, not because it is a lesser version of this.
	Identity string
	// Identifier is what the signature calls the program, and it belongs in the
	// designated requirement. Empty takes the bundle's own identifier, which is
	// nearly always right; codesign left to itself would take the executable's
	// file name, and for a Go build that is whatever the linker chose.
	Identifier string
	// Entitlements is a plist file granting the entitlements this program
	// claims. Empty asks for none, which is right until something needs one.
	Entitlements string
}

// Sign code-signs an assembled bundle in place.
//
// It runs codesign, so it works on macOS and nowhere else -- unlike Build,
// which is file work and assembles a bundle from any platform that can
// cross-compile for this one. Signing is therefore a separate step rather than
// a field on Spec: a Linux builder can produce the bundle, and only a Mac can
// finish it.
func (b Bundle) Sign(s Signer) error {
	if s.Identity == "" {
		return errors.New("appbundle: sign with no identity: name a certificate, or \"-\" for ad hoc")
	}
	id := s.Identifier
	if id == "" {
		var ok bool
		if id, ok = b.identifier(); !ok {
			return errors.New("appbundle: sign: no identifier given and none in Info.plist")
		}
	}
	args := []string{"--force", "--sign", s.Identity, "--identifier", id}
	if s.Entitlements != "" {
		args = append(args, "--entitlements", s.Entitlements)
	}
	args = append(args, b.Path)

	out, err := exec.Command(codesignProgram, args...).CombinedOutput()
	if err != nil {
		// codesign says what is wrong on stderr and says nothing useful in the
		// exit status, so the output is the error.
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("appbundle: codesign %s: %w: %s", b.Path, err, msg)
		}
		return fmt.Errorf("appbundle: codesign %s: %w", b.Path, err)
	}
	return nil
}

// DesignatedRequirement is the rule macOS uses to decide whether a program is
// still the one a permission was granted to.
//
// It is worth looking at rather than trusting, because the two shapes it comes
// in differ in whether the grant survives a rebuild, and they are told apart by
// reading them: one names a cdhash, the other an identifier and a certificate.
func (b Bundle) DesignatedRequirement() (string, error) {
	out, err := exec.Command(codesignProgram, "-d", "-r-", b.Path).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return "", fmt.Errorf("appbundle: read the requirement of %s: %w: %s", b.Path, err, msg)
		}
		return "", fmt.Errorf("appbundle: read the requirement of %s: %w", b.Path, err)
	}
	// codesign writes the requirement among its other remarks, prefixed either
	// "designated =>" or "# designated =>" depending on how it feels.
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "# ")
		if after, ok := strings.CutPrefix(line, "designated => "); ok {
			return after, nil
		}
	}
	return "", fmt.Errorf("appbundle: %s has a signature with no designated requirement", b.Path)
}

// identifier is what the assembled bundle says it is called.
//
// Read back from Info.plist rather than remembered, because a Bundle can also
// come from Of or Running -- neither of which assembled anything -- and because
// a remembered copy is a second answer that can disagree with the file.
func (b Bundle) identifier() (string, bool) {
	plist, err := os.ReadFile(filepath.Join(b.Path, "Contents", "Info.plist"))
	if err != nil {
		return "", false
	}
	_, after, ok := strings.Cut(string(plist), "<key>CFBundleIdentifier</key>")
	if !ok {
		return "", false
	}
	_, after, ok = strings.Cut(after, "<string>")
	if !ok {
		return "", false
	}
	id, _, ok := strings.Cut(after, "</string>")
	if !ok {
		return "", false
	}
	return plistUnescape(strings.TrimSpace(id)), true
}

// plistUnescape undoes plistEscape, and only that: the five predefined
// entities, because those are the only ones written.
//
// A Replacer makes one pass and never looks at what it wrote, which is what
// keeps "&amp;lt;" -- an escaped ampersand followed by text -- from being
// unescaped twice into "<".
func plistUnescape(s string) string {
	return strings.NewReplacer(
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&apos;", "'",
		"&amp;", "&",
	).Replace(s)
}

// codesignProgram is the signing tool, named rather than spelled out at the
// two places it is run.
//
// It is a variable because the tool exists on exactly one operating system,
// and everything here has to be exercised on the others too: a signing step
// that is only ever tested where it happens to work is a signing step whose
// error handling nobody has ever run.
var codesignProgram = "codesign"
