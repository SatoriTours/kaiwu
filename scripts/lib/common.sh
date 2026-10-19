# Shared POSIX helpers for the Kaiwu local deployment scripts.
#
# This file is sourced by scripts/deploy.sh and concatenated ahead of
# scripts/remote-deploy.sh when that script is piped to the target over SSH, so
# it must stay POSIX sh and must never print secrets.

kaiwu_log() { printf '[kaiwu-deploy] %s\n' "$*" >&2; }
kaiwu_die() { printf '[kaiwu-deploy] 失败：%s\n' "$*" >&2; exit 1; }

# kaiwu_valid_sha <value> accepts exactly one lowercase 40-character Git id.
kaiwu_valid_sha() {
	local s=${1-}
	[ "${#s}" -eq 40 ] || return 1
	case $s in
	*[!0-9a-f]*) return 1 ;;
	esac
	return 0
}

# kaiwu_all_zero <value> is true for Git's all-zero object id (a deletion).
kaiwu_all_zero() {
	local z=${1-}
	[ -n "$z" ] || return 1
	case $z in
	*[!0]*) return 1 ;;
	esac
	return 0
}

kaiwu_now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# kaiwu_safe_path accepts only an absolute path made of a conservative
# character set. Config values are passed to ssh, so shell metacharacters must
# never survive validation.
kaiwu_safe_path() {
	_p=${1-}
	case $_p in /*) ;; *) return 1 ;; esac
	case $_p in *[!A-Za-z0-9._/-]*) return 1 ;; esac
	return 0
}

# kaiwu_port accepts a decimal port between 1 and 65535.
kaiwu_port() {
	_p=${1-}
	case $_p in '' | *[!0-9]*) return 1 ;; esac
	[ "${#_p}" -le 5 ] || return 1
	[ "$_p" -ge 1 ] && [ "$_p" -le 65535 ] || return 1
	return 0
}

# kaiwu_hostport accepts a numeric IPv4 address with a port.
kaiwu_hostport() {
	_hp=${1-}
	_h=${_hp%%:*}
	_port=${_hp##*:}
	[ "$_hp" != "$_h" ] || return 1
	[ -n "$_h" ] && [ -n "$_port" ] || return 1
	case $_h in *[!0-9.]*) return 1 ;; esac
	kaiwu_port "$_port"
}

# kaiwu_health_url accepts exactly an http:// IPv4:port entry without path,
# query, fragment or userinfo.
kaiwu_health_url() {
	_u=${1-}
	case $_u in http://*) ;; *) return 1 ;; esac
	_rest=${_u#http://}
	case $_rest in */* | *'?'* | *'#'* | *'@'*) return 1 ;; esac
	kaiwu_hostport "$_rest"
}

# kaiwu_write_status FILE STATUS SHA AT [ACTIVE_SHA ACTIVE_AT]
# Writes the bounded deployment record atomically. ACTIVE_* describe the last
# successful activation and are preserved by the caller across attempts.
kaiwu_write_status() {
	local file=${1-} status=${2-} sha=${3-} at=${4-} active=${5-} deployed=${6-}
	case $status in
	testing | building | deploying | succeeded | failed) ;;
	*) return 1 ;;
	esac
	kaiwu_valid_sha "$sha" || return 1
	[ -n "$at" ] || return 1
	if [ -n "$active" ]; then kaiwu_valid_sha "$active" || return 1; fi
	local dir tmp
	dir=$(dirname "$file") || return 1
	mkdir -p "$dir" || return 1
	tmp="$file.tmp.$$"
	printf '{\n  "revision": "%s",\n  "deployedAt": "%s",\n  "deployment": {\n    "revision": "%s",\n    "status": "%s",\n    "at": "%s"\n  }\n}\n' \
		"$active" "$deployed" "$sha" "$status" "$at" >"$tmp" || return 1
	chmod 0600 "$tmp" 2>/dev/null || true
	mv -f "$tmp" "$file" || return 1
	return 0
}
