package ddprovider

import (
	"context"

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