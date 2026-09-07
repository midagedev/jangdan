#!/usr/bin/env python3
"""tools/rack/rear-extend.py — 뒷면 그림에 새 장치 행을 **결정론으로** 덧붙인다(fal 호출 없음).

  python3 tools/rack/rear-extend.py <adopted-rear.png> <rear.json> <새 행 이름> <본뜰 행 이름> \
      <띠 R> <띠 G> <띠 B> <out.png>

왜 재채색이 아닌 이식인가: rear.json에서 같은 포트 구성의 두 행은 rect·이름판 오프셋·잭 오프셋이
**완전히 같다**(예: poly와 sampler 모두 675×200, OUT 잭 (621,116,r12)). 뒷판은 같은 규격의 금속판이라
부품이 같은 것이 사실에 맞고, diffusion 재생성은 잭 원반·나사·슬릿을 매번 다르게 만들어 rear-conform이
어차피 다시 이식한다. 같은 그림 안 이식이므로 잭 클론(게이트 ②)·나사(⑥)가 구조적으로 성립한다.

백킹도 이식이다. chassis 재채색은 실측 σ 14.6·이탈 13.3%로 게이트 ⑧(σ≤2.5·이탈≤1%)에 걸렸다 —
백킹은 무늬가 아니라 바탕이라 새로 그릴 이유가 없다. 본뜬 행에 대한 **같은 상대 위치**의 행을
가져온다(판 위 여백은 판 위에서, 판 아래는 판 아래에서). 위상을 맞추지 않고 단순 타일링하면
판 밑변 바로 아래 행이 엉뚱한 행과 붙어 테두리 그림자의 결이 끊긴다(σ 3.31로 재실패).

띠는 평면 채우기가 아니라 **본뜬 띠의 픽셀별 편차를 목표색에 더한다** — 붓 결이 살아야 옆 칸과
같은 손으로 읽힌다(평면 크림이 스티커로 튄 사고: docs §12.7 conform 규칙).
"""
import sys, json
import numpy as np
from PIL import Image

adopted, rear_json, new_name, src_name, tr, tg, tb, out_path = sys.argv[1:9]
target = np.array([int(tr), int(tg), int(tb)], np.float32)
R = json.load(open(rear_json))
dev = {d['name']: d for d in R['devices']}
W, H = R['size']
px, py, pw, ph = dev[src_name]['rect']
sx, sy, sw, sh = dev[new_name]['rect']
if (pw, ph) != (sw, sh):
    sys.exit(f'rear-extend: {src_name} {pw}x{ph} != {new_name} {sw}x{sh} — 같은 규격의 행만 본뜰 수 있다')
if (sx, sy) < (px, py):
    sys.exit('rear-extend: 새 행은 본뜰 행보다 아래에 있어야 한다')

old = np.asarray(Image.open(adopted).convert('RGB')).astype(np.float32)
H0, W0 = old.shape[:2]
if W0 != W:
    sys.exit(f'rear-extend: 채택 그림 폭 {W0} != rear.json {W}')
GAP = H0 - (py + ph)          # 본뜰 행 아래 백킹 행 수
if GAP <= 0:
    sys.exit('rear-extend: 본뜰 행 아래에 백킹이 없다')

im = np.zeros((H, W, 3), np.float32)
im[:H0] = old                                     # ① 채택 픽셀은 바이트 그대로
for y in range(H0, H):                            # ② 늘어난 띠의 백킹 — 같은 상대 위치에서
    if y < sy:
        im[y] = old[py - (sy - y)]
    elif y >= sy + sh:
        im[y] = old[py + ph + ((y - (sy + sh)) % GAP)]
im[sy:sy + sh] = old[py:py + ph]                  # ③ 행 전체(가장자리 여백 포함) 이식

STRIPE = 20                                       # rear.py: 판 왼쪽 20px가 섹션 색 띠
band = im[sy:sy + sh, sx:sx + STRIPE]
med = np.median(band.reshape(-1, 3), axis=0)
im[sy:sy + sh, sx:sx + STRIPE] = np.clip(band - med + target, 0, 255)

Image.fromarray(np.clip(im, 0, 255).astype(np.uint8)).save(out_path)
chk = np.asarray(Image.open(out_path).convert('RGB')).astype(np.float32)
if not np.array_equal(chk[:H0], old):
    sys.exit(f'rear-extend: y<{H0} 픽셀이 바뀌었다')
sm = np.median(chk[sy + 6:sy + sh - 6, sx + 2:sx + 18].reshape(-1, 3), axis=0).astype(int).tolist()
pm = np.median(chk[py + 6:py + ph - 6, px + 2:px + 18].reshape(-1, 3), axis=0).astype(int).tolist()
print(f'rear-extend: {out_path} {W}x{H} — {new_name} 띠 {sm} · {src_name} 띠 {pm} · y<{H0} 바이트 동일 OK')
