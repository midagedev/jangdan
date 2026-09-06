#!/usr/bin/env python3
"""tools/rack/rear-conform.py — 랙 뒷면(§14.3) 채색 후보의 결정론 정합(앞면 conform.py 의 뒷면 판).

  python3 tools/rack/rear-conform.py <painted.png> <rear.json> <panel.png(앞면 채택)> <out.png> \
      [--scrub-out scrub.png] [--report out.json] [--plate-src drums] [--front-layout path]
  python3 tools/rack/rear-conform.py --check <png> <rear.json> <panel.png> [--report out.json]
  python3 tools/rack/rear-conform.py --selftest <rear.json> <wire.png>

스펙 P5-back-art. scrub.py 는 rear.json 스키마(devices/in/out, knobs 없음)를 모른다 — 실행 즉
KeyError 라서 같은 규칙(보호 마스크 밖 V>150 또는 S>110 → 5×5 팽창 → MedianFilter(19))을 여기서
돌린다(선택: scrub.py 수정 금지 → 정합기 내장). 이후 순서:
  1) 잭 이식 — 칠해진 그림의 잭 25개 중 '가장 깨끗한' 것(선정 기준은 pick_donor 주석)을 골라
     하드 원반 d<=r+8 을 25자리에 전사(히트 영역과 보이는 구멍이 같은 자리가 되게), 바깥 3px 페더.
     하드 원반이 전부 같은 기증자 픽셀이라는 것이 게이트 ② 이고, 페더가 어떤 잭의 하드 영역도
     덮지 않는 것(피치 42 vs r+8=20·페더 23)이 그 증명의 전제다.
  2) 이름판 이식 — 앞면 panel.json 이름판(기본 drums) 픽셀을 conform.paste_rect 로(여백 6·페더 3·
     섹션 띠 열 제외 — 앞면 conform.py 규칙 그대로). 게이트 ④.
  3) 자국 메움 — conform.py B 규칙(모듈 면/백킹 기준색 거리 > 22 → 5px 팽창 → 기준색+그레인+페더).
     게이트 ⑤.
나사(행 4개)·통풍 슬릿은 layout 에 없지만 rear.py 가 그리는 요소다(§12.7 사건: layout 에 없다고
지웠다가 라벨판 18개를 잃었다) — 좌표를 손으로 베끼지 않고 rear.py 상수·식에서 유도해 보호한다.
  4) 나사 이식 — 스펙은 나사 '보호'만 명시했지만 게이트 ⑥(전 행 4/4 밝음)과 칠해진 실측이
     양립하지 않는다(seed 1234: bassB·main·poly 나사가 판면보다 13~26 어두움 — diffusion 이 지운
     것). 잭 이식과 같은 규칙, 같은 그림 안 부품 재사용으로 닫는다: 밝은 나사의 코어(d<=5)를
     32자리에 이식(페더 5..7). 왼쪽 나사 두 개는 섹션 띠 위에 있어 오른쪽(판면 위)과 문맥이 다르다 —
     기증자를 좌·우 클래스별로 고른다(코어는 저채도 금속색이라 띠색과 무관하다).
--selftest 는 유도 결과를 커밋된 와이어프레임 rear.png 픽셀과 대조한다: rear.py 규칙이 바뀌고 이
파일이 따라가지 않으면 실패한다(코드·산출물 발산 검출).
"""
import sys, os, json, argparse
import numpy as np
from PIL import Image, ImageFilter, ImageChops

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import conform  # 앞면 정합기 — 마스크·메움 프리미티브를 재사용한다(복사 방지)

T_MARK = conform.T_MARK            # 22, 자국 판정 RGB 거리 임계(앞면과 같은 값)
DILATE_MARK = conform.DILATE_MARK  # 5
FILL_FEATHER = conform.FILL_FEATHER  # 5
RECT_PAD = conform.RECT_PAD        # 2
BORDER_BAND = conform.BORDER_BAND  # 8
PLATE_MARGIN = conform.PLATE_MARGIN  # 6, 이름판 이식 여백

# ——— rear.py 에서 유도한 상수(아래 식·주석의 줄번호가 원본이다). 규칙이 바뀌면 여기도 바뀐다.
JEDGE, JCOL, JPERCOL, TOPINSET = 54, 46, 4, 42  # rear.py:20-25 잭 배치 상수
SCREW_INSET, SCREW_R = 14, 6                    # rear.py:73-76 모서리 안쪽 14px, 타원 r6
VENT_LPAD, VENT_H, VENT_STEP = 40, 6, 16        # rear.py:80-86 슬릿 여백·높이·피치
STRIPE_W = 20                                   # rear.py:68 섹션 색 띠 폭(앞면 19 과 다르다 — rear.py 는 정수 20)

JR_HARD = 8        # 잭 하드 원반 = r+8(게이트 ② 의 반지름)
JR_FEATHER = 3     # 하드 밖 페더(피치 42 > 2*(r+8)=40 — 하드끼리 안 겹친다)
SCREW_KEEP = 10    # 나사 보호 반지름(그림 r6+외곽선 2 + 여유)
VENT_PAD = 3       # 슬릿 rect 보호 여유
PLATE_KEEP = 9     # 이름판 보호 = 이식 여백 6 + 페더 3
JACK_DARK = 30     # 게이트 ③ : 중심이 링 중앙보다 이만큼 어두워야 한다
JACK_DARK_SEL = 35 # 기증자 선정 하한(게이트 30 보다 5 여유)
SCREW_BRIGHT = 3   # 게이트 ⑥ : 나사가 판면보다 이만큼 밝아야 한다
SCREW_SEL = 20     # 나사 기증자 선정: 판면보다 이만큼 밝아야 한다(게이트 3 의 여유)
SCREW_CORE, SCREW_RIM = 5, 7  # 나사 이식 코어(d<=5 하드)·가장자리(5..7 페더)
VAL_HI, SAT_HI = 150, 110  # scrub.py 와 같은 스크럽 기준
VAL_FLOOR = 60    # '채도 높은 얼룩'의 휘도 하한 — HSV 채도는 어두운 픽셀에서 발산한다(실측:
                  # 칠해진 잭 구멍 안 V<=10 픽셀의 S 170~180 — 채도 기준으로 세면 구멍 노이즈가
                  # 다 걸린다). 보이는 색 얼룩만 잡는다.
