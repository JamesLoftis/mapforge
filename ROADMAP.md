# Roadmap: fix `place_object` rendering, then build a Terraform provider

Planning doc only — nothing here is implemented yet. Written after a session
of driving this bridge to build a large map, where `place_object` was used
~150 times: every call returned `ok: true` with a correct id/position, but
none of the placed objects ever rendered (confirmed both via `screenshot`/
`export_map` and by the human looking at the live Dungeondraft window).
Everything else (`draw_wall`, `add_portal`, `add_light`, `add_roof`,
`add_text`, `place_pattern`, terrain/paths) rendered correctly in the same
session.

There's a pending uncommitted change on this branch (`fix/idle-connection-reap`,
touching `mcp_bridge.gd` + `bridge_client.py` + `pyproject.toml`) — this
roadmap doesn't touch those files and assumes that work lands first.

**Update, same session — A1 root-caused and fixed.** First retest (after a
DD restart) rendered fine and briefly looked like the whole bug was just
render-state corruption from the original long session. It wasn't — that
first successful test happened to land on bare terrain. The very next test,
on a pattern-floored room, reproduced the original invisible-object bug
immediately. Extending `_place_object`'s debug output to walk the full
CanvasItem ancestor chain (`_debug_ancestor_chain`/`_debug_effective_z`,
still in the working tree) nailed it precisely:

- An `Objects` prop's effective z (summing `z_index` up the parent chain
  while every ancestor is `z_as_relative: true`) is **0**.
- `place_pattern`'s known fix (`z_as_relative=false, z_index=-100`) only
  applies to floors *created through the bridge*. Any floor shape that
  predates that fix, or was placed by hand in the DD UI, keeps the tool's
  default parent — a container literally named `Layer 100`
  (`PatternShapes/Layer 100`) — and stays `z_as_relative: true`, giving it an
  **effective z of 100**. Confirmed directly: a sampled pre-existing floor
  shape reported `effective_z: 100` against the object's `0`.
- So objects were never invisible — they were rendering *underneath their
  own room's floor*, on any room whose floor wasn't built by the current
  bridge. That's why demo_build.py's untextured/bare-terrain test room never
  caught it, and why literally every furnished room in this session's large
  map hid everything placed in it.

**Fix applied** (`_place_object`, both z-fix and the diagnostic instrumentation
still in the working tree — diagnostic can be trimmed once this is committed):
give every placed object an absolute z (`prop.z_as_relative = false;
prop.z_index = int(req.get("z", 200))`), comfortably above the 100 a legacy
relative floor can reach and below roofs (800). Verified: the same
pattern-floored spot that swallowed the previous test object now renders
correctly. `_duplicate_object` shares the pre-fix pattern
(`level.Objects.CreateObject(0)` + `SetTexture`, no z override) and needs the
identical one-line fix — not yet applied, tracked here so it isn't missed.

**Second, more concrete finding this session: repeated Dungeondraft save
failures correlate with bridge activity.** Three consecutive saves failed
right after live tool-call activity; a save attempted after ~15–20s of no
bridge traffic (enough for `bridge_client.py`'s idle timeout / the mod's
`IDLE_REAP_FRAMES` to close lingering connections) succeeded. This lines up
exactly with the concern already written into `mcp_bridge.gd`'s connection-
handling comments ("so DD's autobackup etc. aren't held") and is a strong
argument for finishing and landing the `fix/idle-connection-reap` branch,
then re-testing save reliability specifically (not just connection cleanup)
as its acceptance criterion. This wasn't a controlled experiment (n=1
success after 3 failures), but it's a strong enough correlation to treat as
the leading hypothesis for a real, disruptive bug (mid-session save failures
forcing a revert to an earlier map version, twice, in this session alone).

---

## Part A — Fix the bridge

### A1. Root-cause and fix `place_object` (P0, blocking)

