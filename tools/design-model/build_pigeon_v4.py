"""Basismodel v4: natuurgetrouwe snavel, gelede hals en een productieklaar vooraanzicht.

Bouwt, net als v3, reproduceerbaar bovenop de bevroren eigen bron
model-v2/stadsduif-v2.blend. v3 blijft ongewijzigd reproduceerbaar met
build_pigeon_v3.py; dit script neemt de v3-stappen voor poten, stuit,
vleugels en achterhoofd over en vervangt daarna:

- de snavel: een korte, donkere hoornsnavel met aparte boven- en
  ondersnavel, mondspleet, licht gebogen bovensnavelpunt en een kleine,
  poederige, tweelobbige washuid met de neusgaten aan de onderrand;
- de hals: vier halsbotten (Neck1-Neck4) onder de kop, gewichten over
  schuine doorsnedevlakken langs de halsas, een bewegende ondersnavel
  (JawRig), een staartbot (TailRig) en twee vaste pootankers (LegLeft,
  LegRight), zodat de romp kan meekantelen terwijl de voeten stilstaan;
- de clips Peck en WingWave, per frame uit gemeten poses berekend;
- het vooraanzicht: bredere voorhals en krop (alleen in y, het zijprofiel
  blijft gelijk), een ware-schaal verentextuur op gezicht, keel en borst in
  plaats van de uitgerekte strook, weggestopte vleugelvoorranden en een echte
  staartwaaier met eindband in plaats van twaalf dunne pennen.

Fotobron en afgeleide texturen: Luc Viatour, CC BY-SA 3.0. Snavel, washuid en
staartpennen gebruiken eigen procedurele texturen. Alle binaries blijven
buiten Git. Draai met Blender 5: --background --python dit_bestand.
"""
import bpy, bmesh, math, os, sys, json, struct, hashlib
import numpy as np
from mathutils import Vector, Quaternion
from mathutils.bvhtree import BVHTree

OUT=os.environ.get('DUIVEN_MODEL_OUTPUT') or sys.exit('zet DUIVEN_MODEL_OUTPUT (uitvoermap voor model v4)')
SOURCE=os.environ.get('DUIVEN_MODEL_SOURCE') or sys.exit('zet DUIVEN_MODEL_SOURCE (het .blend-bestand van model v2)')
ITERATION=os.environ.get('DUIVEN_MODEL_ITERATION','17')
# Meetronde zoals in v3: dezelfde geometrie, maar geen export en geen beelden.
MEASURE_ONLY=os.environ.get('DUIVEN_MODEL_MEASURE_ONLY')=='1'
# Snelle tussenronde voor het eigen itereren: minder samples en kleinere
# framestroken. De definitieve iteratie draait zonder deze vlag.
QUICK=os.environ.get('DUIVEN_MODEL_QUICK')=='1'
NAME='stadsduif-v4-basis'
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

def smoothstep(a,b,x):
    t=min(1.,max(0.,(x-a)/(b-a))); return t*t*(3-2*t)

# Kleurslag: de wildkleurige 'blauwe gebande' stadsduif (blue bar), de meest
# voorkomende. De bronfoto is een lichte, crèmekleurige duif met bruine banden;
# die kleur hoort niet bij een leigrijze kop. Op helderheid omgekleurd: lichte
# dekveren worden lichtgrijs met een koele blauwe zweem, de borst middengrijs,
# alles wat donker is (vleugelbanden, handpennen) bijna zwart. Verzadigde pixels
# (groene halsglans, oranje iris) blijven zoals ze zijn. Dezelfde bewerking op
# de foto en op de doorlopende atlas, zodat alles uit één kleurslag komt.
def blue_bar(rgb):
    lum=rgb@np.array([.299,.587,.114]); chroma=rgb.max(axis=-1)-rgb.min(axis=-1)
    dark=np.clip((lum-.20)/.14,0,1); dark=dark*dark*(3-2*dark)
    value=(.06+.20*lum)*(1-dark)+(.10+.68*lum)*dark
    grey=value[...,None]*np.array([.95,.98,1.05])
    keep=np.clip((chroma-.10)/.12,0,1)[...,None]
    return np.clip(grey*(1-keep)+rgb*keep,0,1)
for name in ('Foto voor web','doorlopend-verenkleed'):
    image=next(i for i in bpy.data.images if i.name.startswith(name))
    pixels=np.array(image.pixels[:],np.float32).reshape(image.size[1],image.size[0],4)
    pixels[...,:3]=blue_bar(pixels[...,:3])
    image.pixels=pixels.ravel(); image.pack()

# Voorhals en krop breder, alleen in y. Van voren was de v3-hals een smalle
# zuil tussen kop en romp (kegelpop): bij een echte stadsduif loopt de hals
# vanaf de kop meteen breder uit in de krop. Het zijprofiel (x, z) blijft exact
# gelijk, dus de geregistreerde fotoprojectie en de zijaanblik veranderen niet.
# Achter x = -0,2 (schouders, vleugelaanzet) blijft de romp ongemoeid.
NECK_WIDEN=.26
def neck_widening(p):
    bump=smoothstep(1.10,1.44,p.z)*(1-smoothstep(1.60,1.84,p.z))
    return 1+NECK_WIDEN*bump*smoothstep(-.16,-.42,p.x)
for v in body.data.vertices: v.co.y*=neck_widening(v.co)
# Ook de voorborst iets voller in y: van voren was het lijf een smalle
# fles van 2,3 keer de kopbreedte; bij een stadsduif is dat 2,6 tot 3 keer. Alleen
# het voorste deel van de romp (tot 40 %), zodat vleugels, flanken en rug gelijk
# blijven. Naar de krop loopt het geleidelijk af, anders krijgt de duif schouders.
BREAST_WIDEN=.40
for v in body.data.vertices:
    v.co.y*=1+BREAST_WIDEN*smoothstep(.50,.80,v.co.z)*(1-smoothstep(1.15,1.50,v.co.z))*smoothstep(-.05,-.40,v.co.x)

# Ogen. In v2/v3 lag het hoornvlies gelijk met of net binnen de kophuid en
# keek het zuiver zijwaarts: recht van voren zag je alleen twee rode randjes.
# Een duivenoog puilt zijdelings uit (het is het breedste punt van de kop) en
# is iets naar voren gericht. Het oog schuift daarom naar buiten en draait
# naar voren; de huid eromheen bolt een fractie mee, en een smalle vlezige
# oogring (verderop) dekt de overgang.
EYE_OUT=.014; EYE_TURN=math.radians(16); EYE_SKIN=.008
EYES={}
for side in (-1,1):
    eye=bpy.data.objects['Oog '+str(side)]
    centre=sum((v.co for v in eye.data.vertices),Vector())/len(eye.data.vertices)
    turn=Quaternion((0,0,1),side*EYE_TURN)
    for v in eye.data.vertices: v.co=centre+turn@(v.co-centre)+Vector((0,side*EYE_OUT,0))
    eye.data.update()
    EYES[side]=(centre+Vector((0,side*EYE_OUT,0)),turn@Vector((0,side,0)))
    for v in body.data.vertices:
        if v.co.y*side<.07: continue
        d=math.hypot(v.co.x-centre.x,v.co.z-centre.z)
        v.co.y+=side*EYE_SKIN*(1-smoothstep(.05,.12,d))
# De bovenste ringen op de kruin (en de onderste bij de cloaca) liepen in een
# kartelrand: van boven een klein spiraalvormig vlekje. Die ringen worden echte
# ellipsen rond hun eigen middelpunt, met hun eigen hoek per punt.
for ring in (146,147,148,149,0,1,2):
    verts=body.data.vertices[ring*96:(ring+1)*96]
    cxr=sum(v.co.x for v in verts)/96; cyr=sum(v.co.y for v in verts)/96
    rxr=max(abs(v.co.x-cxr) for v in verts); ryr=max(abs(v.co.y-cyr) for v in verts)
    for i,v in enumerate(verts):
        angle=2*math.pi*i/96; v.co.x=cxr+rxr*math.cos(angle); v.co.y=cyr+ryr*math.sin(angle)
body.data.update()

# Voorhoofd en kruin gladstrijken: de v2-huid had daar een verticale vouw op de
# middenlijn, die in driekwart-voor als harde ribbel over het voorhoofd liep.
# Een paar rondes laplaciaans gladmaken, alleen op het gezicht boven de snavel.
bm=bmesh.new(); bm.from_mesh(body.data)
face_verts=[v for v in bm.verts if v.co.z>1.80 and v.co.x<-.60 and abs(v.co.y)<.13]
for _ in range(6): bmesh.ops.smooth_vert(bm,verts=face_verts,factor=.5,use_axis_x=True,use_axis_y=True,use_axis_z=True)
bm.to_mesh(body.data); bm.free(); body.data.update()

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
    for g in list(ob.vertex_groups): ob.vertex_groups.remove(g)
    for v in ob.data.vertices:
        for bone,w in weight(v.co).items():
            if w>0:
                group=ob.vertex_groups.get(bone) or ob.vertex_groups.new(name=bone)
                group.add([v.index],float(w),'REPLACE')
    if not any(m.type=='ARMATURE' for m in ob.modifiers):
        mod=ob.modifiers.new('Anatomische vervorming','ARMATURE'); mod.object=rig

def photo_uv(x,y): return (x/1920,1-y/1324)

def feather(name,start,end,width,normal,uv_start,uv_end,bone,camber=.003,material=None,flip=False,normal_end=None):
    # normal_end draait de veer langs haar lengte (van normal aan de basis naar
    # normal_end aan de punt), zoals handpennen die over de stuit plat gaan liggen.
    a=Vector(start); delta=Vector(end)-a; n0=Vector(normal).normalized(); n1=Vector(normal_end or normal).normalized()
    vertices=[]; faces=[]; uvs=[]
    times=list(np.linspace(0,.9,15))+[.94,.965,.982,.994,1]
    cap_start=1-min(.075,width/delta.length)
    for j,t in enumerate(times):
        normal=n0.lerp(n1,t*t*(3-2*t)).normalized(); cross=delta.normalized().cross(normal).normalized()
        for k in range(5):
            s=k/2-1
            w=width*(.76+.24*math.sin(math.pi*t))*min(1,t/.08)
            if t>cap_start:w*=math.sqrt(max(0,1-((t-cap_start)/(1-cap_start))**2))
            vertices.append(a+delta*t+cross*(s*w+.013*math.sin(math.pi*t))+normal*camber*(1-s*s)*math.sin(math.pi*t))
            uvs.append((((-s if flip else s)+1)/2,t) if material else photo_uv(uv_start[0]*(1-t)+uv_end[0]*t+s*5,uv_start[1]*(1-t)+uv_end[1]*t+s*2))
    for j in range(len(times)-1):
        for k in range(4):
            i=j*5+k; faces.append((i,i+1,i+6,i+5))
    ob=mesh(name,vertices,faces,uvs,material or photo_material); skin(ob,bone if callable(bone) else (lambda p:{bone:1})); return ob

def sleeve(name,sections,weight):
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
    ob=mesh(name,vertices,faces,uvs); skin(ob,weight); return ob

# Bevederde dij tussen buik en tarsus. De bovenste ringen volgen de romp, de
# onderste het vaste pootanker: als de romp bij het pikken voorover kantelt,
# buigt de dij mee in plaats van los te komen van de rode schacht.
LEG_X={-1:.19-.025,1:.19+.055+.12}
def thigh_weight(side):
    def weight(p):
        leg=1-smoothstep(.43,.60,p.z); return {'BodyRig':1-leg,'Leg'+('Left' if side<0 else 'Right'):leg}
    return weight
for side in [-1,1]:
    y=side*.16*1.12; x=LEG_X[side]
    sleeve('Aansluitende bevederde poot '+str(side),[
        (x-.015,y,.64,.025,.025),(x-.014,y,.58,.070,.064),
        (x-.008,y,.51,.075,.066),(x-.014,y,.455,.058,.051),
        (x-.035,y,.40,.037,.034),(x-.044,y,.355,.025,.024),
        (x-.045,y,.347,.020,.020)],thigh_weight(side))

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

# Tarsusschubben: in de v2-bron lopen regelmatige ringen rondom de poot (van
# achteren een ribbelslang). Bij een duif zitten grote schilden aan de
# voorkant en een fijne korrelhuid achter: schubben op de achterkant zakken
# net onder de pootshuid, zodat alleen de voorste schilden blijven.
scales=bpy.data.objects['PigeonRoot Poot schub']; leg_skin=bpy.data.objects['PigeonRoot Poot huid']
leg_bvh=BVHTree.FromObject(leg_skin,bpy.context.evaluated_depsgraph_get())
for v in scales.data.vertices:
    if v.co.z<.06: continue
    location,normal,index,distance=leg_bvh.find_nearest(v.co)
    if location is None or distance>.03: continue
    back=smoothstep(-.15,.35,normal.x)
    if back>0: v.co=v.co.lerp(location-normal*.0015,back)
scales.data.update()

# A tapered rump joins body and tail root in real geometry. Tail starts deeply
# inside this loft; upper coverts overlap it, so there is no flat detached fan.
# De staart ligt, zoals bij een duif in rust, bijna in het verlengde van de
# rug en hangt niet schuin omlaag: van achteren is hij dan een smalle waaier
# onder de stuit in plaats van een grijs blok tussen de poten. Stuit en
# bovenstaartdekveren liggen daarom ook hoger.
sleeve('Stuit en staartwortel',[(.52,0,.72,.10,.10),(.61,0,.66,.20,.14),(.72,0,.61,.235,.145),(.84,0,.56,.19,.12),(.92,0,.525,.125,.095),(.98,0,.50,.025,.065)],lambda p:{'BodyRig':1})
for i in range(11):
    y=(i-5)*.022
    # Bovenstaartdekveren in het lichtgrijs van de gevouwen vleugel; het
    # eerdere fotostuk had witte randen die van achteren als losse vlokjes lazen.
    feather('Overlappende staartdekveer '+str(i),(.57,y,.65),(1.02-abs(y)*.6,y*1.2,.505),.035,(.3,0,1),(1010,640),(1090,700),'BodyRig')

