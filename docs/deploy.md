# Deployment Guide

This guide covers deploying `unbound-tui`, a stateless file-backed editor for a
slice of Unbound local data.

## 1. Requirements

- Unbound with `remote-control` enabled, so `unbound-control reload` works.
- Go 1.21+ on the build machine only; the binary is self-contained.
- No database and no system SQLite.

## 2. Install

```sh
go build -o unbound-tui .
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

## 6. Enabling and disabling

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

## 7. Cross-compilation

```sh
GOOS=linux GOARCH=arm64 go build -o unbound-tui-linux-arm64 .
GOOS=linux GOARCH=amd64 go build -o unbound-tui-linux-amd64 .
```

## 8. Rollback

1. Remove the `include:` line from the main config.
2. `unbound-control reload`.
3. Optionally delete the fragment file and the binary.
