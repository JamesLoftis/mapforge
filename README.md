# MapForge

An MCP server that lets an LLM (Claude Code, Claude Desktop, etc.) drive a
**running Dungeondraft instance** — place objects, draw walls, inspect the map —
through Dungeondraft's GDScript modding API.

Dungeondraft has no built-in server to talk to, so this project is **three
pieces**, all Go except the mod itself, that meet over a localhost socket —
with a standalone bridge app in the middle, so the wire endpoint is an obvious,
always-up process instead of a listener hidden inside Dungeondraft. (A
Terraform provider talks to the same bridge too, but it's developed in its own
sibling repo, `terraform-provider-dungeondraft` — see that repo, not this one,
for declarative/`.tf`-driven map building.)

```
Claude / MCP client
      │  MCP (stdio)
      ▼
mcpserver/   ── Go MCP server  (this repo)
      │  newline-delimited JSON over TCP  (127.0.0.1:8787)
      │  (via bridgeclient/, the shared wire client)
      ▼
bridge_app/  ── Dungeondraft Bridge app (Go, standalone)
      │  same protocol; forwards commands to the agent
      ▼
mod/      ── Dungeondraft mod  (GDScript, outbound agent inside DD)
      │  modding API calls
      ▼
   the open map  (Objects, Walls, Levels, …)
```

