"""Anatomical v3 and two authored, bounded reaction clips from frozen v2.

Editable canonical source: model-v2/stadsduif-v2.blend. All generated assets
remain outside Git. Photo and textured derivatives: Luc Viatour, CC BY-SA 3.0.
Run in Blender 5 with --background --python this_file.
"""
import bpy, bmesh, math, os, sys, json, struct, hashlib
import numpy as np
from mathutils import Vector, Quaternion

OUT=os.environ.get('DUIVEN_MODEL_OUTPUT') or sys.exit('zet DUIVEN_MODEL_OUTPUT (uitvoermap voor model v3)')
SOURCE=os.environ.get('DUIVEN_MODEL_SOURCE') or sys.exit('zet DUIVEN_MODEL_SOURCE (het .blend-bestand van model v2)')
ITERATION=os.environ.get('DUIVEN_MODEL_ITERATION','04')
# Een meetronde bouwt dezelfde geometrie, maar exporteert en rendert niet: de
# maten in model-evidence.json worden opnieuw berekend terwijl GLB, .blend en
# beelden blijven staan. De GLB-export is byte-identiek bij een
# herbouw uit dezelfde bron, de Cycles-beelden zijn dat niet, dus een meting
# mag ze niet aanraken.
MEASURE_ONLY=os.environ.get('DUIVEN_MODEL_MEASURE_ONLY')=='1'
os.makedirs(OUT,exist_ok=True)
bpy.ops.wm.open_mainfile(filepath=SOURCE)
scene=bpy.context.scene; root=bpy.data.objects['PigeonRoot']; rig=bpy.data.objects['PigeonSkeleton']
photo_material=bpy.data.materials['Veren — foto Luc Viatour CC BY-SA 3.0']
body=bpy.data.objects['Duif doorlopende huid']
for ob in [bpy.data.objects['Oog -1'],bpy.data.objects['Oog 1']]:
    if not any(m.type=='ARMATURE' for m in ob.modifiers):
        modifier=ob.modifiers.new('HeadRig volgt altijd mee','ARMATURE'); modifier.object=rig
for material in bpy.data.materials:
    material.use_backface_culling=False
    if material.use_nodes:
        for n in material.node_tree.nodes:
            if n.type=='NORMAL_MAP': n.inputs['Strength'].default_value*=.65

def mesh(name,vertices,faces,uvs,material=photo_material):
    data=bpy.data.meshes.new(name); data.from_pydata(vertices,[],faces); data.update()
    ob=bpy.data.objects.new(name,data); bpy.context.collection.objects.link(ob)
    data.materials.append(material); uv=data.uv_layers.new()
    for p in data.polygons:
        p.use_smooth=True
        for li in p.loop_indices: uv.data[li].uv=uvs[data.loops[li].vertex_index]
    return ob

def skin(ob,weight):
    world=ob.matrix_world.copy(); ob.parent=rig; ob.matrix_world=world
    for v in ob.data.vertices:
        for bone,w in weight(v.co).items():
            if w>0:
                group=ob.vertex_groups.get(bone) or ob.vertex_groups.new(name=bone)
                group.add([v.index],float(w),'REPLACE')
    mod=ob.modifiers.new('Anatomische vervorming','ARMATURE'); mod.object=rig

def photo_uv(x,y): return (x/1920,1-y/1324)

def feather(name,start,end,width,normal,uv_start,uv_end,bone,camber=.003,material=None):
    a=Vector(start); delta=Vector(end)-a; normal=Vector(normal).normalized()
    cross=delta.normalized().cross(normal).normalized(); vertices=[]; faces=[]; uvs=[]
    times=list(np.linspace(0,.9,15))+[.94,.965,.982,.994,1]
    cap_start=1-min(.075,width/delta.length)
    for j,t in enumerate(times):
        for k in range(5):
            s=k/2-1
            w=width*(.76+.24*math.sin(math.pi*t))*min(1,t/.08)
            if t>cap_start:w*=math.sqrt(max(0,1-((t-cap_start)/(1-cap_start))**2))
            vertices.append(a+delta*t+cross*(s*w+.013*math.sin(math.pi*t))+normal*camber*(1-s*s)*math.sin(math.pi*t))
            uvs.append(((s+1)/2,t) if material else photo_uv(uv_start[0]*(1-t)+uv_end[0]*t+s*5,uv_start[1]*(1-t)+uv_end[1]*t+s*2))
    for j in range(len(times)-1):
        for k in range(4):
            i=j*5+k; faces.append((i,i+1,i+6,i+5))
    ob=mesh(name,vertices,faces,uvs,material or photo_material); skin(ob,lambda p:{bone:1}); return ob

