# Changelog

All notable changes to this project (the mod, `bridge_app`, and `mcpserver`)
are documented here. Format follows [Keep a
Changelog](https://keepachangelog.com/en/1.1.0/); versioning follows
[Semantic Versioning](https://semver.org/) (pre-1.0: a minor bump may still
carry a breaking protocol/behavior change, not just a patch bump).

`mcpserver`/`bridge_app` report their build version from the nearest git tag
(`git describe --tags`, stamped in by `scripts/install.ps1`) — tag a release
here (`git tag vX.Y.Z`) to have it show up in `/health` and the MCP
`serverInfo`. The mod's own version lives in
`mod/dungeondraft-mcp-bridge-agent/mcp_bridge.ddmod`'s `"version"` field
(Dungeondraft has no other way to surface it) — bump both together.

## [Unreleased]

### Added

- Map lifecycle commands `new_map`, `open_map`, `save_map`, and `get_status`
  fields `busy`, `map_file`, `last_map_op` (protocol v18).

### Fixed

- Objects placed or duplicated through the bridge were silently left out of
  every save: Dungeondraft's `Objects.Save()` skips a prop without the
  `preview` meta, which only its own Object tool sets. The mod now sets it.

## [0.2.0] - 2026-09-02

### Fixed

- Terrain slots 4-7 (`splatImage2`) were completely unusable — every
  `paint_terrain`/`fill_region`/`paint_path` call on any of those slots
  failed with `could not read splat image for slot N`, on any map that
  hadn't already used more than 4 terrain types. Root-caused against
  Dungeondraft's terrain behaviour: `splatImage2` is
  only ever allocated by `Terrain.ExpandSlots(true)`, which the mod never
  called. Two earlier fix attempts in this same release cycle were wrong
  and reverted: a hand-rolled blank-image `RestoreSplat2()` call (wrong
  arity — the real signature takes two images, not one) and calling
  `Terrain.Fill()` on an unexpanded level (which silently corrupts the
  *primary* image's data for an unrelated slot instead of allocating
  anything). Verified the shader swap `ExpandSlots(true)` triggers
  (`Terrain2.shader`) doesn't change how already-painted slots 0-3 render.
- A change that added a second `.gd` file anywhere under the mod's own
  folder (tried both `scripts/tools/` and a new `scripts/lib/`) crashed
  Dungeondraft's mod loader outright (`ModManager.LoadScript` unhandled
  `NullReferenceException`, taking down the whole mod, not just the new
  file) — reverted; the mod must stay a single `.gd` file. Documented in
  the mod's own top-of-function comments and in the
  `dungeondraft-bridge-maintenance` Claude Code skill
  so this isn't rediscovered the hard way again.

### Added

- `mod_tests/` — the mod's GDScript logic had zero test coverage before
  this; `mod_tests/unit/test_open_splat.gd` covers the terrain-slot fix
  above via a `FakeTerrain` double modeled on the observed engine
  behavior. Lives outside `mod/` entirely (see the "second .gd file" fix
  above for why) as a hand-synced duplicate of the mod's real inline logic
  — no automated check keeps them in sync, only a comment in each. Run
  with a real Godot 3.4.2 editor binary: `godot --no-window --script
  mod_tests/unit/test_open_splat.gd` from the repo root.
- `bridge_app`: `TestTerrainSlot4to7ErrorRelayedThenFixWorks` (and the
  `fakeAgentFunc` helper it's built on) — regression coverage for the
  relay layer's handling of the exact error-then-recovery sequence above,
  independent of the mod-side fix itself.
- `mod_version` in `ping` and `get_status` responses, read at runtime from
  `mcp_bridge.ddmod`'s own `"version"` field (cached after first read) —
  so which build is actually live is a query, not a guess. Added directly
  because of the debugging pain in the fix above: multiple reload cycles
  produced ambiguous "did my change actually take effect?" moments where
  behavior alone couldn't distinguish stale-code from a genuinely new bug
  (a save-then-reload cycle later reproduced exactly this ambiguity again,
  the same session — a failure that "fixed itself" after reload+re-add,
  impossible to diagnose after the fact without knowing which code was
  running when it failed).

## [0.1.0] - unreleased prior to this changelog existing

Baseline — outbound-agent architecture (mod dials `bridge_app`, not the
reverse), full object/wall/path/light/portal/roof/text/terrain/cave
command set, Terraform provider split into its own sibling repo. No
changelog was kept before this file was added; see `git log` for history
back to the project's start.
