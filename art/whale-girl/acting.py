"""Character-specific acting keys for the five basic desktop clips."""
from math import atan2, pi, sin, sqrt
import bpy
from mathutils import Quaternion, Vector
from portrait import lids


def shape_portrait(face, pieces):
    """Redraw the painted features on the facial surface in each expression."""
    for obj in pieces:
        name=obj.name
        if name.startswith("Soft nose"):
            continue
        brow=name.startswith("Arched brow")
        lash=name.startswith("Swept upper lash")
        mouth=name.startswith(("Coral smile","Smile opening","Smile canine"))
        obj["caelisLayer"]="acting_iris" if name.startswith("Sapphire iris") else "acting_ink"
        source=[v.co.copy() for v in obj.data.vertices]
        keys=[obj.shape_key_add(name=n) for n in ("Basis","Focus","Joy","Alert","Blink")]
        if mouth:
            keys.append(obj.shape_key_add(name="MouthClose"))
        for i,(x,y,z) in enumerate(source):
            def depth(x,z):
                hit,p,_,_=face.ray_cast(Vector((x,-1,z)),Vector((0,1,0)))
                if not hit:
                    raise ValueError(f"{name} outside face")
                return p.y
            offset=y-depth(x,z)
            u=(abs(x)-.175)/.114
            arch=max(0,1-u*u)
            for key in keys:
                style=key.name
                if style=="Blink":
                    style="Basis" if mouth or brow else "Joy"
                nx,nz,dy=x,z,offset
                if mouth:
                    # The jaw is independent of the blink, brow and gaze.
                    # The lips meet along an arc; the tongue and tooth retreat
                    # behind that seam instead of leaving a white horizontal line.
                    nx=.90*x
                    nz=1.213+(z-1.214)*1.15+.0014*x/.073
                    if style=="MouthClose":
                        nx=.75*x
                        nz=1.211+.009*(x/.073)**2+(z-1.214)*.057
                        if name!="Coral smile":
                            # Retract only just behind the outer lip. A deep
                            # retreat would hide the tongue halfway through a smile.
                            dy=-.00765 if name=="Smile opening" else -.00725
                            nz=1.211+.009*(x/.073)**2+(z-1.214)*.020
                elif brow:
                    nz+= {"Basis":-.010,"Focus":-.018+.023*u+(.007 if x<0 else 0),"Joy":.012-.018*u,"Alert":.026}[style]
                elif style=="Joy":
                    # The closed lid forms an upward arc; withdraw the eye paint
                    # just beneath it so no iris is exposed at either corner.
                    nz=1.327+.038*arch-.008*u
                    if lash:
                        upper=lids(abs(x))[1]
                        nz+=(z-upper)*.76
                    else:
                        nz+=(z-1.345)*.025
                        dy=.009
                elif style in ("Focus","Basis"):
                    if style=="Focus":
                        if not (lash or name.startswith(("Oval sclera","Outer canthus","Outer lower lid","Lid crease"))):
                            # Carry the sideways gaze along the lid margins.
                            # A full iris has little spare white at either end.
                            lo,hi=lids(abs(x))
                            nx-=.010
                            low,high=lids(abs(nx))
                            nz=low+(z-lo)*(high-low)/max(.001,hi-lo)
                        nz=1.343+.014*u+(nz-1.345)*(.52 if nz>1.345 else .91)
                        if lash:
                            top=lids(abs(x))[1]
                            nz+=.57*max(0,z-top)
                    else:
                        # Share a single resting aperture across both eyes.
                        half=z-1.335
                        aperture=.935 if half>0 else .89
                        nz=1.334+half*aperture
                        # The standing drawing shows more iris above the pupil.
                        opening=.40
                        nz+=opening*max(0,nz-1.335)+.055*max(0,1.335-nz)
                # Register all painted layers to the same pupil in each eye.
                # Reproject *after* fitting so the ink stays on the curved skin.
                eye_center=.172 if x>=0 else -.172
                if brow:
                    nx=eye_center+.94*(nx-eye_center)
                    nz-=.008
                elif not mouth:
                    nx=eye_center+.81*(nx-eye_center)
                    # Open the entire painted eye around its optical center,
                    # including the blink stroke and lower lid.
                    pivot=.168 if x>=0 else -.168
                    nx=pivot+1.095*(nx-pivot)
                    if style=="Basis":
                        # Counter the inward lean of the projected iris as
                        # the cheeks taper. Keep both pupils at the same aim.
                        nx+=(1 if x>0 else -1)*.16*(1.348-nz)*arch
                    nz=1.345+.944*(nz-1.345)
                    # Lift and spread the outer third as one surface so the
                    # white, lash and crease meet at the drawn corner.
                    outer=max(0,min(1,(abs(x)-.219)/.054))
                    outer=outer*outer*(3-2*outer)
                    nx+=(1 if x>0 else -1)*.0075*outer
                    nz+=.012*outer
                key.data[i].co=(nx,depth(nx,nz)+dy,nz)
        for vertex,point in zip(obj.data.vertices,keys[0].data):
            vertex.co=point.co