def sleeve(name,sections,bone='BodyRig'):
    # Closed tapered loft, with upper rings inside the real body and lower
    # rings overlapping the tarsus. Not a disconnected sphere or floating tuft.
    vertices=[]; faces=[]; uvs=[]; steps=40
    for j,(x,y,z,rx,ry) in enumerate(sections):
        for i in range(steps):
            theta=2*math.pi*i/steps
            vertices.append((x+rx*math.cos(theta),y+ry*math.sin(theta),z))
            uvs.append(photo_uv(806+34*math.cos(theta),785+j*8+10*math.sin(theta)))
    for j in range(len(sections)-1):
        for i in range(steps):
            a=j*steps+i; b=j*steps+(i+1)%steps
            faces.append((a,b,b+steps,a+steps))
    faces.extend([tuple(reversed(range(steps))),tuple(range((len(sections)-1)*steps,len(sections)*steps))])
    ob=mesh(name,vertices,faces,uvs); skin(ob,lambda p:{bone:1}); return ob

# Connect feathered tibia to belly, covering the red flat-cut tarsus top.
for side in [-1,1]:
    y=side*.16*1.12; stagger=.12 if side>0 else-.025
    x=.19+(.055 if side>0 else 0)+stagger
    sleeve('Aansluitende bevederde poot '+str(side),[
        (x-.015,y,.64,.025,.025),(x-.014,y,.58,.070,.064),
        (x-.008,y,.51,.075,.066),(x-.014,y,.455,.058,.051),
        (x-.035,y,.40,.037,.034),(x-.044,y,.355,.025,.024),
        (x-.045,y,.347,.020,.020)])

# Seal all open toe tubes. Add a genuinely connected tapered keratin rear claw.
feet=bpy.data.objects['PigeonRoot Poot huid']
bm=bmesh.new(); bm.from_mesh(feet.data)
bmesh.ops.holes_fill(bm,edges=[e for e in bm.edges if e.is_boundary],sides=32)
bm.to_mesh(feet.data); bm.free()
for side in [-1,1]:
    stagger=.12 if side>0 else-.025
    end=Vector((.17+stagger,(side*.16+side*.06)*1.12,.021))
    vertices=[]; faces=[]; uvs=[]
    for j in range(9):
        t=j/8; center=end+Vector((.040*t,side*.012*t,-.010*t*t)); radius=.010*(1-t)+.0004
        for i in range(10):
            angle=2*math.pi*i/10
            vertices.append(center+Vector((0,math.cos(angle)*radius,math.sin(angle)*radius)))
            uvs.append((i/10,t))
    for j in range(8):
        for i in range(10):
            a=j*10+i; b=j*10+(i+1)%10; faces.append((a,b,b+10,a+10))
    faces.append(tuple(range(80,90)))
    claw=mesh('Achterteen gesloten nagel '+str(side),vertices,faces,uvs,bpy.data.materials['Nagel hoorn'])
    claw.parent=root

# A tapered rump joins body and tail root in real geometry. Tail starts deeply
# inside this loft; upper coverts overlap it, so there is no flat detached fan.
sleeve('Stuit en staartwortel',[(.52,0,.72,.10,.10),(.61,0,.65,.20,.14),(.72,0,.59,.235,.145),(.84,0,.52,.19,.12),(.92,0,.465,.125,.095),(.98,0,.425,.025,.065)])
tail=bpy.data.objects['Dunne donkere staartveren']
for v in tail.data.vertices:
    t=max(0,min(1,(v.co.x-.60)/.88))
    v.co.x-=.16*(1-t); v.co.z+=.14*(1-t)
for i in range(11):
    y=(i-5)*.022
    feather('Overlappende staartdekveer '+str(i),(.57,y,.64),(1.02-abs(y)*.6,y*1.2,.425),.035,(.3,0,1),(1410,920),(1500,979),'BodyRig')

# De v2-bron levert de snavel als platte wig van elf punten zonder washuid: te
# smal, te spits en met twee losse neusdopbolletjes uit v1. Een stadsduif heeft
# een steviger hoornsnavel met stompe punt en daarboven één doorlopende
# gezwollen witte washuid rond de neusgaten. Beide worden hier procedureel
# opgebouwd, zodat de kop reproduceerbaar uit Git komt en niet uit een
# handmatig bewerkte .blend.
bpy.data.objects.remove(bpy.data.objects['Snavel en neusdop'],do_unlink=True)

