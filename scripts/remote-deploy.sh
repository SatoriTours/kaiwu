#!/bin/sh
# Kaiwu deployment target side.
#
# The local script pipes lib/common.sh followed by this file to the target over
# SSH and runs it with `sh -s -- <subcommand> [args]`. All configuration arrives
# through KAIWU_DEPLOY_* environment variables; no secret is passed on the
# command line and none is ever printed.
#
# Subcommands:
#   prepare              create private directories, token file and user unit
#   mark <status> <sha>  record an in-progress or failed attempt
#   apply <sha>          back up the database, atomically activate, verify or roll back
#   status               print the current deployment record
#
# Activating runs while the service is stopped and a consistent SQLite backup
# exists. A failed candidate is only switched back when the database schema is
# confirmed unchanged; otherwise the candidate is stopped and kept for manual
# handling instead of letting an old binary read a newer schema.
set -eu
# Everything the target creates is private by default: cookies, token file,
# database copies and status records.
umask 077

BASE=${KAIWU_DEPLOY_REMOTE_BASE:?缺少远端部署目录}
DATA=${KAIWU_DEPLOY_DATA_DIR:?缺少服务数据目录}
SERVICE=${KAIWU_DEPLOY_SERVICE_NAME:-kaiwu-server}
LISTEN=${KAIWU_DEPLOY_LISTEN_ADDR:?缺少监听地址}
HEALTH=${KAIWU_DEPLOY_HEALTH_URL:?缺少实际访问入口}
HEALTH_TIMEOUT=${KAIWU_DEPLOY_HEALTH_TIMEOUT:-60}
REMOTE_PATH=${KAIWU_DEPLOY_REMOTE_PATH:-}
HTTP_CONNECT_TIMEOUT=${KAIWU_DEPLOY_HTTP_CONNECT_TIMEOUT:-5}
HTTP_MAX_TIME=${KAIWU_DEPLOY_HTTP_MAX_TIME:-15}
DB_NAME=${KAIWU_DEPLOY_DB_NAME:-workbench.db}
NODE_HINT=${KAIWU_DEPLOY_NODE:-}
STOP_TIMEOUT=${KAIWU_DEPLOY_STOP_TIMEOUT:-20}

STATUS_FILE=$DATA/deployment-status.json
STATE_DIR=$BASE/state
ENV_FILE=$BASE/service.env
UNIT_FILE=$BASE/systemd/$SERVICE.service
USER_UNIT_DIR=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user
USER_UNIT_LINK=$USER_UNIT_DIR/$SERVICE.service
INCOMING=$BASE/incoming
RELEASES=$BASE/releases
BACKUPS=$BASE/backups
CURRENT=$BASE/current
LOG_FILE=$BASE/logs/deploy.log
DB=$DATA/$DB_NAME

TMP_DIRS=''
cleanup() {
	for p in $TMP_DIRS; do rm -rf "$p" 2>/dev/null || true; done
	TMP_DIRS=''
}
trap 'cleanup' EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

say() { printf '[kaiwu-remote] %s\n' "$*" >&2; }
fail() {
	say "失败：$*"
	exit 1
}
log() {
	private_dir "$(dirname "$LOG_FILE")" 2>/dev/null || true
	printf '%s %s\n' "$(kaiwu_now)" "$*" >>"$LOG_FILE" 2>/dev/null || true
}
private_dir() {
	mkdir -p "$1" || return 1
	chmod 700 "$1" 2>/dev/null || true
	return 0
}

# User-systemd calls over SSH need the runtime directory even when PAM did not
# export it.
ensure_xdg() {
	if [ -z "${XDG_RUNTIME_DIR:-}" ] && [ -d "/run/user/$(id -u)" ]; then
		XDG_RUNTIME_DIR=/run/user/$(id -u)
		export XDG_RUNTIME_DIR
	fi
}

# find_node prints a usable user-level Node for the built-in sqlite backup API.
find_node() {
	if [ -n "$NODE_HINT" ]; then
		[ -x "$NODE_HINT" ] && {
			printf '%s' "$NODE_HINT"
			return 0
		}
		return 1
	fi
	for c in "$HOME/.local/share/pi-node/current/bin/node" /usr/local/bin/node /usr/bin/node; do
		[ -x "$c" ] && {
			printf '%s' "$c"
			return 0
		}
	done
	command -v node 2>/dev/null || return 1
}

active_revision() {
	[ -f "$STATE_DIR/active-revision" ] && cat "$STATE_DIR/active-revision"
}
active_deployed_at() {
	[ -f "$STATE_DIR/active-deployed-at" ] && cat "$STATE_DIR/active-deployed-at"
}
write_attempt() { # status sha
	kaiwu_write_status "$STATUS_FILE" "$1" "$2" "$(kaiwu_now)" "$(active_revision)" "$(active_deployed_at)" ||
		fail "无法写入部署状态"
}

# ensure_env creates the private service environment file with a random access
# token on first use. The token only exists in this 0600 file and is never
# printed or copied into a release.
ensure_env() {
	private_dir "$BASE" || fail "无法创建部署目录"
	if [ -f "$ENV_FILE" ] && grep -q '^KAIWU_WEB_TOKEN=..' "$ENV_FILE" 2>/dev/null; then
		chmod 600 "$ENV_FILE" 2>/dev/null || true
		return 0
	fi
	token=$(od -An -v -tx1 -N32 /dev/urandom 2>/dev/null | tr -d ' \n') || fail "无法生成访问令牌"
	[ "${#token}" -eq 64 ] || fail "生成的访问令牌长度异常"
	(
		umask 077
		cat >"$ENV_FILE" <<EOF
KAIWU_WEB_TOKEN=$token
KAIWU_LISTEN_ADDR=$LISTEN
KAIWU_DATA_DIR=$DATA
EOF
	) || fail "无法写入服务环境文件"
	chmod 600 "$ENV_FILE" 2>/dev/null || true
	say "已为首次启动生成访问令牌（仅保存在目标私有文件）"
}

# ensure_unit writes our own user unit. It refuses to touch a same-named unit
# that this installer did not create and refuses to adopt a service defined
# elsewhere; it never signals a process outside our own unit.
ensure_unit() {
	private_dir "$BASE/systemd" || fail "无法创建 systemd 目录"
	private_dir "$USER_UNIT_DIR" || fail "无法创建用户单元目录"
	private_dir "$(dirname "$LOG_FILE")" || fail "无法创建日志目录"

	if [ -L "$USER_UNIT_LINK" ] || [ -e "$USER_UNIT_LINK" ]; then
		existing=$(readlink "$USER_UNIT_LINK" 2>/dev/null || true)
		if [ "$existing" != "$UNIT_FILE" ]; then
			fail "同名用户单元不属于本脚本，拒绝覆盖：$USER_UNIT_LINK"
		fi
	fi
	ensure_xdg
	fragment=$(systemctl --user show -p FragmentPath --value "$SERVICE.service" 2>/dev/null || true)
	if [ -n "$fragment" ]; then
		case $fragment in
		"$UNIT_FILE" | "$USER_UNIT_LINK") ;;
		*) fail "已存在无归属的同名服务，拒绝接管：$fragment" ;;
		esac
	fi

	path=${REMOTE_PATH:-$HOME/.pi/agent/bin:$HOME/.local/bin:$HOME/.local/share/pi-node/current/bin:/usr/local/bin:/usr/bin}
	cat >"$UNIT_FILE" <<EOF
[Unit]
Description=Kaiwu local workbench server
After=network.target

[Service]
Type=simple
WorkingDirectory=$HOME
EnvironmentFile=$ENV_FILE
Environment=PATH=$path
# Never inherit auto-delivery credentials from the user manager environment:
# this deployment must not enable an unverified worker.
UnsetEnvironment=KAIWU_GITHUB_API_TOKEN KAIWU_GITHUB_GIT_TOKEN KAIWU_GITHUB_SSH_KEY KAIWU_GITHUB_SSH_AGENT
ExecStart=$CURRENT/kaiwu-server
Restart=on-failure
RestartSec=3
KillMode=mixed
NoNewPrivileges=yes

[Install]
WantedBy=default.target
EOF
	ln -sfn "$UNIT_FILE" "$USER_UNIT_LINK" || fail "无法安装用户服务单元"
	systemctl --user daemon-reload || fail "systemd 用户实例不可用"
}

current_target() { readlink "$CURRENT" 2>/dev/null || true; }

# switch_current replaces the current symlink atomically with a same-directory
# temporary link plus rename; ln -sfn alone is not atomic.
switch_current() { # target
	tmp="$BASE/.current.$$.tmp"
	rm -f "$tmp" 2>/dev/null || true
	ln -s "$1" "$tmp" || return 1
	if ! mv -T "$tmp" "$CURRENT" 2>/dev/null; then
		rm -f "$tmp" 2>/dev/null || true
		return 1
	fi
	return 0
}

# unit_show and unit_stopped read the exact properties that prove our own unit
# has no process left. 0 = stopped, 1 = still running, 2 = unknown (never
# treated as stopped).
unit_show() {
	systemctl --user show -p ActiveState -p SubState -p MainPID -p ControlPID -p ControlGroup \
		"$SERVICE.service" 2>/dev/null || true
}
unit_prop() { # blob name
	printf '%s\n' "$1" | sed -n "s/^$2=//p" | head -n 1
}
unit_stopped() {
	show=$(unit_show)
	[ -n "$show" ] || return 2
	active=$(unit_prop "$show" ActiveState)
	sub=$(unit_prop "$show" SubState)
	main=$(unit_prop "$show" MainPID)
	ctrl=$(unit_prop "$show" ControlPID)
	cg=$(unit_prop "$show" ControlGroup)
	case $active in inactive | failed) ;; *) return 1 ;; esac
	case $sub in dead | failed | exited) ;; *) return 1 ;; esac
	[ "$main" = 0 ] || return 1
	[ -z "$ctrl" ] || [ "$ctrl" = 0 ] || return 1
	# When the cgroup is readable, it must hold no process at all.
	if [ -n "$cg" ] && [ -r "/sys/fs/cgroup$cg/cgroup.procs" ] && [ -s "/sys/fs/cgroup$cg/cgroup.procs" ]; then
		return 1
	fi
	return 0
}

# stop_service stops only our own unit and returns success only with positive
# evidence that nothing from it is still running. A failed, timed-out or
# unknown stop never counts as stopped, so no backup or switch can start while
# a writer may still be alive. It never signals any process by itself.
stop_service() {
	if command -v timeout >/dev/null 2>&1; then
		if ! timeout "$STOP_TIMEOUT" systemctl --user stop "$SERVICE.service" >/dev/null 2>&1; then
			say "systemctl stop 未成功或超时，无法确认服务已停止"
			return 1
		fi
	elif ! systemctl --user stop "$SERVICE.service" >/dev/null 2>&1; then
		say "systemctl stop 未成功，无法确认服务已停止"
		return 1
	fi
	deadline=$(($(date +%s) + STOP_TIMEOUT))
	while :; do
		if unit_stopped; then
			return 0
		fi
		if [ "$(date +%s)" -ge "$deadline" ]; then
			say "未在 ${STOP_TIMEOUT}s 内取得确定停止证据（可能仍在 deactivating）"
			return 1
		fi
		sleep 0.2
	done
}

start_service() { systemctl --user start "$SERVICE.service" >/dev/null 2>&1; }

wait_health() {
	deadline=$(($(date +%s) + HEALTH_TIMEOUT))
	while :; do
		code=$(curl -sS --noproxy '*' --connect-timeout "$HTTP_CONNECT_TIMEOUT" --max-time "$HTTP_MAX_TIME" \
			-o /dev/null -w '%{http_code}' "$HEALTH/healthz" 2>/dev/null || true)
		[ "$code" = 200 ] && return 0
		[ "$(date +%s)" -lt "$deadline" ] || return 1
		sleep 1
	done
}

db_user_version() {
	[ -f "$DB" ] || {
		printf 'absent'
		return 0
	}
	n=$(find_node || true)
	if [ -n "$n" ]; then
		out=$("$n" -e 'const{DatabaseSync}=require("node:sqlite");const d=new DatabaseSync(process.argv[1],{readOnly:true});process.stdout.write(String(d.prepare("PRAGMA user_version").get().user_version));d.close();' "$DB" 2>/dev/null || true)
		if [ -n "$out" ]; then
			printf '%s' "$out"
			return 0
		fi
	fi
	if command -v sqlite3 >/dev/null 2>&1; then
		out=$(sqlite3 "$DB" 'PRAGMA user_version;' 2>/dev/null || true)
		if [ -n "$out" ]; then
			printf '%s' "$out"
			return 0
		fi
	fi
	printf '?'
}

# db_state is the comparison basis for a safe rollback: the schema marker plus
# the actual database, WAL and SHM contents.
db_state() {
	uv=$(db_user_version)
	db_hash=-
	[ -f "$DB" ] && db_hash=$(sha256sum "$DB" 2>/dev/null | cut -d' ' -f1)
	wal_hash=-
	[ -f "$DB-wal" ] && wal_hash=$(sha256sum "$DB-wal" 2>/dev/null | cut -d' ' -f1)
	shm_hash=-
	[ -f "$DB-shm" ] && shm_hash=$(sha256sum "$DB-shm" 2>/dev/null | cut -d' ' -f1)
	printf 'uv=%s db=%s wal=%s shm=%s' "$uv" "$db_hash" "$wal_hash" "$shm_hash"
}

db_integrity_ok() { # file
	f=$1
	n=$(find_node || true)
	if [ -n "$n" ]; then
		out=$("$n" -e 'const{DatabaseSync}=require("node:sqlite");const d=new DatabaseSync(process.argv[1],{readOnly:true});const r=d.prepare("PRAGMA integrity_check").get();process.stdout.write(String(Object.values(r)[0]));d.close();' "$f" 2>/dev/null || true)
		[ "$out" = ok ] && return 0
	fi
	if command -v sqlite3 >/dev/null 2>&1; then
		out=$(sqlite3 "$f" 'PRAGMA integrity_check;' 2>/dev/null || true)
		[ "$out" = ok ] && return 0
	fi
	return 1
}

# backup_database copies the stopped database into a private directory using
# the user-level Node sqlite backup API or sqlite3 .backup, then verifies the
# copy. Only when neither tool works does it fall back to a whole
# database/WAL/SHM snapshot, which is consistent because the service and every
# controlled writer are already stopped. It prints the method used.
backup_database() { # dest-dir
	dir=$1
	private_dir "$dir" || return 1
	if [ ! -f "$DB" ]; then
		printf 'absent'
		return 0
	fi
	dest=$dir/$DB_NAME
	n=$(find_node || true)
	if [ -n "$n" ]; then
		if "$n" -e 'const{DatabaseSync,backup}=require("node:sqlite");const db=new DatabaseSync(process.argv[1],{readOnly:true});backup(db,process.argv[2]).then(()=>{db.close();process.exit(0);}).catch(e=>{console.error(e.message);process.exit(1);});' "$DB" "$dest" >/dev/null 2>&1 &&
			db_integrity_ok "$dest"; then
			chmod 600 "$dest" 2>/dev/null || true
			printf 'node:sqlite'
			return 0
		fi
		say "node:sqlite 备份或校验失败，改用 sqlite3"
	fi
	if command -v sqlite3 >/dev/null 2>&1; then
		if sqlite3 "$DB" ".backup '$dest'" >/dev/null 2>&1 && db_integrity_ok "$dest"; then
			chmod 600 "$dest" 2>/dev/null || true
			printf 'sqlite3'
			return 0
		fi
		say "sqlite3 .backup 或校验失败，改用停写快照"
	fi
	# Service stopped and no controlled writer remains: copy DB, WAL and SHM
	# together so the snapshot stays consistent.
	rm -f "$dest" "$dest-wal" "$dest-shm" 2>/dev/null || true
	cp -f "$DB" "$dest" || return 1
	if [ -f "$DB-wal" ]; then cp -f "$DB-wal" "$dest-wal" || return 1; fi
	if [ -f "$DB-shm" ]; then cp -f "$DB-shm" "$dest-shm" || return 1; fi
	chmod 600 "$dest" "$dest-wal" "$dest-shm" 2>/dev/null || true
	if db_integrity_ok "$dest"; then
		printf 'stopped-copy'
	else
		say "停写快照无法校验（缺少 node 与 sqlite3），仅记录方法"
		printf 'stopped-copy-unverified'
	fi
	return 0
}