# De v2-staart was twaalf smalle, losse pennen: van onder een kam, van voren en
# achter witte haartjes onder de buik. Een stadsduif heeft een gesloten
# staartwaaier van twaalf brede, overlappende pennen, leigrijs met een brede
# zwarte eindband; de buitenste pen heeft een witte buitenvlag. De nieuwe pennen
# hangen aan een echt staartbot, zodat de staart met de romp meekantelt.
bpy.data.objects.remove(bpy.data.objects['Dunne donkere staartveren'],do_unlink=True)
bpy.data.objects.remove(bpy.data.objects['TailRig'],do_unlink=True)
def feather_image(name,rgb):
    image=bpy.data.images.new(name,width=rgb.shape[1],height=rgb.shape[0])
    image.pixels=np.concatenate((rgb,np.ones(rgb.shape[:2]+(1,))),axis=2).astype(np.float32).ravel()
    image.filepath_raw=os.path.join(OUT,name.lower().replace(' ','-')+'.jpg'); image.file_format='JPEG'; image.save(quality=92); image.pack()
    return image
def image_material(name,image,roughness):
    mat=bpy.data.materials.new(name); mat.use_nodes=True; mat.use_backface_culling=False
    shader=mat.node_tree.nodes['Principled BSDF']; shader.inputs['Roughness'].default_value=roughness
    tex=mat.node_tree.nodes.new('ShaderNodeTexImage'); tex.image=image
    mat.node_tree.links.new(tex.outputs['Color'],shader.inputs['Base Color'])
    return mat
ty,tx=np.mgrid[0:512,0:128]/np.array([511,127])[:,None,None]
ts=tx*2-1
tgrain=.975+.03*np.sin(ty*700+np.abs(ts)*38)+.015*np.sin(ty*1650+ts*9)
band=np.clip((ty-.70)/.03,0,1)*np.clip((.955-ty)/.02,0,1)
tshade=(tgrain-.07*np.exp(-ts*ts*1400)-.05*np.abs(ts)**6)[:,:,None]
slate=np.array([.30,.315,.345]); black=np.array([.018,.018,.021])
tailrgb=tshade*(slate*(1-band[:,:,None])+black*band[:,:,None])
tailrgb*=np.where(ty>.955,.55,1.)[:,:,None]
tail_mat=image_material('Staartpen leigrijs met eindband',feather_image('Staartpen eindband',tailrgb),.78)
outer=np.clip((-ts-.08)/.12,0,1)*(1-np.clip((ty-.66)/.05,0,1))
outerrgb=tailrgb*(1-outer[:,:,None])+tshade*np.array([.58,.59,.60])*outer[:,:,None]
outer_mat=image_material('Staartpen buitenste witte vlag',feather_image('Staartpen buitenvlag',outerrgb),.78)
TAIL_BASE=Vector((.60,0,.575)); TAIL_TIP=Vector((1.25,0,.43))
tail_axis=(TAIL_TIP-TAIL_BASE).normalized(); tail_up=Vector((-tail_axis.z,0,tail_axis.x))
for i in range(12):
    c=i-5.5; layer=abs(c)-.5
    # Een gesloten duivenstaart: smal, de pennen liggen dicht over elkaar met
    # de middelste bovenop, het eind is rond (buitenste pennen korter) en licht
    # bol. Kleine hoogteverschillen, zodat hij van achteren geen trap van
    # kaarten is.
    start=TAIL_BASE+Vector((0,c*.012,0))-tail_up*.0016*layer
    end=TAIL_TIP+Vector((-.050*(c/5.5)**2,c*.025,0))-tail_up*(.0016*layer+.0006*layer*layer)
    feather('Staartpen '+str(i),start,end,.040,tail_up,(0,0),(0,0),'TailRig',camber=.006,
        material=outer_mat if i in (0,11) else tail_mat,flip=(i==11))

# ---------------------------------------------------------------------------
# Snavel. De v2-bron levert een platte wig van elf punten; v3 maakte er een
# lichte hoornschede met een grote witte band van. Een stadsduif heeft een
# korte, slanke, leigrijze tot zwartachtige snavel: een bovensnavel met een licht
# gebogen punt die net over de ondersnavel valt, een zichtbare mondspleet, en
# op de basis een kleine, poederig witte, tweelobbige washuid met de neusgaten
# aan de onderrand. De verhoudingen zijn gemeten aan referentiefoto's; hier
# alleen de maten in modeleenheden.
bpy.data.objects.remove(bpy.data.objects['Snavel en neusdop'],do_unlink=True)
body_bvh=BVHTree.FromObject(body,bpy.context.evaluated_depsgraph_get())
BEAK_PITCH=math.radians(33)                    # culmen onder de horizon
AXIS=Vector((-math.cos(BEAK_PITCH),0,-math.sin(BEAK_PITCH))); UP=Vector((-math.sin(BEAK_PITCH),0,math.cos(BEAK_PITCH)))
BEAK_ROOT=Vector((-.880,0,1.858))              # binnen de schedel, op de v3-snavelas
hit=body_bvh.ray_cast(BEAK_ROOT,AXIS)
FEATHER=hit[0]; FEATHER.y=0                    # waar de snavelas uit de voorhoofdsveren komt
BEAK_L=float(os.environ.get('DUIVEN_BEAK_LENGTH','.185'))   # blootliggende culmen, veerlijn tot punt
CERE_FRONT=.60                                 # voorrand washuid als fractie van BEAK_L
def axis_point(s,hook=0.):
    return FEATHER+AXIS*(s*BEAK_L)-UP*hook
def upper_hook(s): return .016*max(0.,(s-.66)/.34)**2
def tuck(s):
    # Achter de veerlijn (s < 0) loopt de snavel in de kop smaller toe, zodat
    # ondersnavel en mondhoek in de kin- en wangveren verdwijnen in plaats van
    # als staaf door te lopen.
    return 1-.45*smoothstep(0.,.32,-s)
def upper_dims(s):
    # Kort en stevig: bij de basis half zo hoog als lang (boven- plus
    # ondersnavel), met een recht verlopende culmen tot een duidelijk
    # gezwollen nagel en een korte, stompe haak. Geen naald: de punt houdt
    # een afgeronde doorsnede van enkele millimeters.
    # De laatste 12 procent rondt als een kwartcirkel af: een stompe, licht
    # gebogen nagel in plaats van een afgeknot eindvlak.
    r=max(0.,1-s); nail=1+.42*math.exp(-((s-.84)/.075)**2)
    cap=math.sqrt(max(0.,1-(max(0.,s-.88)/.12)**2))
    return ((.038*r**.50+.0040)*tuck(s)*(1+.12*math.exp(-((s-.84)/.08)**2))*cap, (.047*r**.75+.0042)*nail*tuck(s)*cap)
def lower_dims(s,tip=.93):
    r=max(0.,1-s/tip); cap=math.sqrt(max(0.,1-(max(0.,s-.83)/(tip-.83))**2))
    return ((.036*r**.60+.0030)*tuck(s)*cap, (.030*r**.80+.0024)*tuck(s)*cap)   # halve breedte, diepte onder de mondspleet
def frame_point(s,lat,up,hook=0.):
    return axis_point(s,hook)+Vector((0,lat,0))+UP*up

def closed_loft(name,rings,material,weight,uv):
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
    skin(ob,weight); return ob

def superellipse_arc(width,height,steps,power,base):
    # Bovenboog van rechts (+y) over de rug naar links, eindigend op hoogte `base`.
    points=[]
    for i in range(steps):
        a=math.pi*i/(steps-1); c=math.cos(a); s=math.sin(a)
        points.append((width*math.copysign(abs(c)**(2/power),c),base+(height-base)*abs(s)**(2/power)))
    return points
def normal_image(name,height,strength):
    # Eigen reliëfkaart uit een hoogteveld (glTF normalTexture, geen extensie).
    gy,gx=np.gradient(height); n=np.stack((-gx*strength,-gy*strength,np.ones_like(gx)),axis=2); n/=np.linalg.norm(n,axis=2,keepdims=True)
    image=bpy.data.images.new(name,width=height.shape[1],height=height.shape[0]); image.colorspace_settings.name='Non-Color'
    image.pixels=np.concatenate((n*.5+.5,np.ones(height.shape+(1,))),axis=2).astype(np.float32).ravel()
    image.filepath_raw=os.path.join(OUT,name.lower().replace(' ','-')+'.png'); image.file_format='PNG'; image.save(); image.pack()
    return image
def add_normal(material,image,strength):
    nodes=material.node_tree.nodes; tex=nodes.new('ShaderNodeTexImage'); tex.image=image
    nm=nodes.new('ShaderNodeNormalMap'); nm.inputs['Strength'].default_value=strength
    material.node_tree.links.new(tex.outputs['Color'],nm.inputs['Color']); material.node_tree.links.new(nm.outputs['Normal'],nodes['Principled BSDF'].inputs['Normal'])

# Eigen hoornkleur (sRGB-waarden; v = fractie langs de snavel): aan de basis donker leigrijs
# met een witte berijping onder de washuid, naar de punt lichter en warmer
# hoornkleurig. Mat, met fijne lengtestrepen en schubjes in het reliëf.
hv,hu=np.mgrid[0:256,0:64]/np.array([255,63])[:,None,None]
slate_horn=np.array([.15,.16,.18]); tip_horn=np.array([.40,.36,.30]); bloom=np.array([.45,.46,.48])
t_tip=np.clip((hv-.55)/.42,0,1); t_tip=t_tip*t_tip*(3-2*t_tip)
t_bloom=np.clip((.42-hv)/.25,0,1); t_bloom=t_bloom*t_bloom*(3-2*t_bloom)
horn=slate_horn*(1-t_tip)[:,:,None]+tip_horn*t_tip[:,:,None]
horn=horn*(1-.55*t_bloom[:,:,None])+bloom*.55*t_bloom[:,:,None]
# Alleen fijne lengtetekening (variatie rond de snavel, niet langs de lengte):
# dwarsbanden lazen als karton of schroefdraad.
horn*=(1+.035*np.sin(hu*2*math.pi*9+np.sin(hv*7)*.6)+.02*np.sin(hu*2*math.pi*23))[:,:,None]
horn_mat=image_material('Snavel hoorn leigrijs',feather_image('Snavel hoorn',np.clip(horn,0,1)),.62)
horn_relief=.5*np.sin(hu*2*math.pi*14+np.sin(hv*5)*.5)+.25*np.sin(hu*2*math.pi*31)
add_normal(horn_mat,normal_image('Snavel reliëf',horn_relief,.25),.20)
UPPER_S=[-.36+1.36*(1-(1-j/33)**1.35) for j in range(34)]
def upper_ring(s):
    # De zijwanden van de bovensnavel vallen als een deksel over de ondersnavel:
    # de mondspleet is een schaduwnaad, geen lichte kier.
    w,h=upper_dims(s); hook=upper_hook(s)
    arc=superellipse_arc(w,h,19,2.3,-.0040)
    palate=[(-.84*w,-.0068),(-.35*w,-.0078),(.35*w,-.0078),(.84*w,-.0068)]
    return [frame_point(s,lat,up,hook) for lat,up in arc+palate]
closed_loft('Snavel bovensnavel',[upper_ring(s) for s in UPPER_S],horn_mat,lambda p:{'HeadRig':1},lambda j,u:(u,max(0.,min(1.,UPPER_S[round(j*33)]))))
LOWER_S=[-.30+1.23*(1-(1-j/27)**1.35) for j in range(28)]
def lower_ring(s):
    w,h=lower_dims(s); ring=[]
    for i in range(17):
        a=math.pi*i/16; c=math.cos(a); sn=math.sin(a)
        ring.append((.93*w*math.copysign(abs(c)**(2/2.2),c),-.0052-h*abs(sn)**(2/2.2)))
    ring+= [(-.80*w,-.0046),(.80*w,-.0046)]
    return [frame_point(s,lat,up) for lat,up in ring]
closed_loft('Snavel ondersnavel',[lower_ring(s) for s in LOWER_S],horn_mat,lambda p:{'JawRig':1},lambda j,u:(u,max(0.,min(1.,LOWER_S[round(j*27)]))))
BEAK_TIP=axis_point(1.,upper_hook(1.))+UP*upper_dims(1.)[1]*.3

# Washuid: een opgezwollen, knobbelige kussenmassa op de basis van de
# bovensnavel, zoals op de referentiefoto's. Twee bolle lobben met een
# middengroef liggen duidelijk boven de culmen en reiken opzij tot ongeveer
# halverwege de bovensnavel; van voren beslaan ze samen ongeveer een derde van
# de kopbreedte. De knobbels zitten in de vorm zelf (een vaste set bulten), het
# fijne poeder in een reliëfkaart. Achter komt de washuid uit de
# voorhoofdsveren, voor eindigt ze rond en loopt ze onder de hoorn door.
CERE_T=.030
cere_rng=np.random.default_rng(1311)
# Grote, lage, ronde knobbels (geen plooien): van dichtbij leest de washuid als
# een zacht, poederig kussen en niet als verfrommeld papier.
CERE_BUMPS=[(cere_rng.uniform(.0,CERE_FRONT-.05),cere_rng.uniform(.25,math.pi-.25),cere_rng.uniform(.0022,.0040),cere_rng.uniform(.05,.09),cere_rng.uniform(.22,.38)) for _ in range(30)]
def cere_thickness(s):
    # Opgezwollen naar voren toe: de washuid is het dikst op ongeveer een
    # derde van haar lengte en eindigt voor in een ronde, bolle kop.
    # Langgerekt langs de snavel, zoals op de zijfoto: het dikst rond een
    # kwart van de snavel, naar achteren glooiend in de voorhoofdsveren en
    # voor rond afgesloten.
    q=(s+.12)/(CERE_FRONT+.12)
    rise=smoothstep(-.34,.10,s)
    # De voorrand rondt als een kwartcirkel af, zodat het kussen zacht op de
    # hoorn overgaat in plaats van met een harde rand te eindigen.
    return CERE_T*rise*math.sqrt(max(0.,1-max(0.,q)**2.2))-.0016 if q<1 else -.0030*q
