#!/usr/bin/env bash
# Isolated tests for scripts/deploy.sh, scripts/remote-deploy.sh,
# scripts/install-hooks.sh and scripts/lib/common.sh.
#
# A real temporary Git repository and a real temporary SQLite database are used;
# ssh, scp, systemctl, curl, go and npm are replaced by local fakes, so nothing
# leaves the machine, no real systemd unit is touched and no model or network
# credential is involved. HOME and XDG_CONFIG_HOME are redirected into the test
# root so no user unit directory outside the sandbox is ever written.
set -uo pipefail

TESTS_DIR=$(CDPATH= cd -- "$(dirname -- "$(readlink -f -- "$0")")" && pwd)
SCRIPTS_DIR=$(dirname "$TESTS_DIR")

ROOT=$(mktemp -d "${TMPDIR:-/tmp}/kaiwu-deploy-test.XXXXXX")
trap 'rm -rf "$ROOT"' EXIT
export FAKE_LOG_DIR=$ROOT/log
export HOME=$ROOT/home
export XDG_CONFIG_HOME=$ROOT/home/.config
# All deploy-side mktemp activity must land here so leaks are observable.
export TMPDIR=$ROOT/tmp
mkdir -p "$FAKE_LOG_DIR" "$ROOT/bin" "$ROOT/repo" "$HOME" "$TMPDIR"
unset GIT_DIR GIT_INDEX_FILE GIT_WORK_TREE

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
expect_eq() { # name got want
	if [ "$2" = "$3" ]; then ok "$1"; else not_ok "$1（期望 [$3] 实际 [$2]）"; fi
}
expect_exit() { # name got want
	if [ "$2" = "$3" ]; then ok "$1"; else not_ok "$1（退出码 $2，期望 $3）"; fi
}
expect_contains() { # name file needle
	if grep -qF -- "$3" "$2" 2>/dev/null; then ok "$1"; else not_ok "$1（$2 未包含 $3）"; fi
}
expect_absent() { # name file needle
	if grep -qF -- "$3" "$2" 2>/dev/null; then not_ok "$1（$2 意外包含 $3）"; else ok "$1"; fi
}
no_temp_leak() { # name
	local n
	n=$(find "$TMPDIR" -mindepth 1 -maxdepth 1 2>/dev/null | wc -l)
	if [ "$n" -eq 0 ]; then
		ok "$1"
	else
		not_ok "$1（$TMPDIR 残留 $n 项：$(ls -A "$TMPDIR" 2>/dev/null | head -n 3 | tr '\n' ' ')）"
	fi
}
calls() { # file
	[ -f "$1" ] && wc -l <"$1" || echo 0
}

# ---------------------------------------------------------------- fake tools
write_fakes() {
	cat >"$ROOT/bin/ssh" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_LOG_DIR/ssh.calls"
[ "${FAKE_SSH_FAIL:-0}" = 1 ] && exit 255
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
printf '%s\n' "$*" >> "$FAKE_LOG_DIR/scp.calls"
[ "${FAKE_SCP_FAIL:-0}" = 1 ] && exit 1
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

	# Stateful user-systemd fake: show exposes the same properties the real
	# check reads, stop can be deferred into a deactivating window or fail,
	# and a forced migration simulates a candidate that upgraded the schema.
	cat >"$ROOT/bin/systemctl" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_LOG_DIR/systemctl.calls"
[ "${FAKE_SYSTEMCTL_FAIL:-0}" = 1 ] && exit 1
action=""
unit=""
value_mode=0
props=""
prev=""
for a in "$@"; do
	case $a in
	start) action=start ;;
	stop) action=stop ;;
	restart) action=restart ;;
	enable) action=enable ;;
	kill) action=kill ;;
	reset-failed) action=reset-failed ;;
	is-active) action=is-active ;;
	show) action=show ;;
	--value) value_mode=1 ;;
	*.service) unit=$a ;;
	esac
	[ "$prev" = "-p" ] && props="$props $a"
	prev=$a
done
statefile="$FAKE_LOG_DIR/systemctl.${unit:-none}.state"
read_state() { [ -f "$statefile" ] && cat "$statefile"; }
case $action in
start | restart)
	[ -n "$unit" ] && printf 'active 4242\n' >"$statefile"
	if [ "${FAKE_SYSTEMCTL_MIGRATE:-0}" = 1 ] && [ -n "${FAKE_SYSTEMCTL_MIGRATE_DB:-}" ]; then
		node -e 'const{DatabaseSync}=require("node:sqlite");const d=new DatabaseSync(process.argv[1]);d.exec("PRAGMA user_version=3");d.close();' "$FAKE_SYSTEMCTL_MIGRATE_DB" 2>/dev/null
	fi
	;;
stop)
	[ "${FAKE_SYSTEMCTL_STOP_FAIL:-0}" = 1 ] && exit 1
	if [ "${FAKE_SYSTEMCTL_DEFER_STOP:-0}" = 1 ]; then
		printf 'deactivating 4242\n' >"$statefile"
	else
		: >"$statefile"
	fi
	;;
kill | reset-failed | enable) : ;;
is-active)
	st=$(read_state)
	case $st in active*) exit 0 ;; *) exit 3 ;; esac
	;;
show)
	[ "${FAKE_SYSTEMCTL_SHOW_FAIL:-0}" = 1 ] && exit 1
	st=$(read_state)
	case $st in
	active*) active=active; sub=running; pid=4242 ;;
	deactivating*) active=deactivating; sub=stop-sigterm; pid=4242 ;;
	*) active=inactive; sub=dead; pid=0 ;;
	esac
	for p in $props; do
		case $p in
		ActiveState) v=$active ;;
		SubState) v=$sub ;;
		MainPID) v=$pid ;;
		ControlPID) v=0 ;;
		ControlGroup) [ "$pid" = 0 ] && v="" || v="/user.slice/fake.slice/$unit" ;;
		FragmentPath) v=${FAKE_SYSTEMCTL_FRAGMENT:-} ;;
		*) v="" ;;
		esac
		if [ "$value_mode" = 1 ]; then printf '%s\n' "$v"; else printf '%s=%s\n' "$p" "$v"; fi
	done
	;;
esac
exit 0
SH

	cat >"$ROOT/bin/curl" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_LOG_DIR/curl.calls"
has_w=0
out=""
hdr=""
url=""
has_origin=0
has_client=0
code=200
extra_hdr=""
while [ $# -gt 0 ]; do
	case $1 in
	-o)
		out=$2
		shift 2
		;;
	-c)
		: >"$2" 2>/dev/null
		shift 2
		;;
	-b)
		shift 2
		;;
	-H)
		case $2 in
		Origin:*) has_origin=1 ;;
		X-Kaiwu-Client:*) has_client=1 ;;
		esac
		shift 2
		;;
	-D)
		hdr=$2
		shift 2
		;;
	-w)
		has_w=1
		shift 2
		;;
	--data)
		shift 2
		;;
	--noproxy)
		shift 2
		;;
	--connect-timeout | --max-time)
		shift 2
		;;
	-*)
		shift
		;;
	*)
		url=$1
		shift
		;;
	esac
done
rev=${FAKE_CURL_REVISION:-none}
asset_sha=${FAKE_CURL_ASSET_SHA:-$rev}
if [ -n "${FAKE_CURL_SLEEP:-}" ]; then
	case $url in */api/version) sleep "$FAKE_CURL_SLEEP" ;; esac
