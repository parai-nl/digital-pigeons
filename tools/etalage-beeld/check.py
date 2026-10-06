#!/usr/bin/env python3
"""Controleert wat build.py in site/etalage/media/duiven/ zet, tegen het
contract van manifest.json (README.md):

- manifest: velden, paden die bestaan, layer-box, credits NL/EN met URL;
- groottes: plaat ≤ 200 KB, elke clip ≤ 400 KB per formaat;
- alfa: WebM is VP9 met alpha_mode=1, HEVC is hvc1; beide gedecodeerd met
  ffmpeg (libvpx-vp9 resp. de HEVC-decoder, die de alfalaag leest) tot RGBA,
  en gemeten: volledig transparante hoeken, dekkende duif, zachte randen;
- naad: eerste en laatste beeld van elke clip tegen rust.webp (alfa en kleur
  onder de duif), plus hetzelfde canvas voor rust en clips;
- deploy-eis: geen dotfiles, geen symlinks en alleen json/webp/png/jpg/webm/
  mov/mp4 onder site/etalage/media/duiven/.

Schrijft het verslag naar tools/etalage-beeld/out/check.json en stopt met een
foutcode als iets niet klopt.

    python3 tools/etalage-beeld/check.py
"""
import io, json, os, subprocess, sys
import numpy as np
from PIL import Image

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
SITE = os.path.join(ROOT, 'site')
MEDIA = os.path.join(SITE, 'etalage', 'media', 'duiven')
HEAD_PX = json.load(open(os.path.join(HERE, 'config.json')))['head']['out']
problems = []


def fail(msg):
    problems.append(msg)
    print('FOUT', msg)


def probe(path):
    r = subprocess.run(['ffprobe', '-v', 'error', '-show_streams', '-show_format', '-of', 'json', path], capture_output=True, text=True)
    return json.loads(r.stdout)


def frames(path, decoder=None):
    """Alle beelden als RGBA-arrays (uint8)."""
    cmd = ['ffmpeg', '-v', 'error']
    if decoder:
        cmd += ['-c:v', decoder]
    info = probe(path)['streams'][0]
    w, h = int(info['width']), int(info['height'])
    cmd += ['-i', path, '-f', 'rawvideo', '-pix_fmt', 'rgba', '-']
    raw = subprocess.run(cmd, capture_output=True).stdout
    n = len(raw) // (w * h * 4)
    return np.frombuffer(raw[: n * w * h * 4], dtype=np.uint8).reshape(n, h, w, 4)


