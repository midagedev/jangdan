# tools/rack/wire.py — 장단 기기 뷰 와이어프레임 + 레이아웃 계약(JSON).
# 사용: python3 tools/rack/wire.py <out.png> <layout.json> [W=720] [H=1280]
# 좌표는 768x1344 기준 설계를 W/768, H/1344 비율로 스케일한다(정수 반올림).
# 이 JSON이 히트 영역·스프라이트 절단·라벨 위치의 단일 소유자다. 채색은 paint.sh.
import sys, json, math
from PIL import Image, ImageDraw
OUT=sys.argv[1]; LAY=sys.argv[2]; W=int(sys.argv[3]) if len(sys.argv)>3 else 720; H=int(sys.argv[4]) if len(sys.argv)>4 else 1280
SX=W/768; SY=H/1344
def X(v): return round(v*SX)
def Y(v): return round(v*SY)
def R(v): return round(v*(SX+SY)/2)
im=Image.new('RGB',(768,1344),(38,36,40)); d=ImageDraw.Draw(im)
layout={'size':[768,1344],'knobs':[],'buttons':[],'pads':[],'plates':[],'panels':[],'leds':[],'displays':[]}
def panel(x,y,w,h,fill,name):
    d.rounded_rectangle([x,y,x+w,y+h],radius=14,fill=fill,outline=(20,20,22),width=4); layout['panels'].append({'name':name,'rect':[x,y,w,h]})
    # 이름판 (글자는 코드가 나중에 폰트로)
    d.rounded_rectangle([x+14,y+10,x+150,y+34],radius=4,fill=(225,215,190)); layout['plates'].append({'for':name,'rect':[x+14,y+10,136,24]})
def knob(cx,cy,r,name,section):
    d.ellipse([cx-r-6,cy-r-6,cx+r+6,cy+r+6],fill=(28,28,30))          # 스커트
    d.ellipse([cx-r,cy-r,cx+r,cy+r],fill=(70,72,78),outline=(15,15,17),width=3)
    d.line([cx,cy,cx,cy-r+5],fill=(235,225,200),width=5)              # 포인터 12시
    d.rounded_rectangle([cx-r-2,cy+r+10,cx+r+2,cy+r+26],radius=3,fill=(225,215,190))  # 라벨판
    layout['knobs'].append({'name':name,'section':section,'cx':cx,'cy':cy,'r':r})
def button(x,y,s,fill,name,section):
    d.rounded_rectangle([x,y,x+s,y+s],radius=5,fill=fill,outline=(15,15,17),width=3); layout['buttons'].append({'name':name,'section':section,'rect':[x,y,s,s]})
def led(cx,cy,on):
    layout.setdefault('leds',[]).append({'cx':cx,'cy':cy,'r':6})
    d.ellipse([cx-6,cy-6,cx+6,cy+6],fill=(230,80,60) if on else (90,40,36),outline=(15,15,17),width=2)
# 헤더: 이름판 + 스코프
d.rounded_rectangle([24,20,300,70],radius=6,fill=(225,215,190)); layout['plates'].append({'for':'title','rect':[24,20,276,50]})
d.rounded_rectangle([330,16,744,116],radius=10,fill=(30,30,32),outline=(15,15,17),width=4)
d.rounded_rectangle([344,26,730,106],radius=6,fill=(60,110,80)); d.line([344,66,730,66],fill=(120,190,140),width=2)
layout['scope']={'rect':[344,26,386,80]}
# 베이스라인 A / B
for i,(y,tint) in enumerate([(140,(150,60,50)),(400,(50,90,140))]):
    name=f'bassline{"AB"[i]}'; panel(24,y,720,240,(58,58,64),name)
    d.rectangle([24,y,44,y+240],fill=tint)  # 섹션 색 띠
    labels=['TUNE','CUTOFF','RESO','ENV','DECAY','ACCENT']
    for k in range(6): knob(120+k*112,y+95,34,labels[k],name)
    # 하단 줄을 빽빽하게: 파형 버튼 2 + 슬라이드/액센트 버튼 2 + 옥타브 버튼 2 + 패턴 버튼 4(LED) + 작은 표시창
    #  (빈 면적이 넓으면 diffusion이 가짜 글자를 채운다 — 의도한 컨트롤로 채운다. 실측 2026-09-05)
    for k,nm in enumerate(['saw','sqr','slide','acc','oct-','oct+']): button(90+k*52,y+178,36,(90,90,96),nm,name); led(108+k*52,y+228,k in (0,3))
    for k in range(4): button(430+k*52,y+178,36,(90,90,96),f'pat{"ABCD"[k]}',name); led(448+k*52,y+228,k==0)
    d.rounded_rectangle([650,y+180,730,y+214],radius=4,fill=(40,60,50),outline=(15,15,17),width=3); layout.setdefault('displays',[]).append({'for':name,'rect':[650,y+180,80,34]})
