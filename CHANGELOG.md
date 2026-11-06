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

- `bridge_app` writes a command log: one JSON line per command, written
  before the command is forwarded, plus its outcome and duration
  (`commands_<stamp>.jsonl` beside the other launch logs; see PROTOCOL.md,
  Command log). Requests carry `client` (`terraform`, `mcp`) to name the
  sender. A timeout error now names the command it was waiting on.

- Map lifecycle commands `new_map`, `open_map`, `save_map`, and `get_status`
  fields `busy`, `modified`, `map_file`, `last_map_op` (protocol v18).
  `save_map` checks the file was written and reports `last_map_op.error`
  when it was not; Dungeondraft itself only logs a failed write.

- The mod saves a map-bound client's edits (Terraform's) to that map once
  they have been quiet for 3 s; commands wait while it writes, and opening
  or creating another map saves pending edits first.
  Commands wait for any save, not only the autosave, for at most 20 s. If
  an autosave fails, `open_map` / `new_map` are refused (`unsaved_edits`)
  until a `save_map` succeeds. `get_status` gains `autosave_pending`.

### Fixed

- The provider no longer sends values a config leaves out. An optional,
  computed attribute left unset was sent as its zero value, so a wall without
  `joint` got sharp corners instead of the default bevel and a text without
  `size` got size 0.

- Edits made through the bridge now mark the map modified, so Dungeondraft
  warns before closing and backs it up; `get_status.modified` reflects them.
  Setting the static `Master.IsModified` from GDScript never took effect.
- `dd-launch` now starts Dungeondraft and `bridge_app` fully detached. Before,
  `bridge_app` shared `dd-launch`'s console window, and closing that window
  shut it down.

### Removed

- The `duplicate_object`, `select_elements` and `clear_selection` commands. No
  client sends them now that the MCP server is look-only.

- Every MCP tool that creates, changes, deletes, paints terrain, digs caves,
  selects or undoes (spec goal 2). The MCP server keeps 15 look-only tools:
  status, listing, element details, levels, assets, screenshot and export,
  camera, and `set_level`. Maps are built with Terraform.

- The mod's undo/redo history and the `undo` / `redo` commands (spec
  finding 7). Its stacks held every created node and terrain or cave
  snapshot for the whole session; the command log replaces what history was
  wanted for. Protocol v20: restart Dungeondraft so it loads the new mod.

### Changed

- The whole repo is MIT-licensed, under one `LICENSE` carrying Brandon
  Florian's copyright and JamesLoftis's. The provider's MPL-2.0 `LICENSE`,
  inherited from HashiCorp's provider template, is gone; no provider file
  contains the template's text beyond framework boilerplate.
- One install script, `scripts/install.ps1`, installs everything: the three Go
  binaries, the Terraform provider (`provider/scripts/install.ps1` is gone), and
  the junction that lets Dungeondraft load the mod from the checkout
  (`-ModsDir` for a mods folder other than `%APPDATA%\Dungeondraft\Mods`). It
  replaces a junction left pointing at another checkout, and never deletes a
  real folder.
- **Protocol 21: required means required.** The mod checks every request's
  parameters before it runs and refuses a missing required value, a value of
  the wrong kind (`"abc"` for a number, `"false"` for a bool, a bad colour or
  point), `x` without `y`, or a value out of range, with the new error code
  `bad_param`; nothing is created. These used to be replaced silently: a
  missing position put the element at the map's centre, a negative light
  energy became 0, a text size of 0 became 32, a bad point was dropped from a
  wall. Required now: `x`/`y` on objects, lights, doors and text, `category` on
  patterns, `floor_asset` (and `floor_category` for a pattern floor) on rooms.
- An optional asset that is given but cannot be loaded (a wall's, a room's
  `wall_asset`, a light's) fails instead of falling back: a bad wall asset used
  to leave Dungeondraft not answering until the 30 s timeout. A misspelt
  portal `mount` is refused.
- A door with no wall within `snap_max` is refused instead of quietly becoming
  freestanding; `mount = "free"` asks for a freestanding one, and
  `fallback_free` (now default false) restores the old fallback.
- `build_room` creates its wall and its floor or nothing: a floor that cannot be
  made no longer leaves a wall behind with only a warning (`floor_error` is gone).
- The provider requires and validates the same values at plan time, and each
  optional attribute's description gives its default. A door's `x`/`y` must lie
  on its wall line.

- Levels are identified by label. Any command may carry `level` (a label);
  the mod applies it for that command and restores the active level.
  `add_level` requires a unique `label`. In the provider, `level_id` is
  replaced by `level` on every element resource, the provider block gains
  `level`, and a resource with neither fails at plan time;
  `dungeondraft_level`'s id is its label. `bridgeclient.RunOnLevel` is gone,
  and the provider builds one bridge client.
- The provider block requires `map_file`, and every command carries it as
  `map`; Dungeondraft refuses commands (`wrong_map`) while another map is
  open, so state can never be read against the wrong map.
- Protocol v19: error replies carry a machine-readable `code` (`offline`,
  `timeout`, `not_found`); see PROTOCOL.md, Error codes.

### Fixed

- `get_element` reports the element's `level` label and every element Read
  stores it, so an imported element keeps its level instead of being
  replaced, and a level change made in Dungeondraft shows as drift.
- `dungeondraft_level` plans its `id` as its label, so an element whose
  `level` references `.id` is replaced on a relabel instead of failing
  apply with "inconsistent final plan".
- The provider README and every example follow the new schema
  (`map_file`, `level`) and are validated by the integration suite.

- After a save and reopen, Terraform replaced every level and every element
  on a non-first level: Dungeondraft renumbers level ids on load. A
  `set_level` from one resource could also leak into another's create.
- `set_level` never changed the active level: the mod called C#
  `World.SetLevel(int, bool loading = false)` with one argument, and
  GDScript does not apply C# default arguments, so every level-targeted
  create landed on whichever level was active. It now passes both.

- A client that timed out on a queued edit and retried it created a
  duplicate element. Requests carry a `req_id`; `bridge_app` runs each once
  and replays its reply to retries, and matches agent replies by `req_id`.
  The client's reply deadline is now 40 s (past bridge_app's 30 s command
  timeout) instead of 5 s.

- `bridge_app` waited out its 30 s command timeout on a dead agent after
  Dungeondraft restarted its scene (`new_map`, `open_map`): the request now
  fails at once with `agent_lost`, so those ops take about a second.
- `bridge_app` data races on agent and connection state, and a user that
  stopped reading could stall every other client (replies now have a 5 s
  write deadline). A mod on another protocol version is refused with
  `protocol_mismatch`; `ping` reports `agent_protocol`.

- A Terraform refresh while Dungeondraft was offline or slow dropped every
  element it read from state. Only a `not_found` answer now means an element
  is gone; any other failure is an error and state is left alone.

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
