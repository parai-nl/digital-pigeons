#!/usr/bin/env python3
"""Beeldstroom van de publieke etalage: platen, duivenlagen, clips, avatars en
het manifest in site/etalage/media/duiven/. Eén commando:

    python3 tools/etalage-beeld/build.py                 # model uit config.json
    python3 tools/etalage-beeld/build.py --model v4      # of kies het model hier
    python3 tools/etalage-beeld/build.py --draft         # snelle proef, alleen in out/

Zie README.md voor de stappen en HERKOMST.md voor de bronnen van de platen.
"""
import argparse, hashlib, json, os, shutil, subprocess, sys, time, urllib.parse, urllib.request
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import numpy as np
from PIL import Image, ImageDraw, ImageFilter, ImageOps

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
MEDIA_REL = 'etalage/media/duiven'
MEDIA = os.path.join(ROOT, 'site', MEDIA_REL)
CACHE = os.path.join(HERE, 'cache')
BLENDER = os.environ.get('BLENDER', '/Applications/Blender.app/Contents/MacOS/Blender')
UA = 'DigitaleDuiven-etalage/1.0 (https://pigeon.parai.nl)'
PIGEONS = ('kennispartner', 'stakeholder', 'signalerend')


def log(*a):
    print('[etalage-beeld]', *a, flush=True)


# ------------------------------------------------------------------ kleur
def to_linear(x):
    return np.where(x <= 0.04045, x / 12.92, ((x + 0.055) / 1.055) ** 2.4)


def to_srgb(x):
    x = np.clip(x, 0, None)
    return np.where(x <= 0.0031308, x * 12.92, 1.055 * np.power(x, 1 / 2.4) - 0.055)


def luminance(lin):
    return lin[..., 0] * 0.2126 + lin[..., 1] * 0.7152 + lin[..., 2] * 0.0722


# ------------------------------------------------------------------ platen
def commons_info(title):
    q = {'action': 'query', 'titles': title, 'prop': 'imageinfo', 'iiprop': 'url|size|sha1|extmetadata', 'format': 'json'}
    req = urllib.request.Request('https://commons.wikimedia.org/w/api.php?' + urllib.parse.urlencode(q), headers={'User-Agent': UA})
    page = next(iter(json.load(urllib.request.urlopen(req, timeout=60))['query']['pages'].values()))
    info = page['imageinfo'][0]
    meta = info.get('extmetadata', {})
    return {'url': info['url'], 'page': info['descriptionurl'], 'sha1': info['sha1'], 'width': info['width'], 'height': info['height'],
            'license': meta.get('LicenseShortName', {}).get('value', ''), 'licenseUrl': meta.get('LicenseUrl', {}).get('value', '')}


def fetch_plate(plate):
    """Origineel van Wikimedia Commons, gecontroleerd op sha1 en licentie."""
    os.makedirs(CACHE, exist_ok=True)
    path = os.path.join(CACHE, plate['sha1'] + os.path.splitext(plate['commons'])[1].lower())
    info_path = path + '.json'
    if os.path.exists(path) and os.path.exists(info_path) and hashlib.sha1(open(path, 'rb').read()).hexdigest() == plate['sha1']:
        return path, json.load(open(info_path))
    info = commons_info(plate['commons'])
    if info['sha1'] != plate['sha1']:
        sys.exit(f"{plate['commons']}: Commons levert een andere versie (sha1 {info['sha1']}), herkomst opnieuw beoordelen")
    if info['license'] != plate['license']:
        sys.exit(f"{plate['commons']}: licentie op Commons is nu {info['license']!r}, config zegt {plate['license']!r}")
    req = urllib.request.Request(info['url'], headers={'User-Agent': UA})
    data = urllib.request.urlopen(req, timeout=300).read()
    if hashlib.sha1(data).hexdigest() != plate['sha1']:
        sys.exit(f"{plate['commons']}: download komt niet overeen met sha1")
    open(path, 'wb').write(data)
    json.dump(info, open(info_path, 'w'), indent=1)
    return path, info


