#!/bin/sh
set -eu

IFACE=wg0
DNS_SERVERS=/etc/wireguard/dns-servers.conf
DNS_PID=/run/dnsmasq.pid
SPEED_LIMITS=/etc/wireguard/speed-limits.conf
SHAPED=/run/speed-limits.applied
SHAPE_CHAIN=tunploy-shape

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

unshape() {
	iptables -t mangle -F "$SHAPE_CHAIN"
	for dev in "$IFACE" eth0; do
		tc qdisc del dev "$dev" root 2>/dev/null || true
	done
}

# A failed shape must not take the tunnel down with it, and a separate
# process keeps set -e working inside it.
shape() {
	"$0" shape 2>/proc/1/fd/2 || echo "warning: speed limits could not be applied" >/proc/1/fd/2
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
	iptables -t mangle -N "$SHAPE_CHAIN"
	iptables -t mangle -A FORWARD -i "$IFACE" -j "$SHAPE_CHAIN"
	rm -f "$SHAPED"
	shape
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
	shape
	dns
	;;
shape)
	# Each line of SPEED_LIMITS is a peer's address and its rate in kbit/s,
	# each way. Downloads leave through the tunnel addressed to the peer;
	# uploads leave through eth0 after NAT rewrote their source, so a mark
	# set on the way in picks them out there. Rebuilding resets the queues,
	# so it only happens when the limits changed.
	# One read, so the file changing midway cannot mix two versions.
	want=/run/speed-limits.want
	cat "$SPEED_LIMITS" >"$want" 2>/dev/null || : >"$want"
	if [ -e "$SHAPED" ] && cmp -s "$want" "$SHAPED"; then
		exit 0
	fi
	rm -f "$SHAPED"
	unshape
	if [ -s "$want" ]; then
		# Half a setup would hold limited peers to one packet in flight.
		trap unshape EXIT
		# Unlimited peers bypass the classes. The tunnel has no queue of its
		# own, which would leave that bypass two packets deep.
		for dev in "$IFACE" eth0; do
			tc qdisc add dev "$dev" root handle 1: htb direct_qlen 1000
		done
		n=0
		while read -r addr kbit; do
			n=$((n + 1))
			class=$(printf '1:%x' "$n")
			for dev in "$IFACE" eth0; do
				tc class add dev "$dev" parent 1: classid "$class" htb rate "${kbit}kbit"
				# Not every kernel has fq_codel.
				tc qdisc add dev "$dev" parent "$class" fq_codel 2>/dev/null ||
					tc qdisc add dev "$dev" parent "$class" pfifo limit 1000
			done
			tc filter add dev "$IFACE" parent 1: protocol ip prio 1 u32 match ip dst "$addr/32" flowid "$class"
			iptables -t mangle -A "$SHAPE_CHAIN" -s "$addr" -j MARK --set-mark "$n"
			tc filter add dev eth0 parent 1: protocol ip prio 1 handle "$n" fw flowid "$class"
		done <"$want"
		trap - EXIT
	fi
	mv "$want" "$SHAPED"
	;;
*)
	echo "usage: tunploy-wg [run|sync|shape]" >&2
	exit 2
	;;
esac
