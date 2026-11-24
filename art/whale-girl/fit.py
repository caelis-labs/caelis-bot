"""Reference-specific rest proportions and costume details, in Blender authoring axes."""
from math import atan2, ceil, cos, exp, floor, pi, sin
import bpy
from mathutils import Vector
from mathutils.bvhtree import BVHTree


def ease(low, high, value):
    t = max(0, min(1, (value-low)/(high-low)))
    return t*t*(3-2*t)


def standing_proportions(point):
    """Map the whole rest figure and its gesture landmarks in the same space."""
    x,y,z=point
    # Keep the shoe's heel and the entire chin-to-crown volume rigid. The
    # visible shin and the length from the hem to the belt set the stature.
    drop=.058*ease(.123,.283,z)+.037*ease(.32,.83,z)
    # Spread the stance around each ankle, without widening the shoes.
    stance=.017*(1-ease(.18,.58,z))
    x+=stance*(2*ease(-.02,.02,x)-1)
    # Widen the shoulder girdle inside the mantle of hair. Apply the same
    # displacement to the clavicle, sleeve cap and upper-arm joint.
    shoulder=ease(.78,.98,z)*(1-ease(1.032,1.108,z))
    x+=.078*x*shoulder*(1-ease(.28,.50,abs(x)))
    return Vector((x,y,z-drop))


def interpolate(z, landmarks):
    for (a,x),(b,y) in zip(landmarks, landmarks[1:]):
        if z <= b:
            return x+(y-x)*(z-a)/(b-a)
    return landmarks[-1][1]+(z-landmarks[-1][0])*.78


# Heel, knee, waist, shoulders, chin and crown have independent height targets.
# Fitted to the illustrated front silhouette: chin at ~45%, waist at ~64%
# and skirt hem at ~87% of the crown-to-sole height (including the cowlick).
HEIGHT = [(0,0),(.17,.16),(.369,.374),(.56,.575),(.805,.825),
          (.945,1.03),(1.072,1.135),(1.916,1.948),(2.14,2.123)]


def body(point):
    x,y,z = point
    head = ease(.995,1.08,z)
    leg = 1-ease(.52,.64,z)
    waist = exp(-((z-.79)/.14)**2)
    shoulder = exp(-((z-.935)/.1)**2)
    jaw = exp(-((z-1.19)/.13)**2)
    width = (1-head)*(1+.12*shoulder+.065*waist-.11*leg) + head*(.915-.025*jaw)
    # Bring the eyes towards the midline and shape the cheeks below them.
    eye = exp(-((abs(x)-.18)/.14)**2-((z-1.35)/.11)**2)
    front = 1-ease(-.12,.04,y)
    nx = x*width - (1 if x>0 else -1)*.012*eye*front
    nz = interpolate(z,HEIGHT)+.04*(z-1.34)*eye*front
    ny = y*(1-.15*head-.09*leg-.12*waist)
    ny-=.023*front*jaw*exp(-((abs(x)-.21)/.13)**2)
    return Vector((nx,ny,nz))


def sagittal_fit(face, fitted):
    """Transfer the shared head's sagittal sections at equal chin-to-crown height.

    X and Z already carry the Whale Girl portrait. Only depth is registered to
    the Blender base head. Use the same mapping for ink, skin, cap and attachments.
    """
    source=[v.co for v in face.data.vertices]
    count=64
    def sections(points):
        bottom=min(p.z for p in points)
        height=max(p.z for p in points)-bottom
        step=height/count
        rows=[[float("inf"),-float("inf")] for _ in range(count)]
        for edge in face.data.edges:
            a,b=(points[i] for i in edge.vertices)
            if a.z>b.z:
                a,b=b,a
            if b.z-a.z<1e-8:
                continue
            first=max(0,ceil((a.z-bottom)/step-.5))
            last=min(count-1,floor((b.z-bottom)/step-.5))
            for i in range(first,last+1):
                t=(bottom+(i+.5)*step-a.z)/(b.z-a.z)
                y=a.y+(b.y-a.y)*t
                rows[i][0]=min(rows[i][0],y)
                rows[i][1]=max(rows[i][1],y)
        axis=(max(p.y for p in points)+min(p.y for p in points))/2
        return bottom,height,axis,rows

    _,source_height,source_axis,reference=sections(source)
    bottom,height,axis,rest=sections(fitted)
    scale=height/source_height
    bands=[]
    for (a,b),(lo,hi) in zip(reference,rest):
        gain=scale*(b-a)/(hi-lo)
        bands.append((gain,axis+scale*(a-source_axis)-gain*lo))

    def adjust(point):
        t=max(0,min(count-1,(point.z-bottom)*count/height-.5))
        i=int(t); blend=t-i
        k0,b0=bands[i]; k1,b1=bands[min(i+1,count-1)]
        target=(k0+(k1-k0)*blend)*point.y+b0+(b1-b0)*blend
        point.y+=(target-point.y)*ease(bottom-.08,bottom+.015,point.z)
        return point
    return adjust


