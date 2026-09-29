# Deployment Guide

This guide covers deploying `unbound-tui`, a stateless file-backed editor for a
slice of Unbound local data.

## 1. Requirements

- Unbound with `remote-control` enabled, so `unbound-control reload` works.
- Go 1.21+ on the build machine only; the binary is self-contained.
- No database and no system SQLite.

## 2. Install

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

On start the tool reads the main config only to warn you when the fragment is
not included (literal path or a matching glob); it never writes to the main
config.

## 4. Run

```sh
sudo unbound-tui
```

On start the tool:

1. Parses its fragment into an in-memory zone/record model (a missing file is
   treated as empty).
2. Probes `unbound-control` (a warning, not fatal) and checks the include.

Edits are held in memory. Press `w` to **apply**: the model is validated, the
fragment is rewritten atomically (temp file + fsync + rename), and
`unbound-control reload` makes the daemon match the file. Quitting with unsaved
changes asks for confirmation.

## 5. What the tool will and will not touch

- **Owns:** the fragment file only. It is rewritten whole, from validated input.
- **Read-only:** everything else. The `f` view lists what Unbound currently
  serves that does not come from our fragment (zones and records from the main
  config or other includes); it is informational and never modified.

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

Disabling a zone or record does not delete it. The entry is moved to a
commented block at the end of the fragment:

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

1. Remove the `include:` line from the main config.
2. `unbound-control reload`.
3. Optionally delete the fragment file and the binary.
