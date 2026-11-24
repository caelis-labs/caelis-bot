"""Shape the bangs and long curls as broad, individually tapered locks."""
from math import cos, pi, sin
from mathutils import Quaternion, Vector


def style_locks(tube, fabric):
    silk=fabric("Whale hair / painted indigo locks","364f91")
    def lock(name,guide,joint,shade):
        # Sample a smooth guide before building rings; the distal taper can
        # turn back on itself without broadening the tip at subdivision.
        rings=[]
        for i,(b,c) in enumerate(zip(guide,guide[1:])):
            a=guide[max(0,i-1)]
            d=guide[min(i+2,len(guide)-1)]
            for step in range(4):
                t=step/4
                rings.append(tuple(.5*(2*q+(-p+r)*t+(2*p-5*q+4*r-s)*t*t+
                            (-p+3*q-3*r+s)*t**3) for p,q,r,s in zip(a,b,c,d)))
        rings.append(guide[-1])
        obj=tube("Hair / "+name,rings,silk,joint)
        obj["caelisLayer"]="ocean_curls"
        for v in obj.data.vertices:
            row,col=divmod(v.index,12)
            t=row/(len(rings)-1)
            center=Vector(rings[row][:3])
            tangent=(Vector(rings[min(row+1,len(rings)-1)][:3])-
                     Vector(rings[max(0,row-1)][:3])).normalized()
            across_axis=tangent.cross(Vector((0,1,0))).normalized()
            front=tangent.cross(across_axis)
            a=2*pi*col/12
            width,thickness=rings[row][3:]
            width*=.73-.13*t
            depth=thickness*.48*sin(a)
            blade=across_axis*width*cos(a)+front*depth
            twist=.46*sin(pi*t*1.4+shade*.51)
            v.co=center+Quaternion(tangent,twist) @ blade

    for side,sign in (("L",1),("R",-1)):
        sidefall=[(.34,.062,1.57,.062),(.42,.075,1.40,.105),(.46,.084,1.23,.116),
                  (.51,.108,1.09,.122),(.60,.122,.965,.110),(.65,.126,.85,.075),
                  (.642,.112,.775,.039),(.598,.094,.722,.015),(.570,.078,.719,.002)]
        collar=[(.32,-.05,1.48,.065),(.353,-.112,1.32,.088),(.339,-.150,1.20,.091),
                (.291,-.174,1.10,.080),(.275,-.187,1.011,.065),(.304,-.195,.934,.047),
                (.344,-.199,.879,.029),(.351,-.192,.830,.013),(.339,-.177,.808,.002)]
        for label,path,shade in (("Lateral wave",sidefall,-1),("Collar wave",collar,1)):
            guide=[]
            for j,(x,y,z,w) in enumerate(path):
                t=j/(len(path)-1)
                guide.append((sign*(x+.020*sign*sin(pi*t)*t),y+.015*sign*t,z+.019*sign*t*t,w,w*.38))
            lock(label+"."+side,guide,"hair."+side,shade)
    return silk


def sweep_back(surface,material,scalp):
    """A folded mantle follows the occiput, with overlapping tips only at the hem."""
    from mathutils.bvhtree import BVHTree
    cap=BVHTree.FromPolygons([v.co for v in scalp.data.vertices],[p.vertices[:] for p in scalp.data.polygons])
    def mantle(t,u):
        a=u*1.93
        bell=t*t*(3-2*t)
        phase=7.4*a+.75*sin(2*a)+.88*sin(2*pi*t+1.2*a)
        flute=cos(phase)
        split=(.5+.5*flute)**1.55
        hem=.836+.052*abs(u)+.038*cos(3*a+.5)-.077*split
        z=1.67+(hem-1.67)*t
        wave=sin(2.3*pi*t+.72*a)
        radius=.237+.185*bell+.045*wave*t+.063*t*t
        axis=.045+.104*bell
        depth=.178+.094*bell+.038*wave*t
        relief=(.008+.022*bell)*flute
        p=Vector(((radius+relief)*sin(a)+.025*t*sin(2*a+5*t),
                  axis+(depth+relief)*cos(a),z))
        radial=Vector((sin(a),cos(a),0))
        if z>1.29:
            center=Vector((0,.045,z))
            hit=cap.ray_cast(center+radial,-radial)[0]
            if hit is not None:
                w=min(1,(z-1.29)/.17)
                root=min(1,max(0,(z-1.48)/.14))
                lift=.003+.003*flute-.011*root
                p+=radial*((hit-p).dot(radial)+lift)*w*w*(3-2*w)
        return p,radial

    def panel(name,rows,cols,axis=None):
        verts,faces,weights=[],[],[]
        for j in range(rows):
            t=j/(rows-1)
            for i in range(cols):
                across=2*i/(cols-1)-1
                if axis is None:
                    u=across
                    p,_=mantle(t,u)
                else:
                    sweep=.072*sin(2*pi*t+2.3*axis)*t
                    width=.125*sin(pi*(.11+.89*t))**.76+.001
                    u=axis+sweep+across*width
                    p,radial=mantle(.13+.87*t,u)
                    p+=radial*(.003+.021*(1-across*across)*sin(pi*t)**.72)
                    p.z-=(.078+.030*cos(6*axis))*t**3
                    p.x+=(.040*sin(6*axis+1.2)+.045*axis)*t**4
                verts.append(p)
                weights.append((max(0,min(1,(1.45-p.z)/.35)),min(1,abs(u))**3,u))
                if j and i:
                    k=len(verts)-1
                    faces.append((k-cols-1,k-cols,k,k-1))
        obj=surface("Hair / "+name,verts,faces,material,"head",0)
        obj["caelisLayer"]="back_hair"
        if axis is None:
            edge=obj.modifiers.new("Folded lower edge","SOLIDIFY")
            edge.thickness=.010
            obj.modifiers.move(len(obj.modifiers)-1,0)
        follow=obj.vertex_groups.new(name="hair.back")
        sides=[obj.vertex_groups.new(name="hair."+label) for label in ("R","L")]
        for i,(w,side,u) in enumerate(weights):
            obj.vertex_groups["head"].add([i],1-w,"REPLACE")
            follow.add([i],w*(1-side),"REPLACE")
            sides[int(u>0)].add([i],w*side,"REPLACE")

    panel("Continuous waved mantle",37,105)
    for i,axis in enumerate((-.85,-.56,-.29,-.035,.24,.53,.83)):
        panel("Mantle fold "+str(i+1),32,9,axis)
