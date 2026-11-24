"""Build painted, curved facial layers on the head surface in its bind pose."""
from math import cos, exp, pi, sin, sqrt
from mathutils import Vector


EYE_CENTER=.172
EYE_RADIUS=.1075


def lids(x):
    u=max(-1,min(1,(x-EYE_CENTER)/EYE_RADIUS))
    arc=sqrt(max(0,1-u*u))
    corner=1.338-.004*u+.009*max(0,-u)**3
    return corner-.070*arc, corner+.067*arc*(1-.19*u-.045*u*u)


def lash_rim(t):
    x=EYE_CENTER+(2*t-1)*EYE_RADIUS
    z=lids(x)[1]
    w=max(0,sin(pi*t))**.68
    thickness=(.010+.036*t)*w*(.87+.13*sin(pi*t)**2)
    return ((x+.005*w*t,z+.63*thickness),(x,z-.37*thickness))


def paint_face(face, surface, fabric):
    ink = fabric("Indigo lash line", "302339")
    pearl = fabric("Porcelain sclera", "fff8f5")
    sclera_shader=pearl.node_tree.nodes.get("Principled BSDF")
    sclera_shader.inputs["Emission Color"].default_value=(.38,.365,.36,1)
    sclera_shader.inputs["Emission Strength"].default_value=1
    white=fabric("Warm ivory sclera","fff9f5")
    iris = fabric("Graduated sapphire", "3971be")
    def painted_color(material, layer):
        # Preserve the painted value range in both Blender and the portable
        # preview, including the underside of the smile in shadow.
        tree=material.node_tree
        color=tree.nodes.new("ShaderNodeVertexColor")
        color.layer_name=layer
        tree.links.new(color.outputs["Color"],tree.nodes.get("Material Output").inputs["Surface"])

    painted_color(iris,"Iris")
    painted_color(white,"Face paint")

    def oval(x,z,w,h):
        return [(x+w*cos(i*pi/24), z+h*sin(i*pi/24)) for i in range(48)]

    def disc(name, outline, material, offset, side=1, rings=12):
        # Each ring is projected independently, so the eyelid, iris and gleams
        # follow the cheek in profile as well as in the front view.
        cx=sum(x for x,z in outline)/len(outline)
        cz=sum(z for x,z in outline)/len(outline)
        vertices=[]
        faces=[]
        count=len(outline)
        for ring in range(rings+1):
            fraction=ring/rings
            for i,(x,z) in enumerate(outline if ring else [(cx,cz)]):
                x=side*(cx+(x-cx)*fraction)
                z=cz+(z-cz)*fraction
                hit,p,_,_=face.ray_cast(Vector((x,-1,z)),Vector((0,1,0)))
                if not hit:
                    raise ValueError(f"{name}: missed facial surface")
                vertices.append((x,p.y-offset,z))
                if ring:
                    current=1+(ring-1)*count+i
                    nxt=1+(ring-1)*count+(i+1)%count
                    poly=(0,current,nxt) if ring==1 else (current-count,current,nxt,nxt-count)
                    faces.append(poly if side==1 else tuple(reversed(poly)))
        return surface(name,vertices,faces,material,"head",0)

    def lash_strip(name,edges,material,offset,sign):
        verts=[]
        faces=[]
        for i,pair in enumerate(edges):
            for x,z in pair:
                hit,p,_,_=face.ray_cast(Vector((sign*x,-1,z)),Vector((0,1,0)))
                if not hit:
                    raise ValueError(f"{name} outside cheek at {(x,z)}")
                verts.append((sign*x,p.y-offset,z))
            if i:
                k=2*i
                quad=(k-2,k-1,k+1,k)
                faces.append(quad if sign==1 else tuple(reversed(quad)))
        return surface(name,verts,faces,material,"head",0)

    # Lay a warm ivory ground under the eyes; tint the cheek at the outer
    # corner and keep the nose and chin clear of the blush.
    # The skin, like the sclera and iris, carries its illustrated values in
    # sRGB. Add the broad turning planes in paint so bright preview lights
    # cannot clip the blush or erase the separation from the white collar.
    painted_color(face.data.materials["Peach blush gradient"],"Color")
    skin=face.data.color_attributes["Color"]
    for vertex in face.data.vertices:
        x,y,z=vertex.co
        # Just outside the iris, the center of the painted cheek is still
        # visible between the lower lid and the inward curl of the hair.
        front=max(0,min(1,(-y-.035)/.10))
        blush=front*exp(-((abs(x)-.243)/.079)**2-((z-1.259)/.043)**2)
        flush=front*exp(-((abs(x)-.180)/.168)**2-((z-1.252)/.092)**2)
        nose=front*exp(-(x/.032)**2-((z-1.258)/.030)**2)
        normal=vertex.normal
        turn=max(0., (abs(normal.x)-.5)*1.6, -normal.z-.35, normal.y)
        skin.data[vertex.index].color_srgb=(
            (254-8*turn)/255, (245-26*blush-10*flush-15*turn-5*nose)/255,
            (243-25*blush-9*flush-15*turn-4*nose)/255, 1)


    disc("Soft nose shadow",oval(0,1.273,.0044,.0022),fabric("Peach nose tint","efcfc0"),.005)
    brow=fabric("Dusty indigo brow","73657e")
    crease=fabric("Warm lid crease","cda9a1")
    for side,sign in (("L",1),("R",-1)):
        brow_line=[]
        for i in range(33):
            t=i/32
            x=.088+.201*t
            z=1.551+.017*sin(pi*t)-.055*t**1.7
            thickness=.00018+.00165*sin(pi*t)**.76*(1.32-.68*t)
            brow_line.append(((x,z+thickness),(x,z-thickness)))
        # A ribbon keeps both edges of the thin arch together. A radial
        # fan crosses the concave underside and fills in the eyebrow.
        lash_strip("Arched brow."+side,brow_line,brow,.006,sign)
        upper=[]
        for i in range(65):
            x=EYE_CENTER-EYE_RADIUS*cos(pi*i/64)
            upper.append((x,lids(x)[1]))
        outline=upper[::-1]+[(x,lids(x)[0]) for x,_ in upper[1:-1]]
        ground=disc("Oval sclera."+side,outline,white,.009,sign)
        sclera=ground.data.color_attributes.new(name="Face paint",type="FLOAT_COLOR",domain="POINT")
        for v in ground.data.vertices:
            x=abs(v.co.x)
            lo,hi=lids(x)
            height=(v.co.z-lo)/max(.001,hi-lo)
            outer=(x-EYE_CENTER+EYE_RADIUS)/(2*EYE_RADIUS)
            shade=max(0,min(1,(height-.47)/.47))**1.3*(.40+.60*outer)
            tint=Vector((.99,.952,.948)).lerp(Vector((.70,.64,.74)),shade)
            sclera.data[v.index].color=(*tint,1)
        fold=[]
        for i in range(41):
            t=i/40
            x=.136+.113*t
            u=(x-EYE_CENTER+EYE_RADIUS)/(2*EYE_RADIUS)
            z=lash_rim(u)[0][1]+.007+.002*sin(pi*t)
            width=.00012+.00135*sin(pi*t)
            fold.append(((x,z+width),(x,z-width)))
        lash_strip("Lid crease."+side,fold,crease,.012,sign)
        # Crop the painted disk at the lid margins so the iris sits under
        # the lashes, even at the quiet standing pose.
        iris_width=.0708
        iris_upper=.087
        iris_lower=.072
        top,bottom=[],[]
        for i in range(65):
            angle=pi*i/64
            x=EYE_CENTER-.0045-iris_width*cos(angle)
            lo,hi=lids(x)
            top.append((x,min(hi,1.338+iris_upper*sin(angle))))
            bottom.append((x,max(lo+.0007,1.338-iris_lower*sin(angle))))
        shape=top[::-1]+bottom[1:-1]
        obj=disc("Sapphire iris."+side,shape,iris,.014,sign,rings=24)
        obj["caelisLayer"]="painted_irises"
        colors=obj.data.color_attributes.new(name="Iris",type="FLOAT_COLOR",domain="POINT")
        iris_shadow=Vector((.025,.034,.134))
        sapphire=Vector((.113,.181,.459))
        sky=Vector((.234,.440,.783))
        ice=Vector((.535,.800,.953))
        def transition(value,softness):
            t=max(0,min(1,.5+value/softness))
            return t*t*(3-2*t)
        for v in obj.data.vertices:
            x,y,z=v.co
            dx=(abs(x)-EYE_CENTER+.0045)/iris_width
            dz=(z-1.338)/(iris_upper if z>1.338 else iris_lower)
            r=sqrt(dx*dx+dz*dz)
            # Three nested upward arcs fill the lower iris in Eye Detail.
            # Place the wavy overhead reflection separately, above the pupil.
            tone=iris_shadow.lerp(sapphire,transition(.15-.57*dx*dx-dz,.16))
            tone=tone.lerp(sky,transition(-.20-.60*dx*dx-dz,.13))
            lens=((dx+.025)/.67)**2+((dz+.91)/.48)**2
            tone=tone.lerp(ice,transition(1-lens,.27))
            cap=((dx+.10)/.79)**2+((dz-.57)/.36)**2
            crest=.31+.055*sin(6*dx)-.065*dx
            reflection=transition(1-cap,.45)*transition(dz-crest,.11)
            tone=tone.lerp(Vector((.095,.121,.325)),reflection)
            edge=transition(r-.94,.12)*(.48+.28*max(0,dz))
            tone=tone.lerp(iris_shadow,edge)
            colors.data[v.index].color=(*tone,1)
        pupil=fabric("Violet pupil "+side,"32407c")
        disc("Ink pupil."+side,oval(.1675,1.349,.018,.032),pupil,.015,sign)
        glow=fabric("Submerged pupil light "+side,"9ac6ee")
        painted_color(glow,"Face paint")
        facet=disc("Pupil glimmer."+side,oval(.1675,1.345,.0094,.016),glow,.017,sign)
        light=facet.data.color_attributes.new(name="Face paint",type="FLOAT_COLOR",domain="POINT")
        for v in facet.data.vertices:
            t=transition(1.345-v.co.z,.030)
            tint=Vector((.19,.34,.65)).lerp(Vector((.51,.77,.95)),t)
            light.data[v.index].color=(*tint,1)
        # The portrait's soft upper gleam has a smaller satellite just
        # below it. The lower blue pool supplies the opposite reflection.
        gx=.1635
        gleam=[(x+.23*(z-1.375),z) for x,z in oval(gx,1.375,.0107,.0071)]
        disc("Key reflection."+side,gleam,pearl,.020,sign)
        disc("Lower reflection."+side,oval(gx+.006,1.362,.0035,.0025),pearl,.020,sign)
        # The plum edge dissolves around the outer corner. A separate
        # tapered stroke swept from the upper arch gives the tip its lift.
        corner=[]
        for i in range(41):
            t=i/40
            angle=1.06-1.44*t
            x=EYE_CENTER+EYE_RADIUS*cos(angle)
            z=lids(x)[1 if angle>=0 else 0]
            thickness=.016*sin(pi*t)**.88
            point=max(0,1-abs(t-.62)/.34)
            corner.append(((x+thickness+.020*point,z+.26*thickness+.008*point),
                           (x-.47*thickness,z-.18*thickness)))
        plum=fabric("Plum outer lash "+side,"583847")
        painted_color(plum,"Face paint")
        canthus=lash_strip("Outer canthus."+side,corner,plum,.022,sign)
        pigment=canthus.data.color_attributes.new(name="Face paint",type="FLOAT_COLOR",domain="POINT")
        for v in canthus.data.vertices:
            t=(v.index//2)/(len(corner)-1)
            tint=Vector((.033,.019,.033)).lerp(Vector((.135,.040,.047)),sin(pi*t)**.8)
            pigment.data[v.index].color=(*tint,1)
        # The lower edge of the flick grows out of the descending lash.
        # The short tapered point leaves the middle of the arc continuous.
        wing=[]
        start=.855
        z0=lash_rim(start)[0][1]
        for i in range(17):
            t=i/16
            x=EYE_CENTER+EYE_RADIUS*(2*(start+.115*t)-1)
            crest=z0-.001+.0055*t**1.7
            width=.009*(1-t)**.83
            wing.append(((x,crest),(x,crest-width)))
        flick=lash_strip("Swept upper lash tip."+side,wing,ink,.0245,sign)
        lower=[]
        for i in range(31):
            t=i/30
            angle=-.11-1.00*t
            x=EYE_CENTER+EYE_RADIUS*cos(angle)
            z=lids(x)[0]
            w=.00018+.0022*sin(pi*t)**.78
            lower.append(((x+.28*w,z+w),(x-.28*w,z-w)))
        lash_strip("Outer lower lid."+side,lower,fabric("Rosewood lower lid "+side,"a07478"),.013,sign)
        lash=[lash_rim((x-EYE_CENTER+EYE_RADIUS)/(2*EYE_RADIUS)) for x,z in upper]
        arch=lash_strip("Swept upper lash."+side,lash,ink,.024,sign)
        for strip in (arch,flick):
            col=strip.data.color_attributes.new(name="Face paint",type="FLOAT_COLOR",domain="POINT")
            for v in strip.data.vertices:
                u=(abs(v.co.x)-EYE_CENTER)/EYE_RADIUS
                warm=min(1,abs(u)**1.65)
                tint=Vector((.021,.016,.031)).lerp(Vector((.145,.045,.054)),warm)
                col.data[v.index].color=(*tint,1)
    painted_color(ink,"Face paint")

    # A shallow upper bow and standing sidewalls descend into a round cup.
    # Draw the tongue onto the cavity so the light lower rim follows the lip.
    smile=[]
    for i in range(64):
        a=2*pi*i/64
        u,v=cos(a),sin(a)
        z=.038*v**.83 if v>0 else -.069*abs(v)**.88
        smile.append((.073*(1 if u>=0 else -1)*abs(u)**.82*(1+.17*v),1.214+z+.0065*u*u))
    lip=fabric("Warm lip tint","ab5e61")
    inside=fabric("Smile interior","ffd0c1")
    rim=disc("Coral smile",smile,lip,.008)
    lip_color=rim.data.color_attributes.new(name="Face paint",type="FLOAT_COLOR",domain="POINT")
    for v in rim.data.vertices:
        high=max(0,min(1,(v.co.z-1.187)/.054))
        rim_tone=Vector((.49,.205,.172)).lerp(Vector((.28,.085,.079)),high)
        corner=min(1,abs(v.co.x/.073)**3)*high
        rim_tone=rim_tone.lerp(Vector((.22,.065,.065)),corner)
        lip_color.data[v.index].color=(*rim_tone,1)
    painted_color(lip,lip_color.name)
    # Paint roof, corners and tongue on one continuous opening. The soft
    # boundary follows the tongue's dome and falls away into each corner.
    verts=[]
    faces=[]
    n=48
    rows=20
    tones=[]
    edges=list(zip(smile,smile[1:]+smile[:1]))
    extent=max(x for x,z in smile)
    for i in range(n+1):
        u=(2*i/n-1)*.9999
        x=u*extent
        hits=[az+(bz-az)*(x-ax)/(bx-ax) for (ax,az),(bx,bz) in edges
              if min(ax,bx)<=x<max(ax,bx)]
        bottom,top=min(hits),max(hits)
        for j in range(rows+1):
            height=j/rows
            z=bottom+(top-bottom)*height
            xx=x*(.922+.018*(1-height)**2)
            zz=1.214+(z-1.214)*(.949-.016*height)-.0008*height**3
            hit,p,_,_=face.ray_cast(Vector((xx,-1,zz)),Vector((0,1,0)))
            if not hit:
                raise ValueError("Smile opening outside lower cheek")
            verts.append((xx,p.y-.011,zz))
            tongue=max(0,min(1,(.83-.48*u*u+.025*u-height)/.14+.5))
            tongue=tongue*tongue*(3-2*tongue)
            corner=abs(u)**3*height
            roof=Vector((.82,.263,.218)).lerp(Vector((.245,.051,.050)),corner)
            petal=Vector((1,.64,.58)).lerp(Vector((.98,.40,.38)),height*.60+abs(u)*.23)
            tones.append((*roof.lerp(petal,tongue),1))
            if i and j:
                k=len(verts)-1
                faces.append((k-rows-2,k-1,k,k-rows-1))
    cavity=surface("Smile opening",verts,faces,inside,"head",0)
    paint=cavity.data.color_attributes.new(name="Face paint",type="FLOAT_COLOR",domain="POINT")
    for vertex,rgba in zip(paint.data,tones):
        vertex.color=rgba
    painted_color(inside,paint.name)
    # A single small milk tooth sits below the upper rim, off the midline.
    def roof_height(x):
        rim=max(az+(bz-az)*(x/.922-ax)/(bx-ax) for (ax,az),(bx,bz) in edges
                if min(ax,bx)<=x/.922<max(ax,bx))
        return 1.214+(rim-1.214)*.933-.0008
    tooth=[(x,roof_height(x)-depth) for x,depth in
           [(-.045,0),(-.042,.008),(-.038,.015),(-.036,.016),
            (-.033,.013),(-.028,0),(-.036,-.0004)]]
    disc("Smile canine",tooth,
         fabric("Ivory milk tooth","fff3df"),.014,rings=4)
