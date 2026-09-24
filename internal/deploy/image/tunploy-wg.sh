#!/bin/sh
set -eu

IFACE=wg0

case "${1:-run}" in
run)
	trap 'wg-quick down "$IFACE"; exit 0' TERM INT
	wg-quick up "$IFACE"
	# Peer traffic leaves with tunnel source addresses that Docker's own NAT
	# does not cover, so masquerade it behind the container's address.
	iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
	iptables -A FORWARD -i "$IFACE" -j ACCEPT
	iptables -A FORWARD -o "$IFACE" -j ACCEPT
	echo "wireguard $IFACE is up"
	while :; do
		sleep 3600 &
		wait $!
	done
	;;
sync)
	# Applies peer changes without dropping existing sessions.
	wg-quick strip "$IFACE" >"/tmp/$IFACE.conf"
	wg syncconf "$IFACE" "/tmp/$IFACE.conf"
	;;
*)
	echo "usage: tunploy-wg [run|sync]" >&2
	exit 2
	;;
esac
