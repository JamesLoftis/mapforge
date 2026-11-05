package ddprovider

import (
	"context"
	"testing"

	bridge "dungeondraft-mcp/bridgeclient"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Values from a real-map acceptance run: Dungeondraft stores float32, so a
// configured 20181.248 comes back as 20181.248047.
func TestReportedFloatKeepsValueDungeondraftApproximates(t *testing.T) {
	cases := []struct{ reported, configured float64 }{
		{20181.248047, 20181.248},
		// A saved map keeps 6 significant digits ("Vector2( 20181.2, 15841.5 )").
		{20181.199219, 20181.248},
		{15841.5, 15841.536},
	}
	for _, c := range cases {
		if got := reportedFloat(c.reported, types.Float64Value(c.configured)); got.ValueFloat64() != c.configured {
			t.Errorf("reportedFloat(%v, %v) = %v; want the configured value kept", c.reported, c.configured, got)
		}
	}
}

// A saved map keeps rotations as radians to 6 decimal places (1.570796).
func TestReportedAngleKeepsValueDungeondraftApproximates(t *testing.T) {
	cases := []struct{ reported, configured float64 }{
		{90.000003, 90},
		{326.000011, 326},
		{12.000028, 12},
		{0.999983, 1},
	}
	for _, c := range cases {
		if got := reportedAngle(c.reported, types.Float64Value(c.configured)); got.ValueFloat64() != c.configured {
			t.Errorf("reportedAngle(%v, %v) = %v; want the configured value kept", c.reported, c.configured, got)
		}
	}
}

func TestReportedAngleTakesARealChange(t *testing.T) {
	if got := reportedAngle(12.01, types.Float64Value(12)); got.ValueFloat64() != 12.01 {
		t.Fatalf("a 0.01 degree turn came back as %v; want 12.01", got)
	}
}

func TestReportedFloatTakesARealChange(t *testing.T) {
	if got := reportedFloat(20182.248, types.Float64Value(20181.248)); got.ValueFloat64() != 20182.248 {
		t.Fatalf("a one-woxel move came back as %v; want 20182.248", got)
	}
}

func TestReportedFloatFillsAnUnknownValue(t *testing.T) {
	if got := reportedFloat(12.5, types.Float64Null()); got.ValueFloat64() != 12.5 {
		t.Fatalf("with no prior value got %v; want 12.5", got)
	}
}

func TestObjectReadKeepsConfiguredPosition(t *testing.T) {
	host, port := startReplyServer(t, `{"ok":true,"result":{"id":3458,"kind":"object","level":"Ground",`+
		`"position":[20181.248047,15841.536133],"rotation":90.000003,"scale":1}}`)
	ctx := context.Background()
	r := &objectResource{newLevelPlacedBase()}
	r.client = bridge.New(host, port)

	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	state := tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(sch.Schema.Type().TerraformType(ctx), nil)}
	var m objectModel
	state.Get(ctx, &m)
	m.ID, m.Level = types.StringValue("3458"), types.StringValue("Ground")
	m.X, m.Y, m.Rotation, m.Scale = types.Float64Value(20181.248), types.Float64Value(15841.536),
		types.Float64Value(90), types.Float64Value(1)
	state.Set(ctx, &m)

	resp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)
	var got objectModel
	resp.State.Get(ctx, &got)
	if got.X.ValueFloat64() != 20181.248 || got.Y.ValueFloat64() != 15841.536 || got.Rotation.ValueFloat64() != 90 {
		t.Fatalf("after Read x=%v y=%v rotation=%v; want 20181.248, 15841.536, 90", got.X, got.Y, got.Rotation)
	}
}
