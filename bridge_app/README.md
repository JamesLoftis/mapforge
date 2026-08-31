# bridge_app — the visible Dungeondraft bridge

This is the standalone bridge application. It owns `127.0.0.1:8787` and speaks
the same newline-delimited JSON protocol the old in-Dungeondraft TCP server
did (see PROTOCOL.md), so the MCP server and Terraform provider keep working
unchanged — but now the wire endpoint is an obvious, always-up process with a
health page and a visible connection list, instead of a listener hidden
inside Dungeondraft that died silently whenever the mod crashed.

The Dungeondraft mod (`mcp_bridge.gd`) is now a thin **agent** that connects
_out_ to this app. The app owns the port; if the mod crashes or DD restarts,
the app survives, reports `dd_connected: false`, and answers `get_status`
with a clear "dungeondraft offline" error instead of refusing the connection.

Any number of clients (MCP server, Terraform provider, a curl script, …) can
connect as **users** at once — every command they send is queued and
serialized onto the single agent link one at a time (see `Bridge.jobs` /
`agentPump` in `internal/core/bridge.go`; `queue_depth` on `/health` shows how
many are currently waiting).

The app also watches for `Dungeondraft.exe` itself (`-watch-dd`, on by
default on Windows): once DD has been seen running and then stays gone for
`-watch-grace` (default 60s — long enough to survive a quick crash-and-relaunch
without exiting on you), the app shuts itself down. It doesn't try to start
DD — that's what `cmd/dd-launch` is for.

## Running

    go run ./cmd/bridge_app                  # TCP on 127.0.0.1:8787, admin HTTP on 127.0.0.1:8788
    go run ./cmd/bridge_app -v               # log every request/response frame
    go run ./cmd/bridge_app -addr 0.0.0.0:8787 -admin 127.0.0.1:8788 -log bridge.log
    go run ./cmd/bridge_app -watch-dd=false  # don't tie lifecycle to Dungeondraft (e.g. for local testing)

For normal use, build+install both this and `dd-launch` via `../scripts/install.ps1`
and launch through the installed `dd-launch.exe` instead of running this
directly — see the repo root README.

## Visibility

    curl http://127.0.0.1:8788/             # console-style status page
    curl http://127.0.0.1:8788/health       # {"up":true,"version":"...","dd_connected":false,"queue_depth":0,...} 200
    curl http://127.0.0.1:8788/connections  # JSON list of live connections/roles

`GET /health` returns 200 with `dd_connected` state even when Dungeondraft is
down — that is the whole point: you can tell "app running, DD offline" from
"nothing running" at a glance. `version` reflects whatever was stamped in at
build time (`-ldflags -X main.version=...`, see `scripts/install.ps1`), so a
stale build shows up immediately instead of being a guessing game.

## Wire protocol (unchanged for users)

    -> {"cmd":"ping"}
    <- {"ok":true,"result":{"pong":true,"protocol":17,"dd_connected":false}}
    -> {"cmd":"get_status"}
    <- {"ok":false,"error":"dungeondraft offline: bridge agent not connected"}

`ping` is answered by the app itself (so liveness doesn't depend on DD).
Every other command is forwarded to the agent when connected.

## Tests

    go test ./...        # offline: fake agent + fake users over real sockets