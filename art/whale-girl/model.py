"""Build the Whale Girl study from the shared sage-dress scene in Blender 5.2.1."""
from math import cos, exp, pi, sin
from pathlib import Path

import bpy
import sys
from mathutils import Euler, Vector

folder = Path(__file__).resolve().parent
sys.path.insert(0,str(folder))
from dress import mary_janes
bpy.ops.wm.open_mainfile(filepath=str(folder.parent / "caelis/sage-dress.blend"), use_scripts=False)
from hands import use_shipped_body
use_shipped_body(folder.parent/"caelis/shipped-sage-dress.blend")
rig = bpy.data.objects["XiaoAi_Rig"]
scene = bpy.context.scene
bpy.context.preferences.filepaths.save_version = 0


def linear(hex_color):
    def channel(start):
        value = int(hex_color[start:start + 2], 16) / 255
        return value / 12.92 if value <= .04045 else ((value + .055) / 1.055) ** 2.4
    return Vector(tuple(channel(i) for i in (0, 2, 4)))


def paint(material, color):
    rgba = (*linear(color), 1)
    material.diffuse_color = rgba
    shader = material.node_tree.nodes.get("Principled BSDF")
    shader.inputs["Base Color"].default_value = rgba


def fabric(name, color):
    material = bpy.data.materials.new(name)
    material.use_nodes = True
    paint(material, color)
    return material


navy = fabric("Midnight velvet", "374683")
white = fabric("Pearl lace", "f8f2f1")
white.node_tree.nodes.get("Principled BSDF").inputs["Emission Color"].default_value=(.055,.078,.115,1)
white.node_tree.nodes.get("Principled BSDF").inputs["Emission Strength"].default_value=1
blue = fabric("Ocean ribbon", "389fd4")
sock = fabric("Milk white stockings", "fff8f3")
sock.node_tree.nodes.get("Principled BSDF").inputs["Emission Color"].default_value=(.89,.88,.84,1)
sock.node_tree.nodes.get("Principled BSDF").inputs["Emission Strength"].default_value=.22
patent = fabric("Marine patent leather","293c6b")
patent.node_tree.nodes.get("Principled BSDF").inputs["Roughness"].default_value=.36
# Preserve the blue in shadows under the community pack's warm key light.
fills={"Rose silk / painted vertex gradient":(.014,.017,.025),
       "Caelis / garden sage cotton":(.026,.025,.036),
       "Midnight velvet":(.012,.015,.028)}
for material in bpy.data.materials:
    name = material.name
    if "soft hand skin" in name:
        # Retain form at the wrist while matching the illustrated face in the
        # preview lights. The exposed hand receives less light under the cuff.
        paint(material, "c5b2a8")
        shader=material.node_tree.nodes.get("Principled BSDF")
        shader.inputs["Emission Color"].default_value=(.525,.45,.47,1)
        shader.inputs["Emission Strength"].default_value=1
    if name=="Neck skin / soft rose":
        paint(material,"f1c7b7")
        shader=material.node_tree.nodes.get("Principled BSDF")
        shader.inputs["Emission Color"].default_value=(.46,.30,.26,1)
        shader.inputs["Emission Strength"].default_value=.48
    if name in fills:
        shader=material.node_tree.nodes.get("Principled BSDF")
        shader.inputs["Emission Color"].default_value=(*fills[name],1)
        shader.inputs["Emission Strength"].default_value=1
    if "sage" in name or "woven ribbon" in name or "olive" in name:
        paint(material, "2c396d")
    elif "cream cotton" in name:
        paint(material, "334777")
    elif "Quiet rose" in name or "rose brows" in name:
        paint(material, "293868")

for obj in list(scene.objects):
    if obj.type != "MESH":
        continue
    if any(part in obj.name for part in ("Caelis / shoe bow","hair pin", "hairpin", "side hair bow", "garden applique")):
        bpy.data.objects.remove(obj, do_unlink=True)
        continue
    if obj.name.startswith(("Caelis / ivory sock","Caelis / ivory vamp","leg.")):
        obj.data.materials[0]=sock
    if obj.name.startswith(("Caelis / sage shoe","Caelis / Mary Jane strap")):
        obj.data.materials[0]=patent
    if obj.name.startswith("Caelis / garden collar"):
        obj.data.materials[0] = white

