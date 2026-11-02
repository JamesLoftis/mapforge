// cmd/tfcheck validates the example .tf plans against the provider's actual
// registered schema (run where no terraform binary is available):
//
//	go run ./cmd/tfcheck ../examples
//
// For every file it checks:
//   - the file parses as valid HCL (the same parser terraform uses)
//   - each resource/data-source/provider block type is actually registered
//   - every attribute written on a block exists in that schema
//   - every schema-required attribute is present
//
// It deliberately does NOT evaluate expressions (locals, references, try()),
// which is what terraform binary's validate/plan adds on top.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"dungeondraft-mcp/provider/internal/ddprovider"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

type attrSet struct {
	required map[string]bool
	all      map[string]bool
}

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		fmt.Fprintln(os.Stderr, "usage: tfcheck <dir> [<dir>...]")
		os.Exit(2)
	}

	p := ddprovider.New()
	resources, datasources := schemaSets(p)
	providerAttrs := providerAttrs(p)

	parser := hclparse.NewParser()
	failed := false
	for _, root := range roots {
		files, err := filepath.Glob(filepath.Join(root, "*.tf"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "glob: %v\n", err)
			os.Exit(1)
		}
		for _, f := range files {
			file, diags := parser.ParseHCLFile(f)
			if diags.HasErrors() {
				failed = true
				fmt.Printf("FAIL %s (parse)\n", f)
				for _, d := range diags {
					fmt.Printf("  %s\n", d.Summary)
				}
				continue
			}
			blocks := file.Body.(*hclsyntax.Body).Blocks
			ok := true
			for _, block := range blocks {
				if msg := validateBlock(block, resources, datasources, providerAttrs); msg != "" {
					ok = false
					fmt.Printf("FAIL %s: %s\n", f, msg)
				}
			}
			if ok {
				fmt.Printf("ok   %s (%d block(s))\n", f, len(blocks))
			}
		}
	}
	if failed {
		fmt.Println("example plans did NOT validate")
		os.Exit(1)
	}
	fmt.Println("all example plans validate against the provider schema")
}

func validateBlock(block *hclsyntax.Block, resources, datasources map[string]attrSet, providerAttrs attrSet) string {
	switch block.Type {
	case "terraform", "locals", "variable", "output":
		return ""
	case "provider":
		return checkAttrs(block, providerAttrs, "provider")
	case "resource":
		if len(block.Labels) != 2 {
			return "resource block needs a type and name label"
		}
		set, ok := resources[block.Labels[0]]
		if !ok {
			return fmt.Sprintf("unknown resource type %q", block.Labels[0])
		}
		return checkAttrs(block, set, "resource "+block.Labels[0])
	case "data":
		if len(block.Labels) != 2 {
			return "data block needs a type and name label"
		}
		set, ok := datasources[block.Labels[0]]
		if !ok {
			return fmt.Sprintf("unknown data source type %q", block.Labels[0])
		}
		return checkAttrs(block, set, "data "+block.Labels[0])
	default:
		return fmt.Sprintf("unknown block type %q", block.Type)
	}
}

func checkAttrs(block *hclsyntax.Block, set attrSet, what string) string {
	body := block.Body
	given := map[string]bool{}
	for name := range body.Attributes {
		given[name] = true
		if !set.all[name] {
			return fmt.Sprintf("%s: attribute %q is not in the schema", what, name)
		}
	}
	for req := range set.required {
		if !given[req] {
			return fmt.Sprintf("%s: missing required attribute %q", what, req)
		}
	}
	return ""
}

// schemaSets loads every resource and data source through the provider entry
// points, exactly as terraform would when it registers them.
func schemaSets(p provider.Provider) (map[string]attrSet, map[string]attrSet) {
	resources := map[string]attrSet{}
	for _, f := range p.Resources(context.Background()) {
		r := f()
		resources[resourceName(r)] = attrSetFrom(resourceSchema(r).Schema.Attributes)
	}
	datasources := map[string]attrSet{}
	for _, f := range p.DataSources(context.Background()) {
		d := f()
		datasources[dataSourceName(d)] = attrSetFrom(dataSourceSchema(d).Schema.Attributes)
	}
	return resources, datasources
}

func providerAttrs(p provider.Provider) attrSet {
	var s provider.SchemaResponse
	p.Schema(context.Background(), provider.SchemaRequest{}, &s)
	return attrSetFrom(s.Schema.Attributes)
}

// attrSetFrom is generic over every schema package's `Attribute` interface
// (resource/schema, datasource/schema, provider/schema — identical method set).
func attrSetFrom[T interface{ IsRequired() bool }](m map[string]T) attrSet {
	set := attrSet{required: map[string]bool{}, all: map[string]bool{}}
	for name, spec := range m {
		set.all[name] = true
		if spec.IsRequired() {
			set.required[name] = true
		}
	}
	return set
}

func resourceName(r resource.Resource) string {
	ctx := context.Background()
	var m resource.MetadataResponse
	r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "dungeondraft"}, &m)
	return m.TypeName
}

func resourceSchema(r resource.Resource) *resource.SchemaResponse {
	var s resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &s)
	return &s
}

func dataSourceName(d datasource.DataSource) string {
	ctx := context.Background()
	var m datasource.MetadataResponse
	d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "dungeondraft"}, &m)
	return m.TypeName
}

func dataSourceSchema(d datasource.DataSource) *datasource.SchemaResponse {
	var s datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &s)
	return &s
}