def blur_profile(h, dof):
    """Blurstraal per rij (in pixels van de plaat): scherp waar de duif staat,
    vager naar de voorgrond en het meest boven de horizon."""
    y = (np.arange(h) + 0.5) / h
    r = np.empty(h)
    for i, v in enumerate(y):
        if v >= dof['focusY']:
            t = (v - dof['focusY']) / max(1e-6, 1 - dof['focusY'])
            r[i] = dof['focus'] + (dof['front'] - dof['focus']) * t
        elif v >= dof['horizonY']:
            t = (dof['focusY'] - v) / max(1e-6, dof['focusY'] - dof['horizonY'])
            r[i] = dof['focus'] + (dof['far'] - dof['focus']) * (t * t * (3 - 2 * t))
        else:
            r[i] = dof['far']
    return r


def make_plate(cfg, pigeon, work):
    pc = cfg['pigeons'][pigeon]['plate']
    src_path, info = fetch_plate(pc)
    src = ImageOps.exif_transpose(Image.open(src_path)).convert('RGB')
    W, H = src.size
    aspect = cfg['card']['aspect']
    ch = pc['crop']['h'] * H
    cw = ch * aspect
    if cw > W:
        cw, ch = W, W / aspect
    x0 = min(max(pc['crop']['cx'] * W - cw / 2, 0), W - cw)
    y0 = min(max(pc['crop']['cy'] * H - ch / 2, 0), H - ch)
    box = (round(x0), round(y0), round(x0 + cw), round(y0 + ch))
    pw, ph = cfg['plate']['width'], cfg['plate']['height']
    img = src.crop(box).resize((pw, ph), Image.LANCZOS)

    # Scherptediepte: een paar blurniveaus, per rij lineair gemengd.
    radii = blur_profile(ph, pc['dof'])
    levels = sorted({0.0, *np.round(np.linspace(radii.min(), radii.max(), 7), 2)})
    stack = [np.asarray(img if r == 0 else img.filter(ImageFilter.GaussianBlur(r)), dtype=np.float64) / 255 for r in levels]
    out = np.empty_like(stack[0])
    for row in range(ph):
        r = radii[row]
        k = int(np.searchsorted(levels, r, side='right') - 1)
        k = min(max(k, 0), len(levels) - 2)
        t = (r - levels[k]) / (levels[k + 1] - levels[k])
        out[row] = stack[k][row] * (1 - t) + stack[k + 1][row] * t

    g = pc['grade']
    lin = to_linear(out) * g['exposure']
    lin = 0.18 * np.power(np.clip(lin, 1e-6, None) / 0.18, g['contrast'])
    Y = luminance(lin)[..., None]
    lin = Y + (lin - Y) * g['saturation']
    tint = to_linear(np.array(g['tint']))
    lin = lin * (1 - g['tintAmount']) + Y * (tint / luminance(tint)) * g['tintAmount']
    yy = ((np.arange(ph) + 0.5) / ph)[:, None, None]
    sky = lin[: int(ph * 0.3)].reshape(-1, 3).mean(axis=0)
    fog = sky * 0.6 + luminance(sky) * 0.4
    lin = lin * (1 - g['haze'] * (1 - yy) ** 1.6) + fog * g['haze'] * (1 - yy) ** 1.6
    srgb = to_srgb(lin)
    srgb = g['lift'] + (1 - g['lift']) * srgb
    srgb = np.where(srgb > 0.86, 0.86 + (1 - np.exp(-(srgb - 0.86) / 0.14 * 1.2)) * 0.14 / 1.2 * 1.0, srgb)
    xx = ((np.arange(pw) + 0.5) / pw)[None, :, None]
    rr = ((xx - 0.5) / 0.62) ** 2 + ((yy - 0.46) / 0.7) ** 2
    srgb = srgb * (1 - g['vignette'] * np.clip(rr, 0, 1.6) ** 1.3 / 1.6 ** 1.3)
    plate = Image.fromarray(np.clip(srgb * 255 + 0.5, 0, 255).astype(np.uint8))

    os.makedirs(work, exist_ok=True)
    plate.save(os.path.join(work, 'plaat.png'))
    webp = os.path.join(work, 'plaat.webp')
    for q in range(86, 40, -3):
        plate.save(webp, 'WEBP', quality=q, method=6)
        if os.path.getsize(webp) <= cfg['plate']['maxBytes']:
            break
    else:
        sys.exit(f'{pigeon}: plaat blijft groter dan {cfg["plate"]["maxBytes"]} bytes')

    # Omgevingslicht voor de duif: lucht en grond uit de bewerkte plaat.
    final = to_linear(np.asarray(plate, dtype=np.float64) / 255)
    light = {'sky': final[: int(ph * 0.35)].reshape(-1, 3).mean(axis=0).tolist(),
             'ground': final[int(ph * 0.82):].reshape(-1, 3).mean(axis=0).tolist(),
             'quality': q, 'bytes': os.path.getsize(webp), 'crop': box, 'source': [W, H], 'info': info}
    json.dump(light, open(os.path.join(work, 'plate.json'), 'w'), indent=1)
    log(f'{pigeon}: plaat {os.path.getsize(webp)} bytes (webp q{q}), uitsnede {box} van {W}×{H}')
    return light


