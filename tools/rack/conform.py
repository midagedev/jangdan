#!/usr/bin/env python3
"""tools/rack/conform.py — 칠해진 새 모듈(mixer·fx2) 후보를 기존 채택 패널의 부품으로 맞춘다.

  python3 tools/rack/conform.py <panel-v3.png> <layout-v3.json> <panel.png(채택)> <out.png> \
      --ymin 1280 [--fill x0,y0,x1,y1 ...] [--plates mixer] [--report out.json] [--crops DIR]
  python3 tools/rack/conform.py --check <png> <layout-v3.json> <panel.png> [--ymin 1280] [--fill ...]

비전 판정(2026-09-06, 채택 불가 ×2)의 FIX를 결정론적 픽셀 연산으로 닫는다 — fal 재채색 없음:
  A 부품 불일치 — 노브는 기존 패널(y<ymin)에서 반지름 클래스별 "가장 깨끗한" 것(cut-knobs.py 기준:
    면 안 평균 채도 최소)을 골라 원판을 이식한다. 원판 반지름 = keep_r: r+18(눈금 고리 포함 — scrub.py의
    keep 반지름)이되 이웃 노브 중심거리/2를 넘지 않는다(믹서 1행 피치 75 → 37; r+18 원판이 11px 겹쳐
    눈금 토막이 보였다). 기증자 이웃 노브의 라벨판 픽셀은 싣지 않는다(드럼 기증자의 위 라벨판이 '헬멧'으로
    따라왔다). 노브 아래 라벨판(레이아웃에 없다 — knob_plate_rect로 유도)은 기증자의 판을 따로 복사한다.
    섹션 이름판은 --plate-src(기본 drums) 이름판 픽셀을 여백 6px 포함해 복사한다(평면 크림은 옆 손그림
    판과 나란히 놓이면 스티커로 튀었다). 정확한 r이 없으면 가장 가까운 r을 Lanczos 스케일(stderr 경고).
  B 미선언 자국 — layout 허용 마스크(노브 keep_r 원판·노브 라벨판·rect+2·LED r+10·모듈 테두리 안팎 8px·
    섹션 색 띠) 밖에서 기준색(모듈 면/백킹별 허용 밖 픽셀 중앙값 — 기준색 필드는 모듈 rect 전체에 깐다,
    LED 자리 메움이 백킹색 구멍으로 들어간 결함)과의 RGB 거리 > T(22)인 픽셀을 자국으로 잡아 5px 팽창 후
    기준색 평면 + 결정론 저주파 그레인(σ≈6, 클립 ±12 → 거리 ≤ 20.8 < T) + 가장자리 3px 페더로 메운다
    (median 필터가 아니다 — 로커 스위치 크기 덩어리는 median 19로 안 덮인다; 표준편차 0 평면은 구멍으로
    읽혔다). --fill rect는 임계와 무관하게 같은 방법으로 메운다(노브 원판·라벨판·plates·buttons는 허용이
    이긴다. LED는 앱이 그리므로 --fill 안에서는 메운다 — 유일한 예외). 페더 층의 알파는 자국 픽셀
    (거리>T)에서는 1로 올린다 — 가장자리 램프가 쓰레기를 재노출하는 것을 막는다.

y < ymin 픽셀은 바이트 그대로 — 마스크·팽창·페더 전부 ymin에서 잘라낸다(scrub.py --rows 방식).
--check는 게이트를 재측정한다: ③ 노브 고리(r+3..r+11) 휘도 중앙값 ±4·캡(r−4) 평균 ±3(스케일 노브 ±6)·
노브 라벨판 안쪽 휘도 ±8, ④ 자국 잔류 ≤ 0.3% + 지정 rect 내 최대 거리 ≤ 22, ⑤ 이름판 안쪽(3px 제외)이 원본 이름판과 최대 차 ≤ 6,
⑥ 이음새 행 1270..1279 vs 1280..1289 평균 휘도 차 ≤ 12, ① y<ymin == 채택 패널.
"""
import sys, os, json, argparse
import numpy as np
from PIL import Image

T_MARK = 22          # 자국 판정 RGB 유클리드 거리 임계
KEEP_KNOB = 18       # 노브 원판 여유(눈금 고리 포함) — scrub.py keep 과 같다
DILATE_MARK = 5      # 자국 마스크 팽창(px, 원판 구조 요소)
FEATHER = 3          # 이식 원판·판 가장자리 페더(px)
FILL_FEATHER = 5     # 자국·지정 메움 가장자리 페더(px) — 직사각 윤곽 완화(비전 v3)
GRAIN_CELL = 16      # 그레인 값 노이즈 셀(px, 이중선형 보간) — 픽셀 노이즈는 lag8 자기상관 0.05로 '사포질'로 읽혔다(비전 v3; 칠해진 면은 0.56)
RECT_PAD = 2         # plates·buttons 등 rect의 허용 여유(px) — scrub.py 와 같다
LED_PAD = 10         # LED 원판 허용 여유(led.r + 10)
BORDER_BAND = 8      # 모듈 테두리 띠(안팎 8px)
# 노브 아래 라벨판(wire.py knob(): 설계 좌표 [cx-r-2, cy+r+10, cx+r+2, cy+r+26], SY≈0.952) — layout.json에
# 항목이 없어 허용 마스크 밖이었고, 기준색 거리 판정이 밝은 크림판을 "자국"으로 지웠다(2026-09-06 리드 실측:
# 새 모듈 라벨판 자리 휘도 150~188 → 99). 노브 부품의 일부로 취급한다: 허용·보호 마스크에 넣고 기증자의 판을 함께 이식한다.
PLATE_MARGIN = 6     # 섹션 이름판 이식 시 rect 바깥 여백(그림자·둥근 모서리 포함)
PLATE_DX = 5         # 라벨판 rect 가로 여유(px) — cx ± (r + PLATE_DX)
PLATE_DY0, PLATE_DY1 = 7, 27   # 라벨판 rect 세로 범위 — cy + r + [DY0, DY1)