G5_MAX = 0.003     # 게이트 ⑤ 자국 잔류 비율(앞면 게이트 ④ 와 같은 값)
G7_MAX = 0.01      # 게이트 ⑦ 백킹 띠 채도 높은 픽셀 비율 상한

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, '..', '..'))
FRONT_LAYOUT_DEFAULT = os.path.join(ROOT, 'app', 'assets', 'device', 'layout.json')

# 뒷면 행 → 앞면 모듈(보고용 면 휘도 비교). reverb·chorus 는 fx2 의 위·아래 절반(rear.py half()).
FRONT_HALF = {'bassA': ('basslineA', None), 'bassB': ('basslineB', None), 'drums': ('drums', None),
              'fx': ('fx', None), 'main': ('mixer', None), 'reverb': ('fx2', 'top'),
              'chorus': ('fx2', 'bottom'), 'poly': ('poly', None)}

def die(msg):
    sys.exit('rear-conform: ' + msg)

def load_rgb(path):
    return np.asarray(Image.open(path).convert('RGB'), dtype=np.uint8)

def hsv_of(arr):
    return np.asarray(Image.fromarray(arr).convert('HSV'), np.uint8)

def rect_of(r):
    x, y, w, h = r
    return x, y, x + w, y + h

def rect_mask(shape, x0, y0, x1e, y1e):
    m = np.zeros(shape, bool)
    x0, y0 = max(0, x0), max(0, y0); x1e, y1e = min(shape[1], x1e), min(shape[0], y1e)
    if x1e > x0 and y1e > y0: m[y0:y1e, x0:x1e] = True
    return m

def disc_mask(shape, cx, cy, R):
    m = np.zeros(shape, bool)
    x0, y0, x1, y1 = max(0, int(cx - R)), max(0, int(cy - R)), min(shape[1], int(cx + R) + 1), min(shape[0], int(cy + R) + 1)
    if x1 <= x0 or y1 <= y0: return m
    yy, xx = np.mgrid[y0:y1, x0:x1]
    m[y0:y1, x0:x1] = (yy - cy) ** 2 + (xx - cx) ** 2 <= R * R
    return m

