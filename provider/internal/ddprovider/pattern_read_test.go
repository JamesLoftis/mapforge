package ddprovider

import (
	"context"
	"testing"

	bridge "dungeondraft-mcp/bridgeclient"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// A rect-declared pattern whose state has no level (pre-level state)
// must record the level Dungeondraft reports, or every plan
// replaces it.
func TestRectPatternReadRecordsItsLevel(t *testing.T) {
	host, port := startReplyServer(t,
		`{"ok":true,"result":{"id":830,"kind":"pattern","level":"Ground","points":[[0,0],[256,0],[256,256],[0,256]]}}`)
	ctx := context.Background()
	r := &patternResource{newLevelPlacedBase()}
	r.client = bridge.New(host, port)

	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	state := tfsdk.State{Schema: sch.Schema, Raw: tftypes.NewValue(sch.Schema.Type().TerraformType(ctx), nil)}
	rect, _ := types.ListValueFrom(ctx, types.Int64Type, []int64{0, 0, 256, 256})
	state.Set(ctx, &patternModel{
		ID: types.StringValue("830"), Asset: types.StringValue("textures/a.png"), Rect: rect,
		Points: types.ListNull(pointElemType), Level: types.StringNull(),
		Category: types.StringNull(), Color: types.StringNull(), Rotation: types.Float64Null(), Z: types.Int64Null(),
	})

	resp := &resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	var got types.String
	resp.State.GetAttribute(ctx, path.Root("level"), &got)
	if got.ValueString() != "Ground" {
		t.Fatalf("level after Read = %v, want \"Ground\"", got)
	}
}
