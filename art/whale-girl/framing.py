"""The hair framing the resting face, fitted between the two pupil landmarks."""
from math import pi, sin

from mathutils import Vector
from mathutils.bvhtree import BVHTree


# Additional samples near either edge keep the painted contour narrow when
# the ribbon spreads across the forehead. The main volume uses the interior.
PANEL_COLUMNS=(0,.012,.038,.069,.104,.145,.19,.24,.29,.34,.39,.445,.50,
               .555,.61,.66,.71,.76,.81,.855,.896,.931,.962,.988,1)
WISP_COLUMNS=(0,.052,.30,.70,.948,1)


def frame_face(surface, material, face, scalp, eyes, round_head):
    # Paired ribbon edges in the head construction's 97-pixel pupil frame.
    # The original leveled face sets the lower stations and the central part.
    cheek = [(215,80,240,82),(170,99,218,98),(136,138,204,136),
             (115,192,173,176),(113,236,153,224),(117,258,152,244),
             (126,273,155,265),(140,285,160,277),(153,291,164,285),
             (164,292,168,289),(172,291,172.4,290.7)]
    temple = [(220,78,239,79),(187,102,220,91),(161,146,209,122),
              (147,169,203,150),(156,188,189,178),(163,202,179,195),
              (169,215,176,209),(173,224,175,221),(175.5,228,176,227.6)]
    flick = [(170,126,193,121),(145,170,183,158),(131,211,167,200),
             (130,237,154,227),(135,253,155,243),(143,268,158,258),
             (154,275,163,271),(162,278.5,167,276.7),(169,277.5,169.5,277.2)]
    under = [(216,81,237,80),(194,115,229,105),(190,153,233,140),
             (199,192,243,175),(207,217,229,211),(224,233,224.6,232.5)]
    center = [(220,80,242,79),(203,109,259,102),(199,149,274,134),
              (207,184,271,164),(215,207,255,200),(232,224,254,215),
              (248,234,259,229),(265,239,265.6,238.6)]
    # The broad sheets end above the brow. Individual tapered ends part
    # below them, exposing small wedges of forehead between the locks.
    central_wisp=[(245,170,266,163),(246,190,264,184),(243,211,257,202),
                  (235,226,249,220),(225,239,225.6,238.7)]
    inner_wisp=[(208,173,217,172),(207,192,219,191),(211,210,220,206),
                (219,225,223,219),(227,233,227.5,232.4)]

    def reflected(rows):
        return [(496-rx,ry,496-lx,ly) for lx,ly,rx,ry in rows]

    def tree(obj):
        return BVHTree.FromPolygons([v.co for v in obj.data.vertices],
                                    [p.vertices[:] for p in obj.data.polygons])

    shells = [tree(face),tree(scalp)]
    left,right=sorted((sum((v.co for v in eye.data.vertices),Vector())/len(eye.data.vertices)
                       for eye in eyes),key=lambda p:p.x)
    scale=(right.x-left.x)/97
    mid=(left+right)/2
    # A smooth forehead envelope spans the scalp opening. The actual skin
    # and cap set a depth limit at the cheeks and near the center part.
    profile=[(1.13,-.09),(1.22,-.218),(1.32,-.269),(1.43,-.287),
             (1.54,-.273),(1.63,-.254),(1.70,-.216),(1.75,-.153),
             (1.78,-.057),(1.81,.025)]

    def depth(x,z,py):
        y=profile[-1][1]
        profile_z=z+.056
        for (z0,y0),(z1,y1) in zip(profile,profile[1:]):
            if profile_z<=z1:
                y=y0+(y1-y0)*max(0,(profile_z-z0)/(z1-z0))
                break
        y+=1.45*x*x
        y=round_head(Vector((x,y,z))).y
        for i,shell in enumerate(shells):
            point,*_=shell.ray_cast(Vector((x,-1,z)),Vector((0,1,0)))
            if point is not None:
                y=min(y,point.y)
                if i==1 and py<116 and point.y<.16:
                    join=max(0,min(1,(py-81)/35))
                    y=(point.y+.007)*(1-join)+y*join
        return y

    short_part=[(259,81,280,80),(265,108,299,113),(260,143,293,146),
                (251,174,286,172),(253,199,278,196),(257,213,269,211),
                (258,222,262,218),(257,226,257.5,225.5)]
    panels=[("Cheek sweep.R",cheek,.009),("Cheek sweep.L",reflected(cheek),.009),
            ("Cheek flick.R",flick,.021),("Cheek flick.L",reflected(flick),.021),
            ("Temple fringe.R",temple,.013),("Temple fringe.L",reflected(temple),.013),
            ("Parted fringe.R",under,.014),("Parted fringe.L",short_part,.014),
            ("Center fringe",center,.023),("Fringe wisp.center",central_wisp,.016),
            ("Fringe wisp.inner",inner_wisp,.017)]
    fringe=[]
    for name,rows,offset in panels:
        verts,faces=[],[]
        wisp=name.startswith("Fringe wisp")
        steps=5 if wisp else 7
        strips=(len(rows)-1)*steps
        columns=WISP_COLUMNS if wisp else PANEL_COLUMNS
        cols=len(columns)
        for i in range(strips+1):
            j=min(i//steps,len(rows)-2)
            t=i/steps-j
            controls=[rows[max(0,j-1)],rows[j],rows[j+1],rows[min(j+2,len(rows)-1)]]
            lx,ly,rx,ry=[.5*(2*b+(-a+c)*t+(2*a-5*b+4*c-d)*t*t+
                             (-a+3*b-3*c+d)*t**3) for a,b,c,d in zip(*controls)]
            for k,u in enumerate(columns):
                px=lx+(rx-lx)*u
                py=ly+(ry-ly)*u
                x=mid.x+(px-249.5)*scale
                rise=(248-py)*scale
                z=mid.z+rise+.011*max(0,rise/.38)**2
                ridge=sin(pi*u)
                y=depth(x,z,py)-offset*(.12+.88*min(1,i/(strips*.44)))-.003*ridge
                if name.startswith("Cheek"):
                    # Wrap the lower edge around the temple in depth. The
                    # transverse skin section keeps its inner edge above skin.
                    turn=max(0,min(1,(py-147)/94))
                    turn=turn*turn*(3-2*turn)
                    y+=turn*(.110 if name.startswith("Cheek sweep") else .071)
                    side=1 if x>0 else -1
                    front_x=x
                    skin,*_=shells[0].ray_cast(Vector((side,y,z)),Vector((-side,0,0)))
                    if skin is not None:
                        x=side*max(abs(x),abs(skin.x)+turn*(.005+offset*.40))
                    # Let the inner edge come around onto the cheek earlier
                    # than the outer edge. Sample slightly inside the cheek:
                    # the steep skin tangent can cut across a ribbon quad.
                    inner=u if name.endswith(".R") else 1-u
                    tuck=max(0,min(1,(py-163+23*inner)/75))
                    tuck=tuck*tuck*(3-2*tuck)
                    x+=(front_x-x)*tuck
                    skin,*_=shells[0].ray_cast(Vector((x*(1-.046*tuck),-1,z)),Vector((0,1,0)))
                    if skin is not None and tuck>0:
                        tip=max(0,min(1,(py-251)/38))
                        front=skin.y-offset*(1-.60*tip)-.001*ridge
                        y=min(y+(front-y)*tuck,skin.y-.003)
                verts.append((x,y,z))
                if i and k:
                    a=i*cols+k
                    faces.append((a-cols-1,a-cols,a,a-1))
        obj=surface("Hair / "+name,verts,[tuple(reversed(f)) for f in faces],
                    material,"head",0)
        obj["caelisLayer"]="ocean_curls"
        if name.startswith(("Parted fringe","Center fringe","Fringe wisp","Temple fringe")):
            fringe.append(obj)
        edge=obj.modifiers.new("Hair edge thickness","SOLIDIFY")
        edge.thickness=.0014 if name.startswith("Cheek") else .003
        obj.modifiers.move(len(obj.modifiers)-1,0)
    return fringe
