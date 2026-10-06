"""Drie verschijningen van één duif, als tweede stap op een basis-.blend.

De stap leest een willekeurige basis met de duivenrig (armature, doorlopende
huid, clips) en maakt daaruit drie zelfstandige GLB's:

- signalerend: de basis ongewijzigd;
- individualistisch: dezelfde duif met een stropdas (knoop, blad, halsband)
  die via de gewichten van de huid meebeweegt;
- biologisch: dezelfde duif in een donkerder, natuurlijk leigrijs verenkleed.

Daarnaast legt de stap per duif de vergadercamera vast (camera-views.json, in
de conventie van pigeonFraming.ts) en rendert rustposter, kaartportret,
kopportret en framestroken uit de opnieuw geïmporteerde GLB.

De basis wordt nooit overschreven: elke variant opent haar opnieuw en schrijft
alleen naar --out. Gebruik:

  Blender --background <basis.blend> --python tools/design-model/variants_v4.py -- --out <map>

Opties: --variants signalerend,biologisch,individualistisch  --samples 64
--no-render (alleen GLB's en metingen)  --look (snelle stropdasstudie).
"""
import bpy, bmesh, math, os, sys, json, struct, hashlib, time
import numpy as np
from mathutils import Vector, Matrix, Quaternion
from mathutils.bvhtree import BVHTree

ARGS = sys.argv[sys.argv.index('--') + 1:] if '--' in sys.argv else []
def option(name, default=None):
    return ARGS[ARGS.index(name) + 1] if name in ARGS else default
OUT = os.path.abspath(option('--out') or sys.exit('geef --out <map voor de varianten>'))
ALIASES = {'signalerend': 'signalerend', 'nonverbal': 'signalerend', 'biologisch': 'biologisch', 'biological': 'biologisch',
           'individualistisch': 'individualistisch', 'individualist': 'individualistisch'}
VARIANTS = []
for _name in option('--variants', 'signalerend,biologisch,individualistisch').split(','):
    if _name.strip() not in ALIASES:
        raise SystemExit(f'Onbekende variant {_name!r}; kies uit {sorted(ALIASES)}')
    if ALIASES[_name.strip()] not in VARIANTS: VARIANTS.append(ALIASES[_name.strip()])
SAMPLES = int(option('--samples', '64'))
RENDER = '--no-render' not in ARGS
LOOK = '--look' in ARGS
# Kleurslag van de Kennispartner: 'donker' (standaard, besluit root) of 'checker'
# (een egaal donkerder leigrijs met diepere banden, zonder blokpatroon).
MORPH = option('--kennis', 'donker')
if MORPH not in ('checker', 'donker'): raise SystemExit(f'Onbekende kleurslag {MORPH!r}; kies checker of donker')
BASIS = bpy.data.filepath
os.makedirs(OUT, exist_ok=True)


# ---------------------------------------------------------------------------
# De basis herkennen zonder op hals- of kopbotnamen te leunen.

def find_rig():
    rigs = [o for o in bpy.data.objects if o.type == 'ARMATURE']
    assert len(rigs) == 1, [o.name for o in rigs]
    return rigs[0]

def find_root(rig):
    root = rig
    while root.parent: root = root.parent
    return root

def find_body(rig):
    # De doorlopende huid is het grootste mesh dat door de rig vervormd wordt
    # en meer dan één botgroep draagt; de schubbenpoot is groter maar statisch.
    named = bpy.data.objects.get('Duif doorlopende huid')
    if named: return named
    skinned = [o for o in bpy.data.objects if o.type == 'MESH' and len(o.vertex_groups) > 1
               and any(m.type == 'ARMATURE' and m.object == rig for m in o.modifiers)]
    return max(skinned, key=lambda o: len(o.data.vertices))

def rest_mesh(ob):
    """Wereldcoördinaten van het vervormde mesh in de bind-pose."""
    dg = bpy.context.evaluated_depsgraph_get()
    ev = ob.evaluated_get(dg); me = ev.to_mesh()
    pts = np.empty(len(me.vertices) * 3, np.float32); me.vertices.foreach_get('co', pts)
    pts = pts.reshape(-1, 3); m = np.array(ev.matrix_world)
    pts = pts @ m[:3, :3].T + m[:3, 3]
    polys = [tuple(p.vertices) for p in me.polygons]
    ev.to_mesh_clear()
    return pts, polys

def with_rest_pose(rig, fn):
    previous = rig.data.pose_position; rig.data.pose_position = 'REST'
    bpy.context.view_layer.update()
    try: return fn()
    finally: rig.data.pose_position = previous; bpy.context.view_layer.update()


# ---------------------------------------------------------------------------
# Het lijfoppervlak als maat: een stropdasvertex heet (u, z, diepte), waarbij
# (u, z) een punt op de voorkant van de duif kiest en diepte de afstand langs
# de gladde normaal naar buiten is. Zo volgt de stof de bolling van borst en
# hals, ongeacht hoe het basismodel er precies uitziet.

class Surface:
    def __init__(self, points, polys):
        self.points = points
        self.bvh = BVHTree.FromPolygons([Vector(p) for p in points], polys)
        # Gladde vertexnormalen, zodat een verschuiving langs de normaal geen
        # facetten in de stof drukt.
        normals = np.zeros_like(points)
        for poly in polys:
            a, b, c = (points[i] for i in poly[:3])
            n = np.cross(b - a, c - a)
            for i in poly: normals[i] += n
        self.normals = normals / np.maximum(np.linalg.norm(normals, axis=1, keepdims=True), 1e-9)
        self.polys = polys
        # Buitenkant controleren: een punt ver voor de duif ligt aan de positieve kant.
        probe = Vector((-4, 0, 1.2)); loc, n, i, d = self.bvh.find_nearest(probe)
        if (probe - loc).dot(self.smooth_normal(loc, i)) < 0: self.normals *= -1
        # Voor binnen/buiten telt de vlaknormaal van het dichtstbijzijnde vlak:
        # bij een scherpe rand (knoopzijde) kan een gemiddelde normaal omklappen.
        self.face_sign = 1 if (probe - loc).dot(n) > 0 else -1

    def smooth_normal(self, loc, index):
        poly = self.polys[index]; pts = self.points[list(poly)]
        w = 1 / np.maximum(np.linalg.norm(pts - np.array(loc), axis=1), 1e-6)
        return Vector((self.normals[list(poly)] * w[:, None]).sum(0)).normalized()

    def front(self, y, z):
        """Treffer op de voorkant (−X) op hoogte z en zijdelingse y."""
        loc, n, i, d = self.bvh.ray_cast(Vector((-4, y, z)), Vector((1, 0, 0)))
        assert loc is not None, (y, z)
        return loc, self.smooth_normal(loc, i)

    def around(self, center, axis, forward, angle):
        """Treffer rond een as (hals): straal van buiten naar het midden."""
        side = axis.cross(forward).normalized()
        direction = (forward * math.cos(angle) + side * math.sin(angle)).normalized()
        origin = center + direction * 3
        loc, n, i, d = self.bvh.ray_cast(origin, -direction)
        assert loc is not None, angle
        return loc, self.smooth_normal(loc, i)

    def depth_inside(self, points, reach=.03):
        """Diepte van punten binnen een gesloten volume (0 = erbuiten).

        Pariteit van straalsnijdingen, dus ook bij scherpe randen betrouwbaar;
        alleen punten binnen `reach` van het oppervlak kunnen erin liggen.
        """
        out = np.zeros(len(points)); direction = Vector((.31, .23, .92)).normalized()
        for k, p in enumerate(points):
            loc, n, i, d = self.bvh.find_nearest(Vector(p), reach)
            if loc is None: continue
            origin = Vector(p); hits = 0
            while True:
                hit = self.bvh.ray_cast(origin, direction)
                if hit[0] is None: break
                hits += 1; origin = hit[0] + direction * 1e-6
            if hits % 2: out[k] = d
        return out

    def signed(self, points):
        """Getekende afstand tot het oppervlak (positief = buiten)."""
        out = np.empty(len(points))
        for k, p in enumerate(points):
            loc, n, i, d = self.bvh.find_nearest(Vector(p))
            out[k] = d if (Vector(p) - loc).dot(n) * self.face_sign >= 0 else -d
        return out


# ---------------------------------------------------------------------------
# Stropdas. Maten in modeleenheden; de duif is ~2,07 hoog, 1 eenheid ≈ 12 cm.

TIE = dict(
    knot_top=1.64,         # standaard; place_tie() zet de knoop onder de kin
    knot_height=.116,
    knot_top_width=.140,
    knot_bottom_width=.074,
    knot_depth=.062,       # hoe ver de knoop van de keel af staat
    wrap_thickness=.009,   # tweede stoflaag over de knoop: de wikkelrand vangt schaduw
    blade_start_width=.074,
    blade_width=.148,      # ongeveer een derde van de borstbreedte
    blade_length=.74,      # langs het oppervlak, knooponderkant tot punt
    thickness=.0068,
    gap=.0045,             # stof ligt op de veren, niet erin
    hang=.20,              # onder de borstbolling hangt het blad licht vrij
    dimple=.013,           # hoe ver de bladranden onder de knoop opbollen
    band_width=.042,
    band_thickness=.0060,
    band_gap=.0035,
    band_visible=52,       # graden naast de knoop waarin de band zichtbaar is
    chin_gap=.055,         # ruimte tussen kin en bovenkant knoop
    stripe_period=.070,
)
BLADE_JOINTS = (.08, .18, .29, .41, .54)   # booglengte van de scharnieren in het blad
SEGMENTS = len(BLADE_JOINTS) + 1

def rounded_ring(half_width, back, front, steps=12, edge=4):
    """Doorsnede van platte stof met afgeronde randen, als (lateraal, diepte)."""
    ring = []
    thick = front - back; r = thick / 2; mid = (front + back) / 2
    core = max(half_width - r, 1e-4)
    for i in range(steps + 1):                       # achterkant, links → rechts
        ring.append((-core + 2 * core * i / steps, back))
    for i in range(1, edge):                        # rechterrand
        a = -math.pi / 2 + math.pi * i / edge
        ring.append((core + r * math.cos(a), mid + r * math.sin(a)))
    for i in range(steps + 1):                       # voorkant, rechts → links
        ring.append((core - 2 * core * i / steps, front))
    for i in range(1, edge):                        # linkerrand
        a = math.pi / 2 + math.pi * i / edge
        ring.append((-core + r * math.cos(a), mid + r * math.sin(a)))
    return ring

def dome_ring(half_width, depth, back_steps=12, front_steps=20):
    """Doorsnede van de knoop: vlakke achterkant op de hals, bolle voorkant."""
    ring = [(-half_width + 2 * half_width * i / back_steps, 0.) for i in range(back_steps + 1)]
    for i in range(1, front_steps):
        phi = math.pi * i / front_steps
        ring.append((half_width * math.cos(phi), depth * math.sin(phi) ** .75))
    return ring

def loft_mesh(name, rings, uvs, material, cap=True):
    steps = len(rings[0]); verts = [p for ring in rings for p in ring]; faces = []
    for j in range(len(rings) - 1):
        for i in range(steps):
            a = j * steps + i; b = j * steps + (i + 1) % steps
            faces.append((a, b, b + steps, a + steps))
    if cap:
        faces.append(tuple(reversed(range(steps))))
        faces.append(tuple(range((len(rings) - 1) * steps, len(rings) * steps)))
    data = bpy.data.meshes.new(name); data.from_pydata([tuple(v) for v in verts], [], faces); data.update()
    bm = bmesh.new(); bm.from_mesh(data); bmesh.ops.recalc_face_normals(bm, faces=bm.faces); bm.to_mesh(data); bm.free()
    ob = bpy.data.objects.new(name, data); bpy.context.scene.collection.objects.link(ob)
    data.materials.append(material)
    layer = data.uv_layers.new(name='UVMap'); flat = [uv for ring in uvs for uv in ring]
    for poly in data.polygons:
        poly.use_smooth = True
        for li in poly.loop_indices: layer.data[li].uv = flat[data.loops[li].vertex_index]
    return ob

def smoothstep(a, b, x):
    t = min(1, max(0, (x - a) / (b - a))); return t * t * (3 - 2 * t)

def blade_bone_weights(arc):
    """Gewichten van een bladvertex over de botketen, zacht over de scharnieren."""
    weights = {0: 1.}
    for i, joint in enumerate(BLADE_JOINTS):
        share = smoothstep(joint - .05, joint + .05, arc)
        if share <= 0: break
        weights = {k: v * (1 - share) for k, v in weights.items()}; weights[i + 1] = weights.get(i + 1, 0) + share
    return {f'{TIE_BLADE_BONE} {k}': v for k, v in weights.items() if v > 1e-4}

