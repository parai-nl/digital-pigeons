"""Rendert één duif voor de etalagekaart. Draait binnen Blender:

    blender -b --factory-startup -P blender_render.py -- job.json

`build.py` schrijft de job en roept dit script aan; zie README.md. Het model is
de GLB van de module (of v4 per duif). De gebaren zijn de gebakken acties uit
die GLB, onveranderd afgespeeld op snelheid 1,0; alleen de camera beweegt mee
(een korte aanloop naar de kijker toe en weer terug), zodat de duif bij een
reactie verder uit de kaart komt en precies op het rustbeeld eindigt.

Per beeld komen er twee lagen uit Cycles: `bird` (de duif, transparante film)
en `shadow` (alleen haar schaduw op een onzichtbare grond). build.py legt de
schaduw binnen de kaartrand en de duif erover.
"""
import bpy, json, math, os, shutil, sys
import numpy as np
from mathutils import Matrix, Vector
from bpy_extras.object_utils import world_to_camera_view

job = json.load(open(sys.argv[sys.argv.index('--') + 1]))
OUT = job['out']
os.makedirs(OUT, exist_ok=True)
FPS = job['fps']

# ---------------------------------------------------------------- scène
bpy.ops.wm.read_factory_settings(use_empty=True)
scene = bpy.context.scene
scene.render.fps, scene.render.fps_base = FPS, 1
# De importer rekent keyframetijden om met de fps van de scène: bij 30 fps
# loopt een gebaar van 3,2 s van beeld 0 tot en met beeld 96.
bpy.ops.import_scene.gltf(filepath=job['glb'])

root = next(o for o in scene.objects if o.parent is None and o.type == 'EMPTY' and any(c.type == 'ARMATURE' for c in o.children_recursive))
keep = {root, *root.children_recursive}
for o in list(scene.objects):
    if o not in keep:  # o.a. de Icosphere die de importer als botvorm aanmaakt
        bpy.data.objects.remove(o, do_unlink=True)
arm = next(o for o in keep if o.type == 'ARMATURE')
if arm.animation_data is None:
    arm.animation_data_create()
for track in arm.animation_data.nla_tracks:
    track.mute = True
meshes = [o for o in keep if o.type == 'MESH']
# De vleugelveren werpen op het lijf een harde schaduwlijn langs de vleugelrand
# (bij frontaal en driekwart goed te zien); zonder die schaduw blijft het lijf rond.
for o in meshes:
    if o.name.startswith('Wing'):
        o.visible_shadow = False

bird_col = bpy.data.collections.new('Duif')
scene.collection.children.link(bird_col)
for o in keep:
    for c in list(o.users_collection):
        c.objects.unlink(o)
    bird_col.objects.link(o)


def set_gesture(action_name):
    arm.animation_data.action = bpy.data.actions[action_name] if action_name else None
    if action_name and hasattr(arm.animation_data, 'action_slot') and arm.animation_data.action_slot is None:
        arm.animation_data.action_slot = bpy.data.actions[action_name].slots[0]


def points(frame=0):
    """Alle vervormde hoekpunten van de duif in wereldcoördinaten."""
    scene.frame_set(frame)
    dg = bpy.context.evaluated_depsgraph_get()
    out = []
    for o in meshes:
        e = o.evaluated_get(dg)
        m = e.to_mesh()
        co = np.empty(len(m.vertices) * 3, dtype=np.float64)
        m.vertices.foreach_get('co', co)
        co = co.reshape(-1, 3)
        mw = np.array([list(row) for row in e.matrix_world], dtype=np.float64)
        with np.errstate(all='ignore'):   # Accelerate meldt soms valse fouten
            out.append(co @ mw[:3, :3].T + mw[:3, 3])
        e.to_mesh_clear()
    return np.concatenate(out)


set_gesture(None)
rest = points(0)
lo, hi = rest.min(axis=0), rest.max(axis=0)
ground = float(lo[2])
centre = Vector(((lo[0] + hi[0]) / 2, (lo[1] + hi[1]) / 2, (lo[2] + hi[2]) / 2))
anchor = Vector((centre.x, centre.y, ground))     # grondpunt onder de duif
height = float(hi[2] - lo[2])