fi
ctype=""
case $url in
*/healthz)
	body='{"status":"ok"}'
	ctype=application/json
	;;
*/api/login)
	# Mirror the CSRF guard: the real server rejects a login that lacks the
	# same-origin and client headers.
	if [ "$has_origin" = 1 ] && [ "$has_client" = 1 ]; then
		body='{"ok":true}'
		ctype=application/json
	else
		body='{"error":{"code":"csrf"}}'
		ctype=application/json
		code=403
	fi
	;;
*/api/version)
	body='{"revision":"'"$rev"'","shortRevision":"x","summary":"","builtAt":"","deployedAt":"","deployment":{"revision":"'"$rev"'","status":"succeeded","at":"2026-01-01T00:00:00Z","message":"部署成功"}}'
	ctype=application/json
	extra_hdr='Cache-Control: no-store'
	;;
*/assets/*.js | */assets/*.mjs)
	body='const v="'"$asset_sha"'";'
	ctype=application/javascript
	;;
*/assets/*.css)
	body='body{color:#000}'
	ctype=text/css
	;;
*/assets/*)
	body='binary'
	ctype=application/octet-stream
	;;
*/)
	body='<div id="app"></div><script src="/assets/index.js"></script><link rel="stylesheet" href="/assets/index.css">'
	ctype=text/html
	;;
*)
	body=''
	;;
