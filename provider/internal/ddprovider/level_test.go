package ddprovider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestElementResourcesUseLevelLabel(t *testing.T) {
	ctx := context.Background()
	for _, newResource := range New().(*ddProvider).Resources(ctx) {
		r := newResource()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: typeName}, &meta)
		var sresp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sresp)
		if _, has := sresp.Schema.Attributes["level_id"]; has {
			t.Errorf("%s still has level_id; levels are referenced by label (level)", meta.TypeName)
		}
		_, hasLevel := sresp.Schema.Attributes["level"]
		if isLevel := meta.TypeName == typeName+"_level"; hasLevel == isLevel {
			t.Errorf("%s: has level attribute = %v; every element resource needs one, the level resource none",
				meta.TypeName, hasLevel)
		}
	}
}

func TestPlanFailsWithoutLevel(t *testing.T) {
	_, diags := effectiveLevel(types.StringNull(), "")
	if !diags.HasError() || !strings.Contains(diags.Errors()[0].Detail(), "no level") {
		t.Fatalf("diags = %v; want an error saying there is no level", diags)
	}
}

func TestResourceLevelOverridesProviderLevel(t *testing.T) {
	for _, tc := range []struct {
		configured types.String
		provider   string
		want       string
	}{
		{types.StringNull(), "Ground", "Ground"},
		{types.StringValue("Upper"), "Ground", "Upper"},
		{types.StringValue("Upper"), "", "Upper"},
	} {
		got, diags := effectiveLevel(tc.configured, tc.provider)
		if diags.HasError() || got.ValueString() != tc.want {
			t.Errorf("effectiveLevel(%v, %q) = %v %v; want %q", tc.configured, tc.provider, got, diags, tc.want)
		}
	}
}

func TestOneClientShared(t *testing.T) {
	ctx := context.Background()
	p := New()
	var sresp provider.SchemaResponse
	p.Schema(ctx, provider.SchemaRequest{}, &sresp)
	objType := sresp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, attrType := range objType.AttributeTypes {
		values[name] = tftypes.NewValue(attrType, nil)
	}
	values["map_file"] = tftypes.NewValue(tftypes.String, `C:\maps\a.dungeondraft_map`)
	config := tfsdk.Config{Schema: sresp.Schema, Raw: tftypes.NewValue(objType, values)}

	var resp provider.ConfigureResponse
	p.Configure(ctx, provider.ConfigureRequest{Config: config}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Configure: %v", resp.Diagnostics)
	}
	data, res := resp.DataSourceData.(*providerData), resp.ResourceData.(*providerData)
	if data.client != res.client {
		t.Fatal("data sources and resources got different bridge clients; one client must serve both")
	}
}
