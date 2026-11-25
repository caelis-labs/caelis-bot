"""Construct one continuous scalloped underskirt and trim the outer dress."""
from math import atan2, cos, pi, sin
import bpy
import bmesh
from mathutils import Vector
from mathutils.kdtree import KDTree


def mary_janes(surface,tube,leather,stocking,welt,brass):
    from math import exp, sqrt
    for side,sign in (("L",1),("R",-1)):
        joint="foot."+side
        for name in ("Caelis / sage shoe ","Caelis / shoe sole ","Caelis / ivory vamp ","Caelis / Mary Jane strap "):
            bpy.data.objects.remove(bpy.data.objects[name+side],do_unlink=True)
        verts,faces=[],[]
        steps=48
        # Turn the upper back into an actual foot opening, with a rolled topline.
        for row in range(8):
            for j in range(steps):
                a=2*pi*j/steps
                rear=(1+sin(a))/2
                x=cos(a)*(.078-.018*rear)
                y=-.032+.127*sin(a)
                arch=.017*exp(-((y-.029)/.048)**2)+.019*rear**4
                z=(.003+arch,.009+arch,.019+arch,.036+.021*rear+.006*cos(a)**2,
                   .055+.023*rear-.004*cos(a)**2,.066+.021*rear,.065+.022*rear,.052+.022*rear)[row]
                scale=(.93,.99,1,.995,.90,.99,.92,.88)[row]
                if row>=5:
                    x=.053*cos(a);y=.010+.066*sin(a)
                verts.append((sign*(.119+x*scale),y,z))
                if row:
                    k=(row-1)*steps+j;n=(row-1)*steps+(j+1)%steps
                    faces.append((k,n,n+steps,k+steps))
        faces.append(tuple(reversed(range(steps))))
        if sign<0:
            faces=[tuple(reversed(f)) for f in faces]
        shoe=surface("Mary Jane upper."+side,verts,faces,leather,joint,1)
        shoe.data.materials.append(welt)
        for poly in shoe.data.polygons:
            if poly.index<2*steps or poly.index==len(faces)-1:
                poly.material_index=1
        # The stocking continues over the instep and disappears under the upper.
        rings=[(.010,.060,.060,.079),(.009,.078,.059,.075),(.011,.091,.048,.065),
               (.017,.100,.039,.052),(.026,.107,.029,.035)]
        def foot_piece(name,rings,mat):
            vertices,quads=[],[]
            for i,(y,z,w,d) in enumerate(rings):
                for j in range(32):
                    a=2*pi*j/32
                    vertices.append((sign*(.119+w*cos(a)),y+d*sin(a),z))
                    if i:
                        k=(i-1)*32+j;n=(i-1)*32+(j+1)%32
                        quads.append((k,n,n+32,k+32))
            quads.extend([tuple(reversed(range(32))),tuple(range(len(vertices)-32,len(vertices)))])
            if sign<0:quads=[tuple(reversed(f)) for f in quads]
            return surface(name+"."+side,vertices,quads,mat,joint,1)
        foot_piece("Stocking instep",rings,stocking)
        foot_piece("Mary Jane heel",[(.066,.003,.040,.028),(.066,.006,.045,.030),
                   (.064,.024,.048,.033),(.062,.032,.050,.034)],welt)
        verts,faces=[],[]
        for i in range(29):
            u=2*i/28-1
            for j in range(4):
                y=-.075+.035*u*u+(j-1.5)*.0048
                z=.057+.023*sqrt(max(0,1-u*u))+.006*u+.0031*(j-1.5)
                verts.append((sign*(.119+.064*u),y,z))
                if i and j:
                    k=len(verts)-1;faces.append((k-5,k-4,k,k-1))
        surface("Mary Jane instep strap."+side,verts,faces,leather,joint,1)
        tube("Mary Jane brass clasp."+side,[(sign*x,y,z,.0027,.0027) for x,y,z in
             ((.171,-.054,.073),(.177,-.051,.065),(.178,-.037,.065),
              (.172,-.040,.076),(.171,-.054,.073))],brass,joint)