bpy.context.view_layer.objects.active = rig
rig.select_set(True)
bpy.ops.object.mode_set(mode="EDIT")
tail_root = (0,.145,.712)
bend = (.36,.610,.466)
fork = (.975,.611,.803)
for name, start, end, parent in [
    ("tail.base",tail_root,bend,"pelvis"), ("tail.flukes",bend,fork,"tail.base")
]:
    bone = rig.data.edit_bones.new(name)
    bone.head, bone.tail = start, end
    bone.parent = rig.data.edit_bones[parent]
bpy.ops.object.mode_set(mode="OBJECT")


def surface(name, vertices, faces, material, joint, subdiv=1):
    mesh = bpy.data.meshes.new(name)
    mesh.from_pydata(vertices, [], faces)
    mesh.update()
    obj = bpy.data.objects.new(name, mesh)
    scene.collection.objects.link(obj)
    mesh.materials.append(material)
    for face in mesh.polygons:
        face.use_smooth = True
    obj["caelisLayer"] = "accessories"
    obj["deform_group"] = joint
    obj.parent = rig
    group = obj.vertex_groups.new(name=joint)
    group.add(list(range(len(vertices))), 1, "REPLACE")
    if subdiv:
        mod = obj.modifiers.new("Rounded cloth edge", "SUBSURF")
        mod.levels = mod.render_levels = subdiv
    mod = obj.modifiers.new("Character binding", "ARMATURE")
    mod.object = rig
    return obj


def tube(name, rings, material, joint):
    # Each transverse ellipse follows the centerline of its fin or ribbon.
    verts, faces = [], []
    sections = 12
    centers = [Vector(row[:3]) for row in rings]
    for i, (_, _, _, width, depth) in enumerate(rings):
        tangent = (centers[min(i+1,len(rings)-1)] - centers[max(0,i-1)]).normalized()
        width_axis = tangent.cross(Vector((0,1,0))).normalized()
        depth_axis = tangent.cross(width_axis).normalized()
        for j in range(sections):
            a = 2*pi*j/sections
            verts.append(centers[i] + width_axis*width*cos(a) + depth_axis*depth*sin(a))
            if i:
                k = (i-1)*sections+j
                following = (i-1)*sections+(j+1)%sections
                faces.append((k,following,following+sections,k+sections))
    faces.extend([tuple(reversed(range(sections))),tuple(range(len(verts)-sections,len(verts)))])
    return surface(name, verts, faces, material, joint)


# The upper opening reveals the stocking. Fit a strap and brass buckle
# over the vamp and attach both to the foot, including in the lifted pose.
brass=fabric("Shoe buckle brass","dbb968")
welt=fabric("Indigo shoe welt","283452")
mary_janes(surface,tube,patent,sock,welt,brass)