CERE_S=[-.34+(CERE_FRONT+.05+.34)*(j/55) for j in range(56)]
def cere_surface(s,a):
    # Punt op de washuid bij fractie s en hoek a (0 = rechteronderrand, pi =
    # linkeronderrand), in (breedte, hoogte) boven de mondspleet.
    w,h=upper_dims(s); t=cere_thickness(s); lobe=max(0.,t)/CERE_T
    width=w+t*1.05; top=h+t*1.25; low=.60*h; c=math.cos(a); sn=math.sin(a)
    # Eén bol kussen: opzij het breedst halverwege de hoogte, onderaan
    # ingetrokken tot de snavelwand (geen flappen naast de snavel), en in het
    # midden alleen een ondiepe, onregelmatige groef.
    bulge=w*.98+(width-w*.98)*abs(sn)**.6
    lat=bulge*math.copysign(abs(c)**.85,c)
    up=low+(top-low)*abs(sn)**.70
    up-=.32*lobe*(top-h)*math.exp(-(lat/(.18*width+1e-6))**2)
    bumps=sum(amp*math.exp(-((s-bs)/ss)**2-((a-ba)/sa)**2) for bs,ba,amp,ss,sa in CERE_BUMPS)
    n=Vector((lat,up-.4*h)).normalized() if (lat or up) else Vector((0,1))
    return lat+n.x*bumps*lobe,up+n.y*bumps*lobe
def cere_ring(s):
    w,h=upper_dims(s)
    ring=[cere_surface(s,math.pi*i/56) for i in range(57)]+[(-.55*w,.30*h),(0,.22*h),(.55*w,.30*h)]
    return [frame_point(s,lat,up,upper_hook(s)) for lat,up in ring]
cv,cu=np.mgrid[0:128,0:128]/127
rng=np.random.default_rng(131)
speck=rng.random((128,128))
powder=.95+.04*np.sin(cu*90+np.sin(cv*40)*3)*np.sin(cv*70)-.08*(speck>.92)-.04*(speck<.06)
cere_rgb=np.clip(powder[:,:,None]*np.array([.70,.71,.72])[None,None,:],0,1)
cere_mat=image_material('Washuid poederig',feather_image('Washuid poeder',cere_rgb),.95)
warts=sum(np.sin(cu*f1+np.sin(cv*f2)*2+ph)*np.sin(cv*f3+ph) for f1,f2,f3,ph in [(61,23,57,.3),(97,31,89,1.7),(143,47,131,2.9)])/3+.6*(speck-.5)
add_normal(cere_mat,normal_image('Washuid reliëf',warts,.9),.75)
closed_loft('Washuid boven de snavelbasis',[cere_ring(s) for s in CERE_S],cere_mat,lambda p:{'HeadRig':1},lambda j,u:(u*3%1,j))

# Neusgaten: een smalle donkere spleet in de onderrand van de washuid, over het
# voorste deel. Ze volgen het washuidoppervlak en steken er nergens buiten.
nostril_material=bpy.data.materials.new('Neusgat'); nostril_material.use_nodes=True; nostril_material.use_backface_culling=False
nostril_shader=nostril_material.node_tree.nodes['Principled BSDF']
nostril_shader.inputs['Base Color'].default_value=(.030,.026,.025,1); nostril_shader.inputs['Roughness'].default_value=.6
for side in [-1,1]:
    rings=[]
    for lift,narrow in [(.0006,1.),(.0012,.8),(.0017,.45)]:
        ring=[]
        for i in range(12):
            u=2*math.pi*i/12; s=.36+.11*narrow*math.cos(u)
            a=(.075+.018*narrow*math.sin(u))*math.pi
            lat,up=cere_surface(s,a if side>0 else math.pi-a)
            p=frame_point(s,lat,up,upper_hook(s)); outward=Vector((0,side,0))*.6+UP*.4
            ring.append(p+outward.normalized()*lift)
        rings.append(ring)
    closed_loft('Neusgat in de washuid '+str(side),rings,nostril_material,lambda p:{'HeadRig':1},lambda j,u:(u,j))

# Vlezige oogring: een smalle torus op de rand van het oog, half in de huid.
eye_ring_material=bpy.data.materials.new('Oogring huid'); eye_ring_material.use_nodes=True; eye_ring_material.use_backface_culling=False
eye_ring_shader=eye_ring_material.node_tree.nodes['Principled BSDF']
eye_ring_shader.inputs['Base Color'].default_value=(.20,.15,.15,1); eye_ring_shader.inputs['Roughness'].default_value=.55
for side,(centre,normal) in EYES.items():
    tangent=normal.cross(Vector((0,0,1))).normalized(); bitangent=normal.cross(tangent).normalized()
    rings=[]
    for j in range(48):
        phi=2*math.pi*j/48; radial=tangent*math.cos(phi)+bitangent*math.sin(phi)
        rings.append([centre+radial*(.0462+.0040*math.cos(2*math.pi*i/12))+normal*(.0040*math.sin(2*math.pi*i/12)-.0030) for i in range(12)])
    verts=[p for ring in rings for p in ring]; faces=[]
    for j in range(48):
        for i in range(12):
            a_=j*12+i; b_=j*12+(i+1)%12; c_=((j+1)%48)*12+(i+1)%12; d_=((j+1)%48)*12+i; faces.append((a_,b_,c_,d_))
    ring_ob=mesh('Oogring '+str(side),verts,faces,[(0,0)]*len(verts),eye_ring_material)
    bm=bmesh.new(); bm.from_mesh(ring_ob.data); bmesh.ops.recalc_face_normals(bm,faces=bm.faces); bm.to_mesh(ring_ob.data); bm.free()
    skin(ring_ob,lambda p:{'HeadRig':1})

# ---------------------------------------------------------------------------
# Rig. De v3-hals was één NeckRig-bot met één HeadRig: de pik knikte de hals op
# één scharnier en de kop hing eraan. Nu vier halsbotten langs de middenlijn van
# de hals, een kopbot op het achterhoofdsgewricht (achter en onder het oog), een
# ondersnavelbot op de mondhoek, een staartbot en twee vaste pootankers. BodyRig
# kantelt om het heupgebied, zodat de romp bij het pikken kan meebuigen terwijl
# de voeten stil blijven staan. Namen: Neck1 (halsbasis) t/m Neck4 (onder de kop).
# De bovenste gewrichten liggen aan de rugzijde van de halszuil, zoals de
# wervels: bij voorover buigen rekt de nek (met de iriserende veren) dan
# nauwelijks. De halsbasis (Neck1) ligt juist aan de voorkant, in de krop: lag
# ook die aan de rugzijde, dan zwaaide de voorhals bij het pikken door de borst
# (zelfdoorsnijding van de huid, zichtbaar in de varianten).
# Tarsus per poot uit de schubbengeometrie: de as door het midden van de schacht
# vlak boven de tenen en vlak onder het verenkleed.
TARSUS={}
scale_points=np.concatenate([np.array([tuple(ob.matrix_world@v.co) for v in ob.data.vertices]) for ob in (bpy.data.objects['PigeonRoot Poot schub'],bpy.data.objects['PigeonRoot Poot huid'])])
for side in (-1,1):
    own=scale_points[scale_points[:,1]*side>0]
    low=own[(own[:,2]>.10)&(own[:,2]<.16)].mean(axis=0); high=own[(own[:,2]>.41)&(own[:,2]<.45)].mean(axis=0)
    direction=Vector(tuple(high-low)).normalized()
    base=Vector(tuple(low))-direction*((low[2]-.058)/direction.z)
    TARSUS[side]=(base,Vector(tuple(high)))
NECK_JOINTS=[(-.72,1.14),(-.56,1.41),(-.60,1.58),(-.62,1.71),(-.63,1.83)]
HIP=(.16,.74)
GAPE=frame_point(-.06,0,-.0045)
bpy.ops.object.select_all(action='DESELECT'); rig.select_set(True); bpy.context.view_layer.objects.active=rig
bpy.ops.object.mode_set(mode='EDIT')
bones=rig.data.edit_bones
body_bone=bones['BodyRig']; body_bone.head=(HIP[0],0,HIP[1]); body_bone.tail=(HIP[0],0,HIP[1]+.52)
parent=body_bone
for k in range(4):
    bone=bones['NeckRig'] if k==0 else bones.new(f'Neck{k+1}')
    if k==0: bone.name='Neck1'
    bone.head=(NECK_JOINTS[k][0],0,NECK_JOINTS[k][1]); bone.tail=(NECK_JOINTS[k+1][0],0,NECK_JOINTS[k+1][1]); bone.parent=parent; parent=bone
head_bone=bones['HeadRig']; head_bone.head=(NECK_JOINTS[4][0],0,NECK_JOINTS[4][1]); head_bone.tail=(-.74,0,2.03); head_bone.parent=parent
jaw=bones.new('JawRig'); jaw.head=GAPE; jaw.tail=GAPE+AXIS*.12; jaw.parent=head_bone
tail_bone=bones.new('TailRig'); tail_bone.head=TAIL_BASE; tail_bone.tail=TAIL_BASE+tail_axis*.30; tail_bone.parent=body_bone
# Poten: een tarsusbot per poot (LegLeft/LegRight) dat op de teenbasis draait,
# en een vast voetbot (FootLeft/FootRight) voor tenen en nagels. Bij het pikken
# zakt de duif door het hielgewricht: de tarsus kantelt met de bovenkant naar
# achteren en omlaag, de tenen blijven exact staan.
for side,label in [(-1,'Left'),(1,'Right')]:
    leg=bones.new('Leg'+label); leg.head=TARSUS[side][0]; leg.tail=TARSUS[side][1]; leg.parent=None
    foot=bones.new('Foot'+label); foot.head=TARSUS[side][0]; foot.tail=TARSUS[side][0]+Vector((-.10,0,0)); foot.parent=None
for side,label in [(-1,'Left'),(1,'Right')]:
    name='Wing'+label; old=bpy.data.objects[name]; old.name=name+'Legacy'
    # Schouderscharnier hoog op de rug, zodat de vleugel bij het optillen uit
    # de schouder komt en niet als een arm uit de borst.
    bone=bones.new(name); bone.head=(-.20,side*.20,1.33); bone.tail=bone.head+Vector((0,0,.2)); bone.parent=body_bone
    for i in range(12):
        fan=bones.new(name+'Fan'+str(i)); fan.head=(-.03+i*.020,side*(.30+.008*i),1.13-.012*i)
        fan.tail=fan.head+Vector((0,0,.12)); fan.parent=bone
bpy.ops.object.mode_set(mode='OBJECT')

# Tarsus en tenen: boven de teenbasis volgt de huid het tarsusbot, eronder het
# vaste voetbot. De nagels blijven losse, onbewogen meshes.
for name in ('PigeonRoot Poot huid','PigeonRoot Poot schub'):
    skin(bpy.data.objects[name],lambda p:(lambda t,label:{'Leg'+label:t,'Foot'+label:1-t})(smoothstep(.062,.10,p.z),'Left' if p.y<0 else 'Right'))

# Halsgewichten over doorsnedevlakken. Onderaan liggen de vlakken horizontaal,
# zoals de hals werkelijk uit de romp komt; naar de kop kantelen ze mee met de
# halsas. Tussen twee vlakken loopt de huid vloeiend (smoothstep) over van het
# ene bot naar het volgende, zodat geen enkele ring op één scharnier knikt.
# Rug en schouders achter de halszuil blijven bij de romp, anders zou de huid
# bij het buigen door de vleugelaanzet steken.
# De huid van de hals begint vooraan op z = 1,28, iets boven het draaipunt van
# Neck1 in de krop: zo draagt de romp de borst tot aan de halsvlek (daar zit
# bij de Stakeholder de stropdasknoop) en buigt de hals er vloeiend uit.
PLANES=[((-.45,1.15),(0,1)),((-.62,1.28),(0,1))]+[(NECK_JOINTS[k],(-.07*k,1)) for k in range(1,5)]+[((-.70,1.875),(-.30,1))]
PLANES=[(Vector((p[0],p[1])),Vector(n).normalized()) for p,n in PLANES]
CENTERS=[('BodyRig',.5),('Neck1',1.5),('Neck2',2.5),('Neck3',3.5),('Neck4',4.5),('HeadRig',5.5)]
def neck_coordinate(p):
    q=Vector((p.x,p.z)); d=[(q-o).dot(n) for o,n in PLANES]
    if d[0]<0: return 0.
    k=max(i for i in range(len(d)) if d[i]>=0)
    if k==len(d)-1: return float(k)
    return k+d[k]/(d[k]-d[k+1])
