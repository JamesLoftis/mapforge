package ddprovider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// dungeondraft_room wraps build_room: a closed wall loop plus a
// pattern/terrain/none floor over the same boundary, in one bridge round
// trip. Like dungeondraft_terrain_fill this has no reshape API — every
// attribute replaces on change, and Update is never actually called.

type roomResource struct{ *levelPlacedBase }

func NewRoomResource() resource.Resource { return &roomResource{newLevelPlacedBase()} }

func (r *roomResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_room"
}

type roomModel struct {
	ID            types.String `tfsdk:"id"`
	Rect          types.List   `tfsdk:"rect"`
	Points        types.List   `tfsdk:"points"`
	WallAsset     types.String `tfsdk:"wall_asset"`
	WallType      types.Int64  `tfsdk:"wall_type"`
	WallJoint     types.Int64  `tfsdk:"wall_joint"`
	WallShadow    types.Bool   `tfsdk:"wall_shadow"`
	Floor         types.String `tfsdk:"floor"`
	FloorAsset    types.String `tfsdk:"floor_asset"`
	FloorCategory types.String `tfsdk:"floor_category"`
	FloorColor    types.String `tfsdk:"floor_color"`
	FloorSlot     types.Int64  `tfsdk:"floor_slot"`
	FloorID       types.String `tfsdk:"floor_id"`
	Level         types.String `tfsdk:"level"`
}

func (r *roomResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A wall loop plus a floor over the same boundary, built in one call (build_room). No reshape API — every attribute replaces on change. `id` is the wall's element id (usable as another resource's wall_id, e.g. dungeondraft_portal).",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"rect": schema.ListAttribute{Optional: true, ElementType: types.Int64Type, Description: "[x, y, w, h] boundary rectangle; alternative to points.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()}},
			"points": schema.ListAttribute{Optional: true, ElementType: pointElemType, Description: "[[x,y], ...] boundary polygon (>= 3 points); alternative to rect.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()}},
			"wall_asset": schema.StringAttribute{Optional: true, Description: "A Walls asset path; omit for the tileset default.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"wall_type": schema.Int64Attribute{Optional: true, Description: "0=auto, 1=manual, 2=cave (default 0).",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"wall_joint": schema.Int64Attribute{Optional: true, Description: "0=sharp, 1=bevel, 2=round (default 1).",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"wall_shadow": schema.BoolAttribute{Optional: true, Description: "Default true.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}},
			"floor": schema.StringAttribute{Optional: true, Description: "\"pattern\" (default), \"terrain\", or \"none\".",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"floor_asset": schema.StringAttribute{Optional: true, Description: "Pattern or terrain asset path; omit for the category/slot default.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"floor_category": schema.StringAttribute{Optional: true, Description: "Pattern asset category (default \"Simple Tiles\"); pattern floors only.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"floor_color": schema.StringAttribute{Optional: true, Description: "#rrggbb tint; pattern floors only.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"floor_slot": schema.Int64Attribute{Optional: true, Description: "Terrain slot index (default 1); terrain floors only.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"floor_id": schema.StringAttribute{Computed: true, Description: "The floor pattern shape's element id. Only set when floor = \"pattern\" (terrain floors have no element id, like dungeondraft_terrain_fill)."},
			"level":    levelAttribute(),
		},
	}
}

