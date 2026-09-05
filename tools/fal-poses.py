#!/usr/bin/env python3
"""tools/fal-poses.py — 방 뷰 배우(캐릭터·고양이) 포즈 스프라이트: 플레이트 크롭을 flux/dev image-to-image로
포즈만 바꿔 그린다(seed 고정, 강도 낮음 → 배경·조명·선 굵기가 원본과 이어진다). 스타일 계약은 docs/concepts/README.md.
  python3 tools/fal-poses.py <base-crop.png> <out-dir> <prefix> <strength> <seed> "<pose1>" "<pose2>" ...
산출: <out-dir>/<prefix>-<n>.png(+ .json 응답 원본). 크기는 입력과 같다(2× 크롭 → 앱이 rect 크기로 축소)."""
import sys, json, base64, urllib.request, os, subprocess
base, out, prefix, strength, seed = sys.argv[1], sys.argv[2], sys.argv[3], float(sys.argv[4]), int(sys.argv[5])
poses = sys.argv[6:]
KEY = os.environ.get('FAL_KEY') or subprocess.run(['bash', '-lc', 'echo -n $FAL_KEY'], capture_output=True, text=True).stdout.strip()
assert KEY, 'FAL_KEY 없음'
STYLE = ('Hand-drawn 1990s TV anime cel look: rough ink lines with slight wobble, flat colors with visible paint texture, '
         'limited muted palette, simple two-tone shadows, matte, no glow, no bloom, no glossy highlights, no photorealism, imperfect and humble. ')
from PIL import Image
img = Image.open(base).convert('RGB')
data = 'data:image/png;base64,' + base64.b64encode(open(base, 'rb').read()).decode()
os.makedirs(out, exist_ok=True)
for i, pose in enumerate(poses):
    body = json.dumps({'prompt': STYLE + pose + ' Same room, same lamp light, same colors, same framing. No text.',
                       'image_url': data, 'strength': strength, 'guidance_scale': 3.0, 'num_inference_steps': 28, 'seed': seed,
                       'image_size': {'width': img.size[0], 'height': img.size[1]}}).encode()
    req = urllib.request.Request('https://fal.run/fal-ai/flux/dev/image-to-image', data=body,
                                 headers={'Authorization': 'Key ' + KEY, 'Content-Type': 'application/json'})
    resp = json.load(urllib.request.urlopen(req, timeout=180))
    p = f'{out}/{prefix}-{i}.png'
    open(p + '.json', 'w').write(json.dumps({'pose': pose, 'strength': strength, 'seed': seed, 'resp': resp}))
    urllib.request.urlretrieve(resp['images'][0]['url'], p)
    print('saved', p, Image.open(p).size)
