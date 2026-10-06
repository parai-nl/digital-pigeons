"""Controleer het v4-basismodel: GLB-buffers, clips, en de herimport in Blender.

Leest de echte GLB (wat de browser krijgt), controleert de webrandvoorwaarden
en de clips, importeert het bestand opnieuw in Blender en meet en rendert
vanuit die herimport: voeten stil, snavel niet door de grond, rek van de
halshuid, rompvolume, en framestroken van beide clips vanuit opzij, voren en
driekwart-voor. Draai met Blender 5: --background --python dit_bestand.
"""
import bpy, os, sys, struct, json, hashlib
import numpy as np
from mathutils import Vector
from mathutils.bvhtree import BVHTree

OUT=os.environ.get('DUIVEN_MODEL_OUTPUT') or sys.exit('zet DUIVEN_MODEL_OUTPUT (map met model v4)')
ITERATION=os.environ.get('DUIVEN_MODEL_ITERATION','17')
QUICK=os.environ.get('DUIVEN_MODEL_QUICK')=='1'
NAME='stadsduif-v4-basis'
path=os.path.join(OUT,NAME+'.glb')
raw=open(path,'rb').read(); length,kind=struct.unpack_from('<II',raw,12)
data=json.loads(raw[20:20+length]); binary=raw[28+length:]

# Webrandvoorwaarden (embeddedPigeon.ts, media-build.ts): één ingesloten
# buffer, geen externe uri, geen extensies, beelden alleen via bufferView.
assert len(raw)<=12*1024*1024,len(raw)
assert len(data['buffers'])==1 and 'uri' not in data['buffers'][0],data['buffers']
assert not data.get('extensionsUsed') and not data.get('extensionsRequired'),data.get('extensionsUsed')
assert all('bufferView' in image and 'uri' not in image for image in data.get('images',[]))

def values(index):
    accessor=data['accessors'][index]; view=data['bufferViews'][accessor['bufferView']]
    components={'SCALAR':1,'VEC3':3,'VEC4':4}[accessor['type']]
    assert accessor['componentType']==5126
    return np.frombuffer(binary,np.float32,accessor['count']*components,view.get('byteOffset',0)+accessor.get('byteOffset',0)).reshape(accessor['count'],components)
clips={a['name']:a for a in data['animations']}
assert set(clips)=={'Peck','WingWave'},set(clips)
# Piekmomenten voor de cameraomhulling van de runtime (extras.peaks).
for clip in clips.values():
    peaks=clip.get('extras',{}).get('peaks')
    assert peaks and all(0<t<3.2 for t in peaks),(clip['name'],'extras.peaks missing',peaks)
proof={'peaks':{c['name']:c['extras']['peaks'] for c in data['animations']},'glb':{'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest(),'within_8_mib':len(raw)<=8*1024*1024,
    'buffers':len(data['buffers']),'images':len(data.get('images',[])),'extensions':data.get('extensionsUsed',[])}}
