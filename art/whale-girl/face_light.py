"""Attach a soft fringe shadow and a warm jaw edge to the fitted head."""
from math import cos, pi, sin
from mathutils import Vector
from mathutils.bvhtree import BVHTree


def paint_contours(face, fringe, surface, fabric):
    def shell(objects):
        verts, faces = [], []
        for obj in objects:
            start = len(verts)
            verts.extend(v.co.copy() for v in obj.data.vertices)
            faces.extend([start+i for i in poly.vertices] for poly in obj.data.polygons)
        return BVHTree.FromPolygons(verts, faces)

    skin, bangs = shell([face]), shell(fringe)
    forward = Vector((0,1,0))
    def point(x, z):
        return skin.ray_cast(Vector((x,-1,z)), forward)[0]

    def ink(name, layer, rows, color):
        # Keep only cells that contain pigment. A single vertex-alpha layer
        # survives glTF export without a light rig or a camera-facing decal.
        indices, verts, alphas, quads = {}, [], [], []
        for j in range(1,len(rows)):
            for i in range(1,len(rows[j])):
                cell = ((j-1,i-1),(j-1,i),(j,i),(j,i-1))
                samples = [rows[y][x] for y,x in cell]
                if any(p is None for p in samples) or max(p[1] for p in samples) == 0:
                    continue
                for key, (p, alpha) in zip(cell,samples):
                    if key not in indices:
                        indices[key] = len(verts)
                        verts.append(p)
                        alphas.append(alpha)
                quads.append(tuple(indices[key] for key in cell))
        mat = fabric(name, color)
        mat.surface_render_method = 'BLENDED'
        nodes = mat.node_tree.nodes
        attribute = nodes.new("ShaderNodeVertexColor")
        attribute.layer_name = "Wash"
        bsdf = nodes.get("Principled BSDF")
        bsdf.inputs["Roughness"].default_value = 1
        mat.node_tree.links.new(attribute.outputs["Alpha"],bsdf.inputs["Alpha"])
        # glTF multiplies COLOR_0 by the material base color. Supply the
        # tint in the attribute on both renderers, avoiding double tinting.
        mat.node_tree.links.new(attribute.outputs["Color"],bsdf.inputs["Base Color"])
        rgba = tuple(mat.diffuse_color[:3])
        obj = surface(name,verts,quads,mat,"head",0)
        obj["caelisLayer"] = layer
        attr = obj.data.color_attributes.new(name="Wash",type="FLOAT_COLOR",domain="POINT")
        for datum,alpha in zip(attr.data,alphas):
            datum.color = (*rgba,alpha)

    rows = []
    taps = [(0,0)] + [(.004*cos(i*pi/4),.004*sin(i*pi/4)) for i in range(8)]
    for j in range(73):
        row = []
        z = 1.275+j*.0045
        for i in range(121):
            x = (i-60)*.0045
            p = point(x,z)
            if p is None:
                row.append(None)
                continue
            # Project the actual fringe in the key-light direction. Sample
            # a small area light to soften only the edge of its silhouette.
            hits = 0
            for dx,dz in taps:
                start = p+Vector((dx-.001,-.001,dz+.008))
                if bangs.ray_cast(start,Vector((-.12,-1,.35)),.19)[0] is not None:
                    hits += 1
            row.append((p+Vector((0,-.0012,0)),.32*hits/len(taps)))
        rows.append(row)
    ink("Fringe shadow wash","fringe_shadow",rows,"a09abf")

    # At each transverse section locate the actual underside of the cheek.
    # Fade the line before the temple and across the turn under the chin.
    rows = [[],[],[]]
    for i in range(125):
        t = (i-62)/62
        x = .232*t
        low, high = 1.10, 1.25
        for _ in range(16):
            z = (low+high)*.5
            if point(x,z) is None:
                low = z
            else:
                high = z
        strength = .32*(1-t*t)**1.1
        for row,dz,alpha in zip(rows,(.00035,.00130,.0031),(0,strength,0)):
            p = point(x,high+dz)
            row.append(None if p is None else (p+Vector((0,-.0012,0)),alpha))
    ink("Warm jaw contour","jaw_contour",rows,"956058")

    # Find each cheek in the fitted vertex paint, so the soft marks follow
    # the blush when the jaw or its bind proportions are edited.
    paint = face.data.color_attributes["Color"].data
    for side,label in ((-1,"R"),(1,"L")):
        cheek = [v for v in face.data.vertices if v.co.x*side>.10 and v.co.y<0]
        center = min(cheek,key=lambda v:paint[v.index].color[1]).co
        rows = []
        for j in range(13):
            t = j/12
            row = []
            for stroke in range(3):
                x = center.x+(stroke-1)*.011+(t-.5)*.006
                z = center.z+(t-.5)*(.015-.002*abs(stroke-1))
                for offset,alpha in ((-.0019,0),(0,.17*sin(pi*t)**.7),(.0019,0)):
                    p = point(x+offset,z)
                    row.append(None if p is None else (p+Vector((0,-.0012,0)),alpha))
            rows.append(row)
        ink("Peach cheek marks."+label,"cheek_paint",rows,"e29288")
