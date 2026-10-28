#!/usr/bin/env bash
# Kaiwu local deployment entry point.
#
#   scripts/deploy.sh [--deploy] [--config FILE] [--repo DIR]
#   scripts/deploy.sh --hook <phase> --config FILE --repo DIR   (Git hook only)
#   scripts/deploy.sh --status [--config FILE] [--repo DIR]
#
# The pipeline only ever deploys a committed branch revision: it archives that
# exact revision, runs the full local test suite in the archive, builds the
# binary with an embedded revision and asks the target to activate it. A failed
# candidate never replaces the running release; the target records the failed
# attempt instead.
set -euo pipefail
# Logs and build/verification scratch stay private.
umask 077
# The pipeline builds from a Git archive without any .git directory, so every
# Go build (the pipeline's own and the browser fixtures that spawn `go build`)
# must skip VCS stamping. The running revision always comes from the explicit
# ldflags injection, never from the checkout.
case ${GOFLAGS:-} in
*-buildvcs=*) ;;
*) export GOFLAGS="${GOFLAGS:+$GOFLAGS }-buildvcs=false" ;;
esac

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$(readlink -f -- "$0")")" && pwd)
# shellcheck source=lib/common.sh
. "$SCRIPT_DIR/lib/common.sh"

# The Git reference-transaction hook fires for every repository, so a browser
# suite in one lifecycle can exhaust the product's eight resident sessions. The
# documented regression splits it into two independent fresh service
# lifecycles: preview/history cases first, then the remaining scenarios. Each
# process keeps the product's eight-resident-session limit; no test is skipped.
HISTORY_GREP='result history UI:|result review opens|a real task HTTP preview|late preview responses'
# New V2 requests need their own fresh eight-session budget too. Complementary
# selectors retain every interaction test and all existing assertions.
INTERACTION_V2_GREP='V2 native|V2 input'
BUILDINFO_PKG='github.com/SatoriTours/kaiwu/server/internal/buildinfo'
# Documented default browser ports; DEPLOY_BROWSER_PORTS may move the whole
# suite to a free range so another task on the same machine cannot conflict.
DEFAULT_BROWSER_PORTS='17331 17332 17441'

mode=deploy
hook_phase=""
repo=""
config=""
usage() {
	cat <<'EOF'
用法：
  scripts/deploy.sh [--config 文件] [--repo 仓库]
  scripts/deploy.sh --status [--config 文件] [--repo 仓库]
  scripts/deploy.sh --hook <prepared|committed|aborted> --config 文件 --repo 仓库

只在显式本地配置存在时工作；不读取全局 Git 配置，不提交、合并或推送。
EOF
}

while [ $# -gt 0 ]; do
	case $1 in
	--hook)
		[ $# -ge 2 ] || kaiwu_die "--hook 需要事务阶段参数"
		mode=hook
		hook_phase=$2
		shift 2
		;;
	--deploy)
		mode=deploy
		shift
		;;
	--status)
		mode=status
		shift
		;;
	--config)
		[ $# -ge 2 ] || kaiwu_die "--config 需要文件参数"
		config=$2
		shift 2
		;;
	--repo)
		[ $# -ge 2 ] || kaiwu_die "--repo 需要目录参数"
		repo=$2
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*) kaiwu_die "未知参数：$1" ;;
	esac
done

# A hook inherits Git's environment for the repository being updated. Clear the
# repository-selecting variables so every later command targets the explicit
# --repo instead of a stale or foreign checkout.
unset GIT_DIR GIT_INDEX_FILE GIT_WORK_TREE GIT_COMMON_DIR GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES

[ -n "$repo" ] || repo=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
repo=$(readlink -f -- "$repo")
[ -d "$repo/.git" ] || [ -f "$repo/.git" ] || kaiwu_die "不是 Git 仓库：$repo"
[ -n "$config" ] || config=$repo/.kaiwu/local-deployment.conf

gitq() { git -c core.hooksPath=/dev/null "$@"; }

