#!/usr/bin/env python3
"""Proefvel: de kaart zoals de site hem opbouwt (plaat, duivenlaag, verloop met
naam) voor het rustbeeld en een reeks clipbeelden, op licht en donker. Alleen
om te beoordelen; niets hiervan gaat de site in.

    python3 tools/etalage-beeld/sheet.py out/v3/final   # of out/v3/draft
"""
import json, os, sys
import numpy as np
from PIL import Image, ImageDraw, ImageFont

HERE = os.path.dirname(os.path.abspath(__file__))
PIGEONS = ('signalerend', 'kennispartner', 'stakeholder')
CARD_W = 300


def font(size):
    for f in ('/System/Library/Fonts/Supplemental/Georgia Bold.ttf', '/System/Library/Fonts/Supplemental/Georgia.ttf'):
        if os.path.exists(f):
            return ImageFont.truetype(f, size)
    return ImageFont.load_default()


def card(work, frame, name, scale=2):
    info = json.load(open(os.path.join(work, 'frames', 'frames.json')))
    L = info['layer']
    cw, ch = CARD_W * scale, round(CARD_W * scale / 0.82)
    lw, lh = round(L['width'] * cw), round(L['height'] * ch)
    lx, ly = round(L['left'] * cw), round(L['top'] * ch)
    ox, oy = max(0, -lx) + 40 * scale, max(0, -ly) + 20 * scale
    W, H = max(cw, lx + lw) + ox + 40 * scale, max(ch, ly + lh) + oy + 20 * scale
    canvas = Image.new('RGBA', (W, H), (0, 0, 0, 0))
    plate = Image.open(os.path.join(work, 'plaat.png')).convert('RGBA').resize((cw, ch), Image.LANCZOS)
    mask = Image.new('L', (cw, ch), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, cw - 1, ch - 1], radius=14 * scale, fill=255)
    grad = np.zeros((ch, cw, 4), dtype=np.uint8)
    t = np.clip((np.arange(ch) / ch - 0.4) / 0.6, 0, 1)
    grad[..., 3] = (t[:, None] * 0.7 * 255).astype(np.uint8)
    plate.alpha_composite(Image.fromarray(grad, 'RGBA'))
    d = ImageDraw.Draw(plate)
    d.text((14 * scale, ch - 44 * scale), name, font=font(22 * scale), fill=(255, 255, 255, 255))
    canvas.paste(plate, (ox, oy), mask)
    layer = Image.open(frame).convert('RGBA').resize((lw, lh), Image.LANCZOS)
    canvas.alpha_composite(layer, (ox + lx, oy + ly))
    # De naam blijft boven de duif leesbaar, zoals in de site (tekst bovenop).
    txt = Image.new('RGBA', canvas.size, (0, 0, 0, 0))
    ImageDraw.Draw(txt).text((ox + 14 * scale, oy + ch - 44 * scale), name, font=font(22 * scale), fill=(255, 255, 255, 255))
    canvas.alpha_composite(txt)
    return canvas


def main():
    base = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, 'out', 'v3', 'final')
    base = base if os.path.isabs(base) else os.path.join(HERE, base)
    cfg = json.load(open(os.path.join(HERE, 'config.json')))
    rows = []
    for kind in ['rest', *cfg['gestures']]:
        cards = []
        for p in PIGEONS:
            work = os.path.join(base, p)
            if not os.path.exists(os.path.join(work, 'frames', 'frames.json')):
                continue
            if kind == 'rest':
                frames = [os.path.join(work, 'frames', 'rest.png')]
            elif os.path.isdir(os.path.join(work, 'frames', kind)):
                names = sorted(os.listdir(os.path.join(work, 'frames', kind)))
                picks = [names[round(i * (len(names) - 1) / 5)] for i in range(1, 5)]
                frames = [os.path.join(work, 'frames', kind, n) for n in picks]
            else:
                frames = []
            for f in frames:
                cards.append(card(work, f, cfg['pigeons'][p]['name']['nl']))
        if cards:
            rows.append(cards)
    for bg, name in (((246, 244, 240), 'licht'), ((36, 36, 38), 'donker')):
        widths = [sum(c.width for c in r) for r in rows]
        heights = [max(c.height for c in r) for r in rows]
        sheet = Image.new('RGB', (max(widths), sum(heights)), bg)
        y = 0
        for r, h in zip(rows, heights):
            x = 0
            for c in r:
                sheet.paste(c, (x, y + h - c.height), c)
                x += c.width
            y += h
        out = os.path.join(base, f'sheet-{name}.png')
        sheet.save(out)
        print(out)


if __name__ == '__main__':
    main()