**Why this is the prime suspect, not a guess:** `git log --follow -p` on
`_place_object` shows it was written once, touched only by the generic
undo-stack refactor, and never touched by any of the commits that fixed
rendering bugs in sibling creators — `54aa53c` (place_pattern's invisible-floor
bug: leaked `PatternShapeTool.Color` state), `4d32580`/`e2e76c3`/`c5984a3`
(wall/portal/roof tint and geometry fixes). Those bugs were all "the call
succeeds and returns an id, but the thing doesn't actually render" — the
exact symptom seen here. `place_object` (and `_duplicate_object`, which
shares the same `level.Objects.CreateObject(...)` + `SetTexture(...)`
pattern) is the one creator in the file that has never been debugged this
way.

**Why nothing caught it:** `test_bridge.py` and `demo_build.py` only assert
`ok: true` from the response — neither ever screenshots and checks that a
non-background pixel exists where the object should be. A pure "did the API
call succeed" test cannot catch "the call succeeded but nothing draws,"
which is exactly this bug's shape. This is a process gap as much as a code
bug (see A6).

**Fix approach** (needs a live DD session + mod reload to iterate — CLAUDE.md's
documented dev loop):
1. Reproduce minimally: `place_object` a single common asset (e.g. a crate)
   at map center in an otherwise empty room, `screenshot`, confirm it's
   invisible/wrong-scale even with nothing else around to explain it away.
2. Follow the CLAUDE.md-documented diagnostic technique — stash intermediate
   state in the response (e.g. the prop's resulting `z_index`, `z_as_relative`,
   `get_rect()`/texture size, parent node path) so one mod reload surfaces
   where it diverges from a UI-placed object's equivalent values.
3. Leading hypotheses to check first, roughly in order of how well they fit
   the evidence:
   - **Sizing/rect**: `CreateObject` + `SetTexture` may not compute the prop's
     `Rect2`/collision the way the UI's object-placement tool does, leaving a
     zero-size or degenerate render rect. (Consistent with objects placed
     outdoors on bare terrain in this session rendering as tiny dots rather
     than full-size sprites — see the audit register — rather than being
     fully absent.)
   - **Layer/z parenting**: mirror of the already-fixed `place_pattern` bug —
     new shapes landed in the tool's default "Layer 100" node and needed an
     explicit absolute `z_index` override to sit correctly relative to
     Objects. `CreateObject`'s prop may need an equivalent explicit
     layer/z assignment that nothing currently sets.
   - **Missing post-`SetTexture` step**: the UI path may call something
     after `SetTexture` (e.g. a `Rect`/size recompute, an `UpdateTexture()`
     or `queue_redraw` equivalent) that `_place_object` skips.
4. Whichever it is, apply the same fix to `_duplicate_object` (identical
   pattern, likely identical bug, currently untested by anything).
5. Verify with a screenshot-based check before calling it fixed, not just a
   successful `ok: true`.

### A2. Expose real geometry for walls, paths, roofs, and pattern shapes (P0 for Part B)

Right now `list_elements`/`get_element` report `position: [0, 0]` for every
wall (and presumably every path/roof) — the only way to learn a wall's
actual shape this session was `fit_elements`'s bounding box, plus manual
binary search via `select_elements` + repeated screenshots to identify which
specific wall among ~90 owned a given piece of geometry. That took roughly
15 tool round-trips to identify **one** wall. This is the single biggest
practical gap found this session, and it's a hard blocker for Part B: a
Terraform provider's core value is `Read` (drift detection), and you cannot
`Read` a wall's shape at all right now, only infer a bounding box.