# can_revert compares the pre-activation and post-failure database state. An
# older binary is only started again when the schema marker is unchanged, or
# when nothing at all was written.
can_revert() { # pre_state post_state
	pre=$1
	post=$2
	pre_uv=${pre#uv=}
	pre_uv=${pre_uv%% *}
	post_uv=${post#uv=}
	post_uv=${post_uv%% *}
	if [ "$pre_uv" != "?" ] && [ "$pre_uv" != "absent" ] && [ "$pre_uv" = "$post_uv" ]; then
		return 0
	fi
	[ "$pre" = "$post" ] && return 0
	return 1
}

# verify_http <expected-sha> creates one private temporary directory, always
# removes it on every return path and registers it for signal cleanup. It is
# called in the main shell so a TERM also removes it.
verify_http() {
	tmp=$(mktemp -d "${TMPDIR:-/tmp}/kaiwu-verify.XXXXXX") || {
		say "无法创建核对临时目录"
		return 1
	}
	TMP_DIRS="$TMP_DIRS $tmp"
	verify_http_in "$tmp" "$1"
	verify_rc=$?
	rm -rf "$tmp" 2>/dev/null || true
	return "$verify_rc"
}

# verify_http_in <tmp-dir> <expected-sha> logs in through the real entry URL,
# requires real 200 responses, rejects an SPA fallback masquerading as a static
# resource and always requires the front-end bundle to embed the candidate
# revision.
verify_http_in() {
	tmp=$1
	expected=$2
	token=$(sed -n 's/^KAIWU_WEB_TOKEN=//p' "$ENV_FILE" | head -n 1)
	[ -n "$token" ] || {
		say "服务环境文件缺少访问令牌"
		return 1
	}
	jar=$tmp/cookies
	headers=$tmp/headers
	body=$tmp/body
	ahdr=$tmp/asset-headers
	abody=$tmp/asset-body

	login=$(printf '{"token":"%s"}' "$token" | curl -sS --noproxy '*' --connect-timeout "$HTTP_CONNECT_TIMEOUT" --max-time "$HTTP_MAX_TIME" \
		-o /dev/null -w '%{http_code}' -c "$jar" -H "Origin: $HEALTH" -H 'X-Kaiwu-Client: web' \
		-H 'Content-Type: application/json' --data @- "$HEALTH/api/login" 2>/dev/null || true)
	if [ "$login" != 200 ]; then
		say "实际入口登录返回 HTTP $login"
		return 1
	fi
	code=$(curl -sS --noproxy '*' --connect-timeout "$HTTP_CONNECT_TIMEOUT" --max-time "$HTTP_MAX_TIME" \
		-D "$headers" -o "$body" -w '%{http_code}' -b "$jar" "$HEALTH/api/version" 2>/dev/null || true)
	if [ "$code" != 200 ]; then
		say "版本接口返回 HTTP $code"
		return 1
	fi
	if ! grep -qi '^cache-control:[[:space:]]*no-store' "$headers"; then
		say "版本接口未声明 no-store"
		return 1
	fi
	vctype=$(tr -d '\r' <"$headers" | sed -n 's/^[Cc]ontent-[Tt]ype:[[:space:]]*//p' | tail -n 1 | cut -d';' -f1 | tr 'A-Z' 'a-z')
	case $vctype in
	application/json*) ;;
	*)
		say "版本接口类型异常：$vctype"
		return 1
		;;
	esac
	# The response lists the running revision first; never take the last match,
	# which could belong to the most recent attempt.
	revision=$(grep -o '"revision":"[0-9a-f]\{40\}"' "$body" | head -n 1 | sed 's/.*:"\(.*\)"/\1/')
	if [ -z "$revision" ]; then
		say "版本响应缺少完整修订号"
		return 1
	fi
	if [ "$revision" != "$expected" ]; then
		say "运行修订号 $revision 与候选 $expected 不符"
		return 1
	fi

	pcode=$(curl -sS --noproxy '*' --connect-timeout "$HTTP_CONNECT_TIMEOUT" --max-time "$HTTP_MAX_TIME" \
		-o "$tmp/index.html" -w '%{http_code}' "$HEALTH/" 2>/dev/null || true)
	if [ "$pcode" != 200 ]; then
		say "入口页面返回 HTTP $pcode"
		return 1
	fi
	assets=$(grep -o '/assets/[A-Za-z0-9._-]*' "$tmp/index.html" | sort -u)
	count=0
	for asset in $assets; do
		acode=$(curl -sS --noproxy '*' --connect-timeout "$HTTP_CONNECT_TIMEOUT" --max-time "$HTTP_MAX_TIME" \
			-D "$ahdr" -o "$abody" -w '%{http_code}' "$HEALTH$asset" 2>/dev/null || true)
		if [ "$acode" != 200 ]; then
			say "静态资源 $asset 返回 HTTP $acode"
			return 1
		fi
		[ -s "$abody" ] || {
			say "静态资源 $asset 内容为空"
			return 1
		}
		actype=$(tr -d '\r' <"$ahdr" | sed -n 's/^[Cc]ontent-[Tt]ype:[[:space:]]*//p' | tail -n 1 | cut -d';' -f1 | tr 'A-Z' 'a-z')
		case $asset in
		*.js | *.mjs)
			case $actype in
			*javascript*) ;;
			*)
				say "脚本资源 $asset 类型为 $actype，疑似 SPA 回退"
				return 1
				;;
			esac
			if ! grep -qF -- "$expected" "$abody"; then
				say "前端构建未嵌入候选修订号 $expected"
				return 1
			fi
			;;
		*.css)
			if [ "$actype" != "text/css" ]; then
				say "样式资源 $asset 类型为 $actype，疑似 SPA 回退"
				return 1
			fi
			;;
		*)
			if [ "$actype" = "text/html" ]; then
				say "静态资源 $asset 被 SPA 回退为 HTML"
				return 1
			fi
			;;
		esac
		count=$((count + 1))
	done
	if [ "$count" -lt 1 ]; then
		say "页面没有可核对的静态资源"
		return 1
	fi
	printf 'revision=%s assets=%d\n' "$revision" "$count"
	return 0
}

