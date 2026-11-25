"""Portable cel paint and skinned, backface-culled character outlines."""
from math import atan2, cos, pi, sin
import bpy
from mathutils import Vector
from framing import PANEL_COLUMNS, WISP_COLUMNS


def ramp(lo,hi,value):
    t=max(0,min(1,(value-lo)/(hi-lo)))
    return t*t*(3-2*t)


def finish_cels(scene, fabric, linear):
    colors={s:linear(s) for s in ("1e2a56","3a5aa9","6fc3f5","b8e6f8","849eda",
                                  "475498","585fa8","a394c6","7589c5","31395f")}
    light=Vector((-.48,-.57,.67)).normalized()
    shaders={}
    def painted(mat):
        if mat not in shaders:
            new=fabric("Cel / "+mat.name,"ffffff")
            tree=new.node_tree
            attr=tree.nodes.new("ShaderNodeVertexColor")
            attr.layer_name="Cel"
            tree.links.new(attr.outputs["Color"],tree.nodes.get("Material Output").inputs["Surface"])
            shaders[mat]=new
        return shaders[mat]

    def hair_color(vert,obj):
        x,y,z=vert.co
        diffuse=vert.normal.dot(light)
        if z>1.13:
            c=colors["1e2a56"].lerp(colors["3a5aa9"],.53+.47*(1-ramp(1.13,1.66,z)))
        elif z>.84:
            c=colors["3a5aa9"].lerp(colors["6fc3f5"],1-ramp(.84,1.13,z))
        else:
            c=colors["6fc3f5"].lerp(colors["b8e6f8"],1-ramp(.64,.84,z))
        # Indigo at the crown, violet reflected by the face and cooler blue
        # on the outer locks. The lower lengths retain their cyan tips.
        upper=ramp(.91,1.24,z)
        crown=colors["475498"].lerp(colors["585fa8"],ramp(-.42,.58,diffuse))
        c=c.lerp(crown,upper)
        c.x+=.018*(1-upper)
        c.y+=.015*(1-upper)
        c.z+=.009*(1-upper)
        c*=.75+.32*ramp(-.25,.58,diffuse)
        if "crown lock" in obj.name:
            return colors["1e2a56"].lerp(crown,.35+.54*ramp(-.2,.2,diffuse))
        if obj.name.startswith(("Hair / Parted fringe","Hair / Center fringe","Hair / Fringe wisp")):
            reflected=(1-ramp(1.245,1.365,z))*(1-.30*ramp(.07,.20,abs(x)))
            c=c.lerp(colors["a394c6"],.89*reflected)
        elif obj.name.startswith(("Hair / Cheek","Hair / Temple fringe")):
            c=c.lerp(colors["7589c5"],.73*(1-ramp(1.16,1.44,z)))
        if obj.get("caelisLayer")=="back_hair":
            a=atan2(x,y-.10)
            flow=cos(7.4*a+.75*sin(2*a)+.88*sin(2*pi*(1.67-z)/.83+1.2*a))
            shade=.27*(1-ramp(-.60,.17,flow))
            if "Mantle fold" in obj.name:
                u=abs((vert.index%9)/4-1)
                t=(vert.index//9)/31
                shade+=.33*ramp(.56,.99,u)*ramp(.22,.88,t)
                c=c.lerp(colors["b8e6f8"],.09*(1-ramp(.10,.43,abs(u-.40)))*ramp(.3,.85,t))
            return c.lerp(colors["1e2a56"],shade)
        arc=1.469-.40*x*x+.009*cos(16*x)+.005*cos(53*x)
        band=1-ramp(.007,.021,abs(z-arc))
        if obj.name.startswith(("Hair / Cheek","Hair / Temple fringe","Hair / Parted fringe",
                                "Hair / Center fringe","Hair / Fringe wisp")):
            columns=WISP_COLUMNS if "wisp" in obj.name else PANEL_COLUMNS
            rows=len(obj.data.vertices)//len(columns)
            u=columns[vert.index%len(columns)]
            t=(vert.index//len(columns))/(rows-1)
            edge=min(u,1-u)
            # Roots disappear into the cap. Thin, indigo edge paint follows
            # each lock around the temple and tapers with the actual tip.
            stroke=(1-ramp(.004,.046,edge))*ramp(.12,.43,t)
            c=c.lerp(colors["31395f"],stroke*.81)
            c=c.lerp(colors["1e2a56"],.15*(1-ramp(.025,.18,edge)))
            pieces=ramp(.10,.26,u)*(1-ramp(.74,.94,u))
        else:
            pieces=.20+.48*ramp(.15,.45,abs(x))
        return c.lerp(colors["849eda"],band*pieces*(.48+.36*ramp(-.38,.25,diffuse)))

    for obj in list(scene.objects):
        if obj.type!="MESH" or "deform_group" not in obj:continue
        if obj.get("caelisLayer") in ("acting_ink","acting_iris","face","jaw_contour","cheek_paint","fringe_shadow"):
            continue
        if obj.name in ("Neck","Soft nose shadow") or obj.name.startswith("Face /"):continue
        mesh=obj.data
        mesh.update()
        original=list(mesh.materials)
        if not original:continue
        hair=obj.name.startswith(("Hair /","Hooked crown lock"))
        tail=obj.get("caelisLayer")=="whale_tail"
        counter=mesh.color_attributes.get("Countershade")
        if not (hair or tail):
            # The shared skirt drives base color from the Caelis sage paint.
            # Give this costume its own dyed cloth before joining the exports.
            for i,mat in enumerate(original):
                if mat.name=="Caelis / garden sage cotton":
                    cloth=fabric("Indigo skirt cloth","303963")
                    bsdf=cloth.node_tree.nodes.get("Principled BSDF")
                    bsdf.inputs["Emission Color"].default_value=(*linear("262b48"),1)
                    bsdf.inputs["Emission Strength"].default_value=.45
                    mesh.materials[i]=cloth
            for color in list(mesh.color_attributes):
                mesh.color_attributes.remove(color)
            original=list(mesh.materials)
            for mat in original:
                shader=mat.node_tree.nodes.get("Principled BSDF")
                if shader:
                    shader.inputs["Roughness"].default_value=.88
                    shader.inputs["Specular IOR Level"].default_value=.17
            continue
        attr=mesh.color_attributes.new(name="Cel",type="BYTE_COLOR",domain="CORNER")
        for poly in mesh.polygons:
            for index in poly.loop_indices:
                vert=mesh.vertices[mesh.loops[index].vertex_index]
                diff=vert.normal.dot(light)
                band=.16+.45*ramp(.04,.35,diff)+.39*ramp(.48,.81,diff)
                if hair:
                    tone=hair_color(vert,obj)
                else:
                    tone=Vector(counter.data[vert.index].color[:3])*(.67+.33*band)
                attr.data[index].color=(*tone,1)
        for other in list(mesh.color_attributes):
            if other.name!="Cel":
                mesh.color_attributes.remove(other)
        mesh.color_attributes.active_color=mesh.color_attributes["Cel"]
        mesh.color_attributes.render_color_index=0
        for i,mat in enumerate(original):
            mesh.materials[i]=painted(mat)

    def ink(name,tint):
        mat=fabric(name,tint)
        mat.use_backface_culling=True
        tree=mat.node_tree
        rgb=tree.nodes.new("ShaderNodeRGB")
        rgb.outputs[0].default_value=(*linear(tint),1)
        tree.links.new(rgb.outputs[0],tree.nodes.get("Material Output").inputs["Surface"])
        # Cycles traces both sides of a polygon. Explicitly cut out its back
        # there; the GLB and the viewport use the single-sided material flag.
        out=tree.nodes.new("ShaderNodeOutputMaterial")
        out.target="CYCLES"
        mix=tree.nodes.new("ShaderNodeMixShader")
        tree.links.new(tree.nodes.new("ShaderNodeNewGeometry").outputs["Backfacing"],mix.inputs[0])
        tree.links.new(rgb.outputs[0],mix.inputs[1])
        tree.links.new(tree.nodes.new("ShaderNodeBsdfTransparent").outputs[0],mix.inputs[2])
        tree.links.new(mix.outputs[0],out.inputs["Surface"])
        return mat

    marine=ink("Marine contour","263252")
    warm=ink("Warm skin contour","b27e79")
    for obj in list(scene.objects):
        if obj.type!="MESH":continue
        name=obj.name
        if name.startswith("Caelis arm and hand"):
            thickness,mat=.0008,warm
        elif name.startswith(("Caelis / ivory sock","Mary Jane heel")):
            thickness,mat=.0023,marine
        elif name.startswith(("Whale tail","Paired whale flukes","Gathered sleeve","Scalloped head fin",
                              "Hooked crown lock","Hair / Collar wave","Hair / Lateral wave")):
            thickness,mat=.0034,marine
        else:
            continue
        # A reversed shell shares the original topology, subdivision and
        # vertex groups. Only its far-facing silhouette survives culling.
        hull=obj.copy()
        hull.data=obj.data.copy()
        scene.collection.objects.link(hull)
        hull.name="Contour / "+name
        hull["caelisLayer"]="outline_skin" if mat==warm else "outline_cloth"
        hull.data.materials.clear()
        hull.data.materials.append(mat)
        for attr in list(hull.data.color_attributes):hull.data.color_attributes.remove(attr)
        hull.data.update()
        directions=[v.normal.copy() for v in hull.data.vertices]
        for vertex,n in zip(hull.data.vertices,directions):
            vertex.co+=thickness*n
        hull.data.flip_normals()
        hull.data.update()
