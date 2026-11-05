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

// savedPrecision is how far, relative to its size, a number Dungeondraft
// reports may sit from the one it was given. A saved map keeps 6 significant
// digits ("Vector2( 20181.2, 15841.5 )", at most 5e-6 off), with headroom; a
// one-woxel move at 20000 (5e-5) still counts as an edit.
const savedPrecision = 1e-5

// savedAngleDegrees is how far a reported rotation may sit from the one it
// was given: a saved map keeps radians to 6 decimal places (1.570796), at
// most 2.9e-5 degrees off.
const savedAngleDegrees = 1e-4

// reportedFloat is the value Read stores for a number Dungeondraft reports:
// the current one when the report only differs by Dungeondraft's own
// rounding (so a plan after apply or reopen shows no change), else the
// report (a real edit, drift).
func reportedFloat(reported float64, current types.Float64) types.Float64 {
	scale := math.Max(math.Abs(current.ValueFloat64()), 1)
	return keepIfWithin(reported, current, savedPrecision*scale)
}

// reportedAngle is reportedFloat for a rotation in degrees.
func reportedAngle(reported float64, current types.Float64) types.Float64 {
	return keepIfWithin(reported, current, savedAngleDegrees)
}

func keepIfWithin(reported float64, current types.Float64, tolerance float64) types.Float64 {
	if !current.IsNull() && !current.IsUnknown() && math.Abs(reported-current.ValueFloat64()) <= tolerance {
		return current
	}
	return types.Float64Value(reported)
}