def soften_chin(face, ink):
    """Fair the mouth-to-chin transition and seat the animated paint back on it."""
    vertices=face.data.vertices
    original=[v.co.copy() for v in vertices]
    polygons=[p.vertices[:] for p in face.data.polygons]
    before=BVHTree.FromPolygons(original,polygons)
    neighbors=[[] for _ in vertices]
    for edge in face.data.edges:
        a,b=edge.vertices
        neighbors[a].append(b)
        neighbors[b].append(a)
    weights=[(1-ease(1.177,1.232,p.z))*(1-ease(.04,.20,p.y)) for p in original]
    active=[i for i,w in enumerate(weights) if w>0]
    coords=original.copy()
    for _ in range(10):
        previous=coords.copy()
        for i in active:
            mean=sum((previous[j] for j in neighbors[i]),Vector())/len(neighbors[i])
            delta=(mean-previous[i])*weights[i]
            # Fair the depth most strongly. Limit silhouette travel so the
            # chin pad still meets the collar and retains its transverse arc.
            p=previous[i]+Vector((.26*delta.x,.65*delta.y,.26*delta.z))
            p.z=max(original[i].z-.0016,min(original[i].z+.0016,p.z))
            coords[i]=p
    # The shared head has directional shape keys. Frame evaluation restores
    # its Basis, so every key must carry the same correction as the skin.
    if face.data.shape_keys:
        for key in face.data.shape_keys.key_blocks:
            for i in active:
                key.data[i].co+=coords[i]-original[i]
    for i in active:
        vertices[i].co=coords[i]
    after=BVHTree.FromPolygons(coords,polygons)
    for obj in ink:
        keys=obj.data.shape_keys
        for points in ([key.data for key in keys.key_blocks] if keys else [obj.data.vertices]):
            for vertex in points:
                x,y,z=vertex.co
                if z>=1.232:
                    continue
                origin=Vector((x,-1,z))
                direction=Vector((0,1,0))
                old=before.ray_cast(origin,direction)[0]
                new=after.ray_cast(origin,direction)[0]
                if old is None or new is None:
                    raise ValueError(f"{obj.name}: ink outside cheek after fairing")
                vertex.co.y+=new.y-old.y
        if keys:
            for v,base in zip(obj.data.vertices,keys.reference_key.data):
                v.co=base.co


