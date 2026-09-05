#!/usr/bin/env python3
"""tools/rack/scrub.py — 채색된 패널에서 '선언되지 않은 밝은/채도 높은 자국'(가짜 글자·낙서)을 지운다.

  python3 tools/rack/scrub.py <panel.png> <layout.json> <out.png>

원리: layout.json이 모든 컨트롤(노브+눈금 고리, 버튼, 패드, 라벨판, LED, 표시창, 섹션 띠, 스코프)의 자리를 안다.
그 밖의 영역에서 밝기 > 150 또는 채도 > 110 인 픽셀은 diffusion이 채운 가짜 글자다 → 3px 팽창 후 주변 중앙값(반경 9)으로 메운다.
노브 면 안에서는 채도 > 90 인 픽셀(붉은 낙서·초록 별)만 같은 방법으로 지운다(포인터는 크림색·저채도라 남는다).
실측(2026-09-05): seed에 따라 'FACUNG COX BECOME' 류의 붉은 글자와 노브 면 붉은 눈금이 반복 출현 — 프롬프트로는 막히지 않는다.
"""
import sys, json
from PIL import Image, ImageDraw, ImageFilter, ImageChops
panel=Image.open(sys.argv[1]).convert('RGB'); L=json.load(open(sys.argv[2])); W,H=panel.size
keep=Image.new('L',(W,H),0); d=ImageDraw.Draw(keep)
for k in L['knobs']: r=k['r']+18; d.ellipse([k['cx']-r,k['cy']-r,k['cx']+r,k['cy']+r],fill=255)
for key in ('buttons','pads','plates','displays'):
    for b in L.get(key,[]): x,y,w,h=b['rect']; d.rectangle([x-2,y-2,x+w+2,y+h+2],fill=255)
# 모듈 테두리 띠(12px)와 섀시 사이 틈은 보호 — 잉크 선·베벨 하이라이트가 글자로 오판된다(v4s에서 13.8% 과다 스크럽)
for p in L['panels']:
    x,y,w,h=p['rect']; d.rectangle([x-8,y-8,x+w+8,y+h+8],outline=255,width=20)
for l in L.get('leds',[]): d.ellipse([l['cx']-10,l['cy']-10,l['cx']+10,l['cy']+10],fill=255)
if 'scope' in L: x,y,w,h=L['scope']['rect']; d.rectangle([x-8,y-8,x+w+8,y+h+8],fill=255)
if 'display' in L: x,y,w,h=L['display']['rect']; d.rectangle([x-4,y-4,x+w+4,y+h+4],fill=255)
for p in L['panels']: x,y,w,h=p['rect']; d.rectangle([x,y,x+round(20*W/768),y+h],fill=255)  # 섹션 색 띠
hsv=panel.convert('HSV'); S=hsv.getchannel('S'); V=hsv.getchannel('V')
bad=ImageChops.lighter(V.point(lambda v:255 if v>150 else 0), S.point(lambda v:255 if v>110 else 0))
bad=ImageChops.subtract(bad, keep)
# 노브 면: 채도만
face=Image.new('L',(W,H),0); fd=ImageDraw.Draw(face)
for k in L['knobs']: r=k['r']-2; fd.ellipse([k['cx']-r,k['cy']-r,k['cx']+r,k['cy']+r],fill=255)
bad=ImageChops.lighter(bad, ImageChops.multiply(S.point(lambda v:255 if v>90 else 0), face))
bad=bad.filter(ImageFilter.MaxFilter(5))
fill=panel.filter(ImageFilter.MedianFilter(19))
out=Image.composite(fill, panel, bad); out.save(sys.argv[3])
n=sum(1 for v in bad.get_flattened_data() if v) if hasattr(bad,'get_flattened_data') else sum(1 for v in bad.getdata() if v); print(f'scrub: {sys.argv[3]} scrubbed {n} px ({100*n/(W*H):.2f}%)')
