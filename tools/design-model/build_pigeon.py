"""Own-source pigeon form/rig study. Run with Blender --background --python this_file.

No downloaded meshes, texture photographs, language models, or user media.
Generated binaries deliberately live outside the repository.
"""
import bpy, math, random, os, sys
from mathutils import Vector

OUT = os.environ.get('DUIVEN_MODEL_OUTPUT') or sys.exit('zet DUIVEN_MODEL_OUTPUT (uitvoermap voor het model)')
os.makedirs(OUT, exist_ok=True)
random.seed(18)
bpy.ops.object.select_all(action='SELECT')
bpy.ops.object.delete(use_global=False)

def material(name, color, rough=.68, metallic=0):
    mat = bpy.data.materials.new(name)
    mat.diffuse_color = (*color, 1)
    mat.use_nodes = True
    bs = mat.node_tree.nodes.get('Principled BSDF')
    bs.inputs['Base Color'].default_value = (*color, 1)
    bs.inputs['Roughness'].default_value = rough
    bs.inputs['Metallic'].default_value = metallic
    return mat

slate = material('Leigrijs', (.17,.205,.235))
breast = material('Borst blauwgrijs', (.22,.255,.285))
headmat = material('Kop', (.105,.13,.155), .72)
green = material('Hals groen', (.045,.135,.118), .36, .42)
purple = material('Hals violet', (.14,.065,.125), .4, .32)
wingmats = [material('Dekveren '+str(i), (.27+i*.015,.30+i*.015,.325+i*.016)) for i in range(5)]
darkmats = [material('Slagveren '+str(i), (.075+i*.008,.09+i*.008,.105+i*.008)) for i in range(4)]
black = material('Pupil en nagels', (.009,.012,.015), .19)
beakmat = material('Snavel hoorn', (.075,.083,.088), .48)
cere = material('Neusdop', (.58,.585,.57), .86)
iris = material('Iris koper', (.61,.195,.035), .4)
skin = material('Poten', (.29,.075,.092), .72)
scales = material('Pootschubben', (.39,.12,.14), .76)

# Repeated feather microstructure, UV mapped identically on each feather.
# This is exported as an embedded image, so Blender and the browser agree.
tex = bpy.data.images.new('Veerstructuur', width=512, height=512)
pixels = []
for y in range(512):
    t = y/511
    for x in range(512):
        s = (x/511-.5)*2
        vane = .93 + .045*math.sin(190*t+27*abs(s))
        shaft = .085*math.exp(-s*s*1200)
        edge = .10*abs(s)**9
        shade = max(.4, vane-shaft-edge + random.uniform(-.025,.025))
        pixels.extend((shade,shade,shade,1))
tex.pixels = pixels
tex.filepath_raw = os.path.join(OUT, 'veerstructuur.png')
tex.file_format = 'PNG'
tex.save()
tex.pack()
for mat in wingmats + darkmats + [green,purple]:
    nodes=mat.node_tree.nodes; links=mat.node_tree.links
    bs=nodes.get('Principled BSDF'); color=tuple(bs.inputs['Base Color'].default_value)
    # Explicit color images survive glTF export. Shader MixRGB/Bump nodes did
    # not round-trip faithfully in the actual GLB inspection, so avoid them.
    def srgb(v): return 12.92*v if v <= .0031308 else 1.055*v**(1/2.4)-.055
    tint=[srgb(v) for v in color[:3]]
    colored=bpy.data.images.new(mat.name+' kleur',width=512,height=512)
    colored.pixels=[v for i in range(0,len(pixels),4) for v in (pixels[i]*tint[0],pixels[i]*tint[1],pixels[i]*tint[2],1)]
    colored.filepath_raw=os.path.join(OUT,mat.name.replace(' ','-')+'.png'); colored.file_format='PNG'; colored.save(); colored.pack()
    img=nodes.new('ShaderNodeTexImage'); img.image=colored
    links.new(img.outputs['Color'],bs.inputs['Base Color'])

