package ddprovider

import (
	"context"

	bridge "dungeondraft-mcp/bridgeclient"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// nodeResourceBase carries the bridge client that Configure hands every
// resource, plus the shared use-it-or-diagnose helper.
type nodeResourceBase struct {
	client       *bridge.Client
	defaultLevel string // the provider block's level label, "" when unset
}

func (b *nodeResourceBase) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	pd, ok := req.ProviderData.(*providerData)
	if !ok || pd == nil || pd.client == nil {
		resp.Diagnostics.AddError("Invalid provider configuration", "Provider data did not carry a bridge client.")
		return
	}
	b.client = pd.client
	b.defaultLevel = pd.level
}

func (b *nodeResourceBase) clientFor(resp *resource.CreateResponse) *bridge.Client {
	if b.client != nil {
		return b.client
	}
	resp.Diagnostics.AddError("Provider not configured", "The bridge client was not available (missing provider block?).")
	return nil
}

// deleteID implements Delete by running delete_element on a stored id.
func (b *nodeResourceBase) deleteID(ctx context.Context, id string, resp *resource.DeleteResponse) {
	if b.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The bridge client was not available.")
		return
	}
	resp.Diagnostics.Append(deleteNode(ctx, b.client, id)...)
}

// ImportState is identical for every node/shape-kind resource — `terraform
// import` supplies the bridge's numeric element id directly as the
// resource's `id` attribute. Promoted by embedding to every resource type
// below and in resources_shapes.go, exactly like Configure already is; none
// of them need to (or should) redeclare it.
func (b *nodeResourceBase) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// pointElemType is the per-point element type ([x, y], a list of two ints)
// shared by every kind whose `points` attribute is a list of [x,y] pairs.
var pointElemType = types.ListType{ElemType: types.Int64Type}

// noPlan is the Update stub for write-mostly resources (all their attributes
// are RequiresReplace, so Terraform never invokes it).
func noUpdate(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddWarning("No in-place update", "This resource replaces on change instead of updating in place.")
}

// idAttribute is the computed element-id attribute.
func idAttribute() schema.StringAttribute {
	return schema.StringAttribute{Computed: true}
}

// ---- dungeondraft_wall (draw_wall) ---------------------------------------

type wallResource struct{ *levelPlacedBase }

func NewWallResource() resource.Resource { return &wallResource{newLevelPlacedBase()} }

func (r *wallResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_wall"
}

type wallModel struct {
	ID     types.String `tfsdk:"id"`
	Points types.List   `tfsdk:"points"`
	Asset  types.String `tfsdk:"asset"`
	Loop   types.Bool   `tfsdk:"loop"`
	Type   types.Int64  `tfsdk:"type"`
	Joint  types.Int64  `tfsdk:"joint"`
	Color  types.String `tfsdk:"color"`
	Shadow types.Bool   `tfsdk:"shadow"`
	Level  types.String `tfsdk:"level"`
}

func (r *wallResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Draw a wall through woxel points (draw_wall). The bridge has no reshape/redraw API — every attribute replaces on change; Update is never actually called.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"points": schema.ListAttribute{Required: true, ElementType: pointElemType, Description: "Polyline in woxels.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()}},
			// Read syncs asset/loop/type/joint from the live wall (A2 geometry
			// read), so these must be Optional+Computed: a value the bridge
			// applied by default (config left it unset) is not drift, only a
			// mismatch against an explicitly-set config value is. RequiresReplace
			// still applies on top — there is no draw_wall equivalent of
			// modify_object, so an explicit change can only be a replace.
			"asset": schema.StringAttribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}},
			"loop": schema.BoolAttribute{Optional: true, Computed: true, Description: "Close into a loop (a room).",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown(), boolplanmodifier.RequiresReplace()}},
			"type": schema.Int64Attribute{Optional: true, Computed: true, Description: "0=auto, 1=manual, 2=cave.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown(), int64planmodifier.RequiresReplace()}},
			"joint": schema.Int64Attribute{Optional: true, Computed: true, Description: "0=sharp, 1=bevel, 2=round.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown(), int64planmodifier.RequiresReplace()}},
			"color":  schema.StringAttribute{Optional: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"shadow": schema.BoolAttribute{Optional: true, PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}},
			"level":  levelAttribute(),
		},
	}
}

