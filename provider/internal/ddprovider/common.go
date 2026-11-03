package ddprovider

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	bridge "dungeondraft-mcp/bridgeclient"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Read support. Every node-kind resource Read calls fetchNode for its element
// descriptor (get_element) and — depending on what that kind's descriptor
// exposes — syncs real geometry back into state so a drift (hand-edit made
// directly in the Dungeondraft UI) is visible on the next `terraform plan`.
// Kinds whose descriptor only reports a subset of the config fields (object z/
// sorting/color, light energy/range, rect-declared patterns) necessarily stay
// write-only for those fields — they cannot be read back, so they are not
// marked Computed and defaults are simply trusted.

// createNode runs a bridge create command on the level labelled level and
// returns the new element's numeric id as the Terraform resource id.
func createNode(ctx context.Context, c *bridge.Client, cmd string, params map[string]any, level string) (string, diag.Diagnostics) {
	params["level"] = level
	res, err := c.Request(ctx, cmd, params)
	if err != nil {
		return "", diagErrf("Failed to run "+cmd, "%v", err)
	}
	return strconv.FormatInt(numAsInt64(res["id"]), 10), nil
}

// coerceUnknownToNull flattens any unknown-typed field of a resource model (a
// struct pointer) to its Null form. Optional+Computed attributes carry
// UseStateForUnknown, so a field omitted from config reaches Create's plan as
// unknown; Terraform rejects an unknown computed value in the state returned
// from Create, so we flatten it to null here and let the first Read populate
// the real value. Fields already known (set by config) are left untouched.
// Call it in every Create just before State.Set.
func coerceUnknownToNull(m any) {
	v := reflect.ValueOf(m)

	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}

	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)

		if !f.CanSet() || !f.CanInterface() {
			continue
		}

		ev, ok := f.Interface().(attr.Value)
		if !ok || !ev.IsUnknown() {
			continue
		}

		switch f.Type().Name() {
		case "StringValue":
			f.Set(reflect.ValueOf(types.StringNull()))
		case "Int64Value":
			f.Set(reflect.ValueOf(types.Int64Null()))
		case "Float64Value":
			f.Set(reflect.ValueOf(types.Float64Null()))
		case "BoolValue":
			f.Set(reflect.ValueOf(types.BoolNull()))
		}
	}
}

// deleteNode calls delete_element for a numeric resource id (destructive and
// not undo-able on the bridge, matching PROTOCOL.md).
func deleteNode(ctx context.Context, c *bridge.Client, idStr string) diag.Diagnostics {
	id, diags := parseElementID(idStr)
	if diags.HasError() {
		return diags
	}
	if _, err := c.Request(ctx, "delete_element", map[string]any{"id": id}); err != nil {
		return diagErrf("Failed to delete element "+idStr, "%v", err)
	}
	return nil
}

// fetchNode returns the element's descriptor (get_element). exists=false only
// when the bridge answers not_found; every other failure (Dungeondraft
// offline, timed out, another map open) is an error that leaves state alone.
func fetchNode(ctx context.Context, c *bridge.Client, idStr string) (map[string]any, bool, diag.Diagnostics) {
	id, diags := parseElementID(idStr)
	if diags.HasError() {
		return nil, false, diags
	}
	res, err := c.Request(ctx, "get_element", map[string]any{"id": id})
	switch {
	case err == nil:
		return res, true, nil
	case bridge.IsNotFound(err):
		return nil, false, nil
	default:
		return nil, false, diagErrf("Failed to read element "+idStr, "%v", err)
	}
}

// parseElementID turns a resource id back into the bridge's numeric id.
func parseElementID(idStr string) (int64, diag.Diagnostics) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, diagErrf("Invalid element id "+idStr, "resource id %q is not a Dungeondraft element id: %v", idStr, err)
	}
	return id, nil
}

