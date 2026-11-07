# Unit tests for terrain_splat_under_test.gd's open_splat/close_splat, a
# hand-synced duplicate of mcp_bridge.gd's real (inline) _open_splat /
# _close_splat -- see that file's header for why this can't live inside
# the mod's own directory tree, or be loaded from it at runtime, at all.
#
# Covers the actual, root-caused fix: slots 4-7 need ExpandSlots(true) called once
# to allocate splatImage2 -- neither of two earlier, wrong attempts
# (hand-rolling a blank Image into RestoreSplat2, or calling Fill() on an
# unexpanded level) actually worked, both discovered by testing against a
# real (blank, throwaway) Dungeondraft map. RestoreSplat2's real signature
# also takes two images (splat, splat2), not one.
#
# Run with (from the repo root): godot --no-window --script mod_tests/unit/test_open_splat.gd
extends SceneTree

const TerrainSplat = preload("res://mod_tests/unit/terrain_splat_under_test.gd")
const FakeTerrain = preload("res://mod_tests/unit/fake_terrain.gd")

var _failures := 0

func _init() -> void:
	_test_slots_0_3_never_touch_expand_slots()
	_test_slot_4_7_expands_when_secondary_already_exists()
	_test_slot_4_7_expands_when_secondary_missing()
	_test_close_splat_slot_0_3_uses_single_image_restore()
	_test_close_splat_slot_4_7_uses_two_image_restore()

	if _failures > 0:
		print("FAILED: %d assertion(s)" % _failures)
		quit(1)
	else:
		print("PASSED: all terrain_splat tests")
		quit(0)


func _check(cond: bool, msg: String) -> void:
	if not cond:
		_failures += 1
		printerr("FAIL: " + msg)


func _test_slots_0_3_never_touch_expand_slots() -> void:
	var terrain = FakeTerrain.new(true, false)
	var sp = TerrainSplat.open_splat(terrain, 2)
	_check(sp != null, "slot 2 (primary-backed) should open")
	_check(sp.which == 0, "slot 2 should use the primary image (which=0)")
	_check(sp.ch == 2, "slot 2 -> channel 2")
	_check(terrain.expand_slots_calls.size() == 0, "slot < 4 must never call ExpandSlots")


func _test_slot_4_7_expands_when_secondary_already_exists() -> void:
	var terrain = FakeTerrain.new(true, true)
	var sp = TerrainSplat.open_splat(terrain, 5)
	_check(sp != null, "slot 5 with an existing splatImage2 should open")
	_check(sp.which == 1, "slot 5 should use the secondary image (which=1)")
	_check(sp.ch == 1, "slot 5 -> channel 1 (5 %% 4)")
	_check(terrain.expand_slots_calls == [true], "ExpandSlots(true) should be called unconditionally for slot >= 4 -- it's idempotent on the real engine, so this is deliberate, not a bug")


func _test_slot_4_7_expands_when_secondary_missing() -> void:
	var terrain = FakeTerrain.new(true, false)
	var sp = TerrainSplat.open_splat(terrain, 6)
	_check(sp != null, "slot 6 with no splatImage2 yet should open after ExpandSlots (this is the actual fix)")
	if sp != null:
		_check(sp.which == 1, "slot 6 should use the secondary image (which=1)")
		_check(sp.ch == 2, "slot 6 -> channel 2 (6 %% 4)")
	_check(terrain.expand_slots_calls == [true], "ExpandSlots(true) should have been called")
	_check(terrain.has_secondary, "splatImage2 should now exist")


func _test_close_splat_slot_0_3_uses_single_image_restore() -> void:
	var terrain = FakeTerrain.new(true, false)
	var img = Image.new()
	img.create(4, 4, false, Image.FORMAT_RGBA8)
	TerrainSplat.close_splat(terrain, 0, img)
	_check(terrain.restore_splat_calls == [img], "which=0 should call RestoreSplat(img) once")
	_check(terrain.restore_splat2_calls.size() == 0, "which=0 must never call RestoreSplat2")
	_check(terrain.update_splat_calls >= 1, "UpdateSplat should be called")


func _test_close_splat_slot_4_7_uses_two_image_restore() -> void:
	var terrain = FakeTerrain.new(true, true)
	var edited = Image.new()
	edited.create(4, 4, false, Image.FORMAT_RGBA8)
	TerrainSplat.close_splat(terrain, 1, edited)
	_check(terrain.restore_splat_calls.size() == 0, "which=1 must never call the single-image RestoreSplat")
	_check(terrain.restore_splat2_calls.size() == 1, "which=1 should call the two-image RestoreSplat2 once")
	if terrain.restore_splat2_calls.size() == 1:
		var call = terrain.restore_splat2_calls[0]
		_check(call[0] != null, "RestoreSplat2's first argument (splat) must be a real image, not null -- real signature is (splat, splat2), both required")
		_check(call[1] == edited, "RestoreSplat2's second argument (splat2) should be the edited image passed in")
	_check(terrain.update_splat_calls >= 1, "UpdateSplat should be called")