def alpha_stats(a):
    al = a[..., 3]
    h, w = al.shape
    k = max(4, min(h, w) // 20)
    # De bovenhoeken liggen altijd boven de kaart en naast de kop; onderin mag de
    # contactschaduw tot de kaartrand lopen.
    corners = np.concatenate([al[:k, :k].ravel(), al[:k, -k:].ravel()])
    return {
        'transparent': round(float((al == 0).mean()), 3),
        'opaque': round(float((al == 255).mean()), 3),
        'partial': round(float(((al > 0) & (al < 255)).mean()), 4),
        'cornersMax': int(corners.max()) if corners.size else None,
    }


def seam(a, b):
    """Verschil tussen twee RGBA-beelden: alfa overal, kleur waar beide dekken."""
    da = np.abs(a[..., 3].astype(int) - b[..., 3].astype(int))
    both = (a[..., 3] > 200) & (b[..., 3] > 200)
    dc = np.abs(a[..., :3].astype(int) - b[..., :3].astype(int))[both]
    return {'alphaMean': round(float(da.mean()), 3), 'alphaP99': int(np.percentile(da, 99)),
            'colourMean': round(float(dc.mean()), 3) if dc.size else None,
            'colourP99': int(np.percentile(dc, 99)) if dc.size else None}


ALLOWED = {'.json', '.webp', '.png', '.jpg', '.webm', '.mov', '.mp4'}


def site_files():
    """Eis van de deploy: onder site/ geen dotfiles, geen symlinks en alleen
    deze bestandstypen. Hier getoetst voor de map van de beeldstroom."""
    for dirpath, dirnames, filenames in os.walk(MEDIA):
        for name in dirnames + filenames:
            full = os.path.join(dirpath, name)
            rel = os.path.relpath(full, SITE)
            if name.startswith('.'):
                fail(f'dotfile onder site/: {rel}')
            if os.path.islink(full):
                fail(f'symlink onder site/: {rel}')
            if name in filenames and os.path.splitext(name)[1].lower() not in ALLOWED:
                fail(f'bestandstype niet toegestaan onder site/: {rel}')


def main():
    report = {'pigeons': {}}
    site_files()
    mpath = os.path.join(MEDIA, 'manifest.json')
    if not os.path.exists(mpath):
        fail('manifest.json ontbreekt')
        return finish(report)
    m = json.load(open(mpath))
    for key in ('schema', 'model', 'card', 'pigeons', 'credits'):
        if key not in m:
            fail(f'manifest mist {key}')
    if m.get('schema') != 1:
        fail('schema is niet 1')
    for key in ('version', 'final'):
        if key not in m.get('model', {}):
            fail(f'model mist {key}')
    if not isinstance(m.get('model', {}).get('final'), bool):
        fail('model.final is geen boolean')
    if m.get('card', {}).get('aspect') != 0.82:
        fail('card.aspect is niet 0.82')
    for c in m.get('credits', []):
        if not (c.get('nl') and c.get('en') and c.get('license') and str(c.get('url', '')).startswith('https://')
                and str(c.get('licenseUrl', '')).startswith('https://')):
            fail(f'credit onvolledig (nl, en, url, license, licenseUrl): {c}')
    report['credits'] = len(m.get('credits', []))

    for pid in ('kennispartner', 'stakeholder', 'signalerend'):
        p = m['pigeons'].get(pid)
        r = report['pigeons'][pid] = {}
        if not p:
            fail(f'{pid}: ontbreekt in manifest')
            continue

        def path(src):
            full = os.path.join(SITE, src)
            if not src.startswith('etalage/media/duiven/') or not os.path.exists(full):
                fail(f'{pid}: pad bestaat niet of ligt buiten media/duiven: {src}')
            return full

        plate = path(p['plate']['src'])
        with Image.open(plate) as im:
            if im.size != (p['plate']['width'], p['plate']['height']):
                fail(f'{pid}: plaat is {im.size}, manifest zegt {p["plate"]["width"]}×{p["plate"]["height"]}')
            if abs(im.size[0] / im.size[1] - m['card']['aspect']) > 0.002:
                fail(f'{pid}: plaat heeft niet de kaartverhouding')
        r['plateBytes'] = os.path.getsize(plate)
        if r['plateBytes'] > 200_000:
            fail(f'{pid}: plaat {r["plateBytes"]} B > 200 KB')

        if 'model' in p and not (p['model'].get('file') and p['model'].get('version')):
            fail(f'{pid}: model zonder file of version: {p["model"]}')
        if 'file' not in m['model'] and 'model' not in p:
            fail(f'{pid}: geen model.file en geen pigeons.{pid}.model')
        L = p['layer']
        r['layer'] = L
        if not (L['top'] < 0 and L['width'] > 0 and L['height'] > 0):
            fail(f'{pid}: layer steekt niet boven de kaart uit: {L}')
        cw, ch = p['rest']['width'] / L['width'], p['rest']['height'] / L['height']
        r['layerAspectCheck'] = round(cw / ch, 4)
        if abs(cw / ch - m['card']['aspect']) > 0.01:
            fail(f'{pid}: canvas en layer-box geven een kaartverhouding van {cw / ch:.3f}')

        rest_path = path(p['rest']['src'])
        rest = np.asarray(Image.open(rest_path).convert('RGBA'))
        if rest.shape[1] != p['rest']['width'] or rest.shape[0] != p['rest']['height']:
            fail(f'{pid}: rust.webp is {rest.shape[1]}×{rest.shape[0]}, manifest zegt anders')
        r['rest'] = {'bytes': os.path.getsize(rest_path), **alpha_stats(rest)}
        # Hoe ver steekt de duif in rust boven de kaart uit (fractie kaarthoogte)?
        ys = np.nonzero(rest[..., 3].max(axis=1) > 128)[0]
        r['rest']['headAboveCard'] = round(-(L['top'] + ys.min() / rest.shape[0] * L['height']), 3)

        avatar = path(p['avatar']['src'])
        with Image.open(avatar) as im:
            r['avatar'] = {'size': im.size, 'mode': im.mode, 'bytes': os.path.getsize(avatar)}
            # Vierkant, op de maat uit config.json (head.out).
            if im.size != (HEAD_PX, HEAD_PX):
                fail(f'{pid}: kop.webp is {im.size}, niet {HEAD_PX}×{HEAD_PX}')
            if im.mode != 'RGBA':
                fail(f'{pid}: kop.webp heeft geen alfakanaal ({im.mode})')

        if p.get('defaultClip') not in p['clips']:
            fail(f'{pid}: defaultClip {p.get("defaultClip")} is geen clip')
        r['clips'] = {}
        for kind, clip in p['clips'].items():
            cr = r['clips'][kind] = {}
            for fmt, decoder, codec in (('webm', 'libvpx-vp9', 'vp9'), ('hevc', None, 'hevc')):
                f = path(clip[fmt])
                size = os.path.getsize(f)
                info = probe(f)
                st = info['streams'][0]
                entry = cr[fmt] = {'bytes': size, 'codec': st['codec_name'], 'tag': st.get('codec_tag_string'),
                                   'size': [st['width'], st['height']], 'ms': round(float(info['format']['duration']) * 1000)}
                if size > 400_000:
                    fail(f'{pid}/{kind}.{fmt}: {size} B > 400 KB')
                if st['codec_name'] != codec:
                    fail(f'{pid}/{kind}.{fmt}: codec {st["codec_name"]}')
                if fmt == 'webm':
                    entry['alphaMode'] = (st.get('tags') or {}).get('alpha_mode') or (st.get('tags') or {}).get('ALPHA_MODE')
                    if entry['alphaMode'] != '1':
                        fail(f'{pid}/{kind}.webm: geen alpha_mode=1')
                else:
                    if st.get('codec_tag_string') != 'hvc1':
                        fail(f'{pid}/{kind}.mov: tag {st.get("codec_tag_string")} in plaats van hvc1')
                if [st['width'], st['height']] != [p['rest']['width'], p['rest']['height']]:
                    fail(f'{pid}/{kind}.{fmt}: canvas {st["width"]}×{st["height"]} wijkt af van het rustbeeld')
                fr = frames(f, decoder)
                entry['frames'] = len(fr)
                mid = fr[len(fr) // 2]
                entry['alphaMid'] = alpha_stats(mid)
                if entry['alphaMid']['transparent'] < 0.2 or entry['alphaMid']['opaque'] < 0.05 or entry['alphaMid']['cornersMax'] > 8:
                    fail(f'{pid}/{kind}.{fmt}: alfa niet zoals verwacht {entry["alphaMid"]}')
                entry['seamFirst'] = seam(fr[0], rest)
                entry['seamLast'] = seam(fr[-1], rest)
                for which in ('seamFirst', 'seamLast'):
                    s = entry[which]
                    if s['alphaMean'] > 1.5 or (s['colourMean'] or 0) > 4:
                        fail(f'{pid}/{kind}.{fmt}: {which} wijkt af van rust.webp {s}')
                # Beweegt er echt iets, en komt de duif verder uit de kaart?
                tops = [np.nonzero(x[..., 3].max(axis=1) > 128)[0].min() for x in fr]
                entry['maxRiseAboveRest'] = round(float(tops[0] - min(tops)) / fr.shape[1] * L['height'], 3)
                entry['maxChange'] = round(float(max(np.abs(x[..., 3].astype(int) - rest[..., 3]).mean() for x in fr)), 2)
                if entry['maxChange'] < 2:
                    fail(f'{pid}/{kind}.{fmt}: clip lijkt stil te staan')
                if abs(entry['ms'] - clip['ms']) > 40:
                    fail(f'{pid}/{kind}.{fmt}: duur {entry["ms"]} ms, manifest zegt {clip["ms"]}')
    finish(report)


def finish(report):
    report['problems'] = problems
    report['ok'] = not problems
    os.makedirs(os.path.join(HERE, 'out'), exist_ok=True)
    json.dump(report, open(os.path.join(HERE, 'out', 'check.json'), 'w'), indent=1)
    print(json.dumps(report, indent=1))
    sys.exit(1 if problems else 0)


if __name__ == '__main__':
    main()
