package ddprovider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// dungeondraft_cave wraps dig_cave (carve or fill cells on the Caves raster
// layer — a genuinely different layer/concept from terrain_fill's texture
// slots). Like terrain_fill, a cave stroke has no object id, so the state id
// is a deterministic hash of the desired params; any change re-applies.
//
// Unlike terrain_fill, Delete here is a REAL reversal, not just a warning:
// dig_cave stamps the `dig` boolean directly into the cave bitmap
// (mod's _cave_stamp), so re-running the identical stroke with `dig`
// inverted un-stamps exactly the cells this resource stamped. The one
// caveat — noted in the schema description, not hidden — is the inherent
// raster-overlap limit: if something else painted over the same cells
// afterward (another dungeondraft_cave, or a hand-edit in the DD UI), this
// resource's Delete still un-stamps its own footprint, which will also
// undo whatever was drawn on top of it there. Same class of limitation as
// two `dungeondraft_object`s occupying the same tile; not special to caves.

type caveResource struct{ *levelPaintedBase }

func NewCaveResource() resource.Resource { return &caveResource{newLevelPaintedBase()} }

func (r *caveResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_cave"
}

type caveModel struct {
	ID          types.String  `tfsdk:"id"`
	Points      types.List    `tfsdk:"points"`
	X           types.Float64 `tfsdk:"x"`
	Y           types.Float64 `tfsdk:"y"`
	Radius      types.Float64 `tfsdk:"radius"`
	Dig         types.Bool    `tfsdk:"dig"`
	GroundColor types.String  `tfsdk:"ground_color"`
	WallColor   types.String  `tfsdk:"wall_color"`
	Texture     types.String  `tfsdk:"texture"`
	Level       types.String  `tfsdk:"level"`
}

func (r *caveResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Carve or fill cells on the Caves raster layer (dig_cave). Give points for a multi-point stroke, or x/y for a single dab. Re-applies on change; Delete genuinely reverses its own stroke (see the package doc comment on caveResource) rather than just warning, unlike dungeondraft_terrain_fill.",
		Attributes: map[string]schema.Attribute{
			"id":           idAttribute(),
			"points":       schema.ListAttribute{Optional: true, ElementType: pointElemType, Description: "[[x,y], ...] stroke centerline; alternative to x/y."},
			"x":            schema.Float64Attribute{Optional: true, Description: "Single dab center; alternative to points. Requires y."},
			"y":            schema.Float64Attribute{Optional: true, Description: "Single dab center; alternative to points. Requires x."},
			"radius":       schema.Float64Attribute{Optional: true, Description: "Brush radius in woxels (default 256)."},
			"dig":          schema.BoolAttribute{Optional: true, Description: "true (default) carves open floor; false fills cells back to rock."},
			"ground_color": schema.StringAttribute{Optional: true, Description: "#rrggbb cave floor tint."},
			"wall_color":   schema.StringAttribute{Optional: true, Description: "#rrggbb cave wall tint."},
			"texture":      schema.StringAttribute{Optional: true, Description: "A Caves floor texture asset path."},
			"level":        paintLevelAttribute(),
		},
	}
}

func (r *caveResource) apply(ctx context.Context, m *caveModel) (string, diag.Diagnostics) {
	var zero string
	if r.client == nil {
		return zero, diagErrf("Provider not configured", "The bridge client was not available.")
	}
	params := map[string]any{}
	havePoints := !m.Points.IsNull() && !m.Points.IsUnknown()
	havePoint := !m.X.IsNull() && !m.Y.IsNull()
	if havePoints {
		pts, ok, diags := attrToPointList(ctx, m.Points)
		if !ok {
			return zero, diags
		}
		params["points"] = pts
	} else if havePoint {
		params["x"] = m.X.ValueFloat64()
		params["y"] = m.Y.ValueFloat64()
	} else {
		return zero, attrError("points or x/y")
	}
	if !m.Radius.IsNull() {
		params["radius"] = m.Radius.ValueFloat64()
	}
	dig := true
	if !m.Dig.IsNull() {
		dig = m.Dig.ValueBool()
	}
	params["dig"] = dig
	if !m.GroundColor.IsNull() && m.GroundColor.ValueString() != "" {
		params["ground_color"] = m.GroundColor.ValueString()
	}
	if !m.WallColor.IsNull() && m.WallColor.ValueString() != "" {
		params["wall_color"] = m.WallColor.ValueString()
	}
	if !m.Texture.IsNull() && m.Texture.ValueString() != "" {
		params["texture"] = m.Texture.ValueString()
	}
	params["level"] = m.Level.ValueString()
	_, err := r.client.Request(ctx, "dig_cave", params)
	if err != nil {
		return zero, diagErrf("Failed to run dig_cave", "%v", err)
	}
	return fmt.Sprintf("cave.%s.%v", caveShapeKey(ctx, m), dig), nil
}

func caveShapeKey(ctx context.Context, m *caveModel) string {
	if !m.Points.IsNull() && !m.Points.IsUnknown() {
		pts, _, _ := attrToPointList(ctx, m.Points)
		return fmt.Sprintf("pts%v/r%v", pts, m.Radius.ValueFloat64())
	}
	return fmt.Sprintf("xy%v,%v/r%v", m.X.ValueFloat64(), m.Y.ValueFloat64(), m.Radius.ValueFloat64())
}

func (r *caveResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m caveModel
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

// Read keeps the carved state (the cave bitmap has no per-stroke getter to
// drift-check against, same limitation as dungeondraft_terrain_fill).
func (r *caveResource) Read(_ context.Context, _ resource.ReadRequest, _ *resource.ReadResponse) {
}

func (r *caveResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m caveModel
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

func (r *caveResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The bridge client was not available.")
		return
	}
	var m caveModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Genuine reversal: re-stamp the identical stroke with dig inverted (see
	// the package doc comment on caveResource for the overlap caveat).
	originalDig := true // apply()'s own default when Dig was left unset
	if !m.Dig.IsNull() {
		originalDig = m.Dig.ValueBool()
	}
	m.Dig = types.BoolValue(!originalDig)
	if _, diags := r.apply(ctx, &m); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		resp.Diagnostics.AddWarning("Could not reverse the cave stroke", "The map may still show this resource's carved/filled cells; use the Dungeondraft undo tool to recover manually.")
	}
}