def derive(devices):
    """rear.py 의 나사·통풍 슬릿 규칙을 rear.json 장치 rect·포트 수로 계산한다(좌표 하드코딩 금지).
    식은 rear.py:73-86 과 문자 하나 같다 — 스크립트를 import 하면 즉시 실행되므로 규칙만 옮기고
    --selftest 로 원본 와이어프레임 픽셀과의 일치를 검증한다."""
    out = []
    for d in devices:
        x, y, w, h = d['rect']
        ins, outs = len(d['in']), len(d['out'])
        screws = [(sx, sy) for sx in (x + SCREW_INSET, x + w - SCREW_INSET)
                  for sy in (y + SCREW_INSET, y + h - SCREW_INSET)]
        lx = x + JEDGE + VENT_LPAD + (JCOL * ((ins - 1) // JPERCOL) if ins else -VENT_LPAD)
        rx = x + w - JEDGE - VENT_LPAD - (JCOL * ((outs - 1) // JPERCOL) if outs else -VENT_LPAD)
        vents = []
        if rx - lx > 120:
            vy = y + TOPINSET + 10
            while vy < y + h - 24:
                vents.append((lx, vy, rx, vy + VENT_H))
                vy += VENT_STEP
        out.append({'name': d['name'], 'rect': (x, y, x + w, y + h), 'screws': screws, 'vents': vents})
    return out

def jacks_of(L):
    js = []
    for d in L['devices']:
        for side in ('in', 'out'):
            for j in d[side]:
                js.append(dict(j, device=d['name'], side=side))
    return js

def build_masks(L, geom, W, H):
    """허용(자국 판정 제외)·스크럽·메움 보호를 겸하는 마스크. 뒷면에는 앱이 그리는 LED 가 없어
    앞면처럼 allowed/protect 를 나눌 필요가 없다(앞면 conform.build_allowed/build_protect 참조)."""
    shape = (H, W)
    A = np.zeros(shape, bool)
    for j in jacks_of(L):
        A |= disc_mask(shape, j['cx'], j['cy'], j['r'] + JR_HARD + JR_FEATHER)
    for d in L['devices']:
        x, y, w, h = d['plate']
        A |= rect_mask(shape, x - PLATE_KEEP, y - PLATE_KEEP, x + w + PLATE_KEEP, y + h + PLATE_KEEP)
    for g in geom:
        x0, y0, x1, y1 = g['rect']
        for (sx, sy) in g['screws']:
            A |= disc_mask(shape, sx, sy, SCREW_KEEP)
        for (vx0, vy0, vx1, vy1) in g['vents']:
            A |= rect_mask(shape, vx0 - VENT_PAD, vy0 - VENT_PAD, vx1 + VENT_PAD, vy1 + VENT_PAD)
        A |= rect_mask(shape, x0 - 2, y0, x0 + STRIPE_W + 2, y1)      # 섹션 색 띠(rear.py:68)
        outer = rect_mask(shape, x0 - BORDER_BAND, y0 - BORDER_BAND, x1 + BORDER_BAND, y1 + BORDER_BAND)
        inner = rect_mask(shape, x0 + BORDER_BAND, y0 + BORDER_BAND, x1 - BORDER_BAND, y1 - BORDER_BAND)
        A |= outer & ~inner                                           # 모듈 테두리 띠(안팎 8px)
    return A

def scrub(arr, keep):
    """scrub.py 의 뒷면 판 — 보호 밖의 밝은/채도 높은 자국(가짜 글자)을 주변 중앙값으로 민다.
    노브 면 채도 규칙(scrub.py:39-42)은 뒷면에 노브가 없어 해당 없다."""
    img = Image.fromarray(arr)
    hsv = img.convert('HSV'); S = hsv.getchannel('S'); V = hsv.getchannel('V')
    bad = ImageChops.lighter(V.point(lambda v: 255 if v > VAL_HI else 0),
                             S.point(lambda v: 255 if v > SAT_HI else 0))
    bad = ImageChops.subtract(bad, Image.fromarray((keep * 255).astype(np.uint8)))
    bad = bad.filter(ImageFilter.MaxFilter(5))
    fill = img.filter(ImageFilter.MedianFilter(19))
    out = np.asarray(Image.composite(fill, img, bad), np.uint8)
    return out, int((np.asarray(bad) > 0).sum())

def jack_contrast(Ll, shape, j):
    """게이트 ③ 축: 링(r+4..r+8 고리) 중앙 휘도 − 중심(d<=6) 중앙 휘도."""
    core = disc_mask(shape, j['cx'], j['cy'], 6)
    ann = disc_mask(shape, j['cx'], j['cy'], j['r'] + JR_HARD) & ~disc_mask(shape, j['cx'], j['cy'], j['r'] + 4)
    return float(np.median(Ll[ann]) - np.median(Ll[core]))

def jack_metrics(Ll, hsv, L, geom):
    """기증자 선정 지표(전 수치가 --report 에 남는다). 자격(수치):
       contrast >= 35 — 링(r+4..r+8) 중앙 휘도 − 중심(d<=6) 중앙 휘도. 이식 후 게이트 ③(>=30)의 여유.
       offset   <= 4px — 링 밝기 무게중심과 잭 중심의 거리. 손그림 링은 흔들려도(좌우 반전 |Δ|는
                         20~48 나온다 — 실측 seed 1234) 소켓은 중심에 있어야 한다. 반전차는 흔들림에
                         지배되어 '한쪽으로 찢어진 링'과 구분이 안 된다.
       sat_foreign == 0 — 원판(r+8) 안 채도 S>110 픽셀 0개. 가짜 글자·낙서는 채도 색이다(§12.7
                         붉은 글자 사건). 밝은 금속 하이라이트(V>150, 저채도)는 소켓의 정상 부분이라
                         밝기로는 세지 않는다(실측: 정상 잭 원판에 V>150 픽셀 30~618개).
       clash    거짓 — 원반(r+11) 이 통풍 슬릿·나사와 겹치지 않는다(이물을 25자리에 옮기지 않는다).
       점수 = contrast − 3·offset(대비는 크게, 비대칭은 벌점)."""
    S = hsv[..., 1]
    shape = Ll.shape
    vents = [v for g in geom for v in g['vents']]
    screws = [s for g in geom for s in g['screws']]
    out = []
    for j in jacks_of(L):
        cx, cy, r = j['cx'], j['cy'], j['r']
        rr = r + 8
        if cx - rr < 0 or cy - rr < 0 or cx + rr >= shape[1] or cy + rr >= shape[0]:
            die(f'jack {j["device"]}/{j["name"]} too close to canvas edge')
        contrast = jack_contrast(Ll, shape, j)
        core = float(np.median(Ll[disc_mask(shape, cx, cy, 6)]))
        box = Ll[cy - rr:cy + rr + 1, cx - rr:cx + rr + 1]
        yy, xx = np.mgrid[0:box.shape[0], 0:box.shape[1]]
        pd = np.sqrt((yy - rr) ** 2 + (xx - rr) ** 2)
        w = np.clip(box - (core + 10), 0, None) * (pd <= r + 6)   # 링 이웃의 밝기 초과분
        if w.sum() > 0:
            gy = float((yy * w).sum() / w.sum()); gx = float((xx * w).sum() / w.sum())
            offset = round(((gy - rr) ** 2 + (gx - rr) ** 2) ** 0.5, 1)
        else:
            offset = 999.0
        sat_foreign = int(((S > SAT_HI) & (hsv[..., 2] > VAL_FLOOR))[cy - rr:cy + rr + 1, cx - rr:cx + rr + 1][pd <= r + JR_HARD].sum())
        d11 = disc_mask(shape, cx, cy, r + JR_HARD + JR_FEATHER)
        clash = any((d11 & rect_mask(shape, vx0, vy0, vx1, vy1)).any() for (vx0, vy0, vx1, vy1) in vents) or \
                any((sx - cx) ** 2 + (sy - cy) ** 2 < (r + JR_HARD + JR_FEATHER + SCREW_R + 2) ** 2 for (sx, sy) in screws)
        out.append({'device': j['device'], 'name': j['name'], 'side': j['side'], 'port': j['port'],
                    'cx': cx, 'cy': cy, 'r': r, 'contrast': round(contrast, 1), 'offset': offset,
                    'sat_foreign': sat_foreign, 'clash': bool(clash),
                    'eligible': bool(contrast >= JACK_DARK_SEL and offset <= 4 and sat_foreign == 0 and not clash),
                    'score': round(contrast - 3 * offset, 1)})
    return out

def screw_metrics(Ll, hsv, geom, allowed):
    """나사 기증자 선정 지표(수치, --report 에 남는다). 자격:
       minus_face >= 20 — 나사 원반(d<=5) 평균 휘도 − 그 행 판면 중앙. 게이트 ⑥(>=3)의 여유.
       offset      <= 3px — d<=7 안 밝기 무게중심과 나사 중심의 거리(나사가 제 자리에 있다).
       sat_foreign == 0 — d<=7 안 채도 얼룩(S>110 & V>60) 없음.
       점수 = minus_face − 3·offset. 좌·우 클래스(왼쪽 나사는 띠 위, 오른쪽은 판면 위 — rear.py 가
       나사를 띠·이름판과 겹치는 모서리에 그린다)별로 따로 고른다."""
    S = hsv[..., 1]; V = hsv[..., 2]
    shape = Ll.shape
    faces = {}
    for g in geom:
        x0, y0, x1, y1 = g['rect']
        faces[g['name']] = float(np.median(Ll[(~allowed) & rect_mask(shape, x0, y0, x1, y1)]))
    out = []
    for g in geom:
        for k, (sx, sy) in enumerate(g['screws']):
            R = SCREW_RIM
            box = Ll[sy - R:sy + R + 1, sx - R:sx + R + 1]
            yy, xx = np.mgrid[0:box.shape[0], 0:box.shape[1]]
            pd = np.sqrt((yy - R) ** 2 + (xx - R) ** 2)
            d5 = disc_mask(shape, sx, sy, SCREW_CORE)
            bright = float(Ll[d5].mean())
            w = np.clip(box - (faces[g['name']] + 8), 0, None) * (pd <= R)
            if w.sum() > 0:
                gy = float((yy * w).sum() / w.sum()) - R; gx = float((xx * w).sum() / w.sum()) - R
                off = round((gy * gy + gx * gx) ** 0.5, 1)
            else:
                off = 999.0
            sf = int(((S > SAT_HI) & (V > VAL_FLOOR))[sy - R:sy + R + 1, sx - R:sx + R + 1][pd <= R].sum())
            out.append({'row': g['name'], 'pos': k, 'side': 'left' if k in (0, 1) else 'right',
                        'cx': sx, 'cy': sy, 'bright': round(bright, 1),
                        'minus_face': round(bright - faces[g['name']], 1), 'offset': off, 'sat_foreign': sf,
                        'eligible': bool(bright - faces[g['name']] >= SCREW_SEL and off <= 3 and sf == 0),
                        'score': round(bright - faces[g['name']] - 3 * off, 1)})
    return out

def pick_screw_donors(metrics):
    """좌·우 클래스별 기증자(없으면 die — 보호만으로 게이트 ⑥ 을 못 채우는 그림이다)."""
    chosen = {}
    for side in ('left', 'right'):
        ok = [e for e in metrics if e['side'] == side and e['eligible']]
        if not ok:
            head = '; '.join(f'{e["row"]}/p{e["pos"]} b{e["minus_face"]} o{e["offset"]} sf{e["sat_foreign"]}' for e in metrics if e['side'] == side)
            die(f'no clean {side} screw donor (need minus_face>={SCREW_SEL}, offset<=3, sat_foreign=0):\n  {head}')
        chosen[side] = max(ok, key=lambda e: e['score'])
    return chosen

def transplant_screws(out, src_u8, donors, geom):
    """코어(d<=5) 하드 이식 + 가장자리(5..7) 페더, 좌·우 클래스 기증자. 32자리.
    나사 위치 순서는 rear.py:74-75 루프와 같다: 0=(x+14,y+14) 1=(x+14,y+h-14) 2=(x+w-14,y+14) 3=(x+w-14,y+h-14).
    게이트 ⑥ 이 재는 d<=5 는 전부 기증자 픽셀이다."""
    patches = {}
    for side, e in donors.items():
        R = SCREW_RIM; sx, sy = e['cx'], e['cy']
        patch = src_u8[sy - R:sy + R + 1, sx - R:sx + R + 1].astype(np.float32)
        n = 2 * R + 1
        yy, xx = np.mgrid[0:n, 0:n]
        pd = np.sqrt((yy - R) ** 2 + (xx - R) ** 2)
        alpha = np.clip((R - pd) / (R - SCREW_CORE), 0, 1)
        patches[side] = (patch, alpha, R)
    total = 0
    for g in geom:
        for k, (sx, sy) in enumerate(g['screws']):
            patch, alpha, R = patches['left' if k in (0, 1) else 'right']
            sub = out[sy - R:sy + R + 1, sx - R:sx + R + 1]
            m = alpha > 0
            a = alpha[m][:, None]
            sub[m] = patch[m] * a + sub[m] * (1.0 - a)
            total += int(m.sum())
    return total

def pick_donor(metrics):
    ok = [e for e in metrics if e['eligible']]
    if not ok:
        head = '; '.join(f'{e["device"]}/{e["name"]} c{e["contrast"]} o{e["offset"]} sf{e["sat_foreign"]} clash{e["clash"]}' for e in metrics)
        die(f'no clean jack donor (need contrast>={JACK_DARK_SEL}, offset<=4, sat_foreign=0, no clash):\n  {head}')
    return max(ok, key=lambda e: e['score'])

def transplant_jacks(out, src_u8, donor, jacks):
    """하드 원반(d<=r+8) 25자리 전사 뒤 페더(r+8..r+11) — 페더는 모든 잭의 하드 영역을 피해
    블렌드한다(세로 피치 42에서 이웃 페더가 하드에 닿는다 — 하드가 먼저고 페더가 피하면
    25개 하드 원반이 전부 기증자 픽셀로 동일하다, 게이트 ②)."""
    R = donor['r'] + JR_HARD + JR_FEATHER
    sx, sy = donor['cx'], donor['cy']
    patch = src_u8[sy - R:sy + R + 1, sx - R:sx + R + 1].astype(np.float32)
    n = 2 * R + 1
    yy, xx = np.mgrid[0:n, 0:n]
    pd = np.sqrt((yy - R) ** 2 + (xx - R) ** 2)
    hard_a = pd <= donor['r'] + JR_HARD
    feather_a = np.clip((R - pd) / JR_FEATHER, 0, 1) * (pd > donor['r'] + JR_HARD)
    shape = out.shape[:2]
    hards = np.zeros(shape, bool)
    for j in jacks:
        hards |= disc_mask(shape, j['cx'], j['cy'], j['r'] + JR_HARD)
    for j in jacks:  # 1) 하드 전사
        x0, y0 = j['cx'] - R, j['cy'] - R
        out[y0:y0 + n, x0:x0 + n][hard_a] = patch[hard_a]
    for j in jacks:  # 2) 페더(다른 잭의 하드는 덮지 않는다)
        x0, y0 = j['cx'] - R, j['cy'] - R
        sub = out[y0:y0 + n, x0:x0 + n]
        m = (feather_a > 0) & ~hards[y0:y0 + n, x0:x0 + n]
        a = feather_a[m][:, None]
        sub[m] = patch[m] * a + sub[m] * (1.0 - a)

def paste_plates(out, panel_old_f, front_plates, L, plate_src):
    """이름판 8개 이식 — conform.paste_rect(여백 6·페더 3·섹션 띠 열 제외) 재사용. 앞면 판(128×23)을
    뒷면 판(136×24)에 Lanczos 로 맞춘다(앞면 conform.py 가 다른 크기 기증자를 다루던 방식 그대로)."""
    srcp = next((p for p in front_plates if p.get('for') == plate_src), None)
    if srcp is None:
        die(f'--plate-src {plate_src}: no such plate in front layout (known: {[p.get("for") for p in front_plates]})')
    total = 0
    for d in L['devices']:
        x, y, w, h = d['plate']
        excl = d['rect'][0] + STRIPE_W + 6  # rear.py 띠 20px — 기증자 앞면 띠가 딸려오지 않게 하는 열
        total += conform.paste_rect(out, panel_old_f, srcp['rect'], [x, y, w, h], 0, PLATE_MARGIN, excl)
    return srcp, total

def refs_and_dist(out, allowed, rects, shape):
    """모듈 면/백킹 기준색(허용 밖 중앙값)과 거리장(conform.region_refs 의 ymin 없는 판)."""
    nonal = ~allowed
    inmod = np.zeros(shape, bool)
    for (x0, y0, x1, y1) in rects:
        inmod |= rect_mask(shape, x0, y0, x1, y1)
    px = out[nonal & ~inmod]
    if len(px) == 0: die('no backing pixels — check rear.json rects')
    back_ref = np.median(px, axis=0)
    C = np.zeros(shape + (3,), np.float32); C[:] = back_ref
    refs = {'backing': [round(float(v), 1) for v in back_ref]}
    for (x0, y0, x1, y1) in rects:
        m = nonal & rect_mask(shape, x0, y0, x1, y1)
        if not m.any(): die(f'module rect {(x0, y0)} has no unprotected face pixels')
        ref = np.median(out[m], axis=0)
        refs[f'{x0},{y0}'] = [round(float(v), 1) for v in ref]
        C[rect_mask(shape, x0, y0, x1, y1)] = ref
    dist = np.sqrt(((out - C) ** 2).sum(axis=2))
    return C, dist, refs, inmod

def front_face_allowed(Lf, W, H):
    """앞면 '판면' 마스크(노브 r+18·rect+2·LED r+10·띠 19·테두리 8 제외 — scrub.py keep 과 같은 요소).
    앞뒤 면 휘도 비교(보고 전용)에만 쓴다."""
    shape = (H, W)
    A = np.zeros(shape, bool)
    for k in Lf['knobs']:
        A |= disc_mask(shape, k['cx'], k['cy'], k['r'] + 18)
    for key in ('buttons', 'pads', 'plates', 'displays'):
        for b in Lf.get(key, []):
            x, y, x1, y1 = rect_of(b['rect'])
            A |= rect_mask(shape, x - RECT_PAD, y - RECT_PAD, x1 + RECT_PAD, y1 + RECT_PAD)
    for l in Lf.get('leds', []):
        A |= disc_mask(shape, l['cx'], l['cy'], l['r'] + 10)
    sec_w = round(20 * W / 768)
    for p in Lf['panels']:
        x, y, x1, y1 = rect_of(p['rect'])
        A |= rect_mask(shape, x, y, x + sec_w + 1, y1)
        outer = rect_mask(shape, x - BORDER_BAND, y - BORDER_BAND, x1 + BORDER_BAND, y1 + BORDER_BAND)
        inner = rect_mask(shape, x + BORDER_BAND, y + BORDER_BAND, x1 - BORDER_BAND, y1 - BORDER_BAND)
        A |= outer & ~inner
    if 'scope' in Lf:
        x, y, x1, y1 = rect_of(Lf['scope']['rect']); A |= rect_mask(shape, x - 8, y - 8, x1 + 8, y1 + 8)
    if 'display' in Lf:
        x, y, x1, y1 = rect_of(Lf['display']['rect']); A |= rect_mask(shape, x - 4, y - 4, x1 + 4, y1 + 4)
    return A

def front_face_lum(Ll_f, Lf):
    """앞면 모듈별 면 휘도 중앙값(reverb·chorus 는 fx2 반씪 — rear.py half() 규칙)."""
    H, W = Ll_f.shape
    A = front_face_allowed(Lf, W, H)
    out = {}
    for p in Lf['panels']:
        x, y, x1, y1 = rect_of(p['rect'])
        m = (~A) & rect_mask((H, W), x, y, x1, y1)
        out[p['name']] = float(np.median(Ll_f[m]))
    fx2 = next(p for p in Lf['panels'] if p['name'] == 'fx2')
    x, y, x1, y1 = rect_of(fx2['rect']); hh = (y1 - y - 4) // 2
    top = (~A) & rect_mask((H, W), x, y, x1, y + hh); bot = (~A) & rect_mask((H, W), x, y + hh + 4, x1, y1)
    out['fx2/top'] = float(np.median(Ll_f[top])); out['fx2/bottom'] = float(np.median(Ll_f[bot]))
    return out

def measure(arr, L, geom, panel_old, front_L, front_plates, plate_src, verbose=False):
    """게이트 ①—⑦ 측정(--check 본체, conform 실행 시 report 에도 들어간다)."""
    H, W = arr.shape[:2]
    shape = (H, W)
    allowed = build_masks(L, geom, W, H)
    Ll = conform.lum(arr.astype(np.float32))
    hsv = hsv_of(arr)
    jacks = jacks_of(L)
    g = {}

    # ① 크기
    g['g1_size'] = {'size': [W, H], 'expect': list(L['size']), 'pass': bool([W, H] == list(L['size']) == [720, 2000])}

    # ② 잭 r+8 원반 픽셀 동일(이식 증거)
    first = None; ident = 0
    for j in jacks:
        px = arr[disc_mask(shape, j['cx'], j['cy'], j['r'] + JR_HARD)]
        if first is None: first = px
        elif np.array_equal(px, first): ident += 1
    g['g2_jack_discs'] = {'identical': ident + 1, 'total': len(jacks), 'pass': bool(ident + 1 == len(jacks))}

    # ③ 잭 중심이 링(r+4..r+8) 중앙보다 30 이상 어둡다
    cons = [round(jack_contrast(Ll, shape, j), 1) for j in jacks]
    bad = [(jacks[i]['device'] + '/' + jacks[i]['name'], cons[i]) for i in range(len(jacks)) if cons[i] < JACK_DARK]
    g['g3_jack_dark'] = {'min': min(cons), 'median': round(float(np.median(cons)), 1), 'need': JACK_DARK,
                         'bad': bad, 'pass': bool(not bad)}

    # ④ 이름판 8개 == 앞면 기증 픽셀(이식을 여기서 그대로 재계산해 비교 — 결정론이라 값이 같아야 한다)
    srcp = next((p for p in front_plates if p.get('for') == plate_src), None)
    if srcp is None: die(f'plate src {plate_src} not found in front layout')
    plates = []
    for d in L['devices']:
        x, y, w, h = d['plate']
        excl = d['rect'][0] + STRIPE_W + 6
        tmp = arr.astype(np.float32).copy()
        conform.paste_rect(tmp, panel_old.astype(np.float32), srcp['rect'], [x, y, w, h], 0, PLATE_MARGIN, excl)
        x0c, x1c = max(x + 3, excl), x + w - 3
        reg = (slice(y + 3, y + h - 3), slice(x0c, x1c))
        mad = float(np.abs(tmp[reg] - arr.astype(np.float32)[reg]).max())
        plates.append({'for': d['name'], 'rect': list(d['plate']), 'max_abs_diff': round(mad, 1), 'pass': bool(mad <= 6)})
    g['g4_plates'] = {'items': plates, 'pass': bool(all(p['pass'] for p in plates))}

    # ⑤ 자국 잔류(앞면 게이트 ④ 와 같은 정의: 허용 밖 기준색 거리 > 22 비율 <= 0.3%)
    rects = [gm['rect'] for gm in geom]
    C, dist, refs, inmod = refs_and_dist(arr.astype(np.float32), allowed, rects, shape)
    marks = (~allowed) & (dist > T_MARK)
    ratio = float(marks.sum()) / (W * H)
    per = {}
    for gm in geom:
        x0, y0, x1, y1 = gm['rect']
        per[gm['name']] = int((marks & rect_mask(shape, x0, y0, x1, y1)).sum())
    per['backing'] = int((marks & ~inmod).sum())
    g['g5_marks'] = {'residue_px': int(marks.sum()), 'ratio': round(ratio, 6), 'max': G5_MAX, 'refs': refs,
                     'per_region': per, 'pass': bool(ratio <= G5_MAX)}

    # ⑥ 나사 4/4 — 나사 원반(d<=5) 평균 휘도가 그 행 판면(허용 밖 중앙)보다 밝다
    rows = []
    for gm in geom:
        x0, y0, x1, y1 = gm['rect']
        face = float(np.median(Ll[(~allowed) & rect_mask(shape, x0, y0, x1, y1)]))
        diffs = [round(float(Ll[disc_mask(shape, sx, sy, 5)].mean()) - face, 1) for (sx, sy) in gm['screws']]
        rows.append({'name': gm['name'], 'face_lum': round(face, 1), 'screw_minus_face': diffs,
                     'pass': bool(all(dv >= SCREW_BRIGHT for dv in diffs))})
    g['g6_screws'] = {'rows': rows, 'pass': bool(all(r['pass'] for r in rows))}

    # ⑦ 행 사이 백킹 띠 — 채도 높은(S>110 & V>60, 어두운 픽셀의 S 발산 제외) 픽셀 비율 <= 1%
    Sat = (hsv[..., 1] > SAT_HI) & (hsv[..., 2] > VAL_FLOOR)
    bands = []
    ys = sorted((gm['rect'][1], gm['rect'][3]) for gm in geom)
    spans = []
    prev = 0
    for (y0, y1) in ys:
        if y0 > prev: spans.append((prev, y0))
        prev = y1
    if H > prev: spans.append((prev, H))
    for (a, b) in spans:
        n = (b - a) * W
        if n == 0: continue
        frac = float(Sat[a:b].sum()) / n
        bands.append({'rows': [a, b], 'hi_sat_ratio': round(frac, 5), 'pass': bool(frac <= G7_MAX)})
    g['g7_backing'] = {'bands': bands, 'pass': bool(all(b['pass'] for b in bands))}

    # ——— 보고 전용 수치: 행 면 휘도 vs 앞면 같은 모듈(목표 |차| <= 15), 섹션 띠 RGB 중앙값
    extra = {'face_lum': [], 'stripes': []}
    flum = front_face_lum(conform.lum(panel_old.astype(np.float32)), front_L)
    for gm in geom:
        x0, y0, x1, y1 = gm['rect']
        rear_face = float(np.median(Ll[(~allowed) & rect_mask(shape, x0, y0, x1, y1)]))
        fname, half = FRONT_HALF[gm['name']]
        fkey = fname if half is None else ('fx2/top' if half == 'top' else 'fx2/bottom')
        extra['face_lum'].append({'name': gm['name'], 'rear': round(rear_face, 1), 'front': round(flum[fkey], 1),
                                  'front_of': fkey, 'diff': round(rear_face - flum[fkey], 1)})
    for d in L['devices']:
        x, y, w, h = d['rect']
        med = np.median(arr[y:y + h, x:x + STRIPE_W].reshape(-1, 3), axis=0)
        extra['stripes'].append({'name': d['name'], 'rgb_median': [round(float(v), 1) for v in med]})
    g['extra'] = extra

    g['pass'] = bool(g['g1_size']['pass'] and g['g2_jack_discs']['pass'] and g['g3_jack_dark']['pass']
                     and g['g4_plates']['pass'] and g['g5_marks']['pass'] and g['g6_screws']['pass']
                     and g['g7_backing']['pass'])
    if verbose:
        print(f'rear-conform: check gates on {W}x{H}, jacks {len(jacks)}, rows {len(geom)}')
        print(f"  g1  size {W}x{H} == 720x2000 : {'PASS' if g['g1_size']['pass'] else 'FAIL'}")
        gj = g['g2_jack_discs']
        print(f"  g2  jack r+8 discs identical {gj['identical']}/{gj['total']} : {'PASS' if gj['pass'] else 'FAIL'}")
        g3 = g['g3_jack_dark']
        print(f"  g3  jack center-vs-ring min {g3['min']} (need >= {g3['need']}), median {g3['median']} "
              f": {'PASS' if g3['pass'] else 'FAIL ' + str(g3['bad'][:6])}")
        for p in g['g4_plates']['items']:
            print(f"  g4  plate {p['for']:8s} max|diff| {p['max_abs_diff']} (<= 6) : {'PASS' if p['pass'] else 'FAIL'}")
        g5 = g['g5_marks']
        print(f"  g5  residue {g5['residue_px']} px = {g5['ratio']*100:.3f}% (<= {G5_MAX*100:.1f}%) per {g5['per_region']} : {'PASS' if g5['pass'] else 'FAIL'}")
        for r in g['g6_screws']['rows']:
            print(f"  g6  screws {r['name']:8s} face {r['face_lum']} diffs {r['screw_minus_face']} (need >= {SCREW_BRIGHT}, 4/4) "
                  f": {'PASS' if r['pass'] else 'FAIL'}")
        for b in g['g7_backing']['bands']:
            print(f"  g7  backing rows {b['rows']} hi-sat {b['hi_sat_ratio']*100:.3f}% (<= {G7_MAX*100:.0f}%) : {'PASS' if b['pass'] else 'FAIL'}")
        for e in extra['face_lum']:
            flag = '' if abs(e['diff']) <= 15 else '  (report-only: |diff| > 15)'
            print(f"  ..  face lum {e['name']:8s} rear {e['rear']} vs front {e['front']} ({e['front_of']}) diff {e['diff']:+.1f}{flag}")
        for s in extra['stripes']:
            print(f"  ..  stripe {s['name']:8s} rgb {s['rgb_median']}")
        print(f'rear-conform: gates {"PASS" if g["pass"] else "FAIL"}')
    return g

def selftest(L, wire_path):
    """유도한 나사·슬릿·잭 좌표를 커밋된 와이어프레임 rear.png 픽셀과 대조(발산 검출기).
    rear.py 가 바뀌고 이 파일의 유도가 어긋나면 여기서 FAIL 한다."""
    w = load_rgb(wire_path); wl = conform.lum(w.astype(np.float32))
    shape = wl.shape
    if list(L['size']) != [shape[1], shape[0]]:
        die(f'selftest: wire {shape[1]}x{shape[0]} != layout {L["size"]}')
    geom = derive(L['devices'])
    nfails = 0
    for gm in geom:
        vstat = []
        for (vx0, vy0, vx1, vy1) in gm['vents']:
            m = rect_mask(shape, vx0 + 2, vy0 + 1, vx1 - 2, vy1 - 1)
            vstat.append(round(float(wl[m].mean()), 1))
        if any(v >= 45 for v in vstat): nfails += 1
        sstat = [round(float(wl[disc_mask(shape, sx, sy, SCREW_R)].mean()), 1) for (sx, sy) in gm['screws']]
        if any(s <= 56 for s in sstat): nfails += 1
        jstat = []
        for j in jacks_of(L):
            if j['device'] != gm['name']: continue
            # rear.py 잭 기형: 구멍(r-4..r, (16,16,18)) · 칼라(r..r+3, (126,126,132)) ·
            # 어두운 외곽선(r+4.5..r+6) — 창을 이렇게 잡아야 판정이 그리는 것과 마주친다.
            hole = disc_mask(shape, j['cx'], j['cy'], j['r']) & ~disc_mask(shape, j['cx'], j['cy'], j['r'] - 4)
            collar = disc_mask(shape, j['cx'], j['cy'], j['r'] + 3) & ~disc_mask(shape, j['cx'], j['cy'], j['r'])
            jstat.append((round(float(np.median(wl[hole])), 1), round(float(np.median(wl[collar])), 1)))
        if any(h >= 30 or c <= 80 for (h, c) in jstat): nfails += 1
        print(f'selftest: {gm["name"]:8s} vents {len(gm["vents"])} lum {vstat[:3]}{"..." if len(vstat) > 3 else ""} '
              f'screws {sstat} jacks(hole/ring) {jstat[:2]}{"..." if len(jstat) > 2 else ""}')
    if nfails: die(f'selftest: {nfails} row(s) diverge from wireframe — derive() no longer matches rear.py rules')
    print(f'selftest: derive() matches {wire_path} (screws 4/row, vents as listed, jacks on layout coords)')

def main():
    ap = argparse.ArgumentParser(add_help=False)
    ap.add_argument('inputs', nargs='*')
    ap.add_argument('--check', action='store_true')
    ap.add_argument('--selftest', action='store_true')
    ap.add_argument('--scrub-out')
    ap.add_argument('--report')
    ap.add_argument('--plate-src', default='drums')
    ap.add_argument('--front-layout', default=FRONT_LAYOUT_DEFAULT)
    a = ap.parse_args()
    if a.selftest:
        if len(a.inputs) != 2: die('--selftest expects <rear.json> <wire.png>')
        selftest(json.load(open(a.inputs[0])), a.inputs[1])
        return
    front_L = json.load(open(a.front_layout))
    front_plates = front_L['plates']
    if a.check:
        if len(a.inputs) != 3: die('--check expects <png> <rear.json> <panel.png>')
        png, rear_path, panel_path = a.inputs
        arr = load_rgb(png); L = json.load(open(rear_path)); panel_old = load_rgb(panel_path)
        H, W = arr.shape[:2]
        if list(L['size']) != [W, H]: die(f'layout size {L["size"]} != image {W}x{H}')
        g = measure(arr, L, derive(L['devices']), panel_old, front_L, front_plates, a.plate_src, verbose=True)
        if a.report: json.dump(g, open(a.report, 'w'), indent=1); print(f'rear-conform: report {a.report}')
        sys.exit(0 if g['pass'] else 1)
    if len(a.inputs) != 4:
        die('usage: rear-conform.py <painted.png> <rear.json> <panel.png> <out.png> [--scrub-out s.png] [--report r.json]\n'
            '       rear-conform.py --check <png> <rear.json> <panel.png>\n'
            '       rear-conform.py --selftest <rear.json> <wire.png>')
    painted_path, rear_path, panel_path, out_path = a.inputs
    src = load_rgb(painted_path); L = json.load(open(rear_path)); panel_old = load_rgb(panel_path)
    H, W = src.shape[:2]
    if list(L['size']) != [W, H]: die(f'layout size {L["size"]} != image {W}x{H}')
    geom = derive(L['devices'])
    allowed = build_masks(L, geom, W, H)
    report = {'inputs': {'painted': painted_path, 'rear': rear_path, 'panel': panel_path, 'out': out_path,
                         'size': [W, H], 'plate_src': a.plate_src},
              'derived': {'screws_per_row': 4, 'vents': {g['name']: len(g['vents']) for g in geom}},
              'scrub': {}, 'donor': {}, 'screws': {}, 'plates': {}, 'marks': {}, 'gates': {}}

    # 0) 스크럽 — scrub.py 규칙의 뒷면 판(레이아웃+나사+슬릿 보호)
    scrubbed, nscrub = scrub(src, allowed)
    report['scrub'] = {'bad_px': nscrub, 'pct': round(nscrub * 100.0 / (W * H), 3)}
    if a.scrub_out:
        Image.fromarray(scrubbed).save(a.scrub_out)
        print(f'rear-conform: scrub {a.scrub_out} ({nscrub} px = {report["scrub"]["pct"]}%)')

    out = scrubbed.astype(np.float32)

    # 1) 잭 이식 — 칠해진(스크럽된) 그림에서 가장 깨끗한 잭 하나를 25자리에
    metrics = jack_metrics(conform.lum(out), hsv_of(scrubbed), L, geom)
    donor = pick_donor(metrics)
    jacks = jacks_of(L)
    transplant_jacks(out, scrubbed, donor, jacks)
    report['donor'] = {'chosen': donor, 'metrics': metrics, 'transplanted': len(jacks)}
    print(f"rear-conform: donor jack {donor['device']}/{donor['name']} ({donor['cx']},{donor['cy']}) "
          f"contrast {donor['contrast']} offset {donor['offset']} -> {len(jacks)} positions")

    # 2) 나사 이식 — 게이트 ⑥(전 행 4/4 생존)을 보호만으로는 채울 수 없는 칠해진 실측에 대한
    #    잭 이식과 같은 규칙(모듈 문서의 스펙 충돌 기록 참조). 좌·우 클래스별 기증자.
    smetrics = screw_metrics(conform.lum(out), hsv_of(scrubbed), geom, allowed)
    sdonors = pick_screw_donors(smetrics)
    nscrew = transplant_screws(out, scrubbed, sdonors, geom)
    report['screws'] = {'chosen': sdonors, 'metrics': smetrics, 'transplanted_px': nscrew,
                        'note': 'core d<=5 hard + rim 5..7 feather, per left/right class'}
    print('rear-conform: donor screws ' + ', '.join(
        f"{side} {e['row']}/p{e['pos']} ({e['cx']},{e['cy']}) bright−face {e['minus_face']} offset {e['offset']}"
        for side, e in sdonors.items()) + f' -> 32 positions ({nscrew} px)')

    # 3) 이름판 이식 — 앞면 기증 픽셀(conform.paste_rect)
    srcp, npx = paste_plates(out, panel_old.astype(np.float32), front_plates, L, a.plate_src)
    report['plates'] = {'src': a.plate_src, 'src_rect': list(srcp['rect']), 'margin': PLATE_MARGIN, 'px': npx,
                        'count': len(L['devices'])}
    print(f"rear-conform: transplanted {len(L['devices'])} nameplates <- {a.plate_src} {srcp['rect']} ({npx} px)")

    # 4) 자국 메움 — conform.py B 규칙(기준색 거리 > 22 → 팽창 5 → 기준색+그레인+페더 5)
    rects = [gm['rect'] for gm in geom]
    C, dist, refs, inmod = refs_and_dist(out, allowed, rects, (H, W))
    junk = dist > T_MARK
    marks = (~allowed) & junk
    dil = conform.mdilate(marks, DILATE_MARK)
    alpha = np.maximum(conform.feather_alpha(dil, FILL_FEATHER), junk.astype(np.float32))
    G = conform.grain_field((H, W))
    filled = conform.apply_fill(out, dil & ~allowed, alpha, C, G)
    report['marks'] = {'refs': refs, 'threshold': T_MARK, 'pre_marks_px': int(marks.sum()),
                       'pre_ratio': round(float(marks.sum()) / (W * H), 6), 'filled_px': filled}
    print(f"rear-conform: marks {int(marks.sum())} px ({report['marks']['pre_ratio']*100:.2f}%) -> filled {filled} px, refs {refs}")

    final = np.rint(np.clip(out, 0, 255)).astype(np.uint8)
    Image.fromarray(final).save(out_path)
    report['gates'] = measure(final, L, geom, panel_old, front_L, front_plates, a.plate_src, verbose=True)
    if a.report: json.dump(report, open(a.report, 'w'), indent=1); print(f'rear-conform: report {a.report}')
    print(f'rear-conform: wrote {out_path} ({W}x{H})')

if __name__ == '__main__':
    main()