def keep_r(k, knobs):
    """이식·허용 원판 반지름: r+KEEP_KNOB, 단 이웃 노브 중심거리/2(내림)를 넘지 않는다 — 믹서 1행 피치 75px에서
    r+18 원판이 11px 겹쳐 눈금 토막·좌우 비대칭이 보였다(비전 2026-09-06). r25 → 37(r+12), fx2 r32 → 50(r+18)."""
    R = k['r'] + KEEP_KNOB
    for o in knobs:
        if o is k: continue
        d = ((o['cx']-k['cx'])**2 + (o['cy']-k['cy'])**2) ** 0.5
        if d < 2*R: R = min(R, int(d//2))
    return R

def grain_field(shape, seed=1234, std=6.0, clip=12.0):
    """평면 메움에 얹는 결정론 저주파 그레인(회색, 3채널 공통). 표준편차 0인 면은 뚫린 자리로 읽혔다(비전 2026-09-06).
    클립 ±12 → 기준색 거리 √3·12 ≈ 20.8 < T_MARK — 게이트 ④가 그레인을 자국으로 세지 않는다."""
    rng = np.random.default_rng(seed)
    H, W = shape
    ch, cw = H//GRAIN_CELL + 2, W//GRAIN_CELL + 2
    coarse = rng.normal(0, 1, (ch, cw)).astype(np.float32)
    n = np.asarray(Image.fromarray(coarse).resize((cw*GRAIN_CELL, ch*GRAIN_CELL), Image.BILINEAR), np.float32)[:H, :W]
    n = n / n.std() * std
    return np.clip(n, -clip, clip)

def knob_plate_rect(k):
    """노브 k 아래 라벨판 rect [x0,y0,x1e,y1e) — 노브 중심·반지름에서 유도(레이아웃에 없다)."""
    cx, cy, r = int(k['cx']), int(k['cy']), int(k['r'])
    return cx-r-PLATE_DX, cy+r+PLATE_DY0, cx+r+PLATE_DX+1, cy+r+PLATE_DY1
# P4-rackart-b 라운드의 지정 메움(리드 스펙 표에서 유도) — --check 를 --fill 없이 부르면 이 값으로 잰다.
# 보존 원판(x,y,r): diffusion이 그린 정상 하드웨어(모듈 상단 나사)가 자국 판정에 지워졌다(비전 v3) — 허용·보호 둘 다.
P4_RACKART_B_KEEPS = [(438,1294,7),(519,1294,7),(597,1295,7),(35,1299,7),(679,1507,5)]  # 비전 v4: 나사 4개 + 우하단 장식 홈
P4_RACKART_B_FILLS = [(546,1434,682,1498),(462,1550,500,1592),
                      (266,1718,350,1760),(636,1776,694,1800),(474,1300,504,1316),
                      (410,1538,458,1585),(496,1423,532,1457)]

def die(msg):
    sys.exit('conform: ' + msg)

def load_rgb(path):
    return np.asarray(Image.open(path).convert('RGB'), dtype=np.uint8)

def lum(a):
    return 0.299*a[...,0].astype(np.float32) + 0.587*a[...,1].astype(np.float32) + 0.114*a[...,2].astype(np.float32)

def rect_of(e):
    x,y,w,h = e['rect']; return x,y,x+w,y+h   # [x0,y0,x1e,y1e) 배타 상한

def rect_mask(shape, x0,y0,x1e,y1e):
    m = np.zeros(shape, bool)
    x0,y0 = max(0,x0), max(0,y0); x1e,y1e = min(shape[1],x1e), min(shape[0],y1e)
    if x1e>x0 and y1e>y0: m[y0:y1e, x0:x1e] = True
    return m

def disc_mask(shape, cx, cy, R):
    m = np.zeros(shape, bool)
    x0,y0,x1,y1 = max(0,int(cx-R)), max(0,int(cy-R)), min(shape[1],int(cx+R)+1), min(shape[0],int(cy+R)+1)
    if x1<=x0 or y1<=y0: return m
    yy,xx = np.mgrid[y0:y1, x0:x1]
    m[y0:y1, x0:x1] = (yy-cy)**2 + (xx-cx)**2 <= R*R
    return m

def disc_offsets(r):
    return [(dy,dx) for dy in range(-r,r+1) for dx in range(-r,r+1) if dy*dy+dx*dx <= r*r]

def _shift(m, dy, dx):
    out = np.zeros_like(m)
    ys,yd = slice(max(0,dy), m.shape[0]+min(0,dy)), slice(max(0,-dy), m.shape[0]+min(0,-dy))
    xs,xd = slice(max(0,dx), m.shape[1]+min(0,dx)), slice(max(0,-dx), m.shape[1]+min(0,-dx))
    out[yd,xd] = m[ys,xs]
    return out

def mdilate(m, r):
    if r <= 0: return m.copy()
    out = np.zeros_like(m)
    for dy,dx in disc_offsets(r): out |= _shift(m, dy, dx)
    return out

def merode(m, r):
    return ~mdilate(~m, r)

def feather_alpha(base, n=FEATHER):
    """base 영역의 가장자리 n px 를 1/n·2/n·…·1 선형 램프로 섞는 알파(0..1). 바깥은 0."""
    a = np.zeros(base.shape, np.float32)
    cur = base
    for k in range(1, n+1):
        a[cur] = k/float(n)
        cur = merode(cur, 1)
    a[cur] = 1.0
    return a

def build_allowed(L, W, H, ymin, keeps=()):
    """새 띠의 허용 마스크(scrub.py keep 과 같은 요소 + LED r+10). ymin 위로는 절대 참이 되지 않는다."""
    shape = (H,W)
    A = np.zeros(shape, bool)
    for k in L['knobs']:
        A |= disc_mask(shape, k['cx'], k['cy'], keep_r(k, L['knobs']))
        A |= rect_mask(shape, *knob_plate_rect(k))            # 노브 아래 라벨판(부품의 일부)
    for key in ('buttons','pads','plates','displays'):
        for b in L.get(key,[]):
            x,y,x1,y1 = rect_of(b)
            A |= rect_mask(shape, x-RECT_PAD, y-RECT_PAD, x1+RECT_PAD, y1+RECT_PAD)
    for l in L.get('leds',[]):
        A |= disc_mask(shape, l['cx'], l['cy'], l['r']+LED_PAD)
    for (kx,ky,kr) in keeps:
        A |= disc_mask(shape, kx, ky, kr)
    sec_w = round(20*W/768)
    for p in L['panels']:
        x,y,x1,y1 = rect_of(p)
        outer = rect_mask(shape, x-BORDER_BAND, y-BORDER_BAND, x1+BORDER_BAND, y1+BORDER_BAND)
        inner = rect_mask(shape, x+BORDER_BAND, y+BORDER_BAND, x1-BORDER_BAND, y1-BORDER_BAND)
        A |= outer & ~inner                                    # 모듈 테두리 띠(안팎 8px)
        A |= rect_mask(shape, x, y, x+sec_w+1, y1)             # 섹션 색 띠(왼쪽 20·W/768 px)
    if 'scope' in L:
        x,y,x1,y1 = rect_of({'rect': L['scope']['rect']}); A |= rect_mask(shape, x-8, y-8, x1+8, y1+8)
    if 'display' in L:
        x,y,x1,y1 = rect_of({'rect': L['display']['rect']}); A |= rect_mask(shape, x-4, y-4, x1+4, y1+4)
    band = np.zeros(shape, bool); band[ymin:,:] = True
    return A & band

def build_protect(L, W, H, ymin, keeps=()):
    """--fill 이 존중하는 보호 마스크(노브 원판·rect+2). LED·테두리·섹션 띠는 없다 — LED는 앱이 그린다."""
    shape = (H,W)
    P = np.zeros(shape, bool)
    for k in L['knobs']:
        P |= disc_mask(shape, k['cx'], k['cy'], keep_r(k, L['knobs']))
        P |= rect_mask(shape, *knob_plate_rect(k))
    for key in ('buttons','pads','plates','displays'):
        for b in L.get(key,[]):
            x,y,x1,y1 = rect_of(b)
            P |= rect_mask(shape, x-RECT_PAD, y-RECT_PAD, x1+RECT_PAD, y1+RECT_PAD)
    for (kx,ky,kr) in keeps:
        P |= disc_mask(shape, kx, ky, kr)
    band = np.zeros(shape, bool); band[ymin:,:] = True
    return P & band

def cleanest_by_r(L, hsv_old, ymin):
    """반지름 클래스별 '가장 깨끗한' 기존 노브 — cut-knobs.py 기준 복제(면 안 타원의 평균 채도 최소).
    cut-knobs.py 는 top-level 스크립트라 임포트하면 즉시 실행되므로 동일 로직을 여기에 둔다."""
    best = {}
    for k in L['knobs']:
        if k['cy'] >= ymin: continue
        r = k['r']; half = r-4                      # crop (cx-r+4, cy-r+4, cx+r-4, cy+r-4) 와 동일
        x0,y0,x1,y1 = k['cx']-half, k['cy']-half, k['cx']+half, k['cy']+half
        if x0 < 0 or y0 < 0 or x1 >= hsv_old.shape[1] or y1 >= hsv_old.shape[0]: continue
        yy,xx = np.mgrid[y0:y1+1, x0:x1+1]
        c = ((yy-k['cy'])**2 + (xx-k['cx'])**2) <= half*half   # 내접 타원 == 반지름 half 원판
        sat = hsv_old[y0:y1+1, x0:x1+1, 1][c].mean()
        if r not in best or sat < best[r][0]: best[r] = (sat, k)
    return best

def pick_source(best, r):
    """r 정확 일치 우선, 없으면 가장 가까운 r(경고). 반환: (스케일 여부, 원본 노브)."""
    if not best: die('no old knobs below ymin — nothing to transplant')
    if r in best: return False, best[r][1]
    near = min(best, key=lambda rr: abs(rr-r))
    print(f'conform: WARNING radius class r={r} has no old candidate — using nearest r={near} '
          f'({best[near][1]["name"]}, scaled {r}/{near})', file=sys.stderr)
    return True, best[near][1]

def knob_stats(arr, cx, cy, r):
    """게이트 ③ 측정: 고리(r+3..r+11 — 이식 원판 최소 반지름 r+12(keep_r, 믹서 피치 75) 안쪽) 휘도 중앙값, 캡(r−4 원판) 휘도 평균."""
    x0,y0 = max(0,cx-r-11), max(0,cy-r-11)
    x1,y1 = min(arr.shape[1],cx+r+12), min(arr.shape[0],cy+r+12)
    yy,xx = np.mgrid[y0:y1, x0:x1]
    d2 = (yy-cy)**2 + (xx-cx)**2
    L = lum(arr[y0:y1, x0:x1])
    ring = (d2 >= (r+3)**2) & (d2 <= (r+11)**2)
    cap  = d2 <= (r-4)**2
    return float(np.median(L[ring])), float(L[cap].mean())

def paste_disc(out, panel_old, src, dst, scaled, ymin, Rd, excl):
    """원본 노브 중심의 원판(반지름 Rd — keep_r)을 새 노브 중심에 복사(가장자리 3px 선형 페더). ymin 위로는
    쓰지 않는다. excl(기존 패널 좌표 bool)은 기증자 이웃 노브들의 라벨판 — 그 픽셀은 싣지 않는다(믹서 노브가
    드럼 기증자의 위 라벨판 조각을 '헬멧'처럼 쓰고 왔던 비전 FIX 2026-09-06)."""
    Rs = Rd if not scaled else int(round(Rd * (2*src['r']+1) / (2*dst['r']+1)))
    cxs, cys = src['cx'], src['cy']
    x0,y0,x1,y1 = cxs-Rs, cys-Rs, cxs+Rs+1, cys+Rs+1
    if x0 < 0 or y0 < 0 or x1 > panel_old.shape[1] or y1 > panel_old.shape[0]:
        die(f'source knob {src["name"]} disc R{Rs} exceeds panel.png bounds')
    patch = panel_old[y0:y1, x0:x1].astype(np.float32)
    keepm = (~excl[y0:y1, x0:x1]).astype(np.float32)
    n = patch.shape[0]
    if scaled:
        n2 = 2*Rd + 1
        patch = np.asarray(Image.fromarray(patch.astype(np.uint8)).resize((n2,n2), Image.LANCZOS), np.float32)
        keepm = np.asarray(Image.fromarray((keepm*255).astype(np.uint8)).resize((n2,n2), Image.NEAREST), np.float32)/255
        n = n2
    R = (n-1)/2.0
    yy,xx = np.mgrid[0:n, 0:n]
    d = np.sqrt((yy-R)**2 + (xx-R)**2)
    alpha = np.clip((R + 1.0 - d)/FEATHER, 0, 1)     # 포함 경계 1/3 → 2/3 → 1(메움 램프와 동일 위상)
    alpha[d > R] = 0.0
    alpha *= keepm
    H, W = out.shape[:2]
    px0,py0,px1,py1 = int(dst['cx']-R), int(dst['cy']-R), int(dst['cx']+R+1), int(dst['cy']+R+1)
    ty0, ty1 = max(py0, ymin, 0), min(py1, H)        # ymin 에서 자른다 — 위 픽셀은 바이트 그대로
    if ty1 <= ty0: return
    sx0, sx1 = max(px0,0)-px0, min(px1,W)-px0
    sy0, sy1 = ty0-py0, ty1-py0
    a = alpha[sy0:sy1, sx0:sx1, None]
    reg = out[ty0:ty1, max(px0,0):min(px1,W)]
    out[ty0:ty1, max(px0,0):min(px1,W)] = patch[sy0:sy1, sx0:sx1]*a + reg*(1.0-a)

def paste_plate(out, panel_old, src, dst, scaled, ymin):
    """기증자 노브 아래 라벨판 rect를 새 노브 아래에 복사(가장자리 FEATHER px 선형 페더). 노브 원판 이식의 짝 —
    라벨판은 레이아웃에 없어 이식·허용 어디에도 안 들어 있었다(헤더 주석). 스케일 노브는 원판과 같은 지름 비율."""
    sx0,sy0,sx1,sy1 = knob_plate_rect(src)
    if sx0 < 0 or sy0 < 0 or sx1 > panel_old.shape[1] or sy1 > panel_old.shape[0]:
        die(f'source knob {src["name"]} plate rect exceeds panel.png bounds')
    patch = panel_old[sy0:sy1, sx0:sx1].astype(np.float32)
    dx0,dy0,dx1,dy1 = knob_plate_rect(dst)
    if scaled:
        patch = np.asarray(Image.fromarray(patch.astype(np.uint8)).resize((dx1-dx0, dy1-dy0), Image.LANCZOS), np.float32)
    h, w = patch.shape[:2]
    yy,xx = np.mgrid[0:h, 0:w]
    dist = np.minimum(np.minimum(yy+1, h-yy), np.minimum(xx+1, w-xx)).astype(np.float32)  # 가장자리까지 거리(1..)
    alpha = np.clip(dist/FEATHER, 0, 1)
    H, W = out.shape[:2]
    ty0, ty1 = max(dy0, ymin, 0), min(dy1, H)
    tx0, tx1 = max(dx0, 0), min(dx1, W)
    if ty1 <= ty0 or tx1 <= tx0: return
    a = alpha[ty0-dy0:ty1-dy0, tx0-dx0:tx1-dx0, None]
    reg = out[ty0:ty1, tx0:tx1]
    out[ty0:ty1, tx0:tx1] = patch[ty0-dy0:ty1-dy0, tx0-dx0:tx1-dx0]*a + reg*(1.0-a)

def paste_rect(out, panel_old, src_rect, dst_rect, ymin, margin, excl_x_lt=None):
    """기존 패널의 rect(+margin)를 같은 크기의 목적지 rect(+margin)에 복사, 가장자리 FEATHER 페더. 크기가 다르면 Lanczos."""
    sx,sy,sw,sh = src_rect; dx,dy,dw,dh = dst_rect
    patch = panel_old[sy-margin:sy+sh+margin, sx-margin:sx+sw+margin]
    if (sw,sh) != (dw,dh):
        patch = np.asarray(Image.fromarray(patch.astype(np.uint8)).resize((dw+2*margin, dh+2*margin), Image.LANCZOS), np.float32)
    h, w = patch.shape[:2]
    yy,xx = np.mgrid[0:h, 0:w]
    dist = np.minimum(np.minimum(yy+1, h-yy), np.minimum(xx+1, w-xx)).astype(np.float32)
    alpha = np.clip(dist/FEATHER, 0, 1)
    x0,y0 = dx-margin, dy-margin
    if excl_x_lt is not None and excl_x_lt > x0:     # 섹션 색 띠 열은 싣지 않는다(기증자 띠 색이 딸려 온 비전 v3 FIX)
        alpha[:, :min(w, excl_x_lt-x0)] = 0.0
    H, W = out.shape[:2]
    ty0, ty1 = max(y0, ymin, 0), min(y0+h, H); tx0, tx1 = max(x0, 0), min(x0+w, W)
    if ty1 <= ty0 or tx1 <= tx0: return 0
    a = alpha[ty0-y0:ty1-y0, tx0-x0:tx1-x0, None]
    out[ty0:ty1, tx0:tx1] = patch[ty0-y0:ty1-y0, tx0-x0:tx1-x0]*a + out[ty0:ty1, tx0:tx1]*(1.0-a)
    return int((a[...,0] > 0).sum())

def plate_lum(arr, k):
    """노브 k 아래 라벨판 안쪽(가장자리 3px 제외) 평균 휘도 — 게이트 ③b(라벨판 실재)의 측정축."""
    x0,y0,x1,y1 = knob_plate_rect(k)
    return float(lum(arr[y0+3:y1-3, x0+3:x1-3]).mean())

def region_refs(out, allowed, panels, band, shape):
    """모듈 면별·백킹 기준색(허용 밖 픽셀 중앙값)과 위치별 기준색 필드 C, 거리장, 자국 마스크."""
    nonal = band & ~allowed
    inmod = np.zeros(shape, bool)
    for p in panels:
        x,y,x1,y1 = rect_of(p); inmod |= rect_mask(shape, x, y, x1, y1)
    refs, C = {}, np.zeros(shape+(3,), np.float32)
    back_px = out[nonal & ~inmod]
    if len(back_px) == 0: die('no backing pixels below ymin — check layout')
    back_ref = np.median(back_px, axis=0)
    refs['backing'] = [round(float(v),1) for v in back_ref]
    C[:] = back_ref
    for p in panels:
        x,y,x1,y1 = rect_of(p)
        m = nonal & rect_mask(shape, x, y, x1, y1)
        px = out[m]
        if len(px) == 0: die(f'module {p["name"]} has no unprotected face pixels')
        ref = np.median(px, axis=0)
        refs[p['name']] = [round(float(v),1) for v in ref]
        C[rect_mask(shape, x, y, x1, y1)] = ref   # 모듈 rect 전체 — 허용 픽셀(LED 자리)도 모듈 면색(비전 FIX: 백킹색 구멍)
    dist = np.sqrt(((out - C)**2).sum(axis=2))
    junk = band & (dist > T_MARK)          # 허용 여부와 무관한 원시 자국(페더 알파 상향용)
    marks = nonal & junk                   # 판정·카운트는 허용 밖에서만
    return C, dist, junk, marks, refs, inmod

def apply_fill(out, mask, alpha, C, G=None):
    """마스크 안을 기준색 C(+그레인 G)로 알파 합성. G가 None이면 평면."""
    m = mask & (alpha > 0)
    if not m.any(): return 0
    a = alpha[m][:,None]
    fillv = C[m] + (G[m][:,None] if G is not None else 0.0)
    out[m] = out[m]*(1.0-a) + fillv*a
    return int(m.sum())

def plate_pixels(arr, x, y, x1, y1, inner):
    if inner: return arr[y+1:y1-1, x+1:x1-1]
    m = np.zeros(arr.shape[:2], bool); m[y:y1, x:x1] = True
    m[y+1:y1-1, x+1:x1-1] = False
    return arr[m]

def old_plate_stats(panel_old, old_plates):
    inner_px, border_px = [], []
    for p in old_plates:
        x,y,x1,y1 = rect_of(p)
        inner_px.append(plate_pixels(panel_old, x, y, x1, y1, True).reshape(-1,3))
        border_px.append(plate_pixels(panel_old, x, y, x1, y1, False).reshape(-1,3))
    return np.median(np.concatenate(inner_px), axis=0), np.median(np.concatenate(border_px), axis=0)

def measure_gates(arr, L, panel_old, hsv_old, ymin, fills, verbose, plate_src='drums', keeps=()):
    """게이트 ①③④⑤⑥ 측정(--check 본체, conform 실행 시 report 에도 들어간다)."""
    H, W = arr.shape[:2]
    band = np.zeros((H,W), bool); band[ymin:,:] = True
    allowed = build_allowed(L, W, H, ymin, keeps)
    protect = build_protect(L, W, H, ymin, keeps)
    old_knobs = [k for k in L['knobs'] if k['cy'] < ymin]
    new_knobs = [k for k in L['knobs'] if k['cy'] >= ymin]
    best = cleanest_by_r(L, hsv_old, ymin)
    panels_band = [p for p in L['panels'] if rect_of(p)[3] > ymin]
    g = {'g1_top_unchanged': None, 'g3_knobs': [], 'g4': {}, 'g5_plates': [], 'g6_seam': {}}
    if panel_old.shape[0] >= ymin:
        g['g1_top_unchanged'] = bool(np.array_equal(arr[:ymin], panel_old[:ymin]))
    for k in new_knobs:
        scaled, src = pick_source(best, k['r'])
        tol = 6.0 if scaled else 4.0
        ctol = 6.0 if scaled else 3.0
        rs, cs = knob_stats(panel_old.astype(np.float32), src['cx'], src['cy'], src['r'])
        rn, cn = knob_stats(arr.astype(np.float32), k['cx'], k['cy'], k['r'])
        pn, ps = plate_lum(arr.astype(np.float32), k), plate_lum(panel_old.astype(np.float32), src)
        g['g3_knobs'].append({'name': k['name'], 'section': k['section'], 'src': src['name'],
                              'ring': round(rn,1), 'ring_src': round(rs,1), 'dring': round(rn-rs,2),
                              'cap': round(cn,1), 'cap_src': round(cs,1), 'dcap': round(cn-cs,2),
                              'plate': round(pn,1), 'plate_src': round(ps,1), 'dplate': round(pn-ps,2),
                              'scaled': scaled,
                              # ③b 라벨판 실재: 안쪽 평균 휘도가 기증자 ±8(FAIL-first 2026-09-06: 지워진 판은 99 vs 150~188)
                              'pass': bool(abs(rn-rs) <= tol and abs(cn-cs) <= ctol and abs(pn-ps) <= 8.0)})
    C, dist, junk, marks, refs, inmod = region_refs(arr.astype(np.float32), allowed, panels_band, band, (H,W))
    band_px = int(band.sum())
    res_px = int(marks.sum())
    g['g4'] = {'refs': refs, 'residue_px': res_px, 'band_px': band_px,
               'residue_ratio': round(res_px/band_px, 6), 'pass': bool(res_px/band_px <= 0.003),
               'fills': []}
    for (x0,y0,x1,y1) in fills:
        m = rect_mask((H,W), x0, y0, x1, y1) & band & ~protect
        md = float(dist[m].max()) if m.any() else 0.0
        g['g4']['fills'].append({'rect': [x0,y0,x1,y1], 'max_dist': round(md,1), 'pass': bool(md <= T_MARK)})
        g['g4']['pass'] = g['g4']['pass'] and md <= T_MARK
    srcp = next((p for p in L['plates'] if p.get('for') == plate_src and p['rect'][1] < ymin), None)
    if srcp is None: die(f'plate src {plate_src} not found above ymin')
    sx,sy,sw,sh = srcp['rect']
    src_in = panel_old[sy+3:sy+sh-3, sx+3:sx+sw-3].astype(np.float32)
    for p in L['plates']:
        if p.get('for') != 'mixer': continue
        x,y,w,h = p['rect']
        # 이식은 섹션 색 띠 열(host.x + sec_w + 6 미만)을 싣지 않으므로 그 열은 비교에서도 뺀다(원본과 같은 열끼리).
        host = next((q for q in L['panels'] if rect_of(q)[0] <= x < rect_of(q)[2] and rect_of(q)[1] <= y < rect_of(q)[3]), None)
        cut = max(0, (rect_of(host)[0] + round(20*W/768) + 6) - (x+3)) if host else 0
        dst_in = arr[y+3:y+h-3, x+3+cut:x+w-3].astype(np.float32)
        src_in_c = src_in[:, cut:]
        ok = dst_in.shape == src_in_c.shape
        mad = float(np.abs(dst_in - src_in_c).max()) if ok else 999.0
        std = dst_in.reshape(-1,3).std(axis=0)
        med = np.median(dst_in.reshape(-1,3), axis=0); smed = np.median(src_in_c.reshape(-1,3), axis=0)
        g['g5_plates'].append({'for': 'mixer', 'rect': list(p['rect']), 'src': plate_src,
                               'std': [round(float(v),2) for v in std],
                               'median': [round(float(v),1) for v in med],
                               'median_src': [round(float(v),1) for v in smed],
                               'max_abs_diff': round(mad,1),
                               # 이식 사본이므로 안쪽(3px 제외)은 원본과 같아야 한다(FAIL-first: 평면 크림은 max diff ≫ 6)
                               'pass': bool(mad <= 6)})
    a, b = lum(arr[1270:1280]).mean(), lum(arr[1280:1290]).mean()
    g['g6_seam'] = {'above': round(float(a),2), 'below': round(float(b),2),
                    'diff': round(float(abs(a-b)),2), 'pass': bool(abs(a-b) <= 12)}
    g['pass'] = bool((g['g1_top_unchanged'] is not False) and all(k['pass'] for k in g['g3_knobs'])
                     and g['g4']['pass'] and all(p['pass'] for p in g['g5_plates']) and g['g6_seam']['pass'])
    if verbose:
        print(f'conform: check gates on {W}x{H}, ymin={ymin}, new knobs {len(new_knobs)}, fills {len(fills)}')
        print(f"  g1  y<{ymin} == panel.png : {'PASS' if g['g1_top_unchanged'] else 'FAIL'}")
        bad = [k for k in g['g3_knobs'] if not k['pass']]
        nok = len(g['g3_knobs']) - len(bad); ntot = len(g['g3_knobs'])
        worst_r = max(g['g3_knobs'], key=lambda k: abs(k['dring']))
        worst_c = max(g['g3_knobs'], key=lambda k: abs(k['dcap']))
        worst_p = max(g['g3_knobs'], key=lambda k: abs(k['dplate']))
        print(f"  g3  knobs {nok}/{ntot} pass — worst ring "
              f"{worst_r['name']} {worst_r['ring']} vs src {worst_r['ring_src']} (d{worst_r['dring']:+.1f}), "
              f"worst cap {worst_c['name']} {worst_c['cap']} vs src {worst_c['cap_src']} (d{worst_c['dcap']:+.1f}), "
              f"worst plate {worst_p['name']} {worst_p['plate']} vs src {worst_p['plate_src']} (d{worst_p['dplate']:+.1f})"
              f" : {'PASS' if not bad else 'FAIL ' + ','.join(k['name'] for k in bad)}")
        print(f"  g4  residue {g['g4']['residue_px']} px = {g['g4']['residue_ratio']*100:.3f}% of band "
              f"(<= 0.3%) refs {refs} : {'PASS' if g['g4']['pass'] else 'FAIL'}")
        for f in g['g4']['fills']:
            print(f"      fill {f['rect']} max_dist {f['max_dist']} (<= {T_MARK}) : {'PASS' if f['pass'] else 'FAIL'}")
        for p in g['g5_plates']:
            print(f"  g5  plate mixer {p['rect']} <- {p['src']} max|diff| {p['max_abs_diff']} (<= 6) std {p['std']} median {p['median']} vs {p['median_src']} "
                  f": {'PASS' if p['pass'] else 'FAIL'}")
        print(f"  g6  seam {g['g6_seam']['above']} vs {g['g6_seam']['below']} "
              f"(diff {g['g6_seam']['diff']} <= 12) : {'PASS' if g['g6_seam']['pass'] else 'FAIL'}")
        print(f'conform: gates {"PASS" if g["pass"] else "FAIL"}')
    return g

def parse_keeps(strings, W, H):
    out = []
    for t in strings:
        try: x,y,r = map(int, t.split(','))
        except ValueError: die(f'--keep expects x,y,r (got {t!r})')
        if not (0 <= x < W and 0 <= y < H and 1 <= r <= 64): die(f'--keep {t}: out of canvas or bad radius')
        out.append((x,y,r))
    return out

def parse_fills(strings, W, H):
    fills = []
    for s in strings:
        try: x0,y0,x1,y1 = (int(v) for v in s.split(','))
        except ValueError: die(f'--fill expects x0,y0,x1,y1 — got {s!r}')
        if not (0 <= x0 < x1 <= W and 0 <= y0 < y1 <= H):
            die(f'--fill {s!r} outside canvas {W}x{H} or reversed')
        fills.append((x0,y0,x1,y1))
    return fills

def make_crops(final, panel_old, L, best, ymin, outdir):
    """비전 라운드용 크롭(좌표만 — 이 도구는 이미지를 보지 않는다). 확대는 NEAREST(픽셀 그대로)."""
    os.makedirs(outdir, exist_ok=True)
    img = Image.fromarray(final); old = Image.fromarray(panel_old)
    def save(im, name):
        im.save(os.path.join(outdir, name)); print(f'conform: crop {name} {im.size}')
    save(img.crop((0,1180,720,1800)), 'strip-new.png')
    names = {}
    for k in L['knobs']:
        if k['cy'] >= ymin and k['name'] in ('REV_SIZE','REV_A'): names[k['name']] = k
    sq = []
    for src_from_old, k in (True, best[32][1]), (False, names.get('REV_SIZE')), (True, best[25][1]), (False, names.get('REV_A')):
        if k is None: continue
        base = old if src_from_old else img
        r = k['r']+22
        sq.append((k, base.crop((k['cx']-r, k['cy']-r, k['cx']+r+1, k['cy']+r+1)).resize(((2*r+1)*2,(2*r+1)*2), Image.NEAREST)))
    if len(sq) == 4:
        hh = max(im.size[1] for _,im in sq); ww = sum(im.size[0] for _,im in sq)
        canvas = Image.new('RGB', (ww,hh), tuple(int(v) for v in np.median(final[1700:1800].reshape(-1,3), axis=0)))
        x = 0
        for _,im in sq:
            canvas.paste(im, (x,0)); x += im.size[0]
        save(canvas, 'knobcmp.png')
    save(img.crop((520,1420,700,1510)).resize((180*3, 90*3), Image.NEAREST), 'mixer_row2_right.png')
    save(img.crop((60,1700,700,1800)).resize((640*2, 100*2), Image.NEAREST), 'fx2_bottom.png')
    save(img.crop((40,1380,520,1425)).resize((480*3, 45*3), Image.NEAREST), 'plates_mixer.png')
    save(img.crop((600,1760,720,1800)).resize((120*3, 40*3), Image.NEAREST), 'backing.png')

def main():
    ap = argparse.ArgumentParser(add_help=False)
    ap.add_argument('inputs', nargs='*')
    ap.add_argument('--check', action='store_true')
    ap.add_argument('--ymin', type=int, default=1280)
    ap.add_argument('--fill', action='append', default=[]); ap.add_argument('--keep', action='append', default=[])
    ap.add_argument('--plates', default='mixer'); ap.add_argument('--plate-src', default='drums')
    ap.add_argument('--report'); ap.add_argument('--crops')
    a = ap.parse_args()
    if a.check:
        if len(a.inputs) != 3: die('--check expects <png> <layout-v3.json> <panel.png>')
        png, layout_path, panel_path = a.inputs
        arr = load_rgb(png); L = json.load(open(layout_path)); panel_old = load_rgb(panel_path)
        H, W = arr.shape[:2]
        if list(L['size']) != [W,H]: die(f'layout size {L["size"]} != image {W}x{H}')
        if not (0 < a.ymin < H): die(f'--ymin {a.ymin} out of range for height {H}')
        if panel_old.shape[1] != W or panel_old.shape[0] < a.ymin:
            die(f'panel.png {panel_old.shape[1]}x{panel_old.shape[0]} does not cover {W}x{a.ymin}')
        fills = parse_fills(a.fill or [','.join(map(str,f)) for f in P4_RACKART_B_FILLS], W, H)
        hsv_old = np.asarray(Image.open(panel_path).convert('RGB').convert('HSV'), np.uint8)
        g = measure_gates(arr, L, panel_old, hsv_old, a.ymin, fills, verbose=True, plate_src=a.plate_src, keeps=parse_keeps(a.keep or [",".join(map(str,k)) for k in P4_RACKART_B_KEEPS], W, H))
        sys.exit(0 if g['pass'] else 1)
    if len(a.inputs) != 4: die('usage: conform.py <panel-v3.png> <layout-v3.json> <panel.png> <out.png> [--ymin N] [--fill x0,y0,x1,y1] [--plates mixer] [--report out.json] [--crops DIR]')
    src_path, layout_path, old_path, out_path = a.inputs
    src = load_rgb(src_path); L = json.load(open(layout_path)); panel_old = load_rgb(old_path)
    H, W = src.shape[:2]
    if list(L['size']) != [W,H]: die(f'layout size {L["size"]} != image {W}x{H}')
    if not (0 < a.ymin < H): die(f'--ymin {a.ymin} out of range for height {H}')
    if panel_old.shape[1] != W or panel_old.shape[0] < a.ymin:
        die(f'panel.png {panel_old.shape[1]}x{panel_old.shape[0]} does not cover {W}x{a.ymin}')
    fills = parse_fills(a.fill, W, H)
    keeps = parse_keeps(a.keep or [','.join(map(str,k)) for k in P4_RACKART_B_KEEPS], W, H)
    hsv_old = np.asarray(Image.open(old_path).convert('RGB').convert('HSV'), np.uint8)
    band = np.zeros((H,W), bool); band[a.ymin:,:] = True
    out = src.astype(np.float32)
    report = {'inputs': {'panel': src_path, 'layout': layout_path, 'old': old_path, 'out': out_path,
                         'size': [W,H], 'ymin': a.ymin, 'fills': [list(f) for f in fills],
                         'plates': a.plates}, 'knobs': [], 'overlaps': {}, 'plates_filled': [],
              'marks': {}, 'fills': [], 'gates': {}}

    # ——— 1. 노브 이식(A) — cut-knobs 선택 기준의 원본 노브를 r+18 원판으로
    new_knobs = [k for k in L['knobs'] if k['cy'] >= a.ymin]
    old_knobs = [k for k in L['knobs'] if k['cy'] < a.ymin]
    best = cleanest_by_r(L, hsv_old, a.ymin)
    chosen = {}
    # 기증자 이웃 라벨판 제외 마스크(기존 패널 좌표) — 기증자 자신의 판은 paste_plate가 따로 싣는다.
    excl_all = np.zeros(panel_old.shape[:2], bool)
    for ok in old_knobs:
        excl_all |= rect_mask(panel_old.shape[:2], *knob_plate_rect(ok))
    for k in new_knobs:
        if k['r'] not in chosen: chosen[k['r']] = pick_source(best, k['r'])
        scaled, s = chosen[k['r']]
        excl = excl_all & ~rect_mask(panel_old.shape[:2], *knob_plate_rect(s))
        Rd = keep_r(k, L['knobs'])
        pre_r, pre_c = knob_stats(out, k['cx'], k['cy'], k['r'])
        pl_pre = plate_lum(out, k)
        paste_disc(out, panel_old.astype(np.float32), s, k, scaled, a.ymin, Rd, excl)
        paste_plate(out, panel_old.astype(np.float32), s, k, scaled, a.ymin)
        post_r, post_c = knob_stats(out, k['cx'], k['cy'], k['r'])
        pl_post, pl_src = plate_lum(out, k), plate_lum(panel_old.astype(np.float32), s)
        sr, sc = knob_stats(panel_old.astype(np.float32), s['cx'], s['cy'], s['r'])
        report['knobs'].append({'name': k['name'], 'section': k['section'], 'cx': k['cx'], 'cy': k['cy'], 'r': k['r'],
                                'src': {'name': s['name'], 'section': s['section'], 'cx': s['cx'], 'cy': s['cy'], 'r': s['r'],
                                        'sat': round(float(best[s['r']][0]),1)},
                                'scaled': scaled, 'disc_r': Rd,
                                'ring_pre': round(pre_r,1), 'ring_post': round(post_r,1), 'ring_src': round(sr,1),
                                'cap_pre': round(pre_c,1), 'cap_post': round(post_c,1), 'cap_src': round(sc,1),
                                'plate_pre': round(pl_pre,1), 'plate_post': round(pl_post,1), 'plate_src': round(pl_src,1)})
    # 겹침 자기 리뷰: r+18 원판 간(나중 이식이 앞 것을 덮는다)·원판 vs 라벨판 rect
    shape = (H,W); discs = {k['name']: disc_mask(shape, k['cx'], k['cy'], keep_r(k, L['knobs'])) for k in new_knobs}
    ov_knob, ov_plate = [], []
    for i,ki in enumerate(new_knobs):
        for kj in new_knobs[i+1:]:
            n = int((discs[ki['name']] & discs[kj['name']]).sum())
            if n: ov_knob.append({'a': ki['name'], 'b': kj['name'], 'px': n})
    for k in new_knobs:
        for p in L['plates']:
            x,y,x1,y1 = rect_of(p)
            n = int((discs[k['name']] & rect_mask(shape, x, y, x1, y1)).sum())
            if n: ov_plate.append({'knob': k['name'], 'plate': p.get('for'), 'px': n})
    report['overlaps'] = {'knob_pairs': ov_knob, 'knob_vs_plate': ov_plate,
                          'note': 'layout 순서로 이식 — 나중 원반이 앞 원판의 테두리 11px 를 덮는다(원본끼리 같은 소스라 내용은 동일 부품)'}
    print(f'conform: transplanted {len(new_knobs)} knobs ' +
          ', '.join(f"r{r}<-{s['name']}({s['section']}){'*scaled*' if sc else ''}" for r,(sc,s) in sorted(chosen.items())) +
          f'; disc overlaps: {len(ov_knob)} knob pairs, {len(ov_plate)} knob/plate')

    # ——— 2. 섹션 이름판 이식(A) — 기존 이름판(--plate-src, 기본 drums)의 픽셀을 여백 6px 포함해 복사.
    #     평면 크림은 옆의 손그림 판과 나란히 놓이면 스티커로 튀었다(모서리·그림자·그라데이션 — 비전 2026-09-06).
    targets = [m.strip() for m in a.plates.split(',') if m.strip()]
    srcp = next((p for p in L['plates'] if p.get('for') == a.plate_src and p['rect'][1] < a.ymin), None)
    if srcp is None: die(f'--plate-src {a.plate_src}: no such plate above ymin')
    for p in L['plates']:
        if p.get('for') not in targets: continue
        host = next((q for q in L['panels'] if rect_of(q)[0] <= p['rect'][0] < rect_of(q)[2] and rect_of(q)[1] <= p['rect'][1] < rect_of(q)[3]), None)
        excl_x = (rect_of(host)[0] + round(20*W/768) + 6) if host else None
        n = paste_rect(out, panel_old.astype(np.float32), srcp['rect'], p['rect'], a.ymin, PLATE_MARGIN, excl_x)
        report['plates_filled'].append({'for': p['for'], 'rect': list(p['rect']), 'src': a.plate_src,
                                        'src_rect': list(srcp['rect']), 'margin': PLATE_MARGIN, 'px': n})
    print(f"conform: transplanted {len(report['plates_filled'])} nameplate(s) {targets} <- {a.plate_src} {srcp['rect']} (margin {PLATE_MARGIN})")

    # ——— 3. 미선언 자국 제거(B) — 기준색 거리 > T → 5px 팽창 → 평면 + 3px 페더
    allowed = build_allowed(L, W, H, a.ymin, keeps)
    panels_band = [p for p in L['panels'] if rect_of(p)[3] > a.ymin]
    C, dist, junk, marks, refs, inmod = region_refs(out, allowed, panels_band, band, (H,W))
    per = {'backing': int((marks & ~inmod).sum())}
    for p in panels_band:
        x,y,x1,y1 = rect_of(p); per[p['name']] = int((marks & rect_mask((H,W), x, y, x1, y1)).sum())
    dil = mdilate(marks, DILATE_MARK) & band
    alpha3 = np.maximum(feather_alpha(dil, FILL_FEATHER), junk.astype(np.float32))
    G = grain_field((H,W))
    filled3 = apply_fill(out, dil & (band & ~allowed), alpha3, C, G)
    report['marks'] = {'refs': refs, 'threshold': T_MARK, 'dilate_px': DILATE_MARK,
                       'pre_marks_px': int(marks.sum()), 'pre_marks_per_region': per,
                       'pre_ratio': round(float(marks.sum())/band.sum(), 6), 'filled_px': filled3}
    print(f"conform: marks {int(marks.sum())} px ({float(marks.sum())/band.sum()*100:.2f}% of band) {per} -> filled {filled3} px, refs {refs}")

    # ——— 4. 지정 메움(B) — 허용(노브 원판·rect+2)은 이기고, LED 는 예외로 메운다
    protect = build_protect(L, W, H, a.ymin, keeps)
    for (x0,y0,x1,y1) in fills:
        base = rect_mask((H,W), x0, y0, x1, y1) & band
        region = base & ~protect
        alpha = np.maximum(feather_alpha(base, FILL_FEATHER), junk.astype(np.float32))
        n = apply_fill(out, region, alpha, C, G)
        report['fills'].append({'rect': [x0,y0,x1,y1], 'filled_px': n,
                                'protected_px': int((base & protect).sum())})
        print(f'conform: fill {x0},{y0},{x1},{y1} -> {n} px (protected {int((base & protect).sum())})')

    # ——— 저장: y<ymin 은 입력 바이트 그대로 재결합
    final = np.concatenate([src[:a.ymin], np.rint(np.clip(out[a.ymin:], 0, 255)).astype(np.uint8)])
    assert np.array_equal(final[:a.ymin], src[:a.ymin]), 'top rows changed — refusing to save'
    Image.fromarray(final).save(out_path)
    report['gates'] = measure_gates(final, L, panel_old, hsv_old, a.ymin, fills, verbose=False, plate_src=a.plate_src, keeps=keeps)
    if a.report: json.dump(report, open(a.report,'w'), indent=1); print(f'conform: report {a.report}')
    if a.crops: make_crops(final, panel_old, L, best, a.ymin, a.crops)
    print(f'conform: wrote {out_path} ({W}x{H})')

if __name__ == '__main__':
    main()