DEPLOY_BRANCH=''
DEPLOY_SSH_HOST=''
DEPLOY_SSH_PORT=22
DEPLOY_REMOTE_BASE=''
DEPLOY_DATA_DIR=''
DEPLOY_SERVICE_NAME=kaiwu-server
DEPLOY_LISTEN_ADDR=''
DEPLOY_HEALTH_URL=''
DEPLOY_HEALTH_TIMEOUT=60
DEPLOY_LOCK_TIMEOUT=600
DEPLOY_BROWSER_TESTS=1
DEPLOY_BROWSER_PORTS=17331,17332,17441
DEPLOY_CHROMIUM=''
DEPLOY_HTTP_CONNECT_TIMEOUT=5
DEPLOY_HTTP_MAX_TIME=15
DEPLOY_DB_NAME=workbench.db
DEPLOY_REMOTE_NODE=''
DEPLOY_REMOTE_PATH=''
DEPLOY_STOP_TIMEOUT=20
DEPLOY_VERSION_BROWSER_PORT=17551

BROWSER_ENABLED=0

# kaiwu_read_config only accepts a fixed KEY=VALUE vocabulary. Every value is
# validated against a strict pattern, so a shell metacharacter can never reach
# the ssh command line.
kaiwu_read_config() {
	local file=$1 key value line p
	[ -f "$file" ] || kaiwu_die "部署配置不存在：$file（可从 scripts/deployment.conf.example 复制）"
	while IFS= read -r line || [ -n "$line" ]; do
		case $line in '' | '#'*) continue ;; esac
		case $line in
		*=*) ;;
		*) kaiwu_die "配置行缺少 = 号：$file" ;;
		esac
		key=${line%%=*}
		value=${line#*=}
		case $value in
		*[[:space:]]*) kaiwu_die "配置值不能包含空白：$key" ;;
		esac
		case $key in
		DEPLOY_BRANCH) DEPLOY_BRANCH=$value ;;
		DEPLOY_SSH_HOST) DEPLOY_SSH_HOST=$value ;;
		DEPLOY_SSH_PORT) DEPLOY_SSH_PORT=$value ;;
		DEPLOY_REMOTE_BASE) DEPLOY_REMOTE_BASE=$value ;;
		DEPLOY_DATA_DIR) DEPLOY_DATA_DIR=$value ;;
		DEPLOY_SERVICE_NAME) DEPLOY_SERVICE_NAME=$value ;;
		DEPLOY_LISTEN_ADDR) DEPLOY_LISTEN_ADDR=$value ;;
		DEPLOY_HEALTH_URL) DEPLOY_HEALTH_URL=$value ;;
		DEPLOY_HEALTH_TIMEOUT) DEPLOY_HEALTH_TIMEOUT=$value ;;
		DEPLOY_LOCK_TIMEOUT) DEPLOY_LOCK_TIMEOUT=$value ;;
		DEPLOY_BROWSER_TESTS) DEPLOY_BROWSER_TESTS=$value ;;
		DEPLOY_BROWSER_PORTS) DEPLOY_BROWSER_PORTS=$value ;;
		DEPLOY_CHROMIUM) DEPLOY_CHROMIUM=$value ;;
		DEPLOY_HTTP_CONNECT_TIMEOUT) DEPLOY_HTTP_CONNECT_TIMEOUT=$value ;;
		DEPLOY_HTTP_MAX_TIME) DEPLOY_HTTP_MAX_TIME=$value ;;
		DEPLOY_DB_NAME) DEPLOY_DB_NAME=$value ;;
		DEPLOY_REMOTE_NODE) DEPLOY_REMOTE_NODE=$value ;;
		DEPLOY_REMOTE_PATH) DEPLOY_REMOTE_PATH=$value ;;
		DEPLOY_STOP_TIMEOUT) DEPLOY_STOP_TIMEOUT=$value ;;
		DEPLOY_VERSION_BROWSER_PORT) DEPLOY_VERSION_BROWSER_PORT=$value ;;
		*) kaiwu_die "配置包含未知项：$key" ;;
		esac
	done <"$file"

	[ -n "$DEPLOY_BRANCH" ] || kaiwu_die "配置缺少 DEPLOY_BRANCH"
	case $DEPLOY_BRANCH in -* | */*..* | *[!A-Za-z0-9._/-]*) kaiwu_die "DEPLOY_BRANCH 无效" ;; esac
	case $DEPLOY_SSH_HOST in '' | *[!A-Za-z0-9._@-]*) kaiwu_die "DEPLOY_SSH_HOST 无效" ;; esac
	kaiwu_port "$DEPLOY_SSH_PORT" || kaiwu_die "DEPLOY_SSH_PORT 无效"
	kaiwu_safe_path "$DEPLOY_REMOTE_BASE" || kaiwu_die "DEPLOY_REMOTE_BASE 必须是不含元字符的绝对路径"
	kaiwu_safe_path "$DEPLOY_DATA_DIR" || kaiwu_die "DEPLOY_DATA_DIR 必须是不含元字符的绝对路径"
	case $DEPLOY_SERVICE_NAME in '' | -* | *[!A-Za-z0-9._-]*) kaiwu_die "DEPLOY_SERVICE_NAME 无效" ;; esac
	kaiwu_hostport "$DEPLOY_LISTEN_ADDR" || kaiwu_die "DEPLOY_LISTEN_ADDR 应为数字地址加端口"
	kaiwu_health_url "$DEPLOY_HEALTH_URL" || kaiwu_die "DEPLOY_HEALTH_URL 应为 http://地址:端口（无路径与参数）"
	case $DEPLOY_BROWSER_TESTS in 0 | 1) ;; *) kaiwu_die "DEPLOY_BROWSER_TESTS 只能是 0 或 1" ;; esac
	if [ -n "$DEPLOY_CHROMIUM" ]; then kaiwu_safe_path "$DEPLOY_CHROMIUM" || kaiwu_die "DEPLOY_CHROMIUM 必须是绝对路径"; fi
	if [ -n "$DEPLOY_REMOTE_NODE" ]; then kaiwu_safe_path "$DEPLOY_REMOTE_NODE" || kaiwu_die "DEPLOY_REMOTE_NODE 必须是绝对路径"; fi
	case $DEPLOY_DB_NAME in '' | *[!A-Za-z0-9._-]*) kaiwu_die "DEPLOY_DB_NAME 无效" ;; esac
	case $DEPLOY_HEALTH_TIMEOUT in '' | *[!0-9]*) kaiwu_die "DEPLOY_HEALTH_TIMEOUT 无效" ;; esac
	case $DEPLOY_LOCK_TIMEOUT in '' | *[!0-9]*) kaiwu_die "DEPLOY_LOCK_TIMEOUT 无效" ;; esac
	case $DEPLOY_HTTP_CONNECT_TIMEOUT in '' | *[!0-9]*) kaiwu_die "DEPLOY_HTTP_CONNECT_TIMEOUT 无效" ;; esac
	case $DEPLOY_HTTP_MAX_TIME in '' | *[!0-9]*) kaiwu_die "DEPLOY_HTTP_MAX_TIME 无效" ;; esac
	case $DEPLOY_STOP_TIMEOUT in '' | *[!0-9]*) kaiwu_die "DEPLOY_STOP_TIMEOUT 无效" ;; esac
	if [ -n "$DEPLOY_REMOTE_PATH" ]; then
		for p in $(printf '%s' "$DEPLOY_REMOTE_PATH" | tr ':' ' '); do
			kaiwu_safe_path "$p" || kaiwu_die "DEPLOY_REMOTE_PATH 含非绝对或含元字符的路径：$p"
		done
	fi
	DEPLOY_BROWSER_PORT_LIST=$(printf '%s' "$DEPLOY_BROWSER_PORTS" | tr ',' ' ')
	port_count=0
	for p in $DEPLOY_BROWSER_PORT_LIST; do
		kaiwu_port "$p" || kaiwu_die "DEPLOY_BROWSER_PORTS 含非法端口：$p"
		port_count=$((port_count + 1))
	done
	[ "$port_count" -eq 3 ] || kaiwu_die "DEPLOY_BROWSER_PORTS 需要三个逗号分隔的端口"
	[ "$(printf '%s\n' $DEPLOY_BROWSER_PORT_LIST | sort -u | wc -l)" -eq 3 ] || kaiwu_die "DEPLOY_BROWSER_PORTS 端口不能重复"
	kaiwu_port "$DEPLOY_VERSION_BROWSER_PORT" || kaiwu_die "DEPLOY_VERSION_BROWSER_PORT 无效"
	for p in $DEPLOY_BROWSER_PORT_LIST; do
		[ "$p" = "$DEPLOY_VERSION_BROWSER_PORT" ] && kaiwu_die "DEPLOY_VERSION_BROWSER_PORT 不能与浏览器端口重复"
	done
	export DEPLOY_BROWSER_PORT_LIST
}

kaiwu_read_config "$config"

# kaiwu_select_ref reads the reference-transaction lines and returns the new
# value of the configured branch only for a committed, non-deletion update.
kaiwu_select_ref() {
	local phase=$1 old new ref
	if [ "$phase" != committed ]; then
		kaiwu_log "跳过：事务阶段为 $phase"
		return 1
	fi
	while read -r old new ref; do
		[ "$ref" = "refs/heads/$DEPLOY_BRANCH" ] || continue
		if kaiwu_all_zero "$new"; then
			kaiwu_log "跳过：$DEPLOY_BRANCH 被删除"
			return 1
		fi
		selected_sha=$new
		return 0
	done
	kaiwu_log "跳过：本次更新不涉及 refs/heads/$DEPLOY_BRANCH"
	return 1
}

selected_sha=""
if [ "$mode" = hook ]; then
	if ! kaiwu_select_ref "$hook_phase"; then
		exit 0
	fi
	sha=$selected_sha
else
	sha=$(gitq -C "$repo" rev-parse --verify "refs/heads/$DEPLOY_BRANCH^{commit}") ||
		kaiwu_die "无法解析 refs/heads/$DEPLOY_BRANCH"
fi
kaiwu_valid_sha "$sha" || kaiwu_die "候选不是完整 40 位修订号：$sha"

LOG_DIR=$repo/.kaiwu/deploy-logs
mkdir -p "$LOG_DIR" || kaiwu_die "无法创建日志目录"
chmod 700 "$LOG_DIR" 2>/dev/null || true
LOG_FILE=$LOG_DIR/$(date -u +%Y%m%dT%H%M%SZ)-$$.log
STEP_LOG=${LOG_FILE}.steps
kaiwu_log() { printf '[kaiwu-deploy] %s\n' "$*" | tee -a "$LOG_FILE" >&2; }

remote_run() { # subcommand [args...]
	local err code=0
	err=$(mktemp) || return 1
	cat "$SCRIPT_DIR/lib/common.sh" "$SCRIPT_DIR/remote-deploy.sh" |
		ssh -p "$DEPLOY_SSH_PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new \
			"$DEPLOY_SSH_HOST" env \
			KAIWU_DEPLOY_REMOTE_BASE="$DEPLOY_REMOTE_BASE" \
			KAIWU_DEPLOY_DATA_DIR="$DEPLOY_DATA_DIR" \
			KAIWU_DEPLOY_SERVICE_NAME="$DEPLOY_SERVICE_NAME" \
			KAIWU_DEPLOY_LISTEN_ADDR="$DEPLOY_LISTEN_ADDR" \
			KAIWU_DEPLOY_HEALTH_URL="$DEPLOY_HEALTH_URL" \
			KAIWU_DEPLOY_HEALTH_TIMEOUT="$DEPLOY_HEALTH_TIMEOUT" \
			KAIWU_DEPLOY_HTTP_CONNECT_TIMEOUT="$DEPLOY_HTTP_CONNECT_TIMEOUT" \
			KAIWU_DEPLOY_HTTP_MAX_TIME="$DEPLOY_HTTP_MAX_TIME" \
			KAIWU_DEPLOY_DB_NAME="$DEPLOY_DB_NAME" \
			KAIWU_DEPLOY_NODE="$DEPLOY_REMOTE_NODE" \
			KAIWU_DEPLOY_STOP_TIMEOUT="$DEPLOY_STOP_TIMEOUT" \
			KAIWU_DEPLOY_REMOTE_PATH="$DEPLOY_REMOTE_PATH" \
			sh -s -- "$@" 2>"$err" || code=$?
	cat "$err" >>"$LOG_FILE" 2>/dev/null || true
	cat "$err" >&2 || true
	rm -f "$err" 2>/dev/null || true
	return $code
}

scp_upload() { # local remote-suffix
	scp -P "$DEPLOY_SSH_PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new -q \
		"$1" "$DEPLOY_SSH_HOST:$DEPLOY_REMOTE_BASE/$2"
}

pipeline_fail() { # reason
	kaiwu_log "候选未通过：$1"
	remote_run mark failed "$sha" >/dev/null 2>&1 || kaiwu_log "目标不可达，未能记录失败状态"
	exit 1
}

# run_step captures the real test or build output into the private run log so a
# failure is named and locatable. No credential ever reaches this log.
run_step() { # workdir label command...
	local dir=$1 label=$2
	shift 2
	kaiwu_log "开始：$label"
	if (cd "$dir" && "$@") >>"$STEP_LOG" 2>&1; then
		kaiwu_log "通过：$label"
		return 0
	fi
	kaiwu_log "失败：$label（完整输出见 $STEP_LOG）"
	tail -n 40 "$STEP_LOG" 2>/dev/null | sed 's/^/    /' >&2 || true
	return 1
}

kaiwu_clean_summary() {
	printf '%s' "$1" | tr '\n\r\t' '   ' | tr -s ' ' | sed "s/['\"\\\\]//g" | cut -c1-120
}

# browser_prepare decides whether the mandatory browser gate can run. A missing
# browser is a blocker, never a silent skip; only an explicit
# DEPLOY_BROWSER_TESTS=0 records that UI acceptance was not executed.
browser_prepare() {
	case $DEPLOY_BROWSER_TESTS in
	0)
		BROWSER_ENABLED=0
		kaiwu_log "浏览器检查已由配置显式关闭（DEPLOY_BROWSER_TESTS=0）：本批未执行 UI 验收"
		return 0
		;;
	1) BROWSER_ENABLED=1 ;;
	*) return 1 ;;
	esac
	if [ -n "$DEPLOY_CHROMIUM" ]; then
		if [ ! -x "$DEPLOY_CHROMIUM" ]; then
			kaiwu_log "配置的 Chromium 不可执行：$DEPLOY_CHROMIUM"
			return 1
		fi
		CHROMIUM_BIN=$DEPLOY_CHROMIUM
	elif [ -x /usr/bin/chromium ]; then
		CHROMIUM_BIN=/usr/bin/chromium
	elif command -v chromium >/dev/null 2>&1; then
		CHROMIUM_BIN=$(command -v chromium)
	elif [ -n "${CHROMIUM_BIN:-}" ] && [ -x "$CHROMIUM_BIN" ]; then
		:
	else
		kaiwu_log "浏览器检查为必需项但未找到 Chromium：请设置 DEPLOY_CHROMIUM，或用 DEPLOY_BROWSER_TESTS=0 显式记录本批未做 UI 验收"
		return 1
	fi
	export CHROMIUM_BIN
	kaiwu_log "浏览器检查使用 $CHROMIUM_BIN"
	return 0
}

# patch_browser_ports moves the copied suite to the configured ports so a
# concurrent task on the same machine cannot collide with it.
patch_browser_ports() { # archive root
	local root=$1 old new f
	local want
	want=$(printf '%s' "$DEPLOY_BROWSER_PORTS" | tr ',' ' ')
	[ "$want" = "$DEFAULT_BROWSER_PORTS" ] && return 0
	# shellcheck disable=SC2086
	set -- $want
	for old in $DEFAULT_BROWSER_PORTS; do
		new=$1
		shift
		while IFS= read -r f; do
			sed -i "s/127\.0\.0\.1:$old/127.0.0.1:$new/g" "$f"
		done < <(grep -rl -- "127.0.0.1:$old" "$root/apps" "$root/server" 2>/dev/null || true)
	done
	kaiwu_log "浏览器测试端口已替换为 $DEPLOY_BROWSER_PORTS"
	return 0
}

# check_browser_ports fails with a named reason instead of taking over another
# task's listener. The independent version suite port is checked too.
check_browser_ports() {
	local p
	for p in $DEPLOY_BROWSER_PORT_LIST $DEPLOY_VERSION_BROWSER_PORT; do
		if (exec 3<>"/dev/tcp/127.0.0.1/$p") 2>/dev/null; then
			kaiwu_log "浏览器端口 $p 已被占用；请用 DEPLOY_BROWSER_PORTS／DEPLOY_VERSION_BROWSER_PORT 改为独立端口，不接管该进程"
			return 1
		fi
	done
	return 0
}

# Test-only globals must never reach the packaged bundle. The unit check in
# version.test.ts only inspects an already existing dist, so the pipeline scans
# the freshly built assets itself.
MOCK_MARKERS='__MOCK_'
scan_build_assets() { # archive root
	local root=$1 marker dir
	for dir in "$root/server/internal/webui/dist/assets" "$root/apps/web/dist/assets"; do
		[ -d "$dir" ] || continue
		for marker in $MOCK_MARKERS; do
			if grep -rlF -- "$marker" "$dir" >/dev/null 2>&1; then
				kaiwu_log "构建产物 $dir 含测试标记 $marker"
				return 1
			fi
		done
	done
	return 0
}

# deploy_sha runs the whole pipeline for one committed revision.
deploy_sha() {
	sha=$1
	local summary built_at ref_now ldflags out
	work=$(mktemp -d "${TMPDIR:-/tmp}/kaiwu-deploy.XXXXXX")
	trap 'rm -rf "$work"' EXIT INT TERM
	mkdir -p "$work/src" "$work/out"

	kaiwu_log "候选：$sha"
	kaiwu_log "目标：$DEPLOY_SSH_HOST:$DEPLOY_REMOTE_BASE（服务 $DEPLOY_SERVICE_NAME）"
	remote_run prepare >/dev/null || kaiwu_die "无法连接部署目标：未开始测试，当前版本不变"
	remote_run mark testing "$sha" >/dev/null || kaiwu_die "无法记录测试状态：当前版本不变"

	browser_prepare || pipeline_fail "浏览器检查不可执行"

	ref_now=$(gitq -C "$repo" rev-parse --verify "refs/heads/$DEPLOY_BRANCH^{commit}")
	[ "$ref_now" = "$sha" ] || pipeline_fail "主分支已前进到 $ref_now"
	gitq -C "$repo" archive --format=tar "$sha" | tar -x -C "$work/src" || pipeline_fail "Git 归档失败"
	summary=$(kaiwu_clean_summary "$(gitq -C "$repo" log -1 --format=%s "$sha")")
	built_at=$(kaiwu_now)

	run_step "$work/src/server" "go vet ./..." env GOTOOLCHAIN=local go vet ./... ||
		pipeline_fail "go vet"
	run_step "$work/src/server" "go test ./... -count=1" env GOTOOLCHAIN=local go test ./... -count=1 ||
		pipeline_fail "go test"
	run_step "$work/src" "npm ci (apps/web)" npm --prefix apps/web ci || pipeline_fail "npm ci"
	run_step "$work/src" "前端单元测试" npm --prefix apps/web test || pipeline_fail "前端单元测试"

	remote_run mark building "$sha" >/dev/null || pipeline_fail "无法记录构建状态"
	# The browser fixtures and the packaged server both serve the page built here,
	# so the front-end build must come before any browser suite: a clean archive
	# has no webui assets and the page would otherwise answer 503.
	run_step "$work/src" "前端构建" env VITE_BUILD_REVISION="$sha" npm --prefix apps/web run build ||
		pipeline_fail "前端构建"
	# The front end reads import.meta.env.VITE_BUILD_REVISION; the candidate must
	# embed the exact full revision or the deployment stops here.
	if ! grep -rlF -- "$sha" "$work/src/apps/web/dist/assets" >/dev/null 2>&1; then
		pipeline_fail "前端构建未嵌入完整候选修订号 $sha（缺少 VITE_BUILD_REVISION 注入）"
	fi
	scan_build_assets "$work/src" || pipeline_fail "构建产物含测试标记"

	if [ "$BROWSER_ENABLED" = 1 ]; then
		patch_browser_ports "$work/src"
		check_browser_ports || pipeline_fail "浏览器端口被占用"
		run_step "$work/src" "浏览器交互测试（原有用例独立生命周期）" npm --prefix apps/web run test:browser -- --config playwright.interaction.config.ts --grep-invert "$INTERACTION_V2_GREP" ||
			pipeline_fail "浏览器交互测试原有用例"
		run_step "$work/src" "浏览器交互测试（V2独立生命周期）" npm --prefix apps/web run test:browser -- --config playwright.interaction.config.ts --grep "$INTERACTION_V2_GREP" ||
			pipeline_fail "浏览器交互测试V2用例"
		run_step "$work/src" "浏览器主流程（独立生命周期一）" npm --prefix apps/web run test:browser -- --project=existing --grep "$HISTORY_GREP" ||
			pipeline_fail "浏览器主流程分组一"
		run_step "$work/src" "浏览器主流程（独立生命周期二）" npm --prefix apps/web run test:browser -- --project=existing --project=delivery --project=ui-consistency --grep-invert "$HISTORY_GREP" ||
			pipeline_fail "浏览器主流程分组二"
		# UI regressions must not inherit the business suite's authentication
		# rate budget or its eight resident agent sessions.
		run_step "$work/src" "浏览器主题与抽屉（独立生命周期）" npm --prefix apps/web run test:browser -- --project=theme --project=drawer ||
			pipeline_fail "浏览器主题与抽屉套件"
		# Project tools create persistent projects and sessions. Run them in a
		# fresh service, preserving the legacy suite's empty-database premise.
		run_step "$work/src" "浏览器项目工具（独立生命周期）" npm --prefix apps/web run test:browser -- --project=project-tools ||
			pipeline_fail "浏览器项目工具套件"
		run_step "$work/src" "浏览器版本套件" env KAIWU_VERSION_TEST_PORT="$DEPLOY_VERSION_BROWSER_PORT" KAIWU_VERSION_TEST_REVISION="$sha" npm --prefix apps/web run test:browser -- --config playwright.version.config.ts ||
			pipeline_fail "浏览器版本套件"
	fi

	ldflags="-X $BUILDINFO_PKG.revision=$sha -X $BUILDINFO_PKG.builtAt=$built_at -X '$BUILDINFO_PKG.summary=$summary'"
	run_step "$work/src/server" "Go 构建" env GOTOOLCHAIN=local go build -buildvcs=false -trimpath -ldflags "$ldflags" \
		-o "$work/out/kaiwu-server" ./cmd/kaiwu-server || pipeline_fail "Go 构建"
	tar -czf "$work/out/kaiwu-server.tgz" -C "$work/out" kaiwu-server || pipeline_fail "打包失败"
	printf '%s  %s\n' "$(sha256sum "$work/out/kaiwu-server.tgz" | cut -d' ' -f1)" "$sha.tgz" >"$work/out/kaiwu-server.tgz.sha256" ||
		pipeline_fail "生成校验值失败"

	ref_now=$(gitq -C "$repo" rev-parse --verify "refs/heads/$DEPLOY_BRANCH^{commit}")
	[ "$ref_now" = "$sha" ] || pipeline_fail "切换前主分支已前进到 $ref_now"
	remote_run mark deploying "$sha" >/dev/null || pipeline_fail "无法记录部署状态"
	scp_upload "$work/out/kaiwu-server.tgz" "incoming/$sha.tgz" || pipeline_fail "上传候选包失败"
	scp_upload "$work/out/kaiwu-server.tgz.sha256" "incoming/$sha.sha256" || pipeline_fail "上传校验值失败"
	out=$(remote_run apply "$sha") || pipeline_fail "远端启用或实际入口核对失败"
	kaiwu_log "部署成功：$out"
	kaiwu_log "最近部署记录位于目标 $DEPLOY_DATA_DIR/deployment-status.json"
}

case $mode in
status)
	remote_run status || kaiwu_die "无法读取部署状态"
	;;
*)
	mkdir -p "$repo/.kaiwu" || kaiwu_die "无法创建 .kaiwu 目录"
	exec 9>"$repo/.kaiwu/deploy.lock"
	if ! flock -w "$DEPLOY_LOCK_TIMEOUT" 9; then
		kaiwu_die "已有部署在进行，未在 ${DEPLOY_LOCK_TIMEOUT}s 内获得锁"
	fi
	deploy_sha "$sha"
	;;
esac
