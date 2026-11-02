package ddprovider

import (
	"context"

	bridge "dungeondraft-mcp/bridgeclient"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// dungeondraft_level wraps add_level. Its `id` is what every other creator
// resource's new level_id attribute (see levelIDAttribute) targets via
// bridge.Client.RunOnLevel.
//
// IMPORTANT ASYMMETRY: there is no delete_level (or rename-level) command in
// the bridge protocol at all — add_level and set_level are the entire level
// write surface. So unlike every other resource here, where Delete is at
// least real-if-permanent (delete_element), dungeondraft_level's Delete can
// only warn: `terraform destroy` (or a `label` change, which RequiresReplace
// since there's no rename API either) does not remove anything on the DD
// side. A replace leaves the old level in place, orphaned, alongside the new
// one. Treat this resource as effectively append-only.
//
// add_level's own response never includes the level's index (only
// list_levels does, and set_level takes an index, not an id) — so Create
// always makes a follow-up list_levels call to resolve it. Unlike
// RunOnLevel, this doesn't need Client.levelMu: add_level doesn't read or
// depend on "current level" state, so a concurrent add_level elsewhere can't
// invalidate this resource's own index lookup — list_levels always reflects
// live ground truth for this resource's own id, whoever else is doing what.

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
		Description: "A map level/floor (add_level). No delete or rename API exists on the bridge — see the package doc comment on levelResource. Reference this resource's id from another resource's level_id to create elements on it.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"label": schema.StringAttribute{Optional: true, Computed: true, Description: "Defaults to \"Level\". No rename API — a change replaces (see the resource description: replace does not remove the old level).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}},
			"index": schema.Int64Attribute{Computed: true, Description: "This level's current position in Global.World.levels. Not stable across level insertion/removal done outside this resource; re-resolved on every Read."},
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
	params := map[string]any{}
	if !m.Label.IsNull() && m.Label.ValueString() != "" {
		params["label"] = m.Label.ValueString()
	}
	result, err := c.Request(ctx, "add_level", params)
	if err != nil {
		resp.Diagnostics.Append(diagErrf("Failed to run add_level", "%v", err)...)
		return
	}
	id, ok := result["id"]
	if !ok || id == nil {
		resp.Diagnostics.Append(diagErrf("add_level did not return an id", "result: %v", result)...)
		return
	}
	m.ID = types.StringValue(idAsString(id))
	if label, ok := result["label"].(string); ok {
		m.Label = types.StringValue(label)
	}

	_, index, found, diags := findLevel(ctx, c, m.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		// Extremely unlikely (we just created it) but not impossible if
		// something concurrently manipulated levels in a way the bridge
		// doesn't expect — surface it as drift-to-be-resolved rather than a
		// hard failure, since the level itself was created successfully.
		resp.Diagnostics.AddWarning("Could not resolve the new level's index", "add_level succeeded but the follow-up list_levels didn't report id "+m.ID.ValueString()+"; index will be unknown until the next Read.")
		m.Index = types.Int64Null()
	} else {
		m.Index = types.Int64Value(index)
	}

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
	label, index, found, diags := findLevel(ctx, r.client, m.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		// Shouldn't happen (nothing can remove a level via the bridge), but
		// stay consistent with every other resource's not-found handling
		// rather than erroring on a hand-edited map.
		resp.State.RemoveResource(ctx)
		return
	}
	m.Label = types.StringValue(label)
	m.Index = types.Int64Value(index)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *levelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	noUpdate(ctx, req, resp)
}

func (r *levelResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("Levels cannot be removed", "There is no delete_level (or any level-removal) command in the bridge protocol. This level remains on the map; Terraform will forget about it, but Dungeondraft will not.")
}

// findLevel looks up a level's current label and index by id via
// list_levels, shared by Create (resolving what add_level just made) and
// Read (drift-checking it).
func findLevel(ctx context.Context, c *bridge.Client, id string) (label string, index int64, found bool, diags diag.Diagnostics) {
	lv, err := c.Request(ctx, "list_levels", nil)
	if err != nil {
		return "", 0, false, diagErrf("Failed to run list_levels", "%v", err)
	}
	levels, _ := lv["levels"].([]any)
	for _, raw := range levels {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if idAsString(entry["id"]) != id {
			continue
		}
		if l, ok := entry["label"].(string); ok {
			label = l
		}
		return label, numAsInt64(entry["index"]), true, nil
	}
	return "", 0, false, nil
}