# handle_candidate_failure stops the candidate, decides whether the previous
# release can safely read the database and otherwise keeps the candidate
# stopped with its release and backup for manual handling. It never overwrites
# the database.
handle_candidate_failure() { # sha previous pre_state reason
	fsha=$1
	fprevious=$2
	fpre=$3
	freason=$4
	say "候选失败：$freason"
	stop_service || say "无法确认候选服务已停止"
	fpost=$(db_state)
	if [ -n "$fprevious" ] && [ -e "$fprevious" ] && can_revert "$fpre" "$fpost"; then
		switch_current "$fprevious" && say "已回退到上一个版本（数据库 schema 未变化）"
		start_service || say "回退后启动失败"
		wait_health || say "回退后健康检查未通过"
		write_attempt failed "$fsha"
		fail "$freason（已回退，数据库未改动）"
	fi
	write_attempt failed "$fsha"
	fail "$freason；数据库或 schema 状态已变化，未自动回退；候选已停止，发布与备份保留，请人工核对"
}

cmd_prepare() {
	for d in "$BASE" "$DATA" "$INCOMING" "$RELEASES" "$BACKUPS" "$STATE_DIR" "$(dirname "$LOG_FILE")"; do
		private_dir "$d" || fail "无法创建私有目录：$d"
	done
	ensure_env
	ensure_unit
	log "prepare"
	printf 'prepare ok\n'
}

cmd_mark() { # status sha
	[ -n "${1-}" ] && [ -n "${2-}" ] || fail "mark 需要状态与修订号"
	write_attempt "$1" "$2"
	log "mark $1 $2"
	printf 'mark ok\n'
}

