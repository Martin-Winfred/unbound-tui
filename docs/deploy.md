# Deployment Guide

This guide covers deploying `unbound-tui`, a stateless file-backed editor for a
slice of Unbound configuration — local zones/records plus a generic
section/entry editor over its own fragment.

## 1. Requirements

- Unbound with `remote-control` enabled, so `unbound-control reload` works.
- Go 1.25+ on the build machine only; release binaries are self-contained.
- No database and no system SQLite.

## 2. Install

Prebuilt Linux binaries (amd64, arm64, armv7) are published on the
[Releases page](https://github.com/Martin-Winfred/unbound-tui/releases) by
GoReleaser. Download the archive for your architecture and install it:

```sh
tar xzf unbound-tui_*_linux_amd64.tar.gz
sudo install -m 0755 unbound-tui /usr/local/bin/unbound-tui
```

Or build from source:

```sh
go build -o unbound-tui ./cmd/unbound-tui
sudo cp unbound-tui /usr/local/bin/
sudo chmod 755 /usr/local/bin/unbound-tui
```

## 3. The fragment file

The tool owns exactly one file and never edits any other config:

```ini
# /etc/unbound/unbound.conf.d/unbound-tui.conf
```

Debian and Ubuntu load `/etc/unbound/unbound.conf.d/*.conf` automatically, so
on those systems no extra configuration is needed. On other layouts add the
include yourself:

```ini
# main unbound.conf
include: /etc/unbound/unbound.conf.d/unbound-tui.conf
```

On start the tool reads the main config to check that the fragment is included
(a literal path or a matching glob); it never writes to the main config. If the
main config does not include the fragment, **apply** is refused (with the fix in
the status line) instead of writing the fragment and reloading a daemon that
never saw it. The check inspects the main config only: a fragment included
indirectly through another file is not detected, so the main config must include
it directly.

## 4. Run

```sh
sudo unbound-tui
```

On start the tool:

1. Parses its fragment into an in-memory section/entry model and projects the
   zone/record view from it (a missing file is treated as empty).
2. Probes `unbound-control` (a warning, not fatal) and checks that the main
   config directly includes the fragment.

Edits are held in memory. Press `w` to **apply**: the model is validated, the
fragment is rewritten atomically (temp file + fsync + rename), and
`unbound-control reload` makes the daemon match the file. Quitting with unsaved
changes asks for confirmation.

Record TTLs are plain integers in seconds; a unit suffix (for example `1h`) is
rejected, not interpreted, when the Local data view projects the fragment.

## 5. What the tool will and will not touch

- **Owns:** the fragment file only. It is rewritten whole, from validated input.
- **Read-only:** everything else. The `f` view shows what Unbound currently
  serves that does not come from our fragment (zones and records from the main
  config or other includes), and its `u` tab lists foreign `forward-zone` /
  `stub-zone` sections read from the include graph. Both are informational and
  never modified.

The generic **Config** view edits every section and entry *of our fragment* —
including `server:` / `remote-control:` options and `forward-zone` / `stub-zone`
sections — while the `local-zone` / `local-data` entries stay owned by the
**Local data** view. It never reaches into foreign files.

Because the tool only rewrites its own file and reloads, it cannot delete or
alter entries it does not own.

## 6. Conflicts and manual resolution

The tool reads the whole include graph (recursively) to show what Unbound
already serves, but it owns and writes only its own fragment. A directive that
appears both in the graph and in our fragment is a conflict; since the tool
never edits foreign files, resolving one is a manual step. Apply is refused
until it is resolved.

**Named sections — `forward-zone` / `stub-zone`.** These are identified by name
(`forward-zone "smoke."`). If an active section of the same kind and name is
declared elsewhere, apply is refused:

```text
error: cannot apply, conflicts with the include graph: forward-zone "smoke." already exists in /etc/unbound/unbound.conf.d/zz.conf
```

Fix it in the file named by the message: remove the foreign
`forward-zone`/`stub-zone` section, or remove ours.

**Singleton options — `server:` / `remote-control:`.** Unbound accepts options
such as `verbosity`, `port`, `interface`, `control-*` and the certificate paths
only once per section. Setting one in our fragment while a section of the same
kind in another file sets it too is a latent conflict (the later declaration
silently wins). Adding or editing such a key warns without blocking; apply is
refused:

```text
error: cannot apply, conflicts with the include graph: server: verbosity already set in /etc/unbound/unbound.conf.d/zz.conf — edit that file manually (see deploy.md: Conflicts and manual resolution)
```

Distributions commonly provision `remote-control.conf` with `control-enable`,
`control-interface`, etc. already set. To use our `remote-control:` section,
remove the option from that file (or remove it from ours) and apply again.

Warnings are advisory and never block an edit. A refusal always leaves the
fragment byte-unchanged and performs no reload. The tool never writes outside
its own fragment; the foreign edit is always yours to make.

## 7. Enabling and disabling

Disabling an entry does not delete it. The entry is moved to a commented block
at the end of the fragment. This works for any entry — a zone or record in the
Local data view, or any section/entry in the Config view:

```ini
# unbound-tui:disabled
# local-zone: "old.example." static
# local-data: "host.old.example. 60 IN A 192.0.2.9"
```

The fragment declares its own `server:` section, so it is valid wherever the
main config includes it.

Re-enabling moves it back. Records of a disabled zone are never written as
active `local-data`, so a disabled zone cannot spring an implicit transparent
zone.

## 8. Cross-compilation

```sh
GOOS=linux GOARCH=arm64 go build -o unbound-tui-linux-arm64 ./cmd/unbound-tui
GOOS=linux GOARCH=amd64 go build -o unbound-tui-linux-amd64 ./cmd/unbound-tui
```

## 9. Rollback

Stop managing the fragment and let Unbound keep serving what remains. The steps
depend on how the fragment was included.

**Debian/Ubuntu (`include-toplevel`, no `include:` line).** Delete or rename the
fragment, then reload:

```sh
sudo rm /etc/unbound/unbound.conf.d/unbound-tui.conf
sudo unbound-control reload
```

**Manual `include:`.** Remove the `include:` line from the main config, then
reload. With the line gone the tool refuses to apply — the main config no
longer includes the fragment — so re-enabling it means adding the line back.

In both cases the binary can be deleted afterwards. No other file is touched.
