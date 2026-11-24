"""Fit the continuous clay study in the standing pose, then bind it to the acting rig."""
from math import cos, exp, radians, sin
from pathlib import Path

import bpy
from mathutils import Matrix, Vector


def smooth(low, high, value):
    t = min(1., max(0., (value - low) / (high - low)))
    return t*t*(3-2*t)


def frame(along, across):
    along = along.normalized()
    across = (across-along*along.dot(across)).normalized()
    return Matrix((along, across, along.cross(across))).transposed()


class HandFit:
    """Landmarks on the gray sculpt and on the corresponding animated hand."""
    def __init__(self, rig, side):
        self.sign = 1 if side == 'L' else -1
        self.side = side
        self.wrist = Vector((.334,-.019,.615))
        self.across = Vector((cos(radians(56)),sin(radians(56)),0))
        self.normal = Vector((self.across.y,-self.across.x,0))
        self.guides = {}
        for name, lateral, length in [('index',-.033,.063),('middle',-.011,.071),
                                      ('ring',.012,.066),('pinky',.033,.049)]:
            start = Vector((.355,-.029,.548+.09*lateral)) + lateral*self.across
            end = (start + Vector((.022,-.007,-length))
                   + .40*lateral*self.across - .012*self.normal)
            self.guides[name] = (start,end)
        self.guides['thumb'] = (Vector((.337,-.051,.572)),Vector((.352,-.096,.511)))
        def landmark(name):
            bone = rig.pose.bones[name+'.'+side]
            return self.reflect(bone.head)
        self.target_wrist = landmark('hand')
        self.target_elbow = landmark('forearm')
        self.target_shoulder = landmark('upper_arm')
        base = frame(self.guides['middle'][0]-self.wrist,self.across)
        target = frame(landmark('middle.01')-self.target_wrist,
                       landmark('pinky.01')-landmark('index.01'))
        self.rotation = target @ base.transposed()

    def reflect(self, p):
        return Vector((self.sign*p.x,p.y,p.z))

    def palm(self,p):
        # Seat the thumb pad on the palmar side and ease the displacement
        # through the thenar eminence. The same map locates its three pivots.
        start,end=self.guides['thumb']
        axis=end-start
        t=max(0.,min(1.,(p-start).dot(axis)/axis.length_squared))
        distance=(p-start-axis*t).length
        pad=(1-smooth(.016,.045,distance))*smooth(.01,.75,t)
        return self.target_wrist + self.rotation @ (p-self.wrist-.018*pad*self.normal)

    def place_phalanges(self,bones):
        # Gray knuckles have different spacing and finger lengths from the
        # original mitten. Put the pivots inside their respective pads.
        for digit,(start,end) in self.guides.items():
            segments=('00','01','02') if digit=='thumb' else ('01','02','03')
            points=[self.reflect(self.palm(start.lerp(end,i/3))) for i in range(4)]
            for i,segment in enumerate(segments):
                bone=bones[digit+'.'+segment+'.'+self.side]
                up=bone.matrix.to_3x3().col[2].copy()
                bone.head,bone.tail=points[i],points[i+1]
                bone.align_roll(up)
            if digit!='thumb':
                metacarpal=bones[digit+'.00.'+self.side]
                metacarpal.tail=points[0]

    def fit(self,p):
        # The same smooth binding moves the shoulders, palms and finger pads
        # into the reference frame without introducing a seam at the wrist.
        z=p.z
        shoulder = self.target_shoulder-Vector((.183,.021,.935))
        elbow = self.target_elbow-Vector((.279,.014,.774))
        offset = elbow.lerp(shoulder,smooth(.77,.94,z))
        offset = (self.target_wrist-self.wrist).lerp(offset,smooth(.615,.774,z))
        posed = (p+offset).lerp(self.palm(p),1-smooth(.614,.696,z))
        elbow_mix = 1-smooth(.716,.827,z)
        hand_mix = 1-smooth(.573,.650,z)
        weights = {'upper_arm':1-elbow_mix,'forearm':elbow_mix*(1-hand_mix),'hand':hand_mix}
        if z < .587:
            scores = []
            for name,(start,end) in self.guides.items():
                axis=end-start
                t=(p-start).dot(axis)/axis.length_squared
                closest=start+axis*min(1.,max(0.,t))
                scores.append(((p-closest).length_squared,name,t))
            nearest=min(row[0] for row in scores)
            falloff=[exp(-(d-nearest)/.000095) for d,_,_ in scores]
            total=sum(falloff)
            for (_,name,t),affinity in zip(scores,falloff):
                share=affinity/total
                start_blend=smooth(-.26,.47,t) if name=='thumb' else smooth(-.13,.13,t)
                finger=share*start_blend*hand_mix
                weights['hand']-=finger
                stops=('00','01','02') if name=='thumb' else ('01','02','03')
                segment=max(0.,min(2.,3*t-.45))
                i=int(segment)
                for j,amount in ((i,1-(segment-i)),(min(i+1,2),segment-i)):
                    key=name+'.'+stops[j]
                    weights[key]=weights.get(key,0)+amount*finger
        return self.reflect(posed), {k+'.'+self.side:w for k,w in weights.items() if w>0}


