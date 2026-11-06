# Provider reference: dungeondraft

The Terraform provider that builds maps in a running Dungeondraft, through
`bridge_app` (see the [root README](../README.md) for installing it and a first
map, and [PROTOCOL.md](../PROTOCOL.md) for the commands it sends). It is built
on [`terraform-plugin-framework`](https://developer.hashicorp.com/terraform/plugin/framework)
and talks to `bridge_app` with the shared `bridgeclient` module.

## Provider config

```hcl
provider "dungeondraft" {
  map_file = "C:/maps/keep.dungeondraft_map" # required, absolute path
  level    = "Ground"   # optional: the level elements go on unless a resource sets its own
  host     = "127.0.0.1" # optional, default 127.0.0.1
  port     = 8787        # optional, default 8787
}
```

## Resources

| Resource | Bridge cmd | In-place update | Everything else |
| --- | --- | --- | --- |
| `dungeondraft_wall` | `draw_wall` | — | replaces |
| `dungeondraft_portal` | `add_portal` | — | replaces |
| `dungeondraft_object` | `place_object` | x, y (`move_element`), rotation, scale, color (`modify_object`) | asset, sorting, z replace |
| `dungeondraft_light` | `add_light` | — | replaces |
| `dungeondraft_roof` | `add_roof` | — | replaces |
| `dungeondraft_text` | `add_text` | x, y (`move_element`) | text, size, color, font replace |
| `dungeondraft_pattern` | `place_pattern` | — | replaces |
| `dungeondraft_path` | `draw_path` | — | replaces |
| `dungeondraft_terrain_fill` | `fill_terrain`/`fill_region`/`paint_terrain`/`paint_path` | re-paints in place on any change | — |
| `dungeondraft_cave` | `dig_cave` | re-applies in place on any change | — |
| `dungeondraft_room` | `build_room` | — | replaces (compound create — a wall loop + floor in one call, no reshape API) |
| `dungeondraft_level` | `add_level` | — | replaces, but see the level-targeting section below — this one can't really be deleted either |

"Replaces" means every attribute in that resource carries `RequiresReplace`:
the bridge has no update command for it, so a config change destroys and
recreates the element rather than silently doing nothing (see git history —
this used to be a real bug: several attributes were `Optional+Computed` for
drift detection but had neither `RequiresReplace` nor a working `Update`,
so changing them in config appeared to succeed and changed nothing).

Every node-kind resource stores the bridge's numeric element id as its
Terraform id, supports `terraform import <id>`, and `Delete` calls
`delete_element` — which is **genuinely permanent** (not undo-able on the
bridge side). `dungeondraft_terrain_fill` cannot un-paint; its Delete warns.
`dungeondraft_room`'s `id` is its wall's element id (so it composes with
`dungeondraft_portal.wall_id` like a plain wall), and `Delete` removes both
the wall and the floor pattern shape when `floor = "pattern"` produced one —
a terrain floor has the same can't-un-paint limitation as
`dungeondraft_terrain_fill`, and Delete warns instead. `dungeondraft_cave`
is the one exception to "can't un-paint": `dig_cave` stamps its `dig`
boolean directly into the cave bitmap, so Delete genuinely reverses a
stroke by re-running it with `dig` inverted (see the package doc comment on
`caveResource`) rather than warning.

`dungeondraft_terrain_fill` picks `paint_terrain` or `paint_path` instead
of `fill_terrain`/`fill_region` when you give it `x`+`y` (a single brush
dab) or `points`+`radius` together (a brush stroke) — see its own schema
description for the full four-way selection. `clear_caves` (wipe the whole
Caves layer) has no resource: like the camera commands, it has no created
identity to track or diff against.

## Map binding

The provider block's `map_file` (required) names the saved
`.dungeondraft_map` this state belongs to. Every command carries it, and
Dungeondraft refuses them (`wrong_map`) while any other map, or an unsaved
one, is open: state can never be read or changed against the wrong map. Save
the map to that path before the first apply.

You never need to save after an apply. Once the provider's edits have been
quiet for 3 seconds, Dungeondraft saves the map to `map_file` itself (also
after an interrupted apply), so the file always matches state. Opening or
creating another map first saves any edits still waiting.

## Levels

A level is identified by its **label**, which Dungeondraft saves; its numeric
id is renumbered by position whenever the map is reopened. Labels must be
unique within a map (`add_level` refuses a label already in use).

Every creator resource (everything in the table above except
`dungeondraft_level` itself) has a `level` attribute naming the level by
label. It defaults to the provider block's `level`; a resource with neither
fails at plan time. Changing it replaces the element (no command moves an
element between levels).

```hcl
provider "dungeondraft" {
  map_file = "C:/maps/keep.dungeondraft_map"
  level    = "Ground"
}

resource "dungeondraft_level" "upper" {
  label = "Upper Floor"
}

resource "dungeondraft_object" "torch" {
  asset = "res://.../torch.png"
  x     = 1024
  y     = 1024
  level = dungeondraft_level.upper.id # the level's id is its label
}
```

Each command that targets a level carries its label; the mod finds that one
level, runs the command on it, and restores the level that was active, so
no command can land on another's level and nothing is locked client-side.
An unknown label (`level_not_found`) or one shared by two levels
(`level_ambiguous`) is an error naming it.

**`dungeondraft_level` has no delete or rename counterpart in the bridge
protocol** — `add_level` is the entire level write surface. `Delete` only
warns; a `label` change replaces (destroys, in Terraform's model) but the
old level is never actually removed from the map, just orphaned. Treat this
resource as effectively append-only.

See `examples/level/` for a worked plan (**do not run it against a real
map** — for the reason above, it's a one-way door).

## Data sources

- `dungeondraft_status` — open map info (size, center, per-kind counts).
- `dungeondraft_assets` — search an asset category (`category`, `search`,
  `limit`) so a `.tf` can reference a discovered asset path instead of
  hardcoding one.
- `dungeondraft_asset_categories` — every category name this Dungeondraft
  install has, for discovering valid `dungeondraft_assets.category` values.
- `dungeondraft_map` — full `dump_state` of every element (id, kind, wall_id,
  position, asset) plus the raw JSON, for drift inspection and import tooling.

## Read / drift-detection model

`Read` calls `get_element` and syncs back every field that kind's descriptor
exposes, so hand-edits made directly in the Dungeondraft UI surface as drift on
the next `terraform plan`:

- **wall** — points, asset, loop, type, joint
- **portal** — wall_id, closed, radius, asset, position
- **object** — position (x/y), rotation, scale
- **light** — position (x/y), asset
- **roof** — points, width, type
- **text** — position (x/y), text, size, color, font
- **path** — points, smoothness
- **pattern** — points (only when the resource was declared with `points`)
- **room** — existence only (whether its wall id still exists); the wall's
  own geometry isn't synced the way a plain `dungeondraft_wall`'s is, and a
  painted terrain floor has no getter to drift-check against at all (same as
  `dungeondraft_terrain_fill`)

Fields the bridge cannot read back stay write-only (trusted from config):

- `object.z` / `object.sorting` / `object.color`, `light.energy` /
  `light.range` / `light.shadows` / `light.color`, and `pattern.rect` (a
  rectangle cannot be reconstructed from an arbitrary polygon, so
  `rect`-declared patterns are existence-checked only).

The only fields with a real in-place update path are `dungeondraft_object`'s
x/y (`move_element`) and rotation/scale/color (`modify_object`), and
`dungeondraft_text`'s x/y (`move_element`). Every other field is
`RequiresReplace` because the bridge has no reshape/modify API for it.

## Related docs

- `PROTOCOL.md` (repo root) — the full wire protocol and per-command semantics.
- `examples/demo/` and `examples/throne_room/` — worked `.tf` plans.
- `../ROADMAP.md` — open work.