func (r *wallResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	c := r.clientFor(resp)
	if c == nil {
		return
	}
	var m wallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	params := map[string]any{}
	if pts, ok, diags := attrToPointList(ctx, m.Points); ok {
		params["points"] = pts
	} else {
		resp.Diagnostics.Append(diags...)
		return
	}
	if !m.Asset.IsNull() && m.Asset.ValueString() != "" {
		params["asset"] = m.Asset.ValueString()
	}
	if !m.Loop.IsNull() {
		params["loop"] = m.Loop.ValueBool()
	}
	if !m.Type.IsNull() {
		params["type"] = m.Type.ValueInt64()
	}
	if !m.Joint.IsNull() {
		params["joint"] = m.Joint.ValueInt64()
	}
	if !m.Color.IsNull() && m.Color.ValueString() != "" {
		params["color"] = m.Color.ValueString()
	}
	if !m.Shadow.IsNull() {
		params["shadow"] = m.Shadow.ValueBool()
	}
	id, diags := createNode(ctx, c, "draw_wall", params, m.Level.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.ID = types.StringValue(id)
	coerceUnknownToNull(&m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *wallResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m wallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The bridge client was not available.")
		return
	}
	// Fully descriptor-aware: walls expose real geometry, so Read syncs shape
	// fields and surfaces hand-edits as drift (ROADMAP Part B, A2).
	desc, exists, diags := fetchNode(ctx, r.client, m.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}
	if a, ok := desc["asset"].(string); ok && a != "" {
		m.Asset = types.StringValue(a)
	}
	if pts, ok := desc["points"].([]any); ok {
		m.Points = pointListToAttr(ctx, pts)
	}
	if v, ok := desc["loop"].(bool); ok {
		m.Loop = types.BoolValue(v)
	}
	if v := numAsInt64(desc["type"]); desc["type"] != nil {
		m.Type = types.Int64Value(v)
	}
	if v := numAsInt64(desc["joint"]); desc["joint"] != nil {
		m.Joint = types.Int64Value(v)
	}
	m.Level = levelFromDescriptor(desc, m.Level)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *wallResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	noUpdate(ctx, req, resp)
}

func (r *wallResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m wallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.deleteID(ctx, m.ID.ValueString(), resp)
}

// ---- dungeondraft_portal (add_portal) ------------------------------------

type portalResource struct{ *levelPlacedBase }

func NewPortalResource() resource.Resource { return &portalResource{newLevelPlacedBase()} }

func (r *portalResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_portal"
}

type portalModel struct {
	ID           types.String  `tfsdk:"id"`
	Asset        types.String  `tfsdk:"asset"`
	X            types.Float64 `tfsdk:"x"`
	Y            types.Float64 `tfsdk:"y"`
	WallID       types.Int64   `tfsdk:"wall_id"`
	Radius       types.Float64 `tfsdk:"radius"`
	Closed       types.Bool    `tfsdk:"closed"`
	Flip         types.Bool    `tfsdk:"flip"`
	Mount        types.String  `tfsdk:"mount"`
	SnapMax      types.Float64 `tfsdk:"snap_max"`
	Rotation     types.Float64 `tfsdk:"rotation"`
	FallbackFree types.Bool    `tfsdk:"fallback_free"`
	Level        types.String  `tfsdk:"level"`
}

func (r *portalResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A door/window portal (add_portal). Give wall_id to mount on a specific wall deterministically; omit it to snap to the nearest wall. No modify-portal API exists — every attribute replaces on change.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"asset": schema.StringAttribute{Required: true, Description: "A Portals asset path.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			// Read syncs wall_id/closed/radius/x/y from the live portal, so
			// these must be Optional+Computed — see the wall schema's comment.
			// RequiresReplace still applies: there is no move/remount API for
			// a portal, unlike a plain object.
			"x": schema.Float64Attribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown(), float64planmodifier.RequiresReplace()}},
			"y": schema.Float64Attribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown(), float64planmodifier.RequiresReplace()}},
			"wall_id": schema.Int64Attribute{Optional: true, Computed: true, Description: "Mount on this specific wall (skips nearest-wall search). Links to a dungeondraft_wall.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown(), int64planmodifier.RequiresReplace()}},
			"radius": schema.Float64Attribute{Optional: true, Computed: true, Description: "Door half-width (~128 = 1-tile door).",
				PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown(), float64planmodifier.RequiresReplace()}},
			"closed": schema.BoolAttribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown(), boolplanmodifier.RequiresReplace()}},
			"flip":          schema.BoolAttribute{Optional: true, PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}},
			"mount":         schema.StringAttribute{Optional: true, Description: "wall or free.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"snap_max":      schema.Float64Attribute{Optional: true, Description: "Max woxel distance a wall may be and still capture the portal.", PlanModifiers: []planmodifier.Float64{float64planmodifier.RequiresReplace()}},
			"rotation":      schema.Float64Attribute{Optional: true, Description: "Degrees; freestanding only.", PlanModifiers: []planmodifier.Float64{float64planmodifier.RequiresReplace()}},
			"fallback_free": schema.BoolAttribute{Optional: true, PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}},
			"level":         levelAttribute(),
		},
	}
}

