// terraform-provider-dungeondraft — a Terraform provider that declaratively
// builds maps in a running Dungeondraft instance.
package main

import (
	"context"

	"dungeondraft-mcp/provider/internal/ddprovider"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

func main() {
	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/jamesloftis/dungeondraft",
	}
	providerserver.Serve(context.Background(), ddprovider.New, opts)
}