def loft(name,rings,material,bone,uv):
    # Gesloten taps volume uit even grote ringen, met dichte kappen voor en
    # achter. Normalen worden herberekend, zodat een ring met omgekeerde
    # looprichting geen zwarte binnenkant in de GLB oplevert.
    steps=len(rings[0]); vertices=[]; uvs=[]; faces=[]
    for j,ring in enumerate(rings):
        for i,point in enumerate(ring):
            vertices.append(point); uvs.append(uv(j/(len(rings)-1),i/steps))
    for j in range(len(rings)-1):
        for i in range(steps):
            a=j*steps+i; b=j*steps+(i+1)%steps; faces.append((a,b,b+steps,a+steps))
    faces.append(tuple(reversed(range(steps)))); faces.append(tuple(range((len(rings)-1)*steps,len(rings)*steps)))
    ob=mesh(name,vertices,faces,uvs,material)
    bm=bmesh.new(); bm.from_mesh(ob.data); bmesh.ops.recalc_face_normals(bm,faces=bm.faces); bm.to_mesh(ob.data); bm.free()
    skin(ob,lambda p:{bone:1}); return ob

# Snavelas van de ingebedde basis tot de punt, met een lichte neerwaartse
# welving van de culmen. De basis ligt bewust binnen de kophuid.
BEAK_BASE_X=-.886; BEAK_BASE_Z=1.840; BEAK_LENGTH=.186; BEAK_DROP=.134; BEAK_CURVE=.018
def beak_axis(t): return (BEAK_BASE_X-BEAK_LENGTH*t,BEAK_BASE_Z-BEAK_DROP*t-BEAK_CURVE*math.sin(math.pi*t))
def beak_dims(t): return (.048*max(0,1-t)**.55+.008,.034*max(0,1-t)**.70+.005,.032*max(0,1-t)**.60+.005)
def profile_point(t,dims,angle,power,grow=1.,tuck=0.):
    # Superellips rond de snavelas: een scherpere mondrand en culmen voor de
    # hoornschede, een rondere doorsnede voor de washuid. `tuck` trekt de
    # onderflank naar binnen, zodat de washuid rond op de mondrand eindigt en
    # geen vierkante plaat wordt; `grow` legt de neusgaten net buiten het
    # washuidoppervlak.
    x,z=beak_axis(t); half,above,below=dims; s=math.sin(angle); c=math.cos(angle)
    half*=1-tuck*max(0.,-c)**1.2
    return (x,grow*half*math.copysign(abs(s)**power,s),z+grow*(above if c>0 else below)*math.copysign(abs(c)**power,c))
def section(t,dims,steps,power,tuck=0.): return [profile_point(t,dims,2*math.pi*i/steps,power,tuck=tuck) for i in range(steps)]

# Fotoprojectie langs de snavelas van de bronfoto: basis naar punt, met de
# rug-buikrichting op de loodrechte van die as. Links en rechts nemen hetzelfde
# fotostuk, zoals de rest van het gespiegelde verenkleed.
def beak_uv(t,u):
    lift=-12*math.cos(2*math.pi*u)
    return photo_uv(603-105*t-.5*lift,341+54*t-.867*lift)
BEAK_RINGS=[t*t*(3-2*t) for t in [j/15 for j in range(16)]]
loft('Snavel hoornschede',[section(t,beak_dims(t),18,1.3) for t in BEAK_RINGS],photo_material,'HeadRig',beak_uv)

# Washuid: één doorlopende gezwollen band over de snavelbasis tegen het
# voorhoofd, niet twee losse bolletjes. De achterste ringen liggen in de kop,
# de voorste lopen dun uit op de hoornschede, zodat er geen harde rand op de
# snavel staat. De zwelling is maximaal waar de band uit de voorhoofdsveren komt.
CERE_BACK=-.06; CERE_FRONT=.55; CERE_POWER=1.05; CERE_TUCK=.45
def cere_dims(t):
    s=(t-CERE_BACK)/(CERE_FRONT-CERE_BACK)
    swell=1. if s<=.68 else .02+.98*(.5+.5*math.cos(math.pi*min(1,(s-.68)/.32)))
    half,above,below=beak_dims(t)
    return (half+(.20*half+.0090)*swell,above+(.16*above+.0098)*swell,below*(1-.18*swell))