func (r *roomResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	c := r.clientFor(resp)
	if c == nil {
		return
	}
	var m roomModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// rect and points are mutually exclusive (rect takes precedence if both
	// are somehow set) — matching dungeondraft_terrain_fill/_cave's
	// precedence-chain convention elsewhere in this provider. Previously
	// these were two independent ifs, so setting both silently sent build_room
	// a request with both rect and points populated at once.
	params := map[string]any{}
	haveShape := false
	if rect := attrToNums(ctx, m.Rect); rect != nil {
		params["rect"] = rect
		haveShape = true
	} else if pts, ok, diags := attrToPointList(ctx, m.Points); ok {
		params["points"] = pts
		haveShape = true
	} else if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	if !haveShape {
		resp.Diagnostics.Append(attrError("rect or points")...)
		return
	}
	if !m.WallAsset.IsNull() && m.WallAsset.ValueString() != "" {
		params["wall_asset"] = m.WallAsset.ValueString()
	}
	if isConfigured(m.WallType) {
		params["wall_type"] = m.WallType.ValueInt64()
	}
	if isConfigured(m.WallJoint) {
		params["wall_joint"] = m.WallJoint.ValueInt64()
	}
	if isConfigured(m.WallShadow) {
		params["wall_shadow"] = m.WallShadow.ValueBool()
	}
	floorKind := "pattern"
	if !m.Floor.IsNull() && m.Floor.ValueString() != "" {
		floorKind = m.Floor.ValueString()
		params["floor"] = floorKind
	}
	if !m.FloorAsset.IsNull() && m.FloorAsset.ValueString() != "" {
		params["floor_asset"] = m.FloorAsset.ValueString()
	}
	if !m.FloorCategory.IsNull() && m.FloorCategory.ValueString() != "" {
		params["floor_category"] = m.FloorCategory.ValueString()
	}
	if !m.FloorColor.IsNull() && m.FloorColor.ValueString() != "" {
		params["floor_color"] = m.FloorColor.ValueString()
	}
	if isConfigured(m.FloorSlot) {
		params["floor_slot"] = m.FloorSlot.ValueInt64()
	}

	params["level"] = m.Level.ValueString()
	result, err := c.Request(ctx, "build_room", params)
	if err != nil {
		resp.Diagnostics.Append(diagErrf("Failed to run build_room", "%v", err)...)
		return
	}
	wallID, ok := result["wall_id"]
	if !ok || wallID == nil {
		resp.Diagnostics.Append(diagErrf("build_room did not return a wall_id", "result: %v", result)...)
		return
	}
	m.ID = types.StringValue(idAsString(wallID))

	// The floor step can fail without failing the overall command (the mod
	// embeds floor_error in an ok:true response rather than rejecting the
	// whole call) — surface that as a warning so a silently-missing floor
	// isn't mistaken for one that was actually placed.
	m.FloorID = types.StringNull()
	if floorKind == "pattern" {
		if fid, ok := result["floor_id"]; ok && fid != nil {
			m.FloorID = types.StringValue(idAsString(fid))
		} else if ferr, ok := result["floor_error"]; ok && ferr != nil {
			resp.Diagnostics.AddWarning("Room floor was not placed", "build_room's wall succeeded but the floor step failed: "+idAsString(ferr))
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// idAsString coerces a JSON value that may be a number (an id) or a string
// (build_room's floor_error is a message, not an id) to its string form.
func idAsString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return strconv.FormatInt(numAsInt64(v), 10)
}

func (r *roomResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m roomModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The bridge client was not available.")
		return
	}
	// Existence check only: the wall id is the room's identity, and (like
	// dungeondraft_terrain_fill) the floor step has no general getter to
	// drift-check against once painted. If the wall is gone, treat the whole
	// room as gone.
	desc, exists, diags := fetchNode(ctx, r.client, m.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}
	m.Level = levelFromDescriptor(desc, m.Level)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *roomResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	noUpdate(ctx, req, resp)
}

func (r *roomResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m roomModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.deleteID(ctx, m.ID.ValueString(), resp)
	if !m.FloorID.IsNull() && m.FloorID.ValueString() != "" {
		resp.Diagnostics.Append(deleteNode(ctx, r.client, m.FloorID.ValueString())...)
	} else if !m.Floor.IsNull() && m.Floor.ValueString() == "terrain" {
		resp.Diagnostics.AddWarning("Terrain floor cannot be un-painted", "delete_element has no terrain counterpart; the room's walls were removed but its terrain floor was not (same limitation as dungeondraft_terrain_fill).")
	}
}
