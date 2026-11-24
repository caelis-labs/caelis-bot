"""Fit the Whale Girl foundation and collect the removable maid ensemble."""
from math import cos, exp, pi, sin
import bpy
from mathutils.bvhtree import BVHTree


def complete_fitting_body(fabric):
    import bmesh
    from math import atan2

    jersey = fabric("Fitting jersey", "66768e")
    torso = bpy.data.objects["Body / independent torso"]
    torso.data.materials[0] = jersey
    for vertex in torso.data.vertices:
        p = vertex.co
        weight = max(0, (.54-p.z)/.107)**2
        p.z += weight*(.012+.040*(abs(p.x)/.13)**2)
        p.x *= 1-.16*weight
        p.y = .023+(p.y-.023)*(1-.35*weight)

    for side,sign in (("L",1),("R",-1)):
        # Keep the Caelis knee loops and their original skin weights. The
        # shipped leg ends inside the sock. Extrude its bottom ring down
        # through the ankle into the toe box for a complete fitting figure.
        leg = bpy.data.objects["leg."+side]
        leg.data.materials[0] = jersey
        bm = bmesh.new()
        bm.from_mesh(leg.data)
        cap = min((f for f in bm.faces if len(f.verts)>8),key=lambda f:f.calc_center_median().z)
        center = cap.calc_center_median()
        previous = sorted(cap.verts,key=lambda v:atan2(v.co.y-center.y,v.co.x-center.x))
        bmesh.ops.delete(bm,geom=[cap],context="FACES_ONLY")
        for v in bm.verts:
            p = v.co
            taper = min(1,max(0,(.30-p.z)/.14))
            p.x = center.x+(p.x-center.x)*(1-.23*taper)
            p.y = center.y+(p.y-center.y)*(1-.21*taper)
            hip = min(1,max(0,(p.z-.39)/.075))**1.4
            axis = sign*.102
            p.x = axis+(p.x-axis)*(1-.32*hip)-sign*.035*hip
            p.y = .024+(p.y-.024)*(1-.22*hip)
            p.z += .058*hip
        deform = bm.verts.layers.deform.active
        shin = leg.vertex_groups["knee."+side].index
        foot = leg.vertex_groups.new(name="foot."+side).index
        for z,x,y,rx,ry,w in ((.127,.122,.020,.033,.034,.18),(.096,.122,.012,.030,.034,.64),
                (.074,.122,-.009,.038,.055,.94),(.059,.121,-.029,.046,.078,1),
                (.045,.121,-.045,.050,.089,1),(.034,.121,-.043,.048,.086,1),
                (.024,.121,-.040,.038,.065,1)):
            ring = []
            for j in range(len(previous)):
                angle = -pi+2*pi*j/len(previous)
                point = bm.verts.new((sign*x+rx*cos(angle),y+ry*sin(angle),z))
                point[deform][shin] = 1-w
                point[deform][foot] = w
                ring.append(point)
            for j,p in enumerate(ring):
                k = (j+1)%len(ring)
                bm.faces.new((previous[j],previous[k],ring[k],p)).smooth = True
            previous = ring
        bm.faces.new(previous)
        bmesh.ops.recalc_face_normals(bm,faces=list(bm.faces))
        bm.to_mesh(leg.data)
        bm.free()
        leg.data.update()


