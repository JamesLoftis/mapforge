package ddprovider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ---- dungeondraft_roof (add_roof) ----------------------------------------

type roofResource struct{ *nodeResourceBase }

func NewRoofResource() resource.Resource { return &roofResource{&nodeResourceBase{}} }

func (r *roofResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_roof"
}

type roofModel struct {
	ID      types.String  `tfsdk:"id"`
	Asset   types.String  `tfsdk:"asset"`
	Points  types.List    `tfsdk:"points"`
	Width   types.Float64 `tfsdk:"width"`
	Type    types.Int64   `tfsdk:"type"`
	Sorting types.Int64   `tfsdk:"sorting"`
	LevelID types.String  `tfsdk:"level_id"`
}

func (r *roofResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A roof built from its RIDGE LINE (a peak line, not a footprint). No modify-roof API exists — every attribute replaces on change.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"asset": schema.StringAttribute{Required: true, Description: "A Roofs asset path.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"points": schema.ListAttribute{Required: true, ElementType: pointElemType, Description: "The roof ridge, [x,y] pairs in woxels.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()}},
			// Read syncs width/type from the live roof, so they must be
			// Optional+Computed — a value DD applied by default is not drift.
			// RequiresReplace still applies on an explicit change.
			"width": schema.Float64Attribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown(), float64planmodifier.RequiresReplace()}},
			"type": schema.Int64Attribute{Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown(), int64planmodifier.RequiresReplace()}},
			"sorting":  schema.Int64Attribute{Optional: true, Description: "0=over, 1=under.", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"level_id": levelIDAttribute(),
		},
	}
}

func (r *roofResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	c := r.clientFor(resp)
	if c == nil {
		return
	}
	var m roofModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.Asset.IsNull() || m.Asset.ValueString() == "" {
		resp.Diagnostics.Append(attrError("asset")...)
		return
	}
	params := map[string]any{"asset": m.Asset.ValueString()}
	if pts, ok, diags := attrToPointList(ctx, m.Points); ok {
		params["points"] = pts
	} else {
		resp.Diagnostics.Append(diags...)
		return
	}
	if !m.Width.IsNull() {
		params["width"] = m.Width.ValueFloat64()
	}
	if !m.Type.IsNull() {
		params["type"] = m.Type.ValueInt64()
	}
	if !m.Sorting.IsNull() {
		params["sorting"] = m.Sorting.ValueInt64()
	}
	id, diags := createNode(ctx, c, "add_roof", params, m.LevelID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.ID = types.StringValue(id)
	coerceUnknownToNull(&m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *roofResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m roofModel
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
	// Roofs expose their ridge points/width/type, so Read syncs them and
	// surfaces hand-edits (moved ridge, resized eaves) as drift.
	if pts, ok := desc["points"].([]any); ok {
		m.Points = pointListToAttr(ctx, pts)
	}
	if desc["width"] != nil {
		m.Width = types.Float64Value(numAsFloat(desc["width"]))
	}
	if v := numAsInt64(desc["type"]); desc["type"] != nil {
		m.Type = types.Int64Value(v)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *roofResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	noUpdate(ctx, req, resp)
}

func (r *roofResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m roofModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.deleteID(ctx, m.ID.ValueString(), resp)
}

// ---- dungeondraft_text (add_text) -----------------------------------------

type textResource struct{ *nodeResourceBase }

func NewTextResource() resource.Resource { return &textResource{&nodeResourceBase{}} }

func (r *textResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_text"
}

type textModel struct {
	ID      types.String  `tfsdk:"id"`
	Text    types.String  `tfsdk:"text"`
	X       types.Float64 `tfsdk:"x"`
	Y       types.Float64 `tfsdk:"y"`
	Size    types.Int64   `tfsdk:"size"`
	Color   types.String  `tfsdk:"color"`
	Font    types.String  `tfsdk:"font"`
	LevelID types.String  `tfsdk:"level_id"`
}

func (r *textResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A text label at a woxel position (add_text). x/y update in place (move_element, like dungeondraft_object); text/size/color/font have no modify-text API and replace.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			// text/size/color/font have no bridge update path, so a change
			// replaces; x/y do not (Update calls move_element) and are left
			// without RequiresReplace.
			"text":     schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"x":        schema.Float64Attribute{Required: true},
			"y":        schema.Float64Attribute{Required: true},
			"size":     schema.Int64Attribute{Optional: true, Computed: true, Description: "Font size in points.", PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown(), int64planmodifier.RequiresReplace()}},
			"color":    schema.StringAttribute{Optional: true, Computed: true, Description: "#rrggbb", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}},
			"font":     schema.StringAttribute{Optional: true, Computed: true, Description: "A DD font name.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}},
			"level_id": levelIDAttribute(),
		},
	}
}

