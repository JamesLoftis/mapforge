terraform {
  required_providers {
    dungeondraft = {
      source = "registry.terraform.io/jamesloftis/dungeondraft"
    }
  }
}

# The provider talks to a running Dungeondraft with the TF Bridge Agent mod
# enabled and a map open. scripts/install.ps1 at the repo root installs it.
provider "dungeondraft" {
  host = "127.0.0.1"
  port = 8787

  # State belongs to one saved map; Dungeondraft refuses the provider's
  # commands while any other (or an unsaved) map is open.
  map_file = var.map_file

  # Elements go on this level unless a resource sets its own.
  level = "Ground"
}

variable "map_file" {
  type        = string
  description = "Absolute path of the saved .dungeondraft_map this example builds on."
}