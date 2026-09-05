#!/usr/bin/env bash
# tools/deploy-pages.sh — 빌드 산출물을 같은 레포(midagedev/jangdan)의 gh-pages 브랜치로 밀어 GitHub Pages에 배포.
#   bash tools/deploy-pages.sh
# URL: https://midagedev.github.io/jangdan/  (worklet/ = 워클릿 계측, app/ = Ebitengine 기기 뷰)
# Pages는 Content-Encoding 사전압축을 못 하므로 app/web/index.html 로더가 app.wasm.gz를 DecompressionStream으로 푼다.
# raw app.wasm(10MB)도 폴백용으로 함께 올린다. 나중에 Actions 워크플로로 전환 예정.
set -euo pipefail
cd "$(dirname "$0")/.."
# 도구 셸이 ~/.zshrc를 다시 읽지 않는 환경(에이전트 하네스)을 위한 폴백: 미설정이면 zshrc에서 읽는다.
: "${JANGDAN_REPORTS_URL:=$(grep -m1 '^export JANGDAN_REPORTS_URL=' ~/.zshrc 2>/dev/null | cut -d'"' -f2)}"
: "${JANGDAN_ADMIN_TOKEN:=$(grep -m1 '^export JANGDAN_ADMIN_TOKEN=' ~/.zshrc 2>/dev/null | cut -d'"' -f2)}"
REPO=midagedev/jangdan; URL="https://github.com/$REPO.git"
bash spike/worklet/build.sh >/dev/null && bash app/build.sh | tail -1
DIST=$(mktemp -d)/dist; mkdir -p "$DIST/worklet" "$DIST/app"
cp spike/worklet/public/{index.html,main.js,processor.js,engine.wasm} "$DIST/worklet/"
REPORT_URL=${JANGDAN_REPORTS_URL:-}; for d in worklet app; do printf "window.JD_REPORT_URL = %s;\n" "$( [[ -n "$REPORT_URL" ]] && printf "'%s/report'" "$REPORT_URL" || printf "''" )" > "$DIST/$d/report-config.js"; done
cp app/web/{index.html,host.js,processor.js,wasm_exec.js,engine.wasm,still.png,app.wasm,app.wasm.gz} "$DIST/app/"
cp -R app/web/assets "$DIST/app/assets"
[[ -n "$REPORT_URL" ]] || echo "warn: JANGDAN_REPORTS_URL unset — Send report는 비활성으로 배포됨" >&2
cat > "$DIST/index.html" <<HTML
<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>장단 lab</title>
<style>body{font:16px/1.6 -apple-system,system-ui,sans-serif;max-width:36rem;margin:3rem auto;padding:0 1rem;color:#222}a{display:block;padding:1rem;border:1px solid #ccc;border-radius:8px;margin:1rem 0;color:#124;text-decoration:none}small{color:#666}</style>
<h1>장단 · Phase 0 스파이크</h1>
<a href="worklet/">워클릿 엔진 계측 <small>— Start → 60초 → 화면의 JSON을 복사해 전달</small></a>
<a href="app/">Ebitengine 기기 뷰 스파이크 <small>— 플레이스홀더 UI, 노브 드래그 → 소리, wasm 2.5MB(gzip)</small></a>
<p><small>빌드 $(date '+%Y-%m-%d %H:%M %Z') · <a href="https://github.com/$REPO" style="display:inline;border:0;padding:0">소스</a></small></p>
HTML
touch "$DIST/.nojekyll"
WORK=$(mktemp -d)/pages
if git ls-remote --exit-code --heads "$URL" gh-pages >/dev/null 2>&1; then
  git clone -q --branch gh-pages --single-branch --depth 1 "$URL" "$WORK"
else
  git init -q "$WORK" && git -C "$WORK" checkout -q -b gh-pages && git -C "$WORK" remote add origin "$URL"
fi
find "$WORK" -mindepth 1 -maxdepth 1 ! -name .git -exec rm -rf {} +
cp -R "$DIST"/. "$WORK"/
git -C "$WORK" add -A
git -C "$WORK" -c user.name=midagedev -c user.email=midagedev@gmail.com commit -q -m "deploy $(date '+%Y-%m-%d %H:%M')" || { echo "nothing to deploy"; exit 0; }
git -C "$WORK" push -q -u origin gh-pages
gh api -X POST "repos/$REPO/pages" -f 'source[branch]=gh-pages' -f 'source[path]=/' >/dev/null 2>&1 && echo "pages: enabled (gh-pages)" || echo "pages: already configured"
du -sh "$DIST" | awk '{print "dist", $1}'
echo "deployed → https://midagedev.github.io/jangdan/  (반영 1~2분)"
