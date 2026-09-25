#!/bin/sh
# Installs or upgrades Tunploy on a Linux server:
#
#   curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh | sudo sh
#
# Running it again upgrades to the newest image; data in /var/lib/tunploy is kept.
# It also installs the tunploy command (/usr/local/bin/tunploy) for managing
# the panel from the server; run "tunploy help" to see what it does.
#
# To remove Tunploy and every VPN server it runs:
#
#   curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh | sudo sh -s uninstall
#
# It asks before deleting /var/lib/tunploy; --purge deletes it without asking,
# and --yes skips the confirmation, for a run without a terminal.
#
# Optional environment, passed after sudo so it survives it
# (curl ... | sudo TUNPLOY_PORT=8080 sh):
#   TUNPLOY_IMAGE        image to install (default: ghcr.io/kwa0x2/tunploy)
#   TUNPLOY_VERSION      image tag to install (default: latest)
#   TUNPLOY_PUBLIC_HOST  hostname or IP devices dial (default: this server's public IPv4)
#   TUNPLOY_PORT         panel port (default: 3000)
#   TUNPLOY_BIND         address the panel listens on (default: 0.0.0.0, reachable
#                        from the internet; 127.0.0.1 keeps it behind an SSH tunnel)
#   TUNPLOY_HTTPS        false keeps TCP 80 and 443 for something else; the panel then
#                        can't serve its own HTTPS domain (default: true)
#   TUNPLOY_TRUSTED_PROXIES
#                        CIDRs of your own reverse proxy, whose X-Forwarded-For is believed
#   TUNPLOY_UPDATE_CHECK false stops the panel from checking GitHub for new releases
#   TUNPLOY_TIMEZONE     time zone for daily and monthly data usage, e.g. Europe/Istanbul
#                        (default: this server's time zone)
#   TUNPLOY_ADMIN_NAME, TUNPLOY_ADMIN_EMAIL, TUNPLOY_ADMIN_PASSWORD
#                        create the admin account without prompting
set -eu

IMAGE=${TUNPLOY_IMAGE:-ghcr.io/kwa0x2/tunploy}
CONTAINER=tunploy
DATA_DIR=/var/lib/tunploy
CLI=/usr/local/bin/tunploy

VERSION=${TUNPLOY_VERSION:-latest}

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
fail() {
	printf '\033[1;31merror:\033[0m %s\n' "$*" >&2
	exit 1
}

check_system() {
	[ "$(id -u)" -eq 0 ] || fail "run this as root, e.g. curl -fsSL <url> | sudo sh"
	[ "$(uname -s)" = Linux ] || fail "Tunploy runs on Linux servers only"
	case "$(uname -m)" in
	x86_64 | amd64 | aarch64 | arm64) ;;
	*) fail "unsupported CPU architecture $(uname -m); images exist for amd64 and arm64" ;;
	esac
	command -v curl >/dev/null || fail "curl is required"
}

ensure_docker() {
	if ! command -v docker >/dev/null; then
		info "Installing Docker"
		curl -fsSL https://get.docker.com | sh
	fi
	if ! docker info >/dev/null 2>&1 && command -v systemctl >/dev/null; then
		systemctl enable --now docker
	fi
	docker info >/dev/null 2>&1 || fail "Docker is installed but not running; start it and run this again"
}

# WireGuard itself ships inside Tunploy's image, but the kernel module
# (built into Linux 5.6+) must come from the host.
check_wireguard() {
	command -v modprobe >/dev/null && modprobe wireguard 2>/dev/null || true
	[ -d /sys/module/wireguard ] && return
	warn "the WireGuard kernel module is not loaded, so VPN servers will fail to start."
	warn "Linux 5.6 and newer include it; some container-based VPS types (OpenVZ, LXC) do not."
}

# Reads a setting from the running container, so an upgrade keeps it.
current_env() {
	docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$CONTAINER" 2>/dev/null |
		sed -n "s/^$1=//p"
}

current_binding() {
	docker inspect -f '{{range index .HostConfig.PortBindings "3000/tcp"}}{{or .HostIp "0.0.0.0"}} {{.HostPort}}{{end}}' \
		"$CONTAINER" 2>/dev/null || true
}

