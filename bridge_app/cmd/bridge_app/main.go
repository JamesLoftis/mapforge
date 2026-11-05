// Command bridge_app is the visible Dungeondraft bridge.
//
// It owns the wire-protocol TCP endpoint (default 127.0.0.1:8787) that the MCP
// server and Terraform provider talk to, plus an admin HTTP endpoint (default
// 127.0.0.1:8788) showing live status, connections, and command statistics.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"dungeondraft-mcp/bridge_app/internal/admin"
	"dungeondraft-mcp/bridge_app/internal/core"
	"dungeondraft-mcp/bridge_app/internal/ddwatch"
	"dungeondraft-mcp/bridge_app/internal/logpaths"
)

// version is stamped at build time (see scripts/install.ps1); "dev" for a
// plain `go build`.
var version = "dev"

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "wire-protocol listen address")
	adminAddr := flag.String("admin", "127.0.0.1:8788", "admin HTTP listen address")
	verbose := flag.Bool("v", false, "log every request/response frame")
	logFile := flag.String("log", "", "append logs to this file as well as the console")
	commandLogFile := flag.String("command-log", "", "append the command log (JSON lines) to this file "+
		`(default: commands_<stamp>.jsonl in %APPDATA%\DungeondraftBridge\logs)`)
	watchDD := flag.Bool("watch-dd", true, "exit once Dungeondraft.exe has been gone for -watch-grace (Windows only; ignored elsewhere)")
	watchInterval := flag.Duration("watch-interval", 5*time.Second, "how often to check whether Dungeondraft is still running")
	watchGrace := flag.Duration("watch-grace", 60*time.Second, "how long Dungeondraft may be gone before bridge_app exits")
	ddExeName := flag.String("dd-exe", "Dungeondraft.exe", "process name -watch-dd looks for")
	flag.Parse()

	admin.Version = version

	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			log.Fatalf("cannot open log file: %v", err)
		}
		defer f.Close()
		log.SetOutput(io.MultiWriter(os.Stdout, f))
	}

	fmt.Println("==============================================================")
	fmt.Printf("  Dungeondraft Bridge app (protocol v%d)\n", core.PROTOCOL_VERSION)
	fmt.Printf("  wire : tcp://%s\n", *addr)
	fmt.Printf("  admin: http://%s  (health: curl http://%s/health)\n", *adminAddr, *adminAddr)
	fmt.Println("==============================================================")

	commandLog, err := openCommandLog(*commandLogFile)
	if err != nil {
		log.Fatalf("cannot open command log: %v", err)
	}
	defer commandLog.Close()
	log.Printf("command log: %s", commandLog.Name())

	b := core.New(core.Options{Verbose: *verbose, CommandLog: commandLog})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("admin endpoint listening on http://%s", *adminAddr)
		if err := admin.New(b).Serve(*adminAddr); err != nil {
			log.Fatalf("admin server: %v", err)
		}
	}()

	if *watchDD && runtime.GOOS == "windows" {
		go watchDungeondraft(ctx, stop, ddwatch.IsRunning, *ddExeName, *watchInterval, *watchGrace)
	}

	if err := b.ListenAndServe(ctx, *addr); err != nil {
		log.Fatal(err)
	}
	log.Printf("shutting down")
}

// openCommandLog opens path for appending, or this launch's default file in
// the shared logs folder when path is empty.
func openCommandLog(path string) (*os.File, error) {
	if path == "" {
		dir, err := logpaths.Dir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(dir, logpaths.CommandLogName(logpaths.Stamp(time.Now())))
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

// watchDungeondraft exits the app once Dungeondraft has been observed
// running at least once and then gone for grace — a DD that never started
// this run (e.g. bridge_app launched standalone for testing) never triggers
// this; only a DD that WAS up and then closed does. grace is deliberately
// longer than a typical crash-and-relaunch cycle so iterating on the mod
// doesn't kill the bridge out from under you.
func watchDungeondraft(ctx context.Context, stop context.CancelFunc, isRunning func(string) (bool, error), exeName string, interval, grace time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	seenDD := false
	var goneSince time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		running, err := isRunning(exeName)
		if err != nil {
			log.Printf("ddwatch: %v", err)
			continue
		}
		if running {
			seenDD = true
			goneSince = time.Time{}
			continue
		}
		if !seenDD {
			continue
		}
		if goneSince.IsZero() {
			goneSince = time.Now()
			log.Printf("%s is no longer running; exiting in %s if it doesn't come back", exeName, grace)
			continue
		}
		if time.Since(goneSince) >= grace {
			log.Printf("%s has been gone for %s; shutting down", exeName, grace)
			stop()
			return
		}
	}
}
