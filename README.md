# Terraform provider: dungeondraft

Declaratively build maps in a **running Dungeondraft** instance, using Terraform
to plan/diff/apply the elements (walls, doors, objects, lights, roofs, text,
patterns, paths, terrain) of the open map.

The provider is a single static Go binary built on
[`terraform-plugin-framework`](https://developer.hashicorp.com/terraform/plugin/framework).
It does **not** go through the MCP server — it talks directly to `bridge_app`'s
TCP/JSON wire protocol (see `PROTOCOL.md` in the repo root) using the shared
`bridgeclient` module, exactly like `mcpserver` and the `ddcheck`/`crudge`
CLI tools do.

```
terraform apply
      │
      ▼
terraform-provider-dungeondraft (this binary)
      │  {"cmd":...}\n in, {"ok":...}\n out  (bridge_app on 127.0.0.1:8787)
      ▼
bridge_app  ⇄  mcp_bridge.gd (the Dungeondraft mod)
      ▼
   the open map
```

## Prerequisites

- Dungeondraft running with the **MCP Bridge Agent** mod enabled and **a map
  open**. The provider only talks to a live map — it cannot create one.
- The standalone **bridge app** owning the wire port (default `127.0.0.1:8787`).
  Start/stage it with `.\scripts\install.ps1` in the repo root (installs
  `bridge_app.exe`, `mcpserver.exe`, `dd-launch.exe` to
  `%LOCALAPPDATA%\DungeondraftBridge\bin`).
- The Terraform CLI (this repo's development workflow can pass a local
  `terraform.exe`; see below).

## Provider config

```hcl
provider "dungeondraft" {
  host = "127.0.0.1"   # optional, default 127.0.0.1
  port = 8787          # optional, default 8787 (matches DD_BRIDGE_PORT)
}
```

## Testing against the local build (before publishing)

The provider address is `registry.terraform.io/jamesloftis/dungeondraft`
(`provider/main.go`). Until it is published to the registry you must point
Terraform at the freshly built binary rather than letting it try to download
the package.

### Option A — CLI config dev_overrides (easiest)

1. Build the provider binary:

   ```powershell
   cd provider
   go build -o terraform-provider-dungeondraft.exe .
   ```

2. Create a Terraform **CLI config** that maps the provider address to that
   binary and disables the plugin cache (dev builds must not be cached):

   `%APPDATA%\terraform.rc` (or pass `-chdir`/`TF_CLI_CONFIG_FILE`):

   ```hcl
   provider_installation {
     dev_overrides {
       "registry.terraform.io/jamesloftis/dungeondraft" = "C:\\path\\to\\dir\\containing\\terraform-provider-dungeondraft.exe"
     }
     direct {}
   }
   ```

   With dev_overrides, `terraform init` is **not** required for the provider and
   `terraform plan` will note that the provider is "overridden by
   dev_overrides".

3. Run against an example (`demo/` floods the map with a small scene; run it on
   a **scratch map** first, since `dungeondraft_terrain_fill` repaints whole
   areas):

   ```powershell
   terraform -chdir=examples\demo init -input=false   # only for backends/locks; provider is override-dn
   terraform -chdir=examples\demo plan
   terraform -chdir=examples\demo apply -auto-approve
   ```

### Option B — filesystem mirror

Build the binary and drop it into a local mirror layout, then reference the
mirror with `-plugin-dir`:

```
<mirror>\registry.terraform.io\jamesloftis\dungeondraft\<version>\terraform-provider-dungeondraft.exe
```

(Keep `version` in sync with `ddprovider.version`.)

### Schema validation without Terraform

`go run ./cmd/tfcheck ../examples` parses every example `.tf` with the same HCL
parser Terraform uses and checks every block type + attribute against the
provider's registered schemas (it does not evaluate expressions — that is what
`terraform plan` adds).

## Development utilities

- `go run ./cmd/ddcheck` — reach out to the live bridge, print map status, and
  report `dd_connected` (exit non-zero if the Dungeondraft mod is not linked).
  Args: `[host] [port]`.
- `go run ./cmd/crudge` — run a real create / read / delete sequence against the
  live map and remove everything it made (proves the full round trip without
  residue).