def group(name, at, parent=None):
    ob=bpy.data.objects.new(name,None); bpy.context.collection.objects.link(ob); ob.location=at
    if parent: ob.parent=parent; ob.matrix_parent_inverse=parent.matrix_world.inverted()
    bpy.context.view_layer.update()
    return ob

root=group('PigeonRoot',(0,0,0))
bodyrig=group('BodyRig',(0,0,1),root)
neckrig=group('NeckRig',(-.34,0,1.4),bodyrig)
leftwing=group('WingLeft',(-.2,-.28,1.29),bodyrig)
rightwing=group('WingRight',(-.2,.28,1.29),bodyrig)
tailrig=group('TailRig',(.54,0,.85),bodyrig)

def parent_keep(ob,parent):
    if parent: ob.parent=parent; ob.matrix_parent_inverse=parent.matrix_world.inverted()

def ellipsoid(name, at, scale, mat, parent=None, rotate=0):
    bpy.ops.mesh.primitive_uv_sphere_add(segments=48, ring_count=24, location=at)
    ob=bpy.context.object; ob.name=name; ob.scale=scale; ob.rotation_euler[1]=rotate
    ob.data.materials.append(mat)
    for p in ob.data.polygons: p.use_smooth=True
    parent_keep(ob,parent)
    return ob

def tube(name, points, radius, mat, parent=None):
    cu=bpy.data.curves.new(name,'CURVE'); cu.dimensions='3D'; cu.resolution_u=10
    cu.bevel_depth=radius; cu.bevel_resolution=3
    spl=cu.splines.new('BEZIER'); spl.bezier_points.add(len(points)-1)
    for b,p in zip(spl.bezier_points,points): b.co=p; b.handle_left_type='AUTO'; b.handle_right_type='AUTO'
    ob=bpy.data.objects.new(name,cu); bpy.context.collection.objects.link(ob); ob.data.materials.append(mat)
    bpy.context.view_layer.objects.active=ob; ob.select_set(True); bpy.ops.object.convert(target='MESH'); ob.select_set(False)
    parent_keep(ob,parent); return ob

bodymesh=ellipsoid('Lichaam',(.17,0,1.04),(.69,.365,.44),breast,bodyrig,rotate=.32)
cropmesh=ellipsoid('Borst',(-.28,0,1.18),(.36,.335,.47),breast,bodyrig,rotate=-.28)
neckmesh=ellipsoid('Hals',(-.39,0,1.54),(.245,.23,.40),headmat,neckrig,rotate=-.20)
headmesh=ellipsoid('Kop',(-.49,0,1.88),(.215,.173,.202),headmat,neckrig,rotate=.02)

def fuse(name, objects):
    bpy.ops.object.select_all(action='DESELECT')
    for ob in objects: ob.select_set(True)
    bpy.context.view_layer.objects.active=objects[0]
    bpy.ops.object.join(); ob=bpy.context.object; ob.name=name
    bpy.ops.object.transform_apply(location=False,rotation=True,scale=True)
    mod=ob.modifiers.new('Doorlopende anatomie','REMESH'); mod.mode='VOXEL'; mod.voxel_size=.015; mod.use_smooth_shade=True
    bpy.ops.object.modifier_apply(modifier=mod.name)
    mod=ob.modifiers.new('Zachte aansluiting','SMOOTH'); mod.factor=1.4; mod.iterations=8
    bpy.ops.object.modifier_apply(modifier=mod.name)
    for p in ob.data.polygons: p.use_smooth=True
fuse('Lichaam en borst',[bodymesh,cropmesh])
fuse('Hals en kop',[neckmesh,headmesh])

