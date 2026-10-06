# Deelbeeld 1200x630 voor og:image, samengesteld uit de platen en rustbeelden
# van het manifest (site/etalage/media/duiven): de drie kaarten van de waaier,
# even groot naast elkaar.
#
# Gebruik: python3 tools/etalage-site/deelbeeld.py . site/etalage/media/deel/deel.jpg
# Draai opnieuw als de beeldstroom nieuwe platen of rustbeelden levert.
import json, sys
from PIL import Image, ImageDraw, ImageFilter

ROOT = sys.argv[1]
OUT = sys.argv[2]
m = json.load(open(f'{ROOT}/site/etalage/media/duiven/manifest.json'))
aspect = m['card']['aspect']
W, H = 1200, 630

def hexrgb(h): return tuple(int(h[i:i+2], 16) for i in (1, 3, 5))
KP, SH, SG = hexrgb('#43674c'), hexrgb('#906714'), hexrgb('#5f6b75')

def mix(a, b, t): return tuple(round(a[i] * (1 - t) + b[i] * t) for i in range(3))

# Achtergrond: dezelfde zachte gloed als de videoband.
bg = Image.new('RGB', (W, H), mix((255, 255, 255), SG, .10))
for color, (cx, cy), t in [(KP, (-.05, 1.2), .28), (SH, (1.05, -.25), .24)]:
    glow = Image.new('L', (W, H), 0)
    d = ImageDraw.Draw(glow)
    rx, ry = W * .55, H * .95
    d.ellipse([cx * W - rx, cy * H - ry, cx * W + rx, cy * H + ry], fill=int(255 * t))
    glow = glow.filter(ImageFilter.GaussianBlur(120))
    bg.paste(Image.new('RGB', (W, H), color), (0, 0), glow)
canvas = bg.convert('RGBA')

def card(id, w):
    p = m['pigeons'][id]
    h = round(w / aspect)
    plate = Image.open(f"{ROOT}/site/{p['plate']['src']}").convert('RGB')
    pw, ph = plate.size
    # object-fit: cover
    scale = max(w / pw, h / ph)
    plate = plate.resize((round(pw * scale), round(ph * scale)), Image.LANCZOS)
    x0 = (plate.width - w) // 2; y0 = (plate.height - h) // 2
    plate = plate.crop((x0, y0, x0 + w, y0 + h))
    mask = Image.new('L', (w, h), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, w - 1, h - 1], radius=round(w * .06), fill=255)
    L = p['layer']
    lw, lh = round(L['width'] * w), round(L['height'] * h)
    rest = Image.open(f"{ROOT}/site/{p['rest']['src']}").convert('RGBA')
    # object-fit: contain in de laag
    s = min(lw / rest.width, lh / rest.height)
    rest = rest.resize((round(rest.width * s), round(rest.height * s)), Image.LANCZOS)
    rx = round(L['left'] * w) + (lw - rest.width) // 2
    ry = round(L['top'] * h) + (lh - rest.height) // 2
    return plate, mask, rest, (rx, ry), (w, h)

def place(id, w, cx, top, fade=0.0):
    global canvas
    plate, mask, rest, (rx, ry), (w, h) = card(id, w)
    x = round(cx - w / 2)
    # schaduw
    sh = Image.new('L', (W, H), 0)
    ImageDraw.Draw(sh).rounded_rectangle([x + 6, top + 14, x + w + 6, top + h + 14], radius=round(w * .06), fill=70)
    sh = sh.filter(ImageFilter.GaussianBlur(18))
    canvas = Image.composite(Image.new('RGBA', (W, H), (0, 0, 0, 255)), canvas, sh)
    layer = Image.new('RGBA', (W, H), (0, 0, 0, 0))
    layer.paste(plate, (x, top), mask)
    layer.alpha_composite(rest, (x + rx, top + ry))
    if fade:
        a = layer.getchannel('A').point(lambda v: round(v * (1 - fade)))
        layer.putalpha(a)
    canvas = Image.alpha_composite(canvas, layer)

# Drie gelijke kaarten naast elkaar: een stil beeld is geen keuze, dus
# geen duif vooraan. De volgorde van de waaier (pigeons.js, ORDER): links de
# Signalerende duif, in het midden de Kennispartner, rechts de Stakeholder; de
# buitenste twee kijken naar het midden.
cw = 272
ch = round(cw / aspect)
gap = 40
top = round((H - ch) / 2 + ch * .19 / 2) + 6
for i, id in enumerate(['signalerend', 'kennispartner', 'stakeholder']):
    place(id, cw, W / 2 + (i - 1) * (cw + gap), top)
canvas.convert('RGB').save(OUT, quality=84, optimize=True, progressive=True)
print(OUT, canvas.size)