## Known risk: saving right after a bulk apply

`dungeondraft-mcp-fork/ROADMAP.md` documents an observed bug: **Dungeondraft
saves can fail/corrupt when attempted shortly after live bridge activity**
(three consecutive saves failed immediately after tool-call activity in one
session; a save after ~15-20s of bridge idle time succeeded). The bridge
rewrite to `bridge_app` (dial-per-request client, outbound-agent mod
connection) was intended to remove the lingering-connection cause of this,
but it has not been re-verified against save-reliability specifically as its
own acceptance criterion — treat it as **mitigated, not confirmed fixed**.

This provider's `Client.Request` opens and closes a fresh TCP connection per
bridge call (see `internal/bridge/client.go`), so it holds nothing open
between calls — but a `terraform apply` that creates many resources still
means a burst of back-to-back bridge activity immediately before whatever
save happens next, which is exactly the trigger condition described above.
This is **not specific to any one resource kind** (`dungeondraft_light` and
`dungeondraft_path` were flagged as suspect in one downstream project's
bisection, but their Create calls send exactly what `PROTOCOL.md` specifies
— nothing in this provider's code is unique to those two).

Until save-reliability is independently re-verified: after a large `apply`,
save manually and confirm the save in Dungeondraft before doing more bridge
work, rather than chaining a big apply straight into an autosave/close.

## Resources

| Resource | Bridge cmd | Shape fields replace | In-place update |
| --- | --- | --- | --- |
| `dungeondraft_wall` | `draw_wall` | points/asset/loop/type/joint | move only |
| `dungeondraft_portal` | `add_portal` | asset (+wall_id) | — |
| `dungeondraft_object` | `place_object` | asset/rotation/scale | x, y (move_element) |
| `dungeondraft_light` | `add_light` | — | — |
| `dungeondraft_roof` | `add_roof` | points (ridge)/width/type | — |
| `dungeondraft_text` | `add_text` | text/size/color/font | — |
| `dungeondraft_pattern` | `place_pattern` | points/rect/asset | — |
| `dungeondraft_path` | `draw_path` | points/asset/smoothness | — |
| `dungeondraft_terrain_fill` | `fill_terrain`/`fill_region` | re-paints on change | — |

Every node-kind resource stores the bridge's numeric element id as its
Terraform id, supports `terraform import <id>`, and `Delete` calls
`delete_element` — which is **genuinely permanent** (not undo-able on the
bridge side). `dungeondraft_terrain_fill` cannot un-paint; its Delete warns.

## Data sources

- `dungeondraft_status` — open map info (size, center, per-kind counts).
- `dungeondraft_assets` — search an asset category (`category`, `search`,
  `limit`) so a `.tf` can reference a discovered asset path instead of
  hardcoding one.
- `dungeondraft_map` — full `dump_state` of every element (id, kind, wall_id,
  position, asset) plus the raw JSON, for drift inspection and import tooling.

## Read / drift-detection model

`Read` calls `get_element` and syncs back every field that kind's descriptor
exposes, so hand-edits made directly in the Dungeondraft UI surface as drift on
the next `terraform plan`:

- **wall** — points, asset, loop, type, joint
- **portal** — wall_id, closed, radius, asset, position
- **object** — position (x/y), rotation, scale
- **light** — position (x/y), asset
- **roof** — points, width, type
- **text** — position (x/y), text, size, color, font
- **path** — points, smoothness
- **pattern** — points (only when the resource was declared with `points`)

Fields the bridge cannot read back stay write-only (trusted from config):

- `object.z` / `object.sorting` / `object.color`, `light.energy` /
  `light.range` / `light.shadows` / `light.color`, and `pattern.rect` (a
  rectangle cannot be reconstructed from an arbitrary polygon, so
  `rect`-declared patterns are existence-checked only).

For fields with an in-place update path only `move_element` (object x/y) is
wired; everything else `RequiresReplace` because the bridge has no reshape API
for them yet.

## Related docs

- `PROTOCOL.md` (repo root) — the full wire protocol and per-command semantics.
- `examples/demo/` and `examples/throne_room/` — worked `.tf` plans.
- `../ROADMAP.md` — design notes and the write-mostly → real-Read trajectory.
