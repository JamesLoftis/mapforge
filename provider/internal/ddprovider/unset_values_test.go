package ddprovider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	bridge "mapforge/bridgeclient"
)

// An optional, computed attribute the config leaves out is unknown in the
// create plan, not null. Sending it anyway sent its zero value (text size 0,
// wall joint 0 = sharp), which the mod used to cover up; it must not be sent,
// so the mod's documented default applies (found in plan 4, task 2).
func TestCreateDoesNotSendUnsetValues(t *testing.T) {
	ctx := context.Background()
	host, port, requests := startCapturingServer(t, `{"ok":true,"result":{"id":7,"kind":"text"}}`)
	r := &textResource{newLevelPlacedBase()}
	r.client = bridge.New(host, port)

	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	plan := tfsdk.Plan{Schema: sch.Schema, Raw: tftypes.NewValue(sch.Schema.Type().TerraformType(ctx), nil)}
	m := textModel{ID: types.StringUnknown(), Text: types.StringValue("note"), X: types.Float64Value(1),
		Y: types.Float64Value(2), Size: types.Int64Unknown(), Color: types.StringUnknown(),
		Font: types.StringUnknown(), Level: types.StringValue("Ground")}
	if diags := plan.Set(ctx, &m); diags.HasError() {
		t.Fatalf("building the plan: %v", diags)
	}

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sch.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, resp)
	sent := <-requests
	for _, unset := range []string{"size", "color", "font"} {
		if v, ok := sent[unset]; ok {
			t.Errorf("add_text sent %s = %v for a value the config left out", unset, v)
		}
	}
}
