// Command dd-launch starts Dungeondraft and bridge_app together — the
// "start" half of tying bridge_app's lifecycle to Dungeondraft's (the
// "end" half is bridge_app's own -watch-dd). Point a shortcut at the
// installed dd-launch.exe instead of Dungeondraft.exe directly.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	ddPath := flag.String("dd-path", envOr("DD_EXE_PATH", `C:\Program Files\Dungeondraft\Dungeondraft.exe`), "path to Dungeondraft.exe")
	flag.Parse()

	if _, err := os.Stat(*ddPath); err != nil {
		log.Fatalf("cannot find Dungeondraft at %s (pass -dd-path or set DD_EXE_PATH): %v", *ddPath, err)
	}

	selfDir, err := selfDirectory()
	if err != nil {
		log.Fatalf("locating dd-launch's own directory: %v", err)
	}
	bridgeAppPath := filepath.Join(selfDir, "bridge_app.exe")
	if _, err := os.Stat(bridgeAppPath); err != nil {
		log.Fatalf("cannot find bridge_app.exe next to dd-launch.exe at %s: %v", bridgeAppPath, err)
	}

	// Dungeondraft has no file logging of its own (its Godot project setting
	// for it is off) and bridge_app's stdout goes nowhere once detached with
	// no console attached — so a crash or a hang previously left zero trail
	// unless someone happened to launch from a terminal. Both now always get
	// a timestamped log file, paired by launch so a bad session's DD and
	// bridge_app output sit next to each other.
	logsDir, err := logsDirectory()
	if err != nil {
		log.Fatalf("creating logs directory: %v", err)
	}
	stamp := time.Now().Format("20060102_150405")
	ddLog := filepath.Join(logsDir, fmt.Sprintf("dd_%s.log", stamp))
	bridgeLog := filepath.Join(logsDir, fmt.Sprintf("bridge_%s.log", stamp))

	fmt.Printf("starting Dungeondraft: %s (log: %s)\n", *ddPath, ddLog)
	if err := startDetachedLogged(*ddPath, nil, ddLog); err != nil {
		log.Fatalf("starting Dungeondraft: %v", err)
	}

	fmt.Printf("starting bridge_app: %s (log: %s)\n", bridgeAppPath, bridgeLog)
	if err := startDetachedLogged(bridgeAppPath, []string{"-log", bridgeLog}, ""); err != nil {
		log.Fatalf("starting bridge_app: %v", err)
	}

	fmt.Println("both launched; dd-launch is done (bridge_app watches Dungeondraft and exits on its own once DD closes)")
}

// startDetachedLogged starts path detached from dd-launch. If ddLog is set,
// the child's stdout/stderr are redirected to that file (the handle is
// inherited by the child process and stays open after dd-launch exits,
// standard Windows CreateProcess behavior — the file isn't truncated when
// dd-launch's own copy of the handle closes). extraArgs are passed through
// as-is (used to hand bridge_app its own -log path instead).
func startDetachedLogged(path string, extraArgs []string, ddLog string) error {
	cmd := exec.Command(path, extraArgs...)
	cmd.Dir = filepath.Dir(path)
	if ddLog != "" {
		f, err := os.OpenFile(ddLog, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("opening log file %s: %w", ddLog, err)
		}
		cmd.Stdout = f
		cmd.Stderr = f
	}
	return cmd.Start()
}

// logsDirectory returns (creating if needed) the shared log directory next
// to the installed binaries' data root.
func logsDirectory() (string, error) {
	base, err := os.UserConfigDir() // %APPDATA% on Windows
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "DungeondraftBridge", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func selfDirectory() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