for name,clip in clips.items():
    duration=0; changed=set(); rotation={}
    for channel in clip['channels']:
        node=data['nodes'][channel['target']['node']]; target=node['name']; path_=channel['target']['path']
        assert target not in {'PigeonRoot','PigeonSkeleton','Camera'},target
        sampler=clip['samplers'][channel['sampler']]; times=values(sampler['input']); output=values(sampler['output'])
        assert np.isfinite(output).all()
        assert np.allclose(output[0],output[-1],atol=1e-6),(name,target,'boundary mismatch')
        # Begin en eind exact in de bind-pose van de node, niet alleen gelijk aan elkaar.
        rest={'rotation':node.get('rotation',[0,0,0,1]),'translation':node.get('translation',[0,0,0]),'scale':node.get('scale',[1,1,1])}[path_]
        assert np.allclose(np.abs(np.dot(output[0],rest)) if path_=='rotation' else output[0],1 if path_=='rotation' else rest,atol=1e-5),(name,target,path_,'not bind pose')
        # Geen verplaatsing of schaal: alleen rotaties bewegen (geen root-beweging).
        # Alleen de halsgewrichten mogen langs hun as schuiven (strekken van de
        # hals, hooguit 0,08 modeleenheid per gewricht); verder niets en nergens schaal.
        if path_=='translation' and target in {'Neck2','Neck3','Neck4'}:
            assert np.max(np.linalg.norm(output-output[0],axis=1))<.08,(name,target,'stretch too large')
        # De romp mag door de poten een paar centimeter zakken (geen
        # verplaatsing van het model zelf: PigeonRoot beweegt nooit).
        elif path_=='translation' and target=='BodyRig':
            assert np.max(np.linalg.norm(output-output[0],axis=1))<.10,(name,target,'body moves too far')
        elif path_!='rotation': assert np.max(np.abs(output-output[0]))<1e-6,(name,target,path_,'moves')
        duration=max(duration,float(times.max()))
        if np.max(np.abs(output-output[0]))>1e-4:
            changed.add(target)
            if path_=='rotation':
                angle=2*np.degrees(np.arccos(np.clip(np.abs(output@np.array(output[0])),0,1)))
                rotation[target]=round(float(angle.max()),2)
    assert duration<=3.2+1e-5 and abs(duration-3.2)<1e-5,(name,duration)
    need={'Peck':{'BodyRig','Neck1','Neck2','Neck3','Neck4','HeadRig','JawRig'},'WingWave':{'WingLeft','WingRight'}}[name]
    assert need<=changed,(name,need-changed)
    proof[name]={'duration_seconds':duration,'channels':len(clip['channels']),'changing_controls':sorted(changed),
        'max_rotation_from_bind_deg':dict(sorted(rotation.items())),'first_last_equal_bind_pose':True,'only_rotations_and_neck_stretch_move':True}

# Studio en camera uit de bewerkbare scène; alle geometrie komt uit de GLB.
bpy.ops.wm.open_mainfile(filepath=os.path.join(OUT,NAME+'.blend'))
scene=bpy.context.scene
for ob in list(scene.objects):
    if ob.type not in {'LIGHT','CAMERA'}:bpy.data.objects.remove(ob,do_unlink=True)
for action in list(bpy.data.actions):bpy.data.actions.remove(action)
bpy.ops.import_scene.gltf(filepath=path)
rig=next(o for o in scene.objects if o.type=='ARMATURE')
# (De Blender-importer neemt animatie-extras niet over; extras.peaks valt hier
# dus niet te toetsen. In three.js leest pigeonMotion.ts ze na het laden.)
for track in rig.animation_data.nla_tracks:track.mute=True
bone_names=[b.name for b in rig.data.bones]
for name in ['Neck1','Neck2','Neck3','Neck4','HeadRig','JawRig','TailRig','LegLeft','LegRight','FootLeft','FootRight']: assert name in bone_names,name
assert 'NeckRig' not in bone_names
for name in ['Oog -1','Oog 1']:
    eye=bpy.data.objects.get(name)
    assert eye and any(m.type=='ARMATURE' for m in eye.modifiers),'Eye detached: '+name
def pose(clip,seconds):
    if clip is None:
        rig.animation_data.action=None
        for b in rig.pose.bones: b.rotation_quaternion=(1,0,0,0); b.location=(0,0,0); b.scale=(1,1,1)
    else:
        action=bpy.data.actions[clip]; rig.animation_data.action=action; rig.animation_data.action_slot=action.slots[0]
    scene.frame_set(round(seconds*30)); bpy.context.view_layer.update()
def evaluated(ob):
    ev=ob.evaluated_get(bpy.context.evaluated_depsgraph_get()); mesh=ev.to_mesh()
    points=np.empty(len(mesh.vertices)*3,np.float32); mesh.vertices.foreach_get('co',points)
    tris=None
    mesh.calc_loop_triangles()
    if len(mesh.loop_triangles):
        tris=np.empty(len(mesh.loop_triangles)*3,np.int32); mesh.loop_triangles.foreach_get('vertices',tris); tris=tris.reshape(-1,3)
    matrix=np.array(ev.matrix_world); ev.to_mesh_clear()
    result=np.einsum('ij,kj->ki',matrix[:3,:3],points.reshape(-1,3))+matrix[:3,3]
    assert np.isfinite(result).all() and np.max(np.abs(result))<10,ob.name
    return result,tris