def build_tie(surface, material, t=TIE):
    """Knoop, blad en halsband als gesloten, afgeronde stofvolumes.

    Geeft per deel ook een rol per vertex (voor de meting) en per vertex de
    gewichten naar de eigen stropdasbotten; wat daar niet heen gaat, volgt de
    huid eronder (zie transfer_weights).
    """
    parts = []; roles = {}; anchors = {}
    period = t['stripe_period']
    knot_bottom = t['knot_top'] - t['knot_height']
    anchor_z = t['knot_top'] - .55 * t['knot_height']
    _, knot_normal = surface.front(0, anchor_z)

    # --- Blad. Het bovenste stuk ligt onder de onderlip van de knoop. Direct
    # onder de knoop bollen de randen op en valt het midden in: het kuiltje.
    hidden = .030
    z = knot_bottom + hidden; stations = []; arc = -hidden; previous = None
    bulge_x = min(surface.front(0, zz)[0].x for zz in np.linspace(knot_bottom - t['blade_length'], knot_bottom, 60))
    while True:
        loc, n = surface.front(0, z)
        if previous is not None: arc += (loc - previous).length
        previous = loc; stations.append((arc, z))
        if arc >= t['blade_length']: break
        z -= .005 if arc < .12 else .012
    tip_length = t['blade_width'] / 2
    body_end = t['blade_length'] - tip_length
    def blade_width(s):
        if s <= 0: return t['blade_start_width'] * .9
        if s <= body_end:
            return t['blade_start_width'] + (t['blade_width'] - t['blade_start_width']) * (s / body_end) ** .55
        return max(.003, t['blade_width'] * (1 - (s - body_end) / tip_length))
    # Het blad volgt een gladgestreken borst, in het midden en in de breedte:
    # de huid heeft rimpels van de veertextuur die stijve stof niet volgt.
    kernel = np.ones(9) / 9
    def smooth(values):
        padded = np.concatenate([np.repeat(values[:1], 4, 0), values, np.repeat(values[-1:], 4, 0)])
        return np.stack([np.convolve(padded[:, k], kernel, 'valid') for k in range(values.shape[1])], axis=1)
    centers = [surface.front(0, z) for arc, z in stations]
    raw = np.array([tuple(c[0]) for c in centers]); normals = np.array([tuple(c[1]) for c in centers])
    smooth_centers = smooth(raw); smooth_normals = smooth(normals)
    smooth_normals /= np.linalg.norm(smooth_normals, axis=1, keepdims=True)
    profile = rounded_ring(.5, 0, t['thickness'], steps=10, edge=3)       # doorsnede op eenheidsbreedte
    lateral_offsets = []
    for k, (arc, z) in enumerate(stations):
        w = blade_width(arc); loc0 = Vector(raw[k])
        lateral_offsets.append([tuple(surface.front(lat * w, z)[0] - loc0) for lat, _ in profile])
    lateral_offsets = np.array(lateral_offsets)                            # stations × profiel × 3
    flat = lateral_offsets.reshape(len(stations), -1)
    lateral_offsets = smooth(flat).reshape(lateral_offsets.shape)
    rings = []; uvs = []; role = []; bones = []; reference = None; centerline = []
    for k, (arc, z) in enumerate(stations):
        w = blade_width(arc); ring = []; uv = []
        dimple = smoothstep(-.005, .015, arc) * (1 - smoothstep(.03, .10, arc))
        n0 = Vector(smooth_normals[k]); base = Vector(smooth_centers[k])
        hang = t['hang'] * max(0, base.x - bulge_x)
        # Het blad komt uit de knoop naar voren en zakt dan op de borst: vlak
        # onder de onderlip ligt het op de halve knoopdiepte, niet erachter.
        hang += .45 * t['knot_depth'] * (1 - smoothstep(-.01, .13, arc))
        centerline.append((arc, tuple(base + n0 * (t['gap'] + hang + t['thickness'] / 2))))
        if reference is None and arc >= .42: reference = tuple(base + n0 * (t['gap'] + hang))
        for i, (unit, depth) in enumerate(profile):
            lateral = unit * w; s = unit * 2
            front = depth > t['thickness'] / 2
            crown = .0016 * (1 - s * s) * front                    # licht bolle voorkant
            puff = .5 * t['dimple'] * dimple * min(1, s * s * 1.3)  # randen bollen iets op
            # Het midden valt in (het kuiltje) over de hele stofdikte: de stof
            # vouwt, de voor- en achterkant gaan niet door elkaar.
            crease = -t['dimple'] * dimple * math.exp(-(s / .22) ** 2)
            ring.append(base + Vector(lateral_offsets[k, i]) + n0 * (t['gap'] + hang + puff + depth + crown + crease))
            # De streep loopt over de gevouwen rand door, zoals bij een echte das.
            uv.append((lateral / period, arc / period))
            role.append('verborgen' if arc < 0 else 'blad-achter' if i <= 10 else 'blad')
            bones.append(blade_bone_weights(arc))
        rings.append(ring); uvs.append(uv)
    parts.append(loft_mesh('Stropdas blad', rings, uvs, material))
    roles['Stropdas blad'] = role; anchors['Stropdas blad'] = bones

    # --- Knoop (four-in-hand). Een omgekeerd trapezium dat van opzij een wig
    # is, met afgeronde bovenkant, iets scheef, en een onderlip die over het
    # blad valt. Daaroverheen de tweede laag: de wikkeling van het brede eind,
    # die schuin eindigt en met haar eigen dikte een schaduwrand geeft.
    shift_max = .008
    def knot_frame(h):
        z = t['knot_top'] - h * t['knot_height']
        dome = math.sqrt(max(0, 1 - (1 - min(h, .30) / .30) ** 2))
        close = math.sqrt(max(0, 1 - (max(h, .92) - .92) / .18 * .8))
        w = t['knot_top_width'] * (1 - min(h, 1)) ** .9 + t['knot_bottom_width'] * (1 - (1 - min(h, 1)) ** .9)
        w *= (.80 + .20 * dome)
        depth = t['knot_depth'] * (1 - .40 * min(h, 1) ** 1.3) * (.25 + .75 * dome) * close
        return z, w, depth, shift_max * h * h
    def knot_point(s, h, extra=0.):
        z, w, depth, shift = knot_frame(h)
        loc, n = surface.front(s * w / 2 + shift, z)
        return loc + knot_normal * (t['gap'] + depth * max(0, 1 - s * s) ** .375 + extra)
    rings = []; uvs = []; role = []
    count = 30
    for j in range(count + 1):
        h = 1.1 * j / count                                      # 0 boven, 1,1 = onderlip over het blad
        z, w, depth, shift = knot_frame(h)
        ring = []; uv = []
        for lateral, d in dome_ring(w / 2, depth):
            loc, n = surface.front(lateral + shift, z)
            ring.append(loc + knot_normal * (t['gap'] + d))
            # De stof loopt in de knoop dwars: de streep staat daar in de andere diagonaal.
            uv.append(((t['knot_height'] * h) / period + .31, -(lateral + shift) / period))
            role.append('knoop-achter' if len(ring) <= 13 else 'knoop')
        rings.append(ring); uvs.append(uv)
    knot = loft_mesh('Stropdas knoop', rings, uvs, material)
    # Wikkellaag: een raster over de voorkant van de knoop tot een schuine lijn,
    # met Solidify tot een gesloten laag van wrap_thickness dik.
    cols, rows = 24, 14; verts = []; wuv = []
    for r in range(rows + 1):
        for c in range(cols + 1):
            s = -1.04 + 2.08 * c / cols
            line = .60 - .16 * s                                 # schuine onderrand, links lager
            h = line * r / rows
            verts.append(tuple(knot_point(max(-1, min(1, s)), h, t['wrap_thickness'] + .0030 - .012 * max(0, abs(s) - 1))))
            # UV vlak op de knoop (werkelijke dwarspositie), niet naar de breedte
            # geschaald: anders waaiert de streep over de kop uit in een boog.
            _, kw, _, kshift = knot_frame(h)
            wuv.append(((t['knot_height'] * h) / period + .55, -(max(-1, min(1, s)) * kw / 2 + kshift) / period))
    faces = [(r * (cols + 1) + c, r * (cols + 1) + c + 1, (r + 1) * (cols + 1) + c + 1, (r + 1) * (cols + 1) + c) for r in range(rows) for c in range(cols)]
    data = bpy.data.meshes.new('Stropdas wikkel'); data.from_pydata(verts, [], faces); data.update()
    layer = data.uv_layers.new(name='UVMap')
    for poly in data.polygons:
        poly.use_smooth = True
        for li in poly.loop_indices: layer.data[li].uv = wuv[data.loops[li].vertex_index]
    shell = bpy.data.objects.new('Stropdas wikkel', data); bpy.context.scene.collection.objects.link(shell)
    data.materials.append(material)
    mod = shell.modifiers.new('Dikte', 'SOLIDIFY'); mod.thickness = t['wrap_thickness']; mod.offset = -1
    mod.use_rim = True; mod.use_even_offset = True
    dg = bpy.context.evaluated_depsgraph_get()
    solid = bpy.data.meshes.new_from_object(shell.evaluated_get(dg))
    bpy.data.objects.remove(shell, do_unlink=True)
    bm = bmesh.new(); bm.from_mesh(knot.data); bm.from_mesh(solid)
    bmesh.ops.recalc_face_normals(bm, faces=bm.faces); bm.to_mesh(knot.data); bm.free()
    for poly in knot.data.polygons: poly.use_smooth = True
    role += ['knoop'] * len(solid.vertices)
    parts.append(knot)
    roles['Stropdas knoop'] = role; anchors['Stropdas knoop'] = [{TIE_BONE: 1.}] * len(knot.data.vertices)

    # --- Halsband: een platte stofband die vanachter de bovenhoeken van de
    # knoop om de hals loopt, in een vlak loodrecht op de halsas, en naast de
    # knoop tussen de halsveren wegzakt; de veren werken als boord.
    band_z = t['knot_top'] - t['band_width'] / 2 - .004
    def neck_center(z):
        f = surface.front(0, z)[0]; b = surface.bvh.ray_cast(Vector((4, 0, z)), Vector((-1, 0, 0)))[0]
        return (f + b) / 2
    axis = (neck_center(band_z + .06) - neck_center(band_z - .06)).normalized()
    if axis.z < 0: axis = -axis
    front_loc, _ = surface.front(0, band_z)
    center = neck_center(band_z)
    forward = (front_loc - center); forward = (forward - axis * forward.dot(axis)).normalized()
    # Het midden langs de halsas verschuiven zodat de straal op 0° precies het
    # voorpunt raakt; een voorover hellende hals laat de band anders onder de
    # knoop door lopen.
    center = front_loc - forward * (front_loc - center).dot(forward)
    rings = []; uvs = []; length = 0; previous = None; role = []; bones = []
    visible = t['band_visible']
    samples = []
    for k in range(-34, 35):
        angle = math.radians(k * 2.5)                          # ±85°
        loc, n = surface.around(center, axis, forward, angle)
        samples.append((abs(math.degrees(angle)), loc, n))
    for k, (a, loc, n) in enumerate(samples):
        # Breedte in het huidvlak, loodrecht op de looprichting: plat op de veren.
        ahead = samples[min(k + 1, len(samples) - 1)][1] - samples[max(k - 1, 0)][1]
        across = n.cross(ahead).normalized()
        if across.dot(axis) < 0: across = -across
        under = smoothstep(visible, visible + 18, a)
        # Wegzakken en smaller worden tot bijna niets, zodat er van achteren
        # geen afgeknipt lipje uit de veren steekt.
        sink = t['band_gap'] - (t['band_gap'] + .024) * under
        width = t['band_width'] * (1 - .92 * under)
        if previous is not None: length += (loc - previous).length
        previous = loc; ring = []; uv = []
        for i, (lateral, d) in enumerate(rounded_ring(width / 2, 0, t['band_thickness'], steps=4, edge=3)):
            ring.append(loc + across * lateral + n * (sink + d))
            uv.append((lateral / period, length / period))
            # Het zichtbare stuk moet buiten de veren blijven; voorbij de
            # zichtbare boog verdwijnt de band bewust onder het verenkleed.
            role.append(('band-achter' if i <= 4 else 'band') if a <= visible + 1 else 'band-onder-veren')
            share = 1 - smoothstep(6, 24, a)
            bones.append({TIE_BONE: share} if share > 1e-4 else {})
        rings.append(ring); uvs.append(uv)
    parts.append(loft_mesh('Stropdas halsband', rings, uvs, material))
    roles['Stropdas halsband'] = role; anchors['Stropdas halsband'] = bones
    footprint = dict(y=t['knot_top_width'] / 2 + .012, z0=knot_bottom - .005, z1=t['knot_top'] + .012, blade_reference=reference,
                     blade_bottom=stations[-1][1], blade_half=t['blade_width'] / 2 + .01, anchor_z=anchor_z,
                     knot_bottom=tuple(surface.front(0, knot_bottom)[0] + knot_normal * t['gap']),
                     centerline=centerline)
    return parts, roles, anchors, footprint


def silk_material():
    """Zijde met klassieke diagonale streep: kleur, ribbel en glans als tegels.

    Grond okergoud (#8d6414, de lichte personakleur van de Stakeholder), een
    crèmegouden streep (#e5c473, de donkere personakleur) met een marineblauwe
    bies. De stof is repp: fijne ribbels in de richting van de streep. Die
    staan in een normal map en een ruwheidskaart (glans op de ribbels, mat in
    de dalen); kleur, normal en metallicRoughness zijn kern-glTF, zonder
    extensies. De kleurtegel is 1024 px, zodat de bies de mipmaps overleeft.
    """
    size = 1024
    y, x = np.mgrid[0:size, 0:size] / size
    q = (x + y) % 1.0                                           # 45° in stofruimte
    def band(a, b, soft=.004):
        return np.clip((q - a) / soft + .5, 0, 1) * np.clip((b - q) / soft + .5, 0, 1)
    ground = np.array([0x8d, 0x64, 0x14]) / 255
    cream = np.array([0xe5, 0xc4, 0x73]) / 255
    navy = np.array([0x1c, 0x25, 0x40]) / 255
    rgb = np.ones((size, size, 3)) * ground
    c = band(.02, .11)[..., None]; rgb = rgb * (1 - c) + cream * c
    nv = (band(.126, .170) + band(-.044, -.008) + band(.956, .992))[..., None].clip(0, 1); rgb = rgb * (1 - nv) + navy * nv
    ribs = 64                                                   # ribbels per tegel, evenwijdig aan de streep
    phase = (x + y) * ribs * 2 * math.pi
    rgb = np.clip(rgb * (1 + .025 * np.sin(phase))[..., None], 0, 1)
    def image(name, pixels, colorspace, path, fmt, **save):
        h, w = pixels.shape[:2]
        img = bpy.data.images.new(name, width=w, height=h)
        img.colorspace_settings.name = colorspace
        img.pixels = np.concatenate((pixels, np.ones((h, w, 1))), axis=2).astype(np.float32).ravel()
        img.filepath_raw = os.path.join(OUT, path); img.file_format = fmt; img.save(**save); img.pack()
        return img
    color = image('Stropdas zijde streep', rgb, 'sRGB', 'stropdas-zijde.jpg', 'JPEG', quality=92)
    # Ribbel als hoogte h = sin(fase); de normaal kantelt loodrecht op de ribbel.
    n = 512; yy, xx = np.mgrid[0:n, 0:n] / n
    slope = .45 * np.cos((xx + yy) * ribs * 2 * math.pi)
    normal = np.stack([-slope, -slope, np.ones_like(slope)], axis=-1)
    normal /= np.linalg.norm(normal, axis=-1, keepdims=True)
    normal_img = image('Stropdas zijde ribbel', normal * .5 + .5, 'Non-Color', 'stropdas-ribbel.png', 'PNG')
    rough = .44 + .14 * (.5 - .5 * np.sin((xx + yy) * ribs * 2 * math.pi))   # zijde, geen gegoten plastic
    rough_img = image('Stropdas zijde glans', np.stack([np.zeros_like(rough), rough, np.zeros_like(rough)], axis=-1), 'Non-Color', 'stropdas-glans.png', 'PNG')
    material = bpy.data.materials.new('Stropdas zijde'); material.use_nodes = True
    nodes = material.node_tree.nodes; links = material.node_tree.links
    shader = nodes['Principled BSDF']; shader.inputs['Metallic'].default_value = 0
    tex = nodes.new('ShaderNodeTexImage'); tex.image = color
    links.new(tex.outputs['Color'], shader.inputs['Base Color'])
    ntex = nodes.new('ShaderNodeTexImage'); ntex.image = normal_img
    nmap = nodes.new('ShaderNodeNormalMap'); nmap.inputs['Strength'].default_value = .6
    links.new(ntex.outputs['Color'], nmap.inputs['Color']); links.new(nmap.outputs['Normal'], shader.inputs['Normal'])
    rtex = nodes.new('ShaderNodeTexImage'); rtex.image = rough_img
    split = nodes.new('ShaderNodeSeparateColor')
    links.new(rtex.outputs['Color'], split.inputs['Color']); links.new(split.outputs['Green'], shader.inputs['Roughness'])
    return material


TIE_BONE = 'Stropdas'
TIE_BLADE_BONE = 'Stropdas blad'          # vier botten: 'Stropdas blad 0' … '3'
PUSH_LIMIT = .08          # verder duwt de stap de knoop nooit van zijn huidpunt af
SPRING = dict(frequency=2.0, damping=.45)  # naloop van het blad (Hz, dempingsgraad)
BEND_LIMIT = math.radians(40)             # grootste knik tussen twee bladdelen

