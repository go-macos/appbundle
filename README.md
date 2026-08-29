# go-macos/appbundle

**The `.app` directory a macOS program lives in: whether the running process is
inside one, and how to assemble one around an executable.** Pure Go,
`CGO_ENABLED=0`, no AppKit — path and file work only, so it builds and is
tested on every platform, which is what makes a bundle assembler useful in a
cross-compiling build.

```go
if b, ok := appbundle.Running(); ok {
    // Launched from the Finder: no arguments, and a menu bar to live in.
}

_, err := appbundle.Build(appbundle.Spec{
    Dir: "dist", Name: "godl", Identifier: "io.github.go-downloader.godl",
    Version: "0.1.0", Executable: "build/godl",
    Accessory: true,          // LSUIElement: a menu-bar item, no dock tile
    MinimumSystem: "11.0",
})
```

## Why a bundle at all

A bare executable is not an application on this system. AppKit reads what a
program **is** from the bundle around it, so a program that wants a menu-bar
item, a dock tile, a name in the menu bar, notification permission or a place
in Login Items has to be in one.

Asked for from outside a bundle, a status item is asked for by nobody: it never
appears, and the process ends without complaining. That silence is the whole
reason this package exists — it is not an error anybody sees, it is a menu bar
that stays empty.

`Accessory` sets `LSUIElement`, which is what separates a status-item program
from an ordinary one: a menu-bar item, no dock tile, no menu of its own.

## Notes

- `Of` asks about a path, not about the machine, so it answers the same way
  everywhere. A build on another platform can reason about the bundle it is
  assembling.
- `Build` replaces what was there. A bundle assembled over an older one keeps
  files nothing refers to any more, and those are the ones that go stale
  without anybody noticing.
- A program that cannot locate itself is treated as not bundled: better a
  command-line program than an application acting on a guess.

BSD-3-Clause.
