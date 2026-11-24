"""Cut the Whale Girl ribbon, gathered puff sleeve and flared skirt patterns."""
from bisect import bisect_right
from math import atan2, cos, exp, hypot, pi, sin
import bpy
from mathutils import Vector
from dress import SLEEVE_SECTIONS


def cut_sleeve(sleeve):
    """Gather a short puff directly into a turned cuff on the fitted arm."""
    n = SLEEVE_SECTIONS
    vertices = sleeve.data.vertices
    rings = [[v.co.copy() for v in vertices[i:i+n]] for i in range(0, len(vertices), n)]
    centers = [sum(row, Vector()) / n for row in rings]
    # Station on the bound arm, pattern radius, amount taken up by gathers.
    cut = [(0,0,0), (1,0,0), (2,0,0), (3,.059,.015), (4,.062,.020),
           (4.9,.067,.027), (5.65,.072,.032), (6.40,.076,.055), (7.00,.079,.094),
           (7.40,.076,.13), (7.71,.063,.15), (7.88,.051,.055), (7.98,.054,.046),
           (8.16,.062,.095), (8.38,.061,.086), (8.54,.052,.040), (8.70,.052,.012),
           (9.06,.053,.012), (9.53,.058,.032), (9.63,.060,.046), (9.64,.053,.033), (9.49,.048,.018)]
    along = (centers[9]-centers[3]).normalized()
    base = rings[3][0]-centers[3]
    across = (base-along*along.dot(base)).normalized()
    around = along.cross(across)
    if around.dot(rings[3][1]-rings[3][0]) < 0:
        around.negate()
    for i,(station,radius,ease) in enumerate(cut):
        if i < 3:
            continue
        k = int(station)
        t = station-k
        center = centers[k].lerp(centers[k+1], t)
        axis = along
        for j in range(n):
            a = 2*pi*j/n
            # Preserve the angular correspondence to the shoulder seam and the handed skin.
            spoke = across*cos(a)+around*sin(a)
            wave = cos(7*a + .32*sin(3*a) + .45*station/9)
            pinch = exp(-((station-7.25)/.65)**2) * max(0,-wave)**2
            p = center + spoke*(radius*(1+ease*wave)-.005*pinch)*(1-.16*spoke.y**2)
            p += along*(.006*pinch)
            if i < 5:
                p = rings[k][j].lerp(rings[k+1][j],t).lerp(p,(i-1)/3)
            if i >= 17:
                p += axis*(.002*wave*min(1,(i-16)/2))
            vertices[i*n+j].co = p
    for f in sleeve.data.polygons:
        row = f.index//n + 1
        f.material_index = int(row >= 12)
    sleeve.data.update()


def shoulder_ruffle(sleeve, bodice, strap, sign, lace):
    """Carry the apron strap over the cap into the back waist tie."""
    from mathutils.bvhtree import BVHTree
    from mathutils.geometry import barycentric_transform
    from textiles import cloth_paint, cotton_shadow
    supports = []
    for obj in (bodice,sleeve):
        mesh=obj.data
        mesh.calc_loop_triangles()
        tris=[tuple(tri.vertices) for tri in mesh.loop_triangles]
        supports.append((obj,tris,BVHTree.FromPolygons([v.co for v in mesh.vertices],tris,all_triangles=True)))
    verts,faces,colors,weights=[],[],[],[]
    rows,cols=105,9
    for i in range(rows):
        t=i/(rows-1)
        arch=sin(pi*t)
        direction=Vector((0,-cos(pi*t),arch**2)).normalized()
        inner=.061+.060*arch+.015*t
        wave=cos(30*pi*t+.44*sin(4*pi*t))
        for j in range(cols):
            u=j/(cols-1)
            width=.030+.039*arch**.75
            x=sign*(inner+width*u+.006*wave*u**2)
            origin=Vector((x,.015,.748+.106*arch)) + direction
            # The free edge can extend beyond the compact cap. Its seam
            # still carries its weight when the last few millimetres turn away.
            hits=[]
            for inset in (0,.006,.012,.024,.036):
                ray = origin-Vector((sign*inset,0,0))
                for obj,triangles,tree in supports:
                    pt,_,idx,dist=tree.ray_cast(ray,-direction)
                    if pt is not None:
                        hits.append((dist,pt,obj,triangles[idx]))
                if hits:
                    break
            if not hits:
                raise ValueError(f"Shoulder strap misses its seam at {t:.3f}, {u:.3f}")
            _,pt,obj,tri=min(hits,key=lambda h:h[0])
            corners=[obj.data.vertices[idx] for idx in tri]
            bary=barycentric_transform(pt,*(v.co for v in corners),
                                       Vector((1,0,0)),Vector((0,1,0)),Vector((0,0,1)))
            blend={}
            for v,factor in zip(corners,bary):
                for group in v.groups:
                    name=obj.vertex_groups[group.group].name
                    blend[name]=blend.get(name,0)+group.weight*max(0,factor)
            weights.append(blend)
            free=max(0,(u-.30)/.70)
            lift=.004+.004*sin(pi*u)+free*(.011+.007*wave)
            verts.append(pt+Vector((sign*inset,0,0))+direction*(lift-.25*inset))
            valley=.12*exp(-((u-.28)/.11)**2)+(.27+.29*arch)*free**.8*(.5-.5*wave)**1.3
            colors.append(cotton_shadow(valley))
            if i and j:
                k=len(verts)-1
                face=(k-cols-1,k-cols,k,k-1)
                faces.append(face if sign>0 else face[::-1])
    mesh=bpy.data.meshes.new(strap.name+' / gathered over shoulder')
    mesh.from_pydata(verts,[],faces)
    mesh.materials.append(lace)
    old=strap.data
    strap.data=mesh
    bpy.data.meshes.remove(old)
    for f in mesh.polygons:f.use_smooth=True
    ids={}
    for i,influences in enumerate(weights):
        top=sorted(influences.items(),key=lambda pair:-pair[1])[:4]
        total=sum(w for _,w in top)
        for name,w in top:
            if name not in ids:ids[name]=strap.vertex_groups.new(name=name)
            ids[name].add([i],w/total,'REPLACE')
    mesh.update()
    cloth_paint(strap,{},lambda v,slot:colors[v.index])