cere_material=bpy.data.materials.new('Washuid'); cere_material.use_nodes=True; cere_material.use_backface_culling=False
cere_shader=cere_material.node_tree.nodes['Principled BSDF']
cere_shader.inputs['Base Color'].default_value=(.815,.812,.792,1); cere_shader.inputs['Roughness'].default_value=.88
CERE_RINGS=[CERE_BACK+(CERE_FRONT-CERE_BACK)*(j/15) for j in range(16)]
loft('Washuid boven de snavelbasis',[section(t,cere_dims(t),20,CERE_POWER,CERE_TUCK) for t in CERE_RINGS],cere_material,'HeadRig',lambda j,u:(u,j))

# De neusgaten zitten in de washuid zelf: twee donkere spleten op de onderflank
# van de zwelling, niet twee losse bolletjes naast de snavel. Ze volgen het
# gebogen washuidoppervlak, zodat geen vin buiten de band uitsteekt.
nostril_material=bpy.data.materials.new('Neusgat'); nostril_material.use_nodes=True; nostril_material.use_backface_culling=False
nostril_shader=nostril_material.node_tree.nodes['Principled BSDF']
nostril_shader.inputs['Base Color'].default_value=(.055,.052,.050,1); nostril_shader.inputs['Roughness'].default_value=.62
NOSTRIL_T=.378; NOSTRIL_ANGLE=math.pi*.575
for side in [-1,1]:
    rings=[]
    for grow,narrow in [(1.006,1.),(1.017,.88),(1.026,.62),(1.032,.22)]:
        ring=[]
        for i in range(12):
            u=2*math.pi*i/12; t=NOSTRIL_T+.085*narrow*math.cos(u)
            ring.append(profile_point(t,cere_dims(t),side*(NOSTRIL_ANGLE+.23*narrow*math.sin(u)),CERE_POWER,grow,CERE_TUCK))
        rings.append(ring)
    loft('Neusgat in de washuid '+str(side),rings,nostril_material,'HeadRig',lambda j,u:(u,j))

# A narrow source-photo projection makes repeated black/white blades. Author a
# single continuous neutral flight-feather microtexture instead: asymmetrical
# vanes, subtle barbs and rachis. The photographed outer coverts stay intact.
fy,fx=np.mgrid[0:512,0:128]/np.array([511,127])[:,None,None]
fs=fx*2-1
grain=.97+.035*np.sin(fy*780+np.abs(fs)*42)+.018*np.sin(fy*1800+fs*11)
shaft=.06*np.exp(-fs*fs*1700)
shade=grain-shaft-.045*np.abs(fs)**8
rgb=shade[:,:,None]*np.array([.15,.163,.181])[None,None,:]
flightimage=bpy.data.images.new('Slagveer fijne baarden',width=128,height=512)
flightimage.pixels=np.concatenate((rgb,np.ones((512,128,1))),axis=2).astype(np.float32).ravel()
flightimage.filepath_raw=os.path.join(OUT,'slagveer-detail.jpg'); flightimage.file_format='JPEG'; flightimage.save(quality=92); flightimage.pack()
flightmat=bpy.data.materials.new('Natuurlijke grijze slagveren'); flightmat.use_nodes=True
bs=flightmat.node_tree.nodes.get('Principled BSDF'); bs.inputs['Roughness'].default_value=.82
tex=flightmat.node_tree.nodes.new('ShaderNodeTexImage'); tex.image=flightimage
flightmat.node_tree.links.new(tex.outputs['Color'],bs.inputs['Base Color'])
flight_palette=[]
for variation,factor in enumerate([.88,.95,1,1.055]):
    mat=flightmat.copy(); mat.name='Slagveer tint '+str(variation)
    variant=flightimage.copy(); variant.name='Slagveer tintbeeld '+str(variation)
    variant.pixels=np.concatenate((rgb*factor,np.ones((512,128,1))),axis=2).astype(np.float32).ravel()
    variant.filepath_raw=os.path.join(OUT,'slagveer-tint-'+str(variation)+'.jpg'); variant.file_format='JPEG'; variant.save(quality=92); variant.pack()
    next(n for n in mat.node_tree.nodes if n.type=='TEX_IMAGE').image=variant
    flight_palette.append(mat)

# Replace the old wing empties with real skeleton controls. One armature action
# then carries every animated channel, avoiding fragmented GLB action exports.
bpy.ops.object.select_all(action='DESELECT'); rig.select_set(True); bpy.context.view_layer.objects.active=rig
bpy.ops.object.mode_set(mode='EDIT')
for side,label in [(-1,'Left'),(1,'Right')]:
    name='Wing'+label; old=bpy.data.objects[name]; old.name=name+'Legacy'
    bone=rig.data.edit_bones.new(name); bone.head=(-.28,side*.22,1.25); bone.tail=bone.head+Vector((0,0,.2)); bone.parent=rig.data.edit_bones['BodyRig']
    for i in range(12):
        bone=rig.data.edit_bones.new(name+'Fan'+str(i)); bone.head=(-.03+i*.020,side*(.30+.008*i),1.13-.012*i)
        bone.tail=bone.head+Vector((0,0,.12)); bone.parent=rig.data.edit_bones[name]
