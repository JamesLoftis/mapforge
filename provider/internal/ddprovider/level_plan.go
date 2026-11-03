package ddprovider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// A level is identified by its label, which Dungeondraft saves; its numeric
// id is renumbered on load (spec, 2026-11-03). Every command that targets a
// level carries the label as "level", and the mod resolves it, applies it
// for that one command, and restores the active level.

// levelAttribute is the level an element resource is created on. No command
// moves an element between levels, so a change replaces it.
func levelAttribute() schema.StringAttribute {
	return schema.StringAttribute{
		Optional: true,
		Computed: true,
		Description: "The label of the level to create this element on. Defaults to the provider block's " +
			"level; one of the two is required. A change replaces the element.",
	}
}

// paintLevelAttribute is the level a terrain or cave resource paints on. A
// change re-applies on the new level, like any other change to them.
func paintLevelAttribute() schema.StringAttribute {
	return schema.StringAttribute{
		Optional: true,
		Computed: true,
		Description: "The label of the level to paint on. Defaults to the provider block's level; one of " +
			"the two is required.",
	}
}

// levelPlacedBase is the base of every resource that creates an element on
// a level.
type levelPlacedBase struct{ *nodeResourceBase }

func newLevelPlacedBase() *levelPlacedBase { return &levelPlacedBase{&nodeResourceBase{}} }

func (b *levelPlacedBase) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	level, planned := b.planLevel(ctx, req, resp)
	if !planned || req.State.Raw.IsNull() {
		return
	}
	var current types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("level"), &current)...)
	if !current.Equal(level) {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("level"))
	}
}

// levelPaintedBase is the base of the gated terrain and cave resources.
type levelPaintedBase struct{ *nodeResourceBase }

func newLevelPaintedBase() *levelPaintedBase { return &levelPaintedBase{&nodeResourceBase{}} }

func (b *levelPaintedBase) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	b.planLevel(ctx, req, resp)
}

// planLevel sets the planned level from config or the provider block and
// reports it, with planned=false when there is nothing to plan yet.
func (b *nodeResourceBase) planLevel(ctx context.Context, req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse) (level types.String, planned bool) {
	if req.Plan.Raw.IsNull() || b.client == nil {
		return types.StringNull(), false // being destroyed, or the provider is not configured yet
	}
	var configured types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("level"), &configured)...)
	if resp.Diagnostics.HasError() || configured.IsUnknown() {
		return types.StringNull(), false
	}
	level, diags := effectiveLevel(configured, b.defaultLevel)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return types.StringNull(), false
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("level"), level)...)
	return level, true
}

// effectiveLevel is the resource's own level, else the provider block's.
// Neither is an error: an element never lands on "whatever level is active".
func effectiveLevel(configured types.String, providerLevel string) (types.String, diag.Diagnostics) {
	if !configured.IsNull() {
		return configured, nil
	}
	if providerLevel != "" {
		return types.StringValue(providerLevel), nil
	}
	return types.StringNull(), diagErrf("Missing level",
		"no level: set level in the provider block or on this resource")
}
