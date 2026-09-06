# tools/rack/rear.py — 랙 뒷면(§14.3) 와이어프레임 + 잭 좌표 계약(JSON).
# 사용: python3 tools/rack/rear.py <out-wire.png> <out-rear.json> <front-layout.json>
#
# 앞면과 같은 좌표계(720×2000)를 쓴다 — 스크롤 규칙이 하나여야 하기 때문이다. 행(장치)의
# rect는 앞면 layout.json의 panels에서 유도한다(같은 자리에 같은 모듈이 있다는 것이 뒷면의
# 읽기 단서다). fx2 한 판에는 장치가 둘(리버브·코러스)이라 위아래로 나눈다.
#
# 잭 좌표의 단일 소유자는 이 스크립트가 내는 JSON이다(Go에 픽셀 상수 금지). 포트 수는
# engine/rack.go kindPorts의 전사이며, Go 게이트(TestRearLayoutPorts)가 두 표의 일치를 잰다.
# 채색은 앞면과 같은 파이프라인(paint.py --only <행 이름>)을 쓴다 — 그래서 panels 목록을 함께 낸다.
import sys, json
from PIL import Image, ImageDraw

WIRE, REAR, FRONT = sys.argv[1], sys.argv[2], sys.argv[3]
front = json.load(open(FRONT))
W, H = int(front['size'][0]), int(front['size'][1])
pan = {p['name']: p['rect'] for p in front['panels']}

# 잭 기하 계약. r12 + 히트 여유 4 = 지름 32 ≥ 28(§14.3 요구).
JR = 12          # 잭 구멍 반지름
JPITCH = 42      # 세로 간격
JCOL = 46        # 열 간격
JPERCOL = 4      # 한 열 최대
JEDGE = 54       # 판 가장자리 → 첫 열 중심
TOPINSET = 42    # 이름판 아래로 밀어내는 여유
PLATE = (14, 10, 136, 24)  # 이름판(앞면 관례와 같은 오프셋·크기)

# 행 = (슬롯, 이름, rect, 섹션 띠색, 입력 라벨, 출력 라벨).
# 포트 라벨은 engine/rack.go의 포트 주석 그대로다(앱이 폰트로 올린다).
def half(r, top):
    x, y, w, h = r
    hh = (h - 4) / 2
    return [x, y if top else y + hh + 4, w, hh]

rows = [
    (0, 'bassA',  pan['basslineA'], (150, 60, 50),  [], ['OUT']),
    (1, 'bassB',  pan['basslineB'], (50, 90, 140),  [], ['OUT']),
    (2, 'drums',  pan['drums'],     (200, 140, 50), [], ['MIX', 'SC', 'BD', 'SD', 'CH', 'OH', 'CP', 'CY']),
    (3, 'fx',     pan['fx'],        (90, 150, 90),  ['DUCK', 'DIR', 'SC', 'DLY'], ['L', 'R']),
    (6, 'main',   pan['mixer'],     (120, 80, 150), ['L', 'R'], []),
    (4, 'reverb', half(pan['fx2'], True),  (120, 80, 150), ['IN'], ['L', 'R']),
    (5, 'chorus', half(pan['fx2'], False), (120, 80, 150), ['IN'], ['L', 'R']),
    (7, 'poly',   pan['poly'],      (60, 130, 170), [], ['OUT']),
]

im = Image.new('RGB', (W, H), (26, 25, 28))
d = ImageDraw.Draw(im)


def jacks(rect, labels, right):
    """열 배치: 한 열 최대 4개, 세로 중앙 정렬. 입력은 왼쪽에서 안쪽으로, 출력은 오른쪽에서 안쪽으로."""
    x, y, w, h = rect
    cyc = y + TOPINSET + (h - TOPINSET - 10) / 2
    out = []
    for i, nm in enumerate(labels):
        col, row = i // JPERCOL, i % JPERCOL
        n = min(len(labels) - col * JPERCOL, JPERCOL)
        cx = (x + w - JEDGE - col * JCOL) if right else (x + JEDGE + col * JCOL)
        cy = cyc + (row - (n - 1) / 2) * JPITCH
        out.append({'name': nm, 'port': i, 'cx': round(cx), 'cy': round(cy), 'r': JR})
    return out


layout = {'size': [W, H], 'panels': [], 'plates': [], 'devices': []}
for slot, name, rect, tint, ins, outs in rows:
    x, y, w, h = [round(v) for v in rect]
    d.rounded_rectangle([x, y, x + w, y + h], radius=14, fill=(46, 45, 50), outline=(18, 18, 20), width=4)
    d.rectangle([x, y, x + 20, y + h], fill=tint)  # 섹션 색 띠(앞면과 같은 자리·같은 색 — 같은 악기라는 단서)
    px, py, pw, ph = x + PLATE[0], y + PLATE[1], PLATE[2], PLATE[3]
    d.rounded_rectangle([px, py, px + pw, py + ph], radius=4, fill=(225, 215, 190))
    layout['panels'].append({'name': name, 'rect': [x, y, w, h]})
    layout['plates'].append({'for': name, 'rect': [px, py, pw, ph]})
    # 나사 4개 — 뒷면의 관례. 잭 열과 겹치지 않게 모서리 안쪽 14px.
    for sx in (x + 14, x + w - 14):
        for sy in (y + 14, y + h - 14):
            d.ellipse([sx - 6, sy - 6, sx + 6, sy + 6], fill=(120, 120, 126), outline=(20, 20, 22), width=2)
            d.line([sx - 4, sy, sx + 4, sy], fill=(40, 40, 44), width=2)
    ij, oj = jacks([x, y, w, h], ins, False), jacks([x, y, w, h], outs, True)
    # 통풍 슬릿 — 빈 면이 넓으면 diffusion이 가짜 글자를 채운다(앞면 실측 규칙). 의도한 요소로 채운다.
    lx = x + JEDGE + 40 + (JCOL * ((len(ins) - 1) // JPERCOL) if ins else -40)
    rx = x + w - JEDGE - 40 - (JCOL * ((len(outs) - 1) // JPERCOL) if outs else -40)
    if rx - lx > 120:
        vy = y + TOPINSET + 10
        while vy < y + h - 24:
            d.rounded_rectangle([lx, vy, rx, vy + 6], radius=3, fill=(32, 31, 35), outline=(20, 20, 22), width=1)
            vy += 16
    for j in ij + oj:
        cx, cy, r = j['cx'], j['cy'], j['r']
        d.ellipse([cx - r - 6, cy - r - 6, cx + r + 6, cy + r + 6], fill=(126, 126, 132), outline=(20, 20, 22), width=3)
        d.ellipse([cx - r, cy - r, cx + r, cy + r], fill=(16, 16, 18))
        d.ellipse([cx - r + 4, cy - r + 4, cx + r - 4, cy + r - 4], fill=(38, 38, 42))
    layout['devices'].append({'slot': slot, 'name': name, 'rect': [x, y, w, h],
                              'plate': [px, py, pw, ph], 'in': ij, 'out': oj})

# 백킹 노이즈(앞면과 같은 손그림 결)
noise = Image.effect_noise((W, H), 18).convert('L')
im = Image.composite(im, Image.blend(im, Image.new('RGB', (W, H), (0, 0, 0)), 0.12),
                     noise.point(lambda v: 255 if v > 128 else 200))
im.save(WIRE)
json.dump(layout, open(REAR, 'w'), indent=1)
print(WIRE, (W, H), 'devices', len(layout['devices']),
      'jacks', sum(len(x['in']) + len(x['out']) for x in layout['devices']))