The bridge app owns `tcp://127.0.0.1:8787` and talks to the mod over the same
protocol in reverse: the mod dials the app as an **agent** and answers the user
commands the app forwards (see [PROTOCOL.md](PROTOCOL.md) for the roles). This
keeps Dungeondraft out of the "is the bridge up?" question — `ping` is answered
by the app, and a crashed mod shows up as a clear `dungeondraft offline` reply
instead of a refused connection. `bridge_app` also watches for Dungeondraft
itself: it exits shortly after `Dungeondraft.exe` closes, so it doesn't linger
for hours after a session ends the way a hand-started process can — see
[dd-launch](#1-install-everything), which starts both together. The app also
serves an admin page:

- `http://127.0.0.1:8788/` — live status (agent connected, heartbeats, connections, queue depth)
- `http://127.0.0.1:8788/health` — JSON health for scripts
- `http://127.0.0.1:8788/connections` — who is connected right now

> **Status: working.** Confirmed end-to-end against Dungeondraft on **Godot
> 3.4.2** — raw TCP/JSON agent bridging works, no fallback needed. The MCP
> server is look-only: 15 tools across query / capture / view (see below).
> Maps are built with the Terraform provider in `provider/`.

## What the AI can do

The MCP server only looks at the map; it cannot create, change, delete,
paint, select or undo anything. Build and change maps with Terraform
(`provider/README.md`), so every change goes through a plan and lands in
state.

- **Inspect:** `get_status`, `list_levels`, `list_elements`, `get_element`,
  `dump_state` (whole-level snapshot in one call),
  `list_asset_categories`, `list_assets` (with substring search).
- **See:** `screenshot` (current window) and `export_map` (clean full-map
  render) return images, so the model can look at its own work and iterate.
- **View:** `set_level` (which level the editor shows), `get_camera`,
  `set_camera`, `focus_element` (center on one
  element), `fit_elements` (frame a group) — point the view before a
  `screenshot` to inspect specific spots like a door cut into a wall.

Every element is referenced by an integer `id`; list calls return ids you
pass to `get_element` and `focus_element`. Coordinates are woxel (pixel) space — call
`get_status` for `map_center`. Discover assets with `list_assets`.

## Setup

**Prerequisites:** [Dungeondraft](https://dungeondraft.net/) (tested on the
Godot 3.4.2 builds), Go **1.25+**, and an MCP client (e.g. Claude Code or
Claude Desktop). PowerShell is used for the install script; everything else is
plain `go build`.

### 1. Install everything

1. In Dungeondraft's title screen, open **Mods** and note (or set) your mods
   folder, then copy `mod/dungeondraft-mcp-bridge-agent/` into it and enable
   **TF Bridge Agent** in the mod list.
2. Build and install the three Go binaries to a stable location outside the
   repo (`%LOCALAPPDATA%\DungeondraftBridge\bin\`), version-stamped from
   `git describe` so a stale build is never a guessing game:

   ```powershell
   .\scripts\install.ps1
   ```

   Re-run it after every `git pull` or local edit — it *is* the deploy step.
3. From now on, launch Dungeondraft via the installed `dd-launch.exe` instead
   of `Dungeondraft.exe` directly (point your shortcut at it) — it starts DD
   and `bridge_app.exe` together. `bridge_app` then watches for DD's process
   and exits on its own a little while after DD closes, so nothing lingers.

On DD's load you should see in its log:

```
[mcp-bridge] agent mode: bridging to tcp://127.0.0.1:8787 (protocol v17)
[mcp-bridge] connected to bridge app at 127.0.0.1:8787
```

The mod doesn't listen for connections — it dials the bridge app, so it's fine
(and normal) for the mod to load before or after the app is started; it
retries with backoff. If the mod isn't connected yet,
`curl http://127.0.0.1:8788/health` still answers `200` with
`dd_connected: false` — that's the point: "app up, Dungeondraft down" is
visible at a glance, not a refused connection.

### 2. Verify the bridge works

With DD open and a map loaded, `curl http://127.0.0.1:8788/health` confirms
`dd_connected: true`, and calling `ping`/`get_status` through your MCP client
confirms the whole chain end to end.

### 3. Wire up your MCP client

Register the installed binary by its **absolute path**. For **Claude Code**:

```powershell
claude mcp add dungeondraft -s user -- "$env:LOCALAPPDATA\DungeondraftBridge\bin\mcpserver.exe"
```

Reconnect (`/mcp` in Claude Code) so the server loads. Or add to a client
config (e.g. Claude Desktop `claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "dungeondraft": {
      "command": "C:/Users/you/AppData/Local/DungeondraftBridge/bin/mcpserver.exe"
    }
  }
}
```

Then ask the model things like *"what's the status of my Dungeondraft map?"* or
*"list some object assets and drop a chair in the middle of the map."*

## Configuration

These env vars configure `mcpserver` (set them where the MCP client launches
it). The defaults match the bridge app, so you only need them if you run the
app on a different port (`-addr`).

| Env var | Default | Purpose |
| --- | --- | --- |
| `DD_BRIDGE_HOST` | `127.0.0.1` | host the server connects to |
| `DD_BRIDGE_PORT` | `8787` | port the server connects to (match the app's `-addr`) |

`bridge_app` has its own flags (`-addr`, `-admin`, `-watch-dd`, `-watch-interval`,
`-watch-grace`, `-dd-exe`) — see `bridge_app/README.md`.

## Extending

Adding a capability is symmetric — one handler on each side:

1. **Mod** (`mod/.../scripts/tools/mcp_bridge.gd`): add a `case` to the `match`
   in `_safe_dispatch()` and a `_my_command(req)` function returning
   `_ok(...)` / `_err(...)`.
2. **MCP server** (`mcpserver/cmd/mcpserver/`): add an input struct + handler
   method (follow the pattern in `tools_*.go` — build a `params` map, call
   `a.forward(ctx, "my_command", params)`), then register it with `addTool(...)`
   in `register.go`. (If `terraform-provider-dungeondraft` should expose it
   too, that's a third, separate change in that repo.)

Good next targets: pattern shapes (floors) and region-scoped terrain fill.

### Dev loop (read this before iterating on the mod)

Three things will bite you if you don't know them up front:

- **The bridge app, the mod, and the MCP server reload differently.**
  GDScript changes in `mcp_bridge.gd` take effect when you **reload the mod
  inside Dungeondraft** (or restart DD). Changes to `bridge_app` or
  `mcpserver` need a rebuild — run `.\scripts\install.ps1` — then, for
  `mcpserver`, an **MCP reconnect** (`/mcp` in Claude Code); for `bridge_app`,
  restart it (or just close/reopen DD via `dd-launch`, since it now watches
  DD's lifecycle). Adding a *new* command needs at least the mod's handler and
  `mcpserver`'s tool registration; if `terraform-provider-dungeondraft` should
  expose it too, that's a separate change in that sibling repo.

- **There's no live `eval`/introspect command**, so probing a running node's
  properties needs a mod reload. When a DD API behaves unexpectedly (e.g. a
  setter that doesn't stick), a quick way to diagnose it is to **return
  intermediate state in the response** — stash before/after values in a debug
  field — so one reload shows where a value changes.

- **A crashed mod no longer kills the bridge.** The app keeps running and
  reports `dungeondraft offline: bridge agent not connected`. If the mod
  crashes, DD's log will show the GDScript error — if DD itself isn't running
  from a console you won't see it; relaunching DD from a terminal
  (`& "C:\Program Files\Dungeondraft\Dungeondraft.exe"`) surfaces stdout/stderr,
  including `SCRIPT ERROR:` parse failures that otherwise fail completely
  silently (a single bad line in the mod script aborts Dungeondraft's *entire*
  mod loader, not just this mod). Restart DD (or reload the mod) and the agent
  reconnects with backoff.

## Layout

```
bridgeclient/                  shared Go wire-protocol client (this repo's mcpserver/ uses it;
                                terraform-provider-dungeondraft, a sibling repo, vendors its own copy)
  client.go                    dial-per-request client, retry/backoff, error classification
  client_test.go               offline tests (fake bridge over a real socket)
mcpserver/                     the Go MCP server (this is what an MCP client launches)
  cmd/mcpserver/main.go        entrypoint (stdio transport)
  cmd/mcpserver/register.go    every tool's name/description -> handler wiring
  cmd/mcpserver/tools_*.go     input structs + handlers, grouped like PROTOCOL.md
  cmd/mcpserver/tools_capture.go  screenshot/export_map (the two tools returning images)
  cmd/mcpserver/limits.go      context-size guardrails (capped limit/ppi, dump_state/image size ceilings)
bridge_app/                    the standalone Dungeondraft Bridge app (Go)
  cmd/bridge_app/main.go       entrypoint (wire :8787, admin :8788, DD-lifecycle watcher)
  cmd/dd-launch/main.go        starts Dungeondraft.exe + bridge_app.exe together
  internal/core/bridge.go      TCP server, agent link, forwarding, stats, queue depth
  internal/core/bridge_test.go offline tests (fake agent + users over sockets)
  internal/admin/admin.go      /health, /connections, status page
  internal/ddwatch/            "is Dungeondraft.exe running?" (Windows toolhelp snapshot)
mod/dungeondraft-mcp-bridge-agent/   the Dungeondraft mod (copy into DD's mods folder)
  mcp_bridge.ddmod             manifest
  scripts/tools/mcp_bridge.gd  outbound agent + command handlers
scripts/install.ps1            builds + deploys mcpserver/bridge_app/dd-launch to %LOCALAPPDATA%\DungeondraftBridge\bin\
PROTOCOL.md                    wire protocol + implementation notes (shared contract with terraform-provider-dungeondraft)
go.work                        ties bridgeclient/bridge_app/mcpserver together for local dev
```

## Credits

Built against the [Dungeondraft Modding API](https://megasploot.github.io/DungeondraftModdingAPI/).
Engine is Godot 3.4.2, so the GDScript uses Godot 3 networking class names.

## License

[MIT](LICENSE).