def neck_weights(p):
    # Brede, overlappende invloeden (gaussisch in de halscoördinaat, hooguit
    # vier botten per punt, de glTF-grens): een buiging verdeelt zich over een lang stuk huid
    # in plaats van over één smalle band. Onder de halsbasis alleen romp, vanaf
    # de ogen alleen kop, zodat oog en snavel exact met de schedel meegaan.
    s=neck_coordinate(p)
    raw={name:math.exp(-((s-centre)/.90)**2) for name,centre in CENTERS}
    top=sorted(raw.items(),key=lambda kv:-kv[1])[:4]; total=sum(w for _,w in top)
    weights={name:w/total for name,w in top}
    # De kop neemt de bovenste helft van de hals geleidelijk mee: de kopbuiging
    # bij het pikken spreidt zich over de hele bovenhals, zodat de nek niet
    # achter het achterhoofd vouwt (geen holle knik, geen zelfdoorsnijding).
    for name,t in (('BodyRig',1-smoothstep(.30,.90,s)),('HeadRig',smoothstep(3.6,6.0,s))):
        if t>0:
            weights={k:v*(1-t) for k,v in weights.items()}; weights[name]=weights.get(name,0)+t
    # Achter de halszuil (mantel, schouders) terug naar de romp.
    column=np.interp(p.z,[j[1] for j in NECK_JOINTS],[j[0] for j in NECK_JOINTS])
    behind=smoothstep(.10,.80,p.x-column)*(1-smoothstep(3.0,4.2,s))
    if behind>0:
        weights={k:v*(1-behind) for k,v in weights.items()}; weights['BodyRig']=weights.get('BodyRig',0)+behind
    return weights
skin(body,neck_weights)
for name in ['Oog -1','Oog 1']: skin(bpy.data.objects[name],lambda p:{'HeadRig':1})

# A narrow source-photo projection makes repeated black/white blades. Author a
# single continuous neutral flight-feather microtexture instead: asymmetrical
# vanes, subtle barbs and rachis. The photographed outer coverts stay intact.
# Handpennen: brede, sterk overlappende pennen met een smalle donkere
# buitenvlag, een lichtere leigrijze binnenvlag, een schacht en een donkere
# punt, met schuine baarden. Eigen procedureel beeld (u over de breedte, v
# langs de pen van basis naar punt). Vier tinten voor natuurlijke variatie.
fy,fx=np.mgrid[0:512,0:128]/np.array([511,127])[:,None,None]
SHAFT_U=.36
outer_vane=np.array([.095,.100,.110]); inner_vane=np.array([.22,.23,.255]); tip_dark=np.array([.050,.052,.058])
inner=np.clip((fx-SHAFT_U)/.05,0,1)
vane=outer_vane*(1-inner)[:,:,None]+inner_vane*inner[:,:,None]
tip_share=np.clip((fy-.58)/.25,0,1); tip_share=tip_share*tip_share*(3-2*tip_share)
tip_share=np.maximum(tip_share,np.clip((fy-.40)/.3,0,1)*np.clip((.30-fx)/.3,0,1))
vane=vane*(1-tip_share)[:,:,None]+tip_dark*tip_share[:,:,None]
barbs=1+.07*np.sin(fy*520+np.abs(fx-SHAFT_U)*140)+.03*np.sin(fy*1400)
shaft=np.exp(-((fx-SHAFT_U)/.012)**2)*(1-.6*fy)
rgb=np.clip(vane*barbs[:,:,None]+np.array([.20,.20,.21])*shaft[:,:,None]*.5,0,1)
flight_palette=[]
for variation,factor in enumerate([.90,.96,1,1.05]):
    image=bpy.data.images.new('Handpen tintbeeld '+str(variation),width=128,height=512)
    image.pixels=np.concatenate((np.clip(rgb*factor,0,1),np.ones((512,128,1))),axis=2).astype(np.float32).ravel()
    image.filepath_raw=os.path.join(OUT,'handpen-tint-'+str(variation)+'.jpg'); image.file_format='JPEG'; image.save(quality=92); image.pack()
    mat=bpy.data.materials.new('Handpen tint '+str(variation)); mat.use_nodes=True; mat.use_backface_culling=False
    shader=mat.node_tree.nodes['Principled BSDF']; shader.inputs['Roughness'].default_value=.78
    tex=mat.node_tree.nodes.new('ShaderNodeTexImage'); tex.image=image
    mat.node_tree.links.new(tex.outputs['Color'],shader.inputs['Base Color'])
    flight_palette.append(mat)

# Onderkant van de vleugel: lichte, grijswitte ondervleugeldekveren. Een eigen
# fijn veerpatroon in plaats van de uitgerekte flankfoto, die bij het optillen
# als een streperig gordijn tussen lijf en vleugel las.
uy,ux=np.mgrid[0:256,0:256]/255
scales=(np.sin(ux*2*math.pi*6+np.sin(uy*2*math.pi*3))*.5+.5)*(np.sin(uy*2*math.pi*9)*.5+.5)
under_rgb=np.clip((.60+.10*scales-.05*np.abs(np.sin(uy*2*math.pi*9+ux*4)))[:,:,None]*np.array([.97,.99,1.03]),0,1)
underwing_mat=image_material('Ondervleugel lichtgrijs',feather_image('Ondervleugel',under_rgb),.85)

