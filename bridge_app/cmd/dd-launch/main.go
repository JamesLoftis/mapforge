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

	fmt.Printf("starting Dungeondraft: %s\n", *ddPath)
	if err := startDetached(*ddPath); err != nil {
		log.Fatalf("starting Dungeondraft: %v", err)
	}

	fmt.Printf("starting bridge_app: %s\n", bridgeAppPath)
	if err := startDetached(bridgeAppPath); err != nil {
		log.Fatalf("starting bridge_app: %v", err)
	}

	fmt.Println("both launched; dd-launch is done (bridge_app watches Dungeondraft and exits on its own once DD closes)")
}

func startDetached(path string) error {
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	return cmd.Start()
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
