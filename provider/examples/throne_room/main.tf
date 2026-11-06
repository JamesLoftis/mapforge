# ROADMAP Part B acceptance: reproduce the throne-room structural fix as a
# .tf file instead of a replay_ops.py run.
#
# The old workflow re-created a damaged wall and re-mounted its portals, but
# the portals snapped to whatever wall was nearest at the time — occasionally
# the wrong one. Here the mounting is explicit: every portal pins its wall_id
# to a dungeondraft_wall resource, so a rebuild can never silently re-home a
# portal elsewhere.
#
# Worked example against the live map:
#   1. terraform plan  # see data.dungeondraft_map.walls below: which id owns
#                      # the damaged throne-room segment (match asset + a point)
#   2. terraform import 'dungeondraft_wall.throne_wall' <id>
#   3. terraform apply  # wall replaced if points drift; portals re-mount on it
#   4. terraform destroy # detaches the portals, then deletes the wall

data "dungeondraft_map" "map" {}

# Discover the wall that owns a throne-room segment (throne area in the
# north-west of the map in this session; the asset + a point makes it unique).
locals {
  throne_point = [20480, 19968] # a woxel the damaged segment ran through
  throne_wall = [for e in data.dungeondraft_map.map.elements :
  e if e.kind == "wall" && contains(e.position, local.throne_point)][0]
}

data "dungeondraft_assets" "portals" {
  category = "Portals"
  search   = "door"
  limit    = 10
}

# After `terraform import`, this resource manages the live wall. If its points
# differ from the state file (a hand-edit, or a previous replay), the plan will
# show a drift diff — and because all shape fields RequiresReplace, applying
# rebuilds it from scratch.
resource "dungeondraft_wall" "throne_wall" {
  points = local.throne_wall.points
  asset  = local.throne_wall.asset
  loop   = true
  joint  = 1
}

# The door and window re-mounted for good. wall_id points at the resource, so
# whatever the rebuild does these can never bind to a different wall.
resource "dungeondraft_portal" "throne_door" {
  asset   = "res://textures/portals/door_04.png"
  wall_id = dungeondraft_wall.throne_wall.id
  x       = local.throne_point[0]
  y       = local.throne_point[1]
  radius  = 128
  closed  = false
}

resource "dungeondraft_portal" "throne_window" {
  asset   = "res://textures/portals/window_02.png"
  wall_id = dungeondraft_wall.throne_wall.id
  # The middle of the wall's first segment.
  x      = (local.throne_wall.points[0][0] + local.throne_wall.points[1][0]) / 2
  y      = (local.throne_wall.points[0][1] + local.throne_wall.points[1][1]) / 2
  radius = 64
}