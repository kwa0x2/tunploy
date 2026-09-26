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

## Quick start

You need a Linux server (amd64 or arm64, kernel 5.6 or newer) with a public IP, root access over SSH, and UDP 51820 open in your provider's firewall. Then run:

```sh
curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh | sudo sh
```

The script installs Docker if it is missing, starts Tunploy, asks for your admin account and prints how to open the panel. Open `http://YOUR_SERVER_IP:3000`, go to **Servers → New server** and press **Deploy**, then **Add peer** and scan the QR code with the WireGuard app on your phone.

Running the same command again upgrades Tunploy; your servers, devices and account stay in `/var/lib/tunploy`.

Prefer Docker Compose, need another port, or already run a reverse proxy? See the [installation guide](https://tunploy.alperkarakoyun.com/docs/installation).

## Documentation

Everything else is at [tunploy.alperkarakoyun.com/docs](https://tunploy.alperkarakoyun.com/docs):

- [Installation](https://tunploy.alperkarakoyun.com/docs/installation) and [your first VPN](https://tunploy.alperkarakoyun.com/docs/first-vpn)
- [More servers (nodes)](https://tunploy.alperkarakoyun.com/docs/guides/nodes), [HTTPS](https://tunploy.alperkarakoyun.com/docs/guides/https), [two-factor authentication](https://tunploy.alperkarakoyun.com/docs/guides/two-factor), [email notifications](https://tunploy.alperkarakoyun.com/docs/guides/notifications), [backups](https://tunploy.alperkarakoyun.com/docs/guides/backups)
- [API](https://tunploy.alperkarakoyun.com/docs/api), with the full description at `/api/v1/openapi.json` on your own panel
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
