# MapForge

Builds Dungeondraft maps with Terraform. The pieces, all in this repo:

- `provider/`: the Terraform provider, the **only** way maps get built or changed.
- `mcpserver/`: a **look-only** MCP server (status, elements, screenshots,
  camera). It cannot create, change or delete anything.
- `bridge_app/`: the always-up relay on `127.0.0.1:8787` (admin page on `:8788`)
  that logs every command; `dd-launch` starts it with Dungeondraft.
- `bridgeclient/`: the shared Go wire client the provider and `mcpserver` use.
- `mod/`: the **TF Bridge Agent** mod, one GDScript file running inside
  Dungeondraft, which dials out to `bridge_app`.

See [README.md](README.md) for the architecture and use, and
[PROTOCOL.md](PROTOCOL.md) for every command, error code and the protocol
version. A wire change (a new command, a changed parameter or result) touches
the mod, `PROTOCOL.md`, and usually the provider (`provider/internal/ddprovider/`);
a protocol version bump changes `PROTOCOL_VERSION` in both `mcp_bridge.gd` and
`bridge_app/internal/core/bridge.go`.

## Before you call any MCP tool

The tools talk to a live Dungeondraft, not this repo. They only work when
Dungeondraft is running with the **TF Bridge Agent** mod enabled and a map is
open. **Verify first:** `ping` (expect `pong` and `dd_connected: true`), then
`get_status` (expect `map_open: true`). If `ping` shows `dd_connected: false`,
ask the user to start Dungeondraft; don't retry blindly.

## Working with the map

- Build and change maps by writing Terraform, never through MCP.
- Coordinates are **woxel** (world-pixel) space, 256 per tile; `get_status`
  gives `map_center`.
- Every element has an integer `id`; `list_elements` / `get_element` report it.
- Find assets with `list_assets` (`category` plus a substring `search`) and
  `list_asset_categories`, or `go run ./cmd/catalog` in `provider/`.
- **Look at your work**: `screenshot` and `export_map`, aimed with `set_camera`,
  `focus_element` and `fit_elements`.

## Editing MapForge itself

- Re-run `scripts\install.ps1` after changing Go code (it builds the binaries,
  the provider and the mod link); `/mcp` reconnect for `mcpserver`; restart
  Dungeondraft for the mod and `bridge_app`.
- Unit tests: `go test ./...` per module; script tests:
  `Invoke-Pester scripts/tests` (Windows PowerShell 5.1). Live tests:
  `go test -tags integration ./...` in `integration/`, with a **scratch map**
  open, never a real one.
- The mod must stay **one `.gd` file**: a second script anywhere in its folder
  crashes Dungeondraft's mod loader.
- GDScript on Godot 3.4.2 has **no try/catch**, and a **parse error anywhere in
  `mcp_bridge.gd`** silently stops Dungeondraft loading **every** mod: the mod
  never connects and the UI shows nothing wrong. The `SCRIPT ERROR:` is in
  `%APPDATA%\DungeondraftBridge\logs\dd_<stamp>.log` (written by `dd-launch`).
  If the mod ever stops connecting with no obvious cause, check for this first.