Add a `points` field to the descriptor for `kind in [wall, path, roof]`
(and to `place_pattern`/`build_room`'s output) — e.g.
`{ id, kind:"wall", points:[[x,y],...], loop, asset, ... }`. This is a
straightforward win: the mod already has `wall.get("Points")` (used
internally by `_nearest_wall_segment`/`_wall_centroid`) — `_describe()`
just needs to surface it for the relevant kinds instead of only exposing it
to internal geometry helpers.

### A3. A real "read whole map" command (P1)

A `dump_state` (or `export_state`) command that walks every collection and
returns full geometry + params for every element in one call — effectively
`list_elements` for every kind, with A2's full geometry, in one round trip.
This is the foundation both for:
- Terraform's provider-level `Read`/import workflow (Part B), and
- proper backup/restore, superseding the delta/replay-log approach used this
  session (a `live_ops_log.jsonl` of intent is good for crash-safety, but a
  point-in-time full-state dump is what you actually want for "restore to
  exactly this snapshot" rather than "replay these deltas from an assumed
  starting point").

### A4. Explicit `wall_id` on `add_portal` (P1)

`add_portal`'s default nearest-wall-snap behavior is convenient for
AI-driven, look-and-adjust workflows, but it's nondeterministic input for a
declarative tool: the "nearest wall" can change silently if geometry shifts
elsewhere on the map. Add an optional `wall_id` param that mounts on a
specific wall directly (skipping the distance search when given). Keep
snap-by-position as the default for backward compatibility / AI use.

### A5. Defensive input validation audit (P1, risk mitigation for Part B)

GDScript on 3.4.2 has no try/catch — an unhandled runtime error in a handler
kills the whole bridge (full DD restart, per CLAUDE.md). That's an
acceptable risk for an interactive AI session (a human is watching and can
restart DD), but it's a much bigger deal for a Terraform apply that might
run a hundred resource creates unattended. Audit every handler for unchecked
assumptions before Part B leans on it hard — e.g. out-of-range `slot`,
malformed `points` arrays, negative `radius`/`width`, unknown `wall_type`/
`joint`/roof `type` enums. Prefer returning `_err(...)` over letting a bad
`int()`/array-index crash the dispatch loop.

### A6. Add a visual-regression smoke test (P1, prevents recurrence)

Extend `test_bridge.py` (or add a new script) so that after each `place_*`/
`draw_*`/`add_*` call it takes a tightly-framed `screenshot` (via
`focus_element`) and asserts the frame isn't just background/floor color —
even a crude "count of non-background-ish pixels in the center region" would
have caught this bug immediately instead of it going unnoticed through
~150 successful-looking calls in a live session. This is cheap insurance
against every future "renders wrong" class of bug in this codebase.

### A7. Nice-to-haves (P2, not blocking)

- A `batch` command accepting an array of `{cmd, params}` in one request —
  cuts round-trip count for large applies (Part B will otherwise be one TCP
  round-trip per resource).
- A `delete_elements` (plural) for bulk cleanup — this session's structural
  fix needed ~30 individual `delete_element` calls.

---

## Part B — Terraform provider

> **Now developed in its own repo, `terraform-provider-dungeondraft`** (moved
> out of `provider/` here once it had working resources/data sources — see
> that repo for current status). The plan below is kept as the design record;
> A1/A2 (this repo's mod) are the prerequisites it depends on.

**Depends on A1 (place_object actually rendering) and A2 (real geometry
reads) to be worth building beyond a write-only proof of concept.** Without
A2, `terraform plan` can never show a true diff for walls/paths/roofs — it
would have to treat them as write-once, which defeats much of the point of
choosing Terraform over just replaying an ops log.

### Architecture

A single static **Go** binary (the Terraform ecosystem norm — no Python
runtime dependency for end users), built on
[`terraform-plugin-framework`](https://developer.hashicorp.com/terraform/plugin/framework),
talking **directly** to the bridge's TCP/JSON-lines wire protocol (see
`PROTOCOL.md`) — no MCP layer involved, same principle as this session's
`replay_ops.py`. The wire protocol is trivial (one JSON object per line,
`net` + `encoding/json` in Go covers it in well under 100 lines), so
reimplementing `bridge_client.py`'s framing in Go is the right call rather
than shelling out to Python.

```
terraform apply
      │
      ▼
terraform-provider-dungeondraft (Go binary)
      │  same wire protocol as bridge_client.py: {"cmd":...}\n in, {"ok":...}\n out
      ▼
mcp_bridge.gd inside a running Dungeondraft
      │
      ▼
   the open map
```

Provider config: `host`/`port` (default `127.0.0.1:8787`, matching
`DD_BRIDGE_HOST`/`DD_BRIDGE_PORT`).

### Resource model

One resource type per bridge "create" command, following the same
params 1:1 so `PROTOCOL.md` stays the single source of truth for both the
MCP tools and the provider:

| Resource | Bridge cmd | Notes |
| --- | --- | --- |
| `dungeondraft_wall` | `draw_wall` | `points`/`asset`/`loop` → `RequiresReplace` (no reshape API yet) |
| `dungeondraft_portal` | `add_portal` | needs A4's explicit `wall_id` to be deterministic |
| `dungeondraft_object` | `place_object` | blocked on A1 |
| `dungeondraft_light` | `add_light` | `x`/`y`/`color`/`energy` etc. updatable in place via `modify_object`-equivalent, or replace if the bridge has no light-update command (check — currently it doesn't; would need one) |
| `dungeondraft_roof` | `add_roof` | `RequiresReplace` (ridge points define the whole shape) |
| `dungeondraft_text` | `add_text` | text/size/color |
| `dungeondraft_pattern` | `place_pattern` | floor shapes |
| `dungeondraft_room` | `build_room` | convenience composite (wall + floor in one resource, mirroring the bridge's own composite) |
| `dungeondraft_terrain_fill` | `fill_region` | scoped to a rect/polygon — a real "resource" in the sense that it's a bounded, trackable paint operation, not a single global fill |
| `dungeondraft_path_paint` | `paint_path` | roads/trails |
| `dungeondraft_cave` | `dig_cave` | `dig=false` on destroy is a natural fit for `Delete` |

Provider-level **data sources** (read-only, for import/inspection/lookup —
not created/destroyed by Terraform):

- `dungeondraft_status` — map info (size, center, counts).
- `dungeondraft_assets` — search an asset category (mirrors `list_assets`),
  so a `.tf` file can reference `data.dungeondraft_assets.chairs.assets[0]`
  instead of hardcoding a resource path string.
- `dungeondraft_map` — the A3 full-state dump, for drift inspection /
  `terraform import` tooling.

Global singletons like `set_terrain_slot` (there are only 8 slots, shared
map-wide) fit better as a `dungeondraft_terrain_slot` resource keyed by slot
index (0–7) than as a per-call side effect — declaring slot 0 = grass is a
piece of state Terraform should own and diff, not a fire-and-forget action.

### CRUD mapping and honest limitations

- **Create** → the matching bridge command; store the returned bridge `id`
  as the Terraform resource id.
- **Read** → blocked today except for a bounding box (`fit_elements`) or a
  bare position; becomes real once A2 lands. Until then, the provider can
  ship in a documented **write-mostly mode**: it trusts its own state file
  for what it created and never detects hand-edits made directly in the DD
  UI. That's a real, load-bearing limitation worth stating up front rather
  than discovering later — Terraform's core promise (state always reflects
  reality) doesn't fully hold until A2 exists.
- **Update** → most resources will need `RequiresReplace` on their
  shape-defining fields (`points`, `rect`, `asset`) since the bridge has no
  "reshape" API for any of them yet; only `move_element`/`modify_object`-style
  fields (position, scale, rotation, color) are true in-place updates today.
- **Delete** → `delete_element`. Note it's **not undo-able** on the bridge
  side (per `PROTOCOL.md`) — a `terraform destroy` is genuinely permanent,
  which is normal for Terraform but worth calling out since this bridge's
  own `undo`/`redo` won't help recover from it.

### Suggested build order

1. A1 + A2 in the bridge (unblocks everything else; A2 alone also directly
   fixes this session's "how do I find which wall is which" pain).
2. Go client for the wire protocol + `dungeondraft_status` data source as
   the walking skeleton (prove the binary talks to a live bridge).
3. `dungeondraft_wall` + `dungeondraft_portal` (with A4) — enough to
   declaratively reproduce this session's structural fix (the throne-room
   wall rebuild) as a `.tf` file instead of a `replay_ops.py` run, which is
   a good acceptance test for the provider.
4. `dungeondraft_object` once A1 lands.
5. Everything else in the resource table.
6. A3-backed `dungeondraft_map` data source + real `Read` implementations,
   closing the write-mostly-mode gap.