# ------------------------------------------------------------------ render
def render(cfg, model, pigeon, work, draft, frame_step=1, parts=None, keep=False, glb_dir=None):
    pc = cfg['pigeons'][pigeon]
    light = json.load(open(os.path.join(work, 'plate.json')))
    L = dict(pc['light'])
    # Een lichte, ontkleurde versie van de plaat als omgeving: kleur zonder zwaarte.
    sky, ground = np.array(light['sky']), np.array(light['ground'])
    L['sky'] = (sky / max(1e-6, luminance(sky)) * 0.55).tolist()
    L['ground'] = (ground / max(1e-6, luminance(ground)) * 0.2).tolist()
    job = {
        'glb': os.path.join(os.path.abspath(glb_dir), os.path.basename(cfg['models'][model]['glb'][pigeon])) if glb_dir
               else os.path.join(ROOT, cfg['models'][model]['glb'][pigeon]),
        'out': os.path.join(work, 'render'), 'fps': cfg['fps'], 'card': cfg['card'], 'pad': cfg['pad'],
        'view': pc['view'], 'rest': pc['rest'], 'reaction': pc['reaction'], 'light': L,
        'quality': cfg['quality']['draft' if draft else 'final'], 'gestures': cfg['gestures'],
        'head': {'px': cfg['head']['px'], 'distance': cfg['head']['distance'], 'azimuthDeg': pc.get('headAzimuthDeg', 0)},
        'do': parts or ['rest', 'head', *cfg['gestures']], 'frameStep': frame_step,
    }
    if not os.path.exists(job['glb']):
        sys.exit(f'{pigeon}: model ontbreekt: {job["glb"]}')
    if keep:    # alleen deze delen opnieuw; de rest van de render blijft staan
        for part in parts:
            shutil.rmtree(os.path.join(job['out'], part), ignore_errors=True)
    else:
        shutil.rmtree(job['out'], ignore_errors=True)
    os.makedirs(job['out'], exist_ok=True)
    jp = os.path.join(work, 'job.json')
    json.dump(job, open(jp, 'w'), indent=1)
    t = time.time()
    with open(os.path.join(work, 'blender.log'), 'w') as fh:
        rc = subprocess.run([BLENDER, '-b', '--factory-startup', '-P', os.path.join(HERE, 'blender_render.py'), '--', jp], stdout=fh, stderr=subprocess.STDOUT).returncode
    if rc or 'ETALAGE-RENDER-OK' not in open(os.path.join(work, 'blender.log')).read():
        sys.exit(f'{pigeon}: Blender faalde, zie {work}/blender.log')
    log(f'{pigeon}: gerenderd in {time.time() - t:.0f} s')


# ------------------------------------------------------------------ samenstellen
def load_rgba(path):
    return np.asarray(Image.open(path).convert('RGBA'), dtype=np.float32) / 255


