package ddprovider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// dungeondraft_terrain_fill paints terrain (fill_terrain for the whole level,
// fill_region for a rect/polygon). Terrain has no object id, so the state id
// is a deterministic hash of the desired params — re-applying the same plan is
// idempotent, and any change re-paints (recovers to desired). Delete cannot
// un-paint: it warns instead.

type terrainFillResource struct{ *nodeResourceBase }

func NewTerrainFillResource() resource.Resource { return &terrainFillResource{&nodeResourceBase{}} }

func (r *terrainFillResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_terrain_fill"
}

type terrainFillModel struct {
	ID     types.String  `tfsdk:"id"`
	Slot   types.Int64   `tfsdk:"slot"`
	Asset  types.String  `tfsdk:"asset"`
	Rect   types.List    `tfsdk:"rect"`
	Points types.List    `tfsdk:"points"`
	Rate   types.Float64 `tfsdk:"rate"`
}

func (r *terrainFillResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Paint terrain into a slot. Whole level by default; give rect or points to paint a region. Re-applies on change.",
		Attributes: map[string]schema.Attribute{
			"id":     idAttribute(),
			"slot":   schema.Int64Attribute{Optional: true, Description: "Terrain slot index (default 0)."},
			"asset":  schema.StringAttribute{Optional: true, Description: "Terrain asset to assign to the slot first."},
			"rect":   schema.ListAttribute{Optional: true, ElementType: types.Int64Type, Description: "[x, y, w, h] region to fill; omit for whole level."},
			"points": schema.ListAttribute{Optional: true, ElementType: pointElemType, Description: "[[x,y], ...] polygon region to fill; alternative to rect."},
			"rate":   schema.Float64Attribute{Optional: true, Description: "Paint strength 0..1 (default 1)."},
		},
	}
}

func (r *terrainFillResource) apply(ctx context.Context, m *terrainFillModel) (string, diag.Diagnostics) {
	var zero string
	if r.client == nil {
		return zero, diagErrf("Provider not configured", "The bridge client was not available.")
	}
	slot := int64(0)
	if !m.Slot.IsNull() {
		slot = m.Slot.ValueInt64()
	}
	params := map[string]any{"slot": slot}
	if !m.Asset.IsNull() && m.Asset.ValueString() != "" {
		params["asset"] = m.Asset.ValueString()
	}
	// Establish the desired shape: rect/polygon = region fill, else whole map.
	haveShape := false
	if !m.Rect.IsNull() && !m.Rect.IsUnknown() {
		if r := attrToNums(ctx, m.Rect); r != nil {
			params["rect"] = r
			haveShape = true
		}
	} else if !m.Points.IsNull() && !m.Points.IsUnknown() {
		if pts, ok, diags := attrToPointList(ctx, m.Points); ok {
			params["points"] = pts
			haveShape = true
		} else {
			return zero, diags
		}
	}
	cmd := "fill_terrain"
	if haveShape {
		cmd = "fill_region"
	}
	if !m.Rate.IsNull() {
		if haveShape {
			params["rate"] = m.Rate.ValueFloat64()
		} else {
			return zero, diagErrf("rate ignored", "rate only applies to region fills (whole-level fill is full-strength).")
		}
	}
	if _, err := r.client.Request(ctx, cmd, params); err != nil {
		return zero, diagErrf("Failed to run "+cmd, "%v", err)
	}
	// Deterministic id so repeated applies (and quantity changes) are stable.
	return fmt.Sprintf("terrain.%d.%s", slot, shapeKey(ctx, m)), nil
}

// shapeKey fingerprints the fill geometry for the state id.
func shapeKey(ctx context.Context, m *terrainFillModel) string {
	if !m.Rect.IsNull() && !m.Rect.IsUnknown() {
		return fmt.Sprintf("rect%v", attrToNums(ctx, m.Rect))
	}
	if !m.Points.IsNull() && !m.Points.IsUnknown() {
		pts, _, _ := attrToPointList(ctx, m.Points)
		return fmt.Sprintf("pts%v", pts)
	}
	return "whole"
}

func (r *terrainFillResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m terrainFillModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, diags := r.apply(ctx, &m)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Read keeps the painted state (terrain has no getter for painted regions).
func (r *terrainFillResource) Read(_ context.Context, _ resource.ReadRequest, _ *resource.ReadResponse) {
}

func (r *terrainFillResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Terrain cannot diff in place; resolve any change by re-painting the
	// desired fill (recovers to the declared plan).
	var m terrainFillModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, diags := r.apply(ctx, &m)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *terrainFillResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("Terrain cannot be un-painted", "delete_element has no terrain counterpart; use the Dungeondraft undo tool or fill another slot to recover the area.")
}
