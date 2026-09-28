# Roadmap

**Goal:** let the tool manage as much *standard* Unbound configuration as
possible, in the simplest way, while still owning a **single fragment file**
(never the main `unbound.conf`).

**Principle:** a generic section/directive model provides the breadth; a small
set of specialized editors covers the frequent or structured directives; any
directive we do not model is passed through verbatim.

## Decisions to settle

- [ ] Ownership boundary: **own our fragment only (current, recommended)** vs
      own the whole `unbound.conf`.
- [ ] Generalize "disabled" to any entry (comment it out), not just
      `local-zone` / `local-data`.
- [ ] Duplicate sections: `forward-zone` / `stub-zone` are distinguished by
      `name`; error or warn on duplicates.

## M1 - Data model (config)

- [ ] `Fragment{Sections []Section}`, `Section{Kind, Entries []Entry}`,
      `Entry{Key, Value string, Disabled bool}`.
- [ ] Parser reads the fragment into sections/entries, **preserving unknown
      directives verbatim**; handles quoting and repeated keys.
- [ ] Serializer is deterministic and preserves unknown directives; disabled
      entries are written as comments (folding in the existing disabled block).
- [ ] Backward compatible: today's `local-zone` / `local-data` fragments load
      unchanged.
- [ ] Round-trip tests (parse/serialize identity; unknown directives; disabled;
      quotes).

## M2 - Generic editor UI (model)

- [ ] Left pane: sections (including multiple `forward-zone` / `stub-zone`
      instances); right pane: the selected section's entries.
- [ ] Add/delete sections; add/edit/delete entries; same key feel as today
      (`a`/`e`/`d`/`w`/`g`/`G`/`ctrl+d`/...).
- [ ] Specialized editors driven by a schema: bool, int, path, address, CIDR,
      RR line.
- [ ] **Unknown directives**: plain text edit, passed through verbatim.
- [ ] Keep the friendly **Local data** (zone/record) view, derived from
      `local-zone` / `local-data`, and add a **Config** view over the generic
      model.
- [ ] `view_test` coverage for section/entry editing, passthrough, disabled
      rendering.

## M3 - forward-zone / stub-zone (immediate need)

- [ ] Model and parsing: `name`, `forward-addr` / `forward-host`,
      `forward-tls-upstream`, `forward-first`; `stub-addr` / `stub-host`,
      `stub-prime`, `stub-first`.
- [ ] Forms: add/remove upstream addresses, DoT toggle, `tls-cert-bundle` path.
- [ ] Validation: `IP[@port][#auth]`, host, booleans, port range.
- [ ] **Conflict detection**: read the other configuration (read-only) and
      warn/refuse when a `forward-zone` / `stub-zone` with the same name
      already exists (avoids Unbound's "duplicate" error).

## M4 - server / remote-control common options

- [ ] `server:` - `interface`, `port`, `access-control` (CIDR + allow/deny),
      `tls-cert-bundle`, `root-hints`, `username`, `verbosity`.
- [ ] `remote-control:` - `control-enable`, `control-interface`,
      `control-use-cert` (and certificate paths).
- [ ] Conflict detection for duplicate scalar options set elsewhere.

## M5 - Polish, docs, release

- [ ] README / deploy: what can be managed, how, the ownership boundary, and
      how conflicts are reported.
- [ ] Keep CI green; align coverage (cmd / config / model).
- [ ] Cut a tag (e.g. `v0.2.0-ea.1`); GoReleaser builds the release.

## Cross-cutting

- [ ] All validation goes through `internal/validate` (or a small dedicated
      validator): the single pre-write gate.
- [ ] Apply stays the same: validate -> atomic write of the fragment ->
      `unbound-control reload`.
- [ ] Actionable errors that name the offending section/entry.

## Testing

- [ ] config: round-trip, unknown passthrough, disabled, quotes, duplicate
      sections.
- [ ] validate: value boundaries for each new directive.
- [ ] model: section/entry add/edit/delete, specialized forms, conflict
      prompts, view switching.
- [ ] End-to-end on Debian: add a `forward-zone "."` with DoT, apply, and
      verify via `unbound-control list_forwards`.

---

M3 is recommended first: it closes the forwarding/recursion gap, which is the
most requested capability.
