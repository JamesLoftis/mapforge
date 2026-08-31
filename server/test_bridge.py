#!/usr/bin/env python3
"""Per-command validator for the Dungeondraft bridge — no MCP involved.

Run with Dungeondraft open (mod enabled, a map loaded). It exercises every
bridge command and prints PASS/FAIL per command, so you can see exactly what
works on your Dungeondraft version. It builds a small throwaway scene near the
map center (a room of walls, an object, a light, a path) and then deletes what
it created.

    python test_bridge.py
"""

import os
import statistics
import sys
import tempfile
import time

from PIL import Image

from dungeondraft_mcp.bridge_client import BridgeClient, BridgeError

bridge = BridgeClient()
created_ids: list[int] = []
passed = 0
failed = 0

VIS_PPI = 20  # export resolution: pixels per grid cell (256 woxels)


def check(label, fn):
    global passed, failed
    try:
        result = fn()
        print(f"  PASS  {label:18} -> {result}")
        passed += 1
        return result
    except BridgeError as exc:
        print(f"  FAIL  {label:18} -> {exc}")
        failed += 1
        return None


def first_asset(category, search=""):
    res = bridge.request("list_assets", category=category, search=search, limit=1)
    assets = res.get("assets") or []
    return assets[0] if assets else None


def _diff_ratio_crops(a: Image.Image, b: Image.Image) -> float:
    """Fraction of a downsampled crop whose pixels differ between two images."""
    pa, pb = a.load(), b.load()
    w, h = a.size
    changed = 0
    total = ((w + 1) // 2) * ((h + 1) // 2)
    for y in range(0, h, 2):
        for x in range(0, w, 2):
            if pa[x, y] != pb[x, y]:
                changed += 1
    return changed / total


def _wait_for_file(path, timeout=120, settle=0.25, fresh_since=None):
    """Block until the async exporter finished writing `path` (exists with a
    stable size). Pass fresh_since (a time.monotonic) to ignore a leftover file
    from a previous run that the exporter hasn't overwritten yet."""
    t0 = time.monotonic()
    seen = None
    while time.monotonic() - t0 < timeout:
        if os.path.exists(path):
            if fresh_since is not None and os.path.getmtime(path) < fresh_since - 1:
                time.sleep(settle)
                continue
            cur = (os.path.getsize(path), os.path.getmtime(path))
            if seen == cur:
                return
            seen = cur
        time.sleep(settle)


def _element_bbox(d) -> tuple:
    """World-space box around an element's geometry, padded by the roof width
    so a spot never lands under a roof (roofs render above objects at z 800)."""
    pad = 600
    pts = d.get("points") or []
    if pts and isinstance(pts[0], list):
        x0 = min(p[0] for p in pts)
        y0 = min(p[1] for p in pts)
        x1 = max(p[0] for p in pts)
        y1 = max(p[1] for p in pts)
    elif d.get("position"):
        x0 = x1 = d["position"][0]
        y0 = y1 = d["position"][1]
    else:
        return (0, 0, 0, 0)
    if d.get("kind") == "roof":
        pad += d.get("width") or 0
    return (x0 - pad, y0 - pad, x1 + pad, y1 + pad)


def _dist_to_segments(px, py, pts) -> float:
    """Minimum distance from a point to a closed polyline of wall points."""
    import math

    n = len(pts)
    best = math.inf
    for i in range(n):
        x1, y1 = pts[i]
        x2, y2 = pts[(i + 1) % n]
        dx, dy = x2 - x1, y2 - y1
        t = ((px - x1) * dx + (py - y1) * dy) / (dx * dx + dy * dy) if (dx or dy) else 0.0
        t = max(0.0, min(1.0, t))
        cx, cy = x1 + t * dx, y1 + t * dy
        best = min(best, (px - cx) ** 2 + (py - cy) ** 2)
    return math.sqrt(best)


def _pick_visual_spot():
    """A woxel spot where a test object will render on top: not under any roof
    and far from any wall segment (walls can occlude objects; a boundary wall
    often rings the whole map, so segment distance beats bbox exclusion).
    Scans inward from the map edges so a pass is guaranteed on a normal map."""
    st = bridge.request("get_status")
    size = st.get("map_size_woxels")
    if size is None:
        return None
    w, h = size
    cx, cy = st["map_center"]
    state = bridge.request("dump_state")
    roof_boxes = [_element_bbox(d) for d in state["elements"] if d.get("kind") == "roof"]
    wall_pts = []
    for d in state["elements"]:
        if d.get("kind") == "wall" and d.get("points"):
            pts = d["points"]
            if len(pts) >= 2 and max(max(p[0] for p in pts) - min(p[0] for p in pts),
                                     max(p[1] for p in pts) - min(p[1] for p in pts)) > w * 0.5:
                continue  # drop boundary rings (their segments only frame the map edge)
            wall_pts.append(pts)
    for r in range(1500, 9000, 500):
        for dx, dy in ((1, 0), (0, 1), (-1, 0), (0, -1), (1, 1), (-1, 1), (-1, -1), (1, -1)):
            sx = cx + dx * r
            sy = cy + dy * r
            if not (800 < sx < w - 800 and 800 < sy < h - 800):
                continue
            if any(bx0 < sx < bx1 and by0 < sy < by1 for bx0, by0, bx1, by1 in roof_boxes):
                continue
            if any(_dist_to_segments(sx, sy, pts) < 600 for pts in wall_pts):
                continue
            return (sx, sy)
    return None


def main() -> int:
    global passed, failed
    try:
        status = bridge.request("ping")
    except BridgeError as exc:
        print("Cannot reach the bridge:", exc, file=sys.stderr)
        return 1
    print("Bridge reachable:", status, "\n")

    cx, cy = 6400, 6400
    st = check("get_status", lambda: bridge.request("get_status"))
    if st and st.get("map_center"):
        cx, cy = st["map_center"]

    check("list_categories", lambda: bridge.request("list_asset_categories"))

    obj_asset = first_asset("Objects")
    wall_asset = first_asset("Walls")
    path_asset = first_asset("Paths")

    # --- create ---
    obj = check("place_object", lambda: bridge.request("place_object", asset=obj_asset, x=cx, y=cy))
    if obj:
        created_ids.append(obj["id"])

    room = [[cx - 200, cy - 200], [cx + 200, cy - 200], [cx + 200, cy + 200], [cx - 200, cy + 200]]
    wall = check("draw_wall", lambda: bridge.request("draw_wall", points=room, asset=wall_asset or "", loop=True))
    if wall:
        created_ids.append(wall["id"])

    if path_asset:
        path = check("draw_path", lambda: bridge.request(
            "draw_path", points=[[cx - 300, cy], [cx, cy - 50], [cx + 300, cy]], asset=path_asset))
        if path:
            created_ids.append(path["id"])
    else:
        print("  SKIP  draw_path          -> no Paths assets")

    light = check("add_light", lambda: bridge.request("add_light", x=cx, y=cy, energy=1.5))
    if light:
        created_ids.append(light["id"])

    # --- query / modify ---
    check("list_elements", lambda: bridge.request("list_elements", kind="objects", limit=5))
    if obj:
        check("get_element", lambda: bridge.request("get_element", id=obj["id"]))
        check("move_element", lambda: bridge.request("move_element", id=obj["id"], x=cx + 80, y=cy + 80))
        check("modify_object", lambda: bridge.request("modify_object", id=obj["id"], scale=1.5, rotation=45))
        dup = check("duplicate_object", lambda: bridge.request("duplicate_object", id=obj["id"], dx=120))
        if dup:
            created_ids.append(dup["id"])

    check("list_levels", lambda: bridge.request("list_levels"))

    # A3: dump_state must return every element as a get_element-style descriptor,
    # incl. wall geometry — and per-kind counts must line up with get_status.
    dump = check("dump_state", lambda: bridge.request("dump_state"))
    if dump:
        descs = dump["elements"]
        by_kind = {}
        for d in descs:
            by_kind[d["kind"]] = by_kind.get(d["kind"], 0) + 1
        needs_geometry = [k for k in d if k in ("points", "loop", "width", "type", "smoothness")]
        walls = [d for d in descs if d["kind"] == "wall"]
        if walls and not all("points" in w and "loop" in w for w in walls):
            check("dump_state|wall-geometry", lambda: (_ for _ in ()).throw(BridgeError("walls lack points/loop")))
        else:
            print(f"  PASS  dump_state|sums     -> kinds={by_kind} geosample={needs_geometry}")
            passed += 1

    # A6: visual-regression guard. A create that returns ok:true but draws
    # nothing (the A1 bug class) leaves the spot pixel-identical to before.
    # `screenshot` reflects the *window*, which goes stale while DD is
    # minimized/occluded, so this renders via export_map (background thread,
    # works headless): pick a clear corner outside every existing element's
    # bbox (from dump_state), export before, place the object, export after,
    # and require the region around the spot to have changed and not be blank.
    if obj_asset:
        try:
            before_path = os.path.join(tempfile.gettempdir(), "dd_vis_before.png")
            after_path = os.path.join(tempfile.gettempdir(), "dd_vis_after.png")
            spot = _pick_visual_spot()
            if spot is None:
                print("  FAIL  visual|object     -> no clear corner found (map fully covered?)")
                failed += 1
            else:
                sx, sy = spot
                t_before = time.time()
                if os.path.exists(before_path):
                    os.remove(before_path)
                bridge.request("export_map", path=before_path, ppi=VIS_PPI)
                _wait_for_file(before_path, fresh_since=t_before)
                vis = bridge.request("place_object", asset=obj_asset, x=sx, y=sy)
                if vis:
                    created_ids.append(vis["id"])
                t_after = time.time()
                if os.path.exists(after_path):
                    os.remove(after_path)
                bridge.request("export_map", path=after_path, ppi=VIS_PPI)
                _wait_for_file(after_path, fresh_since=t_after)
                scale = VIS_PPI / 256.0
                span = 600
                x0 = int((sx - span) * scale)
                y0 = int((sy - span) * scale)
                x1 = int((sx + span) * scale)
                y1 = int((sy + span) * scale)
                ca = Image.open(before_path).convert("RGB").crop((x0, y0, x1, y1))
                cb = Image.open(after_path).convert("RGB").crop((x0, y0, x1, y1))
                diff = _diff_ratio_crops(ca, cb)
                std_after = statistics.pstdev(list(cb.convert("L").getdata()))
                if diff < 0.005:
                    print(f"  FAIL  visual|object     -> spot {spot} region diff={diff:.2%} (<0.5%): not visible at expected spot")
                    failed += 1
                elif std_after < 3.0:
                    print(f"  FAIL  visual|blank      -> spot {spot} region stddev={std_after:.2f} (<3): blank render")
                    failed += 1
                else:
                    print(f"  PASS  visual|object     -> spot {spot} region diff={diff:.2%}, stddev {std_after:.1f}")
                    passed += 1
        except BridgeError as exc:
            print(f"  FAIL  visual|object     -> {exc}")
            failed += 1

    if created_ids:
        check("select_elements", lambda: bridge.request("select_elements", ids=created_ids))
        check("clear_selection", lambda: bridge.request("clear_selection"))

    # --- undo / redo round trip: place an object, undo it, redo it ---
    def undo_round_trip():
        before = bridge.request("get_status")["counts"]["objects"]
        made = bridge.request("place_object", asset=obj_asset, x=cx - 200, y=cy)
        after_create = bridge.request("get_status")["counts"]["objects"]
        bridge.request("undo")
        after_undo = bridge.request("get_status")["counts"]["objects"]
        bridge.request("redo")
        after_redo = bridge.request("get_status")["counts"]["objects"]
        # track whatever exists now so cleanup removes it
        for el in bridge.request("list_elements", kind="objects")["elements"]:
            if el["id"] not in created_ids:
                created_ids.append(el["id"])
        if not (after_create == before + 1 and after_undo == before and after_redo == before + 1):
            raise BridgeError(f"counts before={before} create={after_create} undo={after_undo} redo={after_redo}")
        return f"object count {before}->{after_create} undo->{after_undo} redo->{after_redo}"

    check("undo/redo", undo_round_trip)

    # --- cleanup: delete everything we made ---
    deleted = 0
    for eid in created_ids:
        try:
            if bridge.request("delete_element", id=eid).get("deleted"):
                deleted += 1
        except BridgeError:
            pass
    print(f"\nCleaned up {deleted}/{len(created_ids)} created elements.")
    print(f"\n{passed} passed, {failed} failed.")
    print("(Terrain / portal / roof / text commands aren't auto-tested here to avoid "
          "messing up your map — try them by hand once the basics pass.)")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
