package ddprovider

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

const mapFileExtension = ".dungeondraft_map"

// mapFilePath requires an absolute path to a .dungeondraft_map, checked at
// plan time.
type mapFilePath struct{}

func (mapFilePath) Description(context.Context) string {
	return "an absolute path ending in " + mapFileExtension
}

func (v mapFilePath) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v mapFilePath) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	path := req.ConfigValue.ValueString()
	if !filepath.IsAbs(path) || !strings.HasSuffix(strings.ToLower(path), mapFileExtension) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid map_file",
			"map_file must be "+v.Description(ctx)+"; got "+path)
	}
}