def dress_bib(surface, fabric, ivory):
    blouse = bpy.data.objects["Caelis / connected blouse and sleeves"]
    tree = BVHTree.FromPolygons([v.co for v in blouse.data.vertices],
                                [p.vertices[:] for p in blouse.data.polygons])
    # The cotton insert runs to the velvet waistband. Four narrow pintucks
    # are pressed into the same fabric; the collar bow sits above them.
    def panel(u,t):
        x = u*(.065+.046*t)
        z = .782+.177*t+.012*u*u*(1-t)
        point = tree.ray_cast((x,-1,z),(0,1,0))[0]
        if point is None:
            raise ValueError(f"Shirt bib missed bodice at {(x,z)}")
        tucks = exp(-((abs(u)-.70)/.06)**2)+exp(-((abs(u)-.46)/.055)**2)
        return (x,point.y-.0038-.0028*sin(pi*t)**.8*tucks,z)
    verts, faces = [], []
    cols = 49
    for i in range(25):
        for j in range(cols):
            verts.append(panel(2*j/(cols-1)-1,i/24))
            if i and j:
                k = len(verts)-1
                faces.append((k-cols-1,k-cols,k,k-1))
    surface("Pintucked shirt bib",verts,faces,ivory,"chest",0)
    buttons = fabric("Shirt button indigo", "38446b")
    for i,t in enumerate((.16,.39,.64)):
        x,y,z = panel(0,t)
        outline = [(x+.0044*cos(2*pi*j/20),y-.0018,z+.0053*sin(2*pi*j/20)) for j in range(20)]
        surface("Bib button "+str(i+1),[(x,y-.004,z),*outline],
                [(0,j+1,(j+1)%20+1) for j in range(20)],buttons,"chest",0)


# The costume takes its own decoration and outlines. In particular the
# head fins and tail are anatomy, while the fabric at their roots is removable.
MAID_PIECES = {
    "Dress and sleeves": ("Caelis / connected blouse","Caelis / sage flared skirt",
        "Caelis / garden waistband","Caelis / garden collar","Gathered sleeve","Gathered standing collar",
        "Pintucked shirt bib","Bib button","Velvet waist panel","Waist button","Whale hem"),
    "Apron and ribbons": ("Scalloped white apron","Pleated apron border","Pinafore shoulder frill",
        "Embroidered whale","Collar bow","Knotted collar","Back waist","Back sash","Whale tail root flounce"),
    "Headpiece": ("Temple ribbon","Temple bow knot","Pleated pearl headdress","Bound headdress seam"),
    "Stockings and shoes": ("Mary Jane","Caelis / ivory sock","Stocking instep"),
}


def organize_wardrobe(scene, linear):
    def group(name,parent):
        c = bpy.data.collections.new(name)
        parent.children.link(c)
        return c
    base = group("Whale Girl | Character",scene.collection)
    outfit = group("Whale Girl | Maid outfit",scene.collection)
    outfit["caelisOutfit"] = "whale-maid"
    outfit.asset_mark()
    outfit.asset_data.description = "Fitted Whale Girl maid ensemble. Bind replacement pieces to the character rig."
    parts = {name:group(name,outfit) for name in MAID_PIECES}
    body_groups = {name:group(name,base) for name in ("Figure","Head and expression","Hair and fins","Whale tail and flukes")}
    meshes = [o for o in scene.objects if o.type=="MESH" and "deform_group" in o]
    owners = {}
    for obj in meshes:
        name = obj.name.removeprefix("Contour / ")
        piece = next((k for k,prefixes in MAID_PIECES.items() if name.startswith(prefixes)),None)
        if piece:
            dest = parts[piece]
            layer = {"Dress and sleeves":"maid_dress","Apron and ribbons":"maid_apron",
                     "Headpiece":"maid_headpiece","Stockings and shoes":"maid_footwear"}[piece]
            obj["caelisLayer"] = "maid_ink" if obj.name.startswith("Contour / ") else layer
        elif name.startswith(("Hair /","Hooked crown","Scalloped head fin")):
            dest = body_groups["Hair and fins"]
        elif name in ("Whale tail","Paired whale flukes"):
            dest = body_groups["Whale tail and flukes"]
        elif obj.get("deform_group")=="head":
            dest = body_groups["Head and expression"]
        else:
            dest = body_groups["Figure"]
        owners[obj] = bool(piece)
        for current in list(obj.users_collection):
            current.objects.unlink(obj)
        dest.objects.link(obj)

    base_materials = {m for obj in meshes if not owners[obj] for m in obj.data.materials}
    isolated = {}
    for obj in outfit.all_objects:
        for i,mat in enumerate(obj.data.materials):
            if mat in base_materials:
                if mat not in isolated:
                    isolated[mat] = mat.copy()
                    isolated[mat].name = "Maid / "+mat.name
                obj.data.materials[i] = isolated[mat]
    # A dry navy twill, pressed ivory cotton and a slightly brighter woven
    # ribbon catch light differently even at desktop scale.
    for obj in outfit.all_objects:
        for mat in obj.data.materials:
            shader = mat.node_tree.nodes.get("Principled BSDF")
            if not shader:
                continue
            if mat.name in ("Indigo skirt cloth","Caelis / garden cream cotton","Maid / Midnight velvet"):
                shader.inputs["Roughness"].default_value = .96
            elif "grosgrain" in mat.name or "woven ribbon" in mat.name:
                shader.inputs["Roughness"].default_value = .69
            elif "Pearl lace" in mat.name:
                shader.inputs["Roughness"].default_value = .91
                shader.inputs["Emission Color"].default_value = (*linear("343141"),1)
                shader.inputs["Emission Strength"].default_value = .40


