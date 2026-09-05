#!/usr/bin/env python3
"""tools/font/atlas.py — 라벨용 글리프 아틀라스 생성(ASCII 32..126).
text/v2 + go-text 셰이퍼 + 비트맵 폰트를 앱 wasm에 넣으면 gzip +2.5MB(실측 2026-09-05)라 예산(3.0MB)을 깬다.
대신 빌드 시점에 PNG 아틀라스 하나를 만들고 앱은 DrawImage 서브이미지로 글자를 올린다.
폰트: Go Bold(BSD-3, golang.org/x/image/font/gofont) — 공개 레포 재배포 가능. 한글 시드 단어는 DOM 오버레이가 그린다.
사용: python3 tools/font/atlas.py <Go-Bold.ttf> <out-dir> [px=22]
산출: <out-dir>/atlas.png, <out-dir>/atlas.json {"px","lineHeight","baseline","glyphs":{"A":{"x","y","w","h","adv","ox","oy"}}}"""
import sys, json
from PIL import Image, ImageDraw, ImageFont
ttf, out = sys.argv[1], sys.argv[2]
px = int(sys.argv[3]) if len(sys.argv) > 3 else 22
font = ImageFont.truetype(ttf, px)
asc, desc = font.getmetrics()
glyphs = {}
chars = [chr(c) for c in range(32, 127)]
cell_h = asc + desc + 2
x = 1
widths = []
for ch in chars:
    bbox = font.getbbox(ch)  # (l, t, r, b) relative to origin at baseline-asc
    adv = font.getlength(ch)
    w = max(1, bbox[2] - bbox[0]) + 2
    widths.append((ch, bbox, adv, w))
W = sum(w for _, _, _, w in widths) + 2
img = Image.new('RGBA', (W, cell_h), (0, 0, 0, 0))
d = ImageDraw.Draw(img)
for ch, bbox, adv, w in widths:
    ox = bbox[0]
    d.text((x - ox + 1, 1), ch, font=font, fill=(255, 255, 255, 255))
    glyphs[ch] = {"x": x, "y": 0, "w": w, "h": cell_h, "adv": round(adv, 2), "ox": ox - 1, "oy": 0}
    x += w
img.save(f"{out}/atlas.png", optimize=True)
json.dump({"px": px, "lineHeight": cell_h, "baseline": asc + 1, "glyphs": glyphs}, open(f"{out}/atlas.json", "w"), indent=0)
print("atlas", img.size, len(glyphs), "glyphs")
