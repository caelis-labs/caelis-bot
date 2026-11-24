"""Blender 5.2.1: open the source blend, then pass -- OUTPUT.glb [--caelis-base]."""
import json
import sys
from pathlib import Path

import bpy
from mathutils import Matrix

args = sys.argv[sys.argv.index("--") + 1:]
output = Path(args[0]).resolve()
enhanced = "--caelis-base" in args
scene = bpy.context.scene
acting = bool(scene.get("pairedFaceActions"))
rig, = [obj for obj in scene.objects if obj.type == "ARMATURE"]
rig.animation_data.action = None
if acting:
    for obj in scene.objects:
        keys = getattr(obj.data, "shape_keys", None)
        if keys and keys.animation_data:
            keys.animation_data.action = None
            for key in keys.key_blocks:
                key.value = 0
scene.frame_set(1)
for bone in rig.pose.bones:
    bone.matrix_basis = Matrix.Identity(4)
bpy.context.view_layer.update()
# Outfit collections are an authoring switch. Honor the current view layer
# before joining meshes, including the garment's trim and inverted outlines.
# Legacy scenes without tagged outfits retain their original export behavior.
outfit_meshes = {obj for collection in bpy.data.collections if collection.get("caelisOutfit")
                 for obj in collection.all_objects}
layers = {}
for obj in list(scene.objects):
    if obj.type != "MESH" or "deform_group" not in obj:
        continue
    if obj in outfit_meshes and (obj.name not in bpy.context.view_layer.objects or not obj.visible_get()):
        continue
    if enhanced:
        duplicate = obj.copy()
        duplicate.data = obj.data.copy()
        scene.collection.objects.link(duplicate)
        obj = duplicate
    layer = obj.get("caelisLayer", "body")
    # This operates in memory only. Shape endpoints are restored by the optional
    # enhanced exporter; the basic character uses the closed-mouth rest pose.
    keep_keys = acting and obj.data.shape_keys and obj.data.shape_keys.animation_data
    if not enhanced and obj.data.shape_keys and not keep_keys:
        shape = obj.data.shape_keys.key_blocks.get("mouthClosed")
        if shape:
            positions = [p.co.copy() for p in shape.data]
            obj.shape_key_clear()
            for v,p in zip(obj.data.vertices, positions):
                v.co = p
    if not keep_keys:
        obj.shape_key_clear()
    bpy.context.view_layer.objects.active = obj
    for modifier in list(obj.modifiers):
        if modifier.type in {"SUBSURF", "SOLIDIFY"}:
            bpy.ops.object.modifier_move_to_index(modifier=modifier.name, index=0)
            bpy.ops.object.modifier_apply(modifier=modifier.name)
    layers.setdefault(layer, []).append(obj)

exports = []
for layer, objects in layers.items():
    bpy.ops.object.select_all(action="DESELECT")
    for obj in objects:
        obj.select_set(True)
    bpy.context.view_layer.objects.active = objects[0]
    if not acting or len(objects) > 1:
        bpy.ops.object.join()
    joined = bpy.context.object
    joined.name = "Caelis_" + layer if enhanced else layer
    joined["caelisLayer"] = layer
    slots = list(joined.data.materials)
    materials = list(dict.fromkeys(slots))
    indices = [materials.index(slots[p.material_index]) for p in joined.data.polygons]
    joined.data.materials.clear()
    for material in materials:
        joined.data.materials.append(material)
    for polygon, index in zip(joined.data.polygons, indices):
        polygon.material_index = index
    world = joined.matrix_world.copy()
    joined.parent = None
    joined.matrix_world = world
    exports.append(joined)

if enhanced:
    scene["desktopPetProfile"] = "reference-v1"
    scene["desktopPetMouthRest"] = "closed"
    scene["desktopPetGestures"] = json.loads((Path(__file__).resolve().parents[1] /
                                              "caelis/interaction-poses.json").read_text())
bpy.ops.object.select_all(action="DESELECT")
for obj in [rig, *exports]:
    obj.select_set(True)
bpy.context.view_layer.objects.active = rig
# The basic content contract supports core metallic/roughness materials.
if not enhanced:
    for material in bpy.data.materials:
        if material.node_tree:
            for node in material.node_tree.nodes:
                if node.type == "BSDF_PRINCIPLED":
                    node.inputs["Specular IOR Level"].default_value = .5
bpy.ops.export_scene.gltf(
    filepath=str(output), export_format="GLB", use_selection=True,
    export_animations=True, export_animation_mode="ACTIONS", export_anim_single_armature=not acting,
    export_merge_animation="NLA_TRACK" if acting else "ACTION",
    export_force_sampling=True, export_frame_range=False, export_anim_slide_to_zero=True,
    **({"export_try_sparse_sk":False, "export_morph_normal":False} if acting else {}),
    export_skins=True, export_materials="EXPORT", export_extras=enhanced, export_texcoords=False)
