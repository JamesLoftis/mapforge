package main

import "github.com/modelcontextprotocol/go-sdk/mcp"

// registerTools wires every bridge command (see PROTOCOL.md) to an MCP tool.
// Grouped the same way PROTOCOL.md and the old Python server were.
func (a *app) registerTools(s *mcp.Server) {
	// Read / query
	addTool(s, "ping", "Check connectivity to the running Dungeondraft bridge and report the Godot engine version.", a.Ping)
	addTool(s, "get_status", "Report the current map: whether one is open, its size and center in woxels, per-type element counts, and level info. Call this first — map_center is the default placement point.", a.GetStatus)
	addTool(s, "list_asset_categories", "List the valid asset category names (Objects, Walls, Paths, Terrain, Lights, Portals, Roofs, ...).", a.ListAssetCategories)
	addTool(s, "list_assets", "List asset paths in a category, optionally filtered by a case-insensitive substring. Pass a returned path as the 'asset' argument to the create tools. Results are capped at 300 — narrow with 'search' rather than raising 'limit'.", a.ListAssets)
	addTool(s, "list_elements", "List elements currently on the map with their ids, positions, rotation, scale and asset. Use the returned ids with move_element / modify_object / delete_element / select_elements. Results are capped at 300 — filter by 'kind' rather than raising 'limit'.", a.ListElements)
	addTool(s, "get_element", "Get details (kind, position, rotation, scale, asset) for a single element by id.", a.GetElement)
	addTool(s, "dump_state", "Dump the whole current level: every element with full geometry, in one round trip. Use for a point-in-time snapshot, backup/restore, or drift inspection. Refuses on maps with more than 500 elements — use list_elements/get_element instead on those.", a.DumpState)
	addTool(s, "list_levels", "List the map's levels (floors) with their index, id and label, plus the current index.", a.ListLevels)

	// Create
	addTool(s, "place_object", "Place a prop from an asset; returns the new element id.", a.PlaceObject)
	addTool(s, "draw_wall", "Draw a wall polyline.", a.DrawWall)
	addTool(s, "draw_path", "Draw a path/road/river.", a.DrawPath)
	addTool(s, "add_light", "Place a light.", a.AddLight)
	addTool(s, "add_portal", "Place a door/window, wall-mounted or freestanding.", a.AddPortal)
	addTool(s, "add_roof", "Add a roof from its ridge line (not a footprint — 2 points already make a full roof).", a.AddRoof)
	addTool(s, "place_pattern", "Place a tiled floor pattern shape, either a rect or a polygon.", a.PlacePattern)
	addTool(s, "build_room", "Composite: draw a looped wall and a matching floor along the same boundary.", a.BuildRoom)
	addTool(s, "add_text", "Add a text label.", a.AddText)

	// Terrain
	addTool(s, "set_terrain_slot", "Assign a texture to a terrain slot (0-7); affects everywhere that slot is weighted.", a.SetTerrainSlot)
	addTool(s, "fill_terrain", "Fill the whole level with one terrain slot.", a.FillTerrain)
	addTool(s, "fill_region", "Fill a rect or polygon region with a terrain slot (undoable, unlike a raw Terrain.Paint).", a.FillRegion)
	addTool(s, "paint_terrain", "Paint a soft circular terrain brush dab at a point.", a.PaintTerrain)
	addTool(s, "paint_path", "Paint a continuous terrain stroke along a polyline (for roads/trails) without dotty overlaps.", a.PaintPath)
	addTool(s, "dig_cave", "Dig (or fill back) a cave path with the Cave Brush.", a.DigCave)
	addTool(s, "clear_caves", "Wipe the whole cave layer back to rock.", a.ClearCaves)

	// Modify / delete / levels / selection
	addTool(s, "move_element", "Move an element to an absolute position.", a.MoveElement)
	addTool(s, "modify_object", "Change an object's scale, rotation, color, and/or shadow; omitted fields are left unchanged.", a.ModifyObject)
	addTool(s, "duplicate_object", "Duplicate an object with a position offset.", a.DuplicateObject)
	addTool(s, "delete_element", "Delete an element by id. Not undoable.", a.DeleteElement)
	addTool(s, "add_level", "Add a new level (floor).", a.AddLevel)
	addTool(s, "set_level", "Switch the active level by index.", a.SetLevel)
	addTool(s, "select_elements", "Select elements by id in the DD UI.", a.SelectElements)
	addTool(s, "clear_selection", "Clear the current selection.", a.ClearSelection)

	// Capture
	addTool(s, "screenshot", "Capture the current DD viewport as an image. Synchronous window grab; can go stale if DD is minimized — use export_map instead in that case.", a.Screenshot)
	addTool(s, "export_map", "Render the full map to an image on a background thread (works headless/minimized).", a.ExportMap)

	// Camera
	addTool(s, "get_camera", "Get the editor camera's position, zoom, and viewport size.", a.GetCamera)
	addTool(s, "set_camera", "Pan/zoom the editor camera. Larger zoom = zoomed out.", a.SetCamera)
	addTool(s, "focus_element", "Center the camera on an element.", a.FocusElement)
	addTool(s, "fit_elements", "Frame the camera to fit the union of several elements' bounds.", a.FitElements)

	// History (bridge-managed, independent of DD's own Ctrl+Z)
	addTool(s, "undo", "Undo the last reversible bridge edit.", a.Undo)
	addTool(s, "redo", "Redo the last undone bridge edit.", a.Redo)
}
