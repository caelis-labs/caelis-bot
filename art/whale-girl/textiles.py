"""Dye and finish the bound maid dress in its sewn rest shape."""
from math import atan2, cos, exp, pi, sin
import bpy
from mathutils import Vector
from mathutils.bvhtree import BVHTree
from mathutils.geometry import barycentric_transform, tessellate_polygon


def cotton_shadow(strength):
    return (1-.64*strength, 1-.76*strength, 1-.43*strength, 1)


def cloth_paint(obj, yarns, color_at):
    """Keep base dye and fold paint on this pattern piece through glTF export."""
    mesh = obj.data
    attr = mesh.color_attributes.get("Fold shade")
    if attr is None:
        attr = mesh.color_attributes.new(name="Fold shade", type="BYTE_COLOR", domain="CORNER")
    mesh.color_attributes.active_color = attr
    for slot, dye in yarns.items():
        mesh.materials[slot] = dye
    for face in mesh.polygons:
        for loop in face.loop_indices:
            v = mesh.vertices[mesh.loops[loop].vertex_index]
            attr.data[loop].color = color_at(v, face.material_index)


def fit_apron(scene):
    panel = scene.objects["Scalloped white apron"]
    border = scene.objects["Pleated apron border"]
    skirt = scene.objects["Caelis / sage flared skirt"].data
    twill = BVHTree.FromPolygons([v.co for v in skirt.vertices],
                                [p.vertices[:] for p in skirt.polygons if p.material_index == 0])
    # Bring up the U hem without pulling the cotton away from the navy skirt.
    # Its sewn seam and the adjacent gathering share one displacement.
    bodice = scene.objects["Caelis / connected blouse and sleeves"].data
    bodice_surface = BVHTree.FromPolygons([v.co for v in bodice.vertices],[p.vertices[:] for p in bodice.polygons])
    def adjust(p, t, u):
        old = twill.ray_cast((p.x,-1,p.z),(0,1,0))[0]
        p.x *= 1-.055*t
        p.z += t**1.26*(.052-.008*u*u)
        new = twill.ray_cast((p.x,-1,p.z),(0,1,0))[0]
        if old is not None and new is not None:
            p.y += new.y-old.y
        p.y -= .0045*sin(pi*t)*cos(2.8*pi*u+.35*t)
        if t < .28:
            lining = bodice_surface.ray_cast((p.x,-1,p.z),(0,1,0))[0]
            if lining is not None:
                p.y = min(p.y,lining.y-.004)

    rows, cols = 29, 65
    for v in panel.data.vertices:
        r,c = divmod(v.index,cols)
        adjust(v.co, r/(rows-1), 2*c/(cols-1)-1)
    indices = ([r*cols for r in range(rows)] +
               list(range((rows-1)*cols+1,rows*cols)) +
               [r*cols+cols-1 for r in reversed(range(rows-1))])
    for i,idx in enumerate(indices):
        base = panel.data.vertices[idx].co
        before = panel.data.vertices[indices[max(i-1,0)]].co
        after = panel.data.vertices[indices[min(i+1,len(indices)-1)]].co
        tangent = (after-before).normalized()
        outward = Vector((tangent.z,0,-tangent.x)).normalized()
        t = (idx//cols)/(rows-1)
        gather = cos(i*.83+.43*sin(i*.16))
        for j in range(5):
            s = j/4
            width = (.021+.012*sin(pi*t/2))*(1+.19*gather)
            p = base + outward*(s*width)
            sample = twill.ray_cast((p.x,-1,p.z),(0,1,0))[0]
            # The trim folds toward the viewer. Follow the support surface at
            # its free edge as well as at the inner seam.
            if sample is not None:
                p.y = min(p.y, sample.y-.008)
            p.y -= .0025+.005*sin(pi*s)+(.005+.005*gather)*s
            border.data.vertices[i*5+j].co = p
    panel.data.update()
    border.data.update()
    return panel, border


class SewnPattern:
    """Project a trimmed pattern onto a finished panel and interpolate its skin weights."""
    def __init__(self, support):
        self.support = support
        mesh = support.data
        mesh.calc_loop_triangles()
        self.tris = [tuple(t.vertices) for t in mesh.loop_triangles
                     if support.name != "Caelis / sage flared skirt" or t.material_index == 0]
        self.tree = BVHTree.FromPolygons([v.co for v in mesh.vertices],self.tris,all_triangles=True)
        self.verts, self.weights, self.faces, self.slots = [], [], [], []

    def vertex(self,x,z,lift):
        point,_,tri,_ = self.tree.ray_cast((x,-1,z),(0,1,0))
        if point is None:
            raise ValueError(f"Embroidery outside {self.support.name} at {(x,z)}")
        vs = [self.support.data.vertices[i] for i in self.tris[tri]]
        bary = barycentric_transform(point,*(v.co for v in vs),Vector((1,0,0)),Vector((0,1,0)),Vector((0,0,1)))
        weights = {}
        for v,t in zip(vs,bary):
            for group in v.groups:
                name = self.support.vertex_groups[group.group].name
                weights[name] = weights.get(name,0)+max(0.,t)*group.weight
        point.y -= lift
        self.verts.append(point)
        self.weights.append(weights)
        return len(self.verts)-1

    def face(self,indices,slot):
        a,b,c = (self.verts[i] for i in indices[:3])
        self.faces.append(indices[::-1] if (b-a).cross(c-a).y>0 else indices)
        self.slots.append(slot)

    def sew(self,obj,materials):
        old = obj.data
        mesh = bpy.data.meshes.new(obj.name+" / fitted stitches")
        mesh.from_pydata(self.verts,[],self.faces)
        for mat in materials: mesh.materials.append(mat)
        for f,slot in zip(mesh.polygons,self.slots): f.material_index = slot
        obj.data = mesh
        bpy.data.meshes.remove(old)
        obj.vertex_groups.clear()
        for i,weights in enumerate(self.weights):
            top = sorted(weights.items(),key=lambda p:-p[1])[:4]
            total = sum(w for _,w in top)
            for name,w in top:
                group = obj.vertex_groups.get(name) or obj.vertex_groups.new(name=name)
                group.add([i],w/total,"REPLACE")
        mesh.update()


def embroider_whale(scene, panel, bright):
    obj = scene.objects["Embroidered whale"]
    pattern = SewnPattern(panel)
    def patch(path,slot=0):
        poly = [Vector((x,y,0)) for x,y in path]
        for a,b,c in tessellate_polygon([poly]):
            previous = []
            for i in range(5):
                row = []
                for j in range(5-i):
                    p = poly[a]+(poly[b]-poly[a])*j/4+(poly[c]-poly[a])*i/4
                    row.append(pattern.vertex(.105+.036*p.x,.455+.033*p.y,.004+.0012*slot))
                if previous:
                    for k,v in enumerate(row):
                        pattern.face((previous[k],previous[k+1],v),slot)
                        if k: pattern.face((previous[k],v,row[k-1]),slot)
                previous = row
    # A domed forehead, a short upturned fluke and a rounded belly follow
    # the tiny mark at the lower right of the original apron.
    patch([(-.94,-.18),(-.91,.09),(-.80,.32),(-.55,.47),(-.25,.49),
           (.02,.41),(.23,.25),(.40,.11),(.60,.12),(.75,.24),(.77,.40),
           (.67,.52),(.63,.66),(.78,.65),(.90,.56),(.98,.71),(1.08,.75),
           (1.08,.57),(1.00,.44),(.90,.37),(.86,.16),(.77,-.06),
           (.58,-.25),(.34,-.39),(.01,-.47),(-.38,-.48),(-.68,-.42),(-.85,-.32)])
    patch([(-.42,.57),(-.55,.76),(-.70,.77),(-.73,.67),(-.62,.58)])
    patch([(-.38,.59),(-.39,.82),(-.31,.95),(-.21,.91),(-.23,.75)])
    patch([(-.66+.066*cos(2*pi*i/16),-.02+.075*sin(2*pi*i/16)) for i in range(16)],1)
    pattern.sew(obj,[obj.data.materials[0],bright])


def hem_bow(obj,skirt,sign,satin,gold):
    pattern = SewnPattern(skirt)
    def pt(x,z,lift):
        return pattern.vertex(sign*(.280+x),.405+z+.27*x,lift)
    for hand in (-1,1):
        for tail in (True,False):
            prev=[]
            for i in range(13):
                t=i/12
                row=[]
                for u in (-1,-.68,-.35,0,.35,.68,1):
                    if tail:
                        x=hand*(.003+.022*t)+u*(.003+.005*t)
                        z=-.002-.039*t+.003*u*t+.003*(1-abs(u))*t**8
                        lift=.006+.003*sin(pi*t)
                    else:
                        x=hand*(.003+.037*t-.004*u*u*t)
                        z=.012*t+u*(.004+.015*sin(pi*t*.60))
                        lift=.008+.007*sin(pi*t)*(1-.55*u*u)
                    row.append(pt(x,z,lift))
                if prev:
                    for j in range(6):
                        edge=j in (0,5) or (not tail and i==12)
                        pattern.face((prev[j],prev[j+1],row[j+1],row[j]),int(edge))
                prev=row
    ids=[pt(x,z,.0125) for x,z in [(-.0045,-.006),(.0045,-.006),(.005,.006),(-.005,.006)]]
    pattern.face(tuple(ids),1)
    pattern.sew(obj,[satin,gold])


def seat_hem_stitches(scene,skirt):
    obj = scene.objects["Whale hem anchors"]
    cloth = BVHTree.FromPolygons([v.co for v in skirt.data.vertices],
                                 [p.vertices[:] for p in skirt.data.polygons if p.material_index==0])
    vertices = obj.data.vertices
    count = len(vertices)//4
    for start in range(0,len(vertices),count):
        group = vertices[start:start+count]
        center = sum((v.co for v in group),Vector())/count
        for v in group:
            p = center+(v.co-center)*.55
            p.x += .013 if center.x>0 else -.013
            p.z -= .010
            direction = Vector((p.x,p.y-.023,0)).normalized()
            hit = cloth.ray_cast(p+direction,-direction)[0]
            if hit is not None:
                v.co = hit+direction*.0035
    obj.data.update()
    for name,allowance in (("Whale hem fine piping",.0018),("Whale hem lower piping",.0018),
                           ("Whale hem embroidered wave",.0024),("Whale hem embroidered stars",.0028)):
        trim=scene.objects[name]
        for v in trim.data.vertices:
            direction=Vector((v.co.x,v.co.y-.023,0)).normalized()
            hit=cloth.ray_cast(v.co+direction,-direction)[0]
            if hit is not None:
                v.co=hit+direction*allowance
        trim.data.update()


def finish_textiles(scene, fabric, linear):
    def dyed(name,color,roughness=.94,painted=True):
        mat = bpy.data.materials.get(name) or fabric(name,color)
        tree = mat.node_tree
        bs = tree.nodes.get("Principled BSDF")
        base = (*linear(color),1)
        # Keep the exported skirt material identity used by the palette check.
        # This replaces the old fold multiplier when re-dyeing the twill.
        for node in list(tree.nodes):
            if node.type not in ("BSDF_PRINCIPLED","OUTPUT_MATERIAL"):
                tree.nodes.remove(node)
        mat.diffuse_color = base
        bs.inputs["Base Color"].default_value = base
        bs.inputs["Emission Strength"].default_value = 0
        bs.inputs["Emission Color"].default_value = (0,0,0,1)
        bs.inputs["Roughness"].default_value = roughness
        bs.inputs["Specular IOR Level"].default_value = .15
        if painted:
            attr = tree.nodes.new("ShaderNodeVertexColor")
            attr.layer_name = "Fold shade"
            mix = tree.nodes.new("ShaderNodeMix")
            mix.data_type = "RGBA"
            mix.blend_type = "MULTIPLY"
            mix.inputs[0].default_value = 1
            sockets = {s.identifier:s for s in mix.inputs}
            sockets["A_Color"].default_value = base
            tree.links.new(attr.outputs["Color"],sockets["B_Color"])
            tree.links.new(next(s for s in mix.outputs if s.identifier=="Result_Color"),
                           bs.inputs["Base Color"])
        return mat

    twill = dyed("Indigo skirt cloth","3a4278")
    velvet = dyed("Maid / navy sleeve velvet","333962")
    cotton = dyed("Maid / warm pearl cotton","f7f1f3")
    lace = dyed("Maid / gathered ivory edging","fffafa")
    satin = dyed("Maid / blue satin hem knots","252c4e",.84,False)
    gold = dyed("Maid / champagne silk binding","bfa481",.78,False)
    bright = dyed("Maid / ivory embroidery thread","f8f3ed",.93,False)
    from tailoring import cuff_button, hang_hem, shoulder_ruffle, tie_bows
    tie_bows(scene, cotton, dyed("Maid / cool ribbon lining","c9c2d4"),
             dyed("Maid / indigo neck ribbon","333c68",.86),
             dyed("Maid / neck ribbon lining","232845",.93))
    panel,border = fit_apron(scene)
    embroider_whale(scene,panel,bright)

    def apron_fold(v,_):
        row,col = divmod(v.index,65)
        t,u = row/28, abs(col/32-1)
        sag = t-(.30-.28*u*u)
        drape = .33*exp(-((sag-.019)/.055)**2)*(.6+.4*u)
        side = .42*exp(-((u-(.90-.39*t))/.10)**2)*sin(pi*t)**.72
        return cotton_shadow(drape+side+.08*u**6+.30*exp(-((t-.065)/.022)**2))
    cloth_paint(panel,{0:cotton},apron_fold)

    def flounce(v,_):
        i,j = divmod(v.index,5)
        wave = (.5-.5*cos(i*.83+.43*sin(i*.16)))**1.6
        return cotton_shadow(.45*wave*(.34+.66*j/4)+.23*(1-j/4)**3)
    cloth_paint(border,{0:lace},flounce)

    def skirt_fold(v,slot):
        x,y,z = v.co
        a = atan2((y-.023)*1.45,x)
        t = max(0,min(1,(.75-z)/.48))
        if slot:
            return cotton_shadow(.45*(.5+.5*cos(24*a+.47*sin(3*a)))**2)
        flute = 8*(a+pi/2)+.39*sin(3*a+.36)+.44*(1-t)
        pleat = (.5-.5*cos(flute))**3
        value = .78*pleat*(.44+.56*sin(.64*pi*t))+.06*(1-t)
        return (1-value,1-value*.88,1-value*.63,1)
    skirt = scene.objects["Caelis / sage flared skirt"]
    cloth_paint(skirt,{0:twill,1:lace},skirt_fold)

    def bib_tucks(v,_):
        row,col = divmod(v.index,49)
        t,u = row/24, abs(col/24-1)
        tuck = sum(exp(-((u-center)/.057)**2) for center in (.43,.69))
        return cotton_shadow(.44*tuck*(.47+.53*sin(pi*t))+.14*u**6)
    cloth_paint(scene.objects["Pintucked shirt bib"],{0:cotton},bib_tucks)

    for side in ("L","R"):
        shoulder_ruffle(scene.objects["Gathered sleeve and lace."+side],scene.objects["Caelis / connected blouse and sleeves"],
                        scene.objects["Pinafore shoulder frill."+side],1 if side=="L" else -1,lace)
        def sleeve(v,slot):
            i,j = divmod(v.index,44)
            gather = (.5-.5*cos(2*pi*7*j/44+.32*sin(2*pi*3*j/44)+.022*i))**2
            if slot:
                return cotton_shadow(.14+.42*gather)
            depth = .05+.53*gather*(.16+.84*exp(-((i-9)/3.0)**2))
            return (1-depth,1-depth*.91,1-depth*.63,1)
        cloth_paint(scene.objects["Gathered sleeve and lace."+side],{0:velvet,1:lace},sleeve)

        cuff_button(scene.objects["Gathered sleeve and lace."+side],1 if side=="L" else -1,satin,bright)

    # Turn the bodice under the sleeve cap and flatten its lower edge into
    # the waist seam. The two cloth shells move with their existing weights.
    bodice = scene.objects["Caelis / connected blouse and sleeves"]
    for v in bodice.data.vertices:
        x,y,z = v.co
        cap = max(0,min(1,(abs(x)-.197)/.043))*max(0,min(1,(z-.842)/.051))
        v.co.y = .018+(y-.018)*(1-.23*cap)
        v.co.x *= 1-.045*cap
    bodice.data.update()
    bodice.data.materials[0]=dyed("Maid / indigo bodice","343d66",.94,False)
    seat_hem_stitches(scene,skirt)
    belt = scene.objects["Velvet waist panel"]
    support = BVHTree.FromPolygons([v.co for v in bodice.data.vertices],[p.vertices[:] for p in bodice.data.polygons])
    for v in belt.data.vertices:
        v.co.x *= .91
        hit=support.ray_cast((v.co.x,-1,v.co.z),(0,1,0))[0]
        if hit is not None:
            v.co.y=hit.y-.006
    belt.data.update()
    belt.data.materials[0]=dyed("Maid / bound navy waistband","2f3557",.91,False)

    for obj in scene.objects:
        if obj.name.startswith(("Whale hem embroidered","Whale hem fine piping","Whale hem lower piping","Whale hem anchors")):
            obj.data.materials[0] = gold
    hang_hem(scene)
    for side,sign in (("L",1),("R",-1)):
        hem_bow(scene.objects["Whale hem satin bow."+side],skirt,sign,satin,gold)
