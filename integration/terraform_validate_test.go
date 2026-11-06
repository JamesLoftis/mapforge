//go:build integration

package integration

import (
	"strings"
	"testing"
)

// Spec goal 5's negative tests, at plan time: a config missing a required
// value, or giving one out of range, fails terraform validate and names the
// resource, before anything reaches Dungeondraft.
const badConfig = `
terraform {
  required_providers {
    dungeondraft = { source = "registry.terraform.io/jamesloftis/dungeondraft" }
  }
}
variable "map_file" { type = string }
provider "dungeondraft" {
  map_file = var.map_file
  level    = "Ground"
}
resource "dungeondraft_object" "no_x" {
  asset = "res://a.png"
  y     = 100
}
resource "dungeondraft_light" "negative_energy" {
  x      = 100
  y      = 100
  energy = -1
}
resource "dungeondraft_portal" "no_y" {
  asset = "res://door.png"
  x     = 100
}
resource "dungeondraft_pattern" "no_category" {
  asset = "res://tile.png"
  rect  = [0, 0, 256, 256]
}
resource "dungeondraft_room" "no_floor_asset" {
  rect           = [0, 0, 512, 512]
  floor_category = "Simple Tiles"
}
resource "dungeondraft_text" "named_colour" {
  text  = "hello"
  x     = 100
  y     = 100
  color = "red"
}
`

func TestTerraformValidateRejectsBadConfig(t *testing.T) {
	tf := newTerraformWith(t, map[string]string{"main.tf": badConfig})
	tf.mapFile = scratchMap(t)
	tf.run(t, "init", "-input=false")
	code, out := tf.output(t, "validate", "-no-color")
	if code == 0 {
		t.Fatalf("terraform validate accepted a config with six bad resources:\n%s", out)
	}
	// Terraform names each one as `in resource "<type>" "<name>"`.
	for _, name := range []string{"no_x", "negative_energy", "no_y", "no_category", "no_floor_asset",
		"named_colour"} {
		if !strings.Contains(out, `"`+name+`"`) {
			t.Errorf("terraform validate did not name resource %q:\n%s", name, lastLines(out, 40))
		}
	}
}