# 드럼 6보이스 (LEVEL·TUNE) + 패드
panel(24,660,720,300,(52,54,58),'drums'); d.rectangle([24,660,44,960],fill=(200,140,50))
names=['BD','SD','CH','OH','CP','CB']
for k in range(6):
    x=110+k*110; knob(x,730,26,f'{names[k]}_LEVEL','drums'); knob(x,810,26,f'{names[k]}_TUNE','drums')
    d.rounded_rectangle([x-36,870,x+36,942],radius=8,fill=(76,78,84),outline=(15,15,17),width=3); layout['pads'].append({'name':names[k],'rect':[x-36,870,72,72]})
# FX + 스텝 그리드
panel(24,980,720,340,(58,58,64),'fx'); d.rectangle([24,980,44,1320],fill=(90,150,90))
for k,l in enumerate(['DELAY','DRIVE','COMP','MASTER']): knob(120+k*112,1060,34,l,'fx')
knob(660,1060,44,'TEMPO','fx')
for k in range(16):
    x=64+k*42; button(x,1160,34,(200,110,40) if k%4==0 else (120,80,50),f'step{k+1}','fx'); led(x+17,1215,k==0)
button(64,1250,50,(80,140,90),'play','fx'); button(140,1250,50,(140,80,80),'rec','fx')
d.rounded_rectangle([230,1250,470,1300],radius=6,fill=(40,60,50),outline=(15,15,17),width=3); layout['display']={'rect':[230,1250,240,50]}
# 눈금(손그림 단서) + 노이즈 텍스처
for k in layout['knobs']:
    cx,cy,r=k['cx'],k['cy'],k['r']
    for i in range(11):
        a=math.radians(-135+i*27-90)
        d.line([cx+math.cos(a)*(r+9),cy+math.sin(a)*(r+9),cx+math.cos(a)*(r+15),cy+math.sin(a)*(r+15)],fill=(215,205,180),width=2)
noise=Image.effect_noise((768,1344),18).convert('L')
im=Image.composite(im, Image.blend(im, Image.new('RGB',(768,1344),(0,0,0)),0.12), noise.point(lambda v: 255 if v>128 else 200))
im=im.resize((W,H),Image.LANCZOS); im.save(OUT)
def sc(o):
    if isinstance(o,dict):
        r={}
        for k,v in o.items():
            if k=='rect': r[k]=[X(v[0]),Y(v[1]),X(v[2]),Y(v[3])]
            elif k=='cx': r[k]=X(v)
            elif k=='cy': r[k]=Y(v)
            elif k=='r': r[k]=R(v)
            else: r[k]=sc(v)
        return r
    if isinstance(o,list): return [sc(v) for v in o]
    return o
layout['size']=[W,H]; layout=sc(layout)
json.dump(layout,open(LAY,'w'),indent=1); print(OUT,(W,H),'knobs',len(layout['knobs']),'buttons',len(layout['buttons']))
