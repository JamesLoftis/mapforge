//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"testing"
)

const providerExamples = "../provider/examples"

// TestProviderExamplesValidate keeps the provider's shipped examples in step
// with its schema.
func TestProviderExamplesValidate(t *testing.T) {
	examples, err := os.ReadDir(providerExamples)
	if err != nil {
		t.Fatalf("listing examples: %v", err)
	}
	for _, example := range examples {
		t.Run(example.Name(), func(t *testing.T) {
			tf := newTerraformWith(t, readExample(t, filepath.Join(providerExamples, example.Name())))
			tf.run(t, "init", "-input=false")
			tf.run(t, "validate")
		})
	}
}

func readExample(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	files := map[string]string{}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".tf" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		files[e.Name()] = string(content)
	}
	return files
}