def card_mask(meta, shape, radius, fade):
    """1 binnen de kaart, 0 erbuiten: afgeronde hoeken (4× oversampled) en een
    zachte aflopende rand van `fade` × kaartmaat, zodat er bij het kantelen van
    de kaart nooit een schaduwrand buiten de plaat schuift."""
    h, w = shape
    s = 4
    x0, y0, cw, ch = meta['card']
    big = Image.new('L', (w * s, h * s), 0)
    ImageDraw.Draw(big).rounded_rectangle([x0 * s, y0 * s, (x0 + cw) * s - 1, (y0 + ch) * s - 1], radius=radius * cw * s, fill=255)
    mask = np.asarray(big.resize((w, h), Image.LANCZOS), dtype=np.float32) / 255
    xs, ys = np.arange(w) + 0.5, np.arange(h) + 0.5
    fx = np.clip(np.minimum(xs - x0, x0 + cw - xs) / (fade * cw), 0, 1)
    fy = np.clip(np.minimum(ys - y0, y0 + ch - ys) / (fade * ch), 0, 1)
    smooth = lambda t: t * t * (3 - 2 * t)
    return mask * smooth(fy)[:, None] * smooth(fx)[None, :]


def near_feet(shape, anchor, card, sc):
    """Contactschaduw alleen onder en rond de poten: een ellips om het grondpunt."""
    h, w = shape
    _, _, cw, ch = card
    yy, xx = np.mgrid[0:h, 0:w].astype(np.float32) + 0.5
    r = np.sqrt(((xx - anchor[0]) / (sc['rx'] * cw)) ** 2 + ((yy - anchor[1]) / (sc['ry'] * ch)) ** 2)
    t = np.clip((r - 0.45) / 0.55, 0, 1)
    return 1 - t * t * (3 - 2 * t)


def over(top, bottom):
    a = top[..., 3:] + bottom[..., 3:] * (1 - top[..., 3:])
    c = top[..., :3] * top[..., 3:] + bottom[..., :3] * bottom[..., 3:] * (1 - top[..., 3:])
    return np.concatenate([np.where(a > 0, c / np.maximum(a, 1e-9), 0), a], axis=-1)


def compose(bird_path, shadow_path, mask, sc, anchor, card):
    bird = load_rgba(bird_path)
    shadow = load_rgba(shadow_path)
    shadow[..., :3] = 0
    shadow[..., 3] = np.clip(shadow[..., 3] * sc['strength'], 0, 1) * mask * near_feet(mask.shape, anchor, card, sc)
    return over(bird, shadow)


def to_image(arr):
    return Image.fromarray(np.clip(arr * 255 + 0.5, 0, 255).astype(np.uint8), 'RGBA')


