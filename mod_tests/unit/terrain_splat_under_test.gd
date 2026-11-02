# A hand-synced DUPLICATE of mcp_bridge.gd's _open_splat / _close_splat
# (mod/dungeondraft-mcp-bridge-agent/scripts/tools/mcp_bridge.gd), not
# something the real mod loads. It has to live here, outside the mod's own
# directory tree, and NOT be loaded by mcp_bridge.gd at runtime: confirmed
# live that Dungeondraft's C# ModManager scans and tries to load every .gd
# file anywhere under a mod's folder, not just scripts/tools/, and crashes
# (an unhandled NullReferenceException in ModManager.LoadScript, right
# after a GDScript parse error) on one that doesn't fit whatever it
# expects a mod script to look like.
#
# So: this file is real, tested code (see test_open_splat.gd), but the mod
# itself carries its own copy of the same logic inline. If you change the
# fix in mcp_bridge.gd, change it here too -- there is no automated check
# that these stay identical, only this comment.
#
# Root-caused by tracing Dungeondraft's terrain behaviour: splatImage2
# is only ever
# created by ExpandSlots(true), and RestoreSplat2's real signature is
# (Image splat, Image splat2) -- both halves together, CopyFrom'd into
# already-existing images, not a replacement. See mcp_bridge.gd's own
# comment on _open_splat for the two wrong approaches this replaced.
extends Reference


static func open_splat(terrain, slot : int):
	var which = 0 if slot < 4 else 1
	if which == 1:
		terrain.ExpandSlots(true)
	var img = terrain.CloneSplatImage() if which == 0 else terrain.CloneSplatImage2()
	if img == null:
		return null
	return { "img": img, "ch": slot % 4, "which": which }


static func close_splat(terrain, which : int, img) -> void:
	if which == 0:
		terrain.RestoreSplat(img)
	else:
		terrain.RestoreSplat2(terrain.CloneSplatImage(), img)
	terrain.UpdateSplat()
