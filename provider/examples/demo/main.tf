# Converted from server/demo_build.py: a small visible scene built around the
# map center, exercised on a scratch map. Unlike the script, the door is
# mounted on the specific wall the plan just drew (wall_id), so this is fully
# deterministic — no nearest-wall search.
#
# The discovered asset paths mirror what the script's find_asset() would
# resolve on this install (dungeondraft_assets data sources, with an empty
# search as the last-resort "first asset in the category" fallback).

data "dungeondraft_status" "map" {}

locals {
  cx = data.dungeondraft_status.map.map_center[0]
  cy = data.dungeondraft_status.map.map_center[1]
  h  = 512 # half-size of the room in woxels

  grass = try(
    data.dungeondraft_assets.grass.assets[0],
    data.dungeondraft_assets.grass_fallback.assets[0],
    null,
  )
  wall = try(
    data.dungeondraft_assets.wall.assets[0],
    data.dungeondraft_assets.wall_fallback.assets[0],
    null,
  )
  door = try(
    data.dungeondraft_assets.door.assets[0],
    data.dungeondraft_assets.door_fallback.assets[0],
    null,
  )
  roof  = data.dungeondraft_assets.roof.assets[0]
  table = data.dungeondraft_assets.table.assets[0]
  chair = data.dungeondraft_assets.chair.assets[0]
}

data "dungeondraft_assets" "grass" {
  category = "Terrain"
  search   = "grass"
  limit    = 1
}
data "dungeondraft_assets" "grass_fallback" {
  category = "Terrain"
  limit    = 1
}
data "dungeondraft_assets" "wall" {
  category = "Walls"
  search   = "stone"
  limit    = 1
}
data "dungeondraft_assets" "wall_fallback" {
  category = "Walls"
  limit    = 1
}
data "dungeondraft_assets" "door" {
  category = "Portals"
  search   = "door"
  limit    = 1
}
data "dungeondraft_assets" "door_fallback" {
  category = "Portals"
  limit    = 1
}
data "dungeondraft_assets" "roof" {
  category = "Roofs"
  search   = "tile"
  limit    = 1
}
data "dungeondraft_assets" "table" {
  category = "Objects"
  search   = "table"
  limit    = 1
}
data "dungeondraft_assets" "chair" {
  category = "Objects"
  search   = "chair"
  limit    = 1
}

# Whole-map grass base (fill_terrain on slot 0).
resource "dungeondraft_terrain_fill" "grass" {
  slot  = 0
  asset = local.grass
}

# The room: a looped stone wall.
resource "dungeondraft_wall" "room" {
  points = [
    [local.cx - local.h, local.cy - local.h],
    [local.cx + local.h, local.cy - local.h],
    [local.cx + local.h, local.cy + local.h],
    [local.cx - local.h, local.cy + local.h],
  ]
  asset = local.wall
  loop  = true
  joint = 1
}

# A door centered on the south wall, mounted explicitly on the room wall.
# wall_id is what makes this deterministic (add_portal's A4 change).
resource "dungeondraft_portal" "door" {
  asset   = local.door
  wall_id = dungeondraft_wall.room.id
  x       = local.cx           # on the south wall's line: Read reports the
  y       = local.cy + local.h # snapped position, so an off-wall x/y drifts
  radius  = 64
}

resource "dungeondraft_object" "table" {
  asset = local.table
  x     = local.cx
  y     = local.cy
}

resource "dungeondraft_object" "chair" {
  asset = local.chair
  x     = local.cx + 120
  y     = local.cy
}

resource "dungeondraft_light" "room_light" {
  x       = local.cx
  y       = local.cy - 150
  energy  = 1.5
  range   = 1.5
  shadows = true
}

# Off to the side so it does not hide the room. add_roof takes the RIDGE
# (peak line), not a footprint — one width_scale for the eaves.
resource "dungeondraft_roof" "demo_roof" {
  points = [
    [local.cx + local.h * 3 - local.h, local.cy],
    [local.cx + local.h * 3 + local.h, local.cy],
  ]
  asset = local.roof
  width = 256
  type  = 0
}

resource "dungeondraft_text" "label" {
  text = "MCP Bridge Demo"
  x    = local.cx
  y    = local.cy - local.h - 120
  size = 48
}