# Terugvalportretten van de etalage (site/etalage/media/portretten/) uit de
# eindset van model v4: per duif het kaartportret (borstbeeld) en het
# kopportret, zoals de module ze toont, maar op de maat van de etalage.
#
#   kaart → <id>.webp      840 px: de kaart is hoogstens 330 × 402 CSS-px en het
#                          portret staat er met object-fit: cover en scale(1.04)
#                          in, dus ±418 CSS-px hoog; 2× voor retina.
#   kop   → <id>-kop.webp  512 px: de bron zelf, niet geschaald (videoband tot
#                          ±215 CSS-px breed, print 48 mm).
#
# Alleen schalen (met voorvermenigvuldigde alfa, dus zonder donkere rand) en
# omzetten, geen uitsnede: elk portret heeft in de eindset een eigen camera.
# Herkomst en licentie (CC BY-SA 3.0): NOTICE.md in de root van de repository.
#
# Gebruik: python3 tools/etalage-site/portretten.py <map met de eindset> [site-map]
#   bv.    python3 tools/etalage-site/portretten.py <assets>/model-v4/varianten/eind/it17
import hashlib, os, subprocess, sys, tempfile
from PIL import Image

SRC = os.path.expanduser(sys.argv[1])
SITE = sys.argv[2] if len(sys.argv) > 2 else os.path.join(os.path.dirname(__file__), '..', '..', 'site')
OUT = os.path.join(SITE, 'etalage', 'media', 'portretten')
PIGEONS = {'kennispartner': 'biologisch', 'stakeholder': 'individualistisch', 'signalerend': 'signalerend'}
SIZES = {'kaart': 840, 'kop': 512}

os.makedirs(OUT, exist_ok=True)
for site_id, module_id in PIGEONS.items():
    for kind, size in SIZES.items():
        src = os.path.join(SRC, f'{module_id}-{kind}.png')
        img = Image.open(src)
        if img.mode != 'RGBA' or img.width != img.height:
            sys.exit(f'{src}: verwacht een vierkant RGBA-beeld, niet {img.mode} {img.size}')
        if img.width < size:
            sys.exit(f'{src}: {img.width} px, nooit opschalen naar {size}')
        if img.width != size:
            img = img.convert('RGBa').resize((size, size), Image.LANCZOS).convert('RGBA')
        target = os.path.join(OUT, f'{site_id}.webp' if kind == 'kaart' else f'{site_id}-kop.webp')
        with tempfile.TemporaryDirectory() as tmp:
            png = os.path.join(tmp, 'portret.png')
            img.save(png)
            # Verliesgevend met verliesloze alfa, zoals de portretten van de module.
            subprocess.run(['cwebp', '-quiet', '-q', '92', '-alpha_q', '100', '-m', '6', '-sharp_yuv', png, '-o', target], check=True)
        sha = hashlib.sha256(open(src, 'rb').read()).hexdigest()
        print(f'{os.path.relpath(target, SITE)}: {size}×{size}, {os.path.getsize(target)} B, bron {os.path.basename(src)} (sha256 {sha[:16]}…)')
