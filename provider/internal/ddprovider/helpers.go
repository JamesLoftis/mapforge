package ddprovider

import (
	"context"
	"math"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// numAsInt64 coerces a JSON number (float64 or int64 from encoding/json) to int64.
func numAsInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case int:
		return int64(n)
	}
	return 0
}

// numAsFloat coerces a JSON number to float64.
func numAsFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}

// listOfNums builds a types.List of int64s from a JSON []any of numbers.
func listOfNums(ctx context.Context, v any) types.List {
	var out []int64
	if raw, ok := v.([]any); ok {
		for _, item := range raw {
			out = append(out, numAsInt64(item))
		}
	}
	lst, _ := types.ListValueFrom(ctx, types.Int64Type, out)
	return lst
}

// float32Tolerance is how far, relative to its size, a value Dungeondraft
// reports may sit from the one it was given: Godot stores positions,
// rotations and sizes as float32 (24-bit mantissa), with a little headroom
// for rotations, which pass through radians.
const float32Tolerance = 4.0 / (1 << 23)

// reportedFloat is the value Read stores for a number Dungeondraft reports:
// the current one when the report only differs by float32 rounding (so a
// plan after apply shows no change), else the report (a real edit, drift).
func reportedFloat(reported float64, current types.Float64) types.Float64 {
	if !current.IsNull() && !current.IsUnknown() && isFloat32Rounding(reported, current.ValueFloat64()) {
		return current
	}
	return types.Float64Value(reported)
}

func isFloat32Rounding(reported, original float64) bool {
	scale := math.Max(math.Abs(original), 1)
	return math.Abs(reported-original) <= float32Tolerance*scale
}