def cuff_button(sleeve, sign, enamel, thread):
    """Sew a small domed button through the visible face of the cuff band."""
    n = SLEEVE_SECTIONS
    row = sleeve.data.vertices[17*n:18*n]
    center = sum((v.co for v in row),Vector())/n
    front = Vector((sign*.52,-.85,0))
    idx = max(range(n),key=lambda j:(row[j].co-center).normalized().dot(front))
    anchor = row[idx]
    normal = (anchor.co-center).normalized()
    tangent = (row[(idx+1)%n].co-row[(idx-1)%n].co).normalized()
    across = normal.cross(tangent).normalized()
    tangent = across.cross(normal)
    verts,faces,slots = [],[],[]
    for r,h in [(.0085,.0008),(.009,.0024),(.0078,.0049),(.0053,.0064)]:
        for j in range(20):
            a=2*pi*j/20
            verts.append(anchor.co+normal*h + r*(tangent*cos(a)+across*sin(a)))
            k=len(verts)-1
            if k>=20:
                end=k-j+(j+1)%20
                faces.append((k-20,end-20,end,k))
                slots.append(0)
    faces.append(tuple(range(60,80)))
    slots.append(0)
    for off in (-.002,.002):
        ids=[]
        for j in range(10):
            a=2*pi*j/10
            ids.append(len(verts))
            verts.append(anchor.co+normal*.0066+tangent*(off+.0013*cos(a))+across*.0018*sin(a))
        faces.append(tuple(ids));slots.append(1)
    mesh=bpy.data.meshes.new('Pearl cuff button')
    mesh.from_pydata(verts,[],faces)
    for mat in (enamel,thread):mesh.materials.append(mat)
    for face,slot in zip(mesh.polygons,slots):
        face.material_index=slot
        face.use_smooth=True
    obj=sleeve.copy()
    obj.data=mesh
    obj.name='Pearl cuff button.'+sleeve.name[-1]
    sleeve.users_collection[0].objects.link(obj)
    for mod in list(obj.modifiers):
        if mod.type!='ARMATURE':obj.modifiers.remove(mod)
    for g in anchor.groups:
        group = obj.vertex_groups.new(name=sleeve.vertex_groups[g.group].name)
        group.add(list(range(len(verts))),g.weight,'REPLACE')
    mesh.update()