# Settings from an earlier install survive the upgrade unless given again.
choose_settings() {
	TRUSTED_PROXIES=${TUNPLOY_TRUSTED_PROXIES:-$(current_env TUNPLOY_TRUSTED_PROXIES)}
	TIMEZONE=${TUNPLOY_TIMEZONE:-$(current_env TZ)}
	TIMEZONE=${TIMEZONE:-$(host_timezone)}
	UPDATE_CHECK=${TUNPLOY_UPDATE_CHECK:-$(current_env TUNPLOY_UPDATE_CHECK)}
	HTTPS=${TUNPLOY_HTTPS:-$(current_env TUNPLOY_HTTPS)}
	HTTPS=${HTTPS:-true}

	# shellcheck disable=SC2046
	set -- $(current_binding)
	PORT=${TUNPLOY_PORT:-${2:-3000}}
	# Public by default: there is no sign-up page to race for, since the admin
	# is created below by whoever runs this script.
	BIND=${TUNPLOY_BIND:-${1:-0.0.0.0}}
}

host_timezone() {
	tz=$(timedatectl show -p Timezone --value 2>/dev/null || true)
	if [ -z "$tz" ] && [ -f /etc/timezone ]; then
		tz=$(cat /etc/timezone)
	fi
	if [ -z "$tz" ]; then
		tz=$(readlink /etc/localtime 2>/dev/null | sed -n 's|.*/zoneinfo/||p')
	fi
	echo "${tz:-UTC}"
}

detect_public_host() {
	if [ -n "${TUNPLOY_PUBLIC_HOST:-}" ]; then
		echo "$TUNPLOY_PUBLIC_HOST"
		return
	fi
	for url in https://api.ipify.org https://ifconfig.me/ip https://icanhazip.com; do
		ip=$(curl -4 -fsS --max-time 5 "$url" 2>/dev/null | tr -d '[:space:]') || continue
		if echo "$ip" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$'; then
			echo "$ip"
			return
		fi
	done
}

install_panel() {
	set --
	info "Pulling $IMAGE:$VERSION"
	docker pull "$IMAGE:$VERSION" ||
		fail "could not pull $IMAGE:$VERSION; check that this tag is published, or run 'docker login ${IMAGE%%/*}' if the image is private"

	# Pull first, so a failed download leaves the running panel alone.
	if docker inspect "$CONTAINER" >/dev/null 2>&1; then
		info "Replacing the existing $CONTAINER container"
		docker rm -f "$CONTAINER" >/dev/null
	fi

	mkdir -p "$DATA_DIR"
	info "Starting Tunploy"
	set -- "$@" -p "$BIND:$PORT:3000" -e TZ="$TIMEZONE"
	if [ -n "$TRUSTED_PROXIES" ]; then
		set -- "$@" -e TUNPLOY_TRUSTED_PROXIES="$TRUSTED_PROXIES"
	fi
	if [ -n "$UPDATE_CHECK" ]; then
		set -- "$@" -e TUNPLOY_UPDATE_CHECK="$UPDATE_CHECK"
	fi
	if [ "$HTTPS" = false ]; then
		HTTPS_PORTS=
		run_panel "$@" -e TUNPLOY_HTTPS=false
		return
	fi

	# 80 and 443 let the panel serve the domain set under Settings. When a web
	# server already has them, the panel still runs, on its own port only.
	HTTPS_PORTS=yes
	err=$(run_panel "$@" -p 80:80 -p 443:443 2>&1) && return
	docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
	if ! printf '%s' "$err" | grep -Eq ':(80|443)[^0-9]'; then
		printf '%s\n' "$err" >&2
		fail "could not start the Tunploy container"
	fi
	HTTPS_PORTS=
	warn "TCP 80 or 443 is taken by another program, so the panel can't serve HTTPS itself."
	warn "Free both and run this again, or put the panel behind your web server:"
	warn "https://github.com/kwa0x2/tunploy/blob/main/README.md#https"
	run_panel "$@"
}

# The data dir has the same path on both sides: Tunploy hands directories
# under it to the VPN containers, and the daemon resolves them on the host.
run_panel() {
	docker run -d \
		--name "$CONTAINER" \
		--restart unless-stopped \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v "$DATA_DIR:$DATA_DIR" \
		-e TUNPLOY_PUBLIC_HOST="$PUBLIC_HOST" \
		"$@" \
		"$IMAGE:$VERSION" >/dev/null
}

panel_url() {
	case "$BIND" in
	0.0.0.0 | "") echo "http://127.0.0.1:$PORT" ;;
	*) echo "http://$BIND:$PORT" ;;
	esac
}

wait_until_healthy() {
	i=0
	while [ "$i" -lt 30 ]; do
		curl -fsS --max-time 2 "$(panel_url)/api/health" >/dev/null 2>&1 && return
		i=$((i + 1))
		sleep 1
	done
	docker logs --tail 30 "$CONTAINER" >&2 || true
	fail "Tunploy did not become healthy; the container logs are above"
}

