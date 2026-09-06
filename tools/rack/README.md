# tools/rack — 기기 뷰 패널 파이프라인

코드가 기하를 소유하고 fal.ai가 룩을 입힌다. 스타일 계약은 `docs/concepts/README.md`에서 자동으로 읽는다.

```bash
python3 tools/rack/wire.py      wire.png layout.json 720 1280   # 와이어프레임 + 레이아웃 JSON(단일 소유자)
python3 tools/rack/paint.py     wire.png layout.json panel.png --seed 1234 --s1 0.7 --s2 0.45   # 모듈별 2× 채색
python3 tools/rack/scrub.py     panel.png layout.json panel-clean.png  # 선언되지 않은 자리의 밝은/채도 높은 자국(가짜 글자) 제거
python3 tools/rack/cut-knobs.py panel-clean.png layout.json sprites/  # 반지름 클래스별 가장 깨끗한 노브를 r+3 원형으로 절단
python3 tools/rack/conform.py   panel-v3.png layout-v3.json panel.png out.png --ymin 1280 --fill …  # 새 모듈 부품 일치화: 기존 노브 이식(r+18)·라벨판 평면화·기준색 자국 메움 + `--check` 게이트 재측정
```

- `paint.py`는 `panel.<module>@2x.png`(2× 백킹용 원본)와 `panel.paint.json`(seed·강도·모듈 영역·출처)을 함께 남긴다.
- 라벨·숫자는 앱이 폰트로 올린다. 버튼 lit은 밝기 틴트. 노브 회전은 −135°..+135° = 0..1.
- 판정 이력(2026-09-05): 요소 단독 생성 조립 → 콜라주(탈락) / 전체 한 장 채색 → 실행마다 드리프트 / **모듈별 채색 + 절단** 채택. 강도 0.74↑는 노브 포인터가 돈다. canny는 색이 바랜다. 빈 면적은 가짜 글자로 채워지므로 와이어프레임에 의도한 컨트롤을 빽빽하게 넣는다.
- 판정 이력(2026-09-06): 랙 v3 후보(mixer·fx2 i2i) 비전 채택 불가 — 새 노브가 다른 부품(캡 휘도 93.9 vs 68.9)·라벨판 뭉개짐·모듈 rect 밖 서명 등 → fal 재채색 없이 `conform.py`가 기존 부품 이식·평면화·기준색 메움으로 닫는다(P4-rackart-b).
- 후보 v1(2026-09-05, seed 1234, 0.7→0.45, 스크럽 7.2%)은 `scratch/rack-candidate-v1/`(gitignore)에 보관. 채택되면 `app/assets/`로 옮긴다. 남은 결함: 버튼 옆 붉은 잔여 글자, 헤더 미니 노브 뭉개짐, 스텝 줄 끝 'EC' 가짜 글자.
