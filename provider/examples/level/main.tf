# Exercises dungeondraft_level and every creator resource's level_id
# attribute. CAUTION if you actually run this against a real map: there is
# no delete_level command in the bridge protocol at all (see
# resources_level.go's package doc comment) — `terraform destroy` cannot
# remove the level this creates. Run it on a scratch map, not a real one.

data "dungeondraft_status" "map" {}

locals {
  cx = data.dungeondraft_status.map.map_center[0]
  cy = data.dungeondraft_status.map.map_center[1]

  crate = try(
    data.dungeondraft_assets.crate.assets[0],
    data.dungeondraft_assets.crate_fallback.assets[0],
    null,
  )
}

data "dungeondraft_assets" "crate" {
  category = "Objects"
  search   = "crate"
  limit    = 1
}
data "dungeondraft_assets" "crate_fallback" {
  category = "Objects"
  limit    = 1
}

resource "dungeondraft_level" "upper" {
  label = "Upper Floor"
}

# Lands on whichever level is currently active (unchanged, pre-existing
# behavior) — no level_id set.
resource "dungeondraft_object" "ground_crate" {
  asset = local.crate
  x     = local.cx
  y     = local.cy
}

# Lands on dungeondraft_level.upper specifically, regardless of what's
# currently active or what any other resource in this plan targets.
resource "dungeondraft_object" "upper_crate" {
  asset    = local.crate
  x        = local.cx
  y        = local.cy
  level_id = dungeondraft_level.upper.id
}