feet=[o for o in scene.objects if o.type=='MESH' and ('Poot huid' in o.name or 'Achterteen gesloten' in o.name or 'Nagel hoorn' in o.name or 'Poot schub' in o.name)]
assert len(feet)==5,[o.name for o in feet]
# De tarsus mag bij het pikken door het hielgewricht kantelen; de tenen en
# nagels (alles onder de teenbasis, z < 0,05) staan exact stil.
body=bpy.data.objects['Duif doorlopende huid']
beak=[bpy.data.objects['Snavel bovensnavel'],bpy.data.objects['Snavel ondersnavel']]
pose(None,0)
feet_rest={o.name:evaluated(o)[0] for o in feet}
toes={name:points[:,2]<.05 for name,points in feet_rest.items()}
body_rest,body_tris=evaluated(body)
edges=np.unique(np.sort(np.concatenate([body_tris[:,[0,1]],body_tris[:,[1,2]],body_tris[:,[2,0]]]),axis=1),axis=0)
# Rek = lengte van elke huidrand in de pose gedeeld door in rust, in twee
# gebieden: de iriserende halsvlek (voorhals en zijhals tussen krop en keel) en
# daarboven keel en nek tot onder de kop, waar de kopbuiging opgevangen wordt.
mid=(body_rest[edges[:,0]]+body_rest[edges[:,1]])/2
REGIONS={'halsvlek':(mid[:,2]>1.34)&(mid[:,2]<1.66)&(mid[:,0]<-.35),'keel_nek':(mid[:,2]>=1.66)&(mid[:,2]<1.82)&(mid[:,0]<-.35),
    'mantel':(mid[:,2]>1.15)&(mid[:,2]<1.60)&(mid[:,0]>-.45)}
region_edges={k:edges[m] for k,m in REGIONS.items()}
region_rest={k:np.linalg.norm(body_rest[e[:,0]]-body_rest[e[:,1]],axis=1) for k,e in region_edges.items()}
# Halssilhouet (geen S, geen taille, geen holle knik). In het
# zijaanzicht (xz) de rugmiddenlijn en de buikmiddenlijn van de huid tussen
# schouder (rust z 1,10) en kruin (2,00): hoe diep ligt elk punt binnen de
# convexe omhulling van die twee lijnen samen? In rust ligt de nek door de
# schouderhelling al 0,23 diep; een pose mag nergens dieper komen dan rust.
# De taille: kleinste halsdikte in de bovenste helft gedeeld door de dikte bij
# de kop; mag hooguit 0,05 onder de rustwaarde zakken.
ring_z=body_rest[:,2]
dorsal_idx=[]; ventral_idx=[]
for zc in np.arange(1.12,1.99,.02):
    band=np.where((np.abs(ring_z-zc)<.006)&(np.abs(body_rest[:,1])<.004))[0]
    if len(band)<2: continue
    dorsal_idx.append(band[np.argmax(body_rest[band,0])]); ventral_idx.append(band[np.argmin(body_rest[band,0])])
def hull2d(points):
    ordered=sorted(map(tuple,points))
    def cross(o,a,b): return (a[0]-o[0])*(b[1]-o[1])-(a[1]-o[1])*(b[0]-o[0])
    lower=[]; upper=[]
    for p in ordered:
        while len(lower)>=2 and cross(lower[-2],lower[-1],p)<=0: lower.pop()
        lower.append(p)
    for p in reversed(ordered):
        while len(upper)>=2 and cross(upper[-2],upper[-1],p)<=0: upper.pop()
        upper.append(p)
    return np.array(lower[:-1]+upper[:-1])
def inside_depth(hull,q):
    best=9.
    for i in range(len(hull)):
        a=hull[i]; b=hull[(i+1)%len(hull)]; e=b-a; n=np.array([e[1],-e[0]])/np.linalg.norm(e)
        best=min(best,abs(float(np.dot(q-a,n))))
    return best