def transfer_weights(surface, body, parts, rig, anchors):
    """Gewichten van de dichtstbijzijnde huid, per driehoek geïnterpoleerd.

    Dit is POLYINTERP_NEAREST van Blenders Data Transfer, maar dan zichtbaar:
    elke vertex krijgt de mix van botten van het huidpunt eronder. Welke botten
    dat zijn (één halsbot of vijf) maakt voor de stap niet uit. Het deel dat
    `anchors` per vertex aan de eigen stropdasbotten geeft, gaat daarheen; de
    rest volgt de huid.
    """
    names = [g.name for g in body.vertex_groups]
    weights = np.zeros((len(body.data.vertices), len(names)))
    for v in body.data.vertices:
        for g in v.groups: weights[v.index, g.group] = g.weight
    for ob in parts:
        groups = {}
        def group(name):
            if name not in groups: groups[name] = ob.vertex_groups.get(name) or ob.vertex_groups.new(name=name)
            return groups[name]
        for v in ob.data.vertices:
            own = anchors[ob.name][v.index]; rest = max(0., 1 - sum(own.values()))
            for name, w in own.items(): group(name).add([v.index], float(w), 'REPLACE')
            if rest <= 1e-4: continue
            loc, n, index, d = surface.bvh.find_nearest(v.co)
            poly = list(surface.polys[index]); pts = surface.points[poly]
            dist = np.maximum(np.linalg.norm(pts - np.array(loc), axis=1), 1e-7)
            bary = (1 / dist) / (1 / dist).sum()
            mix = (weights[poly] * bary[:, None]).sum(0); mix = mix / max(mix.sum(), 1e-9) * rest
            for gi, w in enumerate(mix):
                if w > 1e-4: group(names[gi]).add([v.index], float(w), 'REPLACE')
        world = ob.matrix_world.copy(); ob.parent = rig; ob.matrix_world = world
        mod = ob.modifiers.new('Volgt de huid', 'ARMATURE'); mod.object = rig


def kabsch(a, b):
    """Starre transformatie (R, t) die puntwolk a zo goed mogelijk op b legt."""
    ca, cb = a.mean(0), b.mean(0)
    u, _, vt = np.linalg.svd((a - ca).T @ (b - cb))
    d = np.sign(np.linalg.det(vt.T @ u.T))
    r = vt.T @ np.diag([1, 1, d]) @ u.T
    return r, cb - r @ ca

def matrix_from(r, t):
    world = Matrix.Identity(4)
    for i in range(3):
        for j in range(3): world[i][j] = r[i, j]
        world[i][3] = t[i]
    return world

def turn_between(a, b):
    """Kleinste draaiing (3×3) die richting a op richting b legt."""
    return np.array(Vector(a).rotation_difference(Vector(b)).to_matrix())

GROUND_CLEARANCE = .004   # de das blijft minstens zo ver boven de voetzolen

def ground_level(rig, exclude=()):
    """Grondhoogte: het laagste punt van de duif in rust (voetzolen, nagels)."""
    meshes = [o for o in bpy.data.objects if o.type == 'MESH' and o not in exclude and o.visible_get()
              and not o.name.startswith('Stropdas')]
    return float(with_rest_pose(rig, lambda: min(evaluated(o)[0][:, 2].min() for o in meshes if len(o.data.vertices))))

