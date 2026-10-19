#!/usr/bin/env bash
# Real end-to-end pipeline test on a clean Git archive (N1/N2 verification).
#
# A temporary Git repository is created from this worktree's feature content and
# committed; the pipeline then archives that commit (no .git in the extracted
# tree) and runs the real go/npm/chromium toolchain with a fake SSH/scp and a
# fake target systemd that really supervises the built binary. It proves:
#   N1  every Go build in the archive succeeds without VCS stamping;
#   N2  the front-end build and webui assets precede every browser suite, the
#       version suite receives the candidate revision, and no test-only global
#       reaches the packaged bundle;
#   the packaged binary and the served JS embed the candidate full SHA (not the
#       version fixture SHA), the service really answers the entry URL, and the
#       target state says succeeded.
#
# Only this test's own processes are used; HOME/XDG/TMPDIR and all ports are
# isolated, caches are shared read-mostly (GOCACHE/GOMODCACHE/npm cache), and no
# real target, credential or project Git state is touched.
set -uo pipefail

TESTS_DIR=$(CDPATH= cd -- "$(dirname -- "$(readlink -f -- "$0")")" && pwd)
SCRIPTS_DIR=$(dirname "$TESTS_DIR")
WORKTREE=$(CDPATH= cd -- "$SCRIPTS_DIR/.." && pwd)

FIXTURE_SHA='8b54309f03dd75759291be48ff3f3b766ab89b5e'

missing=0
for c in git go npm node chromium curl tar sha256sum rsync timeout flock; do
	if ! command -v "$c" >/dev/null 2>&1; then
		echo "缺少必需工具：$c（本测试需要真实 go/npm/chromium）"
		missing=1
	fi
done
[ "$missing" -eq 0 ] || exit 1

REAL_HOME=$HOME
REAL_NPM_CACHE=$(npm config get cache 2>/dev/null || printf '%s' "$REAL_HOME/.npm")
REAL_GOCACHE=$(go env GOCACHE)
REAL_GOMODCACHE=$(go env GOMODCACHE)

ROOT=$(mktemp -d "${TMPDIR:-/tmp}/kaiwu-archive-e2e.XXXXXX")
export HOME=$ROOT/home
export XDG_CONFIG_HOME=$ROOT/home/.config
export TMPDIR=$ROOT/tmp
export FAKE_LOG_DIR=$ROOT/log
export GOCACHE=$REAL_GOCACHE
export GOMODCACHE=$REAL_GOMODCACHE
export npm_config_cache=$REAL_NPM_CACHE
mkdir -p "$HOME" "$TMPDIR" "$ROOT/bin" "$FAKE_LOG_DIR"
SERVICE="kaiwu-e2e-$$"
LISTEN_PORT=17701
LOG=$ROOT/pipeline.log

cleanup() {
	if [ -f "$FAKE_LOG_DIR/service/pid" ]; then
		pid=$(cat "$FAKE_LOG_DIR/service/pid" 2>/dev/null || true)
		[ -n "$pid" ] && kill -KILL "$pid" 2>/dev/null || true
	fi
	rm -rf "$ROOT"
}
trap cleanup EXIT INT TERM

pass=0
failed=0
ok() {
	pass=$((pass + 1))
	printf 'ok   %s\n' "$1"
}
not_ok() {
	failed=$((failed + 1))
	printf 'FAIL %s\n' "$1"
}
expect_eq() {
	if [ "$2" = "$3" ]; then ok "$1"; else not_ok "$1（期望 [$3] 实际 [$2]）"; fi
}
expect_contains() {
	if grep -qF -- "$3" "$2" 2>/dev/null; then ok "$1"; else not_ok "$1（$2 未包含 $3）"; fi
}
expect_absent() {
	if grep -qF -- "$3" "$2" 2>/dev/null; then not_ok "$1（$2 意外包含 $3）"; else ok "$1"; fi
}