bpy.ops.object.mode_set(mode='OBJECT')
for side,label in [(-1,'Left'),(1,'Right')]:
    name='Wing'+label; legacy=bpy.data.objects[name+'Legacy']
    for ob in list(legacy.children):
        world=ob.matrix_world.copy(); ob.parent=None; ob.matrix_world=world
        # Collapse world transform into mesh so generated and inherited feathers
        # share the same armature coordinate system.
        ob.data.transform(ob.matrix_world); ob.matrix_world.identity()
        # The old closed wing had a second long, pale flight-feather layer.
        # New articulated primaries replace it; keeping both makes stray white
        # spears above the tail. Preserve the photographed coverts and bars.
        bm=bmesh.new(); bm.from_mesh(ob.data); pending=set(bm.verts); remove=[]
        while pending:
            stack=[pending.pop()]; component=[]
            while stack:
                vtx=stack.pop(); component.append(vtx)
                for edge in vtx.link_edges:
                    neighbor=edge.other_vert(vtx)
                    if neighbor in pending:pending.remove(neighbor);stack.append(neighbor)
            if len(component)==75 and max(v.co.x for v in component)-min(v.co.x for v in component)>.4:
                remove.extend(component)
            elif len(component)==75:
                # Separate tiny edge-coverts from the underlying sheet enough
                # to survive glTF triangulation without coplanar black flecks.
                for vtx in component:vtx.co.y+=side*.004
        bmesh.ops.delete(bm,geom=remove,context='VERTS'); bm.to_mesh(ob.data); bm.free()
        skin(ob,lambda p,n=name:{n:1})
    bpy.data.objects.remove(legacy,do_unlink=True)
    # Soft shoulder web is a continuous curved surface, with body-to-wing
    # weights through its width. Its outer surface forms the underwing.
    verts=[]; faces=[]; uvs=[]
    for j in range(16):
        t=j/15; x=-.34+t*.77; z=1.15-t*.24
        for k in range(9):
            q=k/8; verts.append((x,side*(.15+q*(.16+.035*math.sin(math.pi*t))),z-.065*math.sin(math.pi*q)))
            uvs.append(photo_uv(1020+t*150,685+q*80))
    for j in range(15):
        for k in range(8):
            a=j*9+k; faces.append((a,a+1,a+10,a+9))
    web=mesh('Doorlopende ondervleugel '+label,verts,faces,uvs)
    skin(web,lambda p,n=name:{'BodyRig':max(0,1-(abs(p.y)-.17)/.12),n:min(1,max(0,(abs(p.y)-.17)/.12))})
    for i in range(12):
        a=(-.03+i*.020,side*(.30+.008*i),1.13-.012*i)
        b=(.86+i*.012+.014*math.sin(i*1.7),side*(.095+.002*i),.57+.008*i+.023*math.sin(i*1.9))
        feather('Beweegbare handpen '+label+' '+str(i),a,b,.032,(0,side,0),(0,0),(0,0),name+'Fan'+str(i),camber=.009,material=flight_palette[(i*3)%4])
    # A second layer closes the underside when viewed during the wave.
    for i in range(10):
        t=i/9
        feather('Binnenste dekveer '+label+' '+str(i),(-.24+t*.55,side*.29,1.18-t*.17),(.12+t*.68,side*.27,.89-t*.18),.050,(0,-side,0),(1090+t*100,740),(1160+t*100,830),name)

# Back-of-head texture gets a genuinely two-dimensional feather patch. Wrap
# direction is linear at the seam, without stretching a single photo row.
atlas=next(n.image for n in body.data.materials[0].node_tree.nodes if n.type=='TEX_IMAGE' and n.image.colorspace_settings.name=='sRGB')
aw,ah=atlas.size; colors=np.array(atlas.pixels[:],np.float32).reshape(ah,aw,4)
photo=next(i for i in bpy.data.images if i.name.startswith('Foto voor web'))
pw,ph=photo.size; sourcepixels=np.array(photo.pixels[:],np.float32).reshape(ph,pw,4)
v,u=np.mgrid[0:ah,0:aw]/np.array([ah-1,aw-1])[:,None,None]; theta=u*2*math.pi; z=.426+v*1.64
def smooth(a,b,val):
    t=np.clip((val-a)/(b-a),0,1); return t*t*(3-2*t)