def body_point(p,side):
    x,y,z=p
    neck=smooth(.952,1.044,z)
    # Allow room for the shirt and neck band. The hip and abdominal volume
    # below the bodice come from the approved study.
    bodice=smooth(.68,.854,z)
    x*=1-.046*bodice-.21*neck
    y=.021+(y-.014)*(1-.18*bodice-.15*neck)+.018*neck
    z-=.013*neck
    # The barefoot sole sits inside the shoe. Ankle, instep and toe share
    # the same continuous surface as the calf.
    leg=1-smooth(.349,.477,z)
    x+=(side*.118+(x-side*.114)*.79-x)*leg
    y+=(.020+(y-.011)*.79-y)*leg
    z+=.020*(1-smooth(.015,.125,z))
    chest=smooth(.625,.826,z)
    thigh=(1-smooth(.428,.61,z))*smooth(.005,.066,abs(x))
    calf=1-smooth(.264,.358,z)
    foot=1-smooth(.078,.144,z)
    suffix='.L' if side==1 else '.R'
    weights={'chest':chest,'pelvis':(1-chest)*(1-thigh),
             'leg'+suffix:(1-chest)*thigh*(1-calf),
             'knee'+suffix:(1-chest)*thigh*calf*(1-foot),
             'foot'+suffix:(1-chest)*thigh*calf*foot}
    return Vector((x,y,z)),weights


def compact_arms(scene, rig):
    """Shorten the rest segments and carry each weighted surface with its joint."""
    maps={}
    for side in ('L','R'):
        upper,fore,hand=[rig.data.bones[name+'.'+side] for name in ('upper_arm','forearm','hand')]
        shoulder,elbow,wrist=[b.head_local.copy() for b in (upper,fore,hand)]
        new_elbow=shoulder+(elbow-shoulder)*.84
        new_wrist=new_elbow+(wrist-elbow)*.78
        for bone in (upper,*upper.children_recursive):
            if bone==upper or bone.name.startswith(('forearm.','forearm_twist.')):
                a,b,origin,ratio=(shoulder,elbow,shoulder,.84) if bone==upper else (elbow,wrist,new_elbow,.78)
                axis=(b-a).normalized()
                linear=Matrix.Identity(3)
                for i in range(3):
                    for j in range(3):
                        linear[i][j]+=(ratio-1)*axis[i]*axis[j]
            else:
                a,origin=wrist,new_wrist
                linear=Matrix.Identity(3)*.89
            transform=linear.to_4x4()
            transform.translation=origin-linear@a
            maps[bone.name]=transform

    for obj in scene.objects:
        if obj.type!='MESH' or obj.find_armature()!=rig:
            continue
        bind=rig.matrix_world.inverted() @ obj.matrix_world
        transforms={g.index:bind.inverted() @ maps[g.name] @ bind
                    for g in obj.vertex_groups if g.name in maps}
        if not transforms:continue
        for v in obj.data.vertices:
            start=v.co.copy()
            for group in v.groups:
                transform=transforms.get(group.group)
                if transform is not None:
                    v.co+=group.weight*(transform @ start-start)
        obj.data.update()

    bpy.context.view_layer.objects.active=rig
    bpy.ops.object.mode_set(mode='EDIT')
    ends=[(bone.name,maps[bone.name]@bone.head,maps[bone.name]@bone.tail)
          for bone in rig.data.edit_bones if bone.name in maps]
    for name,head,tail in ends:
        bone=rig.data.edit_bones[name]
        bone.head,bone.tail=head,tail
    bpy.ops.object.mode_set(mode='OBJECT')
    scene.frame_set(1)


def paint_exposed_skin(figure,rig):
    mesh=figure.data
    skin=mesh.materials[0]
    shader=skin.node_tree.nodes.get('Principled BSDF')
    base=shader.inputs['Base Color'].default_value[:3]
    tint=mesh.color_attributes.new(name='Neck and palm warmth',type='FLOAT_COLOR',domain='POINT')
    paint=skin.node_tree.nodes.new('ShaderNodeVertexColor')
    paint.layer_name=tint.name
    skin.node_tree.links.new(paint.outputs['Color'],shader.inputs['Base Color'])
    pads={}
    for side,sign in [('L',1),('R',-1)]:
        wrist,index,middle,pinky=[rig.data.bones[n+'.'+side].head_local for n in
                                 ('hand','index.01','middle.01','pinky.01')]
        normal=(pinky-index).cross(middle-wrist).normalized()*sign
        pads[side]=(wrist.lerp(middle,.7),normal)
    for vertex in mesh.vertices:
        p=vertex.co
        under_chin=smooth(.965,1.018,p.z)*(1-smooth(.06,.14,abs(p.x)))
        center,normal=pads['L' if p.x>=0 else 'R']
        palm=exp(-(p-center).length_squared/.0024)*smooth(-.1,.66,vertex.normal.dot(normal))
        warm=min(1.,under_chin+.73*palm)
        tint.data[vertex.index].color=(base[0],base[1]*(1-.37*warm),base[2]*(1-.36*warm),1)