def paint_fabric(scene):
    """Bake the broad drape and gathered-edge shadows into portable cloth color."""
    def darken(obj, shades):
        mesh = obj.data
        attr = mesh.color_attributes.new(name="Fold shade", type="BYTE_COLOR", domain="CORNER")
        for poly in mesh.polygons:
            for index in poly.loop_indices:
                vertex = mesh.vertices[mesh.loops[index].vertex_index]
                weight, cool = shades(vertex, poly.material_index)
                attr.data[index].color = (1-weight,1-weight*(.90 if cool else .88),
                                          1-weight*(.66 if cool else .72),1)
        mesh.color_attributes.active_color = attr
        for i, material in enumerate(list(mesh.materials)):
            if material.users > 1:
                material = material.copy()
                material.name = obj.name+" cloth "+str(i)
                mesh.materials[i] = material
            tree = material.node_tree
            bs = tree.nodes.get("Principled BSDF")
            paint = tree.nodes.new("ShaderNodeVertexColor")
            paint.layer_name = attr.name
            mix = tree.nodes.new("ShaderNodeMix")
            mix.data_type = "RGBA"
            mix.blend_type = "MULTIPLY"
            mix.inputs[0].default_value = 1
            sockets = {s.identifier:s for s in mix.inputs}
            sockets["A_Color"].default_value = bs.inputs["Base Color"].default_value
            tree.links.new(paint.outputs["Color"],sockets["B_Color"])
            result = next(s for s in mix.outputs if s.identifier=="Result_Color")
            tree.links.new(result,bs.inputs["Base Color"])

    from math import atan2
    def skirt_fold(v, slot):
        x,y,z = v.co
        a = atan2((y-.023)*1.48,x)
        t = max(0,min(1,(.73-z)/.44))
        phase = 10*a+.48*sin(3*a)+.45+.38*(1-t)
        trough = (.5-.5*cos(phase))**3
        if slot:
            return .17*(.5+.5*cos(24*a+.47*sin(3*a)))**2,True
        return .28*trough*(.40+.60*sin(.66*pi*t)),False
    darken(scene.objects["Caelis / sage flared skirt"],skirt_fold)

    def apron_fold(v, slot):
        t = (v.index//65)/28
        u = abs(2*(v.index%65)/64-1)
        # Loose cotton hangs in one shallow U below its two waist ties.
        d = t-(.32-.29*u*u)
        upper = exp(-((d-.032)/.042)**2) * (.53+.47*u)
        side = exp(-((u-(.91-.32*t))/.095)**2)
        return .32*upper + .14*side*sin(pi*t)**.8,True
    darken(scene.objects["Scalloped white apron"],apron_fold)
    def border_fold(v, slot):
        i,j = divmod(v.index,5)
        wave = .5-.5*cos(i*.70+.38*sin(i*.19))
        return .25*wave*(.25+.75*j/4)+.10*(1-j/4),True
    darken(scene.objects["Pleated apron border"],border_fold)