def finish_skirt(skirt, surface, fabric, white):
    # Retire the three old overlapping edges before building the new hem.
    for obj in list(bpy.context.scene.objects):
        if obj.name.startswith(("Caelis / garden hem","Caelis / cream scalloped underskirt")):
            bpy.data.objects.remove(obj,do_unlink=True)
    tree=KDTree(len(skirt.data.vertices))
    for v in skirt.data.vertices:
        tree.insert(v.co,v.index)
    tree.balance()
    count=192
    def band(name, rows, material):
        verts=[]
        faces=[]
        for i in range(count):
            angle=2*pi*i/count
            direction=Vector((cos(angle),sin(angle),0))
            for z,radial,pleat,drop in rows:
                start=direction+Vector((0,.024,z))
                hit,p,_,_=skirt.ray_cast(start,-direction)
                assert hit,(name,i,z)
                p+=direction*(radial+pleat*cos(24*angle))
                p.z+=drop
                if pleat:
                    p.z+=pleat*.65*cos(24*angle)
                verts.append(tuple(p))
            nxt=(i+1)%count
            for j in range(len(rows)-1):
                k=i*len(rows)+j
                q=nxt*len(rows)+j
                faces.append((k,k+1,q+1,q))
        return bind_trim(name,verts,faces,material)

    def bind_trim(name,verts,faces,material):
        obj=surface(name,verts,faces,material,"pelvis",0)
        obj["caelisLayer"]="clothing"
        obj.vertex_groups.clear()
        for v in obj.data.vertices:
            _,index,_=tree.find(v.co)
            for item in skirt.data.vertices[index].groups:
                n=skirt.vertex_groups[item.group].name
                g=obj.vertex_groups.get(n) or obj.vertex_groups.new(name=n)
                g.add([v.index],item.weight,"REPLACE")
        return obj
    gold=fabric("Antique gold edging","c4a269")
    # Alternate stitched crosses and four-point stars around the skirt.
    # Sample each point on the actual pleats so the stitches wrap around folds.
    vertices, faces = [], []
    for i in range(48):
        angle = 2*pi*i/48
        tall = i%3 == 0
        length = .009 if tall else .006
        width = .028 if tall else .015
        start = len(vertices)
        for da, dz in [(0,0),(0,1),(.24,.23),(1,0),(.24,-.23),(0,-1),
                       (-.24,-.23),(-1,0),(-.24,.23)]:
            theta = angle+width*da
            direction = Vector((cos(theta),sin(theta),0))
            origin = direction+Vector((0,.024,.532+length*dz))
            hit,point,_,_ = skirt.ray_cast(origin,-direction)
            assert hit
            vertices.append(point+.006*direction)
        for j in range(8):
            faces.append((start,start+1+j,start+1+(j+1)%8))
    bind_trim("Whale hem embroidered stars",vertices,faces,gold)
    # Narrow satin stitches follow the radius and height of the actual skirt.
    vertices,faces=[],[]
    for angle in (-.72,-2.42,.85,2.33):
        for path in (((0,.025),(0,-.020)),((-.056,.007),(.056,.007)),
                     ((-.12,-.003),(-.097,-.020),(0,-.030),(.097,-.020),(.12,-.003)),
                     ((-.12,-.003),(-.135,-.012)),((.12,-.003),(.135,-.012)),
                     tuple((.035*cos(2*pi*k/12),.037+.009*sin(2*pi*k/12)) for k in range(13))):
            start=len(vertices)
            for i,(u,v) in enumerate(path):
                a=path[max(0,i-1)]
                b=path[min(i+1,len(path)-1)]
                normal=Vector((-(b[1]-a[1]),(b[0]-a[0])*.30)).normalized()
                for side in (-1,1):
                    theta=angle+.75*u+normal.x*.006*side
                    direction=Vector((cos(theta),sin(theta),0))
                    origin=direction+Vector((0,.024,.546+.66*v+normal.y*.0023*side))
                    hit,point,_,_=skirt.ray_cast(origin,-direction)
                    assert hit
                    vertices.append(point+direction*.008)
                if i:
                    k=start+2*i
                    faces.append((k-2,k-1,k+1,k))
    bind_trim("Whale hem anchors",vertices,faces,gold)
    band("Whale hem fine piping",[(.518,.004,0,0),(.520,.004,0,0)],gold)
    band("Whale hem lower piping",[(.505,.004,0,0),(.508,.004,0,0)],gold)
    # A fine wave runs between the gold hem piping and the small anchors.
    vertices,faces=[],[]
    for i in range(257):
        theta=2*pi*i/256
        direction=Vector((cos(theta),sin(theta),0))
        for j in range(2):
            z=.523+.0038*sin(22*theta)+.0018*j
            hit,p,_,_=skirt.ray_cast(direction+Vector((0,.024,z)),-direction)
            assert hit
            vertices.append(p+direction*.006)
        if i:
            k=2*i
            faces.append((k-2,k,k+1,k-1))
    bind_trim("Whale hem embroidered wave",vertices,faces,gold)
    bow=fabric("Hem bow deep sea silk","1e2a56")
    for side,angle in (("L",-.81),("R",-pi+.81)):
        vertices,faces=[],[]
        def fabric_point(u,z,lift):
            theta=angle+u
            direction=Vector((cos(theta),sin(theta),0))
            hit,p,_,_=skirt.ray_cast(direction+Vector((0,.024,z)),-direction)
            assert hit
            return p+direction*lift
        for sign in (-1,1):
            for tail in (False,True):
                for i in range(10):
                    t=i/9
                    for j in range(5):
                        u=j/2-1
                        width=.003+(.012 if tail else .020)*sin(pi*t*.66)
                        z=.566+(-.031 if tail else .012)*t+u*width
                        vertices.append(fabric_point(sign*t*(.077 if tail else .118),z,
                                        .012+.007*sin(pi*t)*(1-u*u)+.002*cos(pi*u)))
                        if i and j:
                            k=len(vertices)-1;faces.append((k-6,k-5,k,k-1))
        bind_trim("Whale hem satin bow."+side,vertices,faces,bow)
    # Sew the lining to the actual hem loop. Projecting a parallel ring
    # onto a higher cross-section leaves gaps where the hem flares out.
    bm=bmesh.new()
    bm.from_mesh(skirt.data)
    rim=sorted((v for v in bm.verts if v.is_boundary and v.co.z<.55),
               key=lambda v: atan2(v.co.y-.024,v.co.x))
    assert len(rim)>64 and all(bm.edges.get((v,rim[(i+1)%len(rim)]))
                               for i,v in enumerate(rim))
    weights=bm.verts.layers.deform.active
    skirt.data.materials.append(white)
    # The fine cotton gathering turns faster than the twill. Sample each
    # existing edge four times and join the finer fabric directly to that seam.
    divisions=4
    samples=[]
    for i,a in enumerate(rim):
        b=rim[(i+1)%len(rim)]
        p,q,r,s=(rim[(i+j)%len(rim)].co for j in (-1,0,1,2))
        groups=set(a[weights].keys())|set(b[weights].keys())
        for j in range(divisions):
            t=j/divisions
            pt=(2*q+(r-p)*t+(2*p-5*q+4*r-s)*t*t+(3*q-p-3*r+s)*t**3)*.5
            blend={key:(1-t)*a[weights].get(key,0)+t*b[weights].get(key,0) for key in groups}
            samples.append((pt,blend))
    previous=None
    for radial,amplitude,drop in ((-.003,.001,.006),(.002,.004,.014),
                                  (.011,.011,.031),(.016,.017,.049),(.014,.016,.054),(.013,.015,.056)):
        if drop==.056:
            skirt.data.materials.append(fabric("Petticoat sewn edge","8790ac"))
        current=[]
        for pt,blend in samples:
            x,y,z=pt
            angle=atan2(y-.024,x)
            outward=Vector((x,y-.024,0)).normalized()
            flute=cos(24*angle+.47*sin(3*angle))
            point=pt+outward*(radial+amplitude*flute)
            point.z-=drop-amplitude*.57*flute
            v=bm.verts.new(point)
            for key,value in blend.items():
                v[weights][key]=value
            current.append(v)
        for i,v in enumerate(current):
            j=(i+1)%len(current)
            if previous is None:
                a=rim[i//divisions]
                f=bm.faces.new((a,v,current[j]))
                if j%divisions==0:
                    bridge=bm.faces.new((rim[j//divisions],a,current[j]))
                    bridge.material_index=1
                    bridge.smooth=True
            else:
                f=bm.faces.new((previous[j],previous[i],v,current[j]))
            f.material_index=len(skirt.data.materials)-1
            f.smooth=True
        previous=current
    skirt.data["petticoatRingSize"]=len(samples)
    bmesh.ops.recalc_face_normals(bm,faces=list(bm.faces))
    bm.to_mesh(skirt.data)
    bm.free()
    skirt.data.update()


SLEEVE_SECTIONS = 44


def gather_sleeves(rig, surface, navy, white):
    """Skin the puff, lower sleeve and flounce as one sewn sleeve."""
    blouse=bpy.data.objects["Caelis / connected blouse and sleeves"]
    bm=bmesh.new()
    bm.from_mesh(blouse.data)
    # Keep the shoulder seam inside the cap. The new sleeve covers the arm.
    bmesh.ops.delete(bm,geom=[v for v in bm.verts if abs(v.co.x)>.219],context="VERTS")
    bm.to_mesh(blouse.data)
    bm.free()
    blouse.data.update()
    # Negative stations extend into the bodice. The elbow is at station 1,
    # the wrist at 2. Gathers close into a folded open cuff around the hand.
    rows=[(-.28,.037,.012),(-.14,.049,.018),(-.04,.055,.020),(.08,.063,.030),(.34,.077,.038),(.65,.082,.045),
          (.85,.074,.042),(1,.060,.024),(1.14,.056,.020),(1.32,.059,.018),
          (1.57,.055,.03),(1.63,.065,.06),(1.68,.072,.07),(1.758,.060,.06),
          (1.772,.055,.04),(1.782,.053,.04),(1.83,.054,.07),(1.93,.061,.12),
          (1.985,.065,.15),(2.00,.062,.14),(1.98,.048,.08),(1.93,.034,.025)]
    sections=SLEEVE_SECTIONS
    for side in ("L","R"):
        arm=rig.data.bones["upper_arm."+side]
        fore=rig.data.bones["forearm."+side]
        verts,faces=[],[]
        for i,(t,r,gather) in enumerate(rows):
            base=arm if t<=1 else fore
            axis=base.tail_local-base.head_local
            center=base.head_local+axis*(t if t<=1 else t-1)
            if t < 0:
                # Carry the sleeve head over the shoulder into the bodice.
                # Its inner seam stays down on the clavicle as the arm turns.
                center.z -= max(0,-t)*.154
            u=axis.cross(Vector((0,1,0))).normalized()
            v=axis.normalized().cross(u)
            for j in range(sections):
                a=2*pi*j/sections
                flute=cos(8*a+.36*t)
                radius=r*(1+gather*flute)
                sag=.008*flute if t>=1.90 else 0
                verts.append(center+radius*(u*cos(a)+v*.86*sin(a))+axis.normalized()*sag)
                if i:
                    k=i*sections+j
                    n=i*sections+(j+1)%sections
                    faces.append((k-sections,n-sections,n,k))
        sleeve=surface("Gathered sleeve and lace."+side,verts,faces,navy,arm.name,1)
        sleeve["caelisLayer"]="clothing"
        sleeve.data.materials.append(white)
        chest=sleeve.vertex_groups.new(name="chest")
        distal=sleeve.vertex_groups.new(name=fore.name)
        for i,(t,_,_) in enumerate(rows):
            group=list(range(i*sections,(i+1)*sections))
            w=max(0,min(1,(t-.76)/.49))
            shoulder=max(0,min(1,-t/.26))*.85
            chest.add(group,shoulder,"REPLACE")
            sleeve.vertex_groups[arm.name].add(group,1-w-shoulder,"REPLACE")
            distal.add(group,w,"REPLACE")
        for poly in sleeve.data.polygons:
            t=rows[1+poly.index//sections][0]
            poly.material_index=int(t>1.57 and not 1.758<t<=1.782)


def seat_waist_bow(scene,skirt,blouse):
    """Fit the knot and each ribbon section to the finished back waist."""
    from mathutils.bvhtree import BVHTree
    cloth=[BVHTree.FromPolygons([v.co for v in obj.data.vertices],
                                [p.vertices[:] for p in obj.data.polygons])
           for obj in (skirt,blouse)]
    for name,offsets in (("Back waist knot",(.006,.030,.022)),
                         ("Back waist bow",(.015,.033,.049,.047)),
                         ("Back sash",(.009,.014,.018,.022))):
        pieces=[scene.objects[name]] if name.endswith("knot") else [
            scene.objects[name+"."+side] for side in ("L","R")]
        for piece in pieces:
            for i,offset in enumerate(offsets):
                ring=piece.data.vertices[i*12:(i+1)*12]
                center=sum((v.co for v in ring),Vector())/len(ring)
                hits=[tree.ray_cast((center.x,1,center.z),(0,-1,0))[0] for tree in cloth]
                # A bow wing may extend past the side seam at its free end.
                ys=[hit.y for hit in hits if hit is not None]
                if ys:
                    delta=max(ys)+offset-center.y
                    for v in ring:
                        v.co.y+=delta


def fit_stitches(surface,skirt,apron,white):
    """Seat the embroidery and the tail opening on the finished garment."""
    from mathutils.bvhtree import BVHTree
    def tree(obj):
        return BVHTree.FromPolygons([v.co for v in obj.data.vertices],[f.vertices[:] for f in obj.data.polygons])
    cloth=tree(apron)
    for v in bpy.data.objects["Embroidered whale"].data.vertices:
        p=cloth.ray_cast((v.co.x,-2,v.co.z),(0,1,0))[0]
        assert p is not None
        v.co.y=p.y-.0018
    back=tree(skirt)
    tube=bpy.data.objects["Whale tail"].data.vertices
    centers=[sum((v.co for v in tube[i:i+12]),Vector())/12 for i in range(0,len(tube),12)]
    # Each angle of the tube emerges at a different distance along its axis.
    # Trace that seam before flaring the last two rows into gathered lace.
    steps=72
    vertices,faces=[],[]
    for i in range(steps):
        a=2*pi*i/steps
        path=[c+(tube[12*j].co-c)*cos(a)+(tube[12*j+3].co-c)*sin(a) for j,c in enumerate(centers)]
        def at(path,t):
            k=min(int(t),len(path)-2)
            return path[k].lerp(path[k+1],t-k)
        def exposed(t):
            p=at(path,t)
            hit=back.ray_cast((p.x,2,p.z),(0,-1,0))[0]
            return hit is None or p.y>=hit.y
        last=0
        for k in range(1,81):
            t=k/16
            if exposed(t):
                break
            last=t
        else:
            raise ValueError("Cannot locate tail exit at sector "+str(i))
        # Above the skirt waistband the tube already meets the back sash.
        lo,hi=(0,0) if exposed(0) else (last,t)
        for _ in range(14):
            mid=(lo+hi)/2
            if exposed(mid):hi=mid
            else:lo=mid
        step=(path[int(hi)+1]-path[int(hi)]).length
        for j in range(5):
            u=j/4
            station=max(0,hi+(.002+.037*u)/step)
            p=at(path,station)
            radial=(p-at(centers,station)).normalized()
            vertices.append(p+radial*(.001+.010*sin(pi*u)+(.009+.007*cos(12*a))*u))
    for i in range(steps):
        for j in range(4):
            k=5*i+j;n=5*((i+1)%steps)+j
            faces.append((k,n,n+1,k+1))
    surface("Whale tail root flounce",vertices,faces,white,"tail.base",1)


def fit_sleeves_to_arms(scene,rig):
    """Fit the sleeve pattern over the posed, skinned Caelis arm before export."""
    from mathutils.bvhtree import BVHTree
    graph = bpy.context.evaluated_depsgraph_get()
    for side in ("L","R"):
        arm = scene.objects["Caelis arm and hand."+side].evaluated_get(graph)
        skin = BVHTree.FromPolygons([arm.matrix_world@v.co for v in arm.data.vertices],
                                    [p.vertices[:] for p in arm.data.polygons])
        sleeve = scene.objects["Gathered sleeve and lace."+side]
        bind = rig.matrix_world.inverted() @ sleeve.matrix_world
        transforms = {g.index:rig.matrix_world @ rig.pose.bones[g.name].matrix
                      @ rig.data.bones[g.name].matrix_local.inverted() @ bind
                      for g in sleeve.vertex_groups}
        rings = list(sleeve.data.vertices)
        # All vertices in a pattern row share weights. Work in the standing
        # pose to account for the source wrist twist; write back to rest space.
        # Leave the returned inner lip of the cuff inside the outer lace.
        for start in range(0,len(rings)-2*SLEEVE_SECTIONS,SLEEVE_SECTIONS):
            row = rings[start:start+SLEEVE_SECTIONS]
            weighted = [transforms[g.group]*g.weight for g in row[0].groups if g.weight>0]
            matrix = sum(weighted[1:],weighted[0])
            undo = matrix.inverted()
            pts = [matrix@v.co for v in row]
            center = sum(pts,Vector())/len(row)
            delta = []
            for pt in pts:
                radial = (pt-center).normalized()
                hit = skin.ray_cast(pt+radial*.080,-radial,.12)[0]
                margin = (hit-pt).dot(radial)+.010 if hit is not None else 0
                delta.append(max(0,min(.045,margin)))
            for j,(v,pt) in enumerate(zip(row,pts)):
                # Blend around folds so a single protruding point cannot
                # leave a hard dent in the pattern.
                ease = max(delta[j],.76*delta[j-1],.76*delta[(j+1)%len(row)])
                v.co = undo @ (pt+(pt-center).normalized()*ease)
        sleeve.data.update()