for side, s in (("L",1),("R",-1)):
    verts,faces=[],[]
    # The upper navy surface folds over a scalloped pearl underside. The
    # thickness falls towards the free tip and remains visible in profile.
    samples=36
    for i in range(samples+1):
        t=i/samples
        width=.012+.100*sin(pi*t)**.78
        center=1.563-.137*sin(t*pi*.59)+.021*t**4
        for j in range(16):
            a=2*pi*j/16
            scallop=.019*(.5+.5*cos(t*8*pi))*max(0,-cos(a))*sin(pi*t)
            verts.append((s*(.335+.47*t),-.096+.29*t**1.30+(.008+.059*sin(pi*t))*sin(a),
                          center+width*cos(a)-scallop))
            if i:
                k=(i-1)*16+j
                n=(i-1)*16+(j+1)%16
                faces.append((k,n,n+16,k+16))
    faces.extend([tuple(reversed(range(16))),tuple(range(len(verts)-16,len(verts)))])
    if s>0:
        faces=[tuple(reversed(face)) for face in faces]
    fin=surface("Scalloped head fin."+side,verts,faces,navy,"head",1)
    fin.data.materials.append(white)
    for poly in list(fin.data.polygons)[:samples*16]:
        if cos(2*pi*(poly.index%16+.5)/16)<-.70:
            poly.material_index=1
    if side=="L":
        for d in (-1,1):
            tube("Temple ribbon loop "+str(d),[(s*(.456+d*x),-.350-y,1.588+z,w,h)
                for x,y,z,w,h in [(0,0,0,.015,.012),(.030,.015,.014,.026,.012),
                 (.064,.019,.030,.041,.017),(.082,.007,.041,.030,.009)]],blue,"head")
            tube("Temple ribbon end "+str(d),[(.456,-.350,1.578,.016,.011),
                 (.456+d*.028,-.347,1.533,.024,.013),(.456+d*.051,-.336,1.502,.014,.008)],blue,"head")
        tube("Temple bow knot",[(.455,-.355,1.567,.013,.012),(.457,-.365,1.584,.025,.020),
                                (.458,-.354,1.605,.015,.013)],blue,"head")

tube("Back waist knot",[(0,.195,.762,.033,.029),(0,.217,.79,.049,.037),
                        (0,.223,.818,.037,.029)],white,"pelvis")
for side,s in (("L",1),("R",-1)):
    tube("Back waist bow."+side,[(0,.191,.795,.032,.030),
        (s*.10,.219,.823,.065,.030),(s*.173,.245,.851,.068,.033),
        (s*.185,.250,.842,.015,.016)],white,"pelvis")
    tube("Back sash."+side,[(0,.192,.795,.033,.020),(s*.057,.226,.732,.050,.020),
        (s*.105,.249,.651,.039,.014),(s*.137,.259,.606,.030,.009)],white,"pelvis")
