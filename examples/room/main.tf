# Exercises dungeondraft_asset_categories and dungeondraft_room in one small
# plan: a wall+pattern-floor room built in a single call, with a door mounted
# deterministically on the room's own wall_id — the same wall_id pattern
# examples/demo and examples/throne_room use for dungeondraft_wall, just
# sourced from dungeondraft_room instead of a separate dungeondraft_wall.

data "dungeondraft_status" "map" {}

# Not consumed by anything below (build_room's category defaults are enough)
# — this just proves the data source resolves against a live bridge, and
# gives a starting point for picking a real floor_category by hand.
data "dungeondraft_asset_categories" "all" {}

locals {
  cx = data.dungeondraft_status.map.map_center[0]
  cy = data.dungeondraft_status.map.map_center[1]
  h  = 384 # half-size of the room in woxels

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
  floor = try(
    data.dungeondraft_assets.floor.assets[0],
    data.dungeondraft_assets.floor_fallback.assets[0],
    null,
  )
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
data "dungeondraft_assets" "floor" {
  category = "Simple Tiles"
  search   = "wood"
  limit    = 1
}
data "dungeondraft_assets" "floor_fallback" {
  category = "Simple Tiles"
  limit    = 1
}

resource "dungeondraft_room" "study" {
  rect = [local.cx - local.h, local.cy - local.h, local.h * 2, local.h * 2]

  wall_asset = local.wall
  wall_joint = 1

  floor          = "pattern"
  floor_asset    = local.floor
  floor_category = "Simple Tiles"
}

# Mounted on the room's own wall — dungeondraft_room.id is the wall's
# element id, exactly like a plain dungeondraft_wall's id would be.
resource "dungeondraft_portal" "study_door" {
  asset   = local.door
  wall_id = dungeondraft_room.study.id
  radius  = 64
}
