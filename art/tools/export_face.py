"""Export authored expression/view endpoints as standard sparse glTF morph targets.

The joined base export preserves the skin and clips. Material + exact coordinate
provenance maps its vertices back to editable Blender shape keys, including the
expression/view residuals. Never approximates the authored endpoints in runtime.
"""
from pathlib import Path
import bpy, json, struct, sys
from mathutils import Vector
from mathutils.kdtree import KDTree
# Run with the same source blend used by export_glb.py --caelis-base.
SOURCE, OUTPUT = map(Path, sys.argv[sys.argv.index("--")+1:])
EXPRESSIONS = ["blinkLeft", "blinkRight", "happy", "mouthClosed", "mouthRound",
               "eyeSmile", "eyeFocused", "browRaised", "browFocused", "mouthSmile", "mouthFlat"]
VIEWS = ["view3QLeft", "view3QRight", "viewSideLeft", "viewSideRight"]
parts = [o for o in bpy.context.scene.objects if o.type=='MESH' and 'deform_group' in o and o.data.shape_keys]
keys = {k.name for o in parts for k in o.data.shape_keys.key_blocks[1:]}
names = [*EXPRESSIONS, *VIEWS, *sorted(keys-set(EXPRESSIONS)-set(VIEWS))]
def gltf(p): return Vector((p.x, p.z, -p.y))
rows = {}
for obj in parts:
    for v in obj.data.vertices:
        # Every deformable part has one material in the source.
        rows.setdefault(obj.data.materials[0].name, []).append((gltf(obj.matrix_world @ v.co), obj, v.index))
lookup = {}
for mat, items in rows.items():
    tree = KDTree(len(items))
    for i, (p, _, _) in enumerate(items): tree.insert(p, i)
    tree.balance(); lookup[mat] = (tree, items)
raw = SOURCE.read_bytes(); json_size = struct.unpack_from('<I', raw, 12)[0]
doc = json.loads(raw[20:20+json_size]); binary_size = struct.unpack_from('<I', raw, 20+json_size)[0]
binary = bytearray(raw[28+json_size:28+json_size+binary_size]); original_size = len(binary)
def view(data):
    binary.extend(b'\0'*(-len(binary)%4)); i = len(doc['bufferViews'])
    doc['bufferViews'].append({'buffer':0,'byteOffset':len(binary),'byteLength':len(data)})
    binary.extend(data); return i

def accessor(values):
    values = [struct.unpack('<fff', struct.pack('<fff', *v)) for v in values]
    a = {'componentType':5126,'count':len(values),'type':'VEC3',
         'min':[min(v[k] for v in values) for k in range(3)],
         'max':[max(v[k] for v in values) for k in range(3)]}
    nz = [(i,v) for i,v in enumerate(values) if any(abs(c)>1e-8 for c in v)]
    if nz:
        a['sparse'] = {'count':len(nz),'indices':{'bufferView':view(b''.join(struct.pack('<I',i) for i,_ in nz)),'componentType':5125},
                       'values':{'bufferView':view(b''.join(struct.pack('<fff',*v) for _,v in nz))}}
    i=len(doc['accessors']); doc['accessors'].append(a); return i

matched=0; matched_objects=set(); changed={name:0 for name in names}; active_counts=[]
for mesh in doc['meshes']:
    mesh.setdefault('extras', {})['targetNames']=names
    mesh['weights']=[1 if n=='mouthClosed' else 0 for n in names]
    for primitive in mesh['primitives']:
        a=doc['accessors'][primitive['attributes']['POSITION']]; bv=doc['bufferViews'][a['bufferView']]
        offset=bv.get('byteOffset',0)+a.get('byteOffset',0); stride=bv.get('byteStride',12)
        material=doc['materials'][primitive['material']]['name']; source=lookup.get(material)
        targets=[[] for _ in names]
        for i in range(a['count']):
            p=Vector(struct.unpack_from('<fff',binary,offset+i*stride)); obj=None; vi=0
            if source:
                _,nearest,distance=source[0].find(p)
                if distance<1e-5:
                    _,obj,vi=source[1][nearest]; matched+=1; matched_objects.add(obj.name)
            for ni,name in enumerate(names):
                key=obj.data.shape_keys.key_blocks.get(name) if obj else None
                delta=gltf(obj.matrix_world.to_3x3() @ (key.data[vi].co-obj.data.vertices[vi].co)) if key else Vector((0,0,0))
                targets[ni].append(delta)
                if delta.length>1e-8: changed[name]+=1
        primitive['targets']=[{'POSITION':accessor(v)} for v in targets]
        active_counts.append(sum(any(v.length>1e-8 for v in vs) for vs in targets))
assert matched_objects=={o.name for o in parts}, sorted({o.name for o in parts}-matched_objects)
assert all(changed.values()), changed
assert binary[:original_size]==raw[28+json_size:28+json_size+binary_size]
binary.extend(b'\0'*(-len(binary)%4)); doc['buffers'][0]['byteLength']=len(binary)
encoded=json.dumps(doc,ensure_ascii=False,separators=(',',':')).encode(); encoded+=b' '*(-len(encoded)%4)
OUTPUT.write_bytes(struct.pack('<III',0x46546c67,2,28+len(encoded)+len(binary))+struct.pack('<II',len(encoded),0x4e4f534a)+encoded+struct.pack('<II',len(binary),0x004e4942)+binary)
