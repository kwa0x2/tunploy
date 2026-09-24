#!/bin/sh
set -eu

IFACE=wg0

# The panel follows these markers to show deploy progress.
step() { echo "tunploy:step $1"; }
ready() { echo "tunploy:ready"; }

case "${1:-run}" in
run)
	trap 'wg-quick down "$IFACE"; exit 0' TERM INT
	wg-quick up "$IFACE"
	step interface
	iptables -A FORWARD -i "$IFACE" -j ACCEPT
	iptables -A FORWARD -o "$IFACE" -j ACCEPT
	step firewall
	# Peer traffic leaves with tunnel source addresses that Docker's own NAT
	# does not cover, so masquerade it behind the container's address.
	iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
	step nat
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
	;;
*)
	echo "usage: tunploy-wg [run|sync]" >&2
	exit 2
	;;
esac