def assemble_portrait(scene):
    meshes=[]
    for layer in ("acting_ink","acting_iris"):
        pieces=[o for o in scene.objects if o.get("caelisLayer")==layer]
        bpy.ops.object.select_all(action="DESELECT")
        for obj in pieces:
            obj.select_set(True)
        bpy.context.view_layer.objects.active=pieces[0]
        bpy.ops.object.join()
        joined=bpy.context.object
        joined.name=layer
        meshes.append(joined)
    return meshes


def solve_arm(rig, side, target, pole, fingers, palm, forearm_roll, palm_normal):
    """Place wrist and fingertips in chest space; keep both segment lengths."""
    bones=[rig.pose.bones[n+"."+side] for n in ("upper_arm","forearm","hand")]
    root=bones[0].bone.head_local
    line=Vector(target)-root
    axis=line.normalized()
    a,b=(bone.bone.length for bone in bones[:2])
    reach=min(line.length,a+b-.002)
    along=(a*a-b*b+reach*reach)/(2*reach)
    outside=Vector(pole)-root
    elbow=root+axis*along+(outside-axis*outside.dot(axis)).normalized()*sqrt(max(0,a*a-along*along))
    directions=(elbow-root,root+axis*reach-elbow,Vector(fingers))
    parent=Quaternion()
    for bone,direction in zip(bones,directions):
        rest=bone.bone.matrix_local.to_quaternion()
        neutral=bone.bone.tail_local-bone.bone.head_local
        if bone==bones[-1]:
            # Aim the palm itself. The shared hand bone points away from
            # the middle knuckle and introduces a kink when used as its axis.
            neutral=rig.data.bones["middle.01."+side].head_local-bone.bone.head_local
        # Carry twist from the parent instead of deriving wrist roll from
        # an independent world-space aim.
        swing=(parent @ neutral).rotation_difference(direction)
        rotation=swing @ parent
        if bone==bones[-1] or (bone==bones[1] and forearm_roll):
            # The aim alone leaves wrist roll unconstrained. Align the knuckle
            # row with the camera plane so the palm reads in a greeting.
            index=rig.data.bones["index.01."+side]
            pinky=rig.data.bones["pinky.01."+side]
            spread=rotation @ (pinky.head_local-index.head_local)
            axis=direction.normalized()
            natural=(spread-axis*spread.dot(axis)).normalized()
            wanted=axis.cross(palm_normal).normalized()
            if side=="R":
                wanted.negate()
            angle=atan2(axis.dot(natural.cross(wanted)),natural.dot(wanted))
            rotation=Quaternion(axis,angle*(palm if bone==bones[-1] else forearm_roll)) @ rotation
        bone.rotation_quaternion=rest.inverted() @ parent.inverted() @ rotation @ rest
        parent=rotation


