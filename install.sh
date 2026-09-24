#!/bin/sh
# Installs or upgrades Tunploy on a Linux server:
#
#   curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh | sudo sh
#
# Running it again upgrades to the newest image; data in /var/lib/tunploy is kept.
#
# Optional environment, passed after sudo so it survives it
# (curl ... | sudo TUNPLOY_BIND=0.0.0.0 sh):
#   TUNPLOY_IMAGE        image to install (default: ghcr.io/kwa0x2/tunploy)
#   TUNPLOY_VERSION      image tag to install (default: latest)
#   TUNPLOY_PUBLIC_HOST  hostname or IP devices dial (default: this server's public IPv4)
#   TUNPLOY_PORT         panel port (default: 3000)
#   TUNPLOY_BIND         address the panel listens on (default: 127.0.0.1, reachable
#                        over an SSH tunnel; 0.0.0.0 exposes it to the internet)
set -eu

IMAGE=${TUNPLOY_IMAGE:-ghcr.io/kwa0x2/tunploy}
CONTAINER=tunploy
DATA_DIR=/var/lib/tunploy

VERSION=${TUNPLOY_VERSION:-latest}
PORT=${TUNPLOY_PORT:-3000}
BIND=${TUNPLOY_BIND:-127.0.0.1}

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
		fail "could not pull $IMAGE:$VERSION; if the image is private, run 'docker login ${IMAGE%%/*}' first"

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

wait_until_healthy() {
	case "$BIND" in
	0.0.0.0 | "") probe=127.0.0.1 ;;
	*) probe=$BIND ;;
	esac
	i=0
	while [ "$i" -lt 30 ]; do
		curl -fsS --max-time 2 "http://$probe:$PORT/api/health" >/dev/null 2>&1 && return
		i=$((i + 1))
		sleep 1
	done
	docker logs --tail 30 "$CONTAINER" >&2 || true
	fail "Tunploy did not become healthy; the container logs are above"
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
		echo "  and open http://localhost:$PORT to create the admin account."
	else
		echo "  Open http://${host:-<server-ip>}:$PORT and create the admin account now:"
		echo "  until you do, whoever opens the panel first becomes its admin."
	fi
	echo
	if [ -n "$host" ]; then
		echo "  VPN clients will connect to $host."
	else
		echo "  The public IP could not be detected; set it under Settings in the panel."
	fi
	echo "  Allow UDP 51820 (plus one more port per extra server) in your provider's firewall."
	echo
}

main() {
	check_system
	ensure_docker
	check_wireguard
	host=$(detect_public_host)
	install_panel "$host"
	wait_until_healthy
	print_summary "$host"
}

main "$@"
