//go:build integration

package integration

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	characterizeFixture = "testdata/characterize/main.tf"
)

// TestCharacterizeApplySaveReopenPlan pins today's end-to-end behavior: the
// fixture applies, survives save and reopen with no drift, and destroys clean.
func TestCharacterizeApplySaveReopenPlan(t *testing.T) {
	tf := newTerraform(t)
	path := scratchMap(t)
	tf.mapFile = path

	tf.run(t, "init", "-input=false")
	tf.run(t, "apply", "-auto-approve", "-input=false")
	saveAndReopen(t, path)

	code, _ := tf.output(t, "plan", "-detailed-exitcode", "-input=false")
	if code != 0 {
		t.Errorf("plan after save and reopen exited %d; want 0 (no changes): levels are "+
			"identified by label, which Dungeondraft saves", code)
	}

	tf.run(t, "destroy", "-auto-approve", "-input=false")
	st, err := dd.Status(ctx(t))
	if err != nil {
		t.Fatalf("status after destroy: %v", err)
	}
	if n := totalElements(st); n != 0 {
		t.Errorf("after destroy the map still holds %d elements (%s)", n, describeCounts(st))
	}
}

// terraform runs the terraform CLI in a private copy of the fixture, using
// the provider built from this tree (see buildProvider).
type terraform struct {
	binary        string
	dir           string
	builtProvider string
	cliConfig     string
	// mapFile is the fixture's map_file variable (TF_VAR_map_file).
	mapFile string
}

func newTerraform(t *testing.T) terraform {
	t.Helper()
	binary, err := exec.LookPath("terraform")
	if err != nil {
		t.Skip("terraform CLI not on PATH; install Terraform >= 1.5 to run the characterization test")
	}
	dir := t.TempDir()
	fixture, err := os.ReadFile(characterizeFixture)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), fixture, 0o644); err != nil {
		t.Fatalf("copying fixture: %v", err)
	}
	mirror, built := buildProvider(t)
	return terraform{binary: binary, dir: dir, builtProvider: built, cliConfig: writeCLIConfig(t, mirror)}
}

func (tf terraform) run(t *testing.T, args ...string) {
	t.Helper()
	if code, _ := tf.output(t, args...); code != 0 {
		t.Fatalf("terraform %s exited %d", args[0], code)
	}
}

// output runs terraform and returns its exit code and output, logging the output.
func (tf terraform) output(t *testing.T, args ...string) (int, string) {
	t.Helper()
	cmd := exec.CommandContext(ctx(t), tf.binary, append(args, "-no-color")...)
	cmd.Dir = tf.dir
	cmd.Env = append(os.Environ(), "TF_IN_AUTOMATION=1", "TF_CLI_CONFIG_FILE="+tf.cliConfig,
		"TF_VAR_map_file="+tf.mapFile)
	out, err := cmd.CombinedOutput()
	t.Logf("terraform %s:\n%s", args[0], out)
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), string(out)
	}
	t.Fatalf("running terraform %s: %v", args[0], err)
	return -1, ""
}