# Pleats turn over the crown and gather along a continuous blue welt.
for label,material,rows in (("Pleated pearl headdress",white,6),("Bound headdress seam",navy,2)):
    vertices,faces=[],[]
    for i in range(129):
        t=pi*i/128
        for j in range(rows):
            v=j/(rows-1)
            fold=cos(34*t+.32*sin(5*t))
            if rows==2:
                rise=.002+.011*v
                depth=-.246-.003*v
            else:
                rise=(.067+.010*fold+.004*cos(17*t))*v
                depth=-.257+.061*v-.015*sin(pi*v)+.020*fold*v
            # Follow the sagittal curve of the scalp rather than standing the
            # pleats in one vertical plane in front of the crown.
            depth+=.173*sin(t)**3+.037*abs(cos(t))**3
            vertices.append(((.452+rise)*cos(t),depth,1.54+(.388+rise)*sin(t)))
            if i and j:
                k=i*rows+j
                faces.append((k-rows-1,k-1,k,k-rows))
    cap=surface(label,vertices,faces,material,"head")
    if rows==6:
        shadow=fabric("Cool headdress pleat","d1d9e6")
        cap.data.materials.append(shadow)
        for poly in cap.data.polygons:
            angle=pi*(poly.index//(rows-1)+.5)/128
            if sin(34*angle+.32*sin(5*angle))>.94:
                poly.material_index=1
    cap.modifiers.new("Fold thickness","SOLIDIFY").thickness=.0035

# Place the apron directly on the front of the flared skirt.
skirt=bpy.data.objects["Caelis / sage flared skirt"]
vertices, faces=[],[]
apron_rows,apron_cols=29,65
for row in range(apron_rows):
    t=row/(apron_rows-1)
    for col in range(apron_cols):
        u=2*col/(apron_cols-1)-1
        x=u*(.112+.036*t-.016*t**3)
        z=.788-.257*t+.068*t*u**4
        hit, point, _, _ = skirt.ray_cast(Vector((x,-1,z)),Vector((0,1,0)))
        assert hit,(x,z)
        # A shallow hanging fold descends from either end of the waistband.
        ridge=exp(-((t-(.39-.37*u*u))/.10)**2)
        folds=sin(pi*t)**.72*(.0065*ridge+.003*cos(3*pi*u)*(1-u*u))
        vertices.append((x,point.y-.008-folds,z))
        if row and col:
            k=row*apron_cols+col
            faces.append((k-apron_cols-1,k-apron_cols,k,k-1))
apron=surface("Scalloped white apron",vertices,[f[::-1] for f in faces],white,"pelvis")
# Copy the closest skirt surface weights so the apron follows the dress in motion.
from mathutils.kdtree import KDTree
tree=KDTree(len(skirt.data.vertices))
for v in skirt.data.vertices:
    tree.insert(v.co,v.index)
tree.balance()
apron.vertex_groups.clear()
for v in apron.data.vertices:
    _, index, _=tree.find(v.co)
    for influence in skirt.data.vertices[index].groups:
        name=skirt.vertex_groups[influence.group].name
        group=apron.vertex_groups.get(name) or apron.vertex_groups.new(name=name)
        group.add([v.index],influence.weight,"REPLACE")
apron.modifiers.new("Apron hem thickness","SOLIDIFY").thickness=.0035

# A small embroidered whale breaks up the white apron at desktop scale.
outline = [(-.054,.011),(-.046,.026),(-.018,.031),(.007,.021),
           (.025,.005),(.039,.012),(.052,.026),(.051,.009),
           (.045,-.005),(.060,-.009),(.046,-.015),(.027,-.011),
           (.013,-.023),(-.019,-.025),(-.044,-.014)]
# Stitch into the apron folds. Subdivide the motif in its pattern plane
# before projecting it, so a ridge cannot cut through the whale in profile.
from mathutils.geometry import tessellate_polygon
pattern=[Vector((.53*x+.084,.53*z+.588,0)) for x,z in outline]
badge,panels=[],[]
for a,b,c in tessellate_polygon([pattern]):
    rows=[]
    for row in range(6):
        indices=[]
        for col in range(6-row):
            x,z,_=pattern[a]+(pattern[b]-pattern[a])*col/5+(pattern[c]-pattern[a])*row/5
            hit,p,_,_=apron.ray_cast(Vector((x,-1,z)),Vector((0,1,0)))
            assert hit
            indices.append(len(badge))
            badge.append((x,p.y-.004,z))
        if rows:
            prev=rows[-1]
            for k,index in enumerate(indices):
                panels.append((prev[k],prev[k+1],index))
                if k:
                    panels.append((prev[k],index,indices[k-1]))
        rows.append(indices)
emblem=surface("Embroidered whale",badge,panels,fabric("Whale emblem indigo","1e2a56"),"pelvis",0)
emblem.vertex_groups.clear()
for v in emblem.data.vertices:
    _,index,_=tree.find(v.co)
    for influence in skirt.data.vertices[index].groups:
        name=skirt.vertex_groups[influence.group].name
        group=emblem.vertex_groups.get(name) or emblem.vertex_groups.new(name=name)
        group.add([v.index],influence.weight,"REPLACE")

tail_paint=fabric("Indigo whale with blue belly","344e87")
shader=tail_paint.node_tree.nodes.get("Principled BSDF")
shader.inputs["Roughness"].default_value=.69
attr=tail_paint.node_tree.nodes.new("ShaderNodeVertexColor")
attr.layer_name="Countershade"
tail_paint.node_tree.links.new(attr.outputs["Color"],shader.inputs["Base Color"])

def countershade(obj,rings):
    obj["caelisLayer"]="whale_tail"
    paint=obj.data.color_attributes.new(name="Countershade",type="FLOAT_COLOR",domain="POINT")
    for vertex in obj.data.vertices:
        i=vertex.index%rings
        angle=2*pi*i/rings
        # Front-facing quarter of the lower surface catches the pale band.
        ventral=-cos(angle)+(.24 if rings==12 else -.22)*sin(angle)
        blend=max(0,min(1,(ventral-.38)/.46))
        rgba=linear("354888").lerp(linear("94c5ed"),blend*blend*(3-2*blend))
        paint.data[vertex.index].color=(*rgba,1)

stem=tube("Whale tail",[(*tail_root,.126,.112),(.015,.284,.668,.122,.111),
    (.070,.425,.570,.119,.102),(.183,.558,.485,.113,.096),(*bend,.095,.087),
    (.559,.679,.467,.092,.082),(.737,.703,.526,.084,.073),
    (.867,.677,.633,.065,.063),(.938,.642,.730,.055,.055),
    (*fork,.049,.049)],tail_paint,"tail.base")
countershade(stem,12)
distal=stem.vertex_groups.new(name="tail.flukes")
for v in stem.data.vertices:
    w=max(0,min(1,(v.co.x-.24)/.24))
    stem.vertex_groups["tail.base"].add([v.index],1-w,"REPLACE")
    distal.add([v.index],w,"REPLACE")

# One watertight fluke. Each transverse section spans the upper lip of
# one lobe and its softly rounded underside; the middle dips to the fork.
half=[(0,-.045,.094,.061),(.042,-.038,.174,.062),(.093,-.010,.249,.060),
      (.164,.027,.271,.053),(.225,.086,.281,.040),(.269,.154,.279,.030),
      (.292,.220,.284,.020),(.299,.261,.289,.009)]
stations=[(-x,lo,hi,d) for x,lo,hi,d in reversed(half[1:])]+half
verts,faces=[],[]
for i,(x,lo,hi,depth) in enumerate(stations):
    for j in range(16):
        a=2*pi*j/16
        z=(lo+hi)/2+(hi-lo)*cos(a)/2
        # Cant the fork around the body so the lobes still spread in profile.
        verts.append((fork[0]+.887*x-.462*depth*sin(a),
                      fork[1]+.462*x+.887*depth*sin(a)+.22*x*x,
                      fork[2]+z-.21*x))
        if i:
            k=(i-1)*16+j
            n=(i-1)*16+(j+1)%16
            faces.append((k,n,n+16,k+16))
faces.extend([tuple(reversed(range(16))),tuple(range(len(verts)-16,len(verts)))])
flukes=surface("Paired whale flukes",verts,[tuple(reversed(f)) for f in faces],tail_paint,"tail.flukes",2)
countershade(flukes,16)

from fit import fit
fit(rig, surface, tube, navy, white, blue, fabric, apron_rows, apron_cols)
from wardrobe import complete_fitting_body, dress_bib, organize_wardrobe, paint_fabric
from dress import fit_sleeves_to_arms
fit_sleeves_to_arms(scene,rig)
complete_fitting_body(fabric)
dress_bib(surface, fabric, white)
from cel import finish_cels
finish_cels(scene,fabric,linear)
organize_wardrobe(scene, linear)
paint_fabric(scene)
from body import bind_clay, compact_arms, paint_exposed_skin
figure=bind_clay(scene,rig,folder/"studies/body-clay-v1.blend")
from textiles import finish_textiles
finish_textiles(scene,fabric,linear)
compact_arms(scene,rig)
paint_exposed_skin(figure,rig)
from standing_bind import open_greeting_thumb
open_greeting_thumb(rig)
scene["pairedFaceActions"]=True
scene.render.filepath="//preview.png"
# Open on the standing figure with an orbit pivot inside the torso.
for area in bpy.context.screen.areas:
    if area.type=="VIEW_3D":
        space=area.spaces.active
        space.region_3d.view_rotation=Euler((pi/2,0,0)).to_quaternion()
        space.region_3d.view_location=(0,0,.90)
        space.region_3d.view_distance=2.86
        space.region_3d.view_perspective="ORTHO"
        space.overlay.show_overlays=False
        space.overlay.show_extras=False
bpy.ops.wm.save_as_mainfile(filepath=str(folder/"whale-girl.blend"),compress=True)
