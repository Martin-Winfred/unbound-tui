# unbound-tui

A stateless terminal UI for managing a slice of [Unbound](https://nlnetlabs.nl/projects/unbound/about/) configuration. It owns exactly one config fragment file and never edits the rest of your Unbound setup. You edit an in-memory model of that fragment, and on apply the fragment is validated, written atomically, and `unbound-control reload` makes the daemon match it. There is no database and no state between runs.

## Features

- **Local data view (default).** The zone/record editor: add and delete zones, add/edit/delete records (type, value, TTL), change a zone's type, and enable/disable a zone or record without losing data.
- **Config view (`c`).** A generic editor over the whole fragment: every section and entry, including `server:`, `remote-control:`, `forward-zone:`, `stub-zone:` and any directive the parser passed through verbatim. Add a section (`A`) or entry (`a`), edit an entry (`e`), delete (`d`/`D`), and enable/disable (`space`). Values are type-checked where the schema knows the directive (bool, int, port, address/CIDR, path, RR line, upstream host/address).
- **Specialized forward/stub form (`E`).** For a selected `forward-zone`/`stub-zone`, a structured form for the name, the upstream addresses, and the `forward-tls-upstream`/`forward-first` (or `stub-prime`/`stub-first`) toggles. Unmanaged directives in the section are preserved verbatim.
- **Foreign view (`f`).** Read-only visibility into what Unbound serves outside our fragment. The runtime tab lists foreign zones and records; `u` switches to a tab of foreign `forward-zone`/`stub-zone` sections read from the include graph. `/` filters either tab.
- **Conflict detection.** The include graph is read recursively, so a directive declared both in our fragment and elsewhere is caught. Adding or editing a conflicting named section or singleton option warns immediately; `w` refuses to apply until you resolve it manually. See [docs/deploy.md § Conflicts and manual resolution](docs/deploy.md#6-conflicts-and-manual-resolution).
- **Read-only include graph.** The tool reads the main config and its includes to show the full effective configuration and to warn about conflicts, but writes only its own fragment.
- **Single validated write path.** Every value passes the whitelist validator before it can reach the fragment. Apply is validate → atomic write → `unbound-control reload`.

## Install

Prebuilt Linux binaries (amd64, arm64, armv7) are published on the [Releases page](https://github.com/Martin-Winfred/unbound-tui/releases) by GoReleaser as `unbound-tui_<version>_linux_<arch>.tar.gz`, with a `checksums.txt`. Download, extract, and install the binary:

```sh
tar xzf unbound-tui_*_linux_amd64.tar.gz
sudo install -m 0755 unbound-tui /usr/local/bin/unbound-tui
```

Or build from source (Go 1.25+):

```sh
go build -o unbound-tui ./cmd/unbound-tui
```

## Quick start

On Debian/Ubuntu, Unbound loads `/etc/unbound/unbound.conf.d/*.conf` via `include-toplevel`, so the default fragment is included with no extra setup:

```sh
sudo unbound-tui
```

1. Press `a` to add a zone (for example `example.com`), then `tab` to the records pane and `r` to add a record under it.
2. Press `w` to apply: the fragment is written atomically and `unbound-control reload` runs. Nothing is persisted before that.
3. Verify from another shell:

```sh
sudo unbound-control list_local_zones
sudo unbound-control list_local_data
```

On other layouts, add `include: /etc/unbound/unbound.conf.d/unbound-tui.conf` to the main config. The tool warns on start when the fragment is not included (literal path or a matching glob); it never writes the main config.

## Ownership boundary

- The tool owns **one** fragment file (default `/etc/unbound/unbound.conf.d/unbound-tui.conf`) and rewrites it whole from validated input. It never edits the main config or any other include.
- Everything else in the include graph is read-only: it is used to render the Foreign view and to detect conflicts, never to write.
- When a conflict is detected, the fix is always yours: edit the named foreign file manually. Apply is refused (not silently reordered) until then. Details and examples: [docs/deploy.md § Conflicts and manual resolution](docs/deploy.md#6-conflicts-and-manual-resolution).

## Keys

Local data view and Config view:

| Key | Local data view | Config view |
|-----|-----------------|-------------|
| `j`/`k`, `↓`/`↑` | move the cursor | move the cursor |
| `g`/`G` | jump to top/bottom | jump to top/bottom |
| `ctrl+d`/`ctrl+u` | page down/up | page down/up |
| `tab` | switch the zones/records panes | switch the sections/entries panes |
| `a` | add a zone | add an entry to the focused section |
| `A` | — | add a section |
| `r` | add a record to the focused zone | — |
| `e` | edit the focused record's TTL | edit the focused entry |
| `E` | — | specialized form for the selected forward-zone/stub-zone |
| `t` | change the focused zone's type | — |
| `d` | delete the focused record | delete the focused entry |
| `D` | delete the focused zone and its records | delete the focused section (only when it holds no `local-*` entries) |
| `space` | enable/disable the focused zone or record | enable/disable the focused entry (section pane: every entry of the section) |
| `c` | switch to the Config view | switch to the Local data view |
| `f` | open the read-only Foreign view | open the read-only Foreign view |
| `w` | apply | apply |
| `q` / `ctrl+c` | quit (confirms unsaved changes) | quit (confirms unsaved changes) |

`Type` fields in the New zone, New record and Change zone type forms open a
searchable picker: `enter` opens it, typing filters the list, `↑`/`↓` move the
highlight, `enter`/`tab` pick the value, and `esc` closes the picker keeping the
previous value. Choose the type first and the Value placeholder shows that
type's format (for example `10 mail.example.com` for MX). Submit these forms
with `ctrl+s`; `enter` still submits from a plain last field such as TTL.

Foreign view (opened with `f`):

| Key | Action |
|-----|--------|
| `j`/`k`, `↓`/`↑` | move the cursor |
| `tab`, `h`/`l`, `←`/`→` | switch between the zones and RRs panes |
| `u` | toggle between runtime zones/RRs and foreign forward/stub upstreams |
| `/` | filter (zone name, RR, or upstream kind/name/source); `esc` clears |
| `g`/`G` | jump to top/bottom |
| `ctrl+d`/`ctrl+u` | page down/up |
| `esc`, `q`, `f` | back |

## Flags

| Flag | Default | Purpose |
|------|---------|---------|
| `-config` | `/etc/unbound/unbound.conf` | Path to the Unbound main config (used for the include check and `unbound-control`); must exist |
| `-fragment` | `/etc/unbound/unbound.conf.d/unbound-tui.conf` | Fragment file the tool owns |
| `-version` | | Print the version and exit |

## Fragment layout

```ini
# Generated by unbound-tui - do not edit

server:
local-zone: "example.com." transparent
local-data: "example.com. 300 IN A 192.0.2.1"
local-data: "www.example.com. 300 IN A 192.0.2.2"

# unbound-tui:disabled
# local-zone: "old.example." static
# local-data: "host.old.example. 60 IN A 192.0.2.9"
```

The fragment declares its own `server:` section, so `local-zone`/`local-data` are valid wherever the main config includes it. `include:` is textual inlining, so the section is required regardless of where the include sits.

## Requirements

- Unbound with `remote-control` enabled, so `unbound-control` works.
- Go 1.25+ (build only; release binaries are self-contained).

## Development

```sh
go build -o unbound-tui ./cmd/unbound-tui   # build
go test ./...                               # all tests
go vet ./...                                # static checks
gofmt -l .                                  # list formatting diffs
```

See [ROADMAP.md](ROADMAP.md) for the milestone plan and [docs/deploy.md](docs/deploy.md) for deployment.

## Feedback

Bug reports, feature requests and ideas are welcome. Please open an issue:

<https://github.com/Martin-Winfred/unbound-tui/issues>