def silhouette(points):
    xz=points[:,[0,2]]; D=xz[dorsal_idx]; V=xz[ventral_idx]; hull=hull2d(np.concatenate([D,V]))
    thickness=np.linalg.norm(D-V,axis=1); upper=thickness[len(thickness)//2:]
    return max(inside_depth(hull,q) for q in D),max(inside_depth(hull,q) for q in V),float(upper.min()/thickness[-1])
REST_SILHOUETTE=silhouette(body_rest)

def volume(points,tris):
    a,b,c=points[tris[:,0]],points[tris[:,1]],points[tris[:,2]]
    return float(np.einsum('ij,ij->i',a,np.cross(b,c)).sum()/6)
rest_volume=volume(body_rest,body_tris)
position_key=[tuple(np.round(p,5)) for p in body_rest]
corner_keys=[tuple(position_key[k] for k in tri) for tri in body_tris]
for clip in clips:
    samples=[]; worst={'volume_min':9.,'volume_max':0.,'beak_min_z':9.,'body_min_z':9.,'chest_min_z':9.,'dorsal_depth_max':0.,'ventral_depth_max':0.,'waist_min':9.}
    for region in REGIONS: worst[region]={'stretch_p99_5':0.,'stretch_p95':0.,'compress_p0_5':9.,'worst_seconds':0.}
    # Elk frame van beide clips (0 t/m 96), ook voor de zelfdoorsnijding.
    for frame in range(0,97,1 if not QUICK else 4):
        seconds=frame/30; pose(clip,seconds)
        for foot in feet: assert np.allclose(evaluated(foot)[0][toes[foot.name]],feet_rest[foot.name][toes[foot.name]],atol=1e-6),'Toes moved '+foot.name
        points,_=evaluated(body); sample={'seconds':round(seconds,4)}
        for region,e in region_edges.items():
            ratio=np.linalg.norm(points[e[:,0]]-points[e[:,1]],axis=1)/region_rest[region]
            high,p95,low=float(np.percentile(ratio,99.5)),float(np.percentile(ratio,95)),float(np.percentile(ratio,.5))
            w=worst[region]
            if high>w['stretch_p99_5']: w['stretch_p99_5']=round(high,4); w['worst_seconds']=round(seconds,3)
            w['stretch_p95']=round(max(w['stretch_p95'],p95),4); w['compress_p0_5']=round(min(w['compress_p0_5'],low),4)
            sample[region]={'p99_5':round(high,4),'p95':round(p95,4),'p0_5':round(low,4)}
        # De huid mag zichzelf nergens doorsnijden: geen twee driehoeken zonder
        # gedeelde hoekpunten die elkaar overlappen (bijvoorbeeld voorhals door
        # borst bij het pikken, zoals eerder in de varianten gezien).
        # De GLB-import splitst hoekpunten op UV-naden; buren herkennen we daarom
        # aan de positie in rust, niet aan de index.
        tree=BVHTree.FromPolygons([tuple(p) for p in points],body_tris.tolist())
        crossing=sum(1 for i,j in tree.overlap(tree) if i<j and not set(corner_keys[i])&set(corner_keys[j]))
        assert crossing==0,(clip,seconds,'skin self-intersection',crossing)
        dorsal,ventral,waist=silhouette(points)
        assert dorsal<=REST_SILHOUETTE[0]+.01,(clip,seconds,'hollow nape',dorsal,REST_SILHOUETTE[0])
        assert waist>=REST_SILHOUETTE[2]-.05,(clip,seconds,'neck waist',waist,REST_SILHOUETTE[2])
        worst['dorsal_depth_max']=round(max(worst['dorsal_depth_max'],dorsal),4); worst['ventral_depth_max']=round(max(worst['ventral_depth_max'],ventral),4); worst['waist_min']=round(min(worst['waist_min'],waist),3)
        chest=float(points[(body_rest[:,2]<1.2)&(body_rest[:,0]<-.1),2].min()); worst['chest_min_z']=round(min(worst['chest_min_z'],chest),4)
        assert chest>.10,(clip,seconds,'breast on the ground',chest)
        vol=volume(points,body_tris)/rest_volume
        beak_z=min(float(evaluated(o)[0][:,2].min()) for o in beak)
        worst['volume_min']=round(min(worst['volume_min'],vol),5); worst['volume_max']=round(max(worst['volume_max'],vol),5)
        worst['beak_min_z']=round(min(worst['beak_min_z'],beak_z),4); worst['body_min_z']=round(min(worst['body_min_z'],float(points[:,2].min())),4)
        sample.update({'body_volume_ratio':round(vol,5),'beak_min_z':round(beak_z,4)}); samples.append(sample)
    assert worst['beak_min_z']>-.004,(clip,'beak through ground',worst['beak_min_z'])
    proof[clip].update({'feet_stable':True,'rest_silhouette':[round(v,4) for v in REST_SILHOUETTE],'skin_self_intersections':0,'region_edges':{k:int(len(e)) for k,e in region_edges.items()},'worst':worst,'samples':samples})

# Renders uit de herimport: rust en framestroken, dezelfde camera's als de bouw.
scene.render.engine='CYCLES'; scene.cycles.samples=12 if QUICK else 32; scene.render.film_transparent=True
camera=scene.camera; camera.data.type='ORTHO'
def shoot(file,position,target,scale,resolution):
    camera.data.ortho_scale=scale; camera.location=position
    camera.rotation_euler=(Vector(target)-Vector(position)).to_track_quat('-Z','Y').to_euler()
    scene.render.resolution_x,scene.render.resolution_y=resolution; scene.render.filepath=file; bpy.ops.render.render(write_still=True)
def contact_sheet(paths,columns,out,grey=(.66,.67,.69)):
    arrays=[]
    for p in paths:
        image=bpy.data.images.load(p); arrays.append(np.array(image.pixels[:],np.float32).reshape(image.size[1],image.size[0],4)); bpy.data.images.remove(image)
    h=max(a.shape[0] for a in arrays); w=max(a.shape[1] for a in arrays); rows=(len(arrays)+columns-1)//columns
    sheet=np.ones((rows*h,columns*w,4),np.float32); sheet[:,:,:3]=grey
    for k,a in enumerate(arrays):
        r,c=divmod(k,columns); y0=(rows-1-r)*h; cell=sheet[y0:y0+h,c*w:(c+1)*w]
        dy=(h-a.shape[0])//2; dx=(w-a.shape[1])//2; part=cell[dy:dy+a.shape[0],dx:dx+a.shape[1]]
        part[:,:,:3]=a[:,:,:3]*a[:,:,3:4]+part[:,:,:3]*(1-a[:,:,3:4])
        cell[:,:2,:3]=.5; cell[:2,:,:3]=.5
    image=bpy.data.images.new(os.path.basename(out),width=columns*w,height=rows*h); image.pixels=sheet.ravel()
    image.filepath_raw=out; image.file_format='PNG'; image.save(); bpy.data.images.remove(image)
REST=[('rust-links',(-.65,-6,1.60),(.08,0,1.0),3.05),('rust-voor',(-6,0,1.30),(-.30,0,1.02),2.55),('rust-driekwart-voor',(-4.3,-4.3,1.55),(-.15,0,1.02),2.85),('rust-achter',(6,0,1.45),(.30,0,1.0),2.55)]
STRIPS=[('zij',(-.75,-6,1.25),(-.25,0,.95),3.45),('voor',(-6,0,1.25),(-.30,0,.95),3.0),('driekwart-voor',(-4.5,-4.1,1.45),(-.35,0,.95),3.5)]
MOMENTS={'Peck':[0,.18,.45,.70,.80,.92,1.02,1.18,1.40,1.62,2.00,3.2],'WingWave':[0,.20,.27,.33,.40,.67,.90,1.07,1.13,1.20,1.80,3.2]}
pose(None,0); cells=[]
for label,position,target,scale in REST:
    file=os.path.join(OUT,'herimport',f'herimport-{ITERATION}-{label}.png'); shoot(file,position,target,scale,(550,550) if QUICK else (1000,1000)); cells.append(file)
contact_sheet(cells,4,os.path.join(OUT,f'herimport-{ITERATION}-rust-overzicht.png'))
size=(420,400) if QUICK else (560,520)
for clip,moments in MOMENTS.items():
    cells=[]
    for view,position,target,scale in STRIPS:
        for k,seconds in enumerate(moments):
            pose(clip,seconds); file=os.path.join(OUT,'herimport',f'{clip}-{view}-{k:02d}.png'); shoot(file,position,target,scale,size); cells.append(file)
    contact_sheet(cells,len(moments),os.path.join(OUT,f'herimport-{ITERATION}-{clip}-strook.png'))
proof['strip_moments_seconds']=MOMENTS
with open(os.path.join(OUT,'animation-verification.json'),'w') as f:json.dump(proof,f,indent=2)
print('VERIFIED_V4',proof['glb']['sha256'],json.dumps({c:proof[c]['worst'] for c in clips}),json.dumps({c:proof[c]['max_rotation_from_bind_deg'] for c in clips}))
