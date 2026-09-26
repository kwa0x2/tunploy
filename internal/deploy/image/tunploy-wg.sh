#!/bin/sh
set -eu

IFACE=wg0
DNS_SERVERS=/etc/wireguard/dns-servers.conf
DNS_PID=/run/dnsmasq.pid

# The panel follows these markers to show deploy progress.
step() { echo "tunploy:step $1"; }
ready() { echo "tunploy:ready"; }

# The panel writes DNS_SERVERS when clients should ask this server for DNS.
# Root, so a reload can still read the file the panel keeps root-only; its
# log goes to the container's.
dns() {
	if [ -f "$DNS_SERVERS" ]; then
		if [ -f "$DNS_PID" ] && kill -0 "$(cat "$DNS_PID")" 2>/dev/null; then
			kill -HUP "$(cat "$DNS_PID")"
			return
		fi
		addr=$(ip -4 -o addr show dev "$IFACE" | awk '{ split($4, a, "/"); print a[1]; exit }')
		dnsmasq --conf-file=/dev/null --user=root --no-resolv --no-hosts --servers-file="$DNS_SERVERS" \
			--listen-address="$addr" --bind-interfaces --cache-size=10000 --domain-needed --bogus-priv \
			--pid-file="$DNS_PID" --log-facility=/proc/1/fd/1
	elif [ -f "$DNS_PID" ]; then
		kill "$(cat "$DNS_PID")" 2>/dev/null || true
		rm -f "$DNS_PID"
	fi
}

case "${1:-run}" in
run)
	trap 'wg-quick down "$IFACE"; exit 0' TERM INT
	wg-quick up "$IFACE"
	step interface
	iptables -A FORWARD -i "$IFACE" -j ACCEPT
	iptables -A FORWARD -o "$IFACE" -j ACCEPT
	# The tunnel's smaller MTU would otherwise leave TCP sending segments that
	# only get through fragmented, or not at all where ICMP is filtered: sites
	# that load slowly or hang half way.
	iptables -t mangle -A FORWARD -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu ||
		echo "warning: TCP MSS clamping is unavailable on this kernel" >&2
	step firewall
	# Peer traffic leaves with tunnel source addresses that Docker's own NAT
	# does not cover, so masquerade it behind the container's address.
	iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
	step nat
	dns
	echo "wireguard $IFACE is up"
	ready
	while :; do
		sleep 3600 &
		wait $!
	done
	;;
sync)
	# Applies peer changes without dropping existing sessions.
	wg-quick strip "$IFACE" >"/tmp/$IFACE.conf"
	wg syncconf "$IFACE" "/tmp/$IFACE.conf"
	dns
	;;
*)
	echo "usage: tunploy-wg [run|sync]" >&2
	exit 2
	;;
esac
