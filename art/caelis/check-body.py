"""Run in Blender with shipped-sage-dress.blend to review the fitted body."""
import bpy
import bmesh
import numpy as np
from mathutils.bvhtree import BVHTree


def check_rest_surface(mesh):
    bm = bmesh.new()
    try:
        bm.from_mesh(mesh)
        assert all(edge.is_manifold for edge in bm.edges), "open or non-manifold body edge"
        seen = set()
        pending = [next(iter(bm.verts))]
        while pending:
            vertex = pending.pop()
            if vertex in seen:
                continue
            seen.add(vertex)
            pending.extend(edge.other_vert(vertex) for edge in vertex.link_edges)
        assert len(seen) == len(bm.verts), "disconnected body surface"
        bmesh.ops.triangulate(bm, faces=list(bm.faces))
        bm.faces.ensure_lookup_table()
        tree = BVHTree.FromBMesh(bm)
        for a, b in tree.overlap(tree):
            if a < b:
                assert not set(bm.faces[a].verts).isdisjoint(bm.faces[b].verts), (
                    "intersecting body faces", a, b)
    finally:
        bm.free()


def positions(mesh):
    xyz = np.empty(len(mesh.vertices) * 3, dtype=np.float32)
    mesh.vertices.foreach_get("co", xyz)
    return xyz.reshape((-1, 3))


scene = bpy.context.scene
body = scene.objects["Caelis / continuous fitted body"]
rig = body.find_armature()
check_rest_surface(body.data)
for vertex in body.data.vertices:
    assert 1 <= len(vertex.groups) <= 4
    assert abs(sum(group.weight for group in vertex.groups) - 1) < 0.0001
    assert all(body.vertex_groups[g.group].name in rig.data.bones for g in vertex.groups)

# Track edges across shoulders and hips over the entire authored motion.
# Very short tessellation edges amplify floating-point noise.
rest = positions(body.data)
edges = np.array([edge.vertices[:] for edge in body.data.edges])
starts, ends = edges.T
centers = (rest[starts] + rest[ends]) / 2
x, z = np.abs(centers[:, 0]), centers[:, 2]
shoulders = (x > .085) & (x < .26) & (z > .77) & (z < .995)
hips = (x < .18) & (z > .38) & (z < .65)
lengths = np.linalg.norm(rest[ends] - rest[starts], axis=1)
use = (shoulders | hips) & (lengths > .004) & (lengths < .05)
starts, ends, lengths = starts[use], ends[use], lengths[use]
assert len(starts) > 100

depsgraph = bpy.context.evaluated_depsgraph_get()
for name in ("idle", "working", "attention", "nod", "celebrate"):
    action = bpy.data.actions[name]
    rig.animation_data.action = action
    first, last = map(int, action.frame_range)
    low, high = 1.0, 1.0
    for frame in range(first, last + 1):
        scene.frame_set(frame)
        posed = positions(body.evaluated_get(depsgraph).data)
        assert np.isfinite(posed).all(), (name, frame, "non-finite body position")
        ratios = np.linalg.norm(posed[ends] - posed[starts], axis=1) / lengths
        low, high = min(low, float(ratios.min())), max(high, float(ratios.max()))
    assert low > .25 and high < 2.5, (name, "collapsed or stretched joint", low, high)
    print(f"{name}: {last-first+1} frames, shoulder/hip edge ratios {low:.3f}–{high:.3f}")
