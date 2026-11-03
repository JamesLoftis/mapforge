package ddprovider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// dungeondraft_terrain_fill paints terrain, picking one of four bridge
// commands from which optional fields are set:
//   - x/y set: paint_terrain — a single brush dab (radius sizes the brush)
//   - points + radius both set: paint_path — a brush stroke along points
//   - rect, or points with no radius: fill_region — an exact-shape fill
//   - none of the above: fill_terrain — the whole level
//
// Terrain has no object id, so the state id is a deterministic hash of the
// desired params — re-applying the same plan is idempotent, and any change
// re-paints (recovers to desired). Delete cannot un-paint: it warns instead.

type terrainFillResource struct{ *levelPaintedBase }

func NewTerrainFillResource() resource.Resource { return &terrainFillResource{newLevelPaintedBase()} }

func (r *terrainFillResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_terrain_fill"
}

type terrainFillModel struct {
	ID     types.String  `tfsdk:"id"`
	Slot   types.Int64   `tfsdk:"slot"`
	Asset  types.String  `tfsdk:"asset"`
	Rect   types.List    `tfsdk:"rect"`
	Points types.List    `tfsdk:"points"`
	X      types.Float64 `tfsdk:"x"`
	Y      types.Float64 `tfsdk:"y"`
	Radius types.Float64 `tfsdk:"radius"`
	Rate   types.Float64 `tfsdk:"rate"`
	Level  types.String  `tfsdk:"level"`
}

func (r *terrainFillResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Paint terrain into a slot: whole level by default; rect/points for an exact-shape fill (fill_region); x+y for a single brush dab (paint_terrain); points+radius together for a brush stroke (paint_path). Re-applies on change.",
		Attributes: map[string]schema.Attribute{
			"id":     idAttribute(),
			"slot":   schema.Int64Attribute{Optional: true, Description: "Terrain slot index (default 0)."},
			"asset":  schema.StringAttribute{Optional: true, Description: "Terrain asset to assign to the slot first."},
			"rect":   schema.ListAttribute{Optional: true, ElementType: types.Int64Type, Description: "[x, y, w, h] exact-shape fill region; omit for whole level. Mutually exclusive with x/y."},
			"points": schema.ListAttribute{Optional: true, ElementType: pointElemType, Description: "Without radius: [[x,y], ...] exact-shape fill polygon. With radius: a paint_path brush stroke centerline."},
			"x":      schema.Float64Attribute{Optional: true, Description: "Brush center for a single paint_terrain dab; requires y. Mutually exclusive with rect/points."},
			"y":      schema.Float64Attribute{Optional: true, Description: "Brush center for a single paint_terrain dab; requires x."},
			"radius": schema.Float64Attribute{Optional: true, Description: "Brush radius for x/y (paint_terrain, default 64) or points (paint_path, default 96). Ignored for rect/points-only or whole-level fills."},
			"rate":   schema.Float64Attribute{Optional: true, Description: "Paint strength 0..1 (default 1). Whole-level fill is always full-strength; setting rate without a shape/brush is an error."},
			"level":  paintLevelAttribute(),
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

	havePoint := !m.X.IsNull() && !m.Y.IsNull()
	haveRadius := !m.Radius.IsNull()
	havePoints := !m.Points.IsNull() && !m.Points.IsUnknown()
	haveRect := !m.Rect.IsNull() && !m.Rect.IsUnknown()

	var cmd string
	var haveShapeOrBrush bool
	switch {
	case havePoint:
		// paint_terrain: a single brush dab at x/y.
		cmd = "paint_terrain"
		params["x"] = m.X.ValueFloat64()
		params["y"] = m.Y.ValueFloat64()
		if haveRadius {
			params["radius"] = m.Radius.ValueFloat64()
		}
		haveShapeOrBrush = true
	case havePoints && haveRadius:
		// paint_path: a brush stroke along points' centerline.
		pts, ok, diags := attrToPointList(ctx, m.Points)
		if !ok {
			return zero, diags
		}
		cmd = "paint_path"
		params["points"] = pts
		params["radius"] = m.Radius.ValueFloat64()
		haveShapeOrBrush = true
	case haveRect:
		r := attrToNums(ctx, m.Rect)
		if r != nil {
			cmd = "fill_region"
			params["rect"] = r
			haveShapeOrBrush = true
		} else {
			cmd = "fill_terrain"
		}
	case havePoints:
		pts, ok, diags := attrToPointList(ctx, m.Points)
		if !ok {
			return zero, diags
		}
		cmd = "fill_region"
		params["points"] = pts
		haveShapeOrBrush = true
	default:
		cmd = "fill_terrain"
	}

	if !m.Rate.IsNull() {
		if haveShapeOrBrush {
			params["rate"] = m.Rate.ValueFloat64()
		} else {
			return zero, diagErrf("rate ignored", "rate only applies to region fills or brush paints (whole-level fill is full-strength).")
		}
	}
	params["level"] = m.Level.ValueString()
	_, err := r.client.Request(ctx, cmd, params)
	if err != nil {
		return zero, diagErrf("Failed to run "+cmd, "%v", err)
	}
	// Deterministic id so repeated applies (and quantity changes) are stable.
	return fmt.Sprintf("terrain.%d.%s.%s", slot, cmd, shapeKey(ctx, m)), nil
}

// shapeKey fingerprints the fill/brush geometry for the state id.
func shapeKey(ctx context.Context, m *terrainFillModel) string {
	if !m.X.IsNull() && !m.Y.IsNull() {
		return fmt.Sprintf("xy%v,%v/r%v", m.X.ValueFloat64(), m.Y.ValueFloat64(), m.Radius.ValueFloat64())
	}
	if !m.Rect.IsNull() && !m.Rect.IsUnknown() {
		return fmt.Sprintf("rect%v", attrToNums(ctx, m.Rect))
	}
	if !m.Points.IsNull() && !m.Points.IsUnknown() {
		pts, _, _ := attrToPointList(ctx, m.Points)
		if !m.Radius.IsNull() {
			return fmt.Sprintf("pathpts%v/r%v", pts, m.Radius.ValueFloat64())
		}
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
