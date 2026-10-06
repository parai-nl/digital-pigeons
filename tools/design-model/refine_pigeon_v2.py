"""Refine/export the editable, own-authored Blender pigeon, with visual proofs.

The canonical model source is stadsduif-v2-basis.blend, kept outside Git under
the repository's binary rule. It contains our continuous modeled body, skinning
weights, three-bone armature, independently movable wing/tail surfaces, eyes,
bill and articulated feet; no downloaded 3D mesh. This script reproducibly
refines that source, packs textures and exports the complete browser asset.

Photo: Luc Viatour / Lucnix.be, CC BY-SA 3.0. Textured derivative assets are
provided under the same license. Projection is mirrored, not photogrammetry.
"""
import bpy, math, os, sys, json, struct
import numpy as np
from mathutils import Vector

OUT=os.environ.get('DUIVEN_MODEL_OUTPUT') or sys.exit('zet DUIVEN_MODEL_OUTPUT (uitvoermap voor model v2)')
BASE=os.environ.get('DUIVEN_MODEL_SOURCE',os.path.join(OUT,'stadsduif-v2-basis.blend'))
ITERATION=os.environ.get('DUIVEN_MODEL_ITERATION','06')
bpy.ops.wm.open_mainfile(filepath=BASE)
scene=bpy.context.scene

# The initial projection sampled background at the forehead, frontal crop and
# belly. Keep these pole-facing UVs in actual photographed plumage; preserve
# the registered side view and eye position. UV limits are observed silhouette
# samples, not a guessed solid color painted over a defect.
limits=[
    (.53,842,1430),(.67,723,1450),(.85,677,1450),(1.03,656,1360),
    (1.20,647,1240),(1.36,643,1100),(1.48,641,885),(1.60,642,828),
    (1.70,645,810),(1.78,594,796),(1.85,580,786),(1.92,568,773),
    (1.99,599,748),(2.014,621,724)]

def silhouette(z):
    z=max(limits[0][0],min(limits[-1][0],z))
    idx=next((i for i in range(len(limits)-1) if limits[i+1][0]>=z),len(limits)-2)
    t=(z-limits[idx][0])/(limits[idx+1][0]-limits[idx][0])
    return [limits[idx][k]*(1-t)+limits[idx+1][k]*t for k in [1,2]]

body=bpy.data.objects['Duif doorlopende huid']
for face in body.data.polygons:
    for li in face.loop_indices:
        p=body.data.vertices[body.data.loops[li].vertex_index].co
        z=max(.54,min(2.014,p.z))
        lo,hi=silhouette(z)
        # A narrow safety inset prevents antialiased ground/background texels.
        u=max(lo+7,min(hi-7,p.x*500+1050))
        body.data.uv_layers.active.data[li].uv=(u/1920,(z*500+74)/1324)

# Use the actual source photograph for high-frequency normal/roughness detail.
# These are explicit images with standard PBR connections, not shader-only
# procedural nodes, so the result survives the real GLB re-import check.
previous=next(i for i in bpy.data.images if 'archive-photo' in i.name)
photo=bpy.data.images.load(os.environ.get('DUIVEN_MODEL_PHOTO') or sys.exit('zet DUIVEN_MODEL_PHOTO (de bronfoto voor de textuur)'))
photo.pack()
for material in bpy.data.materials:
    if material.use_nodes:
        for node in material.node_tree.nodes:
            if node.type=='TEX_IMAGE' and node.image==previous: node.image=photo
w,h=photo.size
pixels=np.asarray(photo.pixels[:],dtype=np.float32).reshape(h,w,4)
gray=pixels[:,:,:3].mean(axis=2)
dy,dx=np.gradient(gray)
n=np.stack((-dx*2.5,-dy*2.5,np.ones_like(gray)),axis=2)
n/=np.linalg.norm(n,axis=2,keepdims=True)

