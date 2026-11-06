package ddprovider

import (
	"context"

	bridge "mapforge/bridgeclient"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// assetsDataSource mirrors list_assets: search an asset category so a .tf file
// can reference data.dungeondraft_assets.*.assets[i] instead of hardcoding a
// resource path (e.g. res://textures/objects/.../table.png).
type assetsDataSource struct {
	client *bridge.Client
}

func NewAssetsDataSource() datasource.DataSource {
	return &assetsDataSource{}
}

func (d *assetsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	pd, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider configuration", "Provider data did not carry a bridge client.")
		return
	}
	d.client = pd.client
}

func (d *assetsDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = typeName + "_assets"
}

func (d *assetsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Search a Dungeondraft asset category (mirrors list_assets).",
		Attributes: map[string]schema.Attribute{
			"category": schema.StringAttribute{
				Required:    true,
				Description: "Asset category; one of " + "Objects, Walls, Paths, Terrain, Lights, Portals, Roofs, ...",
			},
			"search": schema.StringAttribute{
				Optional:    true,
				Description: "Case-insensitive substring filter on the asset path.",
			},
			"limit": schema.Int64Attribute{
				Optional:    true,
				Description: "Maximum number of matches (default 100).",
			},
			"assets": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Matching asset paths.",
			},
		},
	}
}

type assetsModel struct {
	Category types.String `tfsdk:"category"`
	Search   types.String `tfsdk:"search"`
	Limit    types.Int64  `tfsdk:"limit"`
	Assets   types.List   `tfsdk:"assets"`
}

func (d *assetsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model assetsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The data source was not given a bridge client (missing provider block?).")
		return
	}
	params := map[string]any{"category": model.Category.ValueString()}
	if !model.Search.IsNull() && model.Search.ValueString() != "" {
		params["search"] = model.Search.ValueString()
	}
	limit := int64(100)
	if !model.Limit.IsNull() {
		limit = model.Limit.ValueInt64()
	}
	params["limit"] = limit

	result, err := d.client.Request(ctx, "list_assets", params)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list Dungeondraft assets", err.Error())
		return
	}

	paths := []string{}
	if raw, ok := result["assets"].([]any); ok {
		for _, a := range raw {
			if s, ok := a.(string); ok {
				paths = append(paths, s)
			}
		}
	}
	model.Assets, _ = types.ListValueFrom(ctx, types.StringType, paths)

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
