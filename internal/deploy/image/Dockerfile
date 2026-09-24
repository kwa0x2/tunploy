FROM alpine:3.22
RUN apk add --no-cache wireguard-tools iproute2 iptables
COPY tunploy-wg.sh /usr/local/bin/tunploy-wg
ENTRYPOINT ["/usr/local/bin/tunploy-wg"]
CMD ["run"]