def packed_image(name,array,colorspace='Non-Color'):
    # Detail maps are deliberately capped: original photographic normals at 4K
    # inflated the GLB without useful visible detail on the presentation stage.
    if colorspace=='Non-Color' and max(array.shape[:2])>1024:
        factor=1024/max(array.shape[:2]); height=max(1,round(array.shape[0]*factor)); width=max(1,round(array.shape[1]*factor))
        ys=np.linspace(0,array.shape[0]-1,height).astype(int); xs=np.linspace(0,array.shape[1]-1,width).astype(int)
        array=array[ys[:,None],xs[None,:]]
    height,width=array.shape[:2]
    image=bpy.data.images.new(name,width=width,height=height)
    image.colorspace_settings.name=colorspace
    image.pixels=np.concatenate((array,np.ones((height,width,1))),axis=2).astype(np.float32).ravel()
    ext='.jpg' if colorspace=='sRGB' else '.png'
    image.filepath_raw=os.path.join(OUT,name+ext); image.file_format='JPEG' if colorspace=='sRGB' else 'PNG'; image.save(quality=92); image.pack()
    return image

normal=packed_image('pluimdetail-normal',n*.5+.5)
green=pixels[:,:,1]/np.maximum(.02,pixels[:,:,0])
rough=np.clip(.86-.10*(green-1),.72,.90)
roughness=packed_image('pluimdetail-roughness',np.repeat(rough[:,:,None],3,axis=2))
for m in bpy.data.materials:
    if not m.name.startswith('Veren —'): continue
    nodes=m.node_tree.nodes; links=m.node_tree.links; bs=nodes.get('Principled BSDF')
    tex=nodes.new('ShaderNodeTexImage'); tex.image=normal
    norm=nodes.new('ShaderNodeNormalMap'); norm.inputs['Strength'].default_value=.35
    links.new(tex.outputs['Color'],norm.inputs['Color']); links.new(norm.outputs['Normal'],bs.inputs['Normal'])
    tex=nodes.new('ShaderNodeTexImage'); tex.image=roughness; links.new(tex.outputs['Color'],bs.inputs['Roughness'])

# Iteration 02 proved that clamping photo UVs only turns background bleed into
# smeared columns. Bake a new full wrap atlas instead: genuine gray chest feather
# patches wrap across the front, head plumage covers the crown, and back/belly
# patches close unphotographed areas. Soft angular/vertical masks preserve the
# accurately registered side/eye while avoiding an artificial center stripe.
size=2048
v,u=np.mgrid[0:size,0:size]/(size-1)
theta=u*2*math.pi; z=.426+v*(2.066-.426)
rings=np.array([tuple(vertex.co) for vertex in body.data.vertices],dtype=np.float32).reshape(150,96,3)
zrows=rings[:,0,2]
cx=(rings[:,:,0].max(axis=1)+rings[:,:,0].min(axis=1))/2
rx=(rings[:,:,0].max(axis=1)-rings[:,:,0].min(axis=1))/2
ry=(rings[:,:,1].max(axis=1)-rings[:,:,1].min(axis=1))/2
xworld=np.interp(z,zrows,cx)+np.interp(z,zrows,rx)*np.cos(theta)
yworld=np.interp(z,zrows,ry)*np.sin(theta)

def sample_photo(xpixel,ypixel):
    xx=np.clip(xpixel*w/1920,0,w-1.001)
    yy=np.clip(h-1-ypixel*h/1324,0,h-1.001)
    xi=xx.astype(np.int32); yi=yy.astype(np.int32)
    fx=(xx-xi)[:,:,None]; fy=(yy-yi)[:,:,None]
    return (pixels[yi,xi,:3]*(1-fx)+pixels[yi,xi+1,:3]*fx)*(1-fy)+(pixels[yi+1,xi,:3]*(1-fx)+pixels[yi+1,xi+1,:3]*fx)*fy

def smooth(a,b,value):
    t=np.clip((value-a)/(b-a),0,1); return t*t*(3-2*t)
def blend(a,b,weight): return a*(1-weight[:,:,None])+b*weight[:,:,None]

