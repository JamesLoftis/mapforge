terraform {
  required_providers {
    dungeondraft = {
      source = "registry.terraform.io/jamesloftis/dungeondraft"
    }
  }
}

# The provider talks to a running Dungeondraft with the MCP Bridge mod enabled
# and a map open. See ../../README.md for how to test against the local
# build (dev_overrides or a filesystem_mirror) before it is published.
provider "dungeondraft" {
  host = "127.0.0.1"
  port = 8787
}
