// Package ddprovider is the Terraform provider for Dungeondraft. It talks
// directly to the bridge mod's TCP/JSON wire protocol (see PROTOCOL.md) — no
// MCP layer — from a single static Go binary.
package ddprovider

import (
	"context"

	bridge "dungeondraft-mcp/bridgeclient"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	version  = "0.1.0"
	typeName = "dungeondraft"
)

// ddProvider is the provider entry point.
type ddProvider struct{}

// New returns the provider instance.
func New() provider.Provider {
	return &ddProvider{}
}

func (p *ddProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = typeName
	resp.Version = version
}

// ddProviderModel mirrors the provider block: host/port config, matching the
// bridge's defaults (and DD_BRIDGE_HOST / DD_BRIDGE_PORT).
type ddProviderModel struct {
	Host    types.String `tfsdk:"host"`
	Port    types.Int64  `tfsdk:"port"`
	MapFile types.String `tfsdk:"map_file"`
	Level   types.String `tfsdk:"level"`
}

func (p *ddProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Declaratively build maps in a running Dungeondraft instance.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Optional:    true,
				Description: "Bridge host (default 127.0.0.1).",
			},
			"port": schema.Int64Attribute{
				Optional:    true,
				Description: "Bridge port (default 8787).",
			},
			"map_file": schema.StringAttribute{
				Required: true,
				Description: "The .dungeondraft_map this state belongs to (absolute path). Every command " +
					"names it, and Dungeondraft refuses commands while a different map is open.",
				Validators: []validator.String{mapFilePath{}},
			},
			"level": schema.StringAttribute{
				Optional: true,
				Description: "Label of the level elements are created on unless a resource sets its own level. " +
					"A resource with neither fails at plan time.",
			},
		},
	}
}

// providerData is stashed in the DataSourceData/ResourceData for each call.
type providerData struct {
	client *bridge.Client
	level  string // default level label for resources without their own
}

func (p *ddProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var model ddProviderModel
	diags := req.Config.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	host := model.Host.ValueString()
	if host == "" {
		host = "127.0.0.1"
	}
	port := 8787
	if !model.Port.IsNull() && !model.Port.IsUnknown() {
		port = int(model.Port.ValueInt64())
	}
	client := bridge.New(host, port)
	client.MapFile = model.MapFile.ValueString()
	data := &providerData{client: client, level: model.Level.ValueString()}
	resp.DataSourceData = data
	resp.ResourceData = data
}

func (p *ddProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewStatusDataSource,
		NewAssetsDataSource,
		NewAssetCategoriesDataSource,
		NewMapDataSource,
	}
}

func (p *ddProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewWallResource,
		NewPortalResource,
		NewObjectResource,
		NewLightResource,
		NewRoofResource,
		NewTextResource,
		NewPatternResource,
		NewPathResource,
		NewTerrainFillResource,
		NewRoomResource,
		NewLevelResource,
		NewCaveResource,
	}
}