def author_clips(rig, meshes, fit_landmark):
    scene=bpy.context.scene
    actions=list(bpy.data.actions)
    # In each row: wrist target, elbow pole and relaxed fingertip direction.
    rests={side:((sign*.382,-.068,.672),(sign*.324,.075,.853),(sign*.42,-.19,-.887))
           for side,sign in (("L",1),("R",-1))}
    focus={"R":((-.169,-.264,1.118),(-.36,-.18,.81),(.94,-.22,.12)),
           "L":((.122,-.221,.764),(.42,-.03,.79),(-.75,-.28,-.29))}
    joy={"R":((-.35,-.304,1.283),(-.46,-.15,.995),(-.30,-.31,.95)),
         "L":((.489,-.110,.822),(.31,.02,.80),(.90,-.23,-.30))}
    frames={"idle":[(1,0),(60,.2),(121,0)],"working":[(1,.89),(61,1),(121,.89)],
            "nod":[(1,0),(13,.4),(28,0)],"attention":[(1,0),(14,1),(30,.82),(46,0)],
            "celebrate":[(1,0),(12,.94),(21,1),(29,.93),(36,.98),(49,0)]}
    closed={"idle":[(1,1),(28,1),(39,.72),(51,.44),(66,.86),(75,1),(121,1)],
            "working":[(1,.97),(34,1),(62,.84),(82,.96),(121,.97)],
            "celebrate":[(1,1),(7,.54),(12,.08),(19,0),(27,.25),(34,.06),(42,.48),(49,1)],
            "attention":[(1,1),(9,.78),(17,.43),(28,.60),(39,.92),(46,1)],
            "nod":[(1,1),(9,.87),(15,.62),(22,.90),(28,1)]}
    for action in actions:
        label=action.name
        rig.animation_data.action=action
        slot=action.slots[0]
        rig.animation_data.action_slot=slot
        # Replace only the arm and hand orientations owned by this gesture.
        bag=action.layers[0].strips[0].channelbag(slot)
        for curve in list(bag.fcurves):
            if any('["'+joint+'.' in curve.data_path for joint in ("upper_arm","forearm","hand")) and curve.data_path.endswith("rotation_quaternion"):
                bag.fcurves.remove(curve)
        for mesh in meshes:
            keys=mesh.data.shape_keys
            anim=keys.animation_data_create()
            anim.action=None
        # Sample the inherited neck motion before applying the key-pose tilt.
        head=rig.pose.bones["head"]
        neck_rest=head.bone.matrix_local.to_quaternion()
        original={}
        for frame,_ in frames[label]:
            scene.frame_set(frame)
            original[frame]=head.rotation_quaternion.copy()
        if label in ("working","celebrate"):
            for curve in list(bag.fcurves):
                if curve.data_path==head.path_from_id("rotation_quaternion"):
                    bag.fcurves.remove(curve)
        previous_arm_keys={}
        for frame,strength in frames[label]:
            scene.frame_set(frame)
            if label in ("working","celebrate"):
                accent=(Quaternion((1,0,0),.064*strength) @ Quaternion((0,0,1),-.15*strength)
                        if label=="working" else Quaternion((0,1,0),.20*strength))
                head.rotation_quaternion=neck_rest.inverted() @ accent @ neck_rest @ original[frame]
                head.keyframe_insert("rotation_quaternion",frame=frame,group="head")
            if label=="celebrate":
                # Transfer weight onto the planted right foot. The left hip is
                # under the skirt; toe and heel rise on the same leg chain.
                for joint,axis,angle in (("chest",(0,1,0),.095),("leg.L",(0,1,0),-.38),
                                         ("knee.L",(1,0,0),.62),
                                         ("foot.L",(1,0,0),.18)):
                    bone=rig.pose.bones[joint]
                    rest=bone.bone.matrix_local.to_quaternion()
                    bone.rotation_quaternion=rest.inverted() @ Quaternion(axis,angle*strength) @ rest
                    bone.keyframe_insert("rotation_quaternion",frame=frame,group=joint)
                leg=rig.pose.bones["leg.L"]
                leg.location=leg.bone.matrix_local.to_quaternion().inverted() @ Vector((0,0,.052*strength))
                leg.keyframe_insert("location",frame=frame,group=leg.name)
            for side in ("L","R"):
                accent=joy if label=="celebrate" else focus if label in ("working","attention") else rests
                w=0 if label in ("idle","nod") else strength*(.50 if label=="attention" else 1)
                wrist,pole,direction=[Vector(a).lerp(Vector(b),w) for a,b in zip(rests[side],accent[side])]
                if label=="celebrate" and side=="R":
                    direction.x+=strength*.55*sin((frame-12)*pi/13)
                palm=(.27+.73*w if side=="R" and label!="working" else .33 if side=="R" else .77+.13*w)
                # In the standing pose the hands hang beside the skirt,
                # palms towards the thighs. Turn them forward for gestures.
                sign=1 if side=="L" else -1
                normal=Vector((-sign*.98,.16,-.04)).lerp(Vector((0,-1,0)),w).normalized()
                solve_arm(rig,side,fit_landmark(wrist),fit_landmark(pole),direction,
                          1+(palm-1)*w,.76*(1-w),normal)
                hand=rig.pose.bones["hand."+side]
                hand.scale=(1+.07*w,)*3
                hand.keyframe_insert("scale",frame=frame,group=hand.name)
                for joint in ("upper_arm","forearm","hand"):
                    bone=rig.pose.bones[joint+"."+side]
                    # Blender interpolates quaternion channels separately.
                    # Keep adjacent representations in the same hemisphere.
                    q=bone.rotation_quaternion.copy()
                    if bone.name in previous_arm_keys:
                        q.make_compatible(previous_arm_keys[bone.name])
                    bone.rotation_quaternion=q
                    previous_arm_keys[bone.name]=q
                    bone.keyframe_insert("rotation_quaternion",frame=frame,group=bone.name)
                spread={"thumb":.55,"index":.32,"middle":.11,"ring":-.13,"pinky":-.40}
                for digit,fan in spread.items():
                    base=rig.pose.bones[digit+".00."+side]
                    rest=base.bone.matrix_local.to_quaternion()
                    opening=.19 if label=="idle" else w if label=="celebrate" else .18 if side=="R" else .10
                    angle=fan*opening*(1 if side=="L" else -1)
                    knuckle=.84*w if label=="working" and side=="R" else .035*(1-w)
                    if digit=="thumb":
                        knuckle*=.70
                        if label=="working" and side=="R":
                            angle+=.48*w
                    flex=Quaternion((0,0,1),knuckle*(1 if side=="R" else -1))
                    base.rotation_quaternion=rest.inverted() @ Quaternion((0,1,0),angle) @ flex @ rest
                    base.keyframe_insert("rotation_quaternion",frame=frame,group=base.name)
                # Roll the finger chain towards the palm for the thinking hand.
                fan_axis=(rig.data.bones["pinky.01."+side].head_local-
                          rig.data.bones["index.01."+side].head_local).normalized()
                if side=="R":
                    fan_axis.negate()
                for digit,rest_curl in (("thumb",.09),("index",.17),("middle",.22),("ring",.30),("pinky",.34)):
                    for segment in ("01","02"):
                        finger=rig.pose.bones[digit+"."+segment+"."+side]
                        rest=finger.bone.matrix_local.to_quaternion()
                        curl=.18+(.98*w if side=="R" else .39*w) if label=="working" else .07*(1-w)
                        if digit=="thumb":
                            curl*=.57
                        relaxed=Quaternion(fan_axis,rest_curl*(.74 if segment=="02" else 1))
                        gesture=Quaternion((0,0,1),curl*(1 if side=="R" else -1))
                        finger.rotation_quaternion=rest.inverted() @ relaxed.slerp(gesture,w) @ rest
                        finger.keyframe_insert("rotation_quaternion",frame=frame,group=finger.name)
            for mesh in meshes:
                keys=mesh.data.shape_keys
                for name in ("Focus","Joy","Alert","Blink"):
                    active={"working":"Focus","celebrate":"Joy","attention":"Alert","nod":"Focus"}.get(label)
                    key=keys.key_blocks[name]
                    key.value=strength if name==active else 0
                    key.keyframe_insert("value",frame=frame)
        for mesh in meshes:
            anim=mesh.data.shape_keys.animation_data
            jaw=mesh.data.shape_keys.key_blocks.get("MouthClose")
            if jaw:
                for frame,value in closed[label]:
                    jaw.value=value
                    jaw.keyframe_insert("value",frame=frame)
            if label=="idle":
                blink=mesh.data.shape_keys.key_blocks["Blink"]
                for frame,value in ((77,0),(80,1),(82,1),(86,0)):
                    blink.value=value
                    blink.keyframe_insert("value",frame=frame)
            face_action=anim.action
            face_action.name=mesh.name+"_"+label
            track=anim.nla_tracks.new()
            track.name=label
            track.strips.new(label,1,face_action)
            track.mute=True
            anim.action=None
        track=rig.animation_data.nla_tracks.new()
        track.name=label
        track.strips.new(label,1,action)
        track.mute=True
    rig.animation_data.action=bpy.data.actions["idle"]
    for mesh in meshes:
        mesh.data.shape_keys.animation_data.action=bpy.data.actions[mesh.name+"_idle"]
    scene.frame_set(1)
