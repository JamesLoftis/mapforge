# Characterization fixture: one of every non-gated resource type, every
# optional attribute set explicitly, on a 20x20-tile scratch map (5120
# woxels square). dungeondraft_terrain_fill and dungeondraft_cave are gated
# in the spec and left out.

terraform {
  required_providers {
    dungeondraft = {
      source = "registry.terraform.io/jamesloftis/dungeondraft"
    }
  }
}

variable "bridge_port" {
  type    = number
  default = 8787
}

provider "dungeondraft" {
  host = "127.0.0.1"
  port = var.bridge_port
}

data "dungeondraft_assets" "object" {
  category = "Objects"
  limit    = 1
}
data "dungeondraft_assets" "wall" {
  category = "Walls"
  limit    = 1
}
data "dungeondraft_assets" "portal" {
  category = "Portals"
  limit    = 1
}
data "dungeondraft_assets" "light" {
  category = "Lights"
  limit    = 1
}
data "dungeondraft_assets" "path" {
  category = "Paths"
  limit    = 1
}
data "dungeondraft_assets" "roof" {
  category = "Roofs"
  limit    = 1
}
data "dungeondraft_assets" "pattern" {
  category = "Patterns"
  limit    = 1
}
data "dungeondraft_assets" "tile" {
  category = "Simple Tiles"
  limit    = 1
}

resource "dungeondraft_level" "upper" {
  label = "Upper"
}

resource "dungeondraft_wall" "wall" {
  points = [[600, 600], [1600, 600]]
  asset  = data.dungeondraft_assets.wall.assets[0]
  loop   = false
  type   = 0
  joint  = 1
  color  = "#ffffff"
  shadow = true
}

resource "dungeondraft_portal" "door" {
  asset         = data.dungeondraft_assets.portal.assets[0]
  x             = 1100
  y             = 600
  wall_id       = dungeondraft_wall.wall.id
  radius        = 64
  closed        = false
  flip          = false
  mount         = "wall"
  snap_max      = 256
  rotation      = 0
  fallback_free = true
}

resource "dungeondraft_object" "object" {
  asset    = data.dungeondraft_assets.object.assets[0]
  x        = 1000
  y        = 1000
  rotation = 0
  scale    = 1
  color    = "#ffffff"
  sorting  = 0
  z        = 200
}

resource "dungeondraft_object" "upper_object" {
  asset    = data.dungeondraft_assets.object.assets[0]
  x        = 1000
  y        = 1000
  rotation = 0
  scale    = 1
  color    = "#ffffff"
  sorting  = 0
  z        = 200
  level_id = dungeondraft_level.upper.id
}

resource "dungeondraft_light" "light" {
  asset   = data.dungeondraft_assets.light.assets[0]
  x       = 1500
  y       = 1500
  color   = "#ffe6b3"
  energy  = 1
  range   = 1
  shadows = true
}

resource "dungeondraft_path" "path" {
  asset      = data.dungeondraft_assets.path.assets[0]
  points     = [[500, 2000], [1800, 2400]]
  smoothness = 0
  width      = 1
  layer      = 0
  sorting    = 0
}

resource "dungeondraft_roof" "roof" {
  asset   = data.dungeondraft_assets.roof.assets[0]
  points  = [[2000, 3200], [3000, 3200]]
  width   = 256
  type    = 0
  sorting = 0
}

resource "dungeondraft_text" "text" {
  text  = "characterize"
  x     = 3000
  y     = 1000
  size  = 40
  color = "#000000"
  font  = "Libre Baskerville"
}

resource "dungeondraft_pattern" "pattern" {
  asset    = data.dungeondraft_assets.pattern.assets[0]
  category = "Patterns"
  rect     = [2000, 2000, 512, 512]
  color    = "#808080"
  rotation = 0
  z        = -100
}

resource "dungeondraft_room" "room" {
  rect           = [3500, 3500, 1024, 1024]
  wall_asset     = data.dungeondraft_assets.wall.assets[0]
  wall_type      = 0
  wall_joint     = 1
  wall_shadow    = true
  floor          = "pattern"
  floor_asset    = data.dungeondraft_assets.tile.assets[0]
  floor_category = "Simple Tiles"
  floor_color    = "#ffffff"
}