def bind_clay(scene,rig,source):
    scene.frame_set(1)
    assert rig.animation_data.action.name=='idle'
    from standing_bind import use_standing_arms
    use_standing_arms(scene,rig)
    with bpy.data.libraries.load(str(Path(source)),link=False) as (_,data):
        data.objects=['Body | continuous clay']
    figure=data.objects[0]
    collection=bpy.data.collections['Figure']
    collection.objects.link(figure)
    bpy.context.view_layer.objects.active=figure
    for modifier in list(figure.modifiers):
        bpy.ops.object.modifier_apply(modifier=modifier.name)
    reduce=figure.modifiers.new('Retain joint and finger curvature','DECIMATE')
    reduce.ratio=.31
    bpy.ops.object.modifier_apply(modifier=reduce.name)
    figure.name='Whale Girl / continuous figure'
    figure.data.name='Whale Girl / shoulder, hip and hand skin'
    figure.data.materials.clear()
    figure.data.materials.append(scene.objects['Caelis arm and hand.L'].data.materials[0])
    figure['deform_group']='body'
    figure['caelisLayer']='body'
    if 'Stage' in figure:del figure['Stage']
    hands={s:HandFit(rig,s) for s in ('L','R')}
    bpy.context.view_layer.objects.active=rig
    bpy.ops.object.mode_set(mode='EDIT')
    for hand in hands.values():hand.place_phalanges(rig.data.edit_bones)
    bpy.ops.object.mode_set(mode='OBJECT')
    scene.frame_set(1)
    matrices={b.name:b.matrix @ b.bone.matrix_local.inverted() for b in rig.pose.bones}
    groups={}
    outline_indices=[]
    for v in figure.data.vertices:
        p=v.co.copy()
        sign=1 if p.x>=0 else -1
        hand=hands['L' if sign==1 else 'R']
        pos,weights=body_point(p,sign)
        ax=abs(p.x)
        arm=smooth(.204,.248,ax)
        arm+=(smooth(.103,.244,ax)-arm)*smooth(.780,.925,p.z)
        arm*=1-smooth(.964,1.008,p.z)
        if arm:
            limb,arm_weights=hand.fit(hand.reflect(p))
            pos=pos.lerp(limb,arm)
            weights={name:w*(1-arm) for name,w in weights.items()}
            weights.update({name:w*arm for name,w in arm_weights.items()})
        if arm>.99 and p.z<.659:outline_indices.append(v.index)
        influences=sorted(((w,n) for n,w in weights.items() if w>.00001),reverse=True)[:4]
        total=sum(w for w,_ in influences)
        skin=Matrix.Identity(4)*0
        for weight,name in influences:
            weight/=total
            if name not in groups:groups[name]=figure.vertex_groups.new(name=name)
            groups[name].add([v.index],weight,'REPLACE')
            skin+=matrices[name]*weight
        v.co=skin.inverted() @ pos
    figure.parent=rig
    skeleton=figure.modifiers.new('Whale Girl FK','ARMATURE')
    skeleton.object=rig
    figure.data.update()
    # Keep the fine warm silhouette around the exposed palms and digits.
    # Its open ends are inside the sleeves and the figure remains closed.
    import bmesh
    outline=figure.copy()
    outline.data=figure.data.copy()
    collection.objects.link(outline)
    outline.name='Contour / Whale Girl palms'
    outline['caelisLayer']='outline_skin'
    outline.data.materials.clear()
    outline.data.materials.append(bpy.data.materials['Warm skin contour'])
    bm=bmesh.new()
    bm.from_mesh(outline.data)
    keep=set(outline_indices)
    bmesh.ops.delete(bm,geom=[v for v in bm.verts if v.index not in keep],context='VERTS')
    bm.normal_update()
    for v in bm.verts:v.co+=.0008*v.normal
    bmesh.ops.reverse_faces(bm,faces=list(bm.faces))
    bm.to_mesh(outline.data)
    bm.free()
    for name in ['Neck','Body / independent torso','leg.L','leg.R','Caelis arm and hand.L',
                 'Caelis arm and hand.R','Contour / Caelis arm and hand.L','Contour / Caelis arm and hand.R']:
        bpy.data.objects.remove(scene.objects[name],do_unlink=True)
    bpy.context.view_layer.update()
    from clay_outfit import fit_outfit
    fit_outfit(scene,figure)
    return figure
