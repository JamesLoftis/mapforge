package ddprovider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
)

func TestProviderRequiresMapFile(t *testing.T) {
	var resp provider.SchemaResponse
	New().Schema(context.Background(), provider.SchemaRequest{}, &resp)
	attr, ok := resp.Schema.Attributes["map_file"].(schema.StringAttribute)
	if !ok || !attr.Required {
		t.Fatalf("provider map_file = %#v; want a required string attribute", resp.Schema.Attributes["map_file"])
	}
}
