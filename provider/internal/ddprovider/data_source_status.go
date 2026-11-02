package ddprovider

import (
	"context"

	"dungeondraft-mcp/provider/internal/bridge"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// statusDataSource mirrors the bridge's get_status tool: map presence, size and
// center in woxels, per-kind element counts, and the active level/tool.
type statusDataSource struct {
	client *bridge.Client
}

func NewStatusDataSource() datasource.DataSource {
	return &statusDataSource{}
}

func (d *statusDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return // retry/skip; Terraform may Configure before data sources run
	}
	pd, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider configuration", "Provider data did not carry a bridge client.")
		return
	}
	d.client = pd.client
}

func (d *statusDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = typeName + "_status"
}

func (d *statusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Report the currently open Dungeondraft map (reading the live bridge).",
		Attributes: map[string]schema.Attribute{
			"map_open": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether a map is currently open.",
			},
			"level_id":    schema.Int64Attribute{Computed: true},
			"level_count": schema.Int64Attribute{Computed: true},
			"map_size_woxels": schema.ListAttribute{
				Computed:    true,
				ElementType: types.Int64Type,
				Description: "Map dimensions in woxels, [width, height]; 256 woxels = 1 grid cell.",
			},
			"map_center": schema.ListAttribute{
				Computed:    true,
				ElementType: types.Int64Type,
				Description: "Default placement point, [x, y] in woxels.",
			},
			"counts": schema.SingleNestedAttribute{
				Computed:    true,
				Description: "Per-kind element counts (wall-mounted portals fold into portals).",
				Attributes: map[string]schema.Attribute{
					"objects": schema.Int64Attribute{Computed: true},
					"walls":   schema.Int64Attribute{Computed: true},
					"lights":  schema.Int64Attribute{Computed: true},
					"paths":   schema.Int64Attribute{Computed: true},
					"portals": schema.Int64Attribute{Computed: true},
					"roofs":   schema.Int64Attribute{Computed: true},
					"texts":   schema.Int64Attribute{Computed: true},
				},
			},
			"active_tool": schema.StringAttribute{Computed: true},
		},
	}
}

type statusModel struct {
	MapOpen    types.Bool   `tfsdk:"map_open"`
	LevelID    types.Int64  `tfsdk:"level_id"`
	LevelCount types.Int64  `tfsdk:"level_count"`
	MapSize    types.List   `tfsdk:"map_size_woxels"`
	MapCenter  types.List   `tfsdk:"map_center"`
	Counts     types.Object `tfsdk:"counts"`
	ActiveTool types.String `tfsdk:"active_tool"`
}

var countsAttrTypes = map[string]attr.Type{
	"objects": types.Int64Type,
	"walls":   types.Int64Type,
	"lights":  types.Int64Type,
	"paths":   types.Int64Type,
	"portals": types.Int64Type,
	"roofs":   types.Int64Type,
	"texts":   types.Int64Type,
}

func (d *statusDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model statusModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The data source was not given a bridge client (missing provider block?).")
		return
	}
	result, err := d.client.Request(ctx, "get_status", nil)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read Dungeondraft status", err.Error())
		return
	}

	model.MapOpen = types.BoolValue(result["map_open"] == true)
	model.LevelID = types.Int64Value(numAsInt64(result["level_id"]))
	model.LevelCount = types.Int64Value(numAsInt64(result["level_count"]))
	model.MapSize = listOfNums(ctx, result["map_size_woxels"])
	model.MapCenter = listOfNums(ctx, result["map_center"])
	if at, ok := result["active_tool"].(string); ok {
		model.ActiveTool = types.StringValue(at)
	}

	counts := map[string]attr.Value{}
	if cm, ok := result["counts"].(map[string]any); ok {
		for _, k := range countsKindOrder {
			counts[k] = types.Int64Value(numAsInt64(cm[k]))
		}
	}
	model.Counts, _ = types.ObjectValue(countsAttrTypes, counts)

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

var countsKindOrder = []string{"objects", "walls", "lights", "paths", "portals", "roofs", "texts"}