def assemble(cfg, pigeon, work):
    """Rust en clips: schaduw binnen de kaart, duif erover, één gedeeld canvas.
    Twee rondes over de beelden (eerst de omhullende, dan uitsnijden), zodat er
    nooit meer dan één beeld tegelijk in het geheugen staat."""
    rdir = os.path.join(work, 'render')
    meta = json.load(open(os.path.join(rdir, 'meta.json')))
    rw, rh = meta['render']
    mask = card_mask(meta, (rh, rw), cfg['card']['radius'], cfg['card']['shadowFade'])
    sc = {**cfg['shadow'], **cfg['pigeons'][pigeon].get('shadow', {})}
    card = meta['card']
    rest = compose(os.path.join(rdir, 'rest', 'bird_0000.png'), os.path.join(rdir, 'rest', 'shadow_0000.png'), mask, sc, meta['anchor'], card)
    clips = {}   # per gebaar de beeldnummers; None = het rustbeeld zelf
    for kind in cfg['gestures']:
        if kind not in meta['clips']:
            continue
        n = meta['clips'][kind]['frames']
        clips[kind] = [None if f in (0, n - 1) else f for f in range(n)
                       if f in (0, n - 1) or os.path.exists(os.path.join(rdir, kind, f'bird_{f:04d}.png'))]

    def frame(kind, f):
        if f is None:
            return rest        # begin en eind zijn exact het rustbeeld
        anchor = meta['clips'][kind].get('anchors', [meta['anchor']] * (f + 1))[f]   # de poten schuiven mee
        return compose(os.path.join(rdir, kind, f'bird_{f:04d}.png'), os.path.join(rdir, kind, f'shadow_{f:04d}.png'), mask, sc, anchor, card)

    # Gezamenlijke omhullende van alle beelden, zodat rust en clips één canvas delen.
    alpha = rest[..., 3] > 1 / 255
    for kind, seq in clips.items():
        for f in seq:
            if f is not None:
                alpha |= frame(kind, f)[..., 3] > 1 / 255
    ys, xs = np.nonzero(alpha)
    x0, x1, y0, y1 = xs.min(), xs.max() + 1, ys.min(), ys.max() + 1
    if x0 == 0 or y0 == 0 or x1 == rw or y1 == rh:
        sys.exit(f'{pigeon}: de duif raakt de rand van het renderbeeld; vergroot "pad" in config.json')
    m = 4
    x0, y0, x1, y1 = max(0, x0 - m), max(0, y0 - m), min(rw, x1 + m), min(rh, y1 + m)
    # Even afmetingen voor yuva420p.
    if (x1 - x0) % 2:
        x1 = x1 + 1 if x1 < rw else x1 - 1
    if (y1 - y0) % 2:
        y1 = y1 + 1 if y1 < rh else y1 - 1
    cx, cy, cw, ch = meta['card']
    layer = {'left': round(float(x0 - cx) / cw, 4), 'top': round(float(y0 - cy) / ch, 4), 'width': round(float(x1 - x0) / cw, 4), 'height': round(float(y1 - y0) / ch, 4)}

    fdir = os.path.join(work, 'frames')
    shutil.rmtree(fdir, ignore_errors=True)
    os.makedirs(fdir)
    to_image(rest[y0:y1, x0:x1]).save(os.path.join(fdir, 'rest.png'))
    for kind, seq in clips.items():
        os.makedirs(os.path.join(fdir, kind))
        for i, f in enumerate(seq):
            to_image(frame(kind, f)[y0:y1, x0:x1]).save(os.path.join(fdir, kind, f'{i:03d}.png'))
    # Kop voor de chatbubbel: de camera kadreert de kop al; de hals loopt door tot de onderrand.
    head = Image.open(os.path.join(rdir, 'head', 'bird_0000.png')).convert('RGBA')
    head.resize((cfg['head']['out'],) * 2, Image.LANCZOS).save(os.path.join(fdir, 'kop.png'))
    info = {'layer': layer, 'canvas': [int(x1 - x0), int(y1 - y0)], 'crop': [int(x0), int(y0), int(x1), int(y1)], 'fps': cfg['fps'],
            'clips': {k: len(v) for k, v in clips.items()}, 'card': meta['card']}
    json.dump(info, open(os.path.join(fdir, 'frames.json'), 'w'), indent=1)
    log(f'{pigeon}: canvas {x1 - x0}×{y1 - y0}, laag {layer}')
    return info


# ------------------------------------------------------------------ coderen
def run(cmd):
    r = subprocess.run(cmd, capture_output=True, text=True)
    if r.returncode:
        sys.exit('mislukt: ' + ' '.join(cmd) + '\n' + r.stderr[-2000:])
    return r


def hevc_tool():
    """Compileert hevc_alpha.swift één keer (opnieuw als de bron nieuwer is)."""
    src = os.path.join(HERE, 'hevc_alpha.swift')
    exe = os.path.join(HERE, 'out', 'bin', 'hevc_alpha')
    if not os.path.exists(exe) or os.path.getmtime(exe) < os.path.getmtime(src):
        os.makedirs(os.path.dirname(exe), exist_ok=True)
        run(['swiftc', '-O', src, '-o', exe])
    return exe


