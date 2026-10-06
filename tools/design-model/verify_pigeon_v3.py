"""Validate animation buffers and render the exact exported/reimported GLB."""
import bpy, os, sys, struct, json, hashlib
import numpy as np
from mathutils import Vector

OUT=os.environ.get('DUIVEN_MODEL_OUTPUT') or sys.exit('zet DUIVEN_MODEL_OUTPUT (map met model v3)')
ITERATION=os.environ.get('DUIVEN_MODEL_ITERATION','04')
path=os.path.join(OUT,'stadsduif-v3.glb')
raw=open(path,'rb').read(); length,kind=struct.unpack_from('<II',raw,12)
data=json.loads(raw[20:20+length]); binary=raw[28+length:]
def values(index):
    accessor=data['accessors'][index]; view=data['bufferViews'][accessor['bufferView']]
    components={'SCALAR':1,'VEC3':3,'VEC4':4}[accessor['type']]
    assert accessor['componentType']==5126
    return np.frombuffer(binary,np.float32,accessor['count']*components,view.get('byteOffset',0)+accessor.get('byteOffset',0)).reshape(accessor['count'],components)
clips={a['name']:a for a in data['animations']}
assert set(clips)=={'Peck','WingWave'}
proof={}
for name,clip in clips.items():
    duration=0; changed=[]
    for channel in clip['channels']:
        target=data['nodes'][channel['target']['node']]['name']
        assert target not in {'PigeonRoot','PigeonSkeleton','Camera'},target
        sampler=clip['samplers'][channel['sampler']]; times=values(sampler['input']); output=values(sampler['output'])
        assert np.isfinite(output).all()
        assert np.allclose(output[0],output[-1],atol=1e-6),(name,target,'boundary mismatch')
        duration=max(duration,float(times.max()))
        if np.max(np.abs(output-output[0]))>1e-4:changed.append(target)
    assert abs(duration-3.2)<1e-5,(name,duration)
    assert ('NeckRig' in changed) if name=='Peck' else ('WingLeft' in changed and 'WingRight' in changed)
    proof[name]={'duration_seconds':duration,'channels':len(clip['channels']),'changing_controls':sorted(set(changed)),'first_last_equal':True}

# Retain only authored photographic studio/camera from the editable scene.
bpy.ops.wm.open_mainfile(filepath=os.path.join(OUT,'stadsduif-v3.blend'))
scene=bpy.context.scene
for ob in list(scene.objects):
    if ob.type not in {'LIGHT','CAMERA'}:bpy.data.objects.remove(ob,do_unlink=True)
for action in list(bpy.data.actions):bpy.data.actions.remove(action)
bpy.ops.import_scene.gltf(filepath=path)
rig=next(o for o in scene.objects if o.type=='ARMATURE')
for track in rig.animation_data.nla_tracks:track.mute=True
for name in ['Oog -1','Oog 1']:
    eye=bpy.data.objects.get(name)
    assert eye and any(m.type=='ARMATURE' for m in eye.modifiers),'Eye detached: '+name
scene.render.fps=30; scene.cycles.samples=32
camera=scene.camera; camera.data.ortho_scale=3.05
def aim(position):
    camera.location=position; camera.rotation_euler=(Vector((.10,0,1))-camera.location).to_track_quat('-Z','Y').to_euler()
def pose(clip,frame):
    action=bpy.data.actions[clip]; rig.animation_data.action=action; rig.animation_data.action_slot=action.slots[0]
    scene.frame_set(frame); bpy.context.view_layer.update()
def coordinates(ob):
    evaluated=ob.evaluated_get(bpy.context.evaluated_depsgraph_get()); mesh=evaluated.to_mesh()
    points=np.empty(len(mesh.vertices)*3,np.float32); mesh.vertices.foreach_get('co',points)
    matrix=np.array(evaluated.matrix_world)
    assert np.isfinite(points).all() and np.isfinite(matrix).all()
    result=np.einsum('ij,kj->ki',matrix[:3,:3],points.reshape(-1,3))+matrix[:3,3]
    assert np.isfinite(result).all() and np.max(np.abs(result))<10,ob.name
    evaluated.to_mesh_clear(); return result
feet=[o for o in scene.objects if o.type=='MESH' and ('Poot huid' in o.name or 'Achterteen gesloten' in o.name)]
assert len(feet)==3,[o.name for o in feet]
pose('Peck',0); feet_rest={o.name:coordinates(o) for o in feet}
for clip in clips:
    extrema=[]
    for second in sorted(set([i*3.2/12 for i in range(13)]+[1,34/30,58/30])):
        pose(clip,round(second*30))
        for foot in feet:assert np.allclose(coordinates(foot),feet_rest[foot.name],atol=1e-6),'Foot moved '+foot.name
        points=np.concatenate([coordinates(o) for o in scene.objects if o.type=='MESH'])
        extrema.append([points.min(axis=0).tolist(),points.max(axis=0).tolist()])
    proof[clip]['sampled_geometry_bounds']=extrema
    proof[clip]['feet_stable']=True
for label,clip,frame,position in [
    ('rust','Peck',0,(-.65,-6,1.65)),('Peck','Peck',30,(-.65,-6,1.65)),
    ('WingWave','WingWave',34,(-.65,-6,1.65)),('WingWave-voor','WingWave',34,(-5,-1.4,1.6)),
    ('WingWave-achter','WingWave',34,(4,-3,1.8)),('achter','Peck',0,(4,-3,1.8))]:
    pose(clip,frame); aim(position)
    scene.render.filepath=os.path.join(OUT,f'herimport-{ITERATION}-{label}.png'); bpy.ops.render.render(write_still=True)
proof['sha256']=hashlib.sha256(raw).hexdigest()
with open(os.path.join(OUT,'animation-verification.json'),'w') as f:json.dump(proof,f,indent=2)
print('VERIFIED_V3_ANIMATION',proof['sha256'])