cmd_apply() { # sha
	sha=${1-}
	kaiwu_valid_sha "$sha" || fail "候选修订号无效"
	ensure_xdg
	tgz=$INCOMING/$sha.tgz
	sumfile=$INCOMING/$sha.sha256
	[ -f "$tgz" ] || fail "未收到候选包"
	[ -f "$sumfile" ] || fail "缺少候选包校验值"
	(cd "$INCOMING" && sha256sum -c "$sumfile" >/dev/null) || fail "候选包校验失败"

	target=$(mktemp -d "$RELEASES/$(kaiwu_now | tr ':' '-')-$sha.XXXXXX") || fail "无法创建发布目录"
	tar -xzf "$tgz" -C "$target" || fail "候选包解压失败"
	[ -x "$target/kaiwu-server" ] || fail "候选包缺少可执行程序"

	ensure_env
	ensure_unit
	systemctl --user enable "$SERVICE.service" >/dev/null 2>&1 || fail "systemd 启用失败"

	previous=$(current_target)

	# Stop our own service before touching the database, then take one
	# consistent backup and record the schema marker.
	if ! stop_service; then
		fail "当前服务未在超时内停止，未切换"
	fi
	pre_state=$(db_state)
	backup_dir=$(mktemp -d "$BACKUPS/$(kaiwu_now | tr ':' '-')-$sha.XXXXXX") || fail "无法创建备份目录"
	if ! backup_method=$(backup_database "$backup_dir"); then
		start_service || say "备份失败后未能重新启动当前服务"
		fail "候选启动前的 SQLite 备份失败，未切换"
	fi
	private_dir "$backup_dir" || true
	printf '%s\n' "$backup_method" >"$backup_dir/method.txt"
	printf '%s\n' "$pre_state" >"$backup_dir/db-state-before.txt"
	printf '%s\n' "$(kaiwu_now)" >"$backup_dir/attempt-at.txt"
	chmod 600 "$backup_dir/method.txt" "$backup_dir/db-state-before.txt" "$backup_dir/attempt-at.txt" 2>/dev/null || true
	log "backup $sha method=$backup_method state=$pre_state"

	if ! switch_current "$target"; then
		start_service || true
		fail "切换当前版本失败，已尝试恢复原服务"
	fi
	write_attempt deploying "$sha"
	log "apply $sha -> $target"

	if ! start_service; then
		handle_candidate_failure "$sha" "$previous" "$pre_state" "服务启动失败"
	fi
	if ! wait_health; then
		handle_candidate_failure "$sha" "$previous" "$pre_state" "健康检查在 ${HEALTH_TIMEOUT}s 内未通过"
	fi
	result_file=$(mktemp) || fail "无法创建核对结果文件"
	TMP_DIRS="$TMP_DIRS $result_file"
	if ! verify_http "$sha" >"$result_file"; then
		handle_candidate_failure "$sha" "$previous" "$pre_state" "实际入口版本或资源核对失败"
	fi
	result=$(cat "$result_file")
	rm -f "$result_file" 2>/dev/null || true

	mkdir -p "$STATE_DIR"
	printf '%s' "$sha" >"$STATE_DIR/active-revision"
	printf '%s' "$(kaiwu_now)" >"$STATE_DIR/active-deployed-at"
	chmod 600 "$STATE_DIR/active-revision" "$STATE_DIR/active-deployed-at" 2>/dev/null || true
	kaiwu_write_status "$STATUS_FILE" succeeded "$sha" "$(kaiwu_now)" "$sha" "$(active_deployed_at)" ||
		fail "无法写入成功状态"
	rm -f "$tgz" "$sumfile"
	log "succeeded $sha $result"
	printf 'apply ok revision=%s %s\n' "$sha" "$result"
}

cmd_stop_check() {
	ensure_xdg
	stop_service || fail "未能取得确定停止证据"
	printf 'stopped ok\n'
}

cmd_status() {
	if [ -f "$STATUS_FILE" ]; then
		head -c 65536 "$STATUS_FILE"
		printf '\n'
	else
		printf '{}\n'
	fi
}

cmd=${1-}
if [ $# -gt 0 ]; then shift; fi
case $cmd in
prepare) cmd_prepare ;;
mark) cmd_mark "${1-}" "${2-}" ;;
apply) cmd_apply "${1-}" ;;
stop-check) cmd_stop_check ;;
status) cmd_status ;;
*) fail "未知子命令（可用：prepare mark apply stop-check status）" ;;
esac
