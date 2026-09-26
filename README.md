<div align="center">
  <img src="web/public/favicon.svg" alt="Tunploy" width="72" height="72">
  <h1>Tunploy</h1>
  <p>A self-hosted control panel for your own WireGuard VPN servers.</p>

  [![CI](https://github.com/kwa0x2/tunploy/actions/workflows/ci.yml/badge.svg)](https://github.com/kwa0x2/tunploy/actions/workflows/ci.yml)
  [![Release](https://img.shields.io/github/v/release/kwa0x2/tunploy)](https://github.com/kwa0x2/tunploy/releases)
  [![License](https://img.shields.io/github/license/kwa0x2/tunploy)](LICENSE)

  [Website](https://tunploy.alperkarakoyun.com) · [Docs](https://tunploy.alperkarakoyun.com/docs) · [API](https://tunploy.alperkarakoyun.com/docs/api) · [Releases](https://github.com/kwa0x2/tunploy/releases)
</div>

Tunploy runs as a single Docker container on your Linux server. From its web panel you spin up WireGuard servers, add devices, scan their QR codes, and see who is connected and how much they use, without ever touching a WireGuard config by hand.

## Features

- **One-click VPN servers.** Each WireGuard server runs in its own container, on its own UDP port, with its own DNS, MTU, keepalive and allowed IPs.
- **More machines from one panel.** Add another VPS with its SSH login and run VPN servers there too; nothing is installed on it but Docker.
- **Devices.** Add a device and scan its QR code with the WireGuard app, or download its `.conf`. Turn devices off, give them a data limit (per month or in total), a speed limit or an expiry date.
- **Share links.** Send a device's owner a link to a page with its QR code, config and remaining data, with no account on the panel.
- **Live status and usage.** See which devices are online, from which country, and their daily and monthly traffic.
- **Activity log.** Connections, changes and sign-ins, in one place.
- **HTTPS from the panel.** Point a domain at the server and the panel gets its own Let's Encrypt certificate.
- **Two-factor sign-in** with any authenticator app.
- **Email notifications** when servers go down, devices hit their limit, sign-ins fail and more.
- **Backups** to your computer or any S3-compatible storage, on a schedule, optionally encrypted.
- **An HTTP API** with scoped keys and signed webhooks, so a billing backend, bot or script can create and manage devices and hear when they run out of data. Described in OpenAPI.
- **In-panel updates** that roll back on their own if the new version doesn't start.
- **A `tunploy` command** on the server for resetting the admin password, restoring backups, reading logs and uninstalling.

## Installation

You need:

- A Linux server with a public IP, amd64 or arm64. Any distribution with Linux 5.6 or newer works; Ubuntu 20.04+ and Debian 11+ are fine.
- Root access over SSH.
- A UDP port open to the internet for each VPN server, starting at 51820.

Then run:

```sh
curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh | sudo sh
```

Open `http://YOUR_SERVER_IP:3000`, sign in, go to **Servers → New server** and press **Deploy**, then **Add peer** and scan the QR code with the WireGuard app on your phone. The [first VPN guide](https://tunploy.alperkarakoyun.com/docs/first-vpn) walks through it.

You do not install WireGuard yourself: the tools ship inside Tunploy's image, and the kernel module is already part of Linux 5.6+.

### What the script does

1. Installs Docker if it is missing.
2. Checks that the WireGuard kernel module can load.
3. Detects the server's public IPv4 address, which VPN clients will connect to.
4. Pulls `ghcr.io/kwa0x2/tunploy:latest` and starts it with its data in `/var/lib/tunploy`.
5. Waits until the panel answers.
6. Installs the `tunploy` command (see [Server commands](#server-commands)).
7. Asks for your name, email and password and creates the admin account.
8. Prints how to reach the panel.

The panel has no sign-up page. The admin account can only be created on the server, so nobody who stumbles on the panel can claim it.

Running the same command again upgrades Tunploy. Your servers, devices and account stay in `/var/lib/tunploy`, and it does not ask for an account again.

### Install options

Pass options after `sudo`, because `sudo` drops the rest of your environment:

```sh
curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh \
  | sudo TUNPLOY_PUBLIC_HOST=vpn.example.com sh
```

| Variable | Default | Meaning |
| --- | --- | --- |
| `TUNPLOY_PUBLIC_HOST` | detected public IPv4 | Hostname or IP that VPN clients dial. |
| `TUNPLOY_HTTPS` | `true` | `false` leaves TCP 80 and 443 alone, for a server whose web server needs them. The panel then can't serve its own domain. |
| `TUNPLOY_TRUSTED_PROXIES` | none | Your reverse proxy's addresses. See [HTTPS](https://tunploy.alperkarakoyun.com/docs/guides/https). |
| `TUNPLOY_TIMEZONE` | the server's time zone | Where days and months begin for data usage and monthly limits, for example `Europe/Istanbul`. |
| `TUNPLOY_UPDATE_CHECK` | `true` | `false` stops the panel from checking GitHub for new releases on its own. |
| `TUNPLOY_BIND` | `0.0.0.0` | Address the panel port listens on. `127.0.0.1` keeps it reachable only over an SSH tunnel. |
| `TUNPLOY_PORT` | `3000` | Panel port. |
| `TUNPLOY_VERSION` | `latest` | Image tag, for example `0.1.0` or `edge`. |
| `TUNPLOY_IMAGE` | `ghcr.io/kwa0x2/tunploy` | Image to install, for forks and mirrors. |
| `TUNPLOY_ADMIN_NAME`, `TUNPLOY_ADMIN_EMAIL`, `TUNPLOY_ADMIN_PASSWORD` | asked | Create the admin account without prompting, for automated installs. |

The panel container's own environment variables are listed under [Configuration](https://tunploy.alperkarakoyun.com/docs/configuration).

### Open the ports

The panel listens on **TCP 3000**, plus **TCP 80 and 443** for its HTTPS domain, and each VPN server on its own UDP port: the first on 51820, the next on 51821, and so on. Docker publishes these ports itself, past host firewalls such as `ufw`, so there is nothing to open on the server. Your hosting provider's firewall is separate: in Hetzner, AWS, Oracle Cloud, GCP and most others, add inbound rules for **TCP 3000, 80 and 443** and **UDP 51820** (and each further UDP port you use) in the provider's console.

### Docker Compose

If you would rather not pipe a script into a shell, this starts the same container:

```yaml
services:
  tunploy:
    image: ghcr.io/kwa0x2/tunploy:latest
    container_name: tunploy
    restart: unless-stopped
    ports:
      - "3000:3000"
      - "80:80"
      - "443:443"
    environment:
      TUNPLOY_PUBLIC_HOST: "YOUR_SERVER_IP"
      TZ: "UTC" # your time zone; monthly data limits reset at its midnight
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      # Must be the same path on both sides.
      - /var/lib/tunploy:/var/lib/tunploy
```

```sh
docker compose up -d
docker exec -it tunploy tunploy admin create
```

## Server commands

The install script puts a `tunploy` command on the server, for what can't be done from the panel:

| Command | What it does |
| --- | --- |
| `tunploy admin` | Create the admin account, reset its password or turn off two-factor sign-in. |
| `tunploy backup` | List the backups in S3 and restore one. |
| `tunploy logs` | Follow the panel's logs. Takes `docker logs` flags, such as `--since 1h`. |
| `tunploy restart` | Restart the panel. VPN servers keep running. |
| `tunploy version` | Print the version the panel runs. |
| `tunploy update [version]` | Update to the newest release, or to the given one. |
| `tunploy uninstall` | Remove Tunploy. |

They need root and ask for `sudo` on their own, unless you are in the `docker` group. With Docker Compose there is no `tunploy` command on the server; run the same commands in the container, as in `docker exec -it tunploy tunploy admin create`.

### Admin account

```sh
# Create the admin, if the install could not ask (for example, no terminal)
tunploy admin create

# Forgot the password: set a new one and sign out every session
tunploy admin reset-password --email you@example.com

# Lost the phone with the authenticator app: turn two-factor sign-in off
tunploy admin disable-2fa --email you@example.com
```

### Updating

The panel checks GitHub for new releases, and **Settings → Updates** updates it in about a minute, rolling back on its own if the new version doesn't start. From the server:

```sh
tunploy update
```

With Docker Compose:

```sh
docker compose pull && docker compose up -d
```

VPN servers run in their own containers and keep running throughout, so connected devices stay connected. See [Updating](https://tunploy.alperkarakoyun.com/docs/maintenance/updating) for how the rollback works.

### Uninstall

```sh
tunploy uninstall
```

or, if the `tunploy` command is missing:

```sh
curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh | sudo sh -s uninstall
```

This removes the panel, every VPN server it runs, and their images. It asks before deleting `/var/lib/tunploy`, which holds every server, device key, backup and the admin account; keep it and a later install picks everything up again. Add `--purge` to delete it without asking, and `--yes` to skip the confirmation: `tunploy uninstall --yes --purge`.

## Documentation

Everything else is at [tunploy.alperkarakoyun.com/docs](https://tunploy.alperkarakoyun.com/docs):

- [Installation](https://tunploy.alperkarakoyun.com/docs/installation) and [your first VPN](https://tunploy.alperkarakoyun.com/docs/first-vpn)
- [More servers (nodes)](https://tunploy.alperkarakoyun.com/docs/guides/nodes), [HTTPS](https://tunploy.alperkarakoyun.com/docs/guides/https), [two-factor authentication](https://tunploy.alperkarakoyun.com/docs/guides/two-factor), [email notifications](https://tunploy.alperkarakoyun.com/docs/guides/notifications), [backups](https://tunploy.alperkarakoyun.com/docs/guides/backups)
- [API](https://tunploy.alperkarakoyun.com/docs/api) and [webhooks](https://tunploy.alperkarakoyun.com/docs/api/webhooks), with the full description at `/api/v1/openapi.json` on your own panel
- [Updating](https://tunploy.alperkarakoyun.com/docs/maintenance/updating), [server commands](https://tunploy.alperkarakoyun.com/docs/maintenance/server-commands), [uninstall](https://tunploy.alperkarakoyun.com/docs/maintenance/uninstall)
- [Configuration](https://tunploy.alperkarakoyun.com/docs/configuration) and [troubleshooting](https://tunploy.alperkarakoyun.com/docs/troubleshooting)

## Development

Needs Go, Node.js and a running Docker daemon.

```sh
make admin  # create the admin account in ./data, once
make dev    # API and Vite with hot reload on http://localhost:5173
make test   # Go tests and a TypeScript type check
make lint   # go vet, gofmt and oxlint
```

Run `make help` for the rest. Bug reports and pull requests are welcome at [github.com/kwa0x2/tunploy](https://github.com/kwa0x2/tunploy/issues); for anything bigger than a fix, open an issue first.

## License

[MIT](LICENSE)
