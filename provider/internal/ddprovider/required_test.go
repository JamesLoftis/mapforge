package ddprovider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Plan 4 (classification rulings J1, J4): what the mod refuses to run
// without, the schema requires, so a config missing it fails at plan time.
func TestRequiredMeansRequired(t *testing.T) {
	required := map[string][]string{
		"dungeondraft_object":  {"x", "y"},
		"dungeondraft_light":   {"x", "y"},
		"dungeondraft_portal":  {"x", "y"},
		"dungeondraft_pattern": {"category"},
	}
	for _, r := range resourcesUnderTest {
		attrs, ok := required[r.name]
		if !ok {
			continue
		}
		var sch resource.SchemaResponse
		r.new().Schema(context.Background(), resource.SchemaRequest{}, &sch)
		for _, name := range attrs {
			if !sch.Schema.Attributes[name].IsRequired() {
				t.Errorf("%s.%s is not required; the mod refuses to run without it", r.name, name)
			}
		}
	}
}
