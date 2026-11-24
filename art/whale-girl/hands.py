"""Source the fitted limbs from the mesh assets shipped with default Caelis."""
import bpy
from mathutils import Matrix


def use_shipped_body(source):
    # Retain the working scene, face, armature and actions. Only these body
    # mesh datablocks come from the authoritative shipped character.
    names = ("Neck","Body / independent torso","leg.L","leg.R",
             "Hand / supported arm wrist and fingers.L")
    objects = [bpy.data.objects[n] for n in names]
    slots = [list(o.data.materials) for o in objects]
    with bpy.data.libraries.load(str(source),link=False) as (_, imported):
        imported.meshes = [o.data.name for o in objects]
    for obj, mesh, materials in zip(objects,imported.meshes,slots):
        obj["sourceMesh"] = obj.data.name
        obj.data = mesh
        mesh.materials.clear()
        for material in materials:
            mesh.materials.append(material)

    # The shipped left arm is closed from shoulder to fingertips. Mirror its
    # topology for the right side; the original right mesh has open edges.
    left = objects[-1]
    left["caelisLayer"] = "hands"
    left.name = "Caelis arm and hand.L"
    old = bpy.data.objects["Hand / supported arm wrist and fingers.R"]
    right = left.copy()
    right.data = left.data.copy()
    right.name = "Caelis arm and hand.R"
    bpy.context.scene.collection.objects.link(right)
    right.data.transform(Matrix.Diagonal((-1,1,1,1)))
    right.data.flip_normals()
    for group in right.vertex_groups:
        if group.name.endswith(".L"):
            group.name = group.name[:-2]+".R"
    bpy.data.objects.remove(old,do_unlink=True)
