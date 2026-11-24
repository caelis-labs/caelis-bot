"""Blender authoring check for the continuous figure and its five performances."""
import bpy
import bmesh
from mathutils.bvhtree import BVHTree

scene = bpy.context.scene
body = scene.objects['Whale Girl / continuous figure']
rig = body.find_armature()
assert rig and len([o for o in scene.objects if o.type == 'ARMATURE']) == 1
bm = bmesh.new()
bm.from_mesh(body.data)
assert all(e.is_manifold for e in bm.edges), 'body has an open or non-manifold edge'
assert len(bm.verts) - len(bm.edges) + len(bm.faces) == 2
seen = set()
pending = [next(iter(bm.verts))]
while pending:
    v = pending.pop()
    if v not in seen:
        seen.add(v)
        pending.extend(e.other_vert(v) for e in v.link_edges)
assert len(seen) == len(bm.verts), 'body is disconnected'
bm.free()

triangles = [p.vertices[:] for p in body.data.polygons]
neighbors = [set(t) for t in triangles]
assert all(len(t) == 3 for t in triangles)
for vertex in body.data.vertices:
    assert 1 <= len(vertex.groups) <= 4
    assert abs(sum(g.weight for g in vertex.groups)-1) < .0001
    assert all(body.vertex_groups[g.group].name in rig.data.bones for g in vertex.groups)


def intersections(mesh):
    coords = [v.co[:] for v in mesh.vertices]
    tree = BVHTree.FromPolygons(coords, triangles, all_triangles=True)
    return sum(1 for a,b in tree.overlap(tree) if a<b and neighbors[a].isdisjoint(neighbors[b]))


assert intersections(body.data) == 0, 'rest mesh intersects itself'
depsgraph = bpy.context.evaluated_depsgraph_get()
failures = []
for name in ('idle','working','attention','nod','celebrate'):
    anim = rig.animation_data
    anim.action = bpy.data.actions[name]
    anim.action_slot = anim.action.slots[0]
    first,last = map(int,anim.action.frame_range)
    for frame in range(first,last+1):
        scene.frame_set(frame)
        count = intersections(body.evaluated_get(depsgraph).data)
        if count:
            failures.append((name,frame,count))
    print(name,last-first+1,'frames checked',flush=True)
assert not failures, failures