func (r *portalResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	c := r.clientFor(resp)
	if c == nil {
		return
	}
	var m portalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.Asset.IsNull() || m.Asset.ValueString() == "" {
		resp.Diagnostics.Append(attrError("asset")...)
		return
	}
	params := map[string]any{"asset": m.Asset.ValueString()}
	if !m.X.IsNull() {
		params["x"] = m.X.ValueFloat64()
	}
	if !m.Y.IsNull() {
		params["y"] = m.Y.ValueFloat64()
	}
	if !m.WallID.IsNull() {
		params["wall_id"] = m.WallID.ValueInt64()
	}
	if !m.Radius.IsNull() {
		params["radius"] = m.Radius.ValueFloat64()
	}
	if !m.Closed.IsNull() {
		params["closed"] = m.Closed.ValueBool()
	}
	if !m.Flip.IsNull() {
		params["flip"] = m.Flip.ValueBool()
	}
	if !m.Mount.IsNull() && m.Mount.ValueString() != "" {
		params["mount"] = m.Mount.ValueString()
	}
	if !m.SnapMax.IsNull() {
		params["snap_max"] = m.SnapMax.ValueFloat64()
	}
	if !m.Rotation.IsNull() {
		params["rotation"] = m.Rotation.ValueFloat64()
	}
	if !m.FallbackFree.IsNull() {
		params["fallback_free"] = m.FallbackFree.ValueBool()
	}
	id, diags := createNode(ctx, c, "add_portal", params, m.Level.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.ID = types.StringValue(id)
	coerceUnknownToNull(&m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *portalResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m portalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The bridge client was not available.")
		return
	}
	// Portals expose wall_id + geometry, so Read syncs the mounting and size
	// fields and surfaces hand-edits as drift.
	desc, exists, diags := fetchNode(ctx, r.client, m.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}
	if v := numAsInt64(desc["wall_id"]); desc["wall_id"] != nil {
		m.WallID = types.Int64Value(v)
	}
	if v, ok := desc["closed"].(bool); ok {
		m.Closed = types.BoolValue(v)
	}
	if v := numAsFloat(desc["radius"]); desc["radius"] != nil {
		m.Radius = reportedFloat(v, m.Radius)
	}
	if a, ok := desc["asset"].(string); ok && a != "" {
		m.Asset = types.StringValue(a)
	}
	if pos, ok := desc["position"].([]any); ok && len(pos) == 2 {
		m.X = reportedFloat(numAsFloat(pos[0]), m.X)
		m.Y = reportedFloat(numAsFloat(pos[1]), m.Y)
	}
	m.Level = levelFromDescriptor(desc, m.Level)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *portalResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	noUpdate(ctx, req, resp)
}

func (r *portalResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m portalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.deleteID(ctx, m.ID.ValueString(), resp)
}

// ---- dungeondraft_object (place_object) -----------------------------------

type objectResource struct{ *levelPlacedBase }

func NewObjectResource() resource.Resource { return &objectResource{newLevelPlacedBase()} }

func (r *objectResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_object"
}

type objectModel struct {
	ID       types.String  `tfsdk:"id"`
	Asset    types.String  `tfsdk:"asset"`
	X        types.Float64 `tfsdk:"x"`
	Y        types.Float64 `tfsdk:"y"`
	Rotation types.Float64 `tfsdk:"rotation"`
	Scale    types.Float64 `tfsdk:"scale"`
	Sorting  types.Int64   `tfsdk:"sorting"`
	Color    types.String  `tfsdk:"color"`
	Z        types.Int64   `tfsdk:"z"`
	Level    types.String  `tfsdk:"level"`
}

func (r *objectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A prop on the map (place_object). x/y/rotation/scale/color update in place (move_element, modify_object); asset/sorting/z have no update API and replace.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"asset": schema.StringAttribute{Required: true, Description: "An Objects asset path.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			// Read syncs position/rotation/scale from the live object, so these
			// must be Optional+Computed (values DD applied by default are not
			// drift, only a mismatch against an explicitly-set config value is).
			// No RequiresReplace here: Update actually calls move_element /
			// modify_object for these.
			"x":        schema.Float64Attribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown()}},
			"y":        schema.Float64Attribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown()}},
			"rotation": schema.Float64Attribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown()}},
			"scale":    schema.Float64Attribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown()}},
			// color isn't in get_element's object descriptor at all, so it can't
			// be Computed/drift-checked — but modify_object does accept it, so
			// Update still applies an explicit change in place (write-only,
			// like sorting/z, just not replace-on-change like them).
			"color":   schema.StringAttribute{Optional: true},
			"sorting": schema.Int64Attribute{Optional: true, Description: "0=over, 1=under.", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"z":       schema.Int64Attribute{Optional: true, Description: "Absolute z_index (default 200; above legacy floors, below roofs).", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"level":   levelAttribute(),
		},
	}
}

