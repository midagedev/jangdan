#!/usr/bin/env python3
"""tools/rack/merge-layout.py — 기존 layout.json에 새 모듈(wire 출력) 항목을 덧붙여 확장판 후보를 만든다.

  python3 tools/rack/merge-layout.py <base layout.json> <wire layout.json> <out.json> \
      [--ymin 1280] [--seed 1234] [--modules mixer,fx2]

규칙(후보 v3 계약): size는 wire 출력값으로 교체하고, knobs/buttons는 section이, plates는 'for'가,
panels는 name이 모듈 목록에 있는 항목만, leds는 cy >= ymin(새 모듈 영역)인 것만 **배열 끝에** 덧붙인다
— leds 순서는 앱 코드가 스텝 LED 인덱스로 쓰므로 기존 순서·개수가 불변이어야 한다. 나머지 항목은
그대로 재직렬화한다. 덧붙이기 전에 base를 읽어 다시 dump한 결과가 원본 파일과 바이트로 같은지 먼저
확인하고(다르면 종료 — indent=1 서식 가정이 깨진 것), 마지막에 out에서 기존 항목이 base와
똑같은지 재단언한다.
"""
import sys, os, json, argparse
ap=argparse.ArgumentParser()
ap.add_argument('base'); ap.add_argument('wire'); ap.add_argument('out')
ap.add_argument('--ymin',type=int,default=1280,help='새 모듈 영역의 최소 y — 이 이상의 LED만 덧붙인다')
ap.add_argument('--seed',type=int,default=1234); ap.add_argument('--modules',default='mixer,fx2')
a=ap.parse_args()
raw=open(a.base,encoding='utf-8').read(); base=json.loads(raw)
ser=json.dumps(base,indent=1)
if ser!=raw and ser+'\n'!=raw: sys.exit('merge-layout: base does not round-trip through json.dumps(indent=1) — refusing to rewrite it')
mods=[m.strip() for m in a.modules.split(',') if m.strip()]
wire=json.load(open(a.wire))
wpanels={p['name'] for p in wire['panels']}
missing=[m for m in mods if m not in wpanels]
if missing: sys.exit(f'merge-layout: module(s) not in wire panels: {missing} (wire has {sorted(wpanels)})')
if wire['size'][1]<=base['size'][1]: sys.exit(f'merge-layout: wire height {wire["size"][1]} must exceed base height {base["size"][1]}')
n0={k:len(base[k]) for k in ('knobs','buttons','plates','panels','leds')}
base['size']=wire['size']
base['knobs']=base['knobs']+[k for k in wire['knobs'] if k.get('section') in mods]
base['buttons']=base['buttons']+[b for b in wire['buttons'] if b.get('section') in mods]
base['plates']=base['plates']+[p for p in wire['plates'] if p.get('for') in mods]
base['panels']=base['panels']+[p for p in wire['panels'] if p['name'] in mods]
base['leds']=base['leds']+[l for l in wire['leds'] if l['cy']>=a.ymin]
base.setdefault('source',{})['v3']={'wire':os.path.basename(a.wire),'seed':a.seed,'modules':mods}
# 재단언: 기존 항목은 앞쪽 그대로(끝에 덧붙였으므로 prefix여야 한다).
for k in ('knobs','buttons','plates','panels','leds'):
    if base[k][:n0[k]]!=json.loads(raw)[k]: sys.exit(f'merge-layout: existing {k} changed — aborting')
with open(a.out,'w',encoding='utf-8') as f: f.write(json.dumps(base,indent=1)+('\n' if raw.endswith('\n') else ''))
print(f'merge-layout: {a.out} size={base["size"]} knobs {n0["knobs"]}+{len(base["knobs"])-n0["knobs"]} '
      f'buttons {n0["buttons"]}+{len(base["buttons"])-n0["buttons"]} plates {n0["plates"]}+{len(base["plates"])-n0["plates"]} '
      f'panels {n0["panels"]}+{len(base["panels"])-n0["panels"]} leds {n0["leds"]}+{len(base["leds"])-n0["leds"]}')
