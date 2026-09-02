package ddprovider

import (
	"context"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// resourcesUnderTest mirrors provider.Resources() so a schema/metadata wiring
// error fails here with a clear message instead of at terraform provider load.
var resourcesUnderTest = []struct {
	name string
	new  func() resource.Resource
}{
	{"dungeondraft_wall", NewWallResource},
	{"dungeondraft_portal", NewPortalResource},
	{"dungeondraft_object", NewObjectResource},
	{"dungeondraft_light", NewLightResource},
	{"dungeondraft_roof", NewRoofResource},
	{"dungeondraft_text", NewTextResource},
	{"dungeondraft_pattern", NewPatternResource},
	{"dungeondraft_path", NewPathResource},
	{"dungeondraft_terrain_fill", NewTerrainFillResource},
	{"dungeondraft_room", NewRoomResource},
}

// dataSourcesUnderTest mirrors provider.DataSources().
var dataSourcesUnderTest = []struct {
	name string
	new  func() datasource.DataSource
}{
	{"dungeondraft_status", NewStatusDataSource},
	{"dungeondraft_assets", NewAssetsDataSource},
	{"dungeondraft_asset_categories", NewAssetCategoriesDataSource},
	{"dungeondraft_map", NewMapDataSource},
}

func TestResourceSchemas(t *testing.T) {
	ctx := context.Background()
	for _, rc := range resourcesUnderTest {
		inst := rc.new()
		var mresp resource.MetadataResponse
		inst.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "dungeondraft"}, &mresp)
		if mresp.TypeName != rc.name {
			t.Errorf("%s: TypeName = %q, want %q", rc.name, mresp.TypeName, rc.name)
		}
		var sresp resource.SchemaResponse
		inst.Schema(ctx, resource.SchemaRequest{}, &sresp)
		if sresp.Diagnostics.HasError() {
			t.Fatalf("%s: schema diagnostics: %v", rc.name, sresp.Diagnostics)
		}
		if len(sresp.Schema.Attributes) == 0 {
			t.Errorf("%s: empty schema", rc.name)
		}
		if _, ok := sresp.Schema.Attributes["id"]; !ok {
			t.Errorf("%s: missing computed id attribute", rc.name)
		}
		for attr, spec := range sresp.Schema.Attributes {
			m, _ := regexp.MatchString(`^[a-z0-9_]+$`, attr)
			if !m {
				t.Errorf("%s: bad attribute name %q (lowercase snake_case expected)", rc.name, attr)
			}
			if spec.IsComputed() && spec.IsRequired() {
				t.Errorf("%s.%s: attribute is both computed and required", rc.name, attr)
			}
		}
	}
}

func TestDataSourceSchemas(t *testing.T) {
	ctx := context.Background()
	for _, dc := range dataSourcesUnderTest {
		inst := dc.new()
		var mresp datasource.MetadataResponse
		inst.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "dungeondraft"}, &mresp)
		if mresp.TypeName != dc.name {
			t.Errorf("%s: TypeName = %q, want %q", dc.name, mresp.TypeName, dc.name)
		}
		var sresp datasource.SchemaResponse
		inst.Schema(ctx, datasource.SchemaRequest{}, &sresp)
		if sresp.Diagnostics.HasError() {
			t.Fatalf("%s: schema diagnostics: %v", dc.name, sresp.Diagnostics)
		}
		if len(sresp.Schema.Attributes) == 0 {
			t.Errorf("%s: empty schema", dc.name)
		}
		for attr, spec := range sresp.Schema.Attributes {
			m, _ := regexp.MatchString(`^[a-z0-9_]+$`, attr)
			if !m {
				t.Errorf("%s: bad attribute name %q (lowercase snake_case expected)", dc.name, attr)
			}
			if spec.IsComputed() && spec.IsRequired() {
				t.Errorf("%s.%s: attribute is both computed and required", dc.name, attr)
			}
		}
	}
}

func TestAllDataSourcesRegistered(t *testing.T) {
	want := make(map[string]bool)
	for _, dc := range dataSourcesUnderTest {
		want[dc.name] = true
	}
	got := map[string]bool{}
	p := &ddProvider{}
	for _, f := range p.DataSources(context.Background()) {
		var mresp datasource.MetadataResponse
		f().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "dungeondraft"}, &mresp)
		got[mresp.TypeName] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("data source %s registered in test list but not in provider.DataSources()", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("data source %s registered in provider.DataSources() but missing from test list", name)
		}
	}
}

func TestAllResourcesRegistered(t *testing.T) {
	want := make(map[string]bool)
	for _, rc := range resourcesUnderTest {
		want[rc.name] = true
	}
	got := map[string]bool{}
	p := &ddProvider{}
	for _, f := range p.Resources(context.Background()) {
		var mresp resource.MetadataResponse
		f().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dungeondraft"}, &mresp)
		got[mresp.TypeName] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("resource %s registered in test list but not in provider.Resources()", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("resource %s registered in provider.Resources() but missing from test list", name)
		}
	}
}
