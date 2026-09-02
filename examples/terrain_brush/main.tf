# Exercises dungeondraft_terrain_fill's brush modes (paint_terrain via x/y,
# paint_path via points+radius) and dungeondraft_cave, alongside the
# existing fill_region/fill_terrain modes for contrast.

data "dungeondraft_status" "map" {}

locals {
  cx = data.dungeondraft_status.map.map_center[0]
  cy = data.dungeondraft_status.map.map_center[1]

  dirt = try(
    data.dungeondraft_assets.dirt.assets[0],
    data.dungeondraft_assets.dirt_fallback.assets[0],
    null,
  )
}

data "dungeondraft_assets" "dirt" {
  category = "Terrain"
  search   = "dirt"
  limit    = 1
}
data "dungeondraft_assets" "dirt_fallback" {
  category = "Terrain"
  limit    = 1
}

# paint_terrain: a single brush dab (x/y set, no points/rect).
resource "dungeondraft_terrain_fill" "dab" {
  slot   = 1
  asset  = local.dirt
  x      = local.cx - 600
  y      = local.cy
  radius = 96
}

# paint_path: a brush stroke along a centerline (points + radius together).
resource "dungeondraft_terrain_fill" "trail" {
  slot = 1
  points = [
    [local.cx - 400, local.cy + 300],
    [local.cx, local.cy + 300],
    [local.cx + 400, local.cy + 200],
  ]
  radius = 48
  rate   = 0.8
}

# dig_cave: carve a small pocket. Delete genuinely reverses this stroke
# (re-runs it with dig=false) rather than just warning, unlike terrain_fill.
resource "dungeondraft_cave" "pocket" {
  points = [
    [local.cx + 600, local.cy - 200],
    [local.cx + 800, local.cy - 200],
  ]
  radius = 128
}