# ------------------------------------------------------------- fake tools
cat >"$ROOT/bin/ssh" <<'SH'
#!/bin/sh
while [ $# -gt 0 ]; do
	case $1 in
	-p | -o | -i | -F) shift 2 ;;
	-*) shift ;;
	*) break ;;
	esac
done
shift # target
exec "$@"
SH

cat >"$ROOT/bin/scp" <<'SH'
#!/bin/sh
while [ $# -gt 0 ]; do
	case $1 in
	-P | -o | -i) shift 2 ;;
	-*) shift ;;
	*) break ;;
	esac
done
src=$1
dst=$2
target=${dst#*:}
mkdir -p "$(dirname "$target")" || exit 1
cp "$src" "$target"
SH

# Fake target systemd that really supervises the built binary: it reads the
# generated unit, starts ExecStart with the service environment and reports the
# same properties the pipeline's stop check reads.
cat >"$ROOT/bin/systemctl" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_LOG_DIR/systemctl.calls"
action=""
unit=""
signal=""
props=""
value_mode=0
prev=""
for a in "$@"; do
	case $a in
	start | stop | restart | enable | is-active | show | kill | reset-failed) action=$a ;;
	--value) value_mode=1 ;;
	*.service) unit=$a ;;
	esac
	[ "$prev" = "-p" ] && props="$props $a"
	[ "$prev" = "-s" ] && signal=$a
	prev=$a
done
unit_dir="$KAIWU_DEPLOY_REMOTE_BASE/systemd"
unit_file="$unit_dir/$KAIWU_DEPLOY_SERVICE_NAME.service"
state_dir="$FAKE_LOG_DIR/service"
pidfile="$state_dir/pid"
running() {
	[ -f "$pidfile" ] || return 1
	spid=$(cat "$pidfile" 2>/dev/null)
	[ -n "$spid" ] || return 1
	kill -0 "$spid" 2>/dev/null
}
stop_running() {
	if running; then
		spid=$(cat "$pidfile")
		kill -TERM "$spid" 2>/dev/null || true
		i=0
		while kill -0 "$spid" 2>/dev/null && [ "$i" -lt 100 ]; do
			sleep 0.1
			i=$((i + 1))
		done
		if kill -0 "$spid" 2>/dev/null; then kill -KILL "$spid" 2>/dev/null || true; fi
	fi
	rm -f "$pidfile" 2>/dev/null || true
}
start_running() {
	mkdir -p "$state_dir"
	envfile=$(sed -n 's/^EnvironmentFile=//p' "$unit_file" 2>/dev/null | head -n 1)
	pathval=$(sed -n 's/^Environment=PATH=//p' "$unit_file" 2>/dev/null | head -n 1)
	execstart=$(sed -n 's/^ExecStart=//p' "$unit_file" 2>/dev/null | head -n 1)
	[ -n "$execstart" ] || exit 1
	if [ -n "$envfile" ] && [ -f "$envfile" ]; then
		set -a
		. "$envfile"
		set +a
	fi
	if [ -n "$pathval" ]; then
		PATH=$pathval
		export PATH
	fi
	nohup "$execstart" >>"$FAKE_LOG_DIR/service.log" 2>&1 &
	echo $! >"$pidfile"
}
case $action in
start | restart)
	stop_running
	start_running
	;;
stop) stop_running ;;
kill)
	if running; then
		spid=$(cat "$pidfile")
		case $signal in
		KILL | SIGKILL) kill -KILL "$spid" 2>/dev/null || true ;;
		*) kill -TERM "$spid" 2>/dev/null || true ;;
		esac
	fi
	;;
is-active)
	if running; then exit 0; else exit 3; fi
	;;
show)
	if running; then
		active=active
		sub=running
		spid=$(cat "$pidfile")
	else
		active=inactive
		sub=dead
		spid=0
	fi
	for p in $props; do
		case $p in
		LoadState) v=loaded ;;
		ActiveState) v=$active ;;
		SubState) v=$sub ;;
		MainPID) v=$spid ;;
		ControlPID) v=0 ;;
		ControlGroup) [ "$spid" = 0 ] && v="" || v="/fake.slice/$unit" ;;
		FragmentPath) [ -f "$unit_file" ] && v=$unit_file || v="" ;;
		*) v="" ;;
		esac
		if [ "$value_mode" = 1 ]; then printf '%s\n' "$v"; else printf '%s=%s\n' "$p" "$v"; fi
	done
	;;
enable | reset-failed) : ;;
esac
exit 0
SH
chmod +x "$ROOT/bin/ssh" "$ROOT/bin/scp" "$ROOT/bin/systemctl"

# ------------------------------------------- clean archive test repository
SRC=$ROOT/src
mkdir -p "$SRC"
rsync -a \
	--exclude '.git' --exclude 'node_modules' --exclude '.kaiwu' \
	--exclude 'apps/web/dist' --exclude 'apps/web/test-results' \
	--exclude 'apps/web/playwright-report' \
	--exclude 'server/internal/webui/dist/assets' --exclude 'server/internal/webui/dist/index.html' \
	"$WORKTREE"/ "$SRC"/
git -C "$SRC" init -q -b main
git -C "$SRC" config user.email e2e@example.invalid
git -C "$SRC" config user.name e2e
git -C "$SRC" add -A
git -C "$SRC" commit -qm "archive pipeline fixture"
SHA=$(git -C "$SRC" rev-parse HEAD)
if [ -f "$SRC/server/internal/webui/dist/index.html" ]; then
	not_ok "测试仓库不应包含已构建的 webui 产物"
else
	ok "测试仓库为干净归档（无 webui 构建产物）"
fi
if [ "$SHA" = "$FIXTURE_SHA" ]; then
	not_ok "候选修订号不应等于版本夹具常量"
else
	ok "候选修订号独立于版本夹具常量（$SHA）"
fi

CFG=$SRC/.kaiwu/local-deployment.conf
mkdir -p "$SRC/.kaiwu"
cat >"$CFG" <<EOF
DEPLOY_BRANCH=main
DEPLOY_SSH_HOST=e2e@fakehost
DEPLOY_SSH_PORT=22
DEPLOY_REMOTE_BASE=$ROOT/remote-base
DEPLOY_DATA_DIR=$ROOT/remote-data
DEPLOY_SERVICE_NAME=$SERVICE
DEPLOY_LISTEN_ADDR=127.0.0.1:$LISTEN_PORT
DEPLOY_HEALTH_URL=http://127.0.0.1:$LISTEN_PORT
DEPLOY_HEALTH_TIMEOUT=120
DEPLOY_LOCK_TIMEOUT=1800
DEPLOY_BROWSER_TESTS=1
DEPLOY_CHROMIUM=/usr/bin/chromium
DEPLOY_BROWSER_PORTS=17711,17712,17713
DEPLOY_VERSION_BROWSER_PORT=17714
DEPLOY_STOP_TIMEOUT=30
DEPLOY_HTTP_CONNECT_TIMEOUT=5
DEPLOY_HTTP_MAX_TIME=15
DEPLOY_DB_NAME=workbench.db
DEPLOY_REMOTE_PATH=
EOF

echo "=== 运行真实归档流水线（候选 $SHA）==="
start=$(date +%s)
PATH="$ROOT/bin:$PATH" timeout 1800 "$SCRIPTS_DIR/deploy.sh" --repo "$SRC" --config "$CFG" >"$LOG" 2>&1
rc=$?
end=$(date +%s)
echo "deploy.sh 退出码=$rc，用时 $((end - start))s"
tail -n 25 "$LOG"
if [ "$rc" -ne 0 ]; then
	echo "===== 流水线失败，完整日志尾部 ====="
	tail -n 80 "$LOG"
	not_ok "真实归档流水线 exit 0"
	printf '\n通过 %d 项，失败 %d 项\n' "$pass" "$failed"
	exit 1
fi
ok "真实归档流水线 exit 0"