def fit(rig, surface, tube, navy, white, blue, fabric, apron_rows, apron_cols):
    scene=bpy.context.scene
    remove=tuple("Hair / "+name for name in ("back principal","flowing rear base","sculpted fringe","long outer sweep","lower inward curl",
            "lower overlapping petal","outer tip below shoulder","shoulder outward flick",
            "long framing tendril","cheek silhouette"))+("Hair / signature curled forelock","Eye /",
            "Mouth /","Brow.","Nose / subtle warm indication","Cheek / delicate blush","Caelis / sock ruffle","Caelis / wrist ruffle","Caelis / blouse small button","Caelis / neck button","Caelis / neck sage bow","Caelis / blouse placket")
    for obj in list(scene.objects):
        if obj.name.startswith(remove):
            bpy.data.objects.remove(obj,do_unlink=True)

    def patch(name, outline, base, mat, joint, offset=.006):
        vertices=[]
        for x,z in outline:
            hit,pt,_,_=base.ray_cast(Vector((x,-1,z)),Vector((0,1,0)))
            if not hit:
                raise ValueError(f"{name} outside attachment surface at {(x,z)}")
            vertices.append((x,pt.y-offset,z))
        # A center fan follows convex surfaces better than a single planar n-gon.
        center=sum(map(Vector,vertices),Vector())/len(vertices)
        _,pt,_,_=base.ray_cast(Vector((center.x,-1,center.z)),Vector((0,1,0)))
        vertices.append((center.x,pt.y-offset,center.z))
        count=len(outline)
        faces=[(count,i,(i+1)%count) for i in range(count)]
        return surface(name,vertices,faces,mat,joint,0)

    def ellipse(x,z,w,h):
        return [(x+w*cos(i*2*pi/32),z+h*sin(i*2*pi/32)) for i in range(32)]

    face=bpy.data.objects["Face / shallow cheeks and rounded jaw"]
    from portrait import paint_face
    from acting import shape_portrait
    existing=set(scene.objects)
    paint_face(face,surface,fabric)
    shape_portrait(face, sorted(set(scene.objects)-existing,key=lambda o:o.name))

    blouse=bpy.data.objects["Caelis / connected blouse and sleeves"]
    # Keep the bodice center uninterrupted. Each strap has a flat woven
    # center and an independently fluted outer edge seated on the blouse.
    for side,sign in (("L",1),("R",-1)):
        verts,faces=[],[]
        for i in range(49):
            t=i/48
            x=.057+.039*t+.008*sin(pi*t)
            z=.819+.150*t
            wave=cos(14*pi*t+.35*sin(2*pi*t))
            for j,(width,lift) in enumerate(((0,.002),(.010,.003),(.021,.003),
                                             (.025,.006),(.033+.004*wave,.009+.004*wave))):
                xx=sign*(x+width)
                hit,pt,_,_=blouse.ray_cast(Vector((xx,-1,z)),Vector((0,1,0)))
                if not hit:
                    raise ValueError("Apron strap outside blouse")
                verts.append((xx,pt.y-.008-lift,z))
                if i and j:
                    k=len(verts)-1
                    faces.append((k-6,k-5,k,k-1))
        surface("Pinafore shoulder frill."+side,verts,faces if sign>0 else [f[::-1] for f in faces],white,"chest",1)
    bow=fabric("Indigo grosgrain","263963")
    for sign,side in ((1,"L"),(-1,"R")):
        tube("Collar bow loop."+side,[(sign*x,y,z,w,d) for x,y,z,w,d in [
            (.008,-.134,.965,.010,.010),(.042,-.145,.975,.023,.015),
            (.077,-.141,.983,.031,.017),(.084,-.127,.984,.026,.011)]],bow,"chest")
        tube("Collar bow end."+side,[(sign*x,y,z,w,d) for x,y,z,w,d in [
            (.008,-.136,.966,.011,.009),(.025,-.150,.932,.014,.008),
            (.039,-.153,.891,.012,.005)]],bow,"chest")
    tube("Knotted collar ribbon",[(0,-.149,.954,.014,.012),
         (0,-.154,.968,.018,.015),(0,-.145,.978,.014,.009)],navy,"chest")
    # Fit a broad velvet waist panel to the blouse. Each stud lies on the
    # same curvature as the fabric, including the side visible in profile.
    gold=fabric("Warm brass buttons","deb368")
    belt_verts, belt_faces=[], []
    for i in range(41):
        angle=pi*i/40
        direction=Vector((cos(angle),-sin(angle),0))
        for j,z in enumerate((.801,.806,.830,.847)):
            start=Vector((0,.025,z))
            hit,p,_,_=blouse.ray_cast(start,direction)
            if not hit:
                raise ValueError("waist panel missed blouse")
            belt_verts.append(p+direction*.010)
            if i and j:
                k=4*i+j
                belt_faces.append((k-5,k-4,k,k-1))
    surface("Velvet waist panel",belt_verts,belt_faces,navy,"chest",0)
    for sign,side in ((1,"L"),(-1,"R")):
        for i,z in enumerate((.810,.836)):
            patch("Waist button "+side+str(i),ellipse(sign*.054,z,.005,.006),
                  blouse,gold,"chest",.015)
    # A little raised white collar closes the gap from the bib to the chin.
    verts,faces=[],[]
    for i in range(48):
        a=2*pi*i/48
        for j,(radius,z) in enumerate(((.082,.981),(.084,.995),(.062,1.060),(.057,1.061))):
            dip=.009*max(0,-sin(a))**8*j/3
            verts.append((radius*cos(a),.037+radius*1.18*sin(a),z-dip+.004*cos(12*a)*j/3))
            if j:
                k=4*i+j
                n=4*((i+1)%48)+j
                faces.append((k-1,n-1,n,k))
    surface("Gathered standing collar",verts,faces,white,"chest",1)

    from dress import gather_sleeves
    gather_sleeves(rig, surface, navy, white)

    from hair import style_locks
    lock_material=style_locks(tube,fabric)
    # The reference has a single sweeping cowlick with a hooked left tip.
    tube("Hooked crown lock",[(.076,-.045,1.89,.012,.009),(.098,.018,2.011,.027,.016),(.091,.043,2.102,.052,.022),
         (-.072,-.020,2.199,.048,.024),(-.219,-.128,2.165,.031,.016),(-.263,-.171,2.096,.021,.012),
         (-.241,-.185,2.012,.010,.006),(-.212,-.168,1.993,.0018,.0015)],navy,"head")

    skirt=bpy.data.objects["Caelis / sage flared skirt"]
    # A pleated border follows the apron outline, including both side seams.
    apron=bpy.data.objects["Scalloped white apron"]
    bottom=(apron_rows-1)*apron_cols
    outline=([row*apron_cols for row in range(apron_rows)]
             +list(range(bottom+1,bottom+apron_cols))
             +[row*apron_cols+apron_cols-1 for row in reversed(range(apron_rows-1))])
    vertices,faces=[],[]
    for i,index in enumerate(outline):
        p=apron.data.vertices[index].co.copy()
        a=apron.data.vertices[outline[max(i-1,0)]].co
        b=apron.data.vertices[outline[min(i+1,len(outline)-1)]].co
        tangent=(b-a).normalized()
        outside=Vector((tangent.z,0,-tangent.x)).normalized()
        wave=cos(i*.70+.38*sin(i*.19))
        for j in range(5):
            t=j/4
            v=p+outside*(t*(.020+.004*wave))
            v.y-=.004+t*(.002+.0035*wave)+.003*sin(pi*t)
            vertices.append(v)
            if i and j:
                k=len(vertices)-1;faces.append((k-6,k-5,k,k-1))
    lace=surface("Pleated apron border",vertices,faces,white,"pelvis",1)
    lace.vertex_groups.clear()
    for i,index in enumerate(outline):
        for item in apron.data.vertices[index].groups:
            name=apron.vertex_groups[item.group].name
            group=lace.vertex_groups.get(name) or lace.vertex_groups.new(name=name)
            group.add(list(range(5*i,5*i+5)),item.weight,"REPLACE")

    from dress import finish_skirt
    finish_skirt(skirt,surface,fabric,white)
    tail_origin=rig.data.bones["tail.base"].head_local.copy()
    tail_shift=body(tail_origin)-tail_origin
    def fit_tail(point):
        p=point+tail_shift
        reach=point.x-tail_origin.x
        p.x-=.31*reach
        p.z-=.060*ease(.18,.86,reach)
        # Sweep the distal fork closer to the hip in depth. Past the bend,
        # both lobes translate together and retain their span.
        p.y-=.078*ease(.18,.72,reach)
        return p
    head_joint=body(rig.data.bones["head"].head_local)

    def fit_head(point, trailing=False):
        # Keep the neck socket and take volume out of the cranium above it.
        # Long hair follows the smaller temples at its roots. Release the
        # bend below the shoulder so the curled ends keep their reach.
        x,y,z=point-head_joint
        follow=ease(-.13,.37,z) if trailing else 1
        jaw=exp(-((z-.16)/.15)**2)
        p=head_joint+Vector((x*(1-(.17+.145*jaw)*follow),
                             y*(1-.105*follow),z*(1-.215*follow)))
        chin=1-ease(1.14,1.29,p.z)
        p.z+=(.052*chin-.056)*follow
        p.y-=.019*chin*(1-ease(-.12,.01,p.y))*follow
        return p

    def portrait_outline(p):
        cheek=exp(-((p.z-1.217)/.044)**2)*ease(.13,.22,abs(p.x))
        lower_jaw=exp(-((p.z-1.150)/.027)**2)
        p.x*=1-.036*cheek+.060*lower_jaw
        p.z-=.0045*lower_jaw
        return p

    round_head=sagittal_fit(face,[portrait_outline(fit_head(body(v.co))) for v in face.data.vertices])

    for obj in list(scene.objects):
        if obj.type!="MESH" or "deform_group" not in obj:
            continue
        hair=obj.name.startswith("Hair /")
        facial_surface=(obj==face or obj.name=="Soft nose shadow" or
                        obj.get("caelisLayer") in ("acting_ink","acting_iris"))
        joint=obj["deform_group"]
        on_head=joint=="head" or joint.startswith("hair.")
        trailing=joint.startswith("hair.") or "hair.back" in obj.vertex_groups
        hem = obj in (skirt,apron,lace) or any(k in obj.name for k in
            ("Whale hem","Embroidered whale","Back sash"))
        shoulder=blouse==obj
        def sculpt(point):
            x,y,z=point
            if obj.get("deform_group") in ("tail.base","tail.flukes"):
                return fit_tail(point)
            p=body(point)
            if obj.name=="Neck":
                # Taper the short column inside the gathered collar while
                # the upper end stays seated behind the turning jaw.
                p.x*=.69
                p.y=.036+.80*(p.y-.036)
                p.z-=.021*ease(.98,1.16,p.z)
            if obj.name.startswith("Caelis / ivory sock"):
                # The stocking runs up behind the skirt; taper its ankle
                # independently of the rounded calf and the shoe opening.
                ankle=exp(-((z-.129)/.074)**2)
                axis=.106 if x>0 else -.106
                p.x=axis+(p.x-axis)*(1-.35*ankle+.08*exp(-((z-.225)/.054)**2))
                p.y=.021+(p.y-.021)*(1-.26*ankle)
                p.z+=.160*ease(.115,.29,z)
            if obj.name.startswith(("Temple ribbon","Temple bow knot")):
                p.x-=.019
                p.y+=.173
            if obj.name in ("Bound headdress seam","Pleated pearl headdress"):
                p.y+=.112*ease(.18,.46,abs(x))
            if obj.name.startswith(("Collar bow","Knotted collar")):
                p.y+=.034
                p.z+=.030
            if obj.name.startswith("Caelis / garden collar"):
                p.z+=.021
            if obj.name.startswith(("Caelis / sage shoe","Caelis / ivory vamp","Instep strap","Buckle.")):
                p.z+=.027*ease(.024,.108,z)
                if obj.name.startswith("Caelis / ivory vamp"):
                    p.y-=.003
                    p.z+=.003
            if z < .20:
                # Rounded Mary Janes with a visible toe box beneath the white socks.
                toe=1-ease(.10,.20,z)
                foot_x=.11 if x>0 else -.11
                p.x+=(x-foot_x)*.13*toe
                p.y-=(.015-.20*y)*toe
            if hair:
                # Back locks taper towards the waist; stagger the face-framing layers.
                t=1-ease(.50,1.46,z)
                p.z=interpolate(z,[(.40,.47),(.65,.64),(1.0,.99),(1.45,1.48),(1.916,1.948),(2.14,2.12)])
                sign=1 if x>0 else -1
                p.x=x*(.95-.11*t)+sign*.048*sin(10*z+.6*sign)*t
                p.y=y*(.85-.07*t)-.030*t
                if obj.get("caelisLayer")=="ocean_curls":
                    p.x=x*(.96+.02*t)
                    p.y=y*.89
            if hem:
                flare=1-ease(.475,.805,z)
                angle=atan2(y-.023,x)
                fold=cos(10*angle+.5*sin(3*angle)+.45)*flare
                # Most of the sweep opens below the hips. The apron falls
                # from the waistband on a shallow front plane.
                fullness=sin(pi*flare)**.75
                p.x*=1+.83*flare+.105*fullness+.036*fold
                p.x*=1-.16*flare
                p.y=(p.y-.023)*(1+.24*flare+.040*fullness+.046*fold)+.023
                # Carry fullness behind the hips, then roll the rear hem back
                # under it. Lift the quarter where the peduncle exits.
                rear=ease(.00,.78,sin(angle))
                p.y+=rear*(.028*fullness-.018*flare**3)
                p.z+=rear*flare*(.023+.044*max(0,cos(angle)))
                p.z-=.150*flare
                p.z+=(.031*cos(angle+.12)**2+.014*cos(7*angle+.38)+.007*cos(4*angle-.6))*flare
            if shoulder:
                # Draw the remaining shoulder seam under the new sleeve cap.
                s=1 if x>=0 else -1
                a=Vector((.17*s,.018,.945))
                d=Vector((.112*s,-.012,-.115))
                t=(point-a).dot(d)/d.length_squared
                radial=point-a-d*t
                covered=ease(.14,.207,abs(x))*(1-ease(.94,1.38,t))
                p-=radial*.66*covered
            if on_head:
                p=fit_head(p,trailing)
            if facial_surface:
                p=portrait_outline(p)
            if on_head:
                p=round_head(p)
            if facial_surface:
                # Carry the malar pad down beside the mouth. The jaw gains
                # height toward the chin and releases into the original nape.
                x=abs(p.x)
                anterior=1-ease(.07,.24,p.y)
                lower=1-ease(1.122,1.239,p.z)
                fullness=exp(-((p.z-1.170)/.060)**2)
                cheek=exp(-((x-.161)/.090)**2-((p.z-1.217)/.064)**2)
                corner=exp(-((x-.044)/.029)**2-((p.z-1.193)/.035)**2)
                p.x+=.0065*(1 if p.x>0 else -1)*ease(.055,.165,x)*fullness*anterior
                p.z-=.013*lower*(.66+.34*exp(-(x/.108)**2))*anterior
                # A broad chin pad meets the cheeks in a U. Lift the center
                # under the mouth and keep volume either side of the midline.
                chin_pad=(1-ease(1.120,1.173,p.z))*anterior
                p.z+=chin_pad*(.009*exp(-(x/.072)**2)-.0015*exp(-((x-.108)/.055)**2))
                p.x+=.005*chin_pad*ease(.015,.082,x)*(1 if p.x>0 else -1)
                # The nape sits behind the neck in front view. Carry the
                # visible front of the pad down to meet the cheek arc.
                front_pad=exp(-(x/.069)**2-((p.y+.010)/.080)**2)
                p.z-=.013*front_pad*(1-ease(1.133,1.172,p.z))
                # Carry the cheek down beside the mouth. The broad lateral
                # pad rounds into the chin and feathers away at the temples.
                pad=exp(-((p.z-1.185)/.049)**2)
                side=ease(.074,.165,x)*anterior
                p.x+=.022*(1 if p.x>0 else -1)*pad*side
                p.z-=.0045*pad*side
                p.z+=.0055*chin_pad*exp(-(x/.092)**2)
                p.y-=.019*pad*exp(-((x-.155)/.081)**2)*(1-ease(-.075,.060,p.y))
                p.y-=(.019*cheek+.004*corner)*(1-ease(-.12,.07,p.y))
                # Support the lower lip across the anterior jaw. Otherwise
                # the opening rolls underneath the face before reaching its rim.
                pad=exp(-((p.z-1.146)/.040)**2)*(1-ease(.092,.218,x))
                p.y-=.032*pad*(1-ease(-.055,.065,p.y))
                # The leveled portrait turns inward at the outer cheek,
                # then descends more gently to the center of the chin.
                outer=ease(.120,.224,x)*exp(-((p.z-1.197)/.058)**2)
                p.x-=(1 if p.x>0 else -1)*.033*outer
                p.z-=.013*(1-ease(1.142,1.179,p.z))*exp(-(x/.100)**2)
            return p
        if obj.data.shape_keys:
            for key in obj.data.shape_keys.key_blocks:
                for v in key.data:
                    v.co=sculpt(v.co.copy())
            for v,base in zip(obj.data.vertices,obj.data.shape_keys.reference_key.data):
                v.co=base.co
        else:
            for v in obj.data.vertices:
                v.co=sculpt(v.co.copy())
    from dress import seat_waist_bow
    seat_waist_bow(scene,skirt,blouse)
    from hair import sweep_back
    scalp=bpy.data.objects["Hair / continuous wrapped scalp"]
    sweep_back(surface,lock_material,scalp)
    from framing import frame_face
    soften_chin(face,[obj for obj in scene.objects if obj.name=="Soft nose shadow" or
                      obj.get("caelisLayer") in ("acting_ink","acting_iris")])
    fringe=frame_face(surface,lock_material,face,scalp,
                      [bpy.data.objects["Ink pupil."+side] for side in ("L","R")],round_head)
    from face_light import paint_contours
    paint_contours(face,fringe,surface,fabric)
    for obj in scene.objects:
        if obj.type!="MESH" or "deform_group" not in obj:
            continue
        keys=obj.data.shape_keys
        if keys:
            for key in keys.key_blocks:
                for v in key.data:
                    v.co=standing_proportions(v.co)
            for v,base in zip(obj.data.vertices,keys.reference_key.data):
                v.co=base.co
        else:
            for v in obj.data.vertices:
                v.co=standing_proportions(v.co)

    for side in ("L","R"):
        sock=bpy.data.objects["Caelis / ivory sock "+side]
        shin,foot=(sock.vertex_groups[k+"."+side] for k in ("knee","foot"))
        for vertex in sock.data.vertices:
            weight=ease(.088,.147,vertex.co.z)
            shin.add([vertex.index],weight,"REPLACE")
            foot.add([vertex.index],1-weight,"REPLACE")

    bpy.context.view_layer.objects.active=rig
    rig.select_set(True)
    bpy.ops.object.mode_set(mode="EDIT")
    # Snapshot all endpoints first: editing a connected parent can move its child.
    def fit_endpoint(bone,point):
        if bone.name.startswith("tail."):
            return standing_proportions(fit_tail(point))
        p=body(point)
        if bone.name=="head" or bone.name.startswith("hair."):
            p=fit_head(p,bone.name!="head")
        return standing_proportions(p)

    targets=[(b.name,fit_endpoint(b,b.head.copy()),fit_endpoint(b,b.tail.copy()))
             for b in rig.data.edit_bones]
    for name,head,tail in targets:
        b=rig.data.edit_bones[name]
        b.head,b.tail=head,tail
    for side in ("L","R"):
        shin=rig.data.edit_bones["knee."+side]
        foot=rig.data.edit_bones["foot."+side]
        ankle=foot.head.copy()
        ankle.z=.112
        # The template foot and shin are siblings. Make the shoe inherit
        # knee flexion around a joint inside the opening of the Mary Jane.
        shin.tail=ankle
        foot.parent=shin
        foot.head=ankle
        foot.use_connect=True
        foot.tail.y=-.115
        foot.tail.z=.043
    bpy.ops.object.mode_set(mode="OBJECT")

    from dress import fit_stitches
    fit_stitches(surface,skirt,apron,white)
    from acting import assemble_portrait, author_clips
    author_clips(rig,assemble_portrait(scene),standing_proportions)