def bird_psnr(path, src, step=4):
    """PSNR op de dekkende duif (bron-alfa > 230), elk 4e beeld: de scherpte die
    je ziet bij de wissel tussen rust.webp en de video."""
    from check import frames
    dec = frames(path, 'libvpx-vp9' if path.endswith('.webm') else None)
    scores = []
    for a, b in list(zip(dec, src))[::step]:
        m = b[..., 3] > 230
        d = (a[..., :3].astype(np.float32) - b[..., :3])[m] / 255
        scores.append(10 * np.log10(1 / max(float((d ** 2).mean()), 1e-12)))
    return float(np.mean(scores))


def hevc_best(frame_dir, fps, mov, limit):
    """Hoogste VideoToolbox-kwaliteit die onder de grens blijft, met alfakwaliteit
    0,5 (0,3 verdubbelde de fout langs de alfarand); pas als niets past 0,3."""
    src = [np.asarray(Image.open(os.path.join(frame_dir, f))) for f in sorted(os.listdir(frame_dir))]
    # Proefbestanden in out/, nooit in site/ (de deploy weigert vreemde bestanden).
    tmp = os.path.join(os.path.dirname(frame_dir), 'proef-' + os.path.basename(mov))
    try:
        for aq in (0.5, 0.3):
            for q in (0.8, 0.76, 0.73, 0.7, 0.68, 0.65, 0.6, 0.55, 0.5, 0.45):
                run([hevc_tool(), frame_dir, str(fps), tmp, str(q), str(aq), 'srgb', 'straight'])
                size = os.path.getsize(tmp)
                if size <= limit:
                    shutil.copy(tmp, mov)
                    return {'psnr': round(bird_psnr(mov, src), 2), 'q': q, 'alphaQ': aq, 'bytes': size}
        return None
    finally:
        # AVAssetWriter laat bij het vooraan zetten van de index soms een
        # tijdelijk broertje (<naam>.sb-…) achter.
        for f in os.listdir(os.path.dirname(tmp)):
            if f.startswith(os.path.basename(tmp)):
                os.remove(os.path.join(os.path.dirname(tmp), f))


def encode(cfg, pigeon, work, dest):
    fdir = os.path.join(work, 'frames')
    info = json.load(open(os.path.join(fdir, 'frames.json')))
    os.makedirs(dest, exist_ok=True)
    run(['cwebp', '-quiet', '-q', '90', '-alpha_q', '100', '-m', '6', os.path.join(fdir, 'rest.png'), '-o', os.path.join(dest, 'rust.webp')])
    run(['cwebp', '-quiet', '-q', '88', '-alpha_q', '100', '-m', '6', os.path.join(fdir, 'kop.png'), '-o', os.path.join(dest, 'kop.webp')])
    limit = cfg['clip']['maxBytes']
    out = {}
    for kind in cfg['gestures']:
        if not os.path.isdir(os.path.join(fdir, kind)):
            continue        # proef zonder clips (--rest-only)
        src = os.path.join(fdir, kind, '%03d.png')
        webm, mov = os.path.join(dest, kind + '.webm'), os.path.join(dest, kind + '.mov')
        # VP9 met alfa. De RGB→YUV-omzetting is BT.601 en zo gelabeld; Chromium
        # toont het eerste beeld dan binnen ±1 niveau van rust.webp.
        for crf in (27, 28, 30, 32, 34, 36, 39, 42, 45):
            run(['ffmpeg', '-v', 'error', '-y', '-framerate', str(cfg['fps']), '-i', src,
                 '-vf', 'scale=out_color_matrix=bt601:out_range=tv,format=yuva420p', '-c:v', 'libvpx-vp9', '-pix_fmt', 'yuva420p',
                 '-b:v', '0', '-crf', str(crf), '-deadline', 'good', '-cpu-used', '1', '-row-mt', '1', '-auto-alt-ref', '0',
                 '-g', '240', '-colorspace', 'smpte170m', '-color_range', 'tv', '-an', '-metadata', 'title=', webm])
            if os.path.getsize(webm) <= limit:
                break
        else:
            sys.exit(f'{pigeon}/{kind}: WebM blijft boven {limit} bytes')
        # HEVC met alfa voor Safari: VideoToolbox via AVAssetWriter, met kleurlabels
        # en rechte alfa (zie hevc_alpha.swift); de hoogste kwaliteit onder de grens.
        q = hevc_best(os.path.join(fdir, kind), cfg['fps'], mov, limit)
        if q is None:
            sys.exit(f'{pigeon}/{kind}: HEVC blijft boven {limit} bytes')
        src = [np.asarray(Image.open(os.path.join(fdir, kind, f))) for f in sorted(os.listdir(os.path.join(fdir, kind)))]
        out[kind] = {'webmCrf': crf, 'webmPsnr': round(bird_psnr(webm, src), 2), 'hevc': q, 'webm': os.path.getsize(webm),
                     'hevcBytes': os.path.getsize(mov), 'ms': round(info['clips'][kind] / cfg['fps'] * 1000)}
        log(f'{pigeon}/{kind}: webm {out[kind]["webm"]} B (crf {crf}, PSNR duif {out[kind]["webmPsnr"]} dB), '
            f'hevc {q["bytes"]} B (q {q["q"]}/alfa {q["alphaQ"]}, PSNR duif {q["psnr"]} dB)')
    shutil.copy(os.path.join(work, 'plaat.webp'), os.path.join(dest, 'plaat.webp'))
    json.dump(out, open(os.path.join(work, 'encode.json'), 'w'), indent=1)
    return out