sidecolor=sample_photo(xworld*500+1050,1250-z*500)
chest=sample_photo(810+(theta-math.pi)*80,np.clip(650+(1.3-z)*300,640,918))
neckpatch=sample_photo(725+(theta-math.pi)*45,np.clip(400+(1.78-z)*480,401,650))
headpatch=sample_photo(730+(theta-math.pi)*28,np.clip(337+(z-1.88)*175,322,373))
front=blend(chest,neckpatch,smooth(1.28,1.49,z))
front=blend(front,headpatch,smooth(1.74,1.88,z))
backpatch=sample_photo(1065+65*np.sin(theta),np.clip(650+(1.3-z)*300,610,890))
backneck=sample_photo(775+18*np.sin(theta),np.clip(415+(1.72-z)*390,400,580))
backhead=sample_photo(720+20*np.sin(theta),np.clip(265+(2.04-z)*195,256,315))
backpatch=blend(backpatch,backneck,smooth(1.28,1.53,z))
backpatch=blend(backpatch,backhead,smooth(1.76,1.9,z))
atlas=blend(sidecolor,front,smooth(.40,.86,-np.cos(theta)))
atlas=blend(atlas,backpatch,smooth(.52,.92,np.cos(theta)))
crown=sample_photo(678+(xworld-np.interp(z,zrows,cx))*180,257+yworld*155)
atlas=blend(atlas,crown,smooth(1.987,2.052,z))
atlas=blend(atlas,chest,1-smooth(.46,.59,z))
atlasimage=packed_image('doorlopend-verenkleed',atlas,'sRGB')
bodymat=bpy.data.materials.new('Doorlopend verenkleed — Luc Viatour CC BY-SA 3.0'); bodymat.use_nodes=True
bs=bodymat.node_tree.nodes.get('Principled BSDF'); bs.inputs['Roughness'].default_value=.84
tex=bodymat.node_tree.nodes.new('ShaderNodeTexImage'); tex.image=atlasimage
bodymat.node_tree.links.new(tex.outputs['Color'],bs.inputs['Base Color'])
ady,adx=np.gradient(atlas.mean(axis=2))
an=np.stack((-adx*2,-ady*2,np.ones_like(adx)),axis=2); an/=np.linalg.norm(an,axis=2,keepdims=True)
animage=packed_image('doorlopend-verenkleed-normal',an*.5+.5)
nt=bodymat.node_tree.nodes.new('ShaderNodeTexImage'); nt.image=animage
nn=bodymat.node_tree.nodes.new('ShaderNodeNormalMap'); nn.inputs['Strength'].default_value=.28
bodymat.node_tree.links.new(nt.outputs['Color'],nn.inputs['Color']); bodymat.node_tree.links.new(nn.outputs['Normal'],bs.inputs['Normal'])
body.data.materials.clear(); body.data.materials.append(bodymat)
for face in body.data.polygons:
    indices=[body.data.loops[li].vertex_index%96 for li in face.loop_indices]
    seam=max(indices)-min(indices)>48
    for li in face.loop_indices:
        vi=body.data.loops[li].vertex_index; angle=vi%96/96
        if seam and angle<.5: angle+=1
        body.data.uv_layers.active.data[li].uv=(angle,(body.data.vertices[vi].co.z-.426)/(2.066-.426))

# Replace the white plate-like tail by twelve narrow dark tail feathers. The
# pale flight feathers remain on the closed wings; tail texture is sampled from
# the genuinely dark photographed tail, clear of its ground-colored edge.
tail=bpy.data.objects['TailRig']
for ob in list(tail.children): bpy.data.objects.remove(ob,do_unlink=True)
verts=[]; faces=[]; coords=[]
for i in range(12):
    spread=(i-5.5)/5.5
    a=Vector((.60,spread*.115,.52)); b=Vector((1.48-.045*abs(spread),spread*.16,.25+.018*abs(spread)))
    direction=b-a; cross=direction.normalized().cross(Vector((0,-1,0))).normalized()
    first=len(verts)
    for j in range(14):
        t=j/13
        for k in range(5):
            s=k/4*2-1; width=.025*math.sin(math.pi*t)**.42
            p=a+direction*t+cross*s*width+Vector((0,-.0015*(1-s*s)*math.sin(math.pi*t),0))
            verts.append(p); coords.append(((1580+200*t+5*s)/1920,1-(1033+62*t+3*s)/1324))
    for j in range(13):
        for k in range(4):
            aidx=first+j*5+k; faces.append((aidx,aidx+1,aidx+6,aidx+5))