STATUS=$ROOT/remote-data/deployment-status.json
CURRENT=$ROOT/remote-base/current
expect_eq "目标状态为 succeeded" "$(grep -o '"status": "[a-z]*"' "$STATUS" | head -n 1 | sed 's/.*"\([a-z]*\)"/\1/')" succeeded
expect_eq "目标激活修订号为候选" "$(grep -o '"revision": "[0-9a-f]*"' "$STATUS" | head -n 1 | sed 's/.*"\([0-9a-f]*\)"/\1/')" "$SHA"
RELEASE=$(readlink "$CURRENT")
if [ -n "$RELEASE" ] && [ -x "$RELEASE/kaiwu-server" ]; then
	ok "current 指向含候选二进制的发布"
else
	not_ok "current 未指向候选发布"
fi
BIN_HASH=$(sha256sum "$RELEASE/kaiwu-server" 2>/dev/null | cut -d' ' -f1)
echo "最终打包二进制 sha256=$BIN_HASH"

TOKEN=$(sed -n 's/^KAIWU_WEB_TOKEN=//p' "$ROOT/remote-base/service.env" | head -n 1)
JAR=$ROOT/cookies
curl -sS --noproxy '*' --connect-timeout 5 --max-time 15 -o /dev/null -c "$JAR" \
	-H "Origin: http://127.0.0.1:$LISTEN_PORT" -H 'X-Kaiwu-Client: web' -H 'Content-Type: application/json' \
	--data "{\"token\":\"$TOKEN\"}" "http://127.0.0.1:$LISTEN_PORT/api/login"
curl -sS --noproxy '*' --connect-timeout 5 --max-time 15 -b "$JAR" -o "$ROOT/version.json" "http://127.0.0.1:$LISTEN_PORT/api/version"
expect_contains "实际入口版本为候选完整 SHA" "$ROOT/version.json" "\"revision\":\"$SHA\""
expect_absent "实际入口版本不是版本夹具常量" "$ROOT/version.json" "$FIXTURE_SHA"

curl -sS --noproxy '*' --connect-timeout 5 --max-time 15 -o "$ROOT/index.html" "http://127.0.0.1:$LISTEN_PORT/"
expect_contains "入口页面已构建（非 web_not_built）" "$ROOT/index.html" '/assets/'
ASSET=$(grep -o '/assets/[A-Za-z0-9._-]*\.js' "$ROOT/index.html" | head -n 1)
curl -sS --noproxy '*' --connect-timeout 5 --max-time 15 -D "$ROOT/asset.hdr" -o "$ROOT/asset.js" "http://127.0.0.1:$LISTEN_PORT$ASSET"
if grep -qi '^content-type:.*javascript' "$ROOT/asset.hdr"; then ok "静态脚本类型正确"; else not_ok "静态脚本类型异常（$(tr -d '\\r' <"$ROOT/asset.hdr" | grep -i content-type)）"; fi
expect_contains "静态脚本内嵌候选完整 SHA" "$ROOT/asset.js" "$SHA"
expect_absent "静态脚本不含版本夹具常量" "$ROOT/asset.js" "$FIXTURE_SHA"
expect_absent "静态脚本不含测试 mock 标记" "$ROOT/asset.js" '__MOCK_'
echo "静态脚本 $ASSET sha256=$(sha256sum "$ROOT/asset.js" | cut -d' ' -f1)"

leftover=$(find "$TMPDIR" -mindepth 1 -maxdepth 1 -name 'kaiwu-verify.*' 2>/dev/null | wc -l)
expect_eq "流水线核对临时目录无残留" "$leftover" 0
others=$(find "$TMPDIR" -mindepth 1 -maxdepth 1 ! -name 'kaiwu-verify.*' 2>/dev/null)
if [ -n "$others" ]; then
	printf 'note: 隔离 TMPDIR 另有浏览器夹具目录（非流水线核对目录）：%s\n' "$(printf '%s ' $others)"
fi

printf '\n通过 %d 项，失败 %d 项\n' "$pass" "$failed"
[ "$failed" -eq 0 ]