# ------------------------------------------------------------------ manifest
def plate_credit(cfg, pigeon):
    p = cfg['pigeons'][pigeon]
    pc = p['plate']
    return {
        'nl': f"Achtergrond {p['name']['nl']}: {pc['place']['nl']}, foto {pc['artist']}, uitgesneden, vervaagd en ingekleurd · {pc['license']}",
        'en': f"Background {p['name']['en']}: {pc['place']['en']}, photo by {pc['artist']}, cropped, blurred and colour-graded · {pc['license']}",
        'url': pc['page'], 'license': pc['license'], 'licenseUrl': pc['licenseUrl'],
    }


def summary_model(cfg, per_pigeon):
    """Het model bovenaan volgt uit de regels per duif. Hebben alle drie hetzelfde
    model, dan file, version en final; één versie met een eigen bestand per duif
    (v4) geeft version en final; gemengd (bv. na `--model v4 --only …`) een
    neutrale samenvatting. Final is alleen true als het model van elke duif final is. Final komt uit
    config.json, opgezocht op de versie van elke duif."""
    final_of = {m['version']: m['final'] for m in cfg['models'].values()}
    known = [e for e in per_pigeon if e and e.get('version') in final_of]
    final = len(known) == len(per_pigeon) and all(final_of[e['version']] for e in known)
    pairs = {(e.get('file'), e.get('version')) for e in per_pigeon if e}
    if len(known) == len(per_pigeon) and len(pairs) == 1:
        file, version = pairs.pop()
        return {'file': file, 'version': version, 'final': final}
    versions = sorted({e.get('version', '?') for e in per_pigeon if e}) or ['onbekend']
    if len(versions) == 1 and len(known) == len(per_pigeon):
        return {'version': versions[0], 'final': final}     # één versie, een eigen bestand per duif (v4)
    return {'version': 'per duif verschillend: ' + ', '.join(versions), 'final': final}


