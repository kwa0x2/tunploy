#!/bin/sh
# Installs or upgrades Tunploy on a Linux server:
#
#   curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh | sudo sh
#
# Running it again upgrades to the newest image; data in /var/lib/tunploy is kept.
#
# Optional environment, passed after sudo so it survives it
# (curl ... | sudo TUNPLOY_PORT=8080 sh):
#   TUNPLOY_IMAGE        image to install (default: ghcr.io/kwa0x2/tunploy)
#   TUNPLOY_VERSION      image tag to install (default: latest)
#   TUNPLOY_PUBLIC_HOST  hostname or IP devices dial (default: this server's public IPv4)
#   TUNPLOY_PORT         panel port (default: 3000)
#   TUNPLOY_BIND         address the panel listens on (default: 0.0.0.0, reachable
#                        from the internet; 127.0.0.1 keeps it behind an SSH tunnel)
#   TUNPLOY_ADMIN_NAME, TUNPLOY_ADMIN_EMAIL, TUNPLOY_ADMIN_PASSWORD
#                        create the admin account without prompting
set -eu

IMAGE=${TUNPLOY_IMAGE:-ghcr.io/kwa0x2/tunploy}
CONTAINER=tunploy
DATA_DIR=/var/lib/tunploy

VERSION=${TUNPLOY_VERSION:-latest}
PORT=${TUNPLOY_PORT:-3000}
# Public by default: there is no sign-up page to race for, since the admin
# is created below by whoever runs this script.
BIND=${TUNPLOY_BIND:-0.0.0.0}

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
	# The data dir has the same path on both sides: Tunploy hands directories
	# under it to the VPN containers, and the daemon resolves them on the host.
	docker run -d \
		--name "$CONTAINER" \
		--restart unless-stopped \
		-p "$BIND:$PORT:3000" \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v "$DATA_DIR:$DATA_DIR" \
		-e TUNPLOY_PUBLIC_HOST="$1" \
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
		warn "  docker exec -it $CONTAINER tunploy admin create"
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
	fail "no admin account was created; run 'docker exec -it $CONTAINER tunploy admin create' to try again"
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
		echo
		echo "  The panel speaks plain HTTP, so avoid signing in over networks you do not trust."
	fi
	echo
	case "${ADMIN:-}" in
	"") echo "  Create the admin account first: docker exec -it $CONTAINER tunploy admin create" ;;
	existing) echo "  Sign in with your existing admin account." ;;
	*) echo "  Sign in as $ADMIN." ;;
	esac
	if [ -n "$host" ]; then
		echo "  VPN clients will connect to $host."
	else
		echo "  The public IP could not be detected; set it under Settings in the panel."
	fi
	if [ "$BIND" = 127.0.0.1 ]; then
		echo "  Allow UDP 51820 (plus one more port per extra server) in your provider's firewall."
	else
		echo "  Allow TCP $PORT for the panel and UDP 51820 (plus one more port per extra server)"
		echo "  in your provider's firewall, if it has one."
	fi
	echo
}

main() {
	# A Ctrl-C during the password prompt must not leave the terminal mute.
	trap 'stty echo </dev/tty 2>/dev/null || true' EXIT
	trap 'exit 130' INT TERM
	check_system
	ensure_docker
	check_wireguard
	host=$(detect_public_host)
	install_panel "$host"
	wait_until_healthy
	ensure_admin
	print_summary "$host"
}

main "$@"
