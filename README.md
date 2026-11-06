# MapForge

Build [Dungeondraft](https://dungeondraft.net/) maps with Terraform.

You describe a map's walls, rooms, doors, objects, lights, patterns, paths,
terrain and levels in `.tf` files; `terraform apply` builds them in a running
Dungeondraft, `terraform plan` shows what changed since, and the map file is
saved for you. A look-only MCP server lets an AI assistant such as Claude see
the map (status, elements, screenshots) without being able to change it.

## Why it exists

MapForge exists so maps can be built **without a human watching**: the next
step is a campaign tool that writes Terraform and applies it to generate maps.
That needs a build path you can trust, so:

- **Terraform is the one way maps get built.** Every change goes through a
  plan and lands in state, so it can be reviewed, repeated and diffed against
  what is really on the map.
- **Required means required.** A missing value, a value of the wrong kind, or
  one out of range is refused, at plan time by the provider and again by the
  mod with the error `bad_param`. Nothing is quietly replaced by a default, so
  a wrong config fails loudly instead of building the wrong map.
- **Commands go to the right map.** The provider names its map file, and
  Dungeondraft refuses commands (`wrong_map`) while any other map is open.
- **Every command is logged** by `bridge_app` before it is forwarded, with its
  outcome and duration, so any build can be reconstructed afterwards.
- **The MCP server only looks.** An assistant that could edit the map directly
  would bypass the plan, the state and the checks; it can look, then change
  the Terraform.

## How it fits together

```
terraform apply                    Claude (or any MCP client)
      │                                  │ MCP, stdio
      ▼                                  ▼
provider/  (terraform-provider-     mcpserver/  (look-only)
            dungeondraft)                │
      │  JSON lines over TCP 127.0.0.1:8787, via bridgeclient/
      └──────────────┬───────────────────┘
                     ▼
             bridge_app/   ── always-up relay; logs every command;
                     ▲        admin page on http://127.0.0.1:8788
                     │  the mod dials out to the app
             mod/  (TF Bridge Agent, GDScript inside Dungeondraft)
                     │  Dungeondraft modding API
                     ▼
                the open map
```

The mod does not listen on a port: it connects out to `bridge_app` and runs the
commands the app forwards to it, one at a time. If Dungeondraft is closed or
the mod crashes, the app stays up and answers with a clear `offline` error.
`bridge_app` exits on its own shortly after Dungeondraft closes. The wire
protocol is in [PROTOCOL.md](PROTOCOL.md).

## Requirements

- Windows (the install script, `dd-launch` and the Dungeondraft watcher are
  Windows-only).
- Dungeondraft **1.2.0.1** (Godot 3.4.2).
- Git (the install script stamps versions with `git describe`).
- Go **1.25.6** or later.
- Terraform (tested with **1.16.0**).
- For the MCP side, an MCP client such as Claude Code.

## Install

```powershell
git clone https://github.com/JamesLoftis/mapforge.git
cd mapforge
powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1
```

(`-ExecutionPolicy Bypass` is only needed where Windows blocks local scripts,
which is its default.)

The script is the deploy step; re-run it after every `git pull` or local edit.
It:

- builds `mcpserver.exe`, `bridge_app.exe` and `dd-launch.exe` into
  `%LOCALAPPDATA%\DungeondraftBridge\bin\`, stamped with a version from
  `git describe`;
- builds the Terraform provider into Terraform's local mirror
  (`%APPDATA%\terraform.d\plugins\registry.terraform.io\jamesloftis\dungeondraft\<version>\windows_amd64`),
  so `terraform init` finds it with no extra configuration;
- links Dungeondraft's mods folder to the mod in your checkout
  (`%APPDATA%\Dungeondraft\Mods\dungeondraft-mcp-bridge-agent`, a junction).
  If your mods folder is elsewhere, pass it:
  `.\scripts\install.ps1 -ModsDir D:\Dungeondraft\Mods`. If you copied the mod
  into the mods folder by hand before, the script stops rather than delete
  your copy: move it aside and re-run.

Close Claude Code first if it is running `mcpserver.exe`: Windows will not
overwrite a running program.

Then:

1. Start Dungeondraft through
   `%LOCALAPPDATA%\DungeondraftBridge\bin\dd-launch.exe` (point your shortcut at
   it). It starts Dungeondraft and `bridge_app` together and writes their logs.
   It looks for `C:\Program Files\Dungeondraft\Dungeondraft.exe`; if yours is
   elsewhere, pass `-dd-path <path>` or set `DD_EXE_PATH`.
2. In Dungeondraft, enable the **TF Bridge Agent** mod.
3. Check the chain: `curl.exe http://127.0.0.1:8788/health` shows
   `"dd_connected": true`.

## Your first map

1. In Dungeondraft, create a map and save it, for example to
   `C:/maps/first.dungeondraft_map`. Leave it open.
2. Copy `provider/examples/room` somewhere and run:

   ```powershell
   terraform init
   terraform apply -var map_file=C:/maps/first.dungeondraft_map
   ```

   A room with a floor and a door appears in the middle of the map.
3. Run `terraform plan` again: it reports **No changes**. Move something in
   Dungeondraft and plan again: Terraform shows the drift.

Every config starts with the provider block:

```hcl
terraform {
  required_providers {
    dungeondraft = { source = "registry.terraform.io/jamesloftis/dungeondraft" }
  }
}

provider "dungeondraft" {
  map_file = "C:/maps/first.dungeondraft_map" # required: the map this state belongs to
  level    = "Ground"                         # default level for every element
  # host and port default to 127.0.0.1 and 8787
}
```

The resources (`dungeondraft_wall`, `_room`, `_portal`, `_object`, `_light`,
`_roof`, `_text`, `_pattern`, `_path`, `_terrain_fill`, `_cave`, `_level`), the
data sources, how levels and drift work, and which changes replace an element
are in the [provider reference](provider/README.md). More worked examples are in
[`provider/examples`](provider/examples) (`demo`, `throne_room`, `level`,
`terrain_brush`).

**Saving.** You never need to save after an apply: once the provider's edits
have been quiet for 3 seconds, Dungeondraft saves the map to `map_file` itself.

**Finding asset paths.** With Dungeondraft open,
`cd provider; go run ./cmd/catalog` writes every asset path in every category
to `asset_catalog.json`. In Terraform, the `dungeondraft_assets` data source
searches one category.

## Looking at the map through MCP

Register the installed server with Claude Code:

```powershell
claude mcp add dungeondraft -s user -- "$env:LOCALAPPDATA\DungeondraftBridge\bin\mcpserver.exe"
```

Reconnect (`/mcp`). Other clients take the same absolute path as `command`.
The server has 15 tools, none of which change the map:

- **Inspect:** `ping`, `get_status`, `list_levels`, `list_elements`,
  `get_element`, `dump_state`, `list_asset_categories`, `list_assets`.
- **See:** `screenshot` (the window) and `export_map` (a clean render of the
  whole map), returned as images.
- **View:** `set_level`, `get_camera`, `set_camera`, `focus_element`,
  `fit_elements`, to point the view before a screenshot.

`mcpserver` reads `DD_BRIDGE_HOST` (default `127.0.0.1`) and `DD_BRIDGE_PORT`
(default `8787`) if `bridge_app` runs elsewhere.

## Troubleshooting

Errors carry a code (full list in [PROTOCOL.md](PROTOCOL.md#error-codes)):

| Code | What it means | What to do |
| --- | --- | --- |
| `bad_param` | A value is missing, the wrong kind or out of range; nothing ran. The message names the parameter. | Fix that value in the config. |
| `wrong_map` | The open map is not the provider's `map_file`. | Open that map, saved at that exact path, in Dungeondraft. |
| `offline` | Dungeondraft is not connected to `bridge_app`. | Start Dungeondraft through `dd-launch` and enable the mod. |
| `timeout` | Dungeondraft did not answer within 30 s. | Check Dungeondraft is not stuck in a dialog; read its log. |
| `agent_lost` | Dungeondraft restarted or crashed during a command. | Re-run; read its log. |
| `protocol_mismatch` | The loaded mod is older or newer than `bridge_app`. | Re-run the install script and restart Dungeondraft. |
| `unsaved_edits` | The last autosave failed, so leaving the map would lose edits. | Save the map, then retry. |
| `level_not_found` | No level has that label. | Check the `level` names. |

**Logs** are in `%APPDATA%\DungeondraftBridge\logs`, one set per launch:
`dd_<stamp>.log` (Dungeondraft's own output, including GDScript errors),
`bridge_<stamp>.log`, and `commands_<stamp>.jsonl` (every command and its
outcome). `bridge_app`'s status page is at `http://127.0.0.1:8788/`, with
`/health` and `/connections` as JSON.

**`mcpserver.exe` is in use during install:** close every Claude Code session
(or run `Stop-Process -Name mcpserver`), then re-run the script.

**The mod does not load:** Dungeondraft's mod loader stops at the first
GDScript error in any mod; the error is in `dd_<stamp>.log`.

## Development

```
bridgeclient/   the shared Go client for the wire protocol (provider and mcpserver use it)
bridge_app/     the relay (cmd/bridge_app) and the launcher (cmd/dd-launch)
mcpserver/      the look-only MCP server
provider/       the Terraform provider, its examples, and cmd/catalog
mod/            the Dungeondraft mod: one GDScript file, scripts/tools/mcp_bridge.gd
mod_tests/      tests for the mod's GDScript logic (run with a Godot 3.4.2 editor)
integration/    tests against a live Dungeondraft, through bridge_app and Terraform
scripts/        install.ps1 and its Pester tests (scripts/tests)
docs/           design specs and plans (docs/superpowers), and a checklist
```

**Tests.**

- Unit tests: `go test ./...` in `bridgeclient`, `bridge_app`, `mcpserver` and
  `provider`. Script tests: `Invoke-Pester scripts/tests` in PowerShell.
- Live tests: `cd integration; go test -tags integration ./...`. They need
  Dungeondraft running with the mod, `bridge_app` up, and a **scratch map**
  open: they create and delete elements on it.

**Rules worth knowing.**

- The mod must stay a **single `.gd` file**: a second script file anywhere in
  the mod's folder crashes Dungeondraft's mod loader.
- The mod and `bridge_app` share a protocol version (`PROTOCOL_VERSION` in
  `mcp_bridge.gd` and `bridge_app/internal/core/bridge.go`); change both
  together, and record it in [CHANGELOG.md](CHANGELOG.md).
- A change to the mod takes effect when Dungeondraft restarts; a change to the
  Go programs after re-running the install script (and `/mcp` for
  `mcpserver`).
- Plans and open work: [ROADMAP.md](ROADMAP.md).

## Credits

Parts of MapForge, and most of its inspiration, come from Brandon Florian's
[dungeondraft-mcp](https://github.com/brann-dev/dungeondraft-mcp): the idea of
driving Dungeondraft from outside through a mod and a local bridge, and the
original mod, bridge and MCP server this project grew from.

Built against the
[Dungeondraft Modding API](https://megasploot.github.io/DungeondraftModdingAPI/).

## License

[MIT](LICENSE), copyright Brandon Florian and JamesLoftis.
