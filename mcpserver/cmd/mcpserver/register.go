package main

import "github.com/modelcontextprotocol/go-sdk/mcp"

// registerTools wires the bridge commands that observe the map (see
// PROTOCOL.md) to MCP tools. The server is look-only (spec goal 2): maps are
// built with Terraform, so nothing here creates, changes, deletes, paints,
// selects or undoes. Capture and camera tools change the view, not the map.
func (a *app) registerTools(s *mcp.Server) {
	// Read / query
	addTool(s, "ping", "Check connectivity to the running Dungeondraft bridge and report the Godot engine version.", a.Ping)
	addTool(s, "get_status", "Report the current map: whether one is open, its file, size and center in woxels, per-type element counts, level info, and whether it has unsaved changes. Call this first.", a.GetStatus)
	addTool(s, "list_asset_categories", "List the valid asset category names (Objects, Walls, Paths, Terrain, Lights, Portals, Roofs, ...).", a.ListAssetCategories)
	addTool(s, "list_assets", "List asset paths in a category, optionally filtered by a case-insensitive substring. A returned path is what a Terraform resource's 'asset' takes. Results are capped at 300 — narrow with 'search' rather than raising 'limit'.", a.ListAssets)
	addTool(s, "list_elements", "List elements currently on the map with their ids, positions, rotation, scale and asset. Pass a returned id to get_element or focus_element. Results are capped at 300 — filter by 'kind' rather than raising 'limit'.", a.ListElements)
	addTool(s, "get_element", "Get details (kind, position, rotation, scale, asset, level) for a single element by id.", a.GetElement)
	addTool(s, "dump_state", "Dump the whole current level: every element with full geometry, in one round trip. Use for a point-in-time snapshot or drift inspection. Refuses on maps with more than 500 elements — use list_elements/get_element instead on those.", a.DumpState)
	addTool(s, "list_levels", "List the map's levels (floors) with their index, id and label, plus the current index.", a.ListLevels)

	// Capture
	addTool(s, "screenshot", "Capture the current DD viewport as an image. Synchronous window grab; can go stale if DD is minimized — use export_map instead in that case.", a.Screenshot)
	addTool(s, "export_map", "Render the full map to an image on a background thread (works headless/minimized).", a.ExportMap)

	// View: camera and the level shown
	addTool(s, "get_camera", "Get the editor camera's position, zoom, and viewport size.", a.GetCamera)
	addTool(s, "set_camera", "Pan/zoom the editor camera. Larger zoom = zoomed out.", a.SetCamera)
	addTool(s, "focus_element", "Center the camera on an element.", a.FocusElement)
	addTool(s, "fit_elements", "Frame the camera to fit the union of several elements' bounds.", a.FitElements)
	addTool(s, "set_level", "Switch which level (floor) the editor shows, by index.", a.SetLevel)
}
