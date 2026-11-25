"""Cut the independent bodice and stockings on the bound figure and fit the sleeves."""
import bpy
import bmesh
from math import cos, pi, sin
from mathutils import Vector, geometry
from mathutils.bvhtree import BVHTree
from dress import SLEEVE_SECTIONS
from tailoring import cut_sleeve


def clip(bm,normal,distance):
    n=Vector(normal)
    bmesh.ops.bisect_plane(bm,geom=list(bm.verts)+list(bm.edges)+list(bm.faces),
                           dist=1e-7,plane_co=n*distance,plane_no=n,clear_outer=True)


def shell(figure, garment, cuts, allowance):
    """Fit a removable pattern on the skin, including the weights at cut edges."""
    source=figure.copy()
    source.data=figure.data.copy()
    garment.users_collection[0].objects.link(source)
    for key in ('deform_group','caelisLayer'):source[key]=garment[key]
    source.data.materials.clear()
    source.data.materials.append(garment.data.materials[0])
    bm=bmesh.new()
    bm.from_mesh(source.data)
    for normal,distance in cuts:clip(bm,normal,distance)
    bm.normal_update()
    for v in bm.verts:v.co+=allowance*v.normal
    bm.to_mesh(source.data)
    bm.free()
    name=garment.name
    bpy.data.objects.remove(garment,do_unlink=True)
    source.name=name
    source.data.name=name+' / fitted pattern'
    source.data.update()
    return source


def replace_outline(garment, old, width):
    outline=garment.copy()
    outline.data=garment.data.copy()
    old.users_collection[0].objects.link(outline)
    outline['caelisLayer']=old['caelisLayer']
    outline.data.materials.clear()
    outline.data.materials.append(old.data.materials[0])
    normals=[v.normal.copy() for v in outline.data.vertices]
    for v,n in zip(outline.data.vertices,normals):v.co+=width*n
    outline.data.flip_normals()
    outline.data.update()
    name=old.name
    bpy.data.objects.remove(old,do_unlink=True)
    outline.name=name


def fit_outfit(scene,figure):
    mesh=figure.data
    skin=BVHTree.FromPolygons([v.co for v in mesh.vertices],[p.vertices[:] for p in mesh.polygons])

    def follow_skin(obj,vertex):
        hit,_,index,_=skin.find_nearest(vertex.co)
        face=mesh.polygons[index]
        a,b,c=[mesh.vertices[i] for i in face.vertices]
        bary=geometry.barycentric_transform(hit,a.co,b.co,c.co,Vector((1,0,0)),Vector((0,1,0)),Vector((0,0,1)))
        weights={}
        for basis,corner in zip(bary,(a,b,c)):
            for group in corner.groups:
                name=figure.vertex_groups[group.group].name
                weights[name]=weights.get(name,0)+max(0.,basis)*group.weight
        active=sorted(weights.items(),key=lambda pair:-pair[1])[:4]
        total=sum(w for _,w in active)
        for index in [g.group for g in vertex.groups]:
            obj.vertex_groups[index].remove([vertex.index])
        for name,w in active:
            if w<.00001:continue
            group=obj.vertex_groups.get(name) or obj.vertex_groups.new(name=name)
            group.add([vertex.index],w/total,'REPLACE')

    old=scene.objects['Caelis / connected blouse and sleeves']
    old_bodice=BVHTree.FromPolygons([v.co for v in old.data.vertices],
                                   [p.vertices[:] for p in old.data.polygons])
    bodice=shell(figure,old,[((0,0,-1),-.690),((0,0,1),1.006),
                              ((1,0,0),.239),((-1,0,0),.239)],.007)
    # Cut the neck opening between the strap seams, retaining the shoulder
    # tops that carry the apron when the arms move.
    pattern=bmesh.new()
    pattern.from_mesh(bodice.data)
    for slope,base in [(-.80,.909),(.80,.909),(0,.944)]:
        bmesh.ops.bisect_plane(pattern,geom=list(pattern.verts)+list(pattern.edges)+list(pattern.faces),
                              dist=1e-7,plane_co=(0,0,base),plane_no=(slope,0,1))
    opening=[]
    for face in pattern.faces:
        center=face.calc_center_median()
        if center.z>max(.944,.909+.80*abs(center.x)):
            opening.append(face)
    bmesh.ops.delete(pattern,geom=opening,context='FACES')
    pattern.to_mesh(bodice.data)
    pattern.free()
    bodice.data.update()
    new_bodice=BVHTree.FromPolygons([v.co for v in bodice.data.vertices],
                                   [p.vertices[:] for p in bodice.data.polygons])
    trims=('Pintucked shirt bib','Bib button','Pinafore shoulder frill','Velvet waist panel',
           'Waist button','Caelis / garden collar','Collar bow','Knotted collar ribbon')
    for obj in scene.objects:
        if not obj.name.startswith(trims):continue
        for v in obj.data.vertices:
            if obj.name.startswith('Caelis / garden collar'):
                v.co.z-=.021
            x,y,z=v.co
            start=(x,-1,z)
            before=old_bodice.ray_cast(start,(0,1,0))[0]
            after=new_bodice.ray_cast(start,(0,1,0))[0]
            if before is not None and after is not None:
                v.co.y+=min(0.,after.y-before.y-.0015)
            follow_skin(obj,v)
        obj.data.update()

    collar=scene.objects['Gathered standing collar']
    for v in collar.data.vertices:
        station,row=divmod(v.index,4)
        angle=2*pi*station/48
        radial=Vector((cos(angle),sin(angle),0))
        front=max(0.,-radial.y)
        heights=(.950,.956,.997,.9975)
        gap=(.014,.017,.008,.004)
        z=heights[row]-.008*front**3+.0018*cos(12*angle)*(row/3)
        origin=Vector((0,.021,z))
        hit=skin.ray_cast(origin,radial)[0]
        if hit is None: raise ValueError('Collar missed the bound neckline')
        # A low lateral ray can reach the shoulder beyond the neckline seam.
        radius=min((hit-origin).length+gap[row],(.128,.128,.104,.101)[row])
        v.co=origin+radial*(radius+.0015*cos(12*angle))
    collar.data.update()

    for side,sign in [('L',1),('R',-1)]:
        sock_name='Caelis / ivory sock '+side
        sock=shell(figure,scene.objects[sock_name],[((0,0,1),.424),((-sign,0,0),0)],.0038)
        replace_outline(sock,scene.objects['Contour / '+sock_name],.0014)
        bpy.data.objects.remove(scene.objects['Stocking instep.'+side],do_unlink=True)
        sleeve=scene.objects['Gathered sleeve and lace.'+side]
        cut_sleeve(sleeve)
        vertices=list(sleeve.data.vertices)
        for i in range(0,len(vertices),SLEEVE_SECTIONS):
            row=vertices[i:i+SLEEVE_SECTIONS]
            center=sum((v.co for v in row),Vector())/len(row)
            for v in row:
                radial=(v.co-center).normalized()
                hit=skin.ray_cast(v.co+radial*.08,-radial,.145)[0]
                if hit is not None:
                    ease=.005 if i<len(vertices)-2*SLEEVE_SECTIONS else .003
                    v.co+=radial*min(.045,max(0.,(hit-v.co).dot(radial)+ease))
                follow_skin(sleeve,v)
        sleeve.data.update()
        replace_outline(sleeve,scene.objects['Contour / '+sleeve.name],.0025)
