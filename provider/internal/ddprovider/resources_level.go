package ddprovider

import (
	"context"

	bridge "dungeondraft-mcp/bridgeclient"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// dungeondraft_level wraps add_level. A level is identified by its label,
// which Dungeondraft saves (its numeric id is renumbered by position on
// load), so the label is this resource's id and what every element
// resource's `level` names. Labels are unique within a map: the mod refuses
// add_level with a label already in use.
//
// IMPORTANT ASYMMETRY: there is no delete_level (or rename-level) command in
// the bridge protocol at all — add_level is the entire level write surface.
// So unlike every other resource here, where Delete is at least
// real-if-permanent (delete_element), dungeondraft_level's Delete can only
// warn: `terraform destroy` (or a `label` change, which RequiresReplace
// since there's no rename API either) does not remove anything on the DD
// side. A replace leaves the old level in place, orphaned, alongside the new
// one. Treat this resource as effectively append-only.

type levelResource struct{ *nodeResourceBase }

func NewLevelResource() resource.Resource { return &levelResource{&nodeResourceBase{}} }

func (r *levelResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeName + "_level"
}

type levelModel struct {
	ID    types.String `tfsdk:"id"`
	Label types.String `tfsdk:"label"`
	Index types.Int64  `tfsdk:"index"`
}

func (r *levelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A map level/floor (add_level), identified by its label. No delete or rename API exists on the bridge — see the package doc comment on levelResource. Name it from another resource's level to create elements on it.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"label": schema.StringAttribute{Required: true, Description: "Unique within the map; it is this level's identity. No rename API — a change replaces (see the resource description: replace does not remove the old level).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"index": schema.Int64Attribute{Computed: true, Description: "This level's current position in Global.World.levels. Not stable across level insertion/removal; re-resolved on every Read."},
		},
	}
}

func (r *levelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	c := r.clientFor(resp)
	if c == nil {
		return
	}
	var m levelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := c.Request(ctx, "add_level", map[string]any{"label": m.Label.ValueString()}); err != nil {
		resp.Diagnostics.Append(diagErrf("Failed to run add_level", "%v", err)...)
		return
	}
	m.ID = m.Label
	index, found, diags := findLevel(ctx, c, m.Label.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.Append(diagErrf("Level missing after add_level",
			"add_level succeeded but list_levels has no level labelled %q", m.Label.ValueString())...)
		return
	}
	m.Index = types.Int64Value(index)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *levelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m levelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "The bridge client was not available.")
		return
	}
	index, found, diags := findLevel(ctx, r.client, m.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.State.RemoveResource(ctx) // list_levels answered, and no level has this label
		return
	}
	m.Label = m.ID
	m.Index = types.Int64Value(index)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ModifyPlan plans id as the label. Without it a relabel leaves id unknown
// at plan time, so an element whose level references this id plans an
// in-place update and then fails apply with an inconsistent final plan.
func (r *levelResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var label types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("label"), &label)...)
	if resp.Diagnostics.HasError() || label.IsUnknown() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("id"), label)...)
}

func (r *levelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	noUpdate(ctx, req, resp)
}

func (r *levelResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("Levels cannot be removed", "There is no delete_level (or any level-removal) command in the bridge protocol. This level remains on the map; Terraform will forget about it, but Dungeondraft will not.")
}

// findLevel returns the index of the one level labelled label. found is
// false only when list_levels answered and no level has the label; a failed
// list_levels, or several levels with the label, is an error.
func findLevel(ctx context.Context, c *bridge.Client, label string) (index int64, found bool, diags diag.Diagnostics) {
	lv, err := c.Request(ctx, "list_levels", nil)
	if err != nil {
		return 0, false, diagErrf("Failed to run list_levels", "%v", err)
	}
	var indexes []int64
	levels, _ := lv["levels"].([]any)
	for _, raw := range levels {
		entry, _ := raw.(map[string]any)
		if l, _ := entry["label"].(string); l == label {
			indexes = append(indexes, numAsInt64(entry["index"]))
		}
	}
	switch len(indexes) {
	case 0:
		return 0, false, nil
	case 1:
		return indexes[0], true, nil
	default:
		return 0, false, diagErrf("Ambiguous level "+label,
			"%d levels are labelled %q; labels identify levels, so they must be unique", len(indexes), label)
	}
}