# The binary stays in the container, so the commands always match the panel's
# database, including right after an in-panel update.
install_cli() {
	mkdir -p "${CLI%/*}"
	sed "s|@IMAGE@|$IMAGE|" >"$CLI" <<'CLI'
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
CLI
	chmod 755 "$CLI"
}

# The panel has no sign-up page, so the admin is created here, by whoever
# has root on the server.
ensure_admin() {
	if ! curl -fsS --max-time 5 "$(panel_url)/api/setup" | grep -q '"setup_required":true'; then
		ADMIN=existing
		return
	fi

	if [ -n "${TUNPLOY_ADMIN_EMAIL:-}" ] && [ -n "${TUNPLOY_ADMIN_PASSWORD:-}" ]; then
		create_admin "${TUNPLOY_ADMIN_NAME:-Admin}" "$TUNPLOY_ADMIN_EMAIL" "$TUNPLOY_ADMIN_PASSWORD" ||
			fail "could not create the admin account"
		ADMIN=$TUNPLOY_ADMIN_EMAIL
		return
	fi

	# Piped into sh, stdin is the script itself; questions go to the terminal.
	if ! (: </dev/tty) 2>/dev/null; then
		warn "no terminal to ask for the admin account; create it with:"
		warn "  tunploy admin create"
		return
	fi

	echo
	info "Create the admin account"
	tries=0
	while [ "$tries" -lt 3 ]; do
		tries=$((tries + 1))
		ask "  Name: "
		name=$answer
		ask "  Email: "
		email=$answer
		ask_secret "  Password (at least 8 characters): "
		password=$answer
		ask_secret "  Repeat password: "
		if [ "$password" != "$answer" ]; then
			warn "the passwords do not match, try again"
		elif create_admin "$name" "$email" "$password"; then
			ADMIN=$email
			return
		fi
	done
	fail "no admin account was created; run 'tunploy admin create' to try again"
}

# The password travels on stdin: as an argument, ps would show it.
create_admin() {
	printf '%s\n' "$3" | docker exec -i "$CONTAINER" tunploy admin create --name "$1" --email "$2"
}

# Both leave the reply in $answer: under sudo-rs, reading /dev/tty inside
# $(...) hangs after the first prompt.
ask() {
	answer=
	while [ -z "$answer" ]; do
		printf '%s' "$1" >/dev/tty
		IFS= read -r answer </dev/tty || fail "no answer"
	done
}

ask_secret() {
	printf '%s' "$1" >/dev/tty
	stty -echo </dev/tty
	IFS= read -r answer </dev/tty || answer=
	stty echo </dev/tty
	printf '\n' >/dev/tty
}

print_summary() {
	host=$1
	echo
	info "Tunploy is running"
	echo
	if [ "$BIND" = 127.0.0.1 ]; then
		echo "  The panel only listens on this server. From your own computer, run:"
		echo
		echo "    ssh -L $PORT:localhost:$PORT ${SUDO_USER:-root}@${host:-<server-ip>}"
		echo
		echo "  and open http://localhost:$PORT."
	else
		echo "  Open http://${host:-<server-ip>}:$PORT"
	fi
	if [ -n "$HTTPS_PORTS" ]; then
		echo
		echo "  For HTTPS, point a domain's A record at ${host:-this server} and add the"
		echo "  domain under Settings -> Domain. The certificate is free and renews itself."
	fi
	echo
	case "${ADMIN:-}" in
	"") echo "  Create the admin account first: tunploy admin create" ;;
	existing) echo "  Sign in with your existing admin account." ;;
	*) echo "  Sign in as $ADMIN." ;;
	esac
	if [ -n "$host" ]; then
		echo "  VPN clients will connect to $host."
	else
		echo "  The public IP could not be detected; set it under Settings in the panel."
	fi
	tcp=
	[ "$BIND" = 127.0.0.1 ] || tcp=$PORT
	[ -n "$HTTPS_PORTS" ] && tcp="${tcp:+$tcp, }80, 443"
	if [ -n "$tcp" ]; then
		echo "  Allow TCP $tcp and UDP 51820 (plus one more UDP port per extra server)"
	else
		echo "  Allow UDP 51820 (plus one more port per extra server)"
	fi
	echo "  in your provider's firewall, if it has one."
	echo
	echo "  Manage the panel from this server with the tunploy command; see: tunploy help"
	echo
}

