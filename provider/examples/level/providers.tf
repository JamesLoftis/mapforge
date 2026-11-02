terraform {
  required_providers {
    dungeondraft = {
      source = "registry.terraform.io/jamesloftis/dungeondraft"
    }
  }
}

provider "dungeondraft" {
  host = "127.0.0.1"
  port = 8787
}