data=bpy.data.meshes.new('Dunne donkere staartveren'); data.from_pydata(verts,[],faces); data.update()
ob=bpy.data.objects.new('Dunne donkere staartveren',data); bpy.context.collection.objects.link(ob)
data.materials.append(bpy.data.materials['Veren — foto Luc Viatour CC BY-SA 3.0'])
uv=data.uv_layers.new()
for face in data.polygons:
    face.use_smooth=True
    for li in face.loop_indices: uv.data[li].uv=coords[data.loops[li].vertex_index]
ob.parent=tail; ob.matrix_parent_inverse=tail.matrix_world.inverted()

for ob in scene.objects:
    if ob.type!='MESH': continue
    if ob.name.startswith('Oog '):
        for vertex in ob.data.vertices:
            vertex.co.y*=.94
    if ob.name=='Snavel en neusdop':
        for vertex in ob.data.vertices: vertex.co.x=-.90+(vertex.co.x+.90)*.82

# Fine mottled tarsus/toe skin avoids the first iteration's uniform pink plastic.
# Procedural pixels are deliberately subtle; the articulated scale geometry is
# already modeled at millimetric size relative to the bird.
size=256
y,x=np.mgrid[0:size,0:size]
rng=np.random.default_rng(307)
noise=rng.normal(0,.028,(size,size))+.025*np.sin(y*.30+x*.043)
skin=np.stack([.43+noise,.12+noise*.45,.102+noise*.38],axis=2)
skin=np.clip(skin,0,1)
skinimage=packed_image('poot-microkleur',skin,'sRGB')
for m in bpy.data.materials:
    if m.name not in ['Poot huid','Poot schub']: continue
    nodes=m.node_tree.nodes; links=m.node_tree.links; bs=nodes.get('Principled BSDF')
    tex=nodes.new('ShaderNodeTexImage'); tex.image=skinimage
    links.new(tex.outputs['Color'],bs.inputs['Base Color']); bs.inputs['Roughness'].default_value=.79
for ob in scene.objects:
    if ob.type!='MESH' or not any(m and m.name in ['Poot huid','Poot schub'] for m in ob.data.materials): continue
    uv=ob.data.uv_layers.active or ob.data.uv_layers.new()
    for face in ob.data.polygons:
        for li in face.loop_indices:
            p=ob.matrix_world @ ob.data.vertices[ob.data.loops[li].vertex_index].co
            uv.data[li].uv=(p.x*7+p.y*5,p.z*6+p.y*2)
    # One foot is slightly behind the other, with a wider natural stance.
    for vertex in ob.data.vertices:
        p=ob.matrix_world @ vertex.co
        p.x+=.12 if p.y>0 else -.025
        p.y*=1.12
        vertex.co=ob.matrix_world.inverted() @ p

# Keep the whole foot together, including its nails. Remove the initial hanging
# tuft fork: tarsi emerge directly from the existing feathered belly silhouette.
for ob in list(scene.objects):
    if ob.type=='MESH' and any(m and m.name=='Nagel hoorn' for m in ob.data.materials):
        for vertex in ob.data.vertices:
            p=ob.matrix_world @ vertex.co; p.x+=.12 if p.y>0 else -.025; p.y*=1.12
            vertex.co=ob.matrix_world.inverted() @ p
    if ob.type=='MESH' and ob.name.startswith('PigeonRoot Veren'):
        bpy.data.objects.remove(ob,do_unlink=True)

root=bpy.data.objects['PigeonRoot']
root['source_photo']='https://commons.wikimedia.org/wiki/File:Columba_livia_Luc_Viatour.jpg'
root['credit']='Luc Viatour / Lucnix.be — source photograph; adapted as projected texture'
root['license']='CC BY-SA 3.0 — textured derivative assets'
root['method']='Own continuous skinned geometry; mirrored photo projection, not photogrammetry'
root['limitations']='Baked photographic illumination; opposite side mirrored; extreme front/back UV stretching'

