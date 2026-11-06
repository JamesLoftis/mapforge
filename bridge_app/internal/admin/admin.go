// Package admin serves the bridge's visibility HTTP endpoints.
package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"mapforge/bridge_app/internal/core"
)

// Server wraps a *core.Bridge behind an HTTP handler.
type Server struct {
	core *core.Bridge
	mux  *http.ServeMux
}

// New builds the admin handler over a bridge.
func New(b *core.Bridge) *Server {
	s := &Server{core: b}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/connections", s.handleConnections)
	s.mux = mux
	return s
}

// Serve runs the admin listener until the server is shut down.
func (s *Server) Serve(addr string) error {
	return http.ListenAndServe(addr, s.mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	agent := s.core.AgentInfo()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"up":           true,
		"version":      Version,
		"protocol":     core.PROTOCOL_VERSION,
		"dd_connected": agent.Connected,
		"agent_remote": agent.Remote,
		"agent_seen":   agent.LastSeen,
		"uptime_s":     int(s.core.UptimeSeconds()),
		"connections":  len(s.core.Connections()),
		"queue_depth":  s.core.QueueDepth(),
	})
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.core.Connections())
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, statusPage(s.core, r.Host))
}

func statusPage(c *core.Bridge, host string) string {
	agent := c.AgentInfo()
	stats := c.Stats()

	var sb strings.Builder
	sb.WriteString("<!doctype html><html><head><meta charset='utf-8'>")
	sb.WriteString("<title>Dungeondraft Bridge</title><style>")
	sb.WriteString("body{font:14px/1.5 monospace;margin:2rem;color:#222} ")
	sb.WriteString(".dot{display:inline-block;width:10px;height:10px;border-radius:50%;margin-right:6px}")
	sb.WriteString(".ok{background:#2e7d32}.bad{background:#c62828} ")
	sb.WriteString("table{border-collapse:collapse;margin-top:.5rem}td,th{border:1px solid #bbb;padding:4px 10px;text-align:left}")
	sb.WriteString("</style></head><body>")

	sb.WriteString("<h1>Dungeondraft Bridge</h1>")
	if agent.Connected {
		sb.WriteString("<p><span class='dot ok'></span><b>Dungeondraft connected</b> (agent #")
		fmt.Fprintf(&sb, "%d", agent.ID)
		sb.WriteString(", protocol v")
		fmt.Fprintf(&sb, "%d", agent.Protocol)
		sb.WriteString(", heartbeat ")
		fmt.Fprintf(&sb, "%d", int(agent.HeartbeatAge.Seconds()))
		sb.WriteString("s ago)</p>")
	} else {
		sb.WriteString("<p><span class='dot bad'></span><b>Dungeondraft offline</b>")
		if agent.ID != 0 {
			sb.WriteString(" &mdash; agent last seen ")
			sb.WriteString(agent.LastSeen)
			sb.WriteString(", heartbeat ")
			fmt.Fprintf(&sb, "%d", int(agent.HeartbeatAge.Seconds()))
			sb.WriteString("s ago")
		}
		sb.WriteString("</p>")
	}

	sb.WriteString("<p>Wire endpoint on <b>127.0.0.1:8787</b> &middot; uptime ")
	fmt.Fprintf(&sb, "%d", int(c.UptimeSeconds()))
	sb.WriteString("s &middot; <code>curl http://")
	sb.WriteString(host)
	sb.WriteString("/health</code></p>")

	sb.WriteString("<h2>Agent</h2><table><tr><th>id</th><th>protocol</th><th>remote</th><th>connected</th><th>last seen</th></tr>")
	if agent.ID != 0 {
		fmt.Fprintf(&sb, "<tr><td>%d</td><td>%d</td><td>%s</td><td>%s</td><td>%s</td></tr>",
			agent.ID, agent.Protocol, agent.Remote, agent.ConnectedAt, agent.LastSeen)
	}
	sb.WriteString("</table>")

	sb.WriteString("<h2>Connections</h2>")
	conns := c.Connections()
	sort.Slice(conns, func(i, j int) bool { return conns[i].ID < conns[j].ID })
	sb.WriteString("<table><tr><th>id</th><th>role</th><th>remote</th><th>connected</th><th>last cmd</th></tr>")
	for _, cn := range conns {
		fmt.Fprintf(&sb, "<tr><td>%d</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>",
			cn.ID, cn.Role, cn.Remote, cn.Connected.Format(time.RFC3339), cn.LastCmd)
	}
	sb.WriteString("</table>")

	sb.WriteString("<h2>Requests</h2><table><tr><th>total</th><th>ok</th><th>errors</th><th>offline rejects</th><th>pings</th><th>queue depth</th><th>last</th></tr>")
	fmt.Fprintf(&sb, "<tr><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%s</td></tr>",
		stats.TotalRequests, stats.CompletedOK, stats.CompletedErr, stats.OfflineRejects, stats.Pings, c.QueueDepth(), stats.LastCmd)
	sb.WriteString("</table>")

	keys := make([]string, 0, len(stats.ByCmd))
	for k := range stats.ByCmd {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		sb.WriteString("<h3>By command</h3><table><tr><th>cmd</th><th>count</th></tr>")
		for _, k := range keys {
			fmt.Fprintf(&sb, "<tr><td>%s</td><td>%d</td></tr>", k, stats.ByCmd[k])
		}
		sb.WriteString("</table>")
	}

	sb.WriteString("<p style='color:#777'>bridge app v" + Version + "</p>")
	sb.WriteString("</body></html>")
	return sb.String()
}

// Version is the bridge_app build version, stamped via -ldflags at build
// time (see scripts/install.ps1); "0.1.0" for a plain `go build`.
var Version = "0.1.0"
