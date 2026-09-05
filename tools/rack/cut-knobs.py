#!/usr/bin/env python3
"""tools/rack/cut-knobs.py — 칠해진 패널에서 반지름 클래스별 노브 스프라이트를 잘라낸다.

  python3 tools/rack/cut-knobs.py <panel.png> <layout.json> <outdir>

각 반지름 클래스에서 "가장 깨끗한" 노브(면 안의 채도 합이 최소 — 붉은 낙서·별 같은 hallucination이 없는 것)를
고르고, r+3 원형 알파로 잘라(눈금은 패널에 남긴다) knob-r<r>.png 로 저장한다. 어떤 노브를 골랐는지 sprites.json에 기록.
실측(2026-09-05): 모델이 노브 면에 붉은 글자 흉내를 그리는 일이 잦다 — 최소 채도 선택이 그것을 피한다.
"""
import sys, json, os
from PIL import Image, ImageDraw, ImageStat
panel=Image.open(sys.argv[1]).convert('RGBA'); L=json.load(open(sys.argv[2])); out=sys.argv[3]; os.makedirs(out,exist_ok=True)
best={}
for k in L['knobs']:
    r=k['r']; face=panel.crop((k['cx']-r+4,k['cy']-r+4,k['cx']+r-4,k['cy']+r-4)).convert('HSV')
    m=Image.new('L',face.size,0); ImageDraw.Draw(m).ellipse([0,0,face.size[0]-1,face.size[1]-1],fill=255)
    sat=ImageStat.Stat(face.getchannel('S'),m).mean[0]
    if r not in best or sat<best[r][0]: best[r]=(sat,k)
rec={}
for r,(sat,k) in sorted(best.items()):
    R=r+3; crop=panel.crop((k['cx']-R,k['cy']-R,k['cx']+R,k['cy']+R)); mask=Image.new('L',crop.size,0); ImageDraw.Draw(mask).ellipse([0,0,2*R-1,2*R-1],fill=255); crop.putalpha(mask)
    p=os.path.join(out,f'knob-r{r}.png'); crop.save(p); rec[f'knob-r{r}.png']={'from':k['name'],'section':k['section'],'saturation':round(sat,1),'size':crop.size}
    print(f'cut-knobs: r={r} <- {k["name"]} (sat {sat:.1f}) -> {p}')
json.dump(rec,open(os.path.join(out,'sprites.json'),'w'),indent=1)