// moveNode shifts a node in place (move_element works for any node kind);
// used for positional in-place updates where supported.
func moveNode(ctx context.Context, c *bridge.Client, idStr string, x, y float64) diag.Diagnostics {
	id, diags := parseElementID(idStr)
	if diags.HasError() {
		return diags
	}
	if _, err := c.Request(ctx, "move_element", map[string]any{"id": id, "x": x, "y": y}); err != nil {
		return diagErrf("Failed to move element "+idStr, "%v", err)
	}
	return nil
}

// modifyObjectNode calls modify_object for the fields it actually supports in
// place (scale, rotation, color) — the only dungeondraft_object attributes
// besides x/y the bridge can change without a destroy+recreate. A nil pointer
// means "this field didn't change in the plan," and is omitted so the bridge
// leaves it alone rather than resetting it.
func modifyObjectNode(ctx context.Context, c *bridge.Client, idStr string, scale, rotation *float64, color *string) diag.Diagnostics {
	id, diags := parseElementID(idStr)
	if diags.HasError() {
		return diags
	}
	params := map[string]any{"id": id}
	if scale != nil {
		params["scale"] = *scale
	}
	if rotation != nil {
		params["rotation"] = *rotation
	}
	if color != nil {
		params["color"] = *color
	}
	if _, err := c.Request(ctx, "modify_object", params); err != nil {
		return diagErrf("Failed to modify element "+idStr, "%v", err)
	}
	return nil
}

// diagErrf builds an error diagnostic from a format string.
func diagErrf(summary, format string, args ...any) diag.Diagnostics {
	var d diag.Diagnostics
	d.AddError(summary, fmt.Sprintf(format, args...))
	return d
}

// attrError returns a single error diagnostic asserting an attribute is null.
func attrError(name string) diag.Diagnostics {
	return diagErrf("Missing required attribute", "attribute %q must be set", name)
}

// ---- JSON <-> types.List helpers ------------------------------------------

// numListToAttr converts a []any of numbers (JSON) into a types.List of int64s.
func numListToAttr(ctx context.Context, raw []any) types.List {
	var out []int64
	for _, item := range raw {
		out = append(out, numAsInt64(item))
	}
	lst, _ := types.ListValueFrom(ctx, types.Int64Type, out)
	return lst
}

// pointListToAttr converts a JSON [][]any of [x,y] pairs into a deeply nested
// types.List (list of lists of int64), matching the points schemas.
func pointListToAttr(ctx context.Context, raw []any) types.List {
	var inner []types.List
	for _, p := range raw {
		pr, _ := p.([]any)
		inner = append(inner, numListToAttr(ctx, pr))
	}
	lst, _ := types.ListValueFrom(ctx, types.ListType{ElemType: types.Int64Type}, inner)
	return lst
}

// stringListToAttr converts a JSON []any of strings to a types.List.
func stringListToAttr(ctx context.Context, raw []any) types.List {
	var out []string
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	lst, _ := types.ListValueFrom(ctx, types.StringType, out)
	return lst
}

// attrToPointList converts a types.List of [x,y] int64 pairs into a [][]any
// (for bridge JSON params), or nil when unset/unknown.
func attrToPointList(ctx context.Context, v types.List) ([][]any, bool, diag.Diagnostics) {
	if v.IsNull() || v.IsUnknown() {
		return nil, false, nil
	}
	var pairs [][]int64
	if err := v.ElementsAs(ctx, &pairs, false); err != nil {
		return nil, false, diagErrf("Invalid points", "%v", err)
	}
	var out [][]any
	for _, p := range pairs {
		out = append(out, []any{p[0], p[1]})
	}
	return out, true, nil
}

// attrToNums converts a types.List of int64 (e.g. rect) to []any or nil.
func attrToNums(ctx context.Context, v types.List) []any {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	var nums []int64
	if err := v.ElementsAs(ctx, &nums, false); err != nil {
		return nil
	}
	out := make([]any, len(nums))
	for i, n := range nums {
		out[i] = n
	}
	return out
}