# ---------------------------------------------------------------- camera
card = job['card']
Wc = card['px']
Hc = Wc / card['aspect']
pad = job['pad']
RW = int(round(Wc * (pad['left'] + 1 + pad['right']) / 2)) * 2
RH = int(round(Hc * (pad['top'] + 1 + pad['bottom']) / 2)) * 2
card_x0, card_y0 = Wc * pad['left'], Hc * pad['top']   # kaart in renderpixels, y van boven

cam_data = bpy.data.cameras.new('Camera')
cam_data.lens = job['view']['focalMm']
cam_data.sensor_width = 36
cam_data.sensor_fit = 'AUTO'
cam = bpy.data.objects.new('Camera', cam_data)
scene.collection.objects.link(cam)
scene.camera = cam
scene.render.resolution_x, scene.render.resolution_y = RW, RH
scene.render.resolution_percentage = 100


def place(azimuth_deg, elevation_deg, distance):
    """Camera rond het midden van de duif. Azimut 0 = recht van voren (de
    duif kijkt naar −X), positief = naar haar rechterflank, zodat de kop in
    beeld naar rechts wijst."""
    az, el = math.radians(azimuth_deg), math.radians(elevation_deg)
    pos = centre + Vector((-math.cos(el) * math.cos(az), math.cos(el) * math.sin(az), math.sin(el))) * distance
    cam.location = pos
    cam.rotation_euler = (centre - pos).to_track_quat('-Z', 'Y').to_euler()
    bpy.context.view_layer.update()


def project(pts):
    """Renderpixels (x naar rechts, y naar beneden) van wereldpunten."""
    mw = cam.matrix_world.normalized().inverted()
    out = []
    for p in pts:
        v = world_to_camera_view(scene, cam, Vector(p))
        out.append((v.x * RW, (1 - v.y) * RH))
    return np.array(out)