def manifest(cfg, model, works):
    """Het manifest voor de site. Een duif die in deze run niet gebouwd is (--only,
    of een verse clone zonder out/), houdt haar regel uit het bestaande manifest."""
    m = cfg['models'][model]
    path = os.path.join(MEDIA, 'manifest.json')
    old = json.load(open(path))['pigeons'] if os.path.exists(path) else {}
    pigeons = {}
    for p in PIGEONS:
        fj, ej = os.path.join(works[p], 'frames', 'frames.json'), os.path.join(works[p], 'encode.json')
        if not (os.path.exists(fj) and os.path.exists(ej)):
            if p not in old:
                sys.exit(f'{p}: geen build in {works[p]} en geen regel in het bestaande manifest; bouw haar eerst')
            log(f'{p}: niet gebouwd in deze run, regel uit het bestaande manifest behouden')
            pigeons[p] = old[p]
            continue
        info, enc = json.load(open(fj)), json.load(open(ej))
        base = f'{MEDIA_REL}/{p}'
        pigeons[p] = {
            'model': {'file': os.path.basename(m['glb'][p]), 'version': m['version']},
            'plate': {'src': f'{base}/plaat.webp', 'width': cfg['plate']['width'], 'height': cfg['plate']['height']},
            'layer': info['layer'],
            'rest': {'src': f'{base}/rust.webp', 'width': info['canvas'][0], 'height': info['canvas'][1]},
            'clips': {k: {'webm': f'{base}/{k}.webm', 'hevc': f'{base}/{k}.mov', 'ms': enc[k]['ms']} for k in cfg['gestures']},
            # Het gebaar in de waaier als een kaart naar voren komt; per duif te kiezen.
            'defaultClip': cfg['pigeons'][p].get('defaultClip', cfg['defaultClip']),
            'avatar': {'src': f'{base}/kop.webp'},
        }
    top = summary_model(cfg, [pigeons[p].get('model') for p in PIGEONS])
    return {
        'schema': 1,
        'model': top,
        'card': {'aspect': cfg['card']['aspect']},
        'pigeons': pigeons,
        'credits': [cfg['modelCredit'], *[plate_credit(cfg, p) for p in PIGEONS]],
    }


# ------------------------------------------------------------------ main
def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--model', choices=['v3', 'v4'], help='model (standaard: "model" in config.json)')
    ap.add_argument('--only', help='komma-gescheiden duiven, bv. kennispartner')
    ap.add_argument('--steps', default='plates,render,assemble,encode,manifest,check')
    ap.add_argument('--draft', action='store_true', help='weinig samples en elk 6e beeld; schrijft alleen naar out/')
    ap.add_argument('--glb-dir', help='lees de GLB-bestanden uit deze map in plaats van de module (zelfde bestandsnamen)')
    ap.add_argument('--rest-only', action='store_true', help='alleen rustbeeld en kop renderen, geen clips (voor proeven)')
    ap.add_argument('--parts', help='alleen deze delen opnieuw renderen, bv. head of rest,head,peck')
    args = ap.parse_args()
    if args.rest_only and not args.draft:
        ap.error('--rest-only alleen met --draft: zonder clips zou site/ een rustbeeld krijgen dat niet bij de clips past')
    cfg = json.load(open(os.path.join(HERE, 'config.json')))
    model = args.model or cfg['model']
    pigeons = args.only.split(',') if args.only else list(PIGEONS)
    steps = args.steps.split(',')
    stamp = 'draft' if args.draft else 'final'
    works = {p: os.path.join(HERE, 'out', model, stamp, p) for p in PIGEONS}
    frame_step = 6 if args.draft else 1
    for p in pigeons:
        if 'plates' in steps:
            make_plate(cfg, p, works[p])
        if 'render' in steps:
            parts = args.parts.split(',') if args.parts else (['rest', 'head'] if args.rest_only else None)
            render(cfg, model, p, works[p], args.draft, frame_step, parts=parts, keep=bool(args.parts), glb_dir=args.glb_dir)
        if 'assemble' in steps:
            assemble(cfg, p, works[p])
        if 'encode' in steps:
            if args.draft:
                shutil.rmtree(os.path.join(works[p], 'media'), ignore_errors=True)   # geen oude proefclips laten staan
            encode(cfg, p, works[p], os.path.join(works[p], 'media') if args.draft else os.path.join(MEDIA, p))
    if args.draft:
        log('proef klaar in', os.path.join(HERE, 'out', model, stamp))
        return
    if 'manifest' in steps:
        man = manifest(cfg, model, works)
        with open(os.path.join(MEDIA, 'manifest.json'), 'w') as fh:
            json.dump(man, fh, indent=2, ensure_ascii=False)
            fh.write('\n')
        log('manifest geschreven')
    if 'check' in steps:
        sys.exit(subprocess.run([sys.executable, os.path.join(HERE, 'check.py')]).returncode)


if __name__ == '__main__':
    main()
