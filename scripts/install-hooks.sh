#!/usr/bin/env bash
# Installs (or removes) the local reference-transaction hook that triggers
# scripts/deploy.sh for committed updates of the configured branch.
#
#   scripts/install-hooks.sh [--repo DIR] [--config FILE]
#   scripts/install-hooks.sh --uninstall [--repo DIR]
#
# The hook is only written into the repository's own hooks directory (or an
# explicit core.hooksPath inside the repository). A shared hooks path outside
# the repository, a disabled hooks path and any foreign existing hook are
# refused instead of overwritten. The configuration file must not be tracked by
# Git and, inside the repository, must be ignored. No global Git configuration
# is changed.
set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$(readlink -f -- "$0")")" && pwd)
# shellcheck source=lib/common.sh
. "$SCRIPT_DIR/lib/common.sh"

MARKER='# kaiwu-local-deployment-hook'
repo=""
config=""
uninstall=0

while [ $# -gt 0 ]; do
	case $1 in
	--repo)
		[ $# -ge 2 ] || kaiwu_die "--repo 需要目录参数"
		repo=$2
		shift 2
		;;
	--config)
		[ $# -ge 2 ] || kaiwu_die "--config 需要文件参数"
		config=$2
		shift 2
		;;
	--uninstall)
		uninstall=1
		shift
		;;
	-h | --help)
		echo "用法：scripts/install-hooks.sh [--repo 仓库] [--config 配置文件] [--uninstall]"
		exit 0
		;;
	*) kaiwu_die "未知参数：$1" ;;
	esac
done

[ -n "$repo" ] || repo=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
repo=$(readlink -f -- "$repo")
[ -n "$config" ] || config=$repo/.kaiwu/local-deployment.conf

# shell_quote makes a value safe inside the generated POSIX hook.
shell_quote() {
	printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

# resolve_hook decides the only hook path this installer may write. An explicit
# core.hooksPath is honoured only when it stays inside the repository; a
# disabled or shared path is refused rather than silently used.
resolve_hook() {
	local hooks_path
	hooks_path=$(git -C "$repo" config --get core.hooksPath 2>/dev/null || true)
	if [ -z "$hooks_path" ]; then
		hook=$(git -C "$repo" rev-parse --path-format=absolute --git-path hooks/reference-transaction 2>/dev/null) || hook=""
		case $hook in
		*/reference-transaction) ;;
		*) hook=$repo/.git/hooks/reference-transaction ;;
		esac
		return 0
	fi
	case $hooks_path in
	/dev/null)
		kaiwu_die "core.hooksPath=/dev/null 已显式停用 Git hooks，拒绝假称安装成功"
		;;
	/*) ;;
	*) hooks_path=$repo/${hooks_path#./} ;;
	esac
	hook=$hooks_path/reference-transaction
	case $hook in
	"$repo"/*) ;;
	*) kaiwu_die "core.hooksPath 指向仓库外的共享目录，拒绝写入：$hooks_path" ;;
	esac
}

# resolve_config refuses a configuration file Git could pick up: it must not be
# tracked, and inside the repository it must be ignored.
resolve_config() {
	local abs rel
	[ -f "$config" ] || kaiwu_die "部署配置不存在：$config（可从 scripts/deployment.conf.example 复制）"
	abs=$(readlink -f -- "$config" 2>/dev/null || printf '%s' "$config")
	case $abs in
	"$repo"/*)
		rel=${abs#"$repo"/}
		if git -c core.hooksPath=/dev/null -C "$repo" ls-files --error-unmatch -- "$rel" >/dev/null 2>&1; then
			kaiwu_die "部署配置已被 Git 跟踪，拒绝继续：$config（请移出仓库或加入 .gitignore）"
		fi
		if ! git -c core.hooksPath=/dev/null -C "$repo" check-ignore -q -- "$rel"; then
			kaiwu_die "部署配置在仓库内但未被忽略，拒绝继续：$config"
		fi
		;;
	esac
	config=$abs
}

resolve_hook

if [ "$uninstall" = 1 ]; then
	[ -f "$hook" ] || kaiwu_die "未安装 reference-transaction hook：$hook"
	if ! grep -qF "$MARKER" "$hook"; then
		kaiwu_die "该 hook 不是本安装脚本创建的，拒绝删除：$hook"
	fi
	rm -f "$hook"
	rmdir "$(dirname "$hook")" 2>/dev/null || true
	printf '已移除 %s\n' "$hook"
	exit 0
fi

[ -x "$SCRIPT_DIR/deploy.sh" ] || kaiwu_die "缺少可执行的 scripts/deploy.sh"
resolve_config
if [ -e "$hook" ]; then
	if ! grep -qF "$MARKER" "$hook"; then
		kaiwu_die "已存在自定义 reference-transaction hook，拒绝覆盖：$hook"
	fi
fi
mkdir -p "$(dirname "$hook")" || kaiwu_die "无法创建 hooks 目录"
# ${1-} is written literally: the hook receives its transaction phase as $1.
cat >"$hook" <<EOF
#!/bin/sh
$MARKER
# Managed by scripts/install-hooks.sh; remove with --uninstall.
exec $(shell_quote "$SCRIPT_DIR/deploy.sh") --hook "\${1-}" --config $(shell_quote "$config") --repo $(shell_quote "$repo")
EOF
chmod 0755 "$hook"

printf '已安装 %s\n' "$hook"
