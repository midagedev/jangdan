#!/usr/bin/env python3
"""tools/rack/paint.py — 와이어프레임을 모듈 단위로 채색해 기기 패널을 만든다.

  python3 tools/rack/paint.py <wire.png> <layout.json> <out.png> [--scale 2] [--s1 0.7] [--s2 0.5] [--seed 1234]

왜 모듈 단위인가(실측 2026-09-05): 패널 전체를 한 번에 image-to-image 하면 실행마다 패드·버튼 줄이
사라지거나 두 배가 되는 드리프트가 있었다(768 실행은 유지, 720 실행은 드럼 패드 소실). 모듈(layout.panels)
을 잘라 2×로 확대해 각각 칠하면 기하가 안정되고 해상도도 2× 백킹에 맞는다. seed를 고정해 재현 가능.
1패스 0.7(기하 보존 + 눈금·잉크 선) → 2패스 0.5(질감·LED). 0.74 이상은 노브 포인터가 제멋대로 돈다.
글자는 diffusion이 망치므로 빈 라벨판만 그리고 앱이 폰트로 올린다. 배경(섀시)은 전체 1패스 결과를 쓴다.
"""
import sys, json, base64, io, os, urllib.request, argparse
from PIL import Image
ap=argparse.ArgumentParser(); ap.add_argument('wire'); ap.add_argument('layout'); ap.add_argument('out')
ap.add_argument('--scale',type=int,default=2); ap.add_argument('--s1',type=float,default=0.7); ap.add_argument('--s2',type=float,default=0.5); ap.add_argument('--seed',type=int,default=1234)
a=ap.parse_args()
KEY=os.environ.get('FAL_KEY') or sys.exit('FAL_KEY is not set — export it in ~/.zshrc (see CLAUDE.md)')
HERE=os.path.dirname(os.path.abspath(__file__)); ROOT=os.path.abspath(os.path.join(HERE,'..','..'))
STYLE=next(l[2:].strip() for l in open(os.path.join(ROOT,'docs/concepts/README.md'),encoding='utf-8') if l.startswith('> Hand-drawn 1990s'))
COMMON=(' seen exactly from directly above, flat even lighting, no perspective, no glare, dark matte charcoal metal with visible brush texture, '
        'wobbly ink outlines, blank cream label plates with no text, no lettering anywhere, no writing, no logos, plain unmarked knob faces, keep every control exactly where it is')
PROMPTS={
 'header': STYLE+' The top strip of a compact electronic groovebox: a blank cream name plate on the left, a few tiny buttons, and a small green oscilloscope screen with a faint horizontal line on the right,'+COMMON,
 'basslineA': STYLE+' One horizontal module of an electronic groovebox: a red section stripe on the left edge, six round black rotary knobs with cream pointer marks and tick marks, a small blank cream label plate under each knob, a square waveform button with a red LED, four small square pattern buttons with LEDs under them,'+COMMON,
 'basslineB': STYLE+' One horizontal module of an electronic groovebox: a blue section stripe on the left edge, six round black rotary knobs with cream pointer marks and tick marks, a small blank cream label plate under each knob, a square waveform button with a red LED, four small square pattern buttons with LEDs under them,'+COMMON,
 'drums': STYLE+' One horizontal drum module of an electronic groovebox: an orange section stripe on the left edge, two rows of six small round black rotary knobs with cream pointer marks and tick marks and blank cream label plates, and a row of six square dark rubber drum pads along the bottom,'+COMMON,
 'fx': STYLE+' One horizontal module of an electronic groovebox: a green section stripe on the left edge, four round black knobs and one large tempo knob with tick marks and blank cream label plates, a row of sixteen small square orange step buttons with tiny red LEDs under them, a green play button, a red record button and a small dark display,'+COMMON,
 'chassis': STYLE+' The front panel of a compact electronic groovebox with four stacked horizontal modules,'+COMMON,
}
def i2i(img, prompt, strength, seed):
    buf=io.BytesIO(); img.save(buf,'PNG'); data='data:image/png;base64,'+base64.b64encode(buf.getvalue()).decode()
    body=json.dumps({'prompt':prompt,'image_url':data,'strength':strength,'guidance_scale':3.5,'num_inference_steps':28,'seed':seed,'image_size':{'width':img.size[0],'height':img.size[1]}}).encode()
    req=urllib.request.Request('https://fal.run/fal-ai/flux/dev/image-to-image',data=body,headers={'Authorization':'Key '+KEY,'Content-Type':'application/json'})
    try: resp=json.load(urllib.request.urlopen(req,timeout=300))
    except urllib.error.HTTPError as e: sys.exit(f'paint: HTTP {e.code} {e.read()[:300]}')
    return Image.open(io.BytesIO(urllib.request.urlopen(resp['images'][0]['url']).read())).convert('RGBA')
def snap16(v): return max(16, (v//16)*16)
wire=Image.open(a.wire).convert('RGBA'); L=json.load(open(a.layout)); W,H=wire.size
base=i2i(wire, PROMPTS['chassis'], a.s1, a.seed); out=base.resize((W,H)).copy()
regions=[('header',[0,0,W,L['panels'][0]['rect'][1]-6])]+[(p['name'],p['rect']) for p in L['panels']]
provenance={'wire':a.wire,'layout':a.layout,'seed':a.seed,'s1':a.s1,'s2':a.s2,'scale':a.scale,'modules':[]}
for name,(x,y,w,h) in regions:
    m=6; x0,y0=max(0,x-m),max(0,y-m); x1,y1=min(W,x+w+m),min(H,y+h+m)
    crop=wire.crop((x0,y0,x1,y1)); tw,th=snap16(crop.size[0]*a.scale),snap16(crop.size[1]*a.scale)
    up=crop.resize((tw,th),Image.LANCZOS)
    p1=i2i(up, PROMPTS.get(name,PROMPTS['chassis']), a.s1, a.seed); p2=i2i(p1, PROMPTS.get(name,PROMPTS['chassis']), a.s2, a.seed)
    hi_path=os.path.splitext(a.out)[0]+f'.{name}@{a.scale}x.png'; p2.save(hi_path)  # 2× 원본은 앱 백킹용으로 보존
    out.paste(p2.resize((x1-x0,y1-y0),Image.LANCZOS),(x0,y0))
    provenance['modules'].append({'name':name,'region':[x0,y0,x1-x0,y1-y0],'hires':os.path.basename(hi_path),'size':[tw,th]})
    print(f'paint: {name} {x1-x0}x{y1-y0} -> painted at {tw}x{th}')
out.save(a.out); json.dump(provenance,open(os.path.splitext(a.out)[0]+'.paint.json','w'),indent=1); print('paint:',a.out,out.size)