def feather(name,start,end,width,normal,mat,parent,camber=.014):
    a=Vector(start); b=Vector(end); d=b-a; n=Vector(normal).normalized(); side=d.normalized().cross(n).normalized()
    verts=[]; faces=[]; rows=13; cols=7
    for j in range(rows):
        t=j/(rows-1); w=width*(math.sin(math.pi*t)**.58)*(.87+.13*t)
        for k in range(cols):
            s=k/(cols-1)*2-1
            p=a+d*t+side*s*w+n*(camber*(1-s*s)*math.sin(math.pi*t)+.005*t*t)
            verts.append(p)
    for j in range(rows-1):
        for k in range(cols-1):
            i=j*cols+k; faces.append((i,i+1,i+cols+1,i+cols))
    mesh=bpy.data.meshes.new(name); mesh.from_pydata(verts,[],faces); mesh.update()
    uv=mesh.uv_layers.new()
    for poly in mesh.polygons:
        poly.use_smooth=True
        for li in poly.loop_indices:
            vi=mesh.loops[li].vertex_index; uv.data[li].uv=(vi%cols/(cols-1),vi//cols/(rows-1))
    ob=bpy.data.objects.new(name,mesh); bpy.context.collection.objects.link(ob); mesh.materials.append(mat); parent_keep(ob,parent)
    return ob

# Tail fan, individual primary and secondary feathers, then overlapping coverts.
for i in range(12):
    y=(i-5.5)*.031
    feather('Staartveer',(.56,y,.86),(1.28+(.05-abs(y)*.2),y*1.28,.43),.052,(.2,0,1),darkmats[i%4],tailrig)
for side,wing in [(-1,leftwing),(1,rightwing)]:
    ellipsoid('Vleugelbasis',(.23,side*.315,1.10),(.56,.105,.29),wingmats[2],wing,rotate=.39)
    for i in range(11):
        t=i/10
        feather('Handpen',(-.04+t*.16,side*(.28+.055*math.sin(t*math.pi)),1.24-t*.07),(.80+t*.27,side*(.23+.16*(1-t)),.59+t*.06),.047,(0,side,0),darkmats[i%4],wing)
    for row in range(5):
        for i in range(10):
            t=i/9
            x=-.22+t*.72+row*.025; z=1.36-row*.093-t*.13
            y=side*(.335+.075*math.sin(math.pi*t)+.012*(4-row))
            mat=darkmats[i%4] if row in [2,4] else wingmats[(i+row)%5]
            feather('Vleugeldekveer',(x,y,z),(x+.30,y+side*.014,z-.20),.058,(0,side,0),mat,wing,.012)

# Short overlapping neck feathers follow a tapered elliptical neck.
for row in range(15):
    z=1.24+row*.036
    cx=-.39-(z-1.54)*.20
    factor=math.sqrt(max(.1,1-((z-1.54)/.40)**2))
    radx=.245*factor+.022; rady=.23*factor+.016
    for i in range(38):
        theta=2*math.pi*(i+(row%2)*.5)/38
        nx=math.cos(theta); ny=math.sin(theta)
        at=(cx+radx*nx,rady*ny,z)
        end=(at[0]+nx*.018,at[1]+ny*.018,z-.10)
        mat=(green if row>6 else purple) if row<13 else headmat
        feather('Halsveer',at,end,.019,(nx,ny,.06),mat,neckrig,.005)

# Cere, tapered horn beak and small side-facing eye with no mascot exaggeration.
verts=[(-.65,-.059,1.905),(-.65,.059,1.905),(-.665,0,1.953),(-.88,0,1.858),(-.68,-.046,1.87),(-.68,.046,1.87)]
mesh=bpy.data.meshes.new('Snavel'); mesh.from_pydata(verts,[],[(0,1,2),(0,2,3),(2,1,3),(0,3,4),(1,5,3),(4,3,5),(0,4,5,1)]); mesh.materials.append(beakmat)
ob=bpy.data.objects.new('Snavel',mesh); bpy.context.collection.objects.link(ob); parent_keep(ob,neckrig)
bevel=ob.modifiers.new('Zachte hoornranden','BEVEL'); bevel.width=.008; bevel.segments=3
for side in [-1,1]:
    ellipsoid('Neusdop',(-.686,side*.027,1.943),(.048,.028,.025),cere,neckrig,rotate=-.23)
    ellipsoid('Neusgat',(-.718,side*.041,1.935),(.012,.005,.006),beakmat,neckrig)
    ellipsoid('Oogrand',(-.548,side*.157,1.93),(.035,.010,.035),slate,neckrig)
    ellipsoid('Iris',(-.548,side*.166,1.93),(.026,.007,.026),iris,neckrig)
    ellipsoid('Pupil',(-.551,side*.172,1.93),(.014,.004,.016),black,neckrig)
    # Legs, articulated toes, horn tips. Flat foot plane establishes weight.
    y=side*.205
    ellipsoid('Dij',(.13,y,.69),(.115,.09,.20),breast,bodyrig,rotate=.15)
    tube('Tarsus',[(.15,y,.59),(.21,y,.34),(.10,y,.12)],.031,skin,root)
    for k in [-1,0,1]:
        end=(-.22-abs(k)*-.04,y+k*.125,.04)
        tube('Teen',[(.10,y,.12),(-.01,y+k*.062,.061),end],.020,skin,root)
        tube('Nagel',[end,(end[0]-.043,end[1]+k*.01,.025)],.011,black,root)
        for j in range(3):
            f=(j+1)/4
            ellipsoid('Teenschub',(.10+(end[0]-.1)*f,y+k*.125*f,.105-(.06*f)),(.026,.023,.008),scales,root)
    tube('Achterteen',[(.1,y,.12),(.30,y+.04*side,.05),(.37,y+.06*side,.045)],.018,skin,root)
    for j in range(7):
        ellipsoid('Pootschub',(.11+j*.014,y-.027,.16+j*.029),(.036,.007,.014),scales,root)

# Collapse meshes per rig and material: preserve animatable groups without
# hundreds of browser draw calls. Retain editable geometry in Blender.
bpy.context.view_layer.update()
for rig in [root,bodyrig,neckrig,leftwing,rightwing,tailrig]:
    children=[o for o in list(rig.children) if o.type=='MESH']
    buckets={}
    for ob in children: buckets.setdefault(ob.data.materials[0].name,[]).append(ob)
    for matname,obs in buckets.items():
        bpy.ops.object.select_all(action='DESELECT')
        for ob in obs: ob.select_set(True)
        bpy.context.view_layer.objects.active=obs[0]
        bpy.ops.object.join(); obs[0].name=rig.name+'_'+matname

# Portable materials use either a base factor or an embedded, tinted UV image.

scene=bpy.context.scene
scene.render.engine='CYCLES'; scene.cycles.samples=48
scene.world.color=(.32,.32,.32)
scene.view_settings.view_transform='AgX'
scene.render.resolution_x=1600; scene.render.resolution_y=1200; scene.render.resolution_percentage=100
scene.render.image_settings.file_format='PNG'; scene.render.film_transparent=True

def area(name,at,power,size,color):
    bpy.ops.object.light_add(type='AREA',location=at); ob=bpy.context.object; ob.name=name
    ob.data.energy=power; ob.data.shape='DISK'; ob.data.size=size; ob.data.color=color
    ob.rotation_euler=(Vector((0,0,1))-ob.location).to_track_quat('-Z','Y').to_euler()
area('Groot raam',(-3,-4,6),500,5,(.9,.95,1))
area('Zachte invulling',(1,3,3),380,4,(1,.9,.78))
area('Randlicht',(3,-1,4),280,3,(1,1,1))
bpy.ops.object.camera_add(location=(-2.65,-5.8,2.70))
camera=bpy.context.object; camera.rotation_euler=(Vector((.12,0,1.07))-camera.location).to_track_quat('-Z','Y').to_euler()
camera.data.type='ORTHO'; camera.data.ortho_scale=3.25; scene.camera=camera

bpy.ops.object.select_all(action='DESELECT')
def select_tree(ob):
    ob.select_set(True)
    for child in ob.children: select_tree(child)
select_tree(root)
bpy.ops.export_scene.gltf(filepath=os.path.join(OUT,'stadsduif.glb'),export_format='GLB',use_selection=True,export_apply=True,export_animations=False)
bpy.ops.wm.save_as_mainfile(filepath=os.path.join(OUT,'stadsduif.blend'))
scene.render.filepath=os.path.join(OUT,'stadsduif-rust.png')
bpy.ops.render.render(write_still=True)
print('DUIVEN_MODEL_COMPLETE',OUT)