# Keep the full original image packed as editable source, but put a 2K JPEG in
# the browser materials. PBR detail images are capped separately above.
photo.use_fake_user=True
browserphoto=photo.copy(); browserphoto.name='Foto voor web — Luc Viatour CC BY-SA 3.0'
browserphoto.scale(2048,round(h*2048/w)); browserphoto.filepath_raw=os.path.join(OUT,'foto-voor-web.jpg'); browserphoto.file_format='JPEG'; browserphoto.save(quality=92); browserphoto.pack()
for material in bpy.data.materials:
    if material.use_nodes:
        for node in material.node_tree.nodes:
            if node.type=='TEX_IMAGE' and node.image==photo: node.image=browserphoto

scene.render.engine='CYCLES'; scene.cycles.samples=48
scene.render.resolution_x=1600; scene.render.resolution_y=1200; scene.render.resolution_percentage=100
scene.render.film_transparent=True; scene.render.image_settings.file_format='PNG'
scene.view_settings.view_transform='AgX'
camera=scene.camera; camera.data.type='ORTHO'; camera.data.ortho_scale=2.90

def aim(position):
    camera.location=position
    camera.rotation_euler=(Vector((.14,0,1.05))-camera.location).to_track_quat('-Z','Y').to_euler()
aim((-.65,-6,1.68))

# Pack this reproducible export/refinement recipe alongside the editable model.
text=bpy.data.texts.get('refine_pigeon_v2.py') or bpy.data.texts.new('refine_pigeon_v2.py')
with open(__file__) as f: text.from_string(f.read())
bpy.ops.object.select_all(action='DESELECT')
def select_tree(ob):
    ob.select_set(True)
    for child in ob.children: select_tree(child)
select_tree(root)
glb=os.path.join(OUT,'stadsduif-v2.glb')
bpy.ops.export_scene.gltf(filepath=glb,export_format='GLB',use_selection=True,export_apply=False,export_animations=False,export_extras=True)
bpy.ops.wm.save_as_mainfile(filepath=os.path.join(OUT,'stadsduif-v2.blend'))
for label,position in [('driekwart',(-.65,-6,1.68)),('profiel',(.10,-6,1.18)),('voor',(-5,-1.4,1.62)),('achter',(4,-3,1.8))]:
    aim(position); scene.render.filepath=os.path.join(OUT,f'iteratie-{ITERATION}-{label}.png'); bpy.ops.render.render(write_still=True)
aim((-.65,-6,1.68)); scene.render.filepath=os.path.join(OUT,'stadsduif-v2-rust.png'); bpy.ops.render.render(write_still=True)

# Verify the exact exported asset, including skinned rig names and materials.
for ob in list(scene.objects):
    if ob.type not in {'CAMERA','LIGHT'}: bpy.data.objects.remove(ob,do_unlink=True)
bpy.ops.import_scene.gltf(filepath=glb)
for name in ['PigeonRoot','BodyRig','NeckRig','HeadRig','WingLeft','WingRight','TailRig']:
    assert name in bpy.data.objects or any(name in a.bones for a in bpy.data.armatures), 'Missing rig '+name
scene.render.filepath=os.path.join(OUT,'stadsduif-v2-glb-check.png'); bpy.ops.render.render(write_still=True)
with open(glb,'rb') as f:
    f.read(12); length,kind=struct.unpack('<II',f.read(8)); data=json.loads(f.read(length))
stats={'bytes':os.path.getsize(glb),'meshes':len(data['meshes']),'materials':len(data['materials']),
       'triangles':sum(data['accessors'][p['indices']]['count']//3 for m in data['meshes'] for p in m['primitives']),
       'skins':len(data.get('skins',[])),'iteration':ITERATION,
       'blender':bpy.app.version_string,'license':'CC BY-SA 3.0',
       'source':'https://commons.wikimedia.org/wiki/File:Columba_livia_Luc_Viatour.jpg'}
with open(os.path.join(OUT,'model-evidence.json'),'w') as f: json.dump(stats,f,indent=2)
print('MODEL_V2_VERIFIED',json.dumps(stats))