esac
if [ "${FAKE_CURL_HTML_ASSET:-0}" = 1 ]; then
	case $url in
	*/assets/*)
		body='<!doctype html><div id="app"></div>'
		ctype=text/html
		;;
	esac
fi
if [ -n "$hdr" ]; then
	{
		printf 'HTTP/1.1 %s OK\r\n' "$code"
		[ -n "$ctype" ] && printf 'Content-Type: %s\r\n' "$ctype"
		[ -n "$extra_hdr" ] && printf '%s\r\n' "$extra_hdr"
		printf '\r\n'
	} >"$hdr"
fi
[ -n "$out" ] && printf '%s' "$body" >"$out"
if [ "$has_w" = 1 ]; then
	printf '%s' "$code"
elif [ -z "$out" ]; then
	printf '%s' "$body"
fi
exit 0
SH

	cat >"$ROOT/bin/go" <<'SH'
#!/bin/sh
printf '%s|%s\n' "${GOFLAGS:-}" "$*" >> "$FAKE_LOG_DIR/go.calls"
echo "go-output-marker"
case " $* " in
*" test "*) [ "${FAKE_GO_FAIL:-}" = test ] && exit 1 ;;
esac
case " $* " in
*" vet "*) [ "${FAKE_GO_FAIL:-}" = vet ] && exit 1 ;;
esac
prev=""
for arg in "$@"; do
	if [ "$prev" = "-o" ]; then
		mkdir -p "$(dirname "$arg")" || exit 1
		printf 'fake-binary\n' >"$arg"
		chmod +x "$arg"
	fi
	prev=$arg
done
exit 0
SH

	cat >"$ROOT/bin/npm" <<'SH'
#!/bin/sh
printf '%s|%s|%s|%s\n' "${VITE_BUILD_REVISION-}" "${KAIWU_VERSION_TEST_PORT-}" "${KAIWU_VERSION_TEST_REVISION-}" "$*" >> "$FAKE_LOG_DIR/npm.calls"
case " $* " in
*" ci "*)
	if [ "${FAKE_NPM_FAIL:-}" = ci ]; then exit 1; fi
	# Record the extracted archive tree so the harness can prove an
	# uncommitted working-tree file was never carried in.
	if [ -n "${FAKE_TREE_LOG:-}" ]; then
		find . -maxdepth 3 -name 'DIRTY_MARKER*' >>"$FAKE_TREE_LOG" 2>/dev/null
	fi
	;;
*" run build "*)
	if [ "${FAKE_NPM_FAIL:-}" = build ]; then exit 1; fi
	# Simulate Vite inlining import.meta.env.VITE_BUILD_REVISION and the
	# copy-build step; FAKE_NPM_NO_SHA strips the SHA to model a missing
	# injection and FAKE_NPM_MOCK_MARKER leaves a test-only global behind.
	mkdir -p apps/web/dist/assets server/internal/webui/dist/assets
	if [ "${FAKE_NPM_MOCK_MARKER:-0}" = 1 ]; then
		content='const v="'"${VITE_BUILD_REVISION:-}"'";globalThis.__MOCK_FRONTEND_REVISION__="x";'
	elif [ -n "${VITE_BUILD_REVISION:-}" ] && [ "${FAKE_NPM_NO_SHA:-0}" != 1 ]; then
		content='const v="'"$VITE_BUILD_REVISION"'";'
	else
		content='const v="stripped";'
	fi
	printf '%s\n' "$content" >apps/web/dist/assets/index.js
	printf '%s\n' "$content" >server/internal/webui/dist/assets/index.js
	;;
*" test:browser "*)
	if [ "${FAKE_NPM_FAIL:-}" = browser ]; then exit 1; fi
	case " $* " in
	*"--project=theme --project=drawer"*)
		if [ "${FAKE_NPM_FAIL:-}" = corebrowser ]; then exit 1; fi
		;;
	*"--project=project-tools"*)
		if [ "${FAKE_NPM_FAIL:-}" = projecttoolsbrowser ]; then exit 1; fi
		;;
	*"playwright.version.config.ts"*)
		if [ "${FAKE_NPM_FAIL:-}" = versionbrowser ]; then exit 1; fi
		;;
	esac
	# A clean archive without the built page answers 503, so the suites can
	# only run after the front-end build produced webui assets.
	if [ ! -f server/internal/webui/dist/assets/index.js ]; then
		echo "web_not_built"
		exit 1
	fi
	if [ -f apps/web/playwright.config.ts ]; then
		grep -o '127\.0\.0\.1:[0-9]*' apps/web/playwright.config.ts | sort -u >>"$FAKE_LOG_DIR/pwports"
	fi
	;;
esac
exit 0
SH
	chmod +x "$ROOT/bin"/*
}
write_fakes
export PATH=$ROOT/bin:$PATH

# ------------------------------------------------------------- test fixtures
git init -q -b main "$ROOT/repo"
git -C "$ROOT/repo" config user.email test@example.invalid
git -C "$ROOT/repo" config user.name tester
cat >"$ROOT/repo/.gitignore" <<'EOF'
.kaiwu/
EOF
mkdir -p "$ROOT/repo/server" "$ROOT/repo/apps/web/tests" "$ROOT/repo/server/internal/delivery/testdata/webfixture"
printf 'package main\n' >"$ROOT/repo/server/main.go"
# Minimal copies of the files whose hardcoded test ports the pipeline may move
# to an independent range.
printf 'baseURL: "http://127.0.0.1:17331"\n' >"$ROOT/repo/apps/web/playwright.config.ts"
printf 'const port = "127.0.0.1:17332";\n' >"$ROOT/repo/apps/web/tests/server.mjs"
printf 'const port = "127.0.0.1:17441";\n' >"$ROOT/repo/apps/web/tests/interaction.server.mjs"
printf '// listen 127.0.0.1:17332\npackage main\n' >"$ROOT/repo/server/internal/delivery/testdata/webfixture/main.go"
git -C "$ROOT/repo" add -A
git -C "$ROOT/repo" commit -qm "first commit"

REPO=$ROOT/repo
CONFIG=$REPO/.kaiwu/local-deployment.conf
mkdir -p "$REPO/.kaiwu"
write_config() { # file lock-timeout
	cat >"$1" <<EOF
DEPLOY_BRANCH=main
DEPLOY_SSH_HOST=tester@fakehost
DEPLOY_SSH_PORT=22
DEPLOY_REMOTE_BASE=$ROOT/remote-base
DEPLOY_DATA_DIR=$ROOT/remote-data
DEPLOY_SERVICE_NAME=kaiwu-test
DEPLOY_LISTEN_ADDR=127.0.0.1:17331
DEPLOY_HEALTH_URL=http://127.0.0.1:17331
DEPLOY_HEALTH_TIMEOUT=5
DEPLOY_LOCK_TIMEOUT=${2:-5}
DEPLOY_BROWSER_TESTS=0
DEPLOY_REMOTE_PATH=
EOF
}
write_config "$CONFIG"
CONFIG_LOCK=$REPO/.kaiwu/local-lock.conf
write_config "$CONFIG_LOCK" 1
STATUS=$ROOT/remote-data/deployment-status.json
CURRENT=$ROOT/remote-base/current
DB=$ROOT/remote-data/workbench.db
UNIT_LINK=$HOME/.config/systemd/user/kaiwu-test.service

run_cfg() { # config [extra args...]
	local cfg=$1
	shift
	"$SCRIPTS_DIR/deploy.sh" --repo "$REPO" --config "$cfg" "$@"
}
run_deploy() { run_cfg "$CONFIG"; }
run_hook() { # phase ref old new
	printf '%s %s %s\n' "$3" "$4" "$2" |
		GIT_DIR=/nonexistent/repo.git GIT_INDEX_FILE=/nonexistent/index \
			"$REPO/.git/hooks/reference-transaction" "$1"
}
commit_sha() { git -C "$REPO" rev-parse HEAD; }
add_commit() { # message
	printf '%s\n' "$1" >>"$REPO/server/main.go"
	git -C "$REPO" add -A
	# Commit without the installed hook: only the explicit run_hook calls may
	# trigger a deployment.
	git -c core.hooksPath=/dev/null -C "$REPO" commit -qm "$1"
}
fresh_remote() { rm -rf "$ROOT/remote-base" "$ROOT/remote-data"; }
activation_revision() { grep -o '"revision": "[0-9a-f]*"' "$STATUS" 2>/dev/null | sed -n '1p' | sed 's/.*"\([0-9a-f]*\)"/\1/'; }
attempt_revision() { grep -o '"revision": "[0-9a-f]*"' "$STATUS" 2>/dev/null | sed -n '2p' | sed 's/.*"\([0-9a-f]*\)"/\1/'; }
attempt_status() { grep -o '"status": "[a-z]*"' "$STATUS" 2>/dev/null | sed -n '1p' | sed 's/.*"\([a-z]*\)"/\1/'; }
current_release() { readlink "$CURRENT" 2>/dev/null; }
newest_log() { ls -t "$REPO"/.kaiwu/deploy-logs/*.log 2>/dev/null | head -n 1; }
dummy_sha() { printf '%s' "$(printf '%s' "$1" | od -An -tx1 | tr -d ' \n' | cut -c1-40)"; }
make_db() { # path user_version
	node -e 'const{DatabaseSync}=require("node:sqlite");const d=new DatabaseSync(process.argv[1]);d.exec("CREATE TABLE IF NOT EXISTS t(x); PRAGMA user_version="+process.argv[2]);d.close();' "$1" "$2"
}
db_integrity_ok() { # path
	[ "$(node -e 'const{DatabaseSync}=require("node:sqlite");const d=new DatabaseSync(process.argv[1],{readOnly:true});const r=d.prepare("PRAGMA integrity_check").get();d.close();process.stdout.write(String(Object.values(r)[0]));' "$1" 2>/dev/null)" = ok ]
}

# --------------------------------------------------------------- 1. 脚本语法
syntax_ok=1
for file in "$SCRIPTS_DIR/lib/common.sh" "$SCRIPTS_DIR/remote-deploy.sh"; do
	sh -n "$file" || {
		syntax_ok=0
		printf 'sh -n 失败：%s\n' "$file"
	}
done
for file in "$SCRIPTS_DIR/deploy.sh" "$SCRIPTS_DIR/install-hooks.sh" "$TESTS_DIR/deploy_test.sh"; do
	bash -n "$file" || {
		syntax_ok=0
		printf 'bash -n 失败：%s\n' "$file"
	}
done
expect_eq "脚本语法检查（sh -n / bash -n）" "$syntax_ok" 1

# --------------------------------------------------- 2. 配置不进入 Git 跟踪
if git -C "$REPO" check-ignore -q .kaiwu/local-deployment.conf; then
	ok "部署配置位于被忽略的 .kaiwu"
else
	not_ok "部署配置未被 Git 忽略"
fi
if git -C "$REPO" status --porcelain | grep -q 'local-deployment.conf'; then
	not_ok "部署配置出现在 Git 待跟踪列表"
else
	ok "部署配置不在 Git 待跟踪列表"
fi

# ---------------------------------------------------- 3. 安装钩子与拒绝覆盖
mkdir -p "$REPO/.git/hooks"
printf '#!/bin/sh\nexit 0\n' >"$REPO/.git/hooks/reference-transaction"
chmod +x "$REPO/.git/hooks/reference-transaction"
foreign_before=$(cat "$REPO/.git/hooks/reference-transaction")
"$SCRIPTS_DIR/install-hooks.sh" --repo "$REPO" --config "$CONFIG" >/dev/null 2>&1
expect_exit "已有自定义 hook 时拒绝安装" "$?" 1
expect_eq "已有自定义 hook 内容未被覆盖" "$(cat "$REPO/.git/hooks/reference-transaction")" "$foreign_before"
rm -f "$REPO/.git/hooks/reference-transaction"
"$SCRIPTS_DIR/install-hooks.sh" --repo "$REPO" --config "$CONFIG" >/dev/null 2>&1
expect_exit "安装 reference-transaction hook" "$?" 0
expect_contains "hook 带管理标记" "$REPO/.git/hooks/reference-transaction" 'kaiwu-local-deployment-hook'
if [ -x "$REPO/.git/hooks/reference-transaction" ]; then ok "hook 可执行"; else not_ok "hook 不可执行"; fi

# ---------------------------------------- 4. 钩子过滤：阶段、分支与删除
# The hook is invoked with a foreign GIT_DIR/GIT_INDEX_FILE, proving the hook
# clears inherited repository variables before acting on --repo.
sha1=$(commit_sha)
run_hook prepared refs/heads/main 0000000000000000000000000000000000000000 "$sha1" >/dev/null 2>&1
expect_exit "prepared 阶段不触发" "$?" 0
run_hook aborted refs/heads/main 0000000000000000000000000000000000000000 "$sha1" >/dev/null 2>&1
expect_exit "aborted 阶段不触发" "$?" 0
run_hook committed refs/heads/other "$sha1" "$sha1" >/dev/null 2>&1
expect_exit "其他分支不触发" "$?" 0
run_hook committed refs/heads/main "$sha1" 0000000000000000000000000000000000000000 >/dev/null 2>&1
expect_exit "主分支删除不触发" "$?" 0
if [ ! -e "$ROOT/remote-base/state" ]; then ok "过滤后未创建远端状态"; else not_ok "过滤后仍写入了远端状态"; fi

# ------------------------------------------------- 5. 首次成功部署与脏工作区
printf 'uncommitted\n' >"$REPO/server/DIRTY_MARKER.txt"
export FAKE_CURL_REVISION=$sha1
export FAKE_TREE_LOG=$ROOT/tree.log
: >"$FAKE_TREE_LOG"
run_hook committed refs/heads/main 0000000000000000000000000000000000000000 "$sha1" >/dev/null 2>&1
expect_exit "主分支提交触发成功部署" "$?" 0
expect_eq "成功状态记录" "$(attempt_status)" succeeded
expect_eq "成功尝试修订号" "$(attempt_revision)" "$sha1"
expect_eq "激活修订号" "$(activation_revision)" "$sha1"
expect_contains "记录了部署时间" "$STATUS" '"deployedAt": "2'
if [ -n "$(current_release)" ] && [ -x "$(current_release)/kaiwu-server" ]; then
	ok "当前版本指向含可执行程序的发布"
else
	not_ok "当前版本未指向发布"
fi
if [ -s "$FAKE_TREE_LOG" ]; then
	not_ok "未提交的脏文件进入了归档（$(cat "$FAKE_TREE_LOG")）"
else
	ok "未提交的脏文件未进入归档"
fi
stop_line=$(grep -n 'stop kaiwu-test.service' "$FAKE_LOG_DIR/systemctl.calls" | head -n 1 | cut -d: -f1)
start_line=$(grep -n 'start kaiwu-test.service' "$FAKE_LOG_DIR/systemctl.calls" | head -n 1 | cut -d: -f1)
if [ -n "$stop_line" ] && [ -n "$start_line" ] && [ "$stop_line" -lt "$start_line" ]; then
	ok "候选启动前先停自有服务"
else
	not_ok "未先停自有服务（stop=$stop_line start=$start_line）"
fi
expect_absent "服务启动未使用 restart 直接切换" "$FAKE_LOG_DIR/systemctl.calls" 'restart kaiwu-test.service'
expect_contains "候选包上传" "$FAKE_LOG_DIR/scp.calls" ".tgz"
expect_contains "构建注入完整修订号" "$FAKE_LOG_DIR/go.calls" "buildinfo.revision=$sha1"
expect_contains "构建以带引号的赋值注入提交说明" "$FAKE_LOG_DIR/go.calls" "-X 'github.com/SatoriTours/kaiwu/server/internal/buildinfo.summary="
expect_contains "构建导出前端修订号" "$FAKE_LOG_DIR/npm.calls" "$sha1|||--prefix apps/web run build"
rm -f "$REPO/server/DIRTY_MARKER.txt"
release1=$(current_release)
rm -f "$FAKE_LOG_DIR/curl.calls"

# ------------------------------- 6. 全量 Go 失败不替换也不标成功，日志落盘
add_commit "second commit"
sha2=$(commit_sha)
export FAKE_GO_FAIL=test
run_deploy >/dev/null 2>&1
expect_exit "Go 测试失败则部署失败" "$?" 1
unset FAKE_GO_FAIL
expect_eq "失败状态记录" "$(attempt_status)" failed
expect_eq "失败尝试修订号" "$(attempt_revision)" "$sha2"
expect_eq "失败保留原激活修订号" "$(activation_revision)" "$sha1"
expect_eq "失败不替换当前版本" "$(current_release)" "$release1"
expect_contains "失败已写入目标日志" "$ROOT/remote-base/logs/deploy.log" "mark failed $sha2"
log_file=$(newest_log)
expect_contains "本地日志记录真实测试输出" "$log_file.steps" "go-output-marker"
expect_eq "部署日志目录权限为私有" "$(stat -c %a "$REPO/.kaiwu/deploy-logs")" 700
expect_eq "部署日志文件权限为私有" "$(stat -c %a "$log_file")" 600
expect_absent "本地日志不含访问令牌" "$log_file" "KAIWU_WEB_TOKEN="

# ----------------------------------- 7. 实际入口版本不符：无DB改动时回退
add_commit "third commit"
sha3=$(commit_sha)
export FAKE_CURL_REVISION=$sha2
run_deploy >/dev/null 2>&1
expect_exit "入口版本不符则部署失败" "$?" 1
unset FAKE_CURL_REVISION
expect_eq "版本不符状态记录" "$(attempt_status)" failed
expect_eq "版本不符尝试修订号" "$(attempt_revision)" "$sha3"
expect_eq "版本不符已回退当前版本" "$(current_release)" "$release1"
expect_eq "版本不符不改变激活修订号" "$(activation_revision)" "$sha1"
failed_release=$(ls -d "$ROOT"/remote-base/releases/*"$sha3"* 2>/dev/null | head -n 1)
if [ -n "$failed_release" ] && [ -d "$failed_release" ]; then
	ok "失败发布现场保留待查"
else
	not_ok "失败发布现场被删除"
fi

# --------------------------------------------- 8. SSH 不可达：不动已有版本
export FAKE_SSH_FAIL=1
run_deploy >/dev/null 2>&1
expect_exit "目标不可达则部署失败" "$?" 1
unset FAKE_SSH_FAIL
expect_eq "不可达时保留最后一次尝试记录" "$(attempt_status)" failed
expect_eq "不可达时保留尝试修订号" "$(attempt_revision)" "$sha3"
expect_eq "不可达时不改变激活修订号" "$(activation_revision)" "$sha1"
expect_eq "不可达时不改变当前版本" "$(current_release)" "$release1"

# ------------------------------------------------- 9. 同一目标并发部署串行
flock -n "$REPO/.kaiwu/deploy.lock" -c 'sleep 4' &
lock_holder=$!
sleep 0.2
run_cfg "$CONFIG_LOCK" >/dev/null 2>&1
expect_exit "锁被占用时拒绝并发部署" "$?" 1
wait "$lock_holder" 2>/dev/null

# ---------------------------------------- 10. 配置错误在写远端前终止
CFG_BAD=$REPO/.kaiwu/bad.conf
write_config "$CFG_BAD"
printf 'DEPLOY_UNKNOWN=1\n' >>"$CFG_BAD"
"$SCRIPTS_DIR/deploy.sh" --repo "$REPO" --config "$CFG_BAD" >/dev/null 2>&1
expect_exit "未知配置项被拒绝" "$?" 1

# ------------------------------------ 11. 配置值校验（元字符与非法地址）
validation_failures=0
for bad in \
	'DEPLOY_SSH_PORT=22;id' \
	'DEPLOY_REMOTE_BASE=/tmp/x;rm' \
	'DEPLOY_HEALTH_URL=http://127.0.0.1:7331/path' \
	'DEPLOY_LISTEN_ADDR=127.0.0.1:99999' \
	'DEPLOY_BROWSER_PORTS=1,2' \
	'DEPLOY_SERVICE_NAME=kaiwu;evil' \
	'DEPLOY_BRANCH=main$(id)'; do
	cfg=$REPO/.kaiwu/validation.conf
	write_config "$cfg"
	printf '%s\n' "$bad" >>"$cfg"
	before=$(calls "$FAKE_LOG_DIR/ssh.calls")
	"$SCRIPTS_DIR/deploy.sh" --repo "$REPO" --config "$cfg" >/dev/null 2>&1
	code=$?
	after=$(calls "$FAKE_LOG_DIR/ssh.calls")
	if [ "$code" -ne 1 ] || [ "$before" != "$after" ]; then
		validation_failures=$((validation_failures + 1))
		printf '  配置未在本地拒绝：%s（exit=%s ssh 调用 %s→%s）\n' "$bad" "$code" "$before" "$after"
	fi
done
expect_eq "非法配置值在接触目标前被拒绝" "$validation_failures" 0

# ---------------------------------------------------- 12. 状态查询与卸载
"$SCRIPTS_DIR/deploy.sh" --repo "$REPO" --config "$CONFIG" --status >"$ROOT/status.out" 2>/dev/null
expect_exit "状态查询可读" "$?" 0
expect_contains "状态查询返回记录" "$ROOT/status.out" '"status": "failed"'
"$SCRIPTS_DIR/install-hooks.sh" --repo "$REPO" --uninstall >/dev/null 2>&1
expect_exit "卸载自有 hook" "$?" 0
if [ -e "$REPO/.git/hooks/reference-transaction" ]; then
	not_ok "卸载后 hook 仍存在"
else
	ok "卸载后 hook 已移除"
fi
"$SCRIPTS_DIR/install-hooks.sh" --repo "$REPO" --uninstall >/dev/null 2>&1
expect_exit "重复卸载被拒绝" "$?" 1

# ------------------------------- 13. 私有权限、原子切换与 systemd 单元
fresh_remote
shaA=$(commit_sha)
export FAKE_CURL_REVISION=$shaA
run_deploy >/dev/null 2>&1
expect_exit "加固测试：成功部署" "$?" 0
releaseA=$(current_release)
expect_eq "远端发布目录权限私有" "$(stat -c %a "$ROOT/remote-base")" 700
expect_eq "远端数据目录权限私有" "$(stat -c %a "$ROOT/remote-data")" 700
if [ -f "$ROOT/remote-base/service.env" ]; then
	expect_eq "服务环境文件权限私有" "$(stat -c %a "$ROOT/remote-base/service.env")" 600
else
	not_ok "服务环境文件缺失"
fi
expect_eq "部署状态文件权限私有" "$(stat -c %a "$STATUS")" 600
expect_contains "单元显式取消敏感自动交付环境" "$ROOT/remote-base/systemd/kaiwu-test.service" 'UnsetEnvironment=KAIWU_GITHUB_API_TOKEN KAIWU_GITHUB_GIT_TOKEN KAIWU_GITHUB_SSH_KEY'
expect_contains "单元以受控 PATH 启动" "$ROOT/remote-base/systemd/kaiwu-test.service" 'Environment=PATH='
if [ -L "$CURRENT" ]; then ok "current 为符号链接"; else not_ok "current 不是符号链接"; fi
leftover=$(find "$ROOT/remote-base" -maxdepth 1 -name '.current.*tmp' 2>/dev/null | wc -l)
expect_eq "原子切换后无临时链接残留" "$leftover" 0
if grep -q 'mv -T' "$SCRIPTS_DIR/remote-deploy.sh"; then
	ok "切换使用同目录临时链接加 rename"
else
	not_ok "切换未使用原子 rename"
fi

# ------------------------------------------ 14. 同名用户单元与服务归属保护
rm -f "$UNIT_LINK"
printf '[Unit]\nDescription=foreign\n' >"$UNIT_LINK"
foreign_unit=$(cat "$UNIT_LINK")
"$SCRIPTS_DIR/deploy.sh" --repo "$REPO" --config "$CONFIG" >/dev/null 2>&1
expect_exit "同名外来用户单元被拒绝覆盖" "$?" 1
expect_eq "外来用户单元内容未被改写" "$(cat "$UNIT_LINK")" "$foreign_unit"
rm -f "$UNIT_LINK"
run_deploy >/dev/null 2>&1
expect_exit "移除外来单元后可再次部署" "$?" 0
export FAKE_SYSTEMCTL_FRAGMENT=/etc/systemd/user/kaiwu-test.service
run_deploy >/dev/null 2>&1
expect_exit "已有无归属服务时拒绝接管" "$?" 1
unset FAKE_SYSTEMCTL_FRAGMENT

# --------------------------------- 15. HTTP 真实 200、资源类型与前端 SHA
fresh_remote
shaH=$(commit_sha)
export FAKE_CURL_REVISION=$shaH
export FAKE_CURL_HTML_ASSET=1
run_deploy >/dev/null 2>&1
expect_exit "SPA 回退伪装资源时部署失败" "$?" 1
unset FAKE_CURL_HTML_ASSET
expect_contains "资源类型不符有具名说明" "$(newest_log)" "资源"
export FAKE_CURL_ASSET_SHA=$(dummy_sha "wrong")
run_deploy >/dev/null 2>&1
expect_exit "前端构建 SHA 与候选不符时部署失败" "$?" 1
unset FAKE_CURL_ASSET_SHA
export FAKE_CURL_ASSET_SHA=$shaH
run_deploy >/dev/null 2>&1
expect_exit "资源类型与前后端 SHA 一致时部署成功" "$?" 0
expect_eq "加固成功状态记录" "$(attempt_status)" succeeded
unset FAKE_CURL_ASSET_SHA

# ------------------------- 16. SQLite 一次性备份与 schema 感知回退
fresh_remote
shaP=$(commit_sha)
export FAKE_CURL_REVISION=$shaP
run_deploy >/dev/null 2>&1
expect_exit "备份测试：首次成功部署" "$?" 0
releaseP=$(current_release)
make_db "$DB" 2
add_commit "unchanged schema candidate"
shaQ=$(commit_sha)
export FAKE_CURL_REVISION=$(dummy_sha "mismatch")
run_deploy >/dev/null 2>&1
expect_exit "schema 未变时失败仍可在核对后回退" "$?" 1
expect_eq "schema 未变回退到旧版本" "$(current_release)" "$releaseP"
backup_q=$(ls -d "$ROOT"/remote-base/backups/*"$shaQ"* 2>/dev/null | head -n 1)
if [ -n "$backup_q" ] && [ -f "$backup_q/workbench.db" ] && db_integrity_ok "$backup_q/workbench.db"; then
	ok "候选启动前存在可校验的 SQLite 备份"
else
	not_ok "候选启动前缺少可校验的 SQLite 备份"
fi
expect_contains "备份记录方法与来源" "$backup_q/method.txt" "node:sqlite"
expect_contains "备份记录 schema" "$backup_q/db-state-before.txt" "uv=2"
expect_eq "备份目录权限私有" "$(stat -c %a "$backup_q")" 700

add_commit "schema changing candidate"
shaR=$(commit_sha)
export FAKE_CURL_REVISION=$(dummy_sha "mismatch")
export FAKE_SYSTEMCTL_MIGRATE=1
export FAKE_SYSTEMCTL_MIGRATE_DB=$DB
run_deploy >/dev/null 2>&1
expect_exit "候选已升级 schema 后失败不自动回退" "$?" 1
unset FAKE_SYSTEMCTL_MIGRATE FAKE_SYSTEMCTL_MIGRATE_DB
if [ "$(current_release)" != "$releaseP" ] && [ "$(current_release)" != "" ]; then
	ok "schema 变化时保留候选现场而未切回旧程序"
else
	not_ok "schema 变化时错误地切回旧程序"
fi
if "$ROOT/bin/systemctl" --user is-active --quiet kaiwu-test.service; then
	not_ok "schema 变化后候选服务未停止"
else
	ok "schema 变化后候选服务已停止"
fi
expect_eq "schema 变化后状态为 failed" "$(attempt_status)" failed
expect_contains "schema 变化提示需人工核对" "$(newest_log)" "未自动回退"
backup_r=$(ls -d "$ROOT"/remote-base/backups/*"$shaR"* 2>/dev/null | head -n 1)
if [ -n "$backup_r" ] && [ -f "$backup_r/workbench.db" ]; then
	ok "schema 变化时备份保留"
else
	not_ok "schema 变化时备份丢失"
fi
rm -f "$DB" "$DB-wal" "$DB-shm"

# 16b/16c. 备用备份路径：sqlite3 .backup 与已停写快照
make_db "$DB" 2
add_commit "sqlite3 fallback"
shaS=$(commit_sha)
export FAKE_CURL_REVISION=$shaS
CFG_NONODE=$REPO/.kaiwu/no-node.conf
write_config "$CFG_NONODE"
printf 'DEPLOY_REMOTE_NODE=/nonexistent/node\n' >>"$CFG_NONODE"
run_cfg "$CFG_NONODE" >/dev/null 2>&1
expect_exit "node 不可用时 sqlite3 .backup 仍可部署" "$?" 0
backup_s=$(ls -d "$ROOT"/remote-base/backups/*"$shaS"* 2>/dev/null | head -n 1)
expect_contains "sqlite3 .backup 方法已记录" "$backup_s/method.txt" "sqlite3"
if [ -f "$backup_s/workbench.db" ] && db_integrity_ok "$backup_s/workbench.db"; then
	ok "sqlite3 备份可校验"
else
	not_ok "sqlite3 备份不可校验"
fi

cat >"$ROOT/bin/sqlite3" <<'SH'
#!/bin/sh
exit 1
SH
chmod +x "$ROOT/bin/sqlite3"
add_commit "snapshot fallback"
shaT=$(commit_sha)
export FAKE_CURL_REVISION=$shaT
run_cfg "$CFG_NONODE" >/dev/null 2>&1
expect_exit "仅剩停写快照时仍完成部署" "$?" 0
backup_t=$(ls -d "$ROOT"/remote-base/backups/*"$shaT"* 2>/dev/null | head -n 1)
expect_contains "停写快照方法已记录" "$backup_t/method.txt" "stopped-copy"
if [ -f "$backup_t/workbench.db" ]; then
	ok "停写快照保留数据库副本"
else
	not_ok "停写快照缺少数据库副本"
fi
rm -f "$ROOT/bin/sqlite3" "$DB" "$DB-wal" "$DB-shm"

# ----------------------------- 17. 浏览器检查必需、端口可配置且不接管
fresh_remote
shaB=$(commit_sha)
export FAKE_CURL_REVISION=$shaB
CFG_NOBROWSER=$REPO/.kaiwu/no-browser.conf
write_config "$CFG_NOBROWSER"
sed -i '/^DEPLOY_BROWSER_TESTS=/d' "$CFG_NOBROWSER"
printf 'DEPLOY_CHROMIUM=/nonexistent/chromium\n' >>"$CFG_NOBROWSER"
before=$(calls "$FAKE_LOG_DIR/npm.calls")
run_cfg "$CFG_NOBROWSER" >/dev/null 2>&1
expect_exit "缺少浏览器时必需检查阻塞部署" "$?" 1
expect_contains "缺少浏览器有具名说明" "$(newest_log)" "Chromium"
after=$(calls "$FAKE_LOG_DIR/npm.calls")
expect_eq "缺少浏览器时不执行前端与浏览器步骤" "$before" "$after"

CFG_BROWSER=$REPO/.kaiwu/browser.conf
write_config "$CFG_BROWSER"
sed -i 's/^DEPLOY_BROWSER_TESTS=0$/DEPLOY_BROWSER_TESTS=1/' "$CFG_BROWSER"
printf 'DEPLOY_CHROMIUM=/usr/bin/chromium\nDEPLOY_BROWSER_PORTS=19331,19332,19441\n' >>"$CFG_BROWSER"
: >"$FAKE_LOG_DIR/pwports"
run_cfg "$CFG_BROWSER" >/dev/null 2>&1
expect_exit "配置独立端口后浏览器门禁通过" "$?" 0
expect_contains "浏览器端口已替换为独立端口" "$FAKE_LOG_DIR/pwports" "127.0.0.1:19331"
if grep -q 'test:browser' "$FAKE_LOG_DIR/npm.calls"; then ok "浏览器套件实际执行"; else not_ok "浏览器套件未执行"; fi
interaction_group='V2 native|V2 input'
expect_contains "原有交互用例在独立服务执行" "$FAKE_LOG_DIR/npm.calls" "--config playwright.interaction.config.ts --grep-invert $interaction_group"
expect_contains "V2交互用例在全新服务执行且无跳过" "$FAKE_LOG_DIR/npm.calls" "--config playwright.interaction.config.ts --grep $interaction_group"
result_group='result history UI:|result review opens|a real task HTTP preview|late preview responses|passive preview reply|opaque preview pointer:'
expect_contains "预览与历史用例在独立生命周期执行" "$FAKE_LOG_DIR/npm.calls" "--project=existing --grep $result_group"
expect_contains "其余用例使用互补分组而非跳过" "$FAKE_LOG_DIR/npm.calls" "--grep-invert $result_group"
expect_contains "原主流程显式排除会污染初始空库的工具套件" "$FAKE_LOG_DIR/npm.calls" "--project=existing --project=delivery --project=ui-consistency --grep-invert $result_group"
expect_contains "主题与抽屉独立生命周期不继承业务登录与驻留会话" "$FAKE_LOG_DIR/npm.calls" "test:browser -- --project=theme --project=drawer"
expect_contains "工具套件在独立新服务生命周期执行" "$FAKE_LOG_DIR/npm.calls" "test:browser -- --project=project-tools"
expect_eq "浏览器门禁成功后激活候选" "$(activation_revision)" "$shaB"

CFG_BUSY=$REPO/.kaiwu/busy.conf
write_config "$CFG_BUSY"
sed -i 's/^DEPLOY_BROWSER_TESTS=0$/DEPLOY_BROWSER_TESTS=1/' "$CFG_BUSY"
printf 'DEPLOY_CHROMIUM=/usr/bin/chromium\nDEPLOY_BROWSER_PORTS=19451,19332,19441\n' >>"$CFG_BUSY"
python3 -m http.server 19451 --bind 127.0.0.1 >/dev/null 2>&1 &
busy_pid=$!
sleep 0.6
run_cfg "$CFG_BUSY" >/dev/null 2>&1
expect_exit "浏览器端口被占用时具名失败而不接管" "$?" 1
expect_contains "端口占用有具名说明" "$(newest_log)" "占用"
kill "$busy_pid" 2>/dev/null
wait "$busy_pid" 2>/dev/null

# ------------------------------------------ 18. install-hooks 路径与配置保护
git -C "$REPO" config --unset core.hooksPath 2>/dev/null || true
git -C "$REPO" config core.hooksPath "$ROOT/outside-hooks"
"$SCRIPTS_DIR/install-hooks.sh" --repo "$REPO" --config "$CONFIG" >/dev/null 2>&1
expect_exit "core.hooksPath 指向仓库外时拒绝安装" "$?" 1
if [ ! -e "$ROOT/outside-hooks/reference-transaction" ]; then
	ok "未写入仓库外的共享 hooks 目录"
else
	not_ok "写入了仓库外的共享 hooks 目录"
fi
git -C "$REPO" config core.hooksPath /dev/null
"$SCRIPTS_DIR/install-hooks.sh" --repo "$REPO" --config "$CONFIG" >/dev/null 2>&1
expect_exit "core.hooksPath=/dev/null 时拒绝假称安装成功" "$?" 1
git -C "$REPO" config core.hooksPath .githooks
"$SCRIPTS_DIR/install-hooks.sh" --repo "$REPO" --config "$CONFIG" >/dev/null 2>&1
expect_exit "仓库内显式 hooksPath 允许安装" "$?" 0
if [ -f "$REPO/.githooks/reference-transaction" ]; then ok "安装到仓库内 hooksPath"; else not_ok "未安装到仓库内 hooksPath"; fi
git -C "$REPO" config --unset core.hooksPath
rm -rf "$REPO/.githooks"

printf 'DEPLOY_BRANCH=main\n' >"$REPO/tracked.conf"
git -C "$REPO" add tracked.conf
"$SCRIPTS_DIR/install-hooks.sh" --repo "$REPO" --config "$REPO/tracked.conf" >/dev/null 2>&1
expect_exit "已被跟踪的配置被拒绝" "$?" 1
git -C "$REPO" rm -q --cached tracked.conf 2>/dev/null || true
rm -f "$REPO/tracked.conf"
printf 'DEPLOY_BRANCH=main\n' >"$REPO/loose.conf"
"$SCRIPTS_DIR/install-hooks.sh" --repo "$REPO" --config "$REPO/loose.conf" >/dev/null 2>&1
expect_exit "仓库内未忽略的配置被拒绝" "$?" 1
rm -f "$REPO/loose.conf"

# ------------------------------- 20. 停止证据：deactivating 与 stop 失败
fresh_remote
shaX=$(commit_sha)
CFG_STOP=$REPO/.kaiwu/stop.conf
write_config "$CFG_STOP"
printf 'DEPLOY_STOP_TIMEOUT=2\n' >>"$CFG_STOP"
export FAKE_CURL_REVISION=$shaX
export FAKE_SYSTEMCTL_DEFER_STOP=1
run_cfg "$CFG_STOP" >/dev/null 2>&1
expect_exit "deactivating 且进程存活时拒绝继续" "$?" 1
unset FAKE_SYSTEMCTL_DEFER_STOP
if [ -d "$ROOT/remote-base/backups" ] && [ -n "$(ls -A "$ROOT/remote-base/backups" 2>/dev/null)" ]; then
	not_ok "未证明停止却已创建备份"
else
	ok "未证明停止时不备份"
fi
expect_contains "停止失败有具名说明" "$(newest_log)" "停止"
export FAKE_SYSTEMCTL_STOP_FAIL=1
run_cfg "$CFG_STOP" >/dev/null 2>&1
expect_exit "systemctl stop 非零即失败" "$?" 1
unset FAKE_SYSTEMCTL_STOP_FAIL
if [ -d "$ROOT/remote-base/backups" ] && [ -n "$(ls -A "$ROOT/remote-base/backups" 2>/dev/null)" ]; then
	not_ok "stop 非零却已创建备份"
else
	ok "stop 非零时不备份"
fi
export FAKE_SYSTEMCTL_SHOW_FAIL=1
run_cfg "$CFG_STOP" >/dev/null 2>&1
expect_exit "停止状态未知时失败" "$?" 1
unset FAKE_SYSTEMCTL_SHOW_FAIL
no_temp_leak "停止失败后无临时目录残留"

# ------------------------- 21. 私有临时目录：正常与信号路径均清理
sig=$ROOT/sig
mkdir -p "$sig" "$ROOT/remote-base/incoming"
cat "$SCRIPTS_DIR/lib/common.sh" "$SCRIPTS_DIR/remote-deploy.sh" >"$sig/body.sh"
shaSig=$(commit_sha)
printf '#!/bin/sh\nexit 0\n' >"$sig/kaiwu-server"
chmod +x "$sig/kaiwu-server"
tar -czf "$ROOT/remote-base/incoming/$shaSig.tgz" -C "$sig" kaiwu-server
printf '%s  %s\n' "$(sha256sum "$ROOT/remote-base/incoming/$shaSig.tgz" | cut -d' ' -f1)" "$shaSig.tgz" >"$ROOT/remote-base/incoming/$shaSig.sha256"
export FAKE_CURL_REVISION=$shaSig
export FAKE_CURL_SLEEP=3
env KAIWU_DEPLOY_REMOTE_BASE="$ROOT/remote-base" KAIWU_DEPLOY_DATA_DIR="$ROOT/remote-data" \
	KAIWU_DEPLOY_SERVICE_NAME=kaiwu-test KAIWU_DEPLOY_LISTEN_ADDR=127.0.0.1:17331 \
	KAIWU_DEPLOY_HEALTH_URL=http://127.0.0.1:17331 KAIWU_DEPLOY_STOP_TIMEOUT=2 \
	sh "$sig/body.sh" apply "$shaSig" >/dev/null 2>&1 &
sigpid=$!
found=""
for i in $(seq 1 80); do
	found=$(find "$TMPDIR" -mindepth 1 -maxdepth 1 -name 'kaiwu-verify.*' 2>/dev/null | head -n 1)
	if [ -n "$found" ] && [ -f "$found/cookies" ]; then break; fi
	sleep 0.1
done
if [ -n "$found" ] && [ -f "$found/cookies" ]; then
	ok "核对期间真实创建了含 cookie 的私有目录"
else
	not_ok "未观察到核对私有目录与 cookie"
fi
kill -TERM "$sigpid" 2>/dev/null
wait "$sigpid" 2>/dev/null
unset FAKE_CURL_SLEEP FAKE_CURL_REVISION
no_temp_leak "信号终止后清理本次私有目录"
no_temp_leak "成功后无临时目录残留"

# -------------------- 22. 版本浏览器门禁：真实执行且失败不切换
fresh_remote
shaV0=$(commit_sha)
CFG_VER=$REPO/.kaiwu/version.conf
write_config "$CFG_VER"
sed -i 's/^DEPLOY_BROWSER_TESTS=0$/DEPLOY_BROWSER_TESTS=1/' "$CFG_VER"
printf 'DEPLOY_CHROMIUM=/usr/bin/chromium\nDEPLOY_BROWSER_PORTS=19331,19332,19441\nDEPLOY_VERSION_BROWSER_PORT=19451\n' >>"$CFG_VER"
export FAKE_CURL_REVISION=$shaV0
: >"$FAKE_LOG_DIR/pwports"
run_cfg "$CFG_VER" >/dev/null 2>&1
expect_exit "版本浏览器套件纳入门禁后可部署" "$?" 0
if grep -q 'playwright.version.config.ts' "$FAKE_LOG_DIR/npm.calls"; then
	ok "门禁真实调用版本浏览器套件"
else
	not_ok "门禁未调用版本浏览器套件"
fi
if grep -q "|19451|$shaV0|--prefix apps/web run test:browser -- --config playwright.version.config.ts" "$FAKE_LOG_DIR/npm.calls"; then
	ok "版本浏览器端口通过环境变量传入"
else
	not_ok "版本浏览器端口未传入"
fi
releaseV=$(current_release)
add_commit "version browser candidate"
shaV=$(commit_sha)
export FAKE_NPM_FAIL=versionbrowser
run_cfg "$CFG_VER" >/dev/null 2>&1
expect_exit "版本浏览器失败则部署失败" "$?" 1
unset FAKE_NPM_FAIL
expect_eq "版本浏览器失败不切换当前版本" "$(current_release)" "$releaseV"
expect_eq "版本浏览器失败保留激活修订号" "$(activation_revision)" "$shaV0"
expect_eq "版本浏览器失败记录尝试" "$(attempt_revision)" "$shaV"
expect_absent "版本浏览器失败不上传候选" "$FAKE_LOG_DIR/scp.calls" "$shaV"
export FAKE_NPM_FAIL=projecttoolsbrowser
run_cfg "$CFG_VER" >/dev/null 2>&1
expect_exit "独立工具浏览器失败则部署失败" "$?" 1
unset FAKE_NPM_FAIL
expect_eq "工具浏览器失败不切换当前版本" "$(current_release)" "$releaseV"
expect_eq "工具浏览器失败保留激活修订号" "$(activation_revision)" "$shaV0"
expect_absent "工具浏览器失败不上传候选" "$FAKE_LOG_DIR/scp.calls" "$shaV"
export FAKE_NPM_FAIL=corebrowser
run_cfg "$CFG_VER" >/dev/null 2>&1
expect_exit "独立主题抽屉失败则部署失败" "$?" 1
unset FAKE_NPM_FAIL
expect_eq "主题抽屉失败不切换当前版本" "$(current_release)" "$releaseV"
expect_eq "主题抽屉失败保留激活修订号" "$(activation_revision)" "$shaV0"
expect_absent "主题抽屉失败不上传候选" "$FAKE_LOG_DIR/scp.calls" "$shaV"
python3 -m http.server 19461 --bind 127.0.0.1 >/dev/null 2>&1 &
vbusy=$!
sleep 0.6
CFG_VERBUSY=$REPO/.kaiwu/version-busy.conf
cp "$CFG_VER" "$CFG_VERBUSY"
sed -i 's/^DEPLOY_VERSION_BROWSER_PORT=19451$/DEPLOY_VERSION_BROWSER_PORT=19461/' "$CFG_VERBUSY"
run_cfg "$CFG_VERBUSY" >/dev/null 2>&1
expect_exit "版本浏览器端口被占用时具名失败" "$?" 1
expect_contains "版本端口占用有具名说明" "$(newest_log)" "占用"
kill "$vbusy" 2>/dev/null
wait "$vbusy" 2>/dev/null
no_temp_leak "版本门禁后无临时目录残留"

# ---------------- 23. 前端必须无条件内嵌完整候选 SHA
fresh_remote
shaM0=$(commit_sha)
export FAKE_CURL_REVISION=$shaM0
run_deploy >/dev/null 2>&1
expect_exit "M3 首次成功部署" "$?" 0
releaseM=$(current_release)
add_commit "missing frontend sha"
shaM=$(commit_sha)
export FAKE_NPM_NO_SHA=1
run_deploy >/dev/null 2>&1
expect_exit "构建产物缺完整 SHA 时失败" "$?" 1
unset FAKE_NPM_NO_SHA
expect_eq "缺 SHA 不切换当前版本" "$(current_release)" "$releaseM"
expect_absent "缺 SHA 不上传候选" "$FAKE_LOG_DIR/scp.calls" "$shaM"
expect_contains "缺 SHA 有具名说明" "$(newest_log)" "VITE_BUILD_REVISION"
export KAIWU_DEPLOY_FRONTEND_SHA_REQUIRED=0
export FAKE_CURL_ASSET_SHA=$(dummy_sha "bypass")
run_deploy >/dev/null 2>&1
expect_exit "远端前端 SHA 核对不可用参数绕过" "$?" 1
unset KAIWU_DEPLOY_FRONTEND_SHA_REQUIRED FAKE_CURL_ASSET_SHA
expect_eq "参数绕过失败后仍不切换" "$(current_release)" "$releaseM"
no_temp_leak "前端 SHA 失败后无临时目录残留"

# ------------------- 24. 归档流水线：GOFLAGS、页面先就绪、mock 扫描
fresh_remote
shaN=$(commit_sha)
export FAKE_CURL_REVISION=$shaN
CFG_ARCH=$REPO/.kaiwu/archive.conf
write_config "$CFG_ARCH"
sed -i 's/^DEPLOY_BROWSER_TESTS=0$/DEPLOY_BROWSER_TESTS=1/' "$CFG_ARCH"
printf 'DEPLOY_CHROMIUM=/usr/bin/chromium\nDEPLOY_BROWSER_PORTS=19531,19532,19541\nDEPLOY_VERSION_BROWSER_PORT=19551\n' >>"$CFG_ARCH"
: >"$FAKE_LOG_DIR/npm.calls"
: >"$FAKE_LOG_DIR/go.calls"
run_cfg "$CFG_ARCH" >/dev/null 2>&1
expect_exit "归档流水线（GOFLAGS 与页面先就绪）成功" "$?" 0
missing_vcs=0
while IFS= read -r line; do
	case $line in *'-buildvcs=false'*) ;; *) missing_vcs=$((missing_vcs + 1)) ;; esac
done <"$FAKE_LOG_DIR/go.calls"
expect_eq "N1 所有 go 调用都带 -buildvcs=false" "$missing_vcs" 0
build_line=$(grep -n '|--prefix apps/web run build' "$FAKE_LOG_DIR/npm.calls" | head -n 1 | cut -d: -f1)
browser_line=$(grep -n '|--prefix apps/web run test:browser' "$FAKE_LOG_DIR/npm.calls" | head -n 1 | cut -d: -f1)
if [ -n "$build_line" ] && [ -n "$browser_line" ] && [ "$build_line" -lt "$browser_line" ]; then
	ok "N2 前端构建先于浏览器门禁"
else
	not_ok "N2 前端构建未先于浏览器门禁（build=$build_line browser=$browser_line）"
fi
if grep -q "|$shaN|--prefix apps/web run test:browser -- --config playwright.version.config.ts" "$FAKE_LOG_DIR/npm.calls"; then
	ok "N2 版本套件收到候选修订号"
else
	not_ok "N2 版本套件未收到候选修订号"
fi
release_arch=$(current_release)
add_commit "mock marker candidate"
shaMock=$(commit_sha)
export FAKE_CURL_REVISION=$shaMock
export FAKE_NPM_MOCK_MARKER=1
run_cfg "$CFG_ARCH" >/dev/null 2>&1
expect_exit "构建产物含测试标记时失败" "$?" 1
unset FAKE_NPM_MOCK_MARKER
expect_contains "mock 扫描有具名说明" "$(newest_log)" "测试标记"
expect_absent "mock 产物不上传候选" "$FAKE_LOG_DIR/scp.calls" "$shaMock"
expect_eq "mock 失败不切换当前版本" "$(current_release)" "$release_arch"
no_temp_leak "归档流水线后无临时目录残留"

# ------------------------------------------- 19. 全部 curl 调用都有超时
missing=0
while IFS= read -r line; do
	case $line in *--connect-timeout*) ;; *) missing=$((missing + 1)) ;; esac
	case $line in *--max-time*) ;; *) missing=$((missing + 1)) ;; esac
done <"$FAKE_LOG_DIR/curl.calls"
expect_eq "所有 curl 调用都带连接与总超时" "$missing" 0

printf '\n通过 %d 项，失败 %d 项\n' "$pass" "$failed"
[ "$failed" -eq 0 ]