func (r *objectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	c := r.clientFor(resp)
	if c == nil {
		return
	}
	var m objectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.Asset.IsNull() || m.Asset.ValueString() == "" {
		resp.Diagnostics.Append(attrError("asset")...)
		return
	}
	params := map[string]any{"asset": m.Asset.ValueString()}
	if !m.X.IsNull() {
		params["x"] = m.X.ValueFloat64()
	}
	if !m.Y.IsNull() {
		params["y"] = m.Y.ValueFloat64()
	}
	if !m.Rotation.IsNull() {
		params["rotation"] = m.Rotation.ValueFloat64()
	}
	if !m.Scale.IsNull() {
		params["scale"] = m.Scale.ValueFloat64()
	}
	if !m.Sorting.IsNull() {
		params["sorting"] = m.Sorting.ValueInt64()
	}
	if !m.Color.IsNull() && m.Color.ValueString() != "" {
		params["color"] = m.Color.ValueString()
	}
	if !m.Z.IsNull() {
		params["z"] = m.Z.ValueInt64()
	}
	id, diags := createNode(ctx, c, "place_object", params, m.Level.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.ID = types.StringValue(id)
	coerceUnknownToNull(&m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *objectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m objectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The bridge client was not available.")
		return
	}
	desc, exists, diags := fetchNode(ctx, r.client, m.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}
	// Objects expose position/rotation/scale/asset, so Read syncs the
	// positional fields and surfaces hand-edits (moves/scales/rotations) as
	// drift. z/sorting/color are not in the descriptor; they stay as config.
	if pos, ok := desc["position"].([]any); ok && len(pos) == 2 {
		m.X = reportedFloat(numAsFloat(pos[0]), m.X)
		m.Y = reportedFloat(numAsFloat(pos[1]), m.Y)
	}
	if desc["rotation"] != nil {
		m.Rotation = reportedFloat(numAsFloat(desc["rotation"]), m.Rotation)
	}
	if desc["scale"] != nil {
		m.Scale = reportedFloat(numAsFloat(desc["scale"]), m.Scale)
	}
	m.Level = levelFromDescriptor(desc, m.Level)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *objectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The bridge client was not available.")
		return
	}
	var plan objectModel
	var state objectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Position updates move the node in place (move_element).
	movedX := !plan.X.IsNull() && !state.X.IsNull() && plan.X != state.X
	if movedX || (plan.X.IsNull() != state.X.IsNull()) {
		x := plan.X.ValueFloat64()
		y := state.Y.ValueFloat64()
		if !plan.Y.IsNull() {
			y = plan.Y.ValueFloat64()
		}
		resp.Diagnostics.Append(moveNode(ctx, r.client, state.ID.ValueString(), x, y)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	movedY := !plan.Y.IsNull() && !state.Y.IsNull() && plan.Y != state.Y
	if movedY && !movedX {
		x := state.X.ValueFloat64()
		if !plan.X.IsNull() {
			x = plan.X.ValueFloat64()
		}
		y := plan.Y.ValueFloat64()
		resp.Diagnostics.Append(moveNode(ctx, r.client, state.ID.ValueString(), x, y)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// scale/rotation/color update in place via modify_object — only the
	// fields that actually changed, so an unrelated field isn't clobbered
	// back to a bridge default.
	var scale, rotation *float64
	var color *string
	if !plan.Scale.Equal(state.Scale) {
		v := plan.Scale.ValueFloat64()
		scale = &v
	}
	if !plan.Rotation.Equal(state.Rotation) {
		v := plan.Rotation.ValueFloat64()
		rotation = &v
	}
	if !plan.Color.Equal(state.Color) && !plan.Color.IsNull() {
		v := plan.Color.ValueString()
		color = &v
	}
	if scale != nil || rotation != nil || color != nil {
		resp.Diagnostics.Append(modifyObjectNode(ctx, r.client, state.ID.ValueString(), scale, rotation, color)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	state.X = plan.X
	state.Y = plan.Y
	state.Rotation = plan.Rotation
	state.Scale = plan.Scale
	// Only when modify_object was actually called with a color — matching
	// scale/rotation would be equally wrong here, but those two are
	// Optional+Computed with a bridge default, so plan is never null for
	// them the way it can be for color once set then removed from config.
	// modify_object has no "clear the color" input, so a removed color
	// stays whatever it last was on the bridge; copying plan.Color (null)
	// into state here would silently record a color change that never
	// actually happened, and since color can't be read back (see the
	// schema comment), that drift would never self-correct on a later Read.
	if color != nil {
		state.Color = plan.Color
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *objectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m objectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.deleteID(ctx, m.ID.ValueString(), resp)
}

// ---- dungeondraft_light (add_light) ---------------------------------------

type lightResource struct{ *levelPlacedBase }

func NewLightResource() resource.Resource { return &lightResource{newLevelPlacedBase()} }

func (r *lightResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_light"
}

type lightModel struct {
	ID      types.String  `tfsdk:"id"`
	X       types.Float64 `tfsdk:"x"`
	Y       types.Float64 `tfsdk:"y"`
	Color   types.String  `tfsdk:"color"`
	Energy  types.Float64 `tfsdk:"energy"`
	Range   types.Float64 `tfsdk:"range"`
	Shadows types.Bool    `tfsdk:"shadows"`
	Asset   types.String  `tfsdk:"asset"`
	Level   types.String  `tfsdk:"level"`
}

func (r *lightResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A light at a woxel position (add_light). No modify-light API exists (unlike dungeondraft_object) — every attribute replaces on change.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"x": schema.Float64Attribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown(), float64planmodifier.RequiresReplace()}},
			"y": schema.Float64Attribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown(), float64planmodifier.RequiresReplace()}},
			"color":   schema.StringAttribute{Optional: true, Description: "#rrggbb", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"energy":  schema.Float64Attribute{Optional: true, PlanModifiers: []planmodifier.Float64{float64planmodifier.RequiresReplace()}},
			"range":   schema.Float64Attribute{Optional: true, Description: "Radius scale.", PlanModifiers: []planmodifier.Float64{float64planmodifier.RequiresReplace()}},
			"shadows": schema.BoolAttribute{Optional: true, PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}},
			"asset": schema.StringAttribute{Optional: true, Computed: true, Description: "Lights gradient/cookie texture.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}},
			"level": levelAttribute(),
		},
	}
}

func (r *lightResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	c := r.clientFor(resp)
	if c == nil {
		return
	}
	var m lightModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// x and y default to map center when omitted; take them as required to be
	// declarative (a light placement is part of the plan).
	params := map[string]any{}
	if !m.X.IsNull() {
		params["x"] = m.X.ValueFloat64()
	}
	if !m.Y.IsNull() {
		params["y"] = m.Y.ValueFloat64()
	}
	if m.X.IsNull() || m.Y.IsNull() {
		resp.Diagnostics.Append(diagErrf("Missing coordinates", "dungeondraft_light requires both x and y")...)
		return
	}
	if !m.Color.IsNull() && m.Color.ValueString() != "" {
		params["color"] = m.Color.ValueString()
	}
	if !m.Energy.IsNull() {
		params["energy"] = m.Energy.ValueFloat64()
	}
	if !m.Range.IsNull() {
		params["range"] = m.Range.ValueFloat64()
	}
	if !m.Shadows.IsNull() {
		params["shadows"] = m.Shadows.ValueBool()
	}
	if !m.Asset.IsNull() && m.Asset.ValueString() != "" {
		params["asset"] = m.Asset.ValueString()
	}
	id, diags := createNode(ctx, c, "add_light", params, m.Level.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.ID = types.StringValue(id)
	coerceUnknownToNull(&m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *lightResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m lightModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The bridge client was not available.")
		return
	}
	desc, exists, diags := fetchNode(ctx, r.client, m.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}
	// Lights expose position/rotation/scale/asset; sync position + asset for
	// drift detection. energy/range/shadows/color are not in the descriptor.
	if pos, ok := desc["position"].([]any); ok && len(pos) == 2 {
		m.X = reportedFloat(numAsFloat(pos[0]), m.X)
		m.Y = reportedFloat(numAsFloat(pos[1]), m.Y)
	}
	if a, ok := desc["asset"].(string); ok && a != "" {
		m.Asset = types.StringValue(a)
	}
	m.Level = levelFromDescriptor(desc, m.Level)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *lightResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	noUpdate(ctx, req, resp)
}

func (r *lightResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m lightModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.deleteID(ctx, m.ID.ValueString(), resp)
}
