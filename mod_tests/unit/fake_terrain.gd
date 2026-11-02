# A duck-typed stand-in for Dungeondraft's real `level.Terrain` object,
# implementing only the methods open_splat/close_splat actually call.
# Lets test_open_splat.gd exercise the ExpandSlots-based fix without a
# running Dungeondraft instance.
#
# Modeled directly on the real engine behavior
# (ExpandSlots/RestoreSplat2 in particular) --
# not just "whatever makes the test pass": ExpandSlots(value) only creates
# splatImage2 `if (splatImage2 == null)` (idempotent, matches has_secondary
# staying true across repeat calls), and RestoreSplat2 takes BOTH images
# and CopyFrom's into the existing ones rather than replacing them.
#
# Configure has_primary/has_secondary in the constructor; everything else
# is recorded so a test can assert exactly what happened.
extends Reference

var has_primary: bool
var has_secondary: bool
var primary_width := 512
var primary_height := 512
var primary_format := Image.FORMAT_RGBA8

var expand_slots_calls := []       # each entry: the bool passed in
var restore_splat_calls := []      # each entry: the Image passed in
var restore_splat2_calls := []     # each entry: [splat, splat2]
var update_splat_calls := 0

func _init(p_has_primary := true, p_has_secondary := false) -> void:
	has_primary = p_has_primary
	has_secondary = p_has_secondary

func CloneSplatImage():
	if not has_primary:
		return null
	var img = Image.new()
	img.create(primary_width, primary_height, false, primary_format)
	return img

func CloneSplatImage2():
	if not has_secondary:
		return null
	var img = Image.new()
	img.create(primary_width, primary_height, false, primary_format)
	return img

func ExpandSlots(value: bool) -> void:
	expand_slots_calls.append(value)
	if value and not has_secondary:
		has_secondary = true

func RestoreSplat(img) -> void:
	restore_splat_calls.append(img)

func RestoreSplat2(splat, splat2) -> void:
	restore_splat2_calls.append([splat, splat2])

func UpdateSplat() -> void:
	update_splat_calls += 1