angle=(theta+math.pi)%(2*math.pi)-math.pi
def sample(x,y):
    xi=np.clip(x*pw/1920,0,pw-1).astype(int); yi=np.clip((1-y/1324)*ph,0,ph-1).astype(int)
    return sourcepixels[yi,xi,:3]
backneck=sample(759+angle*42,405+(1.78-z)*350)
backhead=sample(717+angle*33,320+(z-1.83)*200)
patch=backneck*(1-smooth(1.75,1.87,z)[:,:,None])+backhead*smooth(1.75,1.87,z)[:,:,None]
weight=smooth(.68,.95,np.cos(theta))*smooth(1.33,1.52,z)*(1-smooth(1.99,2.04,z))
colors[:,:,:3]=colors[:,:,:3]*(1-weight[:,:,None])+patch*weight[:,:,None]
# The body under a raised wing is feathered flank, never a second printed wing.
rings=np.array([tuple(vertex.co) for vertex in body.data.vertices],np.float32).reshape(150,96,3)
cx=(rings[:,:,0].max(axis=1)+rings[:,:,0].min(axis=1))/2; rx=(rings[:,:,0].max(axis=1)-rings[:,:,0].min(axis=1))/2
xworld=np.interp(z,rings[:,0,2],cx)+np.interp(z,rings[:,0,2],rx)*np.cos(theta)
flank=sample(785+np.clip(xworld+.25,0,1)*100,670+np.clip(1.35-z,0,1)*270)
weight=smooth(-.3,-.04,xworld)*smooth(.48,.62,z)*(1-smooth(1.20,1.39,z))*smooth(.05,.35,np.abs(np.sin(theta)))
colors[:,:,:3]=colors[:,:,:3]*(1-weight[:,:,None])+flank*weight[:,:,None]
newatlas=bpy.data.images.new('V3 doorlopend achterhoofd',width=aw,height=ah); newatlas.pixels=colors.ravel(); newatlas.filepath_raw=os.path.join(OUT,'verenkleed-v3.jpg'); newatlas.file_format='JPEG'; newatlas.save(quality=92); newatlas.pack()
for n in body.data.materials[0].node_tree.nodes:
    if n.type=='TEX_IMAGE' and n.image==atlas:n.image=newatlas

# Author finite clips, exactly at rest on both boundary keys. Peck is a clear
# downward head/neck movement with feet fixed; WingWave is a bounded half-open
# lift, not flight or a full span that would dwarf the chat presentation.
scene.render.fps=30; scene.frame_start=1; scene.frame_end=97
rig.animation_data_clear()
def rotation_world(bone,axis,angle):
    rest=bone.bone.matrix_local.to_quaternion()
    bone.rotation_mode='QUATERNION'; bone.rotation_quaternion=rest.inverted() @ Quaternion(axis,angle) @ rest
def reset():
    for b in rig.pose.bones: b.rotation_mode='QUATERNION'; b.rotation_quaternion=(1,0,0,0); b.location=(0,0,0); b.scale=(1,1,1)
def key(frame):
    for b in rig.pose.bones:b.keyframe_insert('rotation_quaternion',frame=frame,group=b.name)
clips={}
for clip in ['Peck','WingWave']:
    reset(); action=bpy.data.actions.new(clip); rig.animation_data_create(); rig.animation_data.action=action
    for frame,strength in ([(1,0),(15,.12),(31,1),(40,.88),(53,.20),(72,0),(97,0)] if clip=='Peck' else [(1,0),(16,.13),(35,1),(47,.75),(59,.9),(78,.06),(97,0)]):
        reset()
        if clip=='Peck':
            rotation_world(rig.pose.bones['NeckRig'],(0,1,0),-.67*strength)
            rotation_world(rig.pose.bones['HeadRig'],(0,1,0),-.32*strength)
        else:
            for side,label in [(-1,'Left'),(1,'Right')]:
                rotation_world(rig.pose.bones['Wing'+label],(1,0,0),side*.78*strength)
                for i in range(12):rotation_world(rig.pose.bones['Wing'+label+'Fan'+str(i)],(0,0,1),side*(.10+i*.037)*strength)
            rotation_world(rig.pose.bones['NeckRig'],(0,1,0),.035*strength)
        key(frame)
    track=rig.animation_data.nla_tracks.new(); track.name=clip
    track.strips.new(clip,1,action); track.mute=True; clips[clip]=action
