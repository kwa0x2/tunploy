#!/bin/sh
# Manages the Tunploy panel on this server. Installed by Tunploy's install
# script; the commands themselves run in the panel's container.
set -eu

CONTAINER=tunploy
IMAGE=@IMAGE@
INSTALL_URL=https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh

usage() {
	cat <<EOF
usage: tunploy <command> [arguments]

commands:
  admin            create the admin account, reset its password or turn off 2FA
  backup           list the backups in S3 and restore one
  logs             follow the panel's logs
  restart          restart the panel
  version          print the version the panel runs
  update [version] update to the newest release, or to the given one
  uninstall        remove Tunploy and every VPN server it runs

Run "tunploy admin" or "tunploy backup" to see their commands.
EOF
}

fail() {
	echo "error: $*" >&2
	exit 1
}

need_panel() {
	[ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null)" = true ] ||
		fail "the $CONTAINER container is not running; \"tunploy logs\" shows why"
}

in_panel() {
	need_panel
	if [ -t 0 ] && [ -t 1 ]; then
		exec docker exec -it "$CONTAINER" tunploy "$@"
	fi
	exec docker exec -i "$CONTAINER" tunploy "$@"
}

# A file on this server is copied into the container, which can't see it.
restore() {
	need_panel
	n=$#
	prev=
	while [ "$n" -gt 0 ]; do
		arg=$1
		shift
		n=$((n - 1))
		if [ "$prev" != --s3 ] && [ "${arg#-}" = "$arg" ] && [ -f "$arg" ]; then
			dest=/tmp/$(basename "$arg")
			docker cp "$arg" "$CONTAINER:$dest" >/dev/null
			arg=$dest
		fi
		set -- "$@" "$arg"
		prev=$arg
	done
	in_panel backup restore "$@"
}

installer() {
	command -v curl >/dev/null || fail "curl is required"
	curl -fsSL "$INSTALL_URL" | TUNPLOY_IMAGE=$IMAGE TUNPLOY_VERSION=${version:-latest} sh -s "$@"
}

# Docker needs root unless you are in the docker group; the installer always does.
if [ "$(id -u)" -ne 0 ]; then
	case "${1:-help}" in
	help | -h | --help) ;;
	update | uninstall) exec sudo "$0" "$@" ;;
	*) docker info >/dev/null 2>&1 || exec sudo "$0" "$@" ;;
	esac
fi

case "${1:-help}" in
admin) in_panel "$@" ;;
backup)
	if [ "${2:-}" = restore ]; then
		shift 2
		restore "$@"
	fi
	in_panel "$@"
	;;
logs)
	shift
	exec docker logs -f --tail 100 "$@" "$CONTAINER"
	;;
restart)
	docker restart "$CONTAINER" >/dev/null
	echo "Tunploy restarted."
	;;
version)
	# The label also answers for a stopped panel, or one from before this command.
	v=$(docker inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$CONTAINER" 2>/dev/null) ||
		fail "Tunploy is not installed here"
	if [ -n "$v" ]; then
		echo "$v"
	else
		need_panel
		docker exec "$CONTAINER" tunploy version
	fi
	;;
update)
	[ $# -le 2 ] || fail "usage: tunploy update [version]"
	version=${2:-}
	version=${version#v}
	installer install
	;;
uninstall)
	shift
	installer uninstall "$@"
	;;
help | -h | --help) usage ;;
*)
	printf 'unknown command %s\n\n' "$1" >&2
	usage >&2
	exit 2
	;;
esac
