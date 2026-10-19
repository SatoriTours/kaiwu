#!/usr/bin/env bash
# Real user-systemd reproduction for the stop evidence in remote-deploy.sh.
#
# It creates only its own uniquely named transient unit, holds a live process in
# the `deactivating` window and proves that the shipped stop check refuses to
# continue. systemctl stop is shimmed with --no-block so that window stays
# observable deterministically; every piece of state evidence comes from the
# real user manager. Cleanup only touches this test's own unit.
#
# When the environment has no usable user systemd the script reports SKIP and
# exits 0, because this is an optional real-hardware check on top of the
# fake-based suite.
set -uo pipefail

TESTS_DIR=$(CDPATH= cd -- "$(dirname -- "$(readlink -f -- "$0")")" && pwd)
SCRIPTS_DIR=$(dirname "$TESTS_DIR")

if ! systemctl --user show-environment >/dev/null 2>&1; then
	echo "SKIP：当前环境没有可用的用户 systemd"
	exit 0
fi
for c in systemd-run systemctl; do
	if ! command -v "$c" >/dev/null 2>&1; then
		echo "SKIP：缺少 $c"
		exit 0
	fi
done

ROOT=$(mktemp -d "${TMPDIR:-/tmp}/kaiwu-stop-test.XXXXXX")
UNIT="kaiwu-stop-check-$$"
SHIM=$ROOT/shim
REAL_SYSTEMCTL=$(command -v systemctl)
mkdir -p "$SHIM" "$ROOT/base" "$ROOT/data"
cleanup() {
	systemctl --user kill -s KILL "$UNIT.service" >/dev/null 2>&1 || true
	systemctl --user stop "$UNIT.service" >/dev/null 2>&1 || true
	systemctl --user reset-failed "$UNIT.service" >/dev/null 2>&1 || true
	rm -rf "$ROOT"
}
trap cleanup EXIT INT TERM

cat >"$SHIM/systemctl" <<SH
#!/bin/sh
# Make stop asynchronous so the deactivating window stays observable; every
# other call goes to the real user manager.
args=""
for a in "\$@"; do
	args="\$args \$a"
	[ "\$a" = stop ] && args="\$args --no-block"
done
exec "$REAL_SYSTEMCTL" \$args
SH
chmod +x "$SHIM/systemctl"

cat "$SCRIPTS_DIR/lib/common.sh" "$SCRIPTS_DIR/remote-deploy.sh" >"$ROOT/body.sh"

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
expect_exit() { # name got want
	if [ "$2" = "$3" ]; then ok "$1"; else not_ok "$1（退出码 $2，期望 $3）"; fi
}

run_check() { # stop-timeout
	PATH="$SHIM:$PATH" env \
		KAIWU_DEPLOY_REMOTE_BASE="$ROOT/base" KAIWU_DEPLOY_DATA_DIR="$ROOT/data" \
		KAIWU_DEPLOY_SERVICE_NAME="$UNIT" KAIWU_DEPLOY_LISTEN_ADDR=127.0.0.1:17331 \
		KAIWU_DEPLOY_HEALTH_URL=http://127.0.0.1:17331 KAIWU_DEPLOY_STOP_TIMEOUT="$1" \
		sh "$ROOT/body.sh" stop-check
}

# This test's own transient unit ignores SIGTERM with a long stop timeout.
# The marker file proves the trap is installed before any stop is issued.
MARK=$ROOT/trap-installed
systemd-run --user --unit="$UNIT" --property=TimeoutStopSec=60 \
	/bin/sh -c 'trap "" TERM; : >"$1"; while :; do sleep 1; done' sh "$MARK" >/dev/null 2>&1
mainpid=""
for _ in $(seq 1 50); do
	mainpid=$(systemctl --user show -p MainPID --value "$UNIT.service" 2>/dev/null)
	[ -n "$mainpid" ] && [ "$mainpid" != 0 ] && break
	sleep 0.2
done
for _ in $(seq 1 50); do
	[ -f "$MARK" ] && break
	sleep 0.1
done
if [ ! -f "$MARK" ]; then
	not_ok "瞬态单元未安装 SIGTERM 忽略处理"
fi
if [ -n "$mainpid" ] && [ "$mainpid" != 0 ]; then
	ok "已启动自有瞬态单元（MainPID=$mainpid）"
else
	not_ok "未能启动自有瞬态单元"
fi

systemctl --user stop --no-block "$UNIT.service" >/dev/null 2>&1
state=""
for _ in $(seq 1 30); do
	state=$(systemctl --user show -p ActiveState --value "$UNIT.service" 2>/dev/null)
	[ "$state" = deactivating ] && break
	sleep 0.1
done
live=$(systemctl --user show -p MainPID --value "$UNIT.service" 2>/dev/null)
if [ "$state" = deactivating ] && [ -n "$live" ] && [ "$live" != 0 ]; then
	ok "已复现 deactivating 且进程存活（MainPID=$live）"
else
	not_ok "未复现 deactivating 窗口（state=$state pid=$live）"
fi

run_check 3 >/dev/null 2>&1
expect_exit "deactivating 存活进程不被判为已停" "$?" 1

KAIWU_DEPLOY_REMOTE_BASE="$ROOT/base" KAIWU_DEPLOY_DATA_DIR="$ROOT/data" \
	KAIWU_DEPLOY_SERVICE_NAME="kaiwu-absent-$$" KAIWU_DEPLOY_LISTEN_ADDR=127.0.0.1:17331 \
	KAIWU_DEPLOY_HEALTH_URL=http://127.0.0.1:17331 \
	sh "$ROOT/body.sh" stop-check >/dev/null 2>&1
expect_exit "systemctl stop 非零即失败" "$?" 1

systemctl --user kill -s KILL "$UNIT.service" >/dev/null 2>&1 || true
pid=1
for _ in $(seq 1 50); do
	pid=$(systemctl --user show -p MainPID --value "$UNIT.service" 2>/dev/null)
	[ "$pid" = 0 ] && break
	sleep 0.2
done
run_check 5 >/dev/null 2>&1
expect_exit "进程结束后可取得确定停止证据" "$?" 0

printf '\n通过 %d 项，失败 %d 项\n' "$pass" "$failed"
[ "$failed" -eq 0 ]