def bake_tie_bones(rig, body, knot, blade, band, footprint, anchors, margin=.003, turn=.8):
    """Eigen botten voor knoop en blad, per frame berekend uit de huid.

    Knoop (bot `Stropdas`): per frame twee starre bewegingen uit de huid
    (kleinste kwadraten): die van de keel onder de knoop en die van de onderste
    borst. De knoop draait met de borst mee en neemt 80 % van de extra draaiing
    van de keel over; zijn midden volgt de keel. Daarna langs de werkelijke
    huidnormaal op zijn rustafstand gezet: vrij van het lijf, ook als huid van
    boven komt, en niet ervan los. In rust is dat exact de rustpose.

    Blad (botten `Stropdas blad 0`–`3`): een keten die aan de onderrand van de
    knoop hangt. Elk deel wil zijn rustrichting in de wereld houden, want de
    das hangt met de zwaartekracht; waar het lijf in de weg zit, draait het
    deel naar buiten tot het vrij ligt, dus de borst draagt de das. Een
    gedempte veer per deel geeft een kleine naloop; knikken tussen delen zijn
    begrensd. In rust komt de keten exact op de rustvorm uit en het laatste
    frame van elke clip is exact de rustpose.

    Alle botten hangen aan het wortelbot van de rig, niet aan een halsbot met
    een bepaalde naam. De sleutels staan in dezelfde clips; de runtime kent
    alleen de clipnamen.
    """
    rest_points, polys = with_rest_pose(rig, lambda: rest_mesh(body))
    rest_surface = Surface(rest_points, polys)
    def front_only(mask):
        # Alleen de voorkant van hals en borst, niet de nek of rug erachter.
        keep = []
        for i in np.where(mask)[0]:
            hit = rest_surface.bvh.ray_cast(Vector((-4, rest_points[i, 1], rest_points[i, 2])), Vector((1, 0, 0)))[0]
            if hit is not None and rest_points[i, 0] < hit.x + .03: keep.append(i)
        return np.array(keep)
    idx = front_only((np.abs(rest_points[:, 1]) < footprint['y']) & (rest_points[:, 2] > footprint['z0']) & (rest_points[:, 2] < footprint['z1']))
    # Borst: alleen de onderste helft onder het blad, die star met de romp meegaat.
    chest = front_only((np.abs(rest_points[:, 1]) < footprint['blade_half']) & (rest_points[:, 2] > footprint['blade_bottom']) & (rest_points[:, 2] < footprint['blade_reference'][2]))
    knot_rest = with_rest_pose(rig, lambda: evaluated(knot)[0])
    knot_polys = [tuple(p.vertices) for p in knot.data.polygons]
    ground = ground_level(rig, (knot, blade, band))
    band_all = with_rest_pose(rig, lambda: evaluated(band)[0])
    band_rest = band_all[np.array([w.get(TIE_BONE, 0) > .99 for w in anchors['Stropdas halsband']])]
    knot_d0 = min(rest_surface.signed(knot_rest).min(), rest_surface.signed(band_rest).min() + .0015)
    # Bladketen: scharnieren op de middellijn, rustrichtingen en per deel een
    # handvol achterkantpunten om botsing met het lijf te toetsen.
    line = footprint['centerline']
    def on_line(arc):
        arcs = np.array([a for a, _ in line]); pts = np.array([p for _, p in line])
        return np.array([np.interp(arc, arcs, pts[:, k]) for k in range(3)])
    joints = [on_line(a) for a in (0.,) + BLADE_JOINTS + (line[-1][0],)]
    lengths = [np.linalg.norm(joints[i + 1] - joints[i]) for i in range(SEGMENTS)]
    rest_dirs = [(joints[i + 1] - joints[i]) / lengths[i] for i in range(SEGMENTS)]
    blade_rest = with_rest_pose(rig, lambda: evaluated(blade)[0])
    blade_roles = np.array(footprint['blade_roles'])
    samples = []; everything = []
    for i in range(SEGMENTS):
        name = f'{TIE_BLADE_BONE} {i}'
        mask = np.array([anchors['Stropdas blad'][k].get(name, 0) > .5 for k in range(len(blade_rest))]) & (blade_roles == 'blad-achter')
        # Punten vlak bij het scharnier worden door het vorige deel bepaald;
        # een draaiing om dat scharnier kan ze niet vrijmaken.
        along = (blade_rest - joints[i]) @ rest_dirs[i]
        mask &= along > .25 * lengths[i]
        pts = blade_rest[mask]; samples.append(pts[::max(1, len(pts) // 90)])
        full = np.array([anchors['Stropdas blad'][k].get(name, 0) > .05 for k in range(len(blade_rest))]) & (blade_roles != 'verborgen')
        everything.append(blade_rest[full])
    sample_d0 = [rest_surface.signed(p).min() for p in samples]
    matrix = rig.matrix_world.copy(); inverse = matrix.inverted()
    # Het wortelbot met de meeste nakomelingen (romp), niet een los pootbot.
    root_bone = max((b for b in rig.data.bones if b.parent is None), key=lambda b: len(b.children_recursive))
    bpy.context.view_layer.objects.active = rig; bpy.ops.object.mode_set(mode='EDIT')
    center = Vector(knot_rest.mean(0))
    heads = [(TIE_BONE, center, center + Vector((0, 0, .05)))]
    heads += [(f'{TIE_BLADE_BONE} {i}', Vector(joints[i]), Vector(joints[i + 1])) for i in range(SEGMENTS)]
    for name, head, tail in heads:
        bone = rig.data.edit_bones.new(name)
        bone.head = inverse @ head; bone.tail = inverse @ tail
        bone.parent = rig.data.edit_bones[root_bone.name]; bone.use_deform = True
    bpy.ops.object.mode_set(mode='OBJECT')
    names = [h[0] for h in heads]
    bones = {n: rig.pose.bones[n] for n in names}
    rest_local = {n: rig.data.bones[n].matrix_local.copy() for n in bones}
    for pb in bones.values(): pb.rotation_mode = 'QUATERNION'
    def pose(name, r, t):
        bones[name].matrix = inverse @ matrix_from(r, t) @ matrix @ rest_local[name]
    scene = bpy.context.scene; report = {}
    c = np.array(center)
    omega = 2 * math.pi * SPRING['frequency']; zeta = SPRING['damping']; dt = 1 / scene.render.fps
    for action in sorted(bpy.data.actions, key=lambda a: a.name):
        use_action(rig, action); frames = clip_frames(action)
        report[action.name] = {'knoop_verschuiving': 0., 'blad_max_hoek_graden': 0., 'blad_naloop_graden': 0.}
        state = [d.copy() for d in rest_dirs]; velocity = [np.zeros(3) for _ in range(SEGMENTS)]
        for number, frame in enumerate(frames):
            for pb in bones.values(): pb.location = (0, 0, 0); pb.rotation_quaternion = (1, 0, 0, 0)
            scene.frame_set(frame)
            pts, bpolys = evaluated(body)
            if frame in (frames[0], frames[-1]):
                for name in names: pose(name, np.eye(3), np.zeros(3))
                state = [d.copy() for d in rest_dirs]; velocity = [np.zeros(3) for _ in range(SEGMENTS)]
            else:
                surface = Surface(pts, bpolys)
                rk, tk = kabsch(rest_points[idx], pts[idx])          # keel onder de knoop
                rc, tc = kabsch(rest_points[chest], pts[chest])      # borst onder het blad
                qc = Matrix(rc.tolist()).to_quaternion(); qk = Matrix(rk.tolist()).to_quaternion()
                moved_center = rk @ c + tk; r = np.array(qc.slerp(qk, turn).to_matrix()); t = moved_center - r @ c
                # Duwrichting: de werkelijke huidnormaal onder het knoopmidden.
                loc, _, index, _ = surface.bvh.find_nearest(Vector(moved_center))
                normal = np.array(surface.smooth_normal(loc, index))
                start = t.copy()
                for _ in range(8):
                    moved = knot_rest @ r.T + t
                    need = knot_d0 - min(surface.signed(moved).min(), surface.signed(band_rest @ r.T + t).min() + .0015)
                    near = pts[(np.abs(pts - moved.mean(0)) < .1).all(axis=1)]
                    if len(near):
                        depth = Surface(moved, knot_polys).depth_inside(near).max()
                        if depth > 0: need = max(need, depth + margin)
                    if abs(need) <= 1e-5: break
                    t = t + normal * need
                    if np.linalg.norm(t - start) > PUSH_LIMIT: t = start + (t - start) * PUSH_LIMIT / np.linalg.norm(t - start); break
                report[action.name]['knoop_verschuiving'] = max(report[action.name]['knoop_verschuiving'], float(np.linalg.norm(t - start)))
                pose(TIE_BONE, r, t)
                # Bladketen vanaf de onderrand van de knoop.
                joint = r @ joints[0] + t
                previous = None
                for i in range(SEGMENTS):
                    soft = min(sample_d0[i], margin + .0015) - 1e-4   # rustafstand, met speling voor afrondingsruis
                    hard = .0008                                      # nooit dichterbij dan dit
                    def clearances(direction, need=None):
                        turn_i = turn_between(rest_dirs[i], direction)
                        moved = (samples[i] - joints[i]) @ turn_i.T + joint
                        lowest = ((everything[i] - joints[i]) @ turn_i.T + joint)[:, 2].min()
                        return surface.signed(moved).min() - (soft if need is None else need), lowest - (ground + GROUND_CLEARANCE)
                    def search(direction, axis, ok, limit):
                        # Kleinste draaiing om `axis` waarbij ok(richting) geldt.
                        rotate = lambda angle: np.array(Matrix.Rotation(angle, 3, Vector(axis)) @ Vector(direction))
                        if ok(direction): return direction
                        low, high = 0., limit
                        if not ok(rotate(high)): return rotate(high)
                        for _ in range(16):
                            middle_angle = (low + high) / 2
                            if ok(rotate(middle_angle)): high = middle_angle
                            else: low = middle_angle
                        return rotate(high)
                    def free(direction, need=None):
                        # Kleinste draaiing die het deel vrij maakt: eerst weg van
                        # het lijf, dan omhoog van de grond. Continu gezocht, zodat
                        # opeenvolgende frames niet tussen stapjes heen en weer gaan.
                        for _ in range(3):
                            body_gap, ground_gap = clearances(direction, need)
                            if body_gap >= 0 and ground_gap >= 0: break
                            if body_gap < 0:
                                middle = joint + direction * lengths[i] / 2
                                loc, _, index, _ = surface.bvh.find_nearest(Vector(middle))
                                axis = np.cross(direction, np.array(surface.smooth_normal(loc, index)))
                                if np.linalg.norm(axis) > 1e-9:
                                    direction = search(direction, axis / np.linalg.norm(axis), lambda d: clearances(d, need)[0] >= 0, 1.2)
                            body_gap, ground_gap = clearances(direction, need)
                            if ground_gap < 0:
                                # Stof op de grond: het deel houdt zijn richting over
                                # de grond, maar wordt vlakker tot het erop ligt, in
                                # plaats van erdoorheen te gaan of op te klappen.
                                heading = direction.copy(); heading[2] = 0.
                                if np.linalg.norm(heading) < 1e-6:
                                    heading = rc @ np.array([-1., 0., 0.]); heading[2] = 0.
                                heading /= np.linalg.norm(heading)
                                steep = math.asin(max(-1., min(1., -direction[2])))     # omlaag positief
                                at = lambda angle: heading * math.cos(angle) - np.array([0., 0., math.sin(angle)])
                                low, high = -.35, steep
                                if clearances(at(low), need)[1] < 0: direction = at(low)
                                else:
                                    for _ in range(18):
                                        mid = (low + high) / 2
                                        if clearances(at(mid), need)[1] >= 0: low = mid
                                        else: high = mid
                                    direction = at(low)
                        return direction
                    target = free(rest_dirs[i].copy())                # zwaartekracht, dan het lijf
                    error = target - state[i]
                    velocity[i] = velocity[i] + (omega * omega * error - 2 * zeta * omega * velocity[i]) * dt
                    state[i] = state[i] + velocity[i] * dt; state[i] /= np.linalg.norm(state[i])
                    # De laatste twaalf frames lopen zacht naar het doel, dat daar de rust is.
                    settle = smoothstep(len(frames) - 13, len(frames) - 2, number)
                    state[i] = state[i] * (1 - settle) + target * settle; state[i] /= np.linalg.norm(state[i])
                    if previous is not None:
                        bend = math.acos(max(-1, min(1, float(state[i] @ previous))))
                        if bend > BEND_LIMIT:
                            axis = np.cross(previous, state[i]); axis /= max(np.linalg.norm(axis), 1e-9)
                            state[i] = np.array(Matrix.Rotation(BEND_LIMIT, 3, Vector(axis)) @ Vector(previous))
                    # Het doel (op rustafstand van de borst) loopt via de veer;
                    # alleen echte aanraking wordt direct rechtgezet.
                    state[i] = free(state[i], hard)
                    turn_i = turn_between(rest_dirs[i], state[i])
                    # Laatste zekerheid: raakt een punt van dit deel (ook vlak bij
                    # het scharnier) toch het lijf, dan schuift het deel met de
                    # rest van de keten een fractie naar buiten.
                    for _ in range(6):
                        moved = (everything[i] - joints[i]) @ turn_i.T + joint
                        distance = surface.signed(moved); worst = int(distance.argmin())
                        low = ground + GROUND_CLEARANCE - moved[:, 2].min()
                        if low > 0: joint = joint + np.array([0., 0., low + 2e-4]); continue
                        if distance[worst] >= hard: break
                        loc, _, index, _ = surface.bvh.find_nearest(Vector(moved[worst]))
                        joint = joint + np.array(surface.smooth_normal(loc, index)) * (hard - distance[worst] + 2e-4)
                    pose(f'{TIE_BLADE_BONE} {i}', turn_i, joint - turn_i @ joints[i])
                    angle = math.degrees(math.acos(max(-1, min(1, float(state[i] @ rest_dirs[i])))))
                    lag = math.degrees(math.acos(max(-1, min(1, float(state[i] @ target)))))
                    report[action.name]['blad_max_hoek_graden'] = max(report[action.name]['blad_max_hoek_graden'], angle)
                    report[action.name]['blad_naloop_graden'] = max(report[action.name]['blad_naloop_graden'], lag)
                    if os.environ.get('DUIVEN_DEBUG_BLADE'):
                        print('BLADE', action.name, frame, i, round(angle, 2), round(lag, 2), round(math.degrees(math.acos(max(-1, min(1, float(target @ rest_dirs[i]))))), 2))
                    previous = state[i]; joint = joint + state[i] * lengths[i]
            bpy.context.view_layer.update()
            for name, pb in bones.items():
                pb.keyframe_insert('location', frame=frame, group=name)
                pb.keyframe_insert('rotation_quaternion', frame=frame, group=name)
    rig.animation_data.action = None
    for pb in bones.values(): pb.location = (0, 0, 0); pb.rotation_quaternion = (1, 0, 0, 0)
    scene.frame_set(scene.frame_start)
    print('TIE_BONES_BAKED', len(idx), 'keelpunten', len(chest), 'borstpunten', json.dumps(report))
    return {'footprint_vertices': int(len(idx)), 'chest_vertices': int(len(chest)), 'knot_turn_share': turn,
            'spring': SPRING, 'bend_limit_degrees': math.degrees(BEND_LIMIT), 'ground_z': ground, 'segments': SEGMENTS, 'per_clip': report}


def place_tie(surface):
    """Knoop vlak onder de kin, gevonden aan de vorm van de basis.

    Langs de voorkant omhoog: de kin is waar het oppervlak onder de kop naar
    beneden gaat kijken (normaal met een neerwaartse component van meer dan
    0,3). De knoop komt daar chin_gap onder, zoals een das onder een boord.
    De stap kijkt alleen naar dat ene kantelpunt, niet naar snavel of ogen.
    """
    chin = None
    for z in np.arange(1.35, 2.0, .005):
        loc, n = surface.front(0, float(z))
        if n.z < -.3: chin = float(z); break
    tie = dict(TIE)
    if chin is not None: tie['knot_top'] = round(chin - TIE['chin_gap'], 4)
    print('TIE_PLACED', 'kin', chin, 'knoop', tie['knot_top'])
    return tie


def bake_occlusion(parts, surface, material, reach=.035, rays=32, strength=.6):
    """Omgevingsschaduw per vertex als COLOR_0, vermenigvuldigd met de stofkleur.

    De runtime belicht vlak (hemisfeer plus studio-omgeving); zonder deze
    schaduw verdwijnen wikkelrand, kuiltje en de randen van de band. COLOR_0
    is kern-glTF en werkt in Cycles en three.js hetzelfde.
    """
    trees = [surface.bvh]
    for ob in parts:
        pts = np.array([tuple(v.co) for v in ob.data.vertices])
        trees.append(BVHTree.FromPolygons([Vector(p) for p in pts], [tuple(f.vertices) for f in ob.data.polygons]))
    golden = math.pi * (3 - math.sqrt(5))
    hemisphere = [Vector((math.cos(golden * i) * math.sqrt(1 - ((i + .5) / rays) ** 2), math.sin(golden * i) * math.sqrt(1 - ((i + .5) / rays) ** 2), (i + .5) / rays)) for i in range(rays)]
    for ob in parts:
        ob.data.update()
        layer = ob.data.color_attributes.new('Schaduw', 'BYTE_COLOR', 'POINT')
        for v in ob.data.vertices:
            n = v.normal.normalized(); origin = v.co + n * .0008
            tangent = n.orthogonal().normalized(); bitangent = n.cross(tangent)
            hit = 0
            for d in hemisphere:
                direction = tangent * d.x + bitangent * d.y + n * d.z
                if any(tree.ray_cast(origin, direction, reach)[0] is not None for tree in trees): hit += 1
            shade = 1 - strength * (hit / rays) ** 1.2
            layer.data[v.index].color = (shade, shade, shade, 1)
    nodes = material.node_tree.nodes; links = material.node_tree.links
    shader = nodes['Principled BSDF']; tex = next(n for n in nodes if n.type == 'TEX_IMAGE' and n.image.name == 'Stropdas zijde streep')
    attribute = nodes.new('ShaderNodeVertexColor'); attribute.layer_name = 'Schaduw'
    mix = nodes.new('ShaderNodeMix'); mix.data_type = 'RGBA'; mix.blend_type = 'MULTIPLY'; mix.inputs['Factor'].default_value = 1
    links.new(tex.outputs['Color'], mix.inputs['A']); links.new(attribute.outputs['Color'], mix.inputs['B'])
    links.new(mix.outputs['Result'], shader.inputs['Base Color'])


def add_tie(rig, body):
    points, polys = with_rest_pose(rig, lambda: rest_mesh(body))
    surface = Surface(points, polys)
    tie = place_tie(surface)
    material = silk_material()
    parts, roles, anchors, footprint = build_tie(surface, material, tie)
    bake_occlusion(parts, surface, material)
    transfer_weights(surface, body, parts, rig, anchors)
    footprint['blade_roles'] = roles['Stropdas blad']
    part = {p.name: p for p in parts}
    bake = bake_tie_bones(rig, body, part['Stropdas knoop'], part['Stropdas blad'], part['Stropdas halsband'], footprint, anchors)
    bake['placement'] = {'knot_top': tie['knot_top'], 'blade_length': tie['blade_length'], 'rule': 'chin_gap onder de kin'}
    # Rustposities en rollen per vertex: na export en herimport verandert de
    # vertexvolgorde, dus de meting koppelt terug via de dichtstbijzijnde bronvertex.
    reference = {ob.name: (np.array([tuple(v.co) for v in ob.data.vertices]), np.array(roles[ob.name])) for ob in parts}
    return parts, reference, bake


# ---------------------------------------------------------------------------
# Meting: stropdas tegen lijf, vleugels en de rest van de duif, per frame.

GAP_LIMIT = .025          # grootste toegestane afstand stof–veren (≈ 3 mm echt)
TIE_NAMES = ('Stropdas blad', 'Stropdas knoop', 'Stropdas halsband')

def evaluated(ob):
    dg = bpy.context.evaluated_depsgraph_get()
    ev = ob.evaluated_get(dg); me = ev.to_mesh()
    pts = np.empty(len(me.vertices) * 3, np.float32); me.vertices.foreach_get('co', pts)
    m = np.array(ev.matrix_world); pts = pts.reshape(-1, 3) @ m[:3, :3].T + m[:3, 3]
    polys = [tuple(p.vertices) for p in me.polygons]
    ev.to_mesh_clear()
    return pts, polys

def roles_for(ob, reference):
    """Rol per vertex van een (her)geïmporteerd stropdasdeel, via de rustpose."""
    from mathutils.kdtree import KDTree
    source, roles = reference[ob.name]
    tree = KDTree(len(source))
    for i, p in enumerate(source): tree.insert(Vector(p), i)
    tree.balance()
    rig = next(m.object for m in ob.modifiers if m.type == 'ARMATURE')
    pts, polys = with_rest_pose(rig, lambda: evaluated(ob))
    out = []; worst = 0
    for p in pts:
        co, index, dist = tree.find(Vector(p)); out.append(roles[index]); worst = max(worst, dist)
    assert worst < 1e-4, (ob.name, worst)
    return np.array(out), polys, pts

def clip_frames(action):
    start, end = (int(round(f)) for f in action.frame_range)
    return list(range(start, end + 1))

def use_action(rig, action):
    rig.animation_data_create(); rig.animation_data.action = action
    if getattr(action, 'slots', None) and rig.animation_data.action_slot is None:
        rig.animation_data.action_slot = action.slots[0]

def measure_tie(rig, body, reference, label):
    """Afstanden en doorsnijdingen van de stropdas in elk frame van elke clip."""
    tie = [bpy.data.objects[n] for n in TIE_NAMES]
    # Botweergavevormen (de glTF-import maakt een 'Icosphere' als bone shape)
    # horen niet bij de duif en staan niet in de GLB.
    shapes = {pb.custom_shape for pb in rig.pose.bones if pb.custom_shape}
    others = [o for o in bpy.data.objects if o.type == 'MESH' and o not in tie and o != body and o not in shapes and o.visible_get()]
    roles = {o.name: roles_for(o, reference) for o in tie}
    ground = ground_level(rig, tie)
    # Onderste derde van het blad: daar moet de das op de borst rusten.
    blade_role, _, blade_rest = roles['Stropdas blad']
    back = np.array([r == 'blad-achter' for r in blade_role]); zs = blade_rest[:, 2]
    lower = back & (zs < zs[back].min() + (zs[back].max() - zs[back].min()) / 3)
    scene = bpy.context.scene; report = {'label': label, 'gap_limit': GAP_LIMIT, 'ground_z': ground, 'clips': {}}
    for action in sorted(bpy.data.actions, key=lambda a: a.name):
        use_action(rig, action); frames = []; previous = {}
        for frame in clip_frames(action):
            scene.frame_set(frame)
            bpts, bpolys = evaluated(body)
            body_step = float(np.abs(bpts - previous['lijf']).max()) if 'lijf' in previous else None
            previous['lijf'] = bpts
            normals = Surface(bpts, bpolys); surface = normals.bvh
            opts = []; opolys = []
            for o in others:
                p, f = evaluated(o); base = len(opts); opts.extend(p.tolist()); opolys.extend(tuple(i + base for i in poly) for poly in f)
            other_tree = BVHTree.FromPolygons([Vector(p) for p in opts], opolys)
            row = {'frame': frame}
            for o in tie:
                role, polys, _ = roles[o.name]
                pts, _ = evaluated(o)
                tree = BVHTree.FromPolygons([Vector(p) for p in pts], polys)
                visible = np.array([r not in ('verborgen', 'band-onder-veren') for r in role])
                signed = normals.signed(pts[visible])
                back = np.array([r.endswith('-achter') for r in role])
                gaps = normals.signed(pts[back]) if back.any() else np.array([0.])
                def bad(pairs):
                    return sum(1 for a, b in pairs if all(visible[i] for i in polys[b]))
                row[o.name] = {'min_afstand': float(signed.min()), 'max_kier_achterkant': float(gaps.max()),
                               'aandeel_achterkant_boven_grens': float((gaps > GAP_LIMIT).mean()),
                               'doorsnijding_lijf': bad(surface.overlap(tree)),
                               'doorsnijding_overig': bad(other_tree.overlap(tree))}
                if o.name == 'Stropdas blad':
                    row[o.name]['contact_onderste_derde'] = float(normals.signed(pts[lower]).min())
                if o.name == 'Stropdas knoop':
                    # Op de keel: het dichtstbijzijnde deel van de knoopachterkant.
                    row[o.name]['contact'] = float(gaps.min())
                row[o.name]['boven_grond'] = float(pts[:, 2].min() - ground)
                # Sprongetje: beweging van de das terwijl het lijf stilstaat.
                step = float(np.linalg.norm(pts - previous[o.name], axis=1).max()) if o.name in previous else 0.
                previous[o.name] = pts
                row[o.name]['stap'] = step; row[o.name]['lijf_stil'] = body_step is not None and body_step < 1e-6
            frames.append(row)
        clip = action.name
        summary = {}
        for n in TIE_NAMES:
            summary[n] = {'min_afstand': min(r[n]['min_afstand'] for r in frames),
                          'max_kier_achterkant': max(r[n]['max_kier_achterkant'] for r in frames),
                          'frames_met_kier_boven_grens': sum(1 for r in frames if r[n]['max_kier_achterkant'] > GAP_LIMIT),
                          'max_aandeel_achterkant_boven_grens': max(r[n]['aandeel_achterkant_boven_grens'] for r in frames),
                          'frames_met_doorsnijding_lijf': sum(1 for r in frames if r[n]['doorsnijding_lijf']),
                          'frames_met_doorsnijding_overig': sum(1 for r in frames if r[n]['doorsnijding_overig'])}
        summary['Stropdas blad']['max_afstand_onderste_derde'] = max(r['Stropdas blad']['contact_onderste_derde'] for r in frames)
        summary['Stropdas knoop']['max_contactafstand_keel'] = max(r['Stropdas knoop']['contact'] for r in frames)
        for n in TIE_NAMES:
            summary[n]['min_boven_grond'] = min(r[n]['boven_grond'] for r in frames)
            summary[n]['frames_onder_grond'] = sum(1 for r in frames if r[n]['boven_grond'] < 0)
            still = [r[n]['stap'] for r in frames if r[n]['lijf_stil']]
            summary[n]['max_stap_bij_stil_lijf'] = max(still) if still else 0.
            summary[n]['stap_laatste_frame'] = frames[-1][n]['stap']
        report['clips'][clip] = {'frames': len(frames), 'samenvatting': summary, 'per_frame': frames}
        print('TIE_MEASURE', label, clip, json.dumps(summary))
    rig.animation_data.action = None; scene.frame_set(scene.frame_start)
    return report


# ---------------------------------------------------------------------------
# Kennispartner: een donkerder, koeler leigrijs verenkleed ("blauw met
# banden"), een van de gewoonste kleurslagen van de stadsduif. De vleugelbanden
# worden dieper, de kop donkerder; de iriserende hals houdt haar tint. Oog,
# snavel, washuid en poten blijven natuurgetrouw: hun beeldvlakken in de foto
# worden uitgespaard en hun eigen materialen niet aangeraakt.

def image_array(image):
    w, h = image.size
    return np.array(image.pixels[:], np.float32).reshape(h, w, 4)

def iridescent_share(rgb):
    """Aandeel halsglans: alleen verzadigd groen/blauwgroen en paars/magenta.
    Bruin, roest en oranje tellen als verenkleed en worden leigrijs."""
    mx, mn = rgb.max(axis=-1), rgb.min(axis=-1); chroma = mx - mn
    r, g, b = rgb[..., 0], rgb[..., 1], rgb[..., 2]
    safe = np.maximum(chroma, 1e-6)
    hue = np.where(mx == r, ((g - b) / safe) % 6, np.where(mx == g, (b - r) / safe + 2, (r - g) / safe + 4)) * 60
    green = (hue > 75) & (hue < 200); purple = (hue > 245) & (hue < 340)
    return np.clip((chroma - .07) / .12, 0, 1) * (green | purple)

def slate(rgb, darken=None):
    """Grijs verenkleed naar middelgrijs-blauw leigrijs; halsglans blijft.

    Een donkerder, koelere toonkromme voor het verenkleed, diepere
    vleugelbanden, en `darken` (per pixel, 0–1) maakt kop en hals extra
    donker: bij een blauwe stadsduif zijn die minstens zo donker als de borst.
    """
    lum = rgb @ np.array([.299, .587, .114])
    shine = iridescent_share(rgb)
    target = (.72 if MORPH == 'donker' else .87) * np.power(np.clip(lum, 0, 1), 1.03)
    if darken is not None: target = target * (1 - darken)
    bars = .62 + .38 * np.clip((lum - .16) / .22, 0, 1)        # donkere banden dieper
    tint = np.array([.93, .98, 1.07])
    grey = np.clip((target * bars)[..., None] * tint, 0, 1)
    mean = rgb.mean(axis=-1, keepdims=True)
    iridescent = np.clip((mean + (rgb - mean) * 1.10) * .85, 0, 1)
    if darken is not None: iridescent = iridescent * (1 - .5 * darken)[..., None]
    return grey * (1 - shine[..., None]) + iridescent * shine[..., None]

def height_map(body, image):
    """Rusthoogte (z) per texel van de huidtextuur, uit de UV's van de huid."""
    w, h = image.size; grid = 256
    total = np.zeros((grid, grid)); count = np.zeros((grid, grid))
    uv = body.data.uv_layers.active
    for poly in body.data.polygons:
        for li in poly.loop_indices:
            u, v = uv.data[li].uv; z = body.data.vertices[body.data.loops[li].vertex_index].co.z
            i = min(grid - 1, max(0, int(v * grid))); j = min(grid - 1, max(0, int(u * grid)))
            total[i, j] += z; count[i, j] += 1
    zmap = np.where(count > 0, total / np.maximum(count, 1), np.nan)
    for _ in range(64):                                       # gaten vullen met buren
        if not np.isnan(zmap).any(): break
        padded = np.pad(zmap, 1, constant_values=np.nan)
        neighbours = np.stack([padded[1 + dy:grid + 1 + dy, 1 + dx:grid + 1 + dx] for dy in (-1, 0, 1) for dx in (-1, 0, 1)])
        fill = np.nanmean(np.where(np.isnan(neighbours), np.nan, neighbours), axis=0)
        zmap = np.where(np.isnan(zmap), fill, zmap)
    zmap = np.nan_to_num(zmap, nan=float(np.nanmean(zmap)))
    rows = (np.arange(h) * grid // h); cols = (np.arange(w) * grid // w)
    return zmap[rows][:, cols]

def protect_mask(image, objects):
    """Zachte maskers over de UV-gebieden van oog en snavel in een beeld."""
    w, h = image.size; mask = np.zeros((h, w), np.float32)
    for ob in objects:
        uv = ob.data.uv_layers.active
        if uv is None: continue
        coords = np.array([d.uv[:] for d in uv.data])
        if not len(coords): continue
        x0, y0 = coords.min(0); x1, y1 = coords.max(0)
        pad = .012
        xs = np.linspace(0, 1, w); ys = np.linspace(0, 1, h)
        mx = np.clip(1 - np.maximum(x0 - pad - xs, xs - x1 - pad) / pad, 0, 1)
        my = np.clip(1 - np.maximum(y0 - pad - ys, ys - y1 - pad) / pad, 0, 1)
        mask = np.maximum(mask, my[:, None] * mx[None, :])
    return mask

def wing_shield(rig):
    """Meshes van het gevouwen vleugeldek: alleen gewogen naar een schouderbot
    van de vleugel (niet de handpennen), met een fototextuur als kleur."""
    shoulders = {b.name for b in rig.data.bones if b.children and all(c.name.startswith(b.name) for c in b.children) and len(b.children) > 3}
    out = []
    for ob in bpy.data.objects:
        if ob.type != 'MESH' or not ob.vertex_groups: continue
        if {g.name for g in ob.vertex_groups} <= shoulders and ob.data.uv_layers and len(ob.data.vertices) > 200:
            out.append(ob)
    return out, sorted(shoulders)

def checker_rows(crop, known, seed=7):
    """Geblokt vleugeldek (blue checker) op de veerrijen van het dek.

    Bij een geblokte stadsduif draagt elke dekveer op haar punt een donkere,
    driehoekige vlek met een lichte zoom; de vlekken staan in overlappende
    rijen die de veerrijen volgen. De rijen lopen evenwijdig aan de bovenrand
    van de binnenste vleugelband (de grote dekveren) en stapelen zich naar de
    schouder op. Daar worden de veren, en dus de vlekken, kleiner en dichter,
    en ze vervagen naar de rand van het dek. De bandrand wordt in het beeld
    gezocht (het donkerste doorlopende gebied onder het dek); lukt dat niet,
    dan geldt een vaste boog. Waarde 1 = ongemoeid, lager = donkerder.
    """
    from mathutils.kdtree import KDTree
    ch, cw = known.shape
    lum = crop[..., :3] @ np.array([.299, .587, .114])
    threshold = .45 * np.median(lum[known])
    # Bovenrand van de binnenste band per kolom: van boven naar beneden het
    # eerste stuk van minstens zes donkere pixels (rij 0 = onderkant).
    edge = np.full(cw, np.nan)
    for j in range(cw):
        column = (lum[:, j] < threshold) & known[:, j]
        run = 0
        for i in range(ch - 1, -1, -1):
            run = run + 1 if column[i] else 0
            if run >= 6: edge[j] = i + 5; break
    good = ~np.isnan(edge)
    cols = np.arange(cw)
    if good.sum() > cw * .3:
        fit = np.polyfit(cols[good], edge[good], 2)
    else:
        fit = np.array([0., .45 * ch / cw, .15 * ch])
    curve = np.polyval(fit, cols)
    # Punten van de boog en hun booglengte.
    points = np.stack([cols, curve], axis=1)
    arc = np.concatenate([[0], np.cumsum(np.linalg.norm(np.diff(points, axis=0), axis=1))])
    tree = KDTree(len(points))
    for k, (x, y) in enumerate(points): tree.insert((x, y, 0), k)
    tree.balance()
    ii, jj = np.nonzero(known)
    distance = np.zeros(len(ii)); along = np.zeros(len(ii))
    for n, (i, j) in enumerate(zip(ii, jj)):
        co, k, d = tree.find((j, i, 0))
        distance[n] = d if i >= curve[j] else -d          # boven de band positief
        along[n] = arc[k]
    # Rijen: hoogte h0 bij de band, per rij naar de schouder q keer zo klein.
    h0, q = .105 * cw, .90
    above = distance > 0
    r = np.maximum(distance, 0)
    row_float = np.log(np.maximum(1 - r * (1 - q) / h0, 1e-6)) / math.log(q)
    row = np.floor(row_float); height = h0 * q ** row
    b = row_float - row                                      # 0 = punt (naar de band), 1 = basis
    spacing = 1.10 * height
    u = along / spacing + .5 * (row % 2)
    a = u - np.floor(u)                                     # dwars over de veer
    rng = np.random.default_rng(seed)
    jitter = rng.uniform(-.06, .06, size=(4096,))[(np.floor(u).astype(int) * 131 + row.astype(int) * 17) % 4096]
    a = a + jitter
    # Vlek op de punt van elke veer: een ruitvorm die met de verspringende
    # rijen het schaakbordachtige net geeft; iets smaller aan de basiskant en
    # een fractie van de onderrand weg (de lichte zoom). Een lichte golving
    # houdt de rand onregelmatig, zoals echte pigmentgrenzen.
    wobble = .035 * np.sin(along * .31 + row * 1.7) + .025 * np.sin(distance * .47 + along * .11)
    shape = rng.uniform(.9, 1.12, size=(4096,))[(np.floor(u).astype(int) * 53 + row.astype(int) * 11) % 4096]
    across = np.abs(a - .5) / ((.50 + 2 * wobble) * shape)
    lengthwise = np.abs(b - .45) / np.where(b < .45, .40, .50) / shape
    inside = np.clip((1 - across - lengthwise) / .45, 0, 1)
    inside = inside * inside * (3 - 2 * inside)
    strength = .60 * np.clip(1 - row / 10, .30, 1) * rng.uniform(.85, 1.1, size=(4096,))[(np.floor(u).astype(int) * 29 + row.astype(int) * 7) % 4096]
    top = distance.max() if above.any() else 1
    fade = np.clip((top - distance) / (.18 * top), 0, 1)     # vervagen naar de bovenrand van het dek
    mark = np.where(above, inside * strength * fade, 0.)
    value = np.ones((ch, cw)); value[ii, jj] = 1 - mark
    return value, {'band_found': bool(good.sum() > cw * .3), 'band_fit': [float(c) for c in fit], 'rows': int(row[above].max() + 1) if above.any() else 0}

def apply_checker(rig):
    """Kennispartner als geblokte stadsduif. Het vleugeldek krijgt een eigen
    kopie van zijn kleurbeeld, alleen het gebied dat het dek gebruikt, met het
    checker-patroon erin. Het patroon hangt aan de 3D-rustpositie van elke
    texel (door de driehoeken van het dek in de uitsnede te rasteren), dus aan
    de echte maat van de vleugel en niet aan de pixels van de basis. Een eigen
    materiaal en een tweede UV-laag wijzen naar de uitsnede (TEXCOORD_1,
    kern-glTF); normal en ruwheid blijven op de oorspronkelijke UV's."""
    shield, shoulders = wing_shield(rig)
    if not shield: return {'checker': 'geen vleugeldek gevonden'}
    uvs = np.concatenate([np.array([d.uv[:] for d in ob.data.uv_layers[0].data]) for ob in shield])
    lo = uvs.min(0) - .01; hi = uvs.max(0) + .01
    material = shield[0].material_slots[0].material
    base = next(n for n in material.node_tree.nodes if n.type == 'TEX_IMAGE' and n.image.colorspace_settings.name == 'sRGB')
    src = base.image; w, h = src.size
    px = np.array(src.pixels[:], np.float32).reshape(h, w, 4)
    x0, x1 = max(0, int(lo[0] * w)), min(w, int(math.ceil(hi[0] * w))); y0, y1 = max(0, int(lo[1] * h)), min(h, int(math.ceil(hi[1] * h)))
    crop = px[y0:y1, x0:x1].copy(); ch, cw = crop.shape[:2]
    # Rustpositie per texel: driehoeken van één vleugel rasteren (de andere
    # vleugel gebruikt dezelfde UV's en ligt spiegelsymmetrisch in x en z).
    position = np.full((ch, cw, 3), np.nan)
    ob = shield[0]; points = with_rest_pose(rig, lambda: evaluated(ob)[0])
    ob.data.calc_loop_triangles(); uv = ob.data.uv_layers[0].data
    for tri in ob.data.loop_triangles:
        t = np.array([(uv[l].uv[0] * w - x0, uv[l].uv[1] * h - y0) for l in tri.loops]); p = points[list(tri.vertices)]
        xmin, ymin = np.floor(t.min(0)).astype(int); xmax, ymax = np.ceil(t.max(0)).astype(int)
        xs, ys = np.meshgrid(np.arange(max(0, xmin), min(cw, xmax + 1)), np.arange(max(0, ymin), min(ch, ymax + 1)))
        if not xs.size: continue
        (ax, ay), (bx, by), (cx, cy) = t; det = (by - cy) * (ax - cx) + (cx - bx) * (ay - cy)
        if abs(det) < 1e-9: continue
        l1 = ((by - cy) * (xs + .5 - cx) + (cx - bx) * (ys + .5 - cy)) / det
        l2 = ((cy - ay) * (xs + .5 - cx) + (ax - cx) * (ys + .5 - cy)) / det; l3 = 1 - l1 - l2
        inside = (l1 >= -.02) & (l2 >= -.02) & (l3 >= -.02)
        position[ys[inside], xs[inside]] = (l1[inside, None] * p[0] + l2[inside, None] * p[1] + l3[inside, None] * p[2])
    # Gaten tussen driehoeken en de rand dichtzetten met de buren.
    for _ in range(12):
        missing = np.isnan(position[..., 0])
        if not missing.any(): break
        padded = np.pad(position, ((1, 1), (1, 1), (0, 0)), constant_values=np.nan)
        stack = np.stack([padded[1 + dy:ch + 1 + dy, 1 + dx:cw + 1 + dx] for dy in (-1, 0, 1) for dx in (-1, 0, 1)])
        with np.errstate(invalid='ignore'): fill = np.nanmean(stack, axis=0)
        position = np.where(missing[..., None], fill, position)
    known = ~np.isnan(position[..., 0])
    value, pattern_info = checker_rows(crop, known)
    # Zachte stofrand: vervagen, zoals pigment in veren vervloeit.
    for _ in range(9):
        padded = np.pad(value, 1, mode='edge')
        value = sum(padded[1 + dy:ch + 1 + dy, 1 + dx:cw + 1 + dx] * wgt for (dy, dx), wgt in
                    zip([(dy, dx) for dy in (-1, 0, 1) for dx in (-1, 0, 1)], [1, 2, 1, 2, 4, 2, 1, 2, 1])) / 16
    # Aan de rand van het dek (waar het tegen het lijf ligt) zacht uitlopen.
    inner = known.astype(np.float64)
    for _ in range(28):
        padded = np.pad(inner, 1, constant_values=0)
        inner = np.minimum(inner, np.minimum.reduce([padded[1 + dy:ch + 1 + dy, 1 + dx:cw + 1 + dx] for dy in (-1, 0, 1) for dx in (-1, 0, 1)]) + 1 / 28)
    value = 1 - (1 - value) * np.clip(inner, 0, 1)
    crop[..., :3] *= value[..., None]
    if os.environ.get('DUIVEN_DEBUG_CHECKER'):
        extent = np.nanmax(position.reshape(-1, 3), 0) - np.nanmin(position.reshape(-1, 3), 0)
        print('CHECKER_DEBUG', crop.shape, 'known', known.mean(), 'extent', extent)
        save_pixels(os.path.join(OUT, 'checker-waarde.png'), np.dstack([value, value, value, np.ones_like(value)]))
    image = bpy.data.images.new(src.name + ' — geblokt', width=cw, height=ch)
    image.pixels = crop.astype(np.float32).ravel()
    image.filepath_raw = os.path.join(OUT, 'vleugeldek-geblokt.jpg'); image.file_format = 'JPEG'; image.save(quality=92); image.pack()
    # Eigen materiaal voor het dek: elk beeld (kleur, normal, ruwheid) wordt tot
    # hetzelfde UV-gebied uitgesneden en de UV's van het dek wijzen daarheen.
    # Zo blijft alles op één UV-laag (TEXCOORD_0).
    checkered = material.copy(); checkered.name = material.name + ' — geblokt'
    box = (x0 / w, y0 / h, x1 / w, y1 / h)
    for node in checkered.node_tree.nodes:
        if node.type != 'TEX_IMAGE' or node.image is None: continue
        if node.image == src: node.image = image; continue
        other = node.image; ow, oh = other.size
        opx = np.array(other.pixels[:], np.float32).reshape(oh, ow, 4)
        ox0, oy0 = int(box[0] * ow), int(box[1] * oh); ox1, oy1 = max(ox0 + 1, int(math.ceil(box[2] * ow))), max(oy0 + 1, int(math.ceil(box[3] * oh)))
        piece = opx[oy0:oy1, ox0:ox1]
        cut = bpy.data.images.new(other.name + ' — dek', width=piece.shape[1], height=piece.shape[0])
        cut.colorspace_settings.name = other.colorspace_settings.name
        cut.pixels = piece.astype(np.float32).ravel()
        cut.filepath_raw = os.path.join(OUT, 'dek-' + str(len(bpy.data.images)) + '.png'); cut.file_format = 'PNG'; cut.save(); cut.pack()
        node.image = cut
        # De uitsnede van een kleiner beeld valt op hele pixels; de UV's volgen die van het kleurbeeld,
        # het verschil is minder dan één pixel van het detailbeeld.
    for ob in shield:
        layer = ob.data.uv_layers[0]
        for d in layer.data:
            d.uv = ((d.uv[0] * w - x0) / cw, (d.uv[1] * h - y0) / ch)
        for slot in ob.material_slots:
            if slot.material == material: slot.material = checkered
    return {'checker_objects': [o.name for o in shield], 'checker_crop_px': [int(cw), int(ch)], 'shoulder_bones': shoulders, 'pattern': pattern_info}

def recolor_plumage(body):
    keep = [o for o in bpy.data.objects if o.type == 'MESH' and any(
        k in o.name.lower() for k in ('snavel', 'beak', 'oog', 'eye', 'washuid', 'cere', 'neusgat'))]
    replaced = {}
    for material in bpy.data.materials:
        if not material.use_nodes: continue
        lowered = material.name.lower()
        if any(k in lowered for k in ('poot', 'nagel', 'washuid', 'neusgat', 'stropdas')): continue
        for node in material.node_tree.nodes:
            if node.type != 'TEX_IMAGE' or node.image is None: continue
            image = node.image
            if image.colorspace_settings.name != 'sRGB': continue
            if image.name not in replaced:
                pixels = image_array(image); rgb = pixels[..., :3]
                users = [o for o in keep if any(s.material and any(n.type == 'TEX_IMAGE' and n.image == image
                         for n in s.material.node_tree.nodes) for s in o.material_slots)]
                mask = protect_mask(image, users)[..., None]
                darken = None
                if any(s.material == material for s in body.material_slots):
                    # Kop en hals donkerder tot de kop niet lichter is dan de borst.
                    zmap = height_map(body, image); top = zmap.max()
                    first = slate(rgb)
                    lum = first @ np.array([.299, .587, .114])
                    head = zmap > top - .35; breast = (zmap > .85) & (zmap < 1.25)
                    ratio = float(lum[head].mean() / max(lum[breast].mean(), 1e-6))
                    strength = max(0., 1 - .92 / ratio)
                    ramp = np.clip((zmap - (top - .75)) / .40, 0, 1); ramp = ramp * ramp * (3 - 2 * ramp)
                    darken = strength * ramp
                    print('LEIGRIJS_KOP', round(ratio, 3), 'donkerder', round(strength, 3))
                # Slagveren zijn al donker; zij krijgen dezelfde koele kromme.
                new = slate(rgb, darken) * (1 - mask) + rgb * mask
                copy = bpy.data.images.new(image.name + ' — leigrijs', width=image.size[0], height=image.size[1])
                copy.pixels = np.concatenate((new, pixels[..., 3:]), axis=2).astype(np.float32).ravel()
                copy.filepath_raw = os.path.join(OUT, 'leigrijs-' + str(len(replaced)) + '.jpg')
                copy.file_format = 'JPEG'; copy.save(quality=92); copy.pack()
                replaced[image.name] = copy
            node.image = replaced[image.name]
    return sorted(replaced)


# ---------------------------------------------------------------------------
# GLB: export en controle tegen de webrandvoorwaarden.

def export_glb(root, path):
    bpy.ops.object.select_all(action='DESELECT')
    def select_tree(ob):
        ob.select_set(True)
        for c in ob.children: select_tree(c)
    select_tree(root)
    bpy.ops.export_scene.gltf(filepath=path, export_format='GLB', use_selection=True, export_apply=False,
        export_animations=True, export_animation_mode='ACTIONS', export_merge_animation='ACTION',
        export_extras=True, export_frame_range=False, export_anim_slide_to_zero=True,
        export_vertex_color='MATERIAL')
    exact_bind_pose(path)

def unit_float32(q):
    """Quaternion op één-norm in float32, zonder dat de norm net onder 1 zakt
    (three.js ziet dan een hoek > 0 met zichzelf)."""
    q = np.asarray(q, np.float64); q = (q / np.linalg.norm(q)).astype(np.float32)
    while float(np.dot(q.astype(np.float64), q.astype(np.float64))) < 1.:
        k = int(np.argmax(np.abs(q))); q[k] = np.nextafter(q[k], np.float32(np.copysign(2, q[k])))
    return q

def exact_bind_pose(path):
    """Na de export: rotaties op één-norm en de eerste en laatste sleutel van
    elke rotatie bitgelijk aan de rustrotatie van de node. De exporter rondt
    pose en rust apart af (tot 7e-4 rad), de runtime eist 1e-6. Dezelfde
    correctie als exact_bind_pose() in build_pigeon_v4.py; alleen deze bytes
    veranderen."""
    raw = bytearray(open(path, 'rb').read()); length = struct.unpack_from('<I', raw, 12)[0]
    data = json.loads(bytes(raw[20:20 + length])); base = 28 + length
    for node in data['nodes']:
        if 'rotation' in node: node['rotation'] = [float(x) for x in unit_float32(node['rotation'])]
    for animation in data.get('animations', []):
        for channel in animation['channels']:
            if channel['target']['path'] != 'rotation': continue
            accessor = data['accessors'][animation['samplers'][channel['sampler']]['output']]; view = data['bufferViews'][accessor['bufferView']]
            offset = base + view.get('byteOffset', 0) + accessor.get('byteOffset', 0)
            keys = np.frombuffer(bytes(raw[offset:offset + 16 * accessor['count']]), np.float32).reshape(-1, 4).copy()
            keys = np.array([unit_float32(k) for k in keys], np.float32)
            rest = unit_float32(data['nodes'][channel['target']['node']].get('rotation', [0, 0, 0, 1]))
            for end in (0, -1):
                assert 2 * math.acos(min(1., abs(float(keys[end].astype(np.float64) @ rest.astype(np.float64))))) < 2e-3, (channel, end)
                keys[end] = rest
            raw[offset:offset + keys.nbytes] = keys.tobytes()
            accessor['min'] = [float(x) for x in keys.min(axis=0)]; accessor['max'] = [float(x) for x in keys.max(axis=0)]
    text = json.dumps(data, separators=(',', ':')).encode(); text += b' ' * ((4 - len(text) % 4) % 4)
    out = raw[:12] + struct.pack('<II', len(text), 0x4E4F534A) + text + raw[20 + length:]
    struct.pack_into('<I', out, 8, len(out))
    open(path, 'wb').write(out)

def inspect_glb(path):
    raw = open(path, 'rb').read(); length, kind = struct.unpack_from('<II', raw, 12)
    data = json.loads(raw[20:20 + length]); binary = raw[28 + length:]
    def floats(index):
        a = data['accessors'][index]; v = data['bufferViews'][a['bufferView']]
        n = {'SCALAR': 1, 'VEC3': 3, 'VEC4': 4}[a['type']]
        return np.frombuffer(binary, np.float32, a['count'] * n, v.get('byteOffset', 0) + a.get('byteOffset', 0)).reshape(a['count'], n)
    clips = {}
    for anim in data.get('animations', []):
        duration = 0; boundary = True
        for ch in anim['channels']:
            sampler = anim['samplers'][ch['sampler']]
            times = floats(sampler['input']); out = floats(sampler['output'])
            duration = max(duration, float(times.max()))
            boundary &= bool(np.allclose(out[0], out[-1], atol=1e-6))
        clips[anim['name']] = {'channels': len(anim['channels']), 'duration_seconds': round(duration, 6), 'first_last_equal': boundary}
    images = data.get('images', [])
    evidence = {
        'bytes': len(raw), 'mib': round(len(raw) / 2 ** 20, 3),
        'sha256': hashlib.sha256(raw).hexdigest(),
        'meshes': len(data['meshes']),
        'triangles': sum(data['accessors'][p['indices']]['count'] // 3 for m in data['meshes'] for p in m['primitives']),
        'materials': sorted(m.get('name', '') for m in data.get('materials', [])),
        'images': len(images),
        'image_bytes': sum(data['bufferViews'][i['bufferView']]['byteLength'] for i in images if 'bufferView' in i),
        'clips': clips, 'skins': len(data.get('skins', [])),
        'joints': len(data['skins'][0]['joints']) if data.get('skins') else 0,
    }
    problems = []
    if len(raw) > 12 * 2 ** 20: problems.append('groter dan 12 MiB')
    if len(data.get('buffers', [])) != 1: problems.append('niet precies één buffer')
    if any('uri' in b for b in data.get('buffers', [])) or any('uri' in i for i in images): problems.append('externe uri')
    if data.get('extensionsUsed') or data.get('extensionsRequired'): problems.append('glTF-extensies: ' + str(data.get('extensionsUsed')))
    if any('bufferView' not in i for i in images): problems.append('afbeelding zonder bufferView')
    evidence['web_constraints_ok'] = not problems; evidence['problems'] = problems
    return evidence


# ---------------------------------------------------------------------------
# Camera's. De vergadercamera is framePigeon uit pigeonFraming.ts, hier in
# Python nagebouwd op dezelfde omhulling als cameraEnvelope.ts: dertien
# gelijk verdeelde momenten per clip plus de geschreven pieken, alle vervormde
# vertices, 3,5 % marge rond het midden. glTF-assen: Y omhoog, snavel naar −X;
# Blender (x, y, z) = glTF (x, −z, y).

# azimut/elevatie in radialen, conventie van framePigeon zonder spiegeling
# (facing 'left'): azimut 0 kijkt vanaf +Z (zijaanzicht, snavel links in
# beeld), −π/2 recht van voren.
VIEWS = {
    'signalerend': {'azimuth': -.57, 'elevation': .105, 'fov': 32,
                    'intent': 'zijaanzicht, snavel naar links (ongewijzigd DEFAULT_AZIMUTH/ELEVATION)',
                    # Kopportret in zuiverder zijprofiel: op 37 px is het profiel het kenmerk.
                    'head_azimuth': -.25, 'head_elevation': .06},
    # Vanaf de andere flank (−Z) in plaats van gespiegeld: −(π − 1,30). De
    # snavel wijst naar rechts; het hoofdlicht verhuist mee (zie setup_lights).
    'individualistisch': {'azimuth': -(math.pi - 1.30), 'elevation': .13, 'fov': 32,
                          'intent': 'driekwart van voren vanaf de −Z-flank (15° uit frontaal), snavel naar rechts'},
    'biologisch': {'azimuth': -math.pi / 2, 'elevation': .12, 'fov': 32,
                   'intent': 'recht van voren',
                   # Het kopportret 4° gedraaid, zodat één iris net zichtbaar is.
                   'head_azimuth': -1.50, 'head_elevation': .08},
}
POSTER = (1400, 1100)

def to_gltf(points): return np.stack([points[:, 0], points[:, 2], -points[:, 1]], axis=1)
def to_blender(v): return Vector((v[0], -v[2], v[1]))

def all_points():
    return np.concatenate([evaluated(o)[0] for o in bpy.data.objects if o.type == 'MESH' and o.visible_get()])

def basis_peaks():
    """extras.peaks uit de GLB naast de basis-.blend (zelfde naam), als die er is:
    de modelbouwer zet de piekmomenten daar, niet op de acties in de .blend."""
    path = os.path.splitext(BASIS)[0] + '.glb'
    if not os.path.exists(path): return {}
    raw = open(path, 'rb').read(); length = struct.unpack_from('<I', raw, 12)[0]
    data = json.loads(raw[20:20 + length])
    return {a['name']: [float(t) for t in a.get('extras', {}).get('peaks', [])] for a in data.get('animations', [])}

def measure_peaks(rig):
    """Piekmomenten per clip, gemeten op alle frames, als eigenschap 'peaks' op
    de actie; de glTF-export schrijft die als extras.peaks, die de runtime
    (cameraEnvelope.ts) als extra camera-omhulling leest.

    Een piek is een frame waarop de duif in een richting het verst reikt: voor
    32 richtingen (horizontaal rondom, schuin omhoog en omlaag) het frame met
    de grootste steunfunctie. Bestaande 'peaks' van de basis blijven staan; de
    gemeten momenten komen erbij (de stropdas kan het silhouet veranderen).
    """
    fps = bpy.context.scene.render.fps; out = {}
    directions = []
    for elev in (-.6, -.3, 0., .3, .6):
        for k in range(16):
            az = 2 * math.pi * k / 16
            directions.append((math.cos(az) * math.cos(elev), math.sin(az) * math.cos(elev), math.sin(elev)))
    directions += [(0, 0, 1), (0, 0, -1)]
    # Ook de beeldassen van de vergadercamera's (rechts, omhoog, beide kanten
    # op), over het sleepbereik van de interface (azimut ±0,34).
    for view in VIEWS.values():
        for daz in (-.34, 0., .34):
            az, el = view['azimuth'] + daz, view['elevation']
            outward = np.array([math.sin(az) * math.cos(el), math.sin(el), math.cos(az) * math.cos(el)])
            right = np.cross([0, 1, 0], outward); right /= np.linalg.norm(right); up = np.cross(outward, right)
            for axis in (right, up):
                g = np.array([axis[0], -axis[2], axis[1]])           # glTF → Blender
                directions += [tuple(g), tuple(-g)]
    directions = np.array(directions)
    for action in sorted(bpy.data.actions, key=lambda a: a.name):
        use_action(rig, action); frames = clip_frames(action); support = []
        for frame in frames:
            bpy.context.scene.frame_set(frame)
            support.append((all_points() @ directions.T).max(0))
        support = np.array(support); best = sorted(set(int(i) for i in support.argmax(0)))
        best = [i for i in best if 0 < i < len(frames) - 1]           # rust zit al in de omhulling
        measured = [round((frames[i] - frames[0]) / fps, 4) for i in best]
        given = [float(t) for t in action['peaks']] if action.get('peaks') is not None else []
        given += basis_peaks().get(action.name, [])
        peaks = sorted(set(given + measured))
        action['peaks'] = peaks
        out[action.name] = peaks
    rig.animation_data.action = None; bpy.context.scene.frame_set(bpy.context.scene.frame_start)
    print('PEAKS', json.dumps(out))
    return out

def envelope(rig):
    samples = []
    authored = {'Peck': [1.0], 'WingWave': [34 / 30, 58 / 30]}
    fps = bpy.context.scene.render.fps
    for action in sorted(bpy.data.actions, key=lambda a: a.name):
        use_action(rig, action); start, end = action.frame_range; duration = (end - start) / fps
        # Piekmomenten zoals de runtime ze leest: extras.peaks van de glTF-
        # animatie (in Blender een eigen eigenschap 'peaks' op de actie), anders
        # de v3-momenten.
        peaks = action.get('peaks')
        peaks = [float(t) for t in peaks] if peaks is not None else authored.get(action.name, [])
        times = sorted(set([duration * i / 12 for i in range(13)] + [t for t in peaks if t < duration]))
        for t in times:
            bpy.context.scene.frame_set(int(start), subframe=0)
            frame = start + t * fps; bpy.context.scene.frame_set(int(math.floor(frame)), subframe=frame - math.floor(frame))
            samples.append(to_gltf(all_points()))
    rig.animation_data.action = None; bpy.context.scene.frame_set(bpy.context.scene.frame_start)
    points = np.concatenate(samples)
    lo, hi = points.min(0), points.max(0); center = (lo + hi) / 2
    return center + (points - center) * 1.035

STAGE_COMPOSITION = {'envelope': {'horizontal': .95, 'vertical': .94}, 'fill': {'horizontal': .9, 'vertical': .8}}

def load_composition():
    """De kadreerfactoren uit stage-composition.json van de interface (de enige
    geschreven bron), of de gelijke standaardwaarden als dat bestand er niet is."""
    path = option('--composition') or os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..',
        'module', 'duif', 'frontend', 'src', 'lib', 'media', 'model', 'stage-composition.json')
    if os.path.exists(path):
        data = json.load(open(path))
        return {'envelope': data['envelope'], 'fill': data['fill']}, os.path.abspath(path)
    return STAGE_COMPOSITION, None

def frame_pigeon(points, aspect, azimuth, elevation, fov=32, horizontal=.86, vertical=.78, rest=None, composition=None, center=None):
    """framePigeon() uit pigeonFraming.ts in de frontend van de module.

    Zonder `composition`: de oude passing (marges `horizontal`/`vertical`), die
    de kaartcamera nog gebruikt. Met `composition` en `rest`: het
    vergadercontract. Afstand is het maximum van vier passingen (omhulling in
    envelope, rustpose in fill); horizontaal de omhulling symmetrisch;
    verticaal de rustpose symmetrisch, geklemd zodat de omhulling binnen de
    envelope-marge blijft. `center` is het midden van de rustbounds (Box3 van
    het model in rust).
    """
    lo, hi = points.min(0), points.max(0); size = hi - lo
    target = np.array(center) if center is not None else (lo + hi) / 2
    outward = np.array([math.sin(azimuth) * math.cos(elevation), math.sin(elevation), math.cos(azimuth) * math.cos(elevation)])
    right = np.cross([0, 1, 0], outward); right /= np.linalg.norm(right)
    up = np.cross(outward, right); up /= np.linalg.norm(up)
    tangent = math.tan(math.radians(fov / 2))
    def project(source):
        rel = source - target
        return rel @ right, rel @ up, rel @ outward
    def fit(values, depth, k):
        return ((values + k * depth).max() - (values - k * depth).min()) / (2 * k)
    across, height, depth = project(points)
    if composition is None:
        h, v = tangent * aspect * horizontal, tangent * vertical
        distance = max(fit(across, depth, h), fit(height, depth, v), depth.max() + np.linalg.norm(size) * .01)
    else:
        margin, fill = composition['envelope'], composition['fill']
        distance = max(fit(across, depth, tangent * aspect * margin['horizontal']), fit(height, depth, tangent * margin['vertical']),
                       depth.max() + np.linalg.norm(size) * .01)
        s_across, s_height, s_depth = project(rest)
        distance = max(distance, fit(s_across, s_depth, tangent * aspect * fill['horizontal']), fit(s_height, s_depth, tangent * fill['vertical']))
    def symmetric(values, deep):
        low, high = values.min(), values.max()
        for _ in range(40):
            middle = (low + high) / 2
            projected = (values - middle) / (distance - deep)
            if projected.max() > -projected.min(): low = middle
            else: high = middle
        return (low + high) / 2
    lift = symmetric(height, depth)
    if composition is not None:
        k = tangent * composition['envelope']['vertical']
        lowest = (height - k * (distance - depth)).max(); highest = (height + k * (distance - depth)).min()
        lift = min(max(symmetric(s_height, s_depth), lowest), max(lowest, highest))
    target = target + right * symmetric(across, depth) + up * lift
    position = target + outward * distance
    return position, target, float(distance)

def place_camera(camera, position_g, target_g, fov):
    camera.data.type = 'PERSP'; camera.data.sensor_fit = 'VERTICAL'
    camera.data.sensor_height = 24; camera.data.lens = 12 / math.tan(math.radians(fov / 2))
    camera.data.clip_start = .01; camera.data.clip_end = 100
    camera.location = to_blender(position_g)
    camera.rotation_euler = (to_blender(target_g) - camera.location).to_track_quat('-Z', 'Y').to_euler()

def fit_circle(points, azimuth, elevation, fov, fill, drop=0.):
    """Camera zodat alle punten binnen een cirkel met straal `fill` (fractie
    van het halve beeld) vallen; midden op het midden van de punten."""
    outward = np.array([math.sin(azimuth) * math.cos(elevation), math.sin(elevation), math.cos(azimuth) * math.cos(elevation)])
    right = np.cross([0, 1, 0], outward); right /= np.linalg.norm(right)
    up = np.cross(outward, right)
    tangent = math.tan(math.radians(fov / 2))
    target = (points.min(0) + points.max(0)) / 2; distance = 3.0
    for _ in range(30):
        rel = points - (target + outward * distance)
        z = -(rel @ outward); x = (rel @ right) / (z * tangent); y = (rel @ up) / (z * tangent)
        cx, cy = (x.max() + x.min()) / 2, (y.max() + y.min()) / 2
        target = target + right * cx * distance * tangent + up * cy * distance * tangent
        radius = np.sqrt((x - cx) ** 2 + (y - cy) ** 2).max()
        distance *= radius / fill
    # Kop iets boven het midden: de hals loopt onder uit de cirkel.
    target = target - up * drop * fill * distance * tangent
    return target + outward * distance, target

def projected(points, position, target, fov, aspect=1.0):
    """Beeldcoördinaten (0..1, y omlaag) van glTF-punten in een camera."""
    forward = target - position; forward /= np.linalg.norm(forward)
    right = np.cross(forward, [0, 1, 0]); right /= np.linalg.norm(right)
    up = np.cross(right, forward)
    rel = points - position; z = rel @ forward; tangent = math.tan(math.radians(fov / 2))
    x = (rel @ right) / (z * tangent * aspect); y = (rel @ up) / (z * tangent)
    return np.stack([(x + 1) / 2, (1 - y) / 2], axis=1)


# ---------------------------------------------------------------------------
# Renders: zelfde lichtopzet (de lampen en wereld van de basis) voor alle drie.

AMBIENT = (.30, .31, .33, 1)
LIGHT_BG = (0xf5 / 255, 0xf7 / 255, 0xf6 / 255)
DARK_BG = (0x14 / 255, 0x17 / 255, 0x16 / 255)

def setup_render(samples):
    scene = bpy.context.scene; scene.render.engine = 'CYCLES'
    try:
        prefs = bpy.context.preferences.addons['cycles'].preferences
        prefs.compute_device_type = 'METAL'; prefs.get_devices()
        for d in prefs.devices: d.use = d.type == 'METAL'
        scene.cycles.device = 'GPU'
    except Exception: scene.cycles.device = 'CPU'
    scene.cycles.samples = samples; scene.cycles.use_denoising = True
    # Zelfde lichtopzet voor alle drie: de twee vlaklampen van de basis plus een
    # lichte, egale omgeving, zoals de hemisfeer en de studio-omgeving van de
    # runtime (pigeonStage.ts). De basis had 0,05; daarmee valt een duif die
    # recht naar de camera kijkt met haar borst in de schaduw.
    if scene.world and scene.world.node_tree:
        background = scene.world.node_tree.nodes.get('Background')
        if background: background.inputs[0].default_value = AMBIENT; background.inputs[1].default_value = 1
    scene.render.film_transparent = True; scene.render.resolution_percentage = 100
    scene.render.image_settings.file_format = 'PNG'; scene.render.image_settings.color_mode = 'RGBA'

def render(path, size):
    scene = bpy.context.scene
    scene.render.resolution_x, scene.render.resolution_y = size
    scene.render.filepath = path; bpy.ops.render.render(write_still=True)
    return path

def load_pixels(path):
    image = bpy.data.images.load(path, check_existing=False)
    w, h = image.size; pixels = np.array(image.pixels[:], np.float32).reshape(h, w, 4)
    bpy.data.images.remove(image); return pixels

def save_pixels(path, pixels):
    h, w = pixels.shape[:2]
    image = bpy.data.images.new(os.path.basename(path), width=w, height=h, alpha=True)
    image.pixels = pixels.astype(np.float32).ravel(); image.filepath_raw = path
    image.file_format = 'PNG'; image.save(); bpy.data.images.remove(image)

def over(pixels, background):
    a = pixels[..., 3:]; out = pixels.copy()
    out[..., :3] = pixels[..., :3] * a + np.array(background) * (1 - a); out[..., 3] = 1
    return out

def montage(paths, columns, background=(.91, .925, .918)):
    tiles = [over(load_pixels(p), background) for p in paths]
    h, w = tiles[0].shape[:2]; rows = math.ceil(len(tiles) / columns)
    sheet = np.ones((rows * h, columns * w, 4), np.float32); sheet[..., :3] = background
    for k, tile in enumerate(tiles):
        r, c = divmod(k, columns)
        # Blender-pixels beginnen onderaan: rij 0 van het blad staat bovenaan.
        sheet[(rows - 1 - r) * h:(rows - r) * h, c * w:(c + 1) * w] = tile
    return sheet

def pose_at(rig, action, seconds):
    use_action(rig, action); fps = bpy.context.scene.render.fps
    frame = action.frame_range[0] + seconds * fps
    bpy.context.scene.frame_set(int(math.floor(frame)), subframe=frame - math.floor(frame))

def rest(rig):
    if rig.animation_data: rig.animation_data.action = None
    for b in rig.pose.bones:
        b.location = (0, 0, 0); b.rotation_quaternion = (1, 0, 0, 0); b.rotation_euler = (0, 0, 0); b.scale = (1, 1, 1)
    bpy.context.scene.frame_set(bpy.context.scene.frame_start)

STRIP_TIMES = {'Peck': [0, .5, .8, 1.0, 1.3, 1.8, 2.4, 3.2], 'WingWave': [0, .6, 1.1333, 1.5, 1.9333, 2.3, 2.7, 3.2]}

def head_region(points):
    """Kop en hals tot de knoophoogte: alles boven 64 % van de hoogte (glTF Y).
    Dezelfde regel voor alle drie; bij de Stakeholder valt zo de hele knoop
    in de cirkel, niet een halve."""
    lo, hi = points[:, 1].min(), points[:, 1].max()
    return points[points[:, 1] > lo + .64 * (hi - lo)]

CARD_HEAD = .27     # kophoogte als fractie van het kaartbeeld, gelijk voor alle drie
CARD_HEAD_TOP = .09 # afstand van kruin tot bovenrand

def equal_head(points, position, target, fov):
    """Kaartcamera zo verplaatsen dat de kop (bovenste 15 % van de duif)
    overal even hoog in beeld staat, met de kruin even ver van de bovenrand."""
    lo, hi = points[:, 1].min(), points[:, 1].max()
    head = points[points[:, 1] > lo + .85 * (hi - lo)]
    outward = (position - target) / np.linalg.norm(position - target)
    distance = np.linalg.norm(position - target)
    for _ in range(12):
        uv = projected(head, target + outward * distance, target, fov)
        height = uv[:, 1].max() - uv[:, 1].min()
        distance *= height / CARD_HEAD
        uv = projected(head, target + outward * distance, target, fov)
        up = np.cross(np.cross(outward, [0, 1, 0]), outward); up /= np.linalg.norm(up)
        # Kruin op CARD_HEAD_TOP van de bovenrand: doel langs de beeld-opas schuiven.
        shift = (uv[:, 1].min() - CARD_HEAD_TOP) * 2 * distance * math.tan(math.radians(fov / 2))
        target = target - up * shift
    return target + outward * distance, target

def bust_region(points, head):
    """Kop, hals en borst: boven 52 % van de hoogte en binnen 0,5 hoogte van
    het kopmidden, zodat bij een zijaanzicht rug en staart niet meetellen."""
    lo, hi = points[:, 1].min(), points[:, 1].max(); center = head.mean(0)
    keep = (points[:, 1] > lo + .52 * (hi - lo)) & (np.linalg.norm(points - center, axis=1) < .5 * (hi - lo))
    return points[keep]

def camera_views(rig, name):
    view = VIEWS[name]; env = envelope(rig); rest(rig)
    aspect = POSTER[0] / POSTER[1]
    rest_points = to_gltf(all_points())
    composition, _ = load_composition()
    position, target, distance = frame_pigeon(env, aspect, view['azimuth'], view['elevation'], view['fov'],
        rest=rest_points, composition=composition, center=(rest_points.min(0) + rest_points.max(0)) / 2)
    # Kaart: borstbeeld voor een cirkel (de interface toont het met
    # object-fit: cover in een rondje). Kop, hals en borst vullen de
    # ingeschreven cirkel; bij de Stakeholder vallen knoop en bovenste deel van
    # de das erbinnen. Dezelfde regel voor alle drie.
    card_fov = 24
    head = head_region(rest_points)
    card_elevation = view.get('card_elevation', view['elevation'] + .05)
    card_pos, card_target = fit_circle(bust_region(rest_points, head), view['azimuth'], card_elevation, card_fov, .93, 0.)
    card_pos, card_target = equal_head(rest_points, card_pos, card_target, card_fov)
    head_pos, head_target = fit_circle(head, view.get('head_azimuth', view['azimuth']), view.get('head_elevation', view['elevation'] + .06), 20, .86, .04)
    return {
        'meeting': {'azimuth': view['azimuth'], 'elevation': view['elevation'], 'fov': view['fov'],
                    'intent': view['intent'], 'poster_aspect': round(aspect, 5),
                    'camera_position_gltf': [round(float(c), 5) for c in position],
                    'camera_target_gltf': [round(float(c), 5) for c in target], 'distance': round(distance, 5)},
        'card': {'fov': card_fov, 'azimuth': view['azimuth'], 'elevation': view.get('card_elevation', view['elevation'] + .05), 'camera_position_gltf': [round(float(c), 5) for c in card_pos],
                 'camera_target_gltf': [round(float(c), 5) for c in card_target]},
        'head': {'fov': 20, 'azimuth': view.get('head_azimuth', view['azimuth']),
                 'elevation': view.get('head_elevation', view['elevation'] + .06), 'camera_position_gltf': [round(float(c), 5) for c in head_pos],
                 'camera_target_gltf': [round(float(c), 5) for c in head_target]},
        '_points': (rest_points, head),
    }

def set_light_flank(azimuth):
    """Het licht aan de kant van de camera, zoals pigeonStage.ts: kijkt de
    camera vanaf −Z (glTF), dan spiegelen de lampen naar die flank. In Blender
    is glTF-Z gelijk aan −Y, dus spiegelen we Y."""
    flank = -1 if math.cos(azimuth) < -1e-6 else 1
    for light in [o for o in bpy.data.objects if o.type == 'LIGHT']:
        if 'rest_location' not in light:
            light['rest_location'] = list(light.location)
            light['rest_direction'] = list(light.matrix_world.to_quaternion() @ Vector((0, 0, -1)))
        location = Vector(light['rest_location']); direction = Vector(light['rest_direction'])
        if flank < 0: location.y = -location.y; direction.y = -direction.y
        light.location = location
        light.rotation_mode = 'QUATERNION'; light.rotation_quaternion = direction.to_track_quat('-Z', 'Y')
    bpy.context.view_layer.update()
    return flank

def rim_light(position_g, target_g, energy=260):
    """Vlaklamp achter en boven de duif, tegenover de camera."""
    position, target = to_blender(position_g), to_blender(target_g)
    behind = target + (target - position).normalized() * 3 + Vector((0, 0, 2.2))
    data = bpy.data.lights.new('Strijklicht portret', 'AREA'); data.energy = energy; data.size = 2.5
    data.color = (.96, .98, 1.0)
    light = bpy.data.objects.new('Strijklicht portret', data); bpy.context.scene.collection.objects.link(light)
    light.location = behind; light.rotation_euler = (target - behind).to_track_quat('-Z', 'Y').to_euler()
    return light

def render_variant(rig, name, views, directory):
    scene = bpy.context.scene; camera = scene.camera; files = {}
    meeting, card, head = views['meeting'], views['card'], views['head']
    views['meeting']['light_flank'] = set_light_flank(meeting['azimuth'])
    rest(rig)
    place_camera(camera, np.array(meeting['camera_position_gltf']), np.array(meeting['camera_target_gltf']), meeting['fov'])
    files['rust'] = render(os.path.join(directory, f'stadsduif-v4-{name}-rust.png'), POSTER)
    place_camera(camera, np.array(card['camera_position_gltf']), np.array(card['camera_target_gltf']), card['fov'])
    # Portretten krijgen een zacht strijklicht van achteren-boven, gelijk voor
    # alle drie: zo blijft ook een donkere duif op een donker thema leesbaar.
    rim = rim_light(np.array(card['camera_position_gltf']), np.array(card['camera_target_gltf']))
    files['kaart'] = render(os.path.join(directory, f'{name}-kaart.png'), (1024, 1024))
    # Visueel gewicht: kophoogte in het kaartbeeld en het gedekte oppervlak.
    rest_points, head_pts = views['_points']
    uv = projected(head_pts, np.array(card['camera_position_gltf']), np.array(card['camera_target_gltf']), card['fov'])
    alpha = load_pixels(files['kaart'])[..., 3]
    views['card']['metrics'] = {'head_height': round(float(uv[:, 1].max() - uv[:, 1].min()), 4),
                                'head_width': round(float(uv[:, 0].max() - uv[:, 0].min()), 4),
                                'coverage': round(float((alpha > .5).mean()), 4),
                                'head_max_radius_of_half_frame': round(float(np.sqrt((((uv - .5) * 2) ** 2).sum(axis=1)).max()), 4)}
    uv = projected(head_pts, np.array(head['camera_position_gltf']), np.array(head['camera_target_gltf']), head['fov'])
    views['head']['metrics'] = {'max_radius_of_half_frame': round(float(np.sqrt((((uv - .5) * 2) ** 2).sum(axis=1)).max()), 4)}
    place_camera(camera, np.array(head['camera_position_gltf']), np.array(head['camera_target_gltf']), head['fov'])
    bpy.data.objects.remove(rim, do_unlink=True)
    rim = rim_light(np.array(head['camera_position_gltf']), np.array(head['camera_target_gltf']))
    files['kop'] = render(os.path.join(directory, f'{name}-kop.png'), (512, 512))
    bpy.data.objects.remove(rim, do_unlink=True)
    # Framestroken in de eigen vergadercamera.
    place_camera(camera, np.array(meeting['camera_position_gltf']), np.array(meeting['camera_target_gltf']), meeting['fov'])
    samples = scene.cycles.samples; scene.cycles.samples = max(16, samples // 3)
    tmp = os.path.join(directory, 'tmp'); os.makedirs(tmp, exist_ok=True)
    for action in sorted(bpy.data.actions, key=lambda a: a.name):
        clip = action.name; paths = []
        for k, t in enumerate(STRIP_TIMES.get(clip, [i * .4 for i in range(9)])):
            pose_at(rig, action, t); paths.append(render(os.path.join(tmp, f'{name}-{clip}-{k}.png'), (700, 550)))
        strip = os.path.join(directory, f'{name}-strook-{clip}.png'); save_pixels(strip, montage(paths, 4)); files['strook-' + clip] = strip
    scene.cycles.samples = samples; rest(rig)
    return files

def render_tie_strips(rig, directory):
    """Stropdas van voren, driekwart-voor en opzij door beide clips."""
    scene = bpy.context.scene; camera = scene.camera; files = {}
    set_light_flank(0.)
    samples = scene.cycles.samples; scene.cycles.samples = max(16, samples // 3)
    tmp = os.path.join(directory, 'tmp'); os.makedirs(tmp, exist_ok=True)
    camera.data.type = 'ORTHO'; camera.data.ortho_scale = 1.25
    knot = next(o for o in bpy.data.objects if o.type == 'MESH' and o.name.startswith('Stropdas knoop'))
    rest(rig); offset = Vector((-.78, 0, 1.25)) - Vector(evaluated(knot)[0].mean(0))
    for label, direction in [('voor', (-1, 0, .12)), ('driekwart', (-.72, -.62, .2)), ('zij', (-.05, -1, .1))]:
        for action in sorted(bpy.data.actions, key=lambda a: a.name):
            paths = []
            for k, t in enumerate(STRIP_TIMES.get(action.name, [i * .4 for i in range(9)])):
                pose_at(rig, action, t)
                # De camera volgt de knoop: buigt de hele duif voorover, dan blijft de das in beeld.
                target = Vector(evaluated(knot)[0].mean(0)) + offset
                camera.location = target + Vector(direction).normalized() * 5
                camera.rotation_euler = (target - camera.location).to_track_quat('-Z', 'Y').to_euler()
                paths.append(render(os.path.join(tmp, f'stropdas-{label}-{action.name}-{k}.png'), (480, 560)))
            path = os.path.join(directory, f'stropdas-strook-{label}-{action.name}.png'); save_pixels(path, montage(paths, 8)); files[f'{label}-{action.name}'] = path
    scene.cycles.samples = samples; rest(rig)
    return files

def preview_sheet(directory, names, kind, sizes, circle=False):
    """Portretten verkleind (Lanczos, ffmpeg) op licht en donker, zij aan zij."""
    import subprocess
    rows = []
    for background in (LIGHT_BG, DARK_BG):
        for size in sizes:
            tiles = []
            for name in names:
                small = os.path.join(directory, 'tmp', f'{name}-{kind}-{size}.png')
                subprocess.run(['ffmpeg', '-y', '-loglevel', 'error', '-i', os.path.join(directory, f'{name}-{kind}.png'),
                                '-vf', f'scale={size}:{size}:flags=lanczos', small], check=True)
                tile = load_pixels(small)
                if circle:
                    yy, xx = np.mgrid[0:size, 0:size] + .5
                    tile[..., 3] *= (np.hypot(xx - size / 2, yy - size / 2) <= size / 2).astype(np.float32)
                tiles.append(over(tile, background))
            pad = max(8, size // 6); h = size + 2 * pad; w = len(tiles) * (size + pad) + pad
            row = np.ones((h, w, 4), np.float32); row[..., :3] = background
            for k, tile in enumerate(tiles): row[pad:pad + size, pad + k * (size + pad):pad + k * (size + pad) + size] = tile
            rows.append(row)
    width = max(r.shape[1] for r in rows)
    rows = [np.concatenate([r, np.tile(r[:, -1:], (1, width - r.shape[1], 1))], axis=1) if r.shape[1] < width else r for r in rows]
    sheet = np.concatenate(rows[::-1], axis=0)       # Blender: onderste rij eerst
    path = os.path.join(directory, f'voorbeeld-{kind}.png'); save_pixels(path, sheet); return path


# ---------------------------------------------------------------------------

def sha256(path): return hashlib.sha256(open(path, 'rb').read()).hexdigest()

def reimport(glb):
    """De basisscène (lampen, wereld, kleurbeheer) met alleen de GLB erin."""
    bpy.ops.wm.open_mainfile(filepath=BASIS)
    for ob in list(bpy.data.objects):
        if ob.type not in {'LIGHT', 'CAMERA'}: bpy.data.objects.remove(ob, do_unlink=True)
    for action in list(bpy.data.actions): bpy.data.actions.remove(action)
    bpy.data.orphans_purge(do_recursive=True)
    bpy.context.scene.render.fps = 30
    bpy.ops.import_scene.gltf(filepath=glb)
    # De importer zet extras.peaks niet op de acties; de runtime leest ze wel.
    # Zelf overnemen, zodat de posteromhulling dezelfde momenten gebruikt.
    raw = open(glb, 'rb').read(); length = struct.unpack_from('<I', raw, 12)[0]
    for animation in json.loads(raw[20:20 + length]).get('animations', []):
        peaks = animation.get('extras', {}).get('peaks')
        action = bpy.data.actions.get(animation['name'])
        if peaks is not None and action is not None: action['peaks'] = [float(t) for t in peaks]
    rig = find_rig()
    if rig.animation_data:
        for track in rig.animation_data.nla_tracks: track.mute = True
        rig.animation_data.action = None
    return rig

def look():
    """Snelle stropdasstudie op de basis: meten en close-ups die de knoop volgen."""
    rig = find_rig(); body = find_body(rig)
    parts, reference, bake = add_tie(rig, body)
    if '--measure' in ARGS:
        report = measure_tie(rig, body, reference, 'bron')
        json.dump(report, open(os.path.join(OUT, 'look-meting.json'), 'w'), indent=1)
    setup_render(SAMPLES); scene = bpy.context.scene; camera = scene.camera
    camera.data.type = 'ORTHO'
    knot = bpy.data.objects['Stropdas knoop']
    def shoot(label, direction, scale, lift=0.):
        target = Vector(evaluated(knot)[0].mean(0)) + Vector((0, 0, lift)); camera.data.ortho_scale = scale
        camera.location = target + Vector(direction).normalized() * 5
        camera.rotation_euler = (target - camera.location).to_track_quat('-Z', 'Y').to_euler()
        render(os.path.join(OUT, f'look-{label}.png'), (900, 900))
    rest(rig)
    shoot('voor', (-1, 0, .1), 1.1, -.3); shoot('driekwart', (-.72, -.62, .2), 1.1, -.3)
    shoot('zij', (-.05, -1, .08), 1.1, -.3); shoot('knoop', (-.9, -.35, .22), .30)
    shoot('knoop-zij', (-.3, -1, .1), .30); shoot('kop', (-1, -.25, .1), .7, .1)
    for clip, seconds in [('Peck', 1.0), ('WingWave', 1.1333)]:
        pose_at(rig, bpy.data.actions[clip], seconds)
        shoot(f'{clip}-driekwart', (-.72, -.62, .2), 1.2, -.2); shoot(f'{clip}-zij', (-.05, -1, .08), 1.2, -.2)
    rest(rig)

def main():
    started = time.time()
    assert BASIS and os.path.exists(BASIS), 'open een basis-.blend: Blender --background <basis.blend> --python ...'
    evidence = {'basis': BASIS, 'basis_sha256': sha256(BASIS), 'blender': bpy.app.version_string,
                'script': 'tools/design-model/variants_v4.py', 'variants': {}}
    measurements = {}; reference = None; glbs = {}
    for name in VARIANTS:
        bpy.ops.wm.open_mainfile(filepath=BASIS)
        rig = find_rig(); body = find_body(rig); root = find_root(rig); entry = {}
        if name == 'individualistisch':
            parts, reference, entry['tie_bone'] = add_tie(rig, body)
            measurements['bron'] = measure_tie(rig, body, reference, 'bron')
        elif name == 'biologisch':
            entry['recolored_images'] = recolor_plumage(body)
            entry['morph'] = MORPH
            if MORPH == 'checker': entry['checker'] = apply_checker(rig)
        entry['peaks'] = measure_peaks(rig)
        text = bpy.data.texts.new('variants_v4.py'); text.from_string(open(__file__).read())
        glbs[name] = os.path.join(OUT, f'stadsduif-v4-{name}.glb'); export_glb(root, glbs[name])
        bpy.ops.wm.save_as_mainfile(filepath=os.path.join(OUT, f'stadsduif-v4-{name}.blend'), copy=True)
        entry['glb'] = inspect_glb(glbs[name]); entry['glb']['file'] = os.path.basename(glbs[name])
        evidence['variants'][name] = entry
        print('VARIANT_EXPORTED', name, json.dumps(entry['glb']))
    views_out = {'convention': 'pigeonFraming.ts framePigeon(): glTF Y omhoog, snavel naar -X; outward = '
                 '(sin(az)cos(el), sin(el), cos(az)cos(el)); azimut 0 = zijaanzicht vanaf +Z met de snavel links, '
                 '-pi/2 = recht van voren. facing "left" = zonder spiegeling; laadt de runtime met facing "right" '
                 '(wrapper.scale.x = -1), dan hoort daar azimut * -1 bij. fov is verticaal in graden.',
                 'poster_resolution': list(POSTER), 'variants': {}}
    for name in VARIANTS:
        rig = reimport(glbs[name]); body = find_body(rig)
        if name == 'individualistisch' and reference is not None:
            measurements['glb'] = measure_tie(rig, body, reference, 'glb')
        views = camera_views(rig, name); views_out['variants'][name] = views
        if RENDER:
            setup_render(SAMPLES)
            directory = OUT
            files = render_variant(rig, name, views, directory)
            if name == 'individualistisch': files.update(render_tie_strips(rig, directory))
            evidence['variants'][name]['renders'] = {k: os.path.basename(v) for k, v in files.items()}
        views.pop('_points')
    if RENDER and len(VARIANTS) == 3:
        order = ['signalerend', 'individualistisch', 'biologisch']
        evidence['previews'] = [os.path.basename(preview_sheet(OUT, order, 'kaart', [288, 64, 43], circle=True)),
                                os.path.basename(preview_sheet(OUT, order, 'kop', [128, 37], circle=True))]
    if measurements:
        json.dump(measurements, open(os.path.join(OUT, 'stropdas-metingen.json'), 'w'), indent=1)
        evidence['stropdas'] = {label: {clip: r['samenvatting'] for clip, r in m['clips'].items()} for label, m in measurements.items()}
        evidence['stropdas_gap_limit'] = GAP_LIMIT
    # Losse strookframes en verkleiningen zijn tussenstappen, geen uitvoer.
    import shutil; shutil.rmtree(os.path.join(OUT, 'tmp'), ignore_errors=True)
    # Harde controles: de stap faalt (exitcode 1) als de das ergens door het
    # lijf, een ander deel of de grond gaat, of de knoop van de keel loskomt.
    failures = []
    for label, m in measurements.items():
        for clip, r in m['clips'].items():
            for n, v in r['samenvatting'].items():
                if v['frames_met_doorsnijding_lijf'] or v['frames_met_doorsnijding_overig']: failures.append(f'{label} {clip} {n}: doorsnijding')
                if v['min_afstand'] < 0: failures.append(f'{label} {clip} {n}: in het lijf ({v["min_afstand"]:.4f})')
                if v['frames_onder_grond']: failures.append(f'{label} {clip} {n}: onder de grond in {v["frames_onder_grond"]} frames ({v["min_boven_grond"]:.4f})')
                if n == 'Stropdas knoop' and v['max_contactafstand_keel'] > .01: failures.append(f'{label} {clip}: knoop los van de keel ({v["max_contactafstand_keel"]:.4f})')
    evidence['harde_controles'] = {'ok': not failures, 'fouten': failures}
    evidence['seconds'] = round(time.time() - started, 1)
    # Contract met de UI (camera-views.json): version 1, per persona-id de
    # kijkhoek; de volledige camera's staan eronder als detail.
    persona = {'signalerend': 'nonverbal', 'individualistisch': 'individualist', 'biologisch': 'biological'}
    # 'facing' beschrijft hier alleen de render: altijd ongespiegeld ('left').
    # Of de interface model en poster spiegelt, bepaalt uitsluitend het manifest.
    contract = {'version': 1, 'rendered': 'unmirrored',
                'note': 'alle renders ongespiegeld, vanuit de genoemde kijkhoek; het manifest bepaalt de weergave in de interface',
                'composition': load_composition()[0], 'views': {persona[n]: {'azimuth': round(v['meeting']['azimuth'], 5), 'elevation': v['meeting']['elevation'],
                                                    'fov': v['meeting']['fov']}
                                        for n, v in views_out['variants'].items()}, 'detail': views_out}
    json.dump(contract, open(os.path.join(OUT, 'camera-views.json'), 'w'), indent=2)
    json.dump(evidence, open(os.path.join(OUT, 'variants-evidence.json'), 'w'), indent=2)
    print('VARIANTS_DONE', json.dumps({k: v['glb']['sha256'] for k, v in evidence['variants'].items()}))
    if failures:
        print('HARDE_CONTROLES_GEFAALD', json.dumps(failures)); sys.exit(1)


if LOOK: look()
else: main()