def ribbon_piece(obj, origin, sign, back, tail, dye, lining):
    """A turned ribbon has a continuous outside, inside and open loop at the selvedge."""
    from textiles import cloth_paint, cotton_shadow
    verts, faces, slots, shades = [], [], [], []
    rows = 36 if tail else 56
    cols = 13
    depth = 1 if back else -1
    width = .171 if back else .071
    for i in range(rows + 1):
        t = i/rows
        for j in range(cols):
            u = 2*j/(cols-1)-1
            if tail:
                span = (.017+.036*t**.58) if back else (.007+.009*t**.60)
                across = (.011+.139*t+.025*sin(pi*t)) if back else (.008+.030*t)
                drop = .212 if back else .084
                x = sign*(across+span*u)
                z = -drop*t+(.014 if back else .010)*(1-abs(u))*t**9
                z += (.024 if back else .005)*u*t
                lift = (.130*t + .031*sin(pi*t) if back else .026*t-.005*sin(pi*t))
                lift += (.013 if back else .004)*(1-u*u)*sin(pi*t)
                lift += (.012 if back else .003)*u*cos(pi*t)
                shade = .12+.36*exp(-((u+.43-.31*t)/.22)**2)*sin(pi*t)**.6
                slot = 0
            else:
                # Follow a droplet-shaped closed path from the knot to the broad
                # turned end and back. The near and return layers share that turn.
                a = 2*pi*t
                reach = (1-cos(a))*.5
                h = (.014+.044*reach**.63) if back else (.007+.020*reach**.64)
                x = sign*(.002+width*reach-(.011 if back else .006)*abs(u)**3*reach**3)
                z = (.026 if back else .010)*reach + u*h
                z += (.008 if back else .003)*sin(a+.4)*reach
                fold = cos(pi*(u+.12)) - .55*cos(2*pi*(u-.08))
                lift = (.034 if back else .015)*sin(a) + (.034 if back else .008)*reach
                lift += (.008 if back else .003)*fold*sin(pi*reach)
                # Let the upper and lower lips expose a narrow view into the loop.
                lift += (.018 if back else .007)*u*reach
                z += (.010 if back else .004)*sin(a)
                shade = .06+.68*exp(-((u-.23+.21*reach)/(.18+.13*reach))**2)*(1-reach*.82)
                shade += .32*abs(u)**9*(.4+.6*reach)
                slot = int(t>.5)
            verts.append(origin + Vector((x,depth*lift,z)))
            shades.append(cotton_shadow(shade) if back else (1-.72*shade,1-.76*shade,1-.57*shade,1))
            if i and j:
                k = len(verts)-1
                face = (k-cols-1,k-cols,k,k-1)
                faces.append(face)
                slots.append(slot)
    old = obj.data
    mesh = bpy.data.meshes.new(obj.name+' / turned grosgrain')
    mesh.from_pydata(verts, [], faces)
    for mat in (dye,lining):mesh.materials.append(mat)
    obj.data = mesh
    bpy.data.meshes.remove(old)
    for f,slot in zip(mesh.polygons,slots):
        f.material_index = slot
        f.use_smooth = True
    mesh.update()
    # Orient the visible outward pass. Both loops retain the reverse face inside.
    face = mesh.polygons[(cols-1)*(rows//4)]
    if face.normal.y*depth < 0:
        mesh.flip_normals()
    obj.vertex_groups.clear()
    obj.vertex_groups.new(name='pelvis' if back else 'chest').add(list(range(len(verts))),1,'REPLACE')
    for mod in list(obj.modifiers):
        if mod.type in ('SUBSURF','SOLIDIFY'):obj.modifiers.remove(mod)
    hem = obj.modifiers.new('Bound ribbon edge','SOLIDIFY')
    hem.thickness = .0016 if back else .0011
    cloth_paint(obj,{},lambda v,slot:shades[v.index])


def tie_bows(scene, cotton, pearl_reverse, navy, navy_reverse):
    for back,center_name in [(False,'Knotted collar ribbon'),(True,'Back waist knot')]:
        knot = scene.objects[center_name]
        points = knot.data.vertices
        center = sum((v.co for v in points),Vector())/len(points)
        origin = Vector((0,.176,.723)) if back else Vector((0,-.110,.937))
        for v in points:
            d = v.co-center
            v.co = origin+Vector((d.x*(.60 if back else .68), d.y*.68, d.z*(.69 if back else 1.08)))
            v.co.y += .012 if back else -.008
        knot.data.update()
        for side,sign in [('L',1),('R',-1)]:
            names = ('Back waist bow.','Back sash.') if back else ('Collar bow loop.','Collar bow end.')
            for prefix,tail in zip(names,(False,True)):
                ribbon_piece(scene.objects[prefix+side],origin,sign,back,tail,
                             cotton if back else navy,pearl_reverse if back else navy_reverse)


def skirt_sections(skirt, lining_start):
    """Sample the navy shell before draping, including its uneven hem height."""
    width = skirt.data["petticoatRingSize"] // 4
    rings = []
    for start in range(0, lining_start, width):
        ring = []
        for v in skirt.data.vertices[start:start+width]:
            x,y,z = v.co
            y = (y-.024)*1.62
            ring.append((atan2(y,x), hypot(x,y), z))
        ring.sort()
        # Interpolate across the closing seam just like any other edge.
        a,r,z = ring[-1]
        first = (a-2*pi,r,z)
        a,r,z = ring[0]
        ring = [first,*ring,(a+2*pi,r,z)]
        rings.append(([p[0] for p in ring],ring))
    return rings


def meridian(rings, angle):
    sections = []
    for angles, ring in rings:
        i = bisect_right(angles,angle)-1
        a,r,z = ring[i]
        b,s,h = ring[i+1]
        u = (angle-a)/(b-a)
        sections.append((z+(h-z)*u,r+(s-r)*u))
    return sections


def side_drape(sections, z, ridge):
    top,waist = sections[0]
    bottom,hem = sections[-1]
    h = min(top,max(bottom,z))
    source = hem
    for (upper,r),(lower,s) in zip(sections,sections[1:]):
        if lower <= h <= upper:
            source = r+(s-r)*(upper-h)/(upper-lower)
            break
    fall = (top-h)/(top-bottom)
    # Give the side seam a continuous A-line. A little ease at the hip
    # and shallow pleats survive without a sudden kick in the last two rows.
    target = waist + (.423-waist)*fall + .008*sin(pi*fall)
    target += .009*(ridge-.5)*fall
    return target-source


def turn_petticoat(skirt, start, count):
    """Hang the gathered lining directly from the finished, non-circular hem."""
    mesh = skirt.data
    # Each quarter-edge sample is sewn to exactly one vertex of the navy rim.
    rim = [None]*(count//4)
    for edge in mesh.edges:
        lo,hi = sorted(edge.vertices)
        j = hi-start
        if lo < start and 0 <= j < count and j%4 == 1:
            rim[j//4] = mesh.vertices[lo].co.copy()
    if any(p is None for p in rim):
        raise ValueError("Incomplete seam between skirt and petticoat")
    turns = [(-.002,.001,.005),(.000,.003,.014),(.006,.007,.028),
             (.008,.011,.044),(.007,.010,.048),(.005,.010,.050)]
    for j in range(count):
        i,u = j//4,(j%4)/4
        p,q,r,s = (rim[(i+k)%len(rim)] for k in (-1,0,1,2))
        seam = (2*q+(r-p)*u+(2*p-5*q+4*r-s)*u*u+(3*q-p-3*r+s)*u**3)*.5
        a = atan2((seam.y-.024)*1.62,seam.x)
        flute = cos(24*a+.46*sin(3*a))
        outward = Vector((cos(a),sin(a)/1.62,0))
        for row,(spread,gather,drop) in enumerate(turns):
            v = mesh.vertices[start+row*count+j]
            v.co = seam + outward*(spread+gather*flute)
            v.co.z -= drop - .55*gather*flute
    mesh.update()


def hang_hem(scene):
    """Give the overskirt, its trim and its attached lining one common drape."""
    skirt = scene.objects['Caelis / sage flared skirt']
    rows = skirt.data["petticoatRingSize"]
    # The last six rings belong to the sewn petticoat; the seam itself is shared.
    lining_start = len(skirt.data.vertices)-6*rows
    sections = skirt_sections(skirt, lining_start)
    for obj in scene.objects:
        if obj != skirt and not obj.name.startswith(('Whale hem ', 'Scalloped white apron',
                                                     'Pleated apron border','Embroidered whale')):
            continue
        for v in obj.data.vertices:
            if obj == skirt and v.index >= lining_start:
                continue
            x,y,z = v.co
            a = atan2((y-.024)*1.62,x)
            side = cos(a)**2
            t = max(0,min(1,(.731-z)/.435))
            strength = t*t*(3-2*t)
            phase = 8*(a+pi/2) + .39*sin(3*a+.36) + .44*(1-t)
            ridge = .5+.5*cos(phase)
            depth = (.002+.014*ridge-.029*(1-ridge)**3)*strength
            direction = Vector((x, y-.024, 0)).normalized()
            # The panel, piping, bows and white lining use the same signed
            # displacement from the original shell. Their clearances survive it.
            lateral = max(0,min(1,(abs(cos(a))-.40)/.50))
            lateral = lateral*lateral*(3-2*lateral)
            delta = side_drape(meridian(sections,a), z, ridge)
            elliptical = Vector((cos(a),sin(a)/1.62,0))
            v.co += elliptical*(lateral*delta) + direction*depth*(1-.82*lateral)
            v.co.z += strength*(.018*side-.004+.010*ridge-.011*(1-ridge)**3)
        obj.data.update()
    turn_petticoat(skirt,lining_start,rows)