rig.animation_data.action=None; reset(); scene.frame_set(1)
root['animation_contract']='Peck: peak 1.0s; WingWave: peaks 1.1333s and 1.9333s; duration 3.2s; bind pose at 0 and 3.2s'
root['method']='Own skinned model, photographed plumage, connected leg sleeves/rump, overlapping articulated wing feathers'
root['limitations']='Mirrored photographic source; baked source illumination; compact half-open wing study, not flight simulation'

scene.render.engine='CYCLES'; scene.cycles.samples=24; scene.render.resolution_x=1400; scene.render.resolution_y=1100
scene.render.resolution_percentage=100; scene.render.film_transparent=True; scene.render.image_settings.file_format='PNG'
camera=scene.camera; camera.data.type='ORTHO'; camera.data.ortho_scale=3.05
def aim(position):
    camera.location=position; camera.rotation_euler=(Vector((.10,0,1.0))-camera.location).to_track_quat('-Z','Y').to_euler()
aim((-.65,-6,1.65))
text=bpy.data.texts.new('build_pigeon_v3.py')
with open(__file__) as f:text.from_string(f.read())
bpy.ops.object.select_all(action='DESELECT')
def select_tree(ob):
    ob.select_set(True)
    for c in ob.children:select_tree(c)
select_tree(root)
glb=os.path.join(OUT,'stadsduif-v3.glb')
if not MEASURE_ONLY:
    # ACTIONS exports the single armature's two compatible actions as two complete
    # clips. Keep muted NLA strips as editable source and to retain action users.
    bpy.ops.export_scene.gltf(filepath=glb,export_format='GLB',use_selection=True,export_apply=False,export_animations=True,export_animation_mode='ACTIONS',export_merge_animation='ACTION',export_extras=True,export_frame_range=False,export_anim_slide_to_zero=True)
    bpy.ops.wm.save_as_mainfile(filepath=os.path.join(OUT,'stadsduif-v3.blend'))
    for label,position in [('rust',(-.65,-6,1.65)),('voor',(-5,-1.4,1.6)),('achter',(4,-3,1.8))]:
        aim(position); scene.render.filepath=os.path.join(OUT,f'iteratie-{ITERATION}-{label}.png'); bpy.ops.render.render(write_still=True)
    aim((-.65,-6,1.65))
    for clip,frame in [('Peck',31),('WingWave',35)]:
        rig.animation_data.action=clips[clip]; scene.frame_set(frame)
        scene.render.filepath=os.path.join(OUT,f'iteratie-{ITERATION}-{clip}.png'); bpy.ops.render.render(write_still=True)
    rig.animation_data.action=None; reset(); scene.frame_set(1)
    scene.render.filepath=os.path.join(OUT,'stadsduif-v3-rust.png'); bpy.ops.render.render(write_still=True)
    root.scale.x=-1; camera.location=(.65,-6,1.65)
    camera.rotation_euler=(Vector((-.10,0,1.0))-camera.location).to_track_quat('-Z','Y').to_euler()
    scene.render.filepath=os.path.join(OUT,'stadsduif-v3-rust-rechts.png'); bpy.ops.render.render(write_still=True)
    root.scale.x=1; aim((-.65,-6,1.65))
    # Kopdetails voor de anatomische beoordeling: washuid en snavel van opzij, van
    # voren, driekwart en van boven. Deze vier krijgen een egale achtergrond, omdat
    # een lichte washuid op een transparante laag niet te beoordelen is.
    scene.render.film_transparent=False
    if scene.world.node_tree is None: scene.world.use_nodes=True
    background=scene.world.node_tree.nodes['Background']; previous=tuple(background.inputs[0].default_value)
    background.inputs[0].default_value=(.62,.64,.66,1)
    resolution=(scene.render.resolution_x,scene.render.resolution_y); ortho=camera.data.ortho_scale
    scene.render.resolution_x=900; scene.render.resolution_y=900
    for label,position,target,scale in [('kop-zij',(-.93,-3,1.86),(-.93,0,1.86),.62),('kop-voor',(-3.4,0,1.83),(-.93,0,1.83),.60),('kop-driekwart',(-2.6,-1.8,2.1),(-.93,0,1.86),.72),('kop-boven',(-1.3,0,3.4),(-.95,0,1.85),.62)]:
        camera.data.ortho_scale=scale; camera.location=position
        camera.rotation_euler=(Vector(target)-Vector(position)).to_track_quat('-Z','Y').to_euler()
        scene.render.filepath=os.path.join(OUT,f'iteratie-{ITERATION}-{label}.png'); bpy.ops.render.render(write_still=True)
    scene.render.resolution_x,scene.render.resolution_y=resolution; camera.data.ortho_scale=ortho
    background.inputs[0].default_value=previous; scene.render.film_transparent=True; aim((-.65,-6,1.65))