# Asks a yes/no question on the terminal; no is the default.
confirm() {
	printf '%s [y/N] ' "$1" >/dev/tty
	IFS= read -r reply </dev/tty || reply=
	case "$reply" in
	[yY] | [yY][eE][sS]) return 0 ;;
	*) return 1 ;;
	esac
}

uninstall() {
	purge=
	yes=
	for arg in "$@"; do
		case "$arg" in
		--purge) purge=yes ;;
		--yes | -y) yes=yes ;;
		*) fail "unknown option $arg for uninstall; use --purge or --yes" ;;
		esac
	done
	[ "$(id -u)" -eq 0 ] || fail "run this as root, e.g. curl -fsSL <url> | sudo sh -s uninstall"

	tty=
	(: </dev/tty) 2>/dev/null && tty=yes
	if [ -z "$yes" ]; then
		[ -n "$tty" ] || fail "no terminal to confirm on; run again with: sh -s uninstall --yes"
		echo
		echo "  This removes the Tunploy panel and every VPN server it runs;"
		echo "  connected devices lose their connection."
		echo
		confirm "Uninstall Tunploy?" || fail "nothing was removed"
	fi

	if command -v docker >/dev/null && docker info >/dev/null 2>&1; then
		remove_containers
	else
		warn "Docker is not running, so no containers were removed"
	fi

	if [ -f "$CLI" ]; then
		info "Removing $CLI"
		rm -f "$CLI"
	fi

	if [ -d "$DATA_DIR" ]; then
		if [ -z "$purge" ] && [ -z "$yes" ] &&
			confirm "Also delete $DATA_DIR, with every server, device key, backup and the admin account?"; then
			purge=yes
		fi
		if [ -n "$purge" ]; then
			info "Deleting $DATA_DIR"
			rm -rf "$DATA_DIR"
		fi
	fi

	echo
	info "Tunploy is uninstalled"
	echo
	if [ -d "$DATA_DIR" ]; then
		echo "  Your data is still in $DATA_DIR; installing again picks it up."
		echo "  To delete it: sudo rm -rf $DATA_DIR"
	fi
	echo "  Docker itself is still installed, since other programs may use it."
	echo
}

remove_containers() {
	# Read before the container goes: an install from a fork or a mirror
	# pulled a different image.
	images=$(docker inspect -f '{{.Config.Image}}' "$CONTAINER" 2>/dev/null || true)
	images=${images%@*}
	case "${images##*/}" in *:*) images=${images%:*} ;; esac
	images="$images $IMAGE"

	# The updater and the container it swaps out exist only mid-update.
	for name in "$CONTAINER-updater" "$CONTAINER-previous" "$CONTAINER"; do
		if docker inspect "$name" >/dev/null 2>&1; then
			info "Removing the $name container"
			docker rm -f "$name" >/dev/null
		fi
	done

	servers=$(docker ps -aq --filter label=io.tunploy.managed=true)
	if [ -n "$servers" ]; then
		info "Removing the VPN servers"
		# shellcheck disable=SC2086
		docker rm -f $servers >/dev/null
	fi

	info "Removing Tunploy's images"
	# The panel's by name, so an image that is also tagged otherwise stays.
	refs=$(
		{
			docker images -q --filter label=io.tunploy.managed=true
			for repo in $images; do
				docker images --format '{{.Repository}}:{{.Tag}}' "$repo" | grep -v ':<none>$' || true
			done
		} | sort -u
	)
	if [ -n "$refs" ]; then
		# shellcheck disable=SC2086
		docker rmi -f $refs >/dev/null 2>&1 || warn "some images are still in use and were kept"
	fi
}

install_or_upgrade() {
	[ $# -eq 0 ] || fail "unknown argument $1; the commands are install and uninstall"
	check_system
	ensure_docker
	check_wireguard
	choose_settings
	PUBLIC_HOST=$(detect_public_host)
	install_panel
	install_cli
	wait_until_healthy
	ensure_admin
	print_summary "$PUBLIC_HOST"
}

main() {
	# A Ctrl-C during a prompt must not leave the terminal mute.
	trap 'stty echo 2>/dev/null </dev/tty || true' EXIT
	trap 'exit 130' INT TERM
	case "${1:-install}" in
	install)
		[ $# -eq 0 ] || shift
		install_or_upgrade "$@"
		;;
	uninstall)
		shift
		uninstall "$@"
		;;
	*) fail "unknown command $1; the commands are install and uninstall" ;;
	esac
}

main "$@"