func (r *textResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	c := r.clientFor(resp)
	if c == nil {
		return
	}
	var m textModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	params := map[string]any{"text": m.Text.ValueString(), "x": m.X.ValueFloat64(), "y": m.Y.ValueFloat64()}
	if !m.Size.IsNull() {
		params["size"] = m.Size.ValueInt64()
	}
	if !m.Color.IsNull() && m.Color.ValueString() != "" {
		params["color"] = m.Color.ValueString()
	}
	if !m.Font.IsNull() && m.Font.ValueString() != "" {
		params["font"] = m.Font.ValueString()
	}
	id, diags := createNode(ctx, c, "add_text", params, m.LevelID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.ID = types.StringValue(id)
	coerceUnknownToNull(&m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *textResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m textModel
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
	// Text exposes position/text/size/color/font; sync them so hand edits
	// (moved label, retyped string, resized font) surface as drift.
	if pos, ok := desc["position"].([]any); ok && len(pos) == 2 {
		m.X = types.Float64Value(numAsFloat(pos[0]))
		m.Y = types.Float64Value(numAsFloat(pos[1]))
	}
	if s, ok := desc["text"].(string); ok && s != "" {
		m.Text = types.StringValue(s)
	}
	if v := numAsInt64(desc["size"]); desc["size"] != nil {
		m.Size = types.Int64Value(v)
	}
	if s, ok := desc["color"].(string); ok && s != "" {
		m.Color = types.StringValue(s)
	}
	if s, ok := desc["font"].(string); ok && s != "" {
		m.Font = types.StringValue(s)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *textResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The bridge client was not available.")
		return
	}
	var plan, state textModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// x/y are the only in-place-updatable fields (move_element works on any
	// node kind); everything else that could change is RequiresReplace, so
	// this is the only case Update ever actually runs for.
	if plan.X != state.X || plan.Y != state.Y {
		resp.Diagnostics.Append(moveNode(ctx, r.client, state.ID.ValueString(), plan.X.ValueFloat64(), plan.Y.ValueFloat64())...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	state.X = plan.X
	state.Y = plan.Y
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *textResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m textModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.deleteID(ctx, m.ID.ValueString(), resp)
}

// ---- dungeondraft_pattern (place_pattern) ---------------------------------

type patternResource struct{ *nodeResourceBase }

func NewPatternResource() resource.Resource { return &patternResource{&nodeResourceBase{}} }

func (r *patternResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_pattern"
}

type patternModel struct {
	ID       types.String  `tfsdk:"id"`
	Asset    types.String  `tfsdk:"asset"`
	Category types.String  `tfsdk:"category"`
	Rect     types.List    `tfsdk:"rect"`
	Points   types.List    `tfsdk:"points"`
	Color    types.String  `tfsdk:"color"`
	Rotation types.Float64 `tfsdk:"rotation"`
	Z        types.Int64   `tfsdk:"z"`
	LevelID  types.String  `tfsdk:"level_id"`
}

func (r *patternResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A tiled floor/pattern shape (rect or polygon) rendering below objects. No modify-pattern API exists — every attribute replaces on change.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"asset": schema.StringAttribute{Required: true, Description: "A pattern asset path.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"category": schema.StringAttribute{Optional: true, Description: "Patterns, Patterns Colorable, Materials, Simple Tiles, Smart Tiles.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"rect": schema.ListAttribute{Optional: true, ElementType: types.Int64Type, Description: "[x, y, w, h] rectangle, alternative to points.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()}},
			"points": schema.ListAttribute{Optional: true, ElementType: pointElemType, Description: "[[x,y], ...] polygon, alternative to rect.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()}},
			"color": schema.StringAttribute{Optional: true, Description: "#rrggbb tint; omit for the tileset default.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"rotation": schema.Float64Attribute{Optional: true, Description: "Pattern rotation in degrees.",
				PlanModifiers: []planmodifier.Float64{float64planmodifier.RequiresReplace()}},
			"z": schema.Int64Attribute{Optional: true, Description: "Absolute z_index (default -100).",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"level_id": levelIDAttribute(),
		},
	}
}

func (r *patternResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	c := r.clientFor(resp)
	if c == nil {
		return
	}
	var m patternModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.Asset.IsNull() || m.Asset.ValueString() == "" {
		resp.Diagnostics.Append(attrError("asset")...)
		return
	}
	params := map[string]any{"asset": m.Asset.ValueString()}
	if !m.Category.IsNull() && m.Category.ValueString() != "" {
		params["category"] = m.Category.ValueString()
	}
	shapeSet := 0
	if !m.Rect.IsNull() && !m.Rect.IsUnknown() {
		if r := attrToNums(ctx, m.Rect); r != nil {
			params["rect"] = r
			shapeSet++
		}
	}
	if !m.Points.IsNull() && !m.Points.IsUnknown() {
		if pts, ok, diags := attrToPointList(ctx, m.Points); ok {
			params["points"] = pts
			shapeSet++
		} else {
			resp.Diagnostics.Append(diags...)
			return
		}
	}
	if shapeSet != 1 {
		resp.Diagnostics.Append(diagErrf("Exactly one shape required", "dungeondraft_pattern needs either rect or points, not both/neither")...)
		return
	}
	if !m.Color.IsNull() && m.Color.ValueString() != "" {
		params["color"] = m.Color.ValueString()
	}
	if !m.Rotation.IsNull() {
		params["rotation"] = m.Rotation.ValueFloat64()
	}
	if !m.Z.IsNull() {
		params["z"] = m.Z.ValueInt64()
	}
	id, diags := createNode(ctx, c, "place_pattern", params, m.LevelID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *patternResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m patternModel
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
	// The bridge reports a pattern's polygon as `points` only — never a rect.
	// So we full-sync geometry only for points-declared patterns; a rect can't
	// be reconstructed from an arbitrary polygon, and writing points into a
	// rect-based resource would churn forever (rect -> points each plan). A
	// rect-declared pattern is therefore write-mostly (existence check only).
	if !m.Points.IsNull() && !m.Points.IsUnknown() {
		if pts, ok := desc["points"].([]any); ok {
			m.Points = pointListToAttr(ctx, pts)
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	}
}

func (r *patternResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	noUpdate(ctx, req, resp)
}

func (r *patternResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m patternModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.deleteID(ctx, m.ID.ValueString(), resp)
}

// ---- dungeondraft_path (draw_path) ----------------------------------------

type pathResource struct{ *nodeResourceBase }

func NewPathResource() resource.Resource { return &pathResource{&nodeResourceBase{}} }

func (r *pathResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_path"
}

type pathModel struct {
	ID         types.String  `tfsdk:"id"`
	Asset      types.String  `tfsdk:"asset"`
	Points     types.List    `tfsdk:"points"`
	Smoothness types.Float64 `tfsdk:"smoothness"`
	Width      types.Float64 `tfsdk:"width"`
	Layer      types.Int64   `tfsdk:"layer"`
	Sorting    types.Int64   `tfsdk:"sorting"`
	LevelID    types.String  `tfsdk:"level_id"`
}

func (r *pathResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A path/road/river through woxel points (draw_path). No modify-path API exists — every attribute replaces on change.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"asset": schema.StringAttribute{Required: true, Description: "A Paths asset path.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"points": schema.ListAttribute{Required: true, ElementType: pointElemType,
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()}},
			"smoothness": schema.Float64Attribute{Optional: true, Computed: true, Description: "Curve smoothing.",
				PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown(), float64planmodifier.RequiresReplace()}},
			"width":    schema.Float64Attribute{Optional: true, Description: "Width scale multiplier.", PlanModifiers: []planmodifier.Float64{float64planmodifier.RequiresReplace()}},
			"layer":    schema.Int64Attribute{Optional: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"sorting":  schema.Int64Attribute{Optional: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"level_id": levelIDAttribute(),
		},
	}
}

func (r *pathResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	c := r.clientFor(resp)
	if c == nil {
		return
	}
	var m pathModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.Asset.IsNull() || m.Asset.ValueString() == "" {
		resp.Diagnostics.Append(attrError("asset")...)
		return
	}
	params := map[string]any{"asset": m.Asset.ValueString()}
	if pts, ok, diags := attrToPointList(ctx, m.Points); ok {
		params["points"] = pts
	} else {
		resp.Diagnostics.Append(diags...)
		return
	}
	if !m.Smoothness.IsNull() {
		params["smoothness"] = m.Smoothness.ValueFloat64()
	}
	if !m.Width.IsNull() {
		params["width"] = m.Width.ValueFloat64()
	}
	if !m.Layer.IsNull() {
		params["layer"] = m.Layer.ValueInt64()
	}
	if !m.Sorting.IsNull() {
		params["sorting"] = m.Sorting.ValueInt64()
	}
	id, diags := createNode(ctx, c, "draw_path", params, m.LevelID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.ID = types.StringValue(id)
	coerceUnknownToNull(&m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *pathResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m pathModel
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
	// Paths expose their edit polyline + smoothness; sync them so a moved or
	// re-smoothed path surfaces as drift.
	if pts, ok := desc["points"].([]any); ok {
		m.Points = pointListToAttr(ctx, pts)
	}
	if desc["smoothness"] != nil {
		m.Smoothness = types.Float64Value(numAsFloat(desc["smoothness"]))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *pathResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	noUpdate(ctx, req, resp)
}

func (r *pathResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m pathModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.deleteID(ctx, m.ID.ValueString(), resp)
}