# Snavel- en washuidmaten horen bij het bewijs: de eis is de breedte
# tegenover de kopbreedte, niet alleen een beeld.
#
# FEATHER_LINE is de veerlijn: daar komt de snavel onder de voorhoofdsveren
# vandaan, en daar hoort de maattabel haar getallen vandaan te halen.
FEATHER_LINE=-.943
# Een maat op vaste x is de werkelijke snede door het mesh: elke rand
# die het vlak x = X kruist levert één snijpunt, en breedte en diepte zijn de
# spreiding van die punten. De vorige meting nam de breedste vertex binnen een
# band van ±0,012 rond het vlak. Op een taps toelopende snavel is dat de
# breedte van een ring verderop en niet die op de veerlijn zelf, en op de
# washuid vond dezelfde band de dikste ring binnen de schedel — vandaar dat
# bewijs en maattabel verschillende getallen voor dezelfde maat gaven.
def cross_section(ob,x):
    data=ob.data; points=[]
    for edge in data.edges:
        a=data.vertices[edge.vertices[0]].co; b=data.vertices[edge.vertices[1]].co
        if a.x==b.x:
            if a.x==x: points.extend([a.copy(),b.copy()])
            continue
        if (a.x-x)*(b.x-x)>0: continue
        points.append(a.lerp(b,(x-a.x)/(b.x-a.x)))
    return points
def cross_extent(ob,x,axis):
    points=[getattr(p,axis) for p in cross_section(ob,x)]
    return max(points)-min(points) if points else 0.
def cross_width(ob,x): return cross_extent(ob,x,'y')
def cross_depth(ob,x): return cross_extent(ob,x,'z')
beak_mesh=bpy.data.objects['Snavel hoornschede']; cere_mesh=bpy.data.objects['Washuid boven de snavelbasis']
anatomy={'head_max_width':2*max(abs(v.co.y) for v in body.data.vertices if v.co.z>1.70),
    'beak_max_width':2*max(abs(v.co.y) for v in beak_mesh.data.vertices),
    'feather_line_x':FEATHER_LINE,
    'beak_width_at_feather_line':cross_width(beak_mesh,FEATHER_LINE),
    'beak_depth_at_feather_line':cross_depth(beak_mesh,FEATHER_LINE),
    'beak_width_by_x':{f'{x:.3f}':cross_width(beak_mesh,x) for x in (FEATHER_LINE,-.960,-1.020)},
    'cere_width_at_feather_line':cross_width(cere_mesh,FEATHER_LINE),
    'cere_max_width':2*max(abs(v.co.y) for v in cere_mesh.data.vertices),
    'beak_tip_x':min(v.co.x for v in beak_mesh.data.vertices),'cere_front_x':min(v.co.x for v in cere_mesh.data.vertices)}
anatomy['beak_over_head']=anatomy['beak_max_width']/anatomy['head_max_width']
# De zichtbare washuid is de band op de veerlijn, niet de dikste ring die
# binnen de schedel wegvalt; die laatste staat als cere_max_width ernaast.
anatomy['cere_over_head']=anatomy['cere_width_at_feather_line']/anatomy['head_max_width']
with open(glb,'rb') as f:
    f.read(12); length,kind=struct.unpack('<II',f.read(8)); data=json.loads(f.read(length))
animations={a['name']:len(a['channels']) for a in data.get('animations',[])}
assert set(animations)=={'Peck','WingWave'},animations
stats={'bytes':os.path.getsize(glb),'sha256':hashlib.sha256(open(glb,'rb').read()).hexdigest(),'meshes':len(data['meshes']),'triangles':sum(data['accessors'][p['indices']]['count']//3 for m in data['meshes'] for p in m['primitives']),'animations':animations,'keyposes_seconds':{'Peck':[0,1,3.2],'WingWave':[0,1.133333,1.933333,3.2]},'source':SOURCE,'iteration':ITERATION,'license':'CC BY-SA 3.0','head_anatomy':anatomy}
with open(os.path.join(OUT,'model-evidence.json'),'w') as f:json.dump(stats,f,indent=2)
print('MODEL_V3_EXPORTED',json.dumps(stats))
