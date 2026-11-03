//go:build integration

package integration

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	providerSource   = "registry.terraform.io/jamesloftis/dungeondraft"
	providerVersion  = "0.1.0"
	providerPlatform = "windows_amd64"
	providerExe      = "terraform-provider-dungeondraft_v" + providerVersion + ".exe"
)

// TestTerraformUsesProviderBuiltFromThisTree guards the characterization
// test against proving behavior on a stale installed provider.
func TestTerraformUsesProviderBuiltFromThisTree(t *testing.T) {
	tf := newTerraform(t)
	tf.run(t, "init", "-input=false")

	installed := filepath.Join(tf.dir, ".terraform", "providers", providerSource,
		providerVersion, providerPlatform, providerExe)
	if got, want := fileSHA256(t, installed), fileSHA256(t, tf.builtProvider); got != want {
		t.Fatalf("terraform init installed %s (sha256 %s), not the provider built from this tree "+
			"(%s, sha256 %s)", installed, got, tf.builtProvider, want)
	}
}

// buildProvider builds ../provider into a filesystem-mirror layout under a
// temp dir and returns the mirror root and the built binary's path.
func buildProvider(t *testing.T) (mirror, binary string) {
	t.Helper()
	mirror = filepath.Join(t.TempDir(), "mirror")
	dir := filepath.Join(mirror, providerSource, providerVersion, providerPlatform)
	binary = filepath.Join(dir, providerExe)
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = filepath.Join("..", "provider")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the provider: %v\n%s", err, out)
	}
	return mirror, binary
}

// writeCLIConfig writes a Terraform CLI config that installs this provider
// only from mirror, and every other provider as usual.
func writeCLIConfig(t *testing.T, mirror string) string {
	t.Helper()
	config := fmt.Sprintf(`provider_installation {
  filesystem_mirror {
    path    = %q
    include = [%q]
  }
  direct {
    exclude = [%q]
  }
}
`, filepath.ToSlash(mirror), providerSource, providerSource)
	path := filepath.Join(t.TempDir(), "terraform.rc")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatalf("writing terraform CLI config: %v", err)
	}
	return path
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("hashing %s: %v", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("hashing %s: %v", path, err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
