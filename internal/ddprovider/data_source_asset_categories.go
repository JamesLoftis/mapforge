package ddprovider

import (
	"context"

	"dungeondraft-mcp/provider/internal/bridge"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// assetCategoriesDataSource mirrors list_asset_categories: the full list of
// asset category names (Objects, Walls, Paths, Terrain, Lights, Portals,
// Roofs, Patterns, ...) this Dungeondraft install actually has, so a .tf
// file can enumerate what dungeondraft_assets.category values are valid
// instead of guessing/hardcoding them.
type assetCategoriesDataSource struct {
	client *bridge.Client
}

func NewAssetCategoriesDataSource() datasource.DataSource {
	return &assetCategoriesDataSource{}
}

func (d *assetCategoriesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *assetCategoriesDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = typeName + "_asset_categories"
}

func (d *assetCategoriesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Every asset category this Dungeondraft install has (mirrors list_asset_categories).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Constant; this data source has no inputs to key on.",
			},
			"categories": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Category names, e.g. Objects, Walls, Paths, Terrain, Lights, Portals, Roofs, Patterns.",
			},
		},
	}
}

type assetCategoriesModel struct {
	ID         types.String `tfsdk:"id"`
	Categories types.List   `tfsdk:"categories"`
}

func (d *assetCategoriesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The data source was not given a bridge client (missing provider block?).")
		return
	}
	result, err := d.client.Request(ctx, "list_asset_categories", nil)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list Dungeondraft asset categories", err.Error())
		return
	}

	var model assetCategoriesModel
	model.ID = types.StringValue("asset_categories")
	if raw, ok := result["categories"].([]any); ok {
		model.Categories = stringListToAttr(ctx, raw)
	} else {
		model.Categories, _ = types.ListValueFrom(ctx, types.StringType, []string{})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