def components(bm):
    pending=set(bm.verts); result=[]
    while pending:
        stack=[pending.pop()]; component=[]
        while stack:
            vtx=stack.pop(); component.append(vtx)
            for edge in vtx.link_edges:
                neighbor=edge.other_vert(vtx)
                if neighbor in pending:pending.remove(neighbor);stack.append(neighbor)
        result.append(component)
    return result

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
        bm=bmesh.new(); bm.from_mesh(ob.data); remove=[]; sheet=None
        for component in components(bm):
            if len(component)==75 and max(v.co.x for v in component)-min(v.co.x for v in component)>.4:
                remove.extend(component)
            elif len(component)==75 and max(v.co.x for v in component)<-.20:
                # De twee voorste randdekveertjes zwaaien bij het optillen van de
                # vleugel naar voren uit en staken dan als los schijfje voor de
                # borst uit. De voorrand ligt nu onder de flankveren; ze vervallen.
                remove.extend(component)
            elif len(component)==75:
                # Separate tiny edge-coverts from the underlying sheet enough
                # to survive glTF triangulation without coplanar black flecks.
                for vtx in component:vtx.co.y+=side*.004
            elif sheet is None or len(component)>len(sheet): sheet=component
        # Midden op de v2-vleugel komen ongeveer twintig losse hoekpunten op één
        # plek samen: een klein gat (met een eigen rand, die anders als voorrand
        # onder het lijf getrokken werd) waardoor het lijf als donker vlekje te
        # zien was. Na de indeling in onderdelen (die op de oorspronkelijke
        # aantallen hoekpunten leunt) worden die punten van het hoofdvlak
        # samengevoegd.
        bmesh.ops.remove_doubles(bm,verts=[v for v in sheet if v.is_valid],dist=1e-5)
        sheet=[v for v in sheet if v.is_valid]
        # De voorrand van de gesloten vleugel draagt in de foto een donkere
        # schaduwlijn. Van opzij valt die weg tegen de flank, van voren liep
        # ze als zwarte naad over de borst. De voorrand duikt nu onder de
        # flankveren: randpunten schuiven tot net binnen het lijfoppervlak, de
        # twee ringen erachter lopen geleidelijk mee.
        edge=[v for v in sheet if v.is_boundary and v.co.x<.30]
        hops={v:0 for v in edge}; frontier=list(edge)
        for h in (1,2,3,4,5):
            nxt=[]
            for vtx in frontier:
                for e in vtx.link_edges:
                    n=e.other_vert(vtx)
                    if n not in hops: hops[n]=h; nxt.append(n)
            frontier=nxt
        pull={0:1.,1:.80,2:.56,3:.33,4:.15,5:.04}
        for vtx,h in hops.items():
            # Naar het dichtstbijzijnde punt van de huid, net eronder: zo duiken
            # ook de bovenrand over de rug en de schouderhoek onder de veren.
            location,normal,index,distance=body_bvh.find_nearest(vtx.co)
            if location is None or distance>.08: continue
            outside=(vtx.co-location).dot(normal)>0
            if outside: vtx.co=vtx.co.lerp(location-normal*.007,pull[h])
        # Waar het lijf door het vleugelvlak prikte, zat midden op de vleugel een
        # donker vlekje (het lijf erdoorheen). Het vlak ligt overal buiten de
        # voorranden minstens 6 mm boven de huid.
        tucked=set(hops)
        for vtx in sheet:
            if vtx in tucked: continue
            location,normal,index,distance=body_bvh.find_nearest(vtx.co)
            if location is None or distance>.08: continue
            height=(vtx.co-location).dot(normal)
            if height<.006: vtx.co=location+normal*.006
        # De onderste voorhoek van de gesloten vleugel stak van voren als een
        # grijs vinnetje buiten de borst uit; bij een duif valt die hoek onder
        # de flankveren. Hij zakt geleidelijk tot net onder het lijfoppervlak;
        # dat geldt ook voor de kleine randdekveertjes langs de onderrand.
        removed=set(remove)
        for vtx in [v for v in bm.verts if v not in removed]:
            w=smoothstep(.86,.70,vtx.co.z)*smoothstep(.22,.02,vtx.co.x)
            if w<=0: continue
            location,normal,index,distance=body_bvh.find_nearest(vtx.co)
            if location is not None and (vtx.co-location).dot(normal)>-.004:
                vtx.co=vtx.co.lerp(location-normal*.005,w)
        # De donkere schaduwrand van de foto zit in de buitenste twee rijen. Bij
        # het optillen komt die voorrand vrij; hij krijgt daarom de dekveertextuur
        # van drie rijen verder naar binnen.
        uv_layer=bm.loops.layers.uv.active
        def inward(vtx,h):
            while h<3 and h<5:
                nxt=[e.other_vert(vtx) for e in vtx.link_edges if hops.get(e.other_vert(vtx))==h+1]
                if not nxt: return None
                vtx=nxt[0]; h+=1
            return vtx
        for vtx,h in list(hops.items()):
            if h>1 or not vtx.link_loops: continue
            source=inward(vtx,h)
            if source is None or not source.link_loops: continue
            uv=source.link_loops[0][uv_layer].uv.copy()
            for loop in vtx.link_loops: loop[uv_layer].uv=uv
        bmesh.ops.delete(bm,geom=remove,context='VERTS')
        # Eén keer gladjes onderverdelen: het vlak was grof genoeg om bij het
        # optillen harde facetten en vouwen te tonen.
        bmesh.ops.subdivide_edges(bm,edges=bm.edges[:],cuts=1,use_grid_fill=True,smooth=0)
        bm.to_mesh(ob.data); bm.free()
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
    web=mesh('Doorlopende ondervleugel '+label,verts,faces,[(q_*6%1,t_*3) for q_,t_ in [((k%9)/8,(k//9)/15) for k in range(len(verts))]],underwing_mat)
    # De ondervleugel gaat met de vleugel mee; alleen de binnenste rand hangt
    # aan de romp, anders spant hij bij het optillen als een plat vlak tussen
    # lijf en vleugel.
    # De ondervleugel draait volledig met de vleugel mee: hing de binnenrand aan
    # de romp, dan spande hij bij het optillen als een bleek gordijn tussen lijf
    # en vleugel. Onder de opgetilde vleugel is nu de flank te zien.
    skin(web,lambda p,n=name:{n:1.})
    for i in range(12):
        a=(-.03+i*.020,side*(.30+.008*i),1.13-.007*i)
        # De punten kruisen boven de stuit bijna op de middenlijn en liggen
        # plat op de dekveren, zoals bij een gesloten duivenvleugel.
        b=(.86+i*.012+.014*math.sin(i*1.7),side*(.030+.004*i),.60+.006*i+.015*math.sin(i*1.9))
        # De handpennen liggen schuin langs het lijf (normaal naar buiten en
        # omhoog) in plaats van als staande kaarten, zodat ze van achteren niet
        # als een V van pinnen boven de heupen uitsteken.
        feather('Beweegbare handpen '+label+' '+str(i),a,b,.058,(0,side*.87,.49),(0,0),(0,0),name+'Fan'+str(i),camber=.006,material=flight_palette[(i*3)%4],normal_end=(0,side*.12,.99),flip=side<0)
    # Handpennen die boven de stuit los van het lijf staan, liggen daar in
    # werkelijkheid op de rug en de dekveren. Vanaf een derde van hun lengte
    # worden ze op een kleine, per pen oplopende hoogte boven de huid gelegd, zodat
    # ze van achteren een platte, gekruiste waaier vormen in plaats van losse
    # kaartstapels.
    for i in range(12):
        ob=bpy.data.objects['Beweegbare handpen '+label+' '+str(i)]
        for k,vtx in enumerate(ob.data.vertices):
            t=[*np.linspace(0,.9,15),.94,.965,.982,.994,1][k//5]
            location,normal,index,distance=body_bvh.find_nearest(vtx.co)
            if location is None: continue
            height=(vtx.co-location).dot(normal); want=.010+.0022*i
            if height>want: vtx.co=vtx.co.lerp(location+normal*want,smoothstep(.25,.55,t))
        ob.data.update()
    # A second layer closes the underside when viewed during the wave.
    for i in range(10):
        t=i/9
        feather('Binnenste dekveer '+label+' '+str(i),(-.24+t*.55,side*.29,1.18-t*.17),(.12+t*.68,side*.27,.89-t*.18),.050,(0,-side,0),(1090+t*100,740),(1160+t*100,830),name)

# ---------------------------------------------------------------------------
# Verenkleed. Eerst de v3-stappen (achterhoofd, flank onder de vleugel), dan het
# vooraanzicht. De v2-atlas vulde gezicht, keel en voorhals met fotostukken die
# drie keer vergroot, ongelijk geschaald en op vaste rijen afgekapt waren: van
# voren waren dat horizontale vegen op het voorhoofd en streepjescode onder de
# snavel en op de krop. Die sector krijgt nu fotoveren op ware schaal
# (500 fotopixels per modeleenheid, gelijk in beide richtingen, gemeten langs
# het werkelijke huidoppervlak), zonder afkappen: buiten het bronvenster wordt
# het venster gespiegeld in plaats van één pixelrij uit te rekken.
atlas=next(n.image for n in body.data.materials[0].node_tree.nodes if n.type=='TEX_IMAGE' and n.image.colorspace_settings.name=='sRGB')
aw,ah=atlas.size; colors=np.array(atlas.pixels[:],np.float32).reshape(ah,aw,4)
photo=next(i for i in bpy.data.images if i.name.startswith('Foto voor web'))
pw,ph=photo.size; sourcepixels=np.array(photo.pixels[:],np.float32).reshape(ph,pw,4)
v,u=np.mgrid[0:ah,0:aw]/np.array([ah-1,aw-1])[:,None,None]; theta=u*2*math.pi; z=.426+v*1.64
def smooth(a,b,val):
    t=np.clip((val-a)/(b-a),0,1); return t*t*(3-2*t)
angle=(theta+math.pi)%(2*math.pi)-math.pi
# Bilineair in plaats van naaste pixel: de v3-achterhoofdpatch was van boven
# blokkerig (vergrote fotopixels).
def sample_bilinear(x,y):
    xx=np.clip(x*pw/1920,0,pw-1.001); yy=np.clip((1-y/1324)*ph,0,ph-1.001)
    xi=xx.astype(np.int32); yi=yy.astype(np.int32); fx=(xx-xi)[...,None]; fy=(yy-yi)[...,None]
    return (sourcepixels[yi,xi,:3]*(1-fx)+sourcepixels[yi,xi+1,:3]*fx)*(1-fy)+(sourcepixels[yi+1,xi,:3]*(1-fx)+sourcepixels[yi+1,xi+1,:3]*fx)*fy
backneck=sample_bilinear(759+angle*42,405+(1.78-z)*350)
backhead=sample_bilinear(717+angle*33,320+(z-1.83)*200)
patch=backneck*(1-smooth(1.75,1.87,z)[:,:,None])+backhead*smooth(1.75,1.87,z)[:,:,None]
weight=smooth(.68,.95,np.cos(theta))*smooth(1.33,1.52,z)*(1-smooth(1.99,2.04,z))
colors[:,:,:3]=colors[:,:,:3]*(1-weight[:,:,None])+patch*weight[:,:,None]
# The body under a raised wing is feathered flank, never a second printed wing.
rings=np.array([tuple(vertex.co) for vertex in body.data.vertices],np.float32).reshape(150,96,3)
cx=(rings[:,:,0].max(axis=1)+rings[:,:,0].min(axis=1))/2; rx=(rings[:,:,0].max(axis=1)-rings[:,:,0].min(axis=1))/2
xworld=np.interp(z,rings[:,0,2],cx)+np.interp(z,rings[:,0,2],rx)*np.cos(theta)
flank=sample_bilinear(785+np.clip(xworld+.25,0,1)*100,670+np.clip(1.35-z,0,1)*270)
weight=smooth(-.3,-.04,xworld)*smooth(.48,.62,z)*(1-smooth(1.20,1.39,z))*smooth(.05,.35,np.abs(np.sin(theta)))
colors[:,:,:3]=colors[:,:,:3]*(1-weight[:,:,None])+flank*weight[:,:,None]

# Booglengte vanaf de voorste middenlijn (vertex 48 van elke ring), per
# atlastexel geïnterpoleerd: zo meet de textuur op het echte, verbrede oppervlak.
closed=np.concatenate([rings,rings[:,:1]],axis=1)
arc_ring=np.concatenate([np.zeros((150,1)),np.cumsum(np.linalg.norm(np.diff(closed,axis=1),axis=2),axis=1)],axis=1)
arc_ring-=arc_ring[:,48:49]
columns=np.arange(aw)/(aw-1)*96
arc_cols=np.stack([np.interp(columns,np.arange(97),arc_ring[r]) for r in range(150)])
ring_f=np.clip(v[:,0]*149,0,149); r0=np.floor(ring_f).astype(int); r1=np.minimum(r0+1,149); rf=(ring_f-r0)[:,None]
arc=arc_cols[r0]*(1-rf)+arc_cols[r1]*rf
# Het vooraanzicht wordt gevuld met overlappende fotostukjes (splats): op een
# licht verspringend raster over gezicht, keel, voorhals en borst krijgt elk
# stukje een willekeurige bronplek binnen een vooraf bekeken venster zonder oog,
# snavel of achtergrond, op ware schaal. Waar stukjes overlappen wint de
# dichtstbijzijnde vloeiend. Geen spiegelnaden, geen uitgerekte rijen; aan
# weerszijden van de middenlijn wijzen de veren naar buiten en omlaag, zoals op
# een echte borst. Vaste seed: de atlas is reproduceerbaar.
WINDOWS={ # fotocoördinaten (1920 x 1324), handmatig gecontroleerd op de bronfoto
    'kruin':(692,252,758,298),'voorhoofd':(584,244,622,280),'teugel':(592,286,632,314),
    'wang':(602,332,680,384),'keel':(642,392,700,436),'hals':(700,446,836,566),'borst':(705,620,790,840)}
def window_for(zc,ac,rnd):
    if zc>1.93: return 'kruin' if rnd.random()<.7 else 'voorhoofd'
    if zc>1.84: return 'teugel' if abs(ac)<.07 and zc<1.89 else ('voorhoofd' if rnd.random()<.6 else 'kruin')
    if zc>1.72: return 'wang'
    if zc>1.60: return 'keel' if rnd.random()<.8 else 'wang'
    if zc>1.44: return 'hals'
    return 'borst'
region=(np.abs(u-.5)*360<np.interp(z,[.9,1.0,1.30,1.42,1.70,1.80,2.07],[40,50,52,58,58,60,60])+1)&(z>.92)
ra=arc[region]; rz=z[region]
acc=np.zeros((len(ra),3),np.float32); wsum=np.full(len(ra),1e-6,np.float32)
# Op de kop grotere stukjes (samenhangende bonte vlekken), op hals en borst
# kleinere (fijnere veren).
rnd=np.random.default_rng(1310)
centres=[]
for zc in np.arange(.92,1.72,.030):
    for ac in np.arange(-.48,.49,.030): centres.append((zc,ac,.030))
for zc in np.arange(1.72,2.10,.042):
    for ac in np.arange(-.36,.37,.042): centres.append((zc,ac,.042))
for zc,ac,spacing in centres:
    SIGMA=.45*spacing
    jz=zc+(rnd.random()-.5)*.6*spacing; ja=ac+(rnd.random()-.5)*.6*spacing
    near=(np.abs(rz-jz)<3*SIGMA)&(np.abs(ra-ja)<3*SIGMA)
    if not near.any(): continue
    x0,y0,x1,y1=WINDOWS[window_for(jz,ja,rnd)]
    xs=x0+rnd.random()*(x1-x0); ys=y0+rnd.random()*(y1-y0); side=1 if ja>=0 else -1
    px=np.clip(xs+side*(ra[near]-ja)*500,x0-6,x1+6); py=np.clip(ys+(jz-rz[near])*500,y0-6,y1+6)
    w=np.exp(-((ra[near]-ja)**2+(rz[near]-jz)**2)/(2*SIGMA**2))**2
    acc[near]+=sample_bilinear(px,py)*w[:,None]; wsum[near]+=w
# Kruin rondom (ook achter het oog en op de achterkruin, die van boven en
# bij het pikken te zien is): dezelfde stukjes, over de hele omtrek.
crown_region=(z>1.92)&~region
if crown_region.any():
    ca=arc[crown_region]; cz=z[crown_region]; cacc=np.zeros((len(ca),3),np.float32); csum=np.full(len(ca),1e-6,np.float32)
    for zc in np.arange(1.90,2.10,.040):
        for ac in np.arange(-.70,.71,.040):
            SIGMA=.45*.040; jz=zc+(rnd.random()-.5)*.024; ja=ac+(rnd.random()-.5)*.024
            near=(np.abs(cz-jz)<3*SIGMA)&(np.abs(ca-ja)<3*SIGMA)
            if not near.any(): continue
            x0,y0,x1,y1=WINDOWS['kruin']; xs=x0+rnd.random()*(x1-x0); ys=y0+rnd.random()*(y1-y0); side=1 if ja>=0 else -1
            px=np.clip(xs+side*(ca[near]-ja)*500,x0-6,x1+6); py=np.clip(ys+(jz-cz[near])*500,y0-6,y1+6)
            w=np.exp(-((ca[near]-ja)**2+(cz[near]-jz)**2)/(2*SIGMA**2))**2
            cacc[near]+=sample_bilinear(px,py)*w[:,None]; csum[near]+=w
    crown_colour=colors[:,:,:3].copy(); crown_colour[crown_region]=cacc/csum[:,None]
    cw=smooth(1.93,1.99,z)*(np.abs(u-.5)*360>=40)
    colors[:,:,:3]=colors[:,:,:3]*(1-cw[:,:,None])+crown_colour*cw[:,:,None]
front=colors[:,:,:3].copy(); front[region]=acc/wsum[:,None]
# Hetzelfde voor de achterhals: de v2/v3-projectie was daar horizontaal 2,4 keer
# en verticaal 1,4 keer vergroot (lange vegen), wat bij het pikken van boven
# zichtbaar wordt. Booglengte vanaf de achterste middenlijn.
circ=arc_ring[:,96]-arc_ring[:,0]
circ_row=circ[r0]*(1-rf[:,0])+circ[r1]*rf[:,0]
arc_back=arc-np.sign(arc)*circ_row[:,None]/2
BACK_WINDOWS={'nek_grijs':(752,352,796,424),'nek_groen':(792,444,842,516)}
back_region=(np.minimum(u,1-u)*360<62)&(z>1.40)&(z<1.98)
ba=arc_back[back_region]; bz=z[back_region]
bacc=np.zeros((len(ba),3),np.float32); bsum=np.full(len(ba),1e-6,np.float32)
for zc in np.arange(1.38,2.02,.032):
    for ac in np.arange(-.40,.41,.032):
        SIGMA=.45*.032
        jz=zc+(rnd.random()-.5)*.6*.032; ja=ac+(rnd.random()-.5)*.6*.032
        near=(np.abs(bz-jz)<3*SIGMA)&(np.abs(ba-ja)<3*SIGMA)
        if not near.any(): continue
        x0,y0,x1,y1=BACK_WINDOWS['nek_groen' if jz<1.62 or (jz<1.70 and rnd.random()<.5) else 'nek_grijs']
        xs=x0+rnd.random()*(x1-x0); ys=y0+rnd.random()*(y1-y0); side=1 if ja>=0 else -1
        px=np.clip(xs+side*(ba[near]-ja)*500,x0-6,x1+6); py=np.clip(ys+(jz-bz[near])*500,y0-6,y1+6)
        w=np.exp(-((ba[near]-ja)**2+(bz[near]-jz)**2)/(2*SIGMA**2))**2
        bacc[near]+=sample_bilinear(px,py)*w[:,None]; bsum[near]+=w
back=colors[:,:,:3].copy(); back[back_region]=bacc/bsum[:,None]
bweight=(1-smooth(48,60,np.minimum(u,1-u)*360))*smooth(1.44,1.54,z)*(1-smooth(1.90,1.97,z))
colors[:,:,:3]=colors[:,:,:3]*(1-bweight[:,:,None])+back*bweight[:,:,None]
# Ook de borst krijgt fotostukjes op ware schaal: de v2-projectie rekte de
# borstveren van voren uit tot houtnerfstrepen.
def blur(img,sigma):
    k=int(3*sigma); x=np.arange(-k,k+1); g=np.exp(-x*x/(2*sigma*sigma)); g/=g.sum()
    out=np.apply_along_axis(lambda m:np.convolve(np.pad(m,k,mode='wrap'),g,'valid'),1,img)
    return np.apply_along_axis(lambda m:np.convolve(np.pad(m,k,mode='edge'),g,'valid'),0,out)
# De tint van de borst blijft die van de flanken (de lage frequenties van de
# bestaande atlas); de stukjes leveren alleen de veertekening. Het fotovenster
# is donkerder dan de lichte flank en gaf anders een donkere voorkant.
low_front=np.stack([blur(front[:,:,k],10) for k in range(3)],axis=2)
low_orig=np.stack([blur(colors[:,:,k],10) for k in range(3)],axis=2)
breast=np.clip(low_orig+(front-low_front)*.55,0,1)
front=breast*(1-smooth(1.40,1.48,z))[:,:,None]+front*smooth(1.40,1.48,z)[:,:,None]
half=np.interp(z,[.9,1.0,1.30,1.42,1.70,1.80,2.07],[40,50,52,58,58,60,60])
offset=np.abs(u-.5)*360
weight=(1-smooth(half-12,half,offset))*smooth(.93,1.02,z)
colors[:,:,:3]=colors[:,:,:3]*(1-weight[:,:,None])+front*weight[:,:,None]
# Kop en hals van een gewone 'blauwe' stadsduif: de
# bronfoto is een bonte, lichtkoppige duif, en die vlekken lazen van voren en op
# 37 px als vuil. De kop wordt egaal leigrijs, de hals iriserend groen met
# paars naar de krop, met zachte overgangen. Alleen de fijne veertekening (de
# hoogdoorlaat van de helderheid) blijft uit de foto; de grove vlekken gaan weg.
lum_full=colors[:,:,:3]@np.array([.30,.59,.11])
detail=(lum_full-blur(lum_full,4))/np.maximum(blur(lum_full,4),.05)
detail=np.clip(1.5*detail,-.5,.5)
field=blur(np.random.default_rng(1312).random((ah,aw)),22); field=(field-field.mean())/(field.std()+1e-6)
HEAD_SLATE=np.array([.29,.32,.39])
head_colour=HEAD_SLATE*(1+.22*detail[:,:,None])*(1+.05*field[:,:,None])
# Iriserende hals: groen boven en opzij, paars naar de krop en de voorhals,
# met een zacht, onregelmatig grensvlak en de glans uit de fotoveertjes.
green=np.array([.20,.40,.28]); purple=np.array([.34,.25,.38])
front_share=np.clip(np.cos(theta-math.pi),0,1)
mix=smooth(1.40,1.66,z+.035*field-.06*front_share)
# In de nek (rugzijde) is de veertekening zachter: daar rekt de huid bij het
# pikken het meest, en van voren lazen uitgerekte veertjes als strepen.
nape=np.clip(np.cos(theta),0,1)*smooth(1.40,1.55,z)
neck_colour=(purple*(1-mix)[:,:,None]+green*mix[:,:,None])*(1+.55*(1-.65*nape)[:,:,None]*detail[:,:,None])*(1+.10*field[:,:,None])
neck_colour=neck_colour*(1-smooth(1.66,1.76,z))[:,:,None]+head_colour*smooth(1.66,1.76,z)[:,:,None]
back_share=np.clip(np.cos(theta),0,1)
head_share=smooth(1.28+.16*back_share,1.44+.14*back_share,z+.02*field)
colors[:,:,:3]=colors[:,:,:3]*(1-head_share)[:,:,None]+np.clip(neck_colour,0,1)*head_share[:,:,None]
# De kruinpool: alle kolommen komen in één punt samen, waardoor van boven een
# klein wit vlekje met een punt zichtbaar was. Boven z = 2,02 loopt de kleur
# geleidelijk over in het gemiddelde van de ring eronder.
ring_row=int(round((2.02-.426)/1.64*(ah-1)))
pole_colour=colors[ring_row,:,:3].mean(axis=0)
pole=smooth(2.02,2.06,z)[:,:,None]
colors[:,:,:3]=colors[:,:,:3]*(1-pole)+pole_colour*pole
# Onderkant: de onderste rijen van de atlas waren een bleke, uitgerekte
# fotostrook met een pool (van onderen een witte rechthoek met ster). Ze lopen
# over in de buikkleur van de rij erboven, met dezelfde fijne tekening.
belly_row=int(round((.62-.426)/1.64*(ah-1)))
belly_colour=colors[belly_row,:,:3].mean(axis=0)
belly=(1-smooth(.50,.60,z))[:,:,None]
colors[:,:,:3]=colors[:,:,:3]*(1-belly)+belly_colour*(1+.5*detail[:,:,None])*belly
# Onderbuik van voren: de v2-projectie had daar donkere, krasserige
# vegen. Tussen de poten en de borst blijft alleen de zachte kleur met een
# gedempte fijne tekening over.
belly_front=(1-smooth(70,100,np.abs(u-.5)*360))*(1-smooth(.95,1.08,z))
# De vegen zijn breder dan elke vervaging; de grondkleur wordt daarom per rij
# het gemiddelde van de voorkant, met een vleugje van de fijne tekening.
front_cols=slice(int(.36*aw),int(.64*aw))
row_colour=colors[:,front_cols,:3].mean(axis=1)
row_colour=np.stack([np.convolve(np.pad(row_colour[:,k],40,mode='edge'),np.ones(81)/81,'valid') for k in range(3)],axis=1)
calm=row_colour[:,None,:]*(1+.20*detail[:,:,None])*(1+.04*field[:,:,None])
colors[:,:,:3]=colors[:,:,:3]*(1-belly_front[:,:,None])+calm*belly_front[:,:,None]
newatlas=bpy.data.images.new('V4 doorlopend verenkleed',width=aw,height=ah); newatlas.pixels=colors.ravel(); newatlas.filepath_raw=os.path.join(OUT,'verenkleed-v4.jpg'); newatlas.file_format='JPEG'; newatlas.save(quality=92); newatlas.pack()
# De normaalkaart van de huid kwam uit de oude atlas en droeg daarmee ook de
# streepjescode als reliëf. Ze wordt op dezelfde manier uit de nieuwe atlas
# afgeleid (helling van de helderheid), op 1024 pixels zoals in v2.
lum=colors[::2,::2,:3].mean(axis=2); gy,gx=np.gradient(lum)
polar=smooth(2.00,2.04,z[::2,::2]); gy*=1-polar; gx*=1-polar
# Kop en hals: het reliëf uit de fotohelderheid las als leer en reptielenhuid.
# Daar blijft alleen een zwakke, vervaagde veerstructuur over.
soft_head=1-.85*smooth(1.24,1.40,z[::2,::2]); gy*=soft_head; gx*=soft_head
normal=np.stack((-gx*2,-gy*2,np.ones_like(gx)),axis=2); normal/=np.linalg.norm(normal,axis=2,keepdims=True)
normal_image=bpy.data.images.new('V4 verenkleed normaal',width=lum.shape[1],height=lum.shape[0]); normal_image.colorspace_settings.name='Non-Color'
normal_image.pixels=np.concatenate((normal*.5+.5,np.ones(lum.shape+(1,))),axis=2).astype(np.float32).ravel()
normal_image.filepath_raw=os.path.join(OUT,'verenkleed-v4-normaal.png'); normal_image.file_format='PNG'; normal_image.save(); normal_image.pack()
for n in body.data.materials[0].node_tree.nodes:
    if n.type=='TEX_IMAGE' and n.image==atlas: n.image=newatlas
    elif n.type=='TEX_IMAGE' and n.image.colorspace_settings.name=='Non-Color': n.image=normal_image

# De huid sluit boven op de kruin en onder bij de cloaca met een plat
# 96-hoekig deksel. Van boven was dat een wit plaatje met een kartelrand. Het
# deksel wordt een fan rond een middenpunt dat net boven (onder) het vlak ligt,
# zodat de kruin rond doorloopt. De nieuwe middenpunten volgen het kop- of rompbot.
bm=bmesh.new(); bm.from_mesh(body.data); uv_layer=bm.loops.layers.uv.active
caps=[f for f in bm.faces if len(f.verts)>4]
result=bmesh.ops.poke(bm,faces=caps)
for vert in result['verts']:
    top=vert.co.z>1.5; vert.co.z+=.003 if top else -.003
    for loop in vert.link_loops: loop[uv_layer].uv=(.5,1. if top else 0.)
bm.to_mesh(body.data); bm.free(); body.data.update()
for vert in body.data.vertices[14400:]:
    body.vertex_groups['HeadRig' if vert.co.z>1.5 else 'BodyRig'].add([vert.index],1.,'REPLACE')
for poly in body.data.polygons: poly.use_smooth=True
# De atlas herhaalt zich verticaal: op v = 0 en v = 1 precies mengt de sampler
# de bovenste en onderste beeldrij (kruin en cloaca). Dat gaf het spiraalvlekje
# op de kruin; de uiterste rijen blijven daarom een halve texel binnen de rand.
edge=.5/ah
for loop_uv in body.data.uv_layers.active.data: loop_uv.uv=(loop_uv.uv[0],min(1-edge,max(edge,loop_uv.uv[1])))

# ---------------------------------------------------------------------------
# Clips. Elke clip is een stel continue kanalen (hoeken in radialen), per frame
# bemonsterd en op alle botten gekeyd. Tussen sleutelposes interpoleert een
# monotone kubische spline (PCHIP): vloeiend, zonder doorschieten, met exacte
# stilstand waar twee sleutels gelijk zijn (fixatiepauzes). Begin- en eindsleutel
# zijn exact de bind-pose, met snelheid nul.
scene.render.fps=30; scene.frame_start=1; scene.frame_end=97
DURATION=3.2; FRAMES=97
def pchip(keys):
    ts=np.array([k[0] for k in keys],float); ys=np.array([k[1] for k in keys],float)
    h=np.diff(ts); d=np.diff(ys)/h; m=np.zeros_like(ys)
    for i in range(1,len(ys)-1):
        if d[i-1]*d[i]>0:
            w1=2*h[i]+h[i-1]; w2=h[i]+2*h[i-1]; m[i]=(w1+w2)/(w1/d[i-1]+w2/d[i])
    def f(t):
        if t<=ts[0]: return float(ys[0])
        if t>=ts[-1]: return float(ys[-1])
        i=int(np.searchsorted(ts,t,side='right'))-1; hh=h[i]; s=(t-ts[i])/hh
        return float((2*s**3-3*s**2+1)*ys[i]+(s**3-2*s**2+s)*hh*m[i]+(-2*s**3+3*s**2)*ys[i+1]+(s**3-s**2)*hh*m[i+1])
    return f

rig.animation_data_clear()
def set_rotation(name,pitch=0.,roll=0.,axis=None,angle=0.):
    pb=rig.pose.bones[name]; rest=pb.bone.matrix_local.to_quaternion()
    q=Quaternion(axis,angle) if axis else Quaternion((0,1,0),pitch)@Quaternion((1,0,0),roll)
    pb.rotation_mode='QUATERNION'; pb.rotation_quaternion=rest.inverted()@q@rest
def reset():
    for b in rig.pose.bones: b.rotation_mode='QUATERNION'; b.rotation_quaternion=(1,0,0,0); b.location=(0,0,0); b.scale=(1,1,1)
# Halsprofiel voor het reiken naar de grond: alle vier de gewrichten buigen
# dezelfde kant op, het meest aan de basis. Zo blijft het halssilhouet tussen
# borst en kop één gladde, bolle boog; een S (onderhals voorover, bovenhals
# terug) las als zwanenhals. Negatief = voorover/omlaag.
NECK_PROFILE=[-.50,-.45,-.15,0]
# Daarnaast een gelijkmatige kromming (curl) voor de houdingen waarin de kop
# dichter bij de borst blijft, zoals bij het fixeren boven de grond.
CURL_PROFILE=[-.25,-.25,-.25,-.25]
NECK_STRETCH=.045
WING_FOLD=.5   # hoe ver de vleugelpunt onderweg naar achteren zwaait (radialen)
def neck_angles(c): return [r*c.get('neck',0.)+q*c.get('curl',0.) for r,q in zip(NECK_PROFILE,CURL_PROFILE)]
def pose(c):
    # c: body (romppitch), roll (romprol), neck (totale halsbuiging), head
    # (kophoek in de wereld; het kopbot krijgt head-body-neck, dus de kop
    # houdt zijn eigen stand vast terwijl hals en romp bewegen), jaw, tail,
    # wing (0..1), fan (0..1).
    reset()
    set_rotation('BodyRig',c.get('body',0.),c.get('roll',0.))
    # Door de poten zakken: de romp gaat omlaag en een fractie naar achteren,
    # de tarsus kantelt met de bovenkant naar achteren (hielgewricht buigt).
    body_bone=rig.pose.bones['BodyRig']
    body_bone.location=body_bone.bone.matrix_local.to_3x3().inverted()@Vector((c.get('back',0.),0,-c.get('drop',0.)))
    for label in ('Left','Right'): set_rotation('Leg'+label,c.get('crouch',0.))
    angles=neck_angles(c)
    for k,angle in enumerate(angles):
        set_rotation(f'Neck{k+1}',angle,-c.get('roll',0.)*(.4 if k==0 else .15))
    # Een kleine zijwaartse draai van de halsbasis: de duif pikt iets naast
    # de middenlijn, zodat van voren gezicht en snavel in beeld blijven.
    yaw=c.get('yaw',0.)
    if yaw:
        for name,share in (('Neck1',.6),('Neck2',.4)):
            pb=rig.pose.bones[name]; rest=pb.bone.matrix_local.to_quaternion()
            pb.rotation_quaternion=rest.inverted()@Quaternion((0,0,1),yaw*share)@rest@pb.rotation_quaternion
    set_rotation('HeadRig',c.get('head',0.)-c.get('body',0.)-sum(angles),-c.get('roll',0.)*.1)
    # De kop draait bij het pikken bovendien een fractie om zijn eigen
    # lengteas (zoals een duif schuin kijkt): van voren blijven gezicht, oog en
    # snavel in beeld.
    turn=c.get('headturn',0.)
    if turn:
        pb=rig.pose.bones['HeadRig']; pb.rotation_quaternion=pb.rotation_quaternion@Quaternion((0,1,0),turn)
    # Strekken: de in rust in een S gevouwen halswervels vouwen bij het reiken
    # uit, dus de hals wordt werkelijk langer. De drie gewrichten tussen
    # halsbasis en kop schuiven daarvoor een stukje langs hun eigen as (geen root-beweging; de
    # voeten en de romp blijven waar ze zijn).
    for name in ('Neck2','Neck3','Neck4'):
        rig.pose.bones[name].location=(0,NECK_STRETCH*c.get('stretch',0.),0)
    set_rotation('JawRig',c.get('jaw',0.))
    set_rotation('TailRig',c.get('tail',0.))
    for side,label in [(-1,'Left'),(1,'Right')]:
        # Optillen om de lengteas van de romp: de vleugels komen uit de
        # schouder omhoog tot boven de rug in een V (piek ruim 135 graden,
        # dus hooguit ongeveer 45 graden naar buiten), niet zijwaarts als armen.
        # Eerst gaat de pols omhoog (de vleugel kantelt om een dwarsas, de
        # punt komt boven de rug), daarna klapt hij pas open: zo passeert de
        # vleugel van voren nooit horizontaal als een peddel.
        lift=c.get('wing',0.); tilt=c.get('wingtilt',0.); fold=c.get('wingfold',0.)
        pb=rig.pose.bones['Wing'+label]; rest=pb.bone.matrix_local.to_quaternion()
        # Onderweg naar boven en terug zwaait de punt even naar achteren
        # (om de verticale as), zodat de vleugel niet horizontaal opzij
        # uitschiet: de zijwaartse uitslag loopt in een vloeiende boog over
        # zeven frames in plaats van één flits. De kanteling is begrensd door
        # de cameraomhulling van de runtime.
        q=Quaternion((0,0,1),-side*WING_FOLD*fold)@Quaternion((0,1,0),-.20*lift-.26*tilt)@Quaternion((1,0,0),side*2.05*lift)
        pb.rotation_mode='QUATERNION'; pb.rotation_quaternion=rest.inverted()@q@rest
        for i in range(12): set_rotation('Wing'+label+'Fan'+str(i),axis=(0,0,1),angle=side*(.10+i*.037)*c.get('fan',0.))
def tip_position():
    bpy.context.view_layer.update(); pb=rig.pose.bones['HeadRig']
    return pb.matrix@pb.bone.matrix_local.inverted()@BEAK_TIP
def solve_pose(channels,bill_deg,target_z):
    # Zoek de reikschaal van de hals waarbij de snavelpunt precies op target_z
    # komt en de snavelas bill_deg onder de horizon staat, bij gegeven romp,
    # poten, strekking en zijdraai (bisectie op de reikschaal). Geeft
    # (reikschaal, kophoek in de wereld).
    head=math.radians(34-bill_deg)
    def apply(scale):
        c=dict(channels); c.update(neck=scale,head=head); pose(c)
    low,high=-.3,2.4
    for _ in range(40):
        mid=(low+high)/2; apply(mid)
        if tip_position().z>target_z: low=mid
        else: high=mid
    apply((low+high)/2); tip=tip_position()
    assert abs(tip.z-target_z)<.003,(channels,bill_deg,target_z,tip)
    return (low+high)/2,head
# Peck, naar hoe een duif pikt (Zweers 1982; eigen video-observatie van
# stadsduiven): kort vooruitkijken, dan daalt de kop in twee stappen met een
# korte fixatiepauze, waarbij de kop de beweging leidt en de romp voorover
# meekantelt; de laatste stoot naar de grond is snel (0,1 s), de snavel gaat
# vlak voor het contact open en sluit bij het grijpen; direct terug naar de
# tweede fixatiehoogte en een tweede pik; daarna komt de kop eerst omhoog en
# richt de romp zich op, met een kleine uitslinger. De voeten staan stil.
GROUND=.014
HEADTURN=float(os.environ.get('DUIVEN_HEADTURN','.15'))
# Sleutelposes, naar foto's en de kinematica van pikkende duiven. Theunissen
# e.a. (J Exp Biol 2017, "Pigeons use distinct stop phases to control
# pecking") meten twee stilstanden van de kop: F1 van gemiddeld 250 ms en F2
# van 122 ms, daarna een ballistische stoot; de snavel gaat pas 25-30 ms na F2
# open. Goodale (1983) vond F1 op meer dan 80 mm en F2 op ongeveer 55 mm (ogen
# tot doel); bij deze modelschaal (1 eenheid ~ 14 cm) is dat een snavelpunt op
# ongeveer 0,45 en 0,28 boven de grond. Het hele lijf kantelt voorover (37
# graden) en zakt door de poten (hielgewricht buigt, tenen blijven staan); de
# hals schuift grotendeels gestrekt naar voren en omlaag, zodat haar silhouet
# bol blijft (geen S). Stoot en terug duren elk 0,10 s.
# headturn: de kop draait bij het naderen en pikken zo'n 9 graden om zijn eigen
# as, bovenop de zijdraai van de hals; zo blijft van voren het gezicht
# in beeld. Van opzij verandert de houding nauwelijks.
LOW=dict(body=-.64,drop=.02,back=.03,crouch=.10,headturn=HEADTURN)
F1=dict(body=-.46,drop=.012,back=.018,crouch=.06,headturn=HEADTURN)
F2=dict(body=-.56,drop=.016,back=.024,crouch=.08,headturn=HEADTURN)
PECK_POSES=[ # t, kanalen, snavelhoek, snavelhoogte (None = reikschaal 0)
    (0,{},34,None),(.18,dict(body=.02),44,None),
    (.45,dict(F1,stretch=.25,yaw=.16),72,.45),(.70,dict(F1,stretch=.25,yaw=.22),72,.45),
    (.80,dict(F2,stretch=.35,yaw=.26),82,.28),(.92,dict(F2,stretch=.35,yaw=.26),82,.28),
    (1.02,dict(LOW,stretch=.5,yaw=.30),92,GROUND),(1.08,dict(LOW,stretch=.5,yaw=.30),92,GROUND),
    (1.18,dict(F2,stretch=.35,yaw=.26),82,.28),(1.30,dict(F2,stretch=.35,yaw=.26),82,.28),
    (1.40,dict(LOW,stretch=.5,yaw=.28),93,GROUND),(1.46,dict(LOW,stretch=.5,yaw=.28),93,GROUND),
    (1.62,dict(body=-.40,drop=.01,back=.015,crouch=.05,stretch=.2,yaw=.08,headturn=HEADTURN*.6),76,.46),
    (1.98,dict(body=-.12,stretch=.05,yaw=.02),46,None),(2.36,dict(body=.012),32,None),
    (2.70,{},34,None),(DURATION,{},34,None)]
CHANNEL_NAMES=('body','drop','back','crouch','curl','stretch','yaw','headturn')
PECK_KEYS=[]
for t,channels,bill,z in PECK_POSES:
    if z is None: neck,head=0.,math.radians(34-bill)
    else: neck,head=solve_pose(channels,bill,z)
    PECK_KEYS.append((t,dict({k:channels.get(k,0.) for k in CHANNEL_NAMES},neck=neck,head=head)))
PECK_CURVES={k:pchip([(t,c[k]) for t,c in PECK_KEYS]) for k in CHANNEL_NAMES+('neck','head')}
peck_jaw=pchip([(0,0),(.95,0),(.99,-.12),(1.02,-.10),(1.06,0),(1.33,0),(1.37,-.11),(1.40,-.09),(1.44,0),(DURATION,0)])
peck_flick=pchip([(0,0),(1.62,0),(1.98,-.06),(2.32,.03),(2.60,0),(DURATION,0)])
def peck_channels(t):
    c={k:curve(t) for k,curve in PECK_CURVES.items()}
    c.update(jaw=peck_jaw(t),tail=-.30*c['body']+peck_flick(t))
    return c
# WingWave: de duif trekt de vleugels even aan, tilt ze snel half op met de
# handpennen iets later uitwaaierend, laat ze half zakken, geeft een tweede
# kleinere slag, vouwt ze eerst weer dicht en legt ze dan schuin terug (zodat
# ze van voren nooit als platte schijf door de horizontaal gaan), en schudt
# het verenkleed kort op zijn plek. De kop blijft in de ruimte stil staan: hals
# en kopbot compenseren romphoek en rol.
wave_wing=pchip([(0,0),(.16,0),(.40,1.0),(.52,1.0),(.66,.82),(.80,.98),(1.00,.96),(1.30,0),(DURATION,0)])
wave_fold=pchip([(0,0),(.16,0),(.28,.5),(.42,0),(DURATION,0)])
wave_tilt=pchip([(0,0),(.10,0),(.26,1.0),(.40,.5),(.50,0),(.96,0),(1.10,1.0),(1.24,.6),(1.36,0),(DURATION,0)])
wave_fan=pchip([(0,0),(.38,0),(.50,.8),(.58,1.0),(.70,.55),(.84,.85),(.88,.80),(.98,0),(DURATION,0)])
wave_body=pchip([(0,0),(.18,-.045),(.48,.03),(.95,.01),(1.50,0),(DURATION,0)])
wave_neck=pchip([(0,0),(.18,-.02),(.50,.05),(1.10,.03),(1.60,0),(DURATION,0)])
# Schudden: een korte, uitdempende rol van ruim 4 Hz (zeven frames per slag),
# zodat het in 30 fps een vloeiende beweging blijft en geen haperen.
wave_roll=pchip([(0,0),(1.62,0),(1.74,.07),(1.86,-.06),(1.98,.04),(2.10,-.02),(2.22,0),(DURATION,0)])
wave_tail=pchip([(0,0),(.30,0),(.50,-.10),(.80,-.04),(1.00,-.08),(1.50,0),(DURATION,0)])
def wave_channels(t):
    return {'body':wave_body(t),'neck':wave_neck(t),'head':0.,'roll':wave_roll(t),'wing':wave_wing(t),'wingtilt':wave_tilt(t),'wingfold':wave_fold(t),'fan':wave_fan(t),'tail':wave_tail(t)}
CHANNELS={'Peck':peck_channels,'WingWave':wave_channels}
clips={}; bone_extremes={}
for clip,channels in CHANNELS.items():
    action=bpy.data.actions.new(clip); rig.animation_data_create(); rig.animation_data.action=action
    extremes={}
    for frame in range(1,FRAMES+1):
        t=(frame-1)/30; c=channels(t) if frame not in (1,FRAMES) else {}
        pose(c)
        for b in rig.pose.bones:
            b.keyframe_insert('rotation_quaternion',frame=frame,group=b.name)
            if b.name in ('Neck2','Neck3','Neck4','BodyRig'): b.keyframe_insert('location',frame=frame,group=b.name)
            q=b.rotation_quaternion; angle=2*math.acos(min(1.,abs(q.w)))
            if angle>1e-6: extremes[b.name]=max(extremes.get(b.name,0.),math.degrees(angle))
    track=rig.animation_data.nla_tracks.new(); track.name=clip
    track.strips.new(clip,1,action); track.mute=True; clips[clip]=action
    bone_extremes[clip]={k:round(v,2) for k,v in sorted(extremes.items())}
rig.animation_data.action=None; reset(); scene.frame_set(1)
# Harde controle in de bron: de huid doorsnijdt zichzelf in geen enkel frame van
# beide clips (geen twee vlakken zonder gedeeld hoekpunt die elkaar overlappen).
# De herimport van de GLB wordt in verify_pigeon_v4.py op dezelfde manier getest.
self_intersections={}
for clip,action in clips.items():
    rig.animation_data.action=action
    if hasattr(rig.animation_data,'action_slot') and action.slots: rig.animation_data.action_slot=action.slots[0]
    worst=0
    for frame in range(1,FRAMES+1):
        scene.frame_set(frame); evaluated=body.evaluated_get(bpy.context.evaluated_depsgraph_get()); data=evaluated.to_mesh()
        points=[v.co.copy() for v in data.vertices]; polygons=[tuple(p.vertices) for p in data.polygons]; evaluated.to_mesh_clear()
        tree=BVHTree.FromPolygons(points,polygons)
        crossing=sum(1 for i,j in tree.overlap(tree) if i<j and not set(polygons[i])&set(polygons[j]))
        if crossing: print('SELF_INTERSECTION',clip,frame,crossing)
        assert crossing==0 or os.environ.get('DUIVEN_MODEL_EXPLORE')=='1',(clip,frame,'skin self-intersection',crossing)
    self_intersections[clip]=0
rig.animation_data.action=None; reset(); scene.frame_set(1)
root['animation_contract']='Peck: fixaties F1 0.45-0.70 s en F2 0.80-0.92 s, twee pikken (grondcontact 1.02 s en 1.40 s); WingWave: vleugels boven de rug 0.40-0.52 s en 0.80-1.00 s, schudden 1.62-2.28 s; duur 3.2 s; bind pose op 0 en 3.2 s'
root['method']='Own skinned model, photographed plumage, four-bone neck, articulated jaw, connected leg sleeves/rump, closed tail fan, overlapping articulated wing feathers'
root['limitations']='Mirrored photographic source; baked source illumination; compact half-open wing study, not flight simulation'

# ---------------------------------------------------------------------------
# Export en beelden.
scene.render.engine='CYCLES'; scene.cycles.samples=12 if QUICK else 32
scene.render.resolution_percentage=100; scene.render.film_transparent=True; scene.render.image_settings.file_format='PNG'
scene.render.image_settings.color_mode='RGBA'
camera=scene.camera; camera.data.type='ORTHO'
if scene.world.node_tree is None: scene.world.use_nodes=True
background=scene.world.node_tree.nodes['Background']; world_color=tuple(background.inputs[0].default_value)
def shoot(path,position,target,scale,resolution,flat=None):
    camera.data.ortho_scale=scale; camera.location=position
    camera.rotation_euler=(Vector(target)-Vector(position)).to_track_quat('-Z','Y').to_euler()
    scene.render.resolution_x,scene.render.resolution_y=resolution
    scene.render.film_transparent=flat is None
    background.inputs[0].default_value=flat if flat else world_color
    scene.render.filepath=path; bpy.ops.render.render(write_still=True)
    background.inputs[0].default_value=world_color; scene.render.film_transparent=True
def use_clip(clip,seconds):
    if clip is None: rig.animation_data.action=None; reset()
    else:
        action=clips[clip]; rig.animation_data.action=action
        if hasattr(rig.animation_data,'action_slot') and action.slots: rig.animation_data.action_slot=action.slots[0]
    scene.frame_set(1+round(seconds*30)); bpy.context.view_layer.update()
def contact_sheet(paths,columns,out,grey=(.66,.67,.69)):
    # Framestrook op een egale lichtgrijze ondergrond: een donkere snavel is op
    # een transparante (in viewers zwarte) achtergrond niet te beoordelen.
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

def unit_float32(q):
    # Genormaliseerd in float64, dan float32; zolang de float32-waarden samen
    # net onder norm 1 uitkomen, schuift de grootste component één ulp op. Een
    # quaternion met |q|^2 < 1 geeft in three.js een hoek > 0 met zichzelf
    # (2·acos(q·q)), en dan faalt de eindpose-controle van de runtime.
    q=np.asarray(q,np.float64); q=(q/np.linalg.norm(q)).astype(np.float32)
    while float(np.dot(q.astype(np.float64),q.astype(np.float64)))<1.:
        k=int(np.argmax(np.abs(q))); q[k]=np.nextafter(q[k],np.float32(np.copysign(2,q[k])))
    return q
def clip_peaks(rate=60):
    # Piekmomenten per clip voor de cameraomhulling van de runtime
    # (extras.peaks): de tijden waarop de vervormde geometrie in een van
    # dertien richtingen (de drie assen en hun diagonalen, beide kanten) het
    # verst uitsteekt. De runtime samplet die momenten expliciet, zodat een
    # piek tussen twee vaste samples niet buiten het podium valt.
    directions=[np.array(d,float) for d in [(1,0,0),(0,1,0),(0,0,1),(1,1,0),(1,-1,0),(1,0,1),(1,0,-1),(0,1,1),(0,1,-1),(1,1,1),(1,1,-1),(1,-1,1),(-1,1,1)]]
    directions=[d/np.linalg.norm(d) for d in directions]
    meshes=[o for o in scene.objects if o.type=='MESH']
    peaks={}
    for clip,action in clips.items():
        rig.animation_data.action=action
        if hasattr(rig.animation_data,'action_slot') and action.slots: rig.animation_data.action_slot=action.slots[0]
        best={}
        for k in range(int(DURATION*rate)+1):
            t=k/rate; frame=1+t*30; scene.frame_set(int(frame),subframe=frame-int(frame))
            depsgraph=bpy.context.evaluated_depsgraph_get(); points=[]
            for ob in meshes:
                ev=ob.evaluated_get(depsgraph); data=ev.to_mesh(); co=np.empty(len(data.vertices)*3,np.float32); data.vertices.foreach_get('co',co)
                matrix=np.array(ev.matrix_world); points.append(co.reshape(-1,3)@matrix[:3,:3].T+matrix[:3,3]); ev.to_mesh_clear()
            points=np.concatenate(points)
            for i,d in enumerate(directions):
                projection=points@d
                for sign,value in ((1,projection.max()),(-1,-projection.min())):
                    if (i,sign) not in best or value>best[(i,sign)][0]: best[(i,sign)]=(value,t)
        times=sorted({round(t,4) for _,t in best.values() if 0<t<DURATION})
        merged=[]
        for t in times:
            if not merged or t-merged[-1]>1.5/rate: merged.append(t)
        peaks[clip]=merged
    rig.animation_data.action=None; reset(); scene.frame_set(1)
    return peaks

def exact_bind_pose(path,peaks=None):
    # Na de export: node-rotaties en alle rotatiesleutels op één-norm, en de
    # eerste en laatste sleutel bitgelijk aan de rustrotatie van de node. De
    # exporter rondt pose en rust apart af, waardoor ze tot 7e-4 rad uiteen
    # konden liggen; de runtime eist 1e-6. Geen andere bytes veranderen.
    raw=bytearray(open(path,'rb').read()); length=struct.unpack_from('<I',raw,12)[0]
    data=json.loads(bytes(raw[20:20+length])); base=28+length
    for node in data['nodes']:
        if 'rotation' in node: node['rotation']=[float(x) for x in unit_float32(node['rotation'])]
    for animation in data['animations']:
        for channel in animation['channels']:
            if channel['target']['path']!='rotation': continue
            accessor=data['accessors'][animation['samplers'][channel['sampler']]['output']]; view=data['bufferViews'][accessor['bufferView']]
            offset=base+view.get('byteOffset',0)+accessor.get('byteOffset',0)
            keys=np.frombuffer(bytes(raw[offset:offset+16*accessor['count']]),np.float32).reshape(-1,4).copy()
            keys=np.array([unit_float32(k) for k in keys],np.float32)
            rest=unit_float32(data['nodes'][channel['target']['node']].get('rotation',[0,0,0,1]))
            for end in (0,-1):
                assert 2*math.acos(min(1.,abs(float(keys[end].astype(np.float64)@rest.astype(np.float64)))))<2e-3,(channel,end)
                keys[end]=rest
            raw[offset:offset+keys.nbytes]=keys.tobytes()
            accessor['min']=[float(x) for x in keys.min(axis=0)]; accessor['max']=[float(x) for x in keys.max(axis=0)]
    for animation in data['animations']:
        if peaks and animation['name'] in peaks: animation.setdefault('extras',{})['peaks']=peaks[animation['name']]
    text_json=json.dumps(data,separators=(',',':')).encode()
    text_json+=b' '*((4-len(text_json)%4)%4)
    out=raw[:12]+struct.pack('<II',len(text_json),0x4E4F534A)+text_json+raw[20+length:]
    struct.pack_into('<I',out,8,len(out))
    open(path,'wb').write(out)

text=bpy.data.texts.new('build_pigeon_v4.py')
with open(__file__) as f:text.from_string(f.read())
bpy.ops.object.select_all(action='DESELECT')
def select_tree(ob):
    ob.select_set(True)
    for c in ob.children:select_tree(c)
select_tree(root)
glb=os.path.join(OUT,NAME+'.glb')
REST_VIEWS=[('rust-links',(-.65,-6,1.60),(.08,0,1.0),3.05,(1400,1100)),
    ('rust-voor',(-6,0,1.30),(-.30,0,1.02),2.55,(1100,1100)),
    ('rust-driekwart-voor',(-4.3,-4.3,1.55),(-.15,0,1.02),2.85,(1200,1100)),
    ('rust-achter',(6,0,1.45),(.30,0,1.0),2.55,(1100,1100)),
    ('rust-driekwart-achter',(4.3,-4.3,1.7),(.25,0,1.0),2.95,(1200,1100))]
HEAD_VIEWS=[('kop-zij',(-.95,-3,1.86),(-.95,0,1.86),.62),('kop-voor',(-3.4,0,1.83),(-.93,0,1.83),.60),
    ('kop-driekwart',(-2.6,-1.8,2.1),(-.93,0,1.86),.72),('kop-boven',(-1.3,0,3.4),(-.95,0,1.85),.62),
    ('kop-driekwart-voor',(-2.7,-2.2,1.86),(-.90,0,1.84),.66)]
STRIP_VIEWS=[('zij',(-.75,-6,1.25),(-.25,0,.95),3.45),('voor',(-6,0,1.25),(-.30,0,.95),3.0),('driekwart-voor',(-4.5,-4.1,1.45),(-.35,0,.95),3.5)]
STRIP_MOMENTS={'Peck':[0,.18,.45,.70,.80,.92,1.02,1.18,1.40,1.62,2.00,3.2],'WingWave':[0,.20,.27,.33,.40,.67,.90,1.07,1.13,1.20,1.80,3.2]}
if not MEASURE_ONLY:
    # ACTIONS exports the single armature's two compatible actions as two complete
    # clips. Keep muted NLA strips as editable source and to retain action users.
    bpy.ops.export_scene.gltf(filepath=glb,export_format='GLB',use_selection=True,export_apply=False,export_animations=True,export_animation_mode='ACTIONS',export_merge_animation='ACTION',export_extras=True,export_frame_range=False,export_anim_slide_to_zero=True)
    CLIP_PEAKS=clip_peaks()
    exact_bind_pose(glb,CLIP_PEAKS)
    bpy.ops.wm.save_as_mainfile(filepath=os.path.join(OUT,NAME+'.blend'))
    use_clip(None,0)
    for label,position,target,scale,resolution in REST_VIEWS:
        shoot(os.path.join(OUT,f'iteratie-{ITERATION}-{label}.png'),position,target,scale,resolution if not QUICK else (resolution[0]//2,resolution[1]//2))
    contact_sheet([os.path.join(OUT,f'iteratie-{ITERATION}-{v[0]}.png') for v in REST_VIEWS[:4]],4,os.path.join(OUT,f'iteratie-{ITERATION}-rust-overzicht.png'))
    # Kopdetails op een egale achtergrond, zoals in v3: een lichte washuid en
    # een donkere snavel zijn op een transparante laag niet te beoordelen.
    for label,position,target,scale in HEAD_VIEWS:
        shoot(os.path.join(OUT,f'iteratie-{ITERATION}-{label}.png'),position,target,scale,(900,900) if not QUICK else (450,450),flat=(.62,.64,.66,1))
    size=(420,400) if QUICK else (560,520)
    for clip,moments in STRIP_MOMENTS.items():
        cells=[]
        for view,position,target,scale in STRIP_VIEWS:
            for k,seconds in enumerate(moments):
                use_clip(clip,seconds); path=os.path.join(OUT,'frames',f'{clip}-{view}-{k:02d}.png')
                shoot(path,position,target,scale,size); cells.append(path)
        contact_sheet(cells,len(moments),os.path.join(OUT,f'iteratie-{ITERATION}-{clip}-strook.png'))
    use_clip(None,0)

# ---------------------------------------------------------------------------
# Maten. De snavel wordt gemeten zoals de referentiefoto's: in het zijaanzicht
# (xz-vlak), vanaf de veerlijn op de culmen tot de punt, en tegen de afstand
# oog-snavelpunt en de koplengte (veerlijn tot achterhoofd op ooghoogte).
def world_vertices(name):
    ob=bpy.data.objects[name]; M=ob.matrix_world
    return np.array([tuple(M@v.co) for v in ob.data.vertices])
eye=world_vertices('Oog -1'); eye_centre=eye.mean(axis=0); eye_diameter=float(eye[:,0].max()-eye[:,0].min())
skin_pts=np.array([tuple(v.co) for v in body.data.vertices])
head_width=float(2*np.abs(skin_pts[skin_pts[:,2]>1.70,1]).max())
nape=skin_pts[(np.abs(skin_pts[:,2]-eye_centre[2])<.012)&(np.abs(skin_pts[:,1])<.03)]
head_back_x=float(nape[:,0].max())
upper=world_vertices('Snavel bovensnavel'); lower=world_vertices('Snavel ondersnavel'); cere=world_vertices('Washuid boven de snavelbasis')
tip=BEAK_TIP; front_s=next(s/1000 for s in range(0,1000) if cere_thickness(s/1000)<=0)
def xz(p): return Vector((p[0],p[2]))
def span(points,axis_vec,s):
    # Doorsnede loodrecht op de snavelas bij fractie s: breedte en hoogte van de
    # punten binnen een dunne plak.
    centre=axis_point(s); d=(points-np.array(centre))@np.array(AXIS)
    sl=points[np.abs(d)<.004]
    if len(sl)==0: return 0.,0.
    return float(sl[:,1].max()-sl[:,1].min()),float(((sl-np.array(centre))@np.array(UP)).max()-((sl-np.array(centre))@np.array(UP)).min())
bill_w_front,_=span(upper,AXIS,front_s)
_,upper_h_front=span(upper,AXIS,front_s); _,lower_h_front=span(lower,AXIS,front_s)
cere_w,_=span(cere,AXIS,.05)
anatomy={
    'eye_centre':[round(float(c),4) for c in eye_centre],'eye_diameter':round(eye_diameter,4),
    'head_max_width':round(head_width,4),'head_length_featherline_to_nape':round(head_back_x-FEATHER.x,4),
    'feather_line':[round(FEATHER.x,4),round(FEATHER.z,4)],'beak_tip':[round(tip.x,4),round(tip.z,4)],
    'culmen_exposed':round((xz(tip)-xz(FEATHER)).length,4),'eye_to_tip':round((xz(tip)-xz(eye_centre)).length,4),
    'cere_length_along_culmen':round(front_s*BEAK_L,4),'cere_width_visible':round(cere_w,4),
    'cere_height_above_culmen':round(CERE_T*1.65,4),
    'bill_width_at_cere_front':round(bill_w_front,4),'bill_depth_at_cere_front':round(upper_h_front+lower_h_front,4),
    'beak_pitch_deg':34}
anatomy['culmen_over_eye_to_tip']=round(anatomy['culmen_exposed']/anatomy['eye_to_tip'],3)
anatomy['culmen_over_head_length']=round(anatomy['culmen_exposed']/anatomy['head_length_featherline_to_nape'],3)
anatomy['cere_over_culmen']=round(anatomy['cere_length_along_culmen']/anatomy['culmen_exposed'],3)
anatomy['cere_width_over_head_width']=round(cere_w/head_width,3)
anatomy['bill_width_over_head_width']=round(bill_w_front/head_width,3)
anatomy['bill_depth_over_culmen']=round(anatomy['bill_depth_at_cere_front']/anatomy['culmen_exposed'],3)
rig_summary={'bones':[b.name for b in rig.data.bones],'neck_joints_xz':NECK_JOINTS,'hip_pivot_xz':HIP,'neck_profile':NECK_PROFILE,'max_local_rotation_deg':bone_extremes}
stats={'iteration':ITERATION,'skin_self_intersections_source':self_intersections,'source':SOURCE,'license':'CC BY-SA 3.0','head_anatomy':anatomy,'rig':rig_summary,
    'clips':{'duration_seconds':DURATION,'fps':30,'strip_moments_seconds':STRIP_MOMENTS,
             'peck_contact_seconds':[1.02,1.08,1.40,1.46],'peck_ground_z':GROUND},
    'neck_widening':NECK_WIDEN}
if not MEASURE_ONLY or os.path.exists(glb):
    with open(glb,'rb') as f:
        f.read(12); length,kind=struct.unpack('<II',f.read(8)); data=json.loads(f.read(length))
    animations={a['name']:len(a['channels']) for a in data.get('animations',[])}
    stats['peaks']={a['name']:a.get('extras',{}).get('peaks') for a in data.get('animations',[])}
    assert set(animations)=={'Peck','WingWave'},animations
    stats.update({'bytes':os.path.getsize(glb),'sha256':hashlib.sha256(open(glb,'rb').read()).hexdigest(),'meshes':len(data['meshes']),
        'triangles':sum(data['accessors'][p['indices']]['count']//3 for m in data['meshes'] for p in m['primitives']),
        'images':len(data.get('images',[])),'animations':animations,'extensions':data.get('extensionsUsed',[]),'buffers':len(data['buffers'])})
with open(os.path.join(OUT,'model-evidence.json'),'w') as f:json.dump(stats,f,indent=2)
print('MODEL_V4_EXPORTED',json.dumps({k:stats[k] for k in stats if k in ('bytes','sha256','meshes','triangles','images')}),json.dumps(anatomy))
