# dungeondraft-mcp

An MCP server that drives a **running Dungeondraft instance** over a localhost
socket. Three pieces: a Go MCP server (`mcpserver/`), a standalone Go bridge
daemon (`bridge_app/`) that owns the actual TCP port, a shared Go wire client
(`bridgeclient/`) it uses, and a GDScript mod (`mod/`) that runs inside
Dungeondraft as an outbound agent. See [README.md](README.md) for the
architecture and [PROTOCOL.md](PROTOCOL.md) for every command + implementation
notes.

A Terraform provider (`terraform-provider-dungeondraft`) talks to the same
`bridge_app` but lives in its own sibling repo, not here — it vendors its own
copy of the wire client rather than depending on this repo's `bridgeclient/`.
If you change the wire protocol (a new command, a changed param/result shape),
update `PROTOCOL.md` here — it's the contract both repos build against — and
flag to the user that the sibling repo's `internal/ddprovider/` may need a
matching change; this repo's tools can't see or edit that repo.

## Before you call any tool

The tools talk to a live Dungeondraft, not this repo. They only work when:

1. **Dungeondraft is running** with the **MCP Bridge mod enabled**, and
2. **a map is open**.

If both aren't true, calls fail (connection refused, or `"no map open"`).
**Verify first:** call `ping` (expect `pong` + a protocol version), then
`get_status` (expect `map_open: true` and a `map_center`). If `ping` fails, the
mod isn't loaded or DD isn't running — ask the user to start it; don't retry
blindly.

## Working with the map

- Coordinates are **woxel** (world-pixel) space; get `map_center` from
  `get_status`. There are 256 woxels per tile.
- Every element has an integer `id`; create calls return it, `list_elements` /
  `get_element` report it. Feed ids back into move/modify/delete.
- Discover assets with `list_assets` (it takes a `category` and a substring
  `search`); list categories with `list_asset_categories`.
- **Look at your work**: `screenshot` (current view) and the camera tools
  (`set_camera`, `focus_element`, `fit_elements`) return/aim the view so you can
  inspect what you built and iterate. Use them.
- The bridge keeps its **own** `undo` / `redo` stacks (independent of DD's
  Ctrl+Z). `delete_element` is **not** undoable.

## Editing the bridge itself

If you change code (not just drive the map), the pieces reload differently:

- **Mod** (`mod/.../mcp_bridge.gd`, GDScript): reload the mod inside Dungeondraft
  (keeps the current map). Changing an existing handler needs only this.
- **MCP server** (`mcpserver/cmd/mcpserver/`): rebuild (`.\scripts\install.ps1`),
  then an **MCP reconnect** (`/mcp` → reconnect). Adding a *new tool* needs
  **both** reloads (mod handler + `mcpserver` registration in `register.go`).
- **Bridge app** (`bridge_app/`): rebuild (`.\scripts\install.ps1`) and restart
  it — or just close/reopen Dungeondraft via the installed `dd-launch.exe`,
  since `bridge_app` now watches DD's process and exits on its own a while
  after DD closes.
- GDScript on Godot 3.4.2 has **no try/catch**: an unhandled runtime error in a
  handler crashes *that call*, but a **parse error anywhere in `mcp_bridge.gd`
  is worse** — Dungeondraft's mod loader has no per-mod isolation, so one bad
  line (e.g. a mis-indented statement) throws during `ModManager.Load()` and
  silently aborts loading **every** mod, not just this one. Symptoms: the mod
  never appears in DD's tool panel, no `agent_hello` ever reaches `bridge_app`
  (`ping` stays `dd_connected: false` forever), and DD's own log/UI shows
  nothing wrong. The only way to see the actual `SCRIPT ERROR:` is to relaunch
  Dungeondraft from a terminal so stdout/stderr aren't swallowed
  (`& "C:\Program Files\Dungeondraft\Dungeondraft.exe"`). Validate inputs
  before calling DD API methods, and if the mod ever stops connecting with no
  obvious cause, check for exactly this before anything else.
