//go:build integration

package integration

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

var replacementLine = regexp.MustCompile(`# (\S+) must be replaced`)

const importConfig = `
terraform {
  required_providers {
    dungeondraft = { source = "registry.terraform.io/jamesloftis/dungeondraft" }
  }
}
variable "map_file" { type = string }
provider "dungeondraft" {
  map_file = var.map_file
  level    = "Ground"
}
resource "dungeondraft_light" "lamp" {
  x     = 100
  y     = 100
  level = "Upper"
}
`

// TestImportedElementKeepsItsLevel: an imported element must not be
// replaced just because import cannot know its level; Read reports it.
func TestImportedElementKeepsItsLevel(t *testing.T) {
	tf := newTerraformWith(t, map[string]string{"main.tf": importConfig})
	tf.mapFile = scratchMap(t)
	addLevel(ctx(t), t, upperLevel)
	res, err := dd.Request(ctx(t), "add_light", map[string]any{"x": 100, "y": 100, "level": upperLevel})
	if err != nil {
		t.Fatalf("add_light: %v", err)
	}
	tf.run(t, "init", "-input=false")
	tf.run(t, "import", "-input=false", "dungeondraft_light.lamp", fmt.Sprint(res["id"]))

	_, plan := tf.output(t, "plan", "-input=false")
	if replaced := replacementLine.FindAllString(plan, -1); len(replaced) > 0 {
		t.Fatalf("plan after import replaces %v; an imported element on its configured level must stay", replaced)
	}
}

// TestRelabelingLevelReplacesItsElements: an element referencing its level
// by id (the label) must be planned for replacement when the level is
// relabeled, not fail at apply with an inconsistent final plan.
func TestRelabelingLevelReplacesItsElements(t *testing.T) {
	fixture, err := os.ReadFile(characterizeFixture)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	byID := strings.Replace(string(fixture), "dungeondraft_level.upper.label", "dungeondraft_level.upper.id", 1)
	tf := newTerraformWith(t, map[string]string{"main.tf": byID})
	tf.mapFile = scratchMap(t)
	tf.run(t, "init", "-input=false")
	tf.run(t, "apply", "-auto-approve", "-input=false")
	t.Cleanup(func() { tf.run(t, "destroy", "-auto-approve", "-input=false") })

	relabeled := strings.Replace(byID, `label = "Upper"`, `label = "Upper Two"`, 1)
	if err := os.WriteFile(tf.dir+"/main.tf", []byte(relabeled), 0o644); err != nil {
		t.Fatalf("rewriting config: %v", err)
	}
	if code, out := tf.output(t, "apply", "-auto-approve", "-input=false"); code != 0 {
		t.Fatalf("apply after relabeling the level exited %d:\n%s", code, lastLines(out, 12))
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
