#!/usr/bin/env python3
"""tools/pixcheck.py — 캡처 PNG의 수치 검사(비전 없이 렌더 결함을 잡는 게이트).
  python3 tools/pixcheck.py white <png> [--exclude x,y,w,h ...]     # min(R,G,B) ≥ 250 픽셀 수(순백 사각형 버그)
  python3 tools/pixcheck.py diff  <a.png> <b.png> x,y,w,h            # 영역 내 다른 픽셀 수와 비율(오버레이가 실제로 그려졌나)
  python3 tools/pixcheck.py stats <png> x,y,w,h                      # 영역 평균 RGB·표준편차·밝기
  python3 tools/pixcheck.py ink   <png> x,y,w,h                      # 영역에서 어두운(밝기<90) 픽셀 비율(글자가 있나)
좌표는 720×1280 캡처 픽셀(레이아웃 JSON과 같은 좌표계)."""
import sys
from PIL import Image, ImageChops

def rect(s):
    x, y, w, h = [int(float(v)) for v in s.split(',')]
    return (x, y, x + w, y + h)

cmd = sys.argv[1]
if cmd == 'white':
    im = Image.open(sys.argv[2]).convert('RGB')
    ex = [rect(a) for i, a in enumerate(sys.argv[3:]) if sys.argv[3 + i - 1] == '--exclude' or (i > 0 and sys.argv[3 + i - 1] == '--exclude')]
    px = im.load(); n = 0; boxes = {}
    for y in range(im.height):
        for x in range(im.width):
            r, g, b = px[x, y]
            if min(r, g, b) >= 250 and not any(e[0] <= x < e[2] and e[1] <= y < e[3] for e in ex):
                n += 1; k = (x // 40, y // 40); boxes[k] = boxes.get(k, 0) + 1
    top = sorted(boxes.items(), key=lambda kv: -kv[1])[:5]
    print({'white_px': n, 'clusters_40px': [(k[0] * 40, k[1] * 40, v) for k, v in top]})
elif cmd == 'diff':
    a = Image.open(sys.argv[2]).convert('RGB'); b = Image.open(sys.argv[3]).convert('RGB'); r = rect(sys.argv[4])
    d = ImageChops.difference(a.crop(r), b.crop(r)).convert('L')
    hist = d.histogram(); tot = sum(hist); changed = tot - hist[0]; strong = sum(hist[24:])
    print({'region': r, 'changed_px': changed, 'changed_ratio': round(changed / tot, 4), 'strong_px(>=24)': strong})
elif cmd == 'stats':
    im = Image.open(sys.argv[2]).convert('RGB').crop(rect(sys.argv[3]))
    px = list(im.getdata()); n = len(px)
    mean = [sum(p[i] for p in px) / n for i in range(3)]
    lum = [0.299 * p[0] + 0.587 * p[1] + 0.114 * p[2] for p in px]
    ml = sum(lum) / n; sd = (sum((l - ml) ** 2 for l in lum) / n) ** 0.5
    print({'mean_rgb': [round(m, 1) for m in mean], 'lum_mean': round(ml, 1), 'lum_sd': round(sd, 1)})
elif cmd == 'ink':
    im = Image.open(sys.argv[2]).convert('L').crop(rect(sys.argv[3]))
    px = list(im.getdata()); dark = sum(1 for v in px if v < 90); bright = sum(1 for v in px if v > 200)
    print({'dark_ratio': round(dark / len(px), 4), 'bright_ratio': round(bright / len(px), 4), 'n': len(px)})
else:
    print(__doc__); sys.exit(2)
