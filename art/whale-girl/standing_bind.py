"""Move the arm bind pose to the standing frame and retarget the five authored performances."""
import bpy
from math import radians, tanh
from mathutils import Matrix, Quaternion


def fitted_rotation(name, q):
    # The 1.18 glove tolerated more than a half-turn split between the
    # forearm and wrist. Preserve the elbow path and share a smaller roll.
    joint=name.split('.')[0]
    limits={'forearm':(91,36),'hand':(36,32),'thumb':(6,4),
            'index':(11,4),'middle':(13,4),'ring':(13,4),'pinky':(10,4)}
    if '.00.' in name and joint!='thumb':return Quaternion()
    if joint not in limits:return q
    max_bend,max_roll=map(radians,limits[joint])
    swing,roll=q.to_swing_twist('Y')
    if swing.w<0:swing.negate()
    angle=swing.angle
    if angle>.0001:swing=Quaternion(swing.axis,max_bend*tanh(angle/max_bend))
    return swing @ Quaternion((0,1,0),max_roll*tanh(roll/max_roll))


def open_greeting_thumb(rig):
    """Abduct the thumb toward the radial edge of the fitted palm at the wave apex."""
    action=bpy.data.actions['celebrate']
    joint=rig.pose.bones['thumb.00.R']
    across=rig.data.bones['index.01.R'].head_local-rig.data.bones['pinky.01.R'].head_local
    axis=(joint.bone.tail_local-joint.bone.head_local).cross(across).normalized()
    local_axis=joint.bone.matrix_local.to_quaternion().inverted() @ axis
    bag=action.layers[0].strips[0].channelbag(action.slots[0])
    curves=[bag.fcurves.find(joint.path_from_id('rotation_quaternion'),index=i) for i in range(4)]
    for points in zip(*(c.keyframe_points for c in curves)):
        frame=points[0].co.x
        t=max(0.,min(1.,(frame-1)/11,(49-frame)/13))
        q=Quaternion([p.co.y for p in points]) @ Quaternion(local_axis,radians(23)*t*t*(3-2*t))
        for point,value in zip(points,q.normalized()):
            point.co.y=value
    for curve in curves:curve.update()


def use_standing_arms(scene,rig):
    animation=rig.animation_data
    actions=[track.strips[0].action for track in animation.nla_tracks]
    scene.frame_set(1)
    arms=[b for side in ('L','R') for upper in [rig.data.bones['upper_arm.'+side]]
          for b in (upper,*upper.children_recursive)]
    names=[b.name for b in arms]
    old_rest={name:rig.data.bones[name].matrix_local.copy() for name in names}
    standing={name:rig.pose.bones[name].matrix.copy() for name in names}
    deform={name:standing[name] @ old_rest[name].inverted() for name in names}
    # Sleeve rings and trim must retain their evaluated standing shape under
    # the new bind matrices. Head, face keys and legs use other bones.
    identity=Matrix.Identity(4)
    for obj in scene.objects:
        if obj.type!='MESH' or obj.find_armature()!=rig:
            continue
        if not any(g.name in deform for g in obj.vertex_groups):
            continue
        assert not obj.data.shape_keys, obj.name
        bind=rig.matrix_world.inverted() @ obj.matrix_world
        undo=bind.inverted()
        transforms={g.index:undo @ deform.get(g.name,identity) @ bind for g in obj.vertex_groups}
        for vertex in obj.data.vertices:
            terms=[transforms[g.group]*g.weight for g in vertex.groups if g.weight>0]
            if terms:vertex.co=sum(terms[1:],terms[0]) @ vertex.co
        obj.data.update()

    # Record the relative pose of every keyed frame before editing the rest
    # axes. Quaternion channels alone describe the original arm rotations.
    samples={}
    for action in actions:
        animation.action=action
        animation.action_slot=action.slots[0]
        first,last=map(int,action.frame_range)
        frames={}
        for i in range(first,last+1):
            scene.frame_set(i)
            frames[i]={name:rig.pose.bones[name].parent.matrix.inverted() @ rig.pose.bones[name].matrix
                       for name in names}
        samples[action]=frames

    bpy.context.view_layer.objects.active=rig
    bpy.ops.object.mode_set(mode='EDIT')
    for name in names:
        bone=rig.data.edit_bones[name]
        bone.use_connect=False
        bone.matrix=standing[name]
    bpy.ops.object.mode_set(mode='OBJECT')
    inverse_local={name:rig.data.bones[name].matrix_local.inverted()
                         @ rig.data.bones[name].parent.matrix_local for name in names}
    for action,frames in samples.items():
        animation.action=action
        animation.action_slot=action.slots[0]
        bag=action.layers[0].strips[0].channelbag(action.slots[0])
        paths={name:rig.pose.bones[name].path_from_id() for name in names}
        for curve in list(bag.fcurves):
            if curve.data_path.rsplit('.',1)[0] in paths.values():
                bag.fcurves.remove(curve)
        previous={}
        for name in names:
            rotations=[]
            for i,frame in frames.items():
                local=inverse_local[name] @ frame[name]
                assert local.translation.length<.0001,(action.name,name,i,local.translation)
                q=fitted_rotation(name,local.to_quaternion())
                if action.name=="working" and name=="upper_arm.R":
                    q=Quaternion().slerp(q,.91)
                if name in previous:q.make_compatible(previous[name])
                previous[name]=q
                rotations.append((i,q))
            bone=rig.pose.bones[name]
            bone.matrix_basis=identity
            for channel in range(4):
                f=bag.fcurves.new(paths[name]+'.rotation_quaternion',index=channel,
                                  group_name=name)
                f.keyframe_points.add(len(rotations))
                for key,(i,q) in zip(f.keyframe_points,rotations):
                    key.co=(i,q[channel])
                    key.interpolation='LINEAR'
                f.update()
    animation.action=bpy.data.actions['idle']
    animation.action_slot=animation.action.slots[0]
    scene.frame_set(1)
