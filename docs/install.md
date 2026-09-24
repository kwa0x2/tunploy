# Installing Tunploy

Tunploy runs as a single Docker container on a Linux server and starts one more container for each WireGuard server you create. You need:

- A Linux server with a public IP, amd64 or arm64. Any distribution with Linux 5.6 or newer works; Ubuntu 20.04+ and Debian 11+ are fine.
- Root access over SSH.
- A UDP port open to the internet for each VPN server, starting at 51820.

Tunploy installs Docker if it is missing. You do not install WireGuard yourself: the tools ship inside Tunploy's image, and the kernel module is already part of Linux 5.6+.

## One-line install

```sh
curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh | sudo sh
```

The script:

1. Installs Docker if it is missing.
2. Checks that the WireGuard kernel module can load.
3. Detects the server's public IPv4 address, which VPN clients will connect to.
4. Pulls `ghcr.io/kwa0x2/tunploy:latest` and starts it with its data in `/var/lib/tunploy`.
5. Waits until the panel answers, then prints how to reach it.

To upgrade later, run the same command again. It pulls the newest image and replaces the container; your servers, peers and account stay in `/var/lib/tunploy`.

### Options

Pass options after `sudo`, because `sudo` drops the rest of your environment:

```sh
curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh \
  | sudo TUNPLOY_PUBLIC_HOST=vpn.example.com sh
```

| Variable | Default | Meaning |
| --- | --- | --- |
| `TUNPLOY_PUBLIC_HOST` | detected public IPv4 | Hostname or IP that VPN clients dial. |
| `TUNPLOY_BIND` | `127.0.0.1` | Address the panel listens on. `0.0.0.0` exposes it to the internet. |
| `TUNPLOY_PORT` | `3000` | Panel port. |
| `TUNPLOY_VERSION` | `latest` | Image tag, for example `0.1.0` or `edge`. |
| `TUNPLOY_IMAGE` | `ghcr.io/kwa0x2/tunploy` | Image to install, for forks and mirrors. |

## First sign-in over an SSH tunnel

By default the panel listens only on the server itself. This matters: **the first person to open a fresh panel becomes its admin**, and the panel serves plain HTTP. Reach it through SSH instead, which encrypts the connection and keeps everyone else out.

On your own computer:

```sh
ssh -L 3000:localhost:3000 root@YOUR_SERVER_IP
```

Leave that terminal open, browse to <http://localhost:3000> and create the admin account.

If you set `TUNPLOY_BIND=0.0.0.0`, create the admin account right after installing, before anyone else finds the port. For anything beyond a quick test, put the panel behind a reverse proxy with HTTPS and set `TUNPLOY_SECURE_COOKIES=true`.

## Open the WireGuard port

Each VPN server listens on its own UDP port: the first on 51820, the next on 51821, and so on. Docker publishes these ports itself, past host firewalls such as `ufw`, so there is nothing to open on the server. Your hosting provider's firewall is separate: in Hetzner, AWS, Oracle Cloud, GCP and most others, add an inbound rule for **UDP 51820** (and each further port you use) in the provider's console.

## Create your first VPN

1. Go to **Servers → New server**, give it a name and press **Deploy**.
2. Open the server and choose **Add peer**. Scan the QR code with the WireGuard app on your phone, or download the `.conf` file for a laptop.
3. Connect. The peer shows up as **Online** within a few seconds.

The endpoint that clients connect to comes from **Settings → Public host**. If the install script detected the wrong address, change it there before creating servers. Existing servers keep their own endpoint, which you can change under each server's settings.

## Manual install with Docker Compose

If you would rather not pipe a script into a shell, this starts the same container:

```yaml
services:
  tunploy:
    image: ghcr.io/kwa0x2/tunploy:latest
    container_name: tunploy
    restart: unless-stopped
    ports:
      - "127.0.0.1:3000:3000"
    environment:
      TUNPLOY_PUBLIC_HOST: "YOUR_SERVER_IP"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      # Must be the same path on both sides.
      - /var/lib/tunploy:/var/lib/tunploy
```

```sh
docker compose up -d
```

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `TUNPLOY_LISTEN` | `:3000` | Address inside the container. |
| `TUNPLOY_DATA_DIR` | `/var/lib/tunploy` | Database and WireGuard configs. Mount it at the same path on the host. |
| `TUNPLOY_PUBLIC_HOST` | empty | Default endpoint for new servers, used while **Settings → Public host** is empty. |
| `TUNPLOY_SESSION_TTL` | `168h` | How long a sign-in lasts. |
| `TUNPLOY_SECURE_COOKIES` | `false` | Set to `true` when the panel is served over HTTPS. |
| `TUNPLOY_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |

## Troubleshooting

**The panel does not come up.** Check its logs with `docker logs tunploy`. `permission denied` on `docker.sock` means the socket is not mounted, or a rootless Docker is in use.

**A server shows as stopped or keeps restarting.** Open the server and choose **View logs**. `RTNETLINK answers: Operation not supported` means the host kernel has no WireGuard module. Run `sudo modprobe wireguard`; if that fails, the VPS type (often OpenVZ or LXC) does not support WireGuard.

**The peer never comes online.** The UDP port is almost always blocked by the provider's firewall. Also check that the endpoint in the client config is the server's public address and not `localhost` or a private IP.

**Connected, but no internet.** Check that the server itself has outbound connectivity, and that no host firewall rule drops forwarded traffic.

**Forgot the admin password.** Stop the panel and remove the database: `docker rm -f tunploy && sudo rm /var/lib/tunploy/tunploy.db`, then run the installer again. This deletes all servers and peers; their containers are removed on the next start.
