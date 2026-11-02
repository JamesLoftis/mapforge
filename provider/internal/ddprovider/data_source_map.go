package ddprovider

import (
	"context"
	"encoding/json"

	"dungeondraft-mcp/provider/internal/bridge"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// mapDataSource mirrors the bridge's dump_state tool: full geometry for every
// element in one call (ROADMAP Part B A3). Primarily for drift inspection and
// terraform import tooling.
type mapDataSource struct {
	client *bridge.Client
}

func NewMapDataSource() datasource.DataSource {
	return &mapDataSource{}
}

func (d *mapDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *mapDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = typeName + "_map"
}

var elementAttrTypes = map[string]attr.Type{
	"id":       types.Int64Type,
	"kind":     types.StringType,
	"wall_id":  types.Int64Type,
	"position": types.ListType{ElemType: types.Int64Type},
	"asset":    types.StringType,
}

func (d *mapDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Full state dump of the open map (dump_state): every element with its geometry.",
		Attributes: map[string]schema.Attribute{
			"element_count": schema.Int64Attribute{Computed: true, Description: "Total element count."},
			"elements": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Every element, one object per row (id, kind, wall_id, position, asset).",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":       schema.Int64Attribute{Computed: true},
						"kind":     schema.StringAttribute{Computed: true},
						"wall_id":  schema.Int64Attribute{Computed: true, Description: "Mounting wall for wall_portal kinds; 0 otherwise."},
						"position": schema.ListAttribute{Computed: true, ElementType: types.Int64Type},
						"asset":    schema.StringAttribute{Computed: true},
					},
				},
			},
			"json": schema.StringAttribute{
				Computed:    true,
				Description: "The raw dump_state payload as JSON, for full-fidelity inspection.",
			},
		},
	}
}

type mapModel struct {
	Count    types.Int64  `tfsdk:"element_count"`
	Elements types.List   `tfsdk:"elements"`
	JSON     types.String `tfsdk:"json"`
}

func (d *mapDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model mapModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The data source was not given a bridge client (missing provider block?).")
		return
	}
	result, err := d.client.Request(ctx, "dump_state", nil)
	if err != nil {
		resp.Diagnostics.AddError("Failed to dump Dungeondraft map state", err.Error())
		return
	}

	model.Count = types.Int64Value(numAsInt64(result["count"]))

	var elements []attr.Value
	if rawEls, ok := result["elements"].([]any); ok {
		for _, raw := range rawEls {
			el, isEl := raw.(map[string]any)
			if !isEl {
				continue
			}
			fields := map[string]attr.Value{
				"id":       types.Int64Value(numAsInt64(el["id"])),
				"kind":     types.StringValue(fieldAsString(el["kind"])),
				"wall_id":  types.Int64Value(numAsInt64(el["wall_id"])),
				"position": listOfNums(ctx, el["position"]),
				"asset":    types.StringValue(fieldAsString(el["asset"])),
			}
			obj, _ := types.ObjectValue(elementAttrTypes, fields)
			elements = append(elements, obj)
		}
	}
	model.Elements, _ = types.ListValue(types.ObjectType{AttrTypes: elementAttrTypes}, elements)

	if raw, err := json.Marshal(result); err == nil {
		model.JSON = types.StringValue(string(raw))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func fieldAsString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