def hull_sample(pts, n=6000):
    step = max(1, len(pts) // n)
    return pts[::step]


view = job['view']
rest_sample = hull_sample(rest)
target = job['rest']   # top/bottom/centerX in fracties van de kaart
want_top, want_bottom = card_y0 + target['top'] * Hc, card_y0 + target['bottom'] * Hc
want_cx = card_x0 + target['centerX'] * Wc
distance = height * cam_data.lens / 36 * 1.2
cam_data.shift_x = cam_data.shift_y = 0
for _ in range(8):
    place(view['azimuthDeg'], view['elevationDeg'], distance)
    xy = project(rest_sample)
    top, bottom = xy[:, 1].min(), xy[:, 1].max()
    distance *= (bottom - top) / (want_bottom - want_top)
place(view['azimuthDeg'], view['elevationDeg'], distance)
for _ in range(6):
    xy = project(rest_sample)
    cx = (xy[:, 0].min() + xy[:, 0].max()) / 2
    top = xy[:, 1].min()
    cam_data.shift_x += (cx - want_cx) / max(RW, RH)
    cam_data.shift_y -= (top - want_top) / max(RW, RH)
    bpy.context.view_layer.update()
rest_cam = dict(distance=distance, shift_x=cam_data.shift_x, shift_y=cam_data.shift_y)
rest_loc, rest_quat = cam.location.copy(), cam.rotation_euler.to_quaternion()
anchor_px = project([anchor])[0]


def smooth(x):
    x = min(1.0, max(0.0, x))
    return x * x * x * (x * (x * 6 - 15) + 10)


def reach_at(t, duration):
    """0 in rust, 1 op het verste punt; begint en eindigt met snelheid nul."""
    r = job['reaction']
    return smooth(t / r['inSeconds']) * (1 - smooth((t - (duration - r['outSeconds'])) / r['outSeconds']))


def pose_camera(u):
    """Een stap van de duif naar de kijker. De camera schuift op dezelfde hoogte
    over de grond naar haar toe en houdt haar kijkrichting (geen herrichten, dus
    geen zoom rond een vast punt), draait een paar graden om haar heen, en de
    lensverschuiving zet het grondpunt `footDrop` kaarthoogte lager: de poten
    komen iets naar voren, de kop verder boven de kaart uit."""
    r = job['reaction']
    yaw = Matrix.Rotation(math.radians(r['orbitDeg'] * u), 4, 'Z')
    pivot = Vector((centre.x, centre.y, 0))
    flat = Vector((centre.x - rest_loc.x, centre.y - rest_loc.y, 0))
    loc = rest_loc + flat * (r['reach'] * u)
    cam.location = pivot + yaw @ (loc - pivot)
    cam.rotation_euler = (yaw.to_quaternion() @ rest_quat).to_euler()
    cam_data.shift_x, cam_data.shift_y = rest_cam['shift_x'], rest_cam['shift_y']
    bpy.context.view_layer.update()
    want = (anchor_px[0], anchor_px[1] + r.get('footDrop', 0) * Hc * u)
    for _ in range(3):
        p = project([anchor])[0]
        cam_data.shift_x += (p[0] - want[0]) / max(RW, RH)
        cam_data.shift_y -= (p[1] - want[1]) / max(RW, RH)
        bpy.context.view_layer.update()
    return [float(v) for v in project([anchor])[0]]


pose_camera(0)

# ---------------------------------------------------------------- licht
light = job['light']
world = bpy.data.worlds.new('Plaatlicht')
scene.world = world
nt = world.node_tree
nt.nodes.clear()
coord = nt.nodes.new('ShaderNodeTexCoord')
sep = nt.nodes.new('ShaderNodeSeparateXYZ')
ramp = nt.nodes.new('ShaderNodeMapRange')
mix = nt.nodes.new('ShaderNodeMix')
mix.data_type = 'RGBA'
bg = nt.nodes.new('ShaderNodeBackground')
outn = nt.nodes.new('ShaderNodeOutputWorld')
ramp.inputs['From Min'].default_value, ramp.inputs['From Max'].default_value = -0.25, 0.55
nt.links.new(coord.outputs['Generated'], sep.inputs[0])
nt.links.new(sep.outputs['Z'], ramp.inputs['Value'])
nt.links.new(ramp.outputs['Result'], mix.inputs['Factor'])
mix.inputs['A'].default_value = (*light['ground'], 1)
mix.inputs['B'].default_value = (*light['sky'], 1)
nt.links.new(mix.outputs['Result'], bg.inputs['Color'])
bg.inputs['Strength'].default_value = light['worldStrength']
nt.links.new(bg.outputs['Background'], outn.inputs['Surface'])


def area(name, az_deg, el_deg, dist, size, power, colour):
    """Lamp op azimut/elevatie t.o.v. de kijkrichting van de rustcamera."""
    d = light_basis(az_deg, el_deg)
    data = bpy.data.lights.new(name, 'AREA')
    data.shape, data.size, data.size_y = 'RECTANGLE', size, size * 0.7
    data.energy, data.color = power, colour
    ob = bpy.data.objects.new(name, data)
    scene.collection.objects.link(ob)
    ob.location = centre + d * dist
    ob.rotation_euler = (-d).to_track_quat('-Z', 'Y').to_euler()
    return ob


def light_basis(az_deg, el_deg):
    forward = (centre - cam.location).normalized()          # camera → duif
    flat = Vector((forward.x, forward.y, 0)).normalized()
    towards_cam = -flat
    right = flat.cross(Vector((0, 0, 1))).normalized()      # rechts in beeld
    az, el = math.radians(az_deg), math.radians(el_deg)
    return (towards_cam * math.cos(az) + right * math.sin(az)) * math.cos(el) + Vector((0, 0, math.sin(el)))


s = height
area('Hoofdlicht', light['key']['az'], light['key']['el'], s * 3, s * light['key']['size'], light['key']['power'], light['key']['colour'])
rim = area('Randlicht', light['rim']['az'], light['rim']['el'], s * 3, s * light['rim']['size'], light['rim']['power'], light['rim']['colour'])
# Het randlicht tekent alleen de rand; een slagschaduw van achteren naar de
# kijker toe past niet bij bewolkt daglicht en schoof als band over de kaart.
rim.data.use_shadow = False

# Onzichtbare grond die alleen schaduw vangt.
bpy.ops.mesh.primitive_plane_add(size=height * 12, location=(anchor.x, anchor.y, ground))
floor = bpy.context.object
floor.name = 'Grond'
floor.is_shadow_catcher = True
floor_col = bpy.data.collections.new('Grond')
scene.collection.children.link(floor_col)
for c in list(floor.users_collection):
    c.objects.unlink(floor)
floor_col.objects.link(floor)

# ---------------------------------------------------------------- render
scene.render.engine = 'CYCLES'
prefs = bpy.context.preferences.addons['cycles'].preferences
try:
    prefs.compute_device_type = 'METAL'
    prefs.get_devices()
    gpus = [d for d in prefs.devices if d.type == 'METAL']
    for d in prefs.devices:
        d.use = d.type == 'METAL'
    scene.cycles.device = 'GPU' if gpus else 'CPU'
except Exception:
    scene.cycles.device = 'CPU'
q = job['quality']
scene.cycles.samples = q['samples']
scene.cycles.use_adaptive_sampling = True
scene.cycles.adaptive_threshold = q['noise']
scene.cycles.use_denoising = True
scene.cycles.denoiser = 'OPENIMAGEDENOISE'
scene.cycles.denoising_use_gpu = True
print('ETALAGE-DEVICE', scene.cycles.device, [d.name for d in prefs.devices if d.use])
scene.cycles.seed = 7
scene.cycles.use_animated_seed = False
scene.cycles.pixel_filter_type = 'BLACKMAN_HARRIS'
scene.cycles.filter_width = 1.5
scene.render.film_transparent = True
scene.render.use_persistent_data = q.get('persistent', False)
scene.view_settings.view_transform = 'AgX'
scene.view_settings.look = light.get('look', 'None')
scene.view_settings.exposure = light.get('exposure', 0)
scene.render.image_settings.file_format = 'PNG'
scene.render.image_settings.color_mode = 'RGBA'
scene.render.image_settings.color_depth = '8'

vl_bird = scene.view_layers[0]
vl_bird.name = 'bird'
vl_bird.layer_collection.children['Grond'].exclude = True
vl_shadow = scene.view_layers.new('shadow')
vl_shadow.layer_collection.children['Duif'].indirect_only = True
vl_shadow.samples = q['shadowSamples']
# Het gebaar en de camera bewegen samen; bewegingsonscherpte zoals een camera.
scene.render.use_motion_blur = False
scene.render.motion_blur_shutter = q.get('shutter', 0.5)

# Compositor: beide lagen als losse PNG's per beeld.
cnt = scene.node_tree if hasattr(scene, 'node_tree') and scene.node_tree else None
if cnt is None:   # Blender 5: compositor als node group
    group = bpy.data.node_groups.new('Etalage', 'CompositorNodeTree')
    scene.compositing_node_group = group
    cnt = group
cnt.nodes.clear()
file_out = cnt.nodes.new('CompositorNodeOutputFile')


def configure_output(directory):
    """Blender 5 schrijft per invoer een bestand <map>/<naam><beeld>.png."""
    if hasattr(file_out, 'directory'):
        file_out.directory = directory
        file_out.file_name = ''
    else:
        file_out.base_path = directory
    fmt = file_out.format
    if hasattr(fmt, 'media_type'):
        fmt.media_type = 'IMAGE'
    fmt.file_format, fmt.color_mode, fmt.color_depth = 'PNG', 'RGBA', '8'


items = file_out.file_output_items if hasattr(file_out, 'file_output_items') else None
for name in ('bird', 'shadow'):
    rl = cnt.nodes.new('CompositorNodeRLayers')
    rl.layer = name
    if items is not None:
        items.new('RGBA', name + '_')
        sock = file_out.inputs[name + '_']
    else:
        file_out.file_slots.new(name + '_')
        sock = file_out.inputs[name + '_']
    cnt.links.new(rl.outputs['Image'], sock)
# Blender wil een gewone uitvoer; die schrijven we naar een wegwerpmap.
scene.render.filepath = os.path.join(OUT, '_discard', '')


def render_frames(directory, start, end, step=1):
    """Animatierender, zodat de File Output-node beeldnummers schrijft."""
    configure_output(directory)
    scene.frame_start, scene.frame_end, scene.frame_step = start, end, step
    bpy.ops.render.render(animation=True)


meta = {
    'render': [RW, RH], 'card': [card_x0, card_y0, Wc, Hc], 'anchor': [float(anchor_px[0]), float(anchor_px[1])],
    'restCamera': rest_cam, 'ground': ground, 'height': height, 'clips': {},
}

jobs = job['do']
if 'rest' in jobs:
    set_gesture(None)
    pose_camera(0)
    scene.render.use_motion_blur = False
    render_frames(os.path.join(OUT, 'rest'), 0, 0)

for kind, action in job['gestures'].items():
    if kind not in jobs:
        continue
    set_gesture(action)
    act = bpy.data.actions[action]
    last = int(round(act.frame_range[1]))            # 96 bij 30 fps
    duration = last / FPS
    # Keyframe de camera per beeld, zodat bewegingsonscherpte ook haar ziet.
    cam.animation_data_clear(); cam_data.animation_data_clear()
    anchors = []
    for f in range(0, last + 1):
        anchors.append(pose_camera(reach_at(f / FPS, duration)))
        cam.keyframe_insert('location', frame=f)
        cam.keyframe_insert('rotation_euler', frame=f)
        cam_data.keyframe_insert('shift_x', frame=f)
        cam_data.keyframe_insert('shift_y', frame=f)
    scene.render.use_motion_blur = q.get('motionBlur', True)
    render_frames(os.path.join(OUT, kind), 1, last - 1, job.get('frameStep', 1))
    meta['clips'][kind] = {'frames': last + 1, 'ms': round(duration * 1000), 'action': action, 'anchors': anchors}
    cam.animation_data_clear(); cam_data.animation_data_clear()
    pose_camera(0)

if 'head' in jobs:
    # Avatar: dezelfde kijkhoek, kop groot in beeld, geen schaduw.
    set_gesture(None)
    scene.frame_set(0)
    scene.render.use_motion_blur = False
    head = job['head']
    pts = rest[rest[:, 2] > ground + height * (1 - head.get('share', 0.22))]   # de kop: bovenste ~22 %
    # Kop iets boven het midden, zodat de hals doorloopt tot de onderrand.
    hc = Vector(((pts.min(axis=0) + pts.max(axis=0)) / 2)) - Vector((0, 0, float(np.ptp(pts[:, 2])) * 0.22))
    hlo, hhi = pts.min(axis=0), pts.max(axis=0)
    az, el = math.radians(view['azimuthDeg'] + head.get('azimuthDeg', 0)), math.radians(view['elevationDeg'])
    hsize = float(max(hhi - hlo))
    d = hsize * cam_data.lens / 36 * head['distance']
    cam.location = hc + Vector((-math.cos(el) * math.cos(az), math.cos(el) * math.sin(az), math.sin(el))) * d
    cam.rotation_euler = (hc - cam.location).to_track_quat('-Z', 'Y').to_euler()
    cam_data.shift_x = cam_data.shift_y = 0
    scene.render.resolution_x = scene.render.resolution_y = head['px']
    vl_shadow.use = False
    render_frames(os.path.join(OUT, 'head'), 0, 0)
    vl_shadow.use = True

shutil.rmtree(os.path.join(OUT, '_discard'), ignore_errors=True)
# Bij een gedeeltelijke render (bv. alleen de kop) blijven eerdere clipgegevens staan.
mp = os.path.join(OUT, 'meta.json')
if os.path.exists(mp):
    old = json.load(open(mp))
    meta['clips'] = {**old.get('clips', {}), **meta['clips']}
json.dump(meta, open(mp, 'w'), indent=1)
print('ETALAGE-RENDER-OK', json.dumps(meta))
