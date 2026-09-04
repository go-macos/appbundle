// Copyright (c) the go-macos authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package appbundle

import (
	"strings"
	"testing"
)

// TestAUsageDescriptionReachesThePlist.
//
// ⛔ It is the difference between a program that can open a camera and one that
// macOS TERMINATES for trying. TCC does not refuse a program with no usage
// description; it ends the process, with a message in the crash log and nothing
// the program can catch.
func TestAUsageDescriptionReachesThePlist(t *testing.T) {
	s := Spec{
		Name: "XR desk", Identifier: "com.example.xrdesk", Executable: "x",
		UsageDescriptions: map[string]string{
			"NSCameraUsageDescription":     "XR desk shows what the glasses see.",
			"NSMicrophoneUsageDescription": "XR desk records sound with a video.",
		},
	}
	p := s.plist()
	for k, v := range s.UsageDescriptions {
		if !strings.Contains(p, "<key>"+k+"</key>") {
			t.Errorf("the plist has no %s", k)
		}
		if !strings.Contains(p, "<string>"+v+"</string>") {
			t.Errorf("the plist does not say %q", v)
		}
	}
}

// TestThePlistIsTheSameEveryTime.
//
// A map has no order, and a bundle whose Info.plist changes between builds for
// no reason is a bundle that looks modified to everything that watches it -- a
// notarisation, a checksum, a backup.
func TestThePlistIsTheSameEveryTime(t *testing.T) {
	s := Spec{
		Name: "X", Identifier: "com.example.x", Executable: "x",
		UsageDescriptions: map[string]string{
			"NSCameraUsageDescription":       "a",
			"NSMicrophoneUsageDescription":   "b",
			"NSPhotoLibraryUsageDescription": "c",
			"NSLocationUsageDescription":     "d",
		},
	}
	first := s.plist()
	for range 20 {
		if got := s.plist(); got != first {
			t.Fatal("two builds of the same Spec produced different plists")
		}
	}
	// And in a stable order that a person can predict: alphabetical.
	cam := strings.Index(first, "NSCameraUsageDescription")
	loc := strings.Index(first, "NSLocationUsageDescription")
	mic := strings.Index(first, "NSMicrophoneUsageDescription")
	if !(cam < loc && loc < mic) {
		t.Errorf("the keys are at %d, %d, %d; they should be in order", cam, loc, mic)
	}
}

// TestThePlistIsEscaped.
//
// ⛔ A plist is XML. An application called "Bed & Breakfast", or a usage
// description with an apostrophe in it -- which is most of them, in a sentence
// about what a program wants -- would otherwise produce a file macOS cannot
// parse. That does not fail loudly either: the Finder simply refuses to open
// the bundle, with no message that names the reason.
func TestThePlistIsEscaped(t *testing.T) {
	s := Spec{
		Name: `Bed & Breakfast <"beta">`, Identifier: "com.example.b", Executable: "x",
		UsageDescriptions: map[string]string{
			"NSCameraUsageDescription": "It's for the glasses' camera <not yours>.",
		},
	}
	p := s.plist()
	for _, raw := range []string{"Bed & Breakfast", `<"beta">`, "It's", "<not yours>"} {
		if strings.Contains(p, raw) {
			t.Errorf("%q went into the plist unescaped", raw)
		}
	}
	for _, want := range []string{
		"Bed &amp; Breakfast &lt;&quot;beta&quot;&gt;",
		"It&apos;s for the glasses&apos; camera &lt;not yours&gt;.",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the plist does not contain %q", want)
		}
	}
	// And the escaping is exactly the five predefined entities: anything else
	// would be a numeric escape that is valid XML and not the string meant.
	if strings.Contains(p, "&#") {
		t.Error("the plist carries a numeric character reference")
	}
}

// TestNoUsageDescriptionsAddsNothing, so a bundle that needs none is the file
// it was before this existed.
func TestNoUsageDescriptionsAddsNothing(t *testing.T) {
	s := Spec{Name: "X", Identifier: "com.example.x", Executable: "x"}
	if strings.Contains(s.plist(), "UsageDescription") {
		t.Error("a bundle that asked for nothing carries a usage description")
	}
}
