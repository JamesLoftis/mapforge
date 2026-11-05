// Package logpaths names the log files dd-launch and bridge_app write, so a
// launch's Dungeondraft, bridge and command logs sit side by side.
package logpaths

import (
	"os"
	"path/filepath"
	"time"
)

// stampLayout pairs the files of one launch: dd_<stamp>.log,
// bridge_<stamp>.log and commands_<stamp>.jsonl.
const stampLayout = "20060102_150405"

// Dir returns %APPDATA%\DungeondraftBridge\logs, creating it if needed.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "DungeondraftBridge", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// Stamp is the launch stamp for t.
func Stamp(t time.Time) string { return t.Format(stampLayout) }

// CommandLogName is the command log's file name for a launch stamp.
func CommandLogName(stamp string) string { return "commands_" + stamp + ".jsonl" }
