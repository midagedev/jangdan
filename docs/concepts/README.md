# 컨셉 이미지 — 확정 (2026-09-05)

## 채택 (사용자 판정 "좋다 이걸로 고정")

| 파일 | 역할 | 모델 |
|---|---|---|
| **room-attic-portrait-v2.png** | **방 뷰 기준컷(세로).** 다락방 밤, 경사 천창의 비, 단 하나의 스탠드, 작은 인물, 큰 여백. 화면녹화 기본 구도 | fal-ai/flux/dev, guidance 2.5, 28 steps, portrait_16_9 |
| **device-panel-v1.png** | **악기 뷰 UI 배치 기준.** 베이스라인 노브 2열×6, 드럼 패드 12, 하단 16스텝 버튼, 상단 중앙 녹색 스코프, 나무 옆판, 키보드 없음 | 같은 설정, landscape_4_3 |
| room-attic-v1.png | 가로 방 뷰(초기 채택). 남성형 캐릭터·기기 미상세 → 세로 v2가 대체. 가로 구도 참조용 | 같은 설정 |

프롬프트 원문은 각 `.prompt.txt`, 응답 원본은 `.json`.

## 스타일 계약 — 이후 모든 에셋 프롬프트에 그대로 붙인다

**스타일**
> Hand-drawn 1990s TV anime cel look: rough ink lines with slight wobble, flat colors with visible paint texture, limited muted palette, simple two-tone shadows, matte, no glow, no bloom, no glossy highlights, no photorealism, imperfect and humble.

**캐릭터(고유, 레지던트 DJ)**
> an original character: a young woman, short dark wavy bob haircut, round glasses, oversized dark green hoodie, no headphones

**기기(우리가 구현하는 것과 일치)**
> a flat tabletop groovebox with wooden side panels and NO keyboard: on its left half two identical horizontal rows of six chunky knobs (two bassline sections), on its right half a grid of twelve square rubber drum pads, along the bottom edge a single row of sixteen small orange step buttons with a few lit, and top center a small green oscilloscope screen showing a waveform

**방**
> a small attic room at night, a slanted skylight where rain streaks down, a single desk lamp making the only warm pool of light in a big dark blue room … an orange tabby cat on the floorboards near the desk, a radiator, a rug, records leaning against the wall, lots of quiet empty space

**규칙**
- 모델: `fal-ai/flux/dev`, `guidance_scale` 2.5, `num_inference_steps` 28. flux-pro는 과렌더("AI 이미지"), recraft는 무드 약함 — 실측으로 제외.
- 리얼리즘·광택·블룸·소품 과밀 금지. 책상 위 소품 3~4개. 여백을 남긴다.
- Lofi Girl과 겹치는 요소 금지: 측면 프로필 구도, 왼쪽 위 스탠드, 창턱 고양이, 헤드폰. 참조 작품 캐릭터 금지.
- 실기 이름·외관 금지(Roland, TB-303, TR-808, TR-909, ReBirth, Winamp).
- 시간대 변주(저녁·비 오는 오후)는 같은 방·같은 인물로만. 포모도로/에너지 곡선의 시각 표현으로 쓴다.

## 판정 이력 (스크래치에만 남은 것)
recraft pixel_art 3장(무드 눌림) → flux-pro 3장(무드 좋으나 AI 냄새) → ghibli 3장(지나치게 리얼) → retro 3장 → cel 3장(AI 냄새, 첫 장만 양호) → 손그림 규칙+flux/dev 3장(fluxdev 채택, 구도가 Lofi Girl과 겹침) → 구도 3안(와이드 채택) → 와이드 확장 3장(세로 채택) → v2(여성 캐릭터·실제 기기 레이아웃) **확정**.

## 배우 포즈 스프라이트 실험 (2026-09-06) — 보류

방 뷰 캐릭터 6동작·고양이 4동작을 플레이트 크롭의 flux/dev image-to-image(seed 고정)로 만들려 했다. `tools/fal-poses.py`.
- 강도 0.45: 배경·정체성은 지키지만 **포즈가 바뀌지 않는다**(포즈 간 픽셀 차 0.6%). 고양이도 같다(cat-2 한 장만 표정 변화).
- 강도 0.72(전체 크롭): 포즈는 움직이나 인물이 다른 사람(포니테일·안경 소실·후디 소실), 기기가 랩톱으로, 의자·신발 색이 바뀌고 rect 우변에서 기기가 두 물체로 갈라진다(opus 비전 판정 FIX).
- 상체 크롭 + 정체성 잠금 프롬프트 + 강도 0.55/0.6: 다시 포즈 무변화(포즈 간 차 ≤ 1.5%).
결론: 이 방식으로는 "정체성 유지"와 "포즈 변화"를 동시에 얻지 못한다. 방 뷰는 **플레이트 서브이미지 변형(회전·이동·스케일 ≤ 3%)**으로 동작을 내는 현재 구현을 유지한다. 재시도 후보: 캐릭터 단독 시트를 흰 배경에 생성 → birefnet 절단 → 플레이트 위 합성(조명 불일치 위험), 또는 ControlNet 포즈 조건(flux-lora-canny는 색이 바래는 실측이 있어 pose 컨트롤 모델을 따로 실측해야 한다).
