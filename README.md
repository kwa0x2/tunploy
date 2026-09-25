<div align="center">
  <img src="web/public/favicon.svg" alt="Tunploy" width="72" height="72">
  <h1>Tunploy</h1>
  <p>A self-hosted control panel for your own WireGuard VPN servers.</p>

  [![CI](https://github.com/kwa0x2/tunploy/actions/workflows/ci.yml/badge.svg)](https://github.com/kwa0x2/tunploy/actions/workflows/ci.yml)
  [![Release](https://img.shields.io/github/v/release/kwa0x2/tunploy)](https://github.com/kwa0x2/tunploy/releases)
  [![License](https://img.shields.io/github/license/kwa0x2/tunploy)](LICENSE)
</div>

Tunploy runs as a single Docker container on your Linux server. From its web panel you spin up WireGuard servers, add devices, scan their QR codes, and see who is connected and how much they use, without ever touching a WireGuard config by hand.

## Features

- **One-click VPN servers.** Each WireGuard server runs in its own container, on its own UDP port, with its own DNS, MTU, keepalive and allowed IPs.
- **Devices.** Add a device and scan its QR code with the WireGuard app, or download its `.conf`. Turn devices off, give them a monthly data limit or an expiry date.
- **Live status and usage.** See which devices are online, from which country, and their daily and monthly traffic.
- **Activity log.** Connections, changes and sign-ins, in one place.
- **HTTPS from the panel.** Point a domain at the server and the panel gets its own Let's Encrypt certificate.
- **Two-factor sign-in** with any authenticator app.
- **Email notifications** when servers go down, devices hit their limit, sign-ins fail and more.
- **Backups** to your computer or any S3-compatible storage, on a schedule, optionally encrypted.
- **In-panel updates** that roll back on their own if the new version doesn't start.
- **A `tunploy` command** on the server for resetting the admin password, restoring backups, reading logs and uninstalling.

## Getting started

You need:

- A Linux server with a public IP, amd64 or arm64. Any distribution with Linux 5.6 or newer works; Ubuntu 20.04+ and Debian 11+ are fine.
- Root access over SSH.
- A UDP port open to the internet for each VPN server, starting at 51820.

Then run:

```sh
curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh | sudo sh
```

The script installs Docker if it is missing, starts Tunploy, asks for your admin account and prints how to open the panel. You do not install WireGuard yourself: the tools ship inside Tunploy's image, and the kernel module is already part of Linux 5.6+.

Open `http://YOUR_SERVER_IP:3000`, sign in, and [create your first VPN](#create-your-first-vpn). Have a domain? [Give the panel HTTPS](#https) next.

## Contents

- [Installation](#installation)
  - [What the script does](#what-the-script-does)
  - [Install options](#install-options)
  - [Open the ports](#open-the-ports)
  - [Manual install with Docker Compose](#manual-install-with-docker-compose)
- [Using the panel](#using-the-panel)
  - [Sign in](#sign-in)
  - [Create your first VPN](#create-your-first-vpn)
  - [HTTPS](#https)
  - [Two-factor authentication](#two-factor-authentication)
  - [Email notifications](#email-notifications)
  - [Backups](#backups)
- [Maintenance](#maintenance)
  - [Updating](#updating)
  - [Server commands](#server-commands)
  - [Manage the admin account](#manage-the-admin-account)
  - [Uninstall](#uninstall)
- [Configuration](#configuration)
- [Troubleshooting](#troubleshooting)
- [Development](#development)

## Installation

Tunploy runs as a single Docker container and starts one more container for each WireGuard server you create.

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

Running the same command again upgrades Tunploy (see [Updating](#updating)). Your servers, peers and account stay in `/var/lib/tunploy`, and it does not ask for an account again. The port, bind address and trusted proxies you chose before are kept; the panel domain lives in the database, so it is kept too.

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
| `TUNPLOY_TRUSTED_PROXIES` | none | Your reverse proxy's addresses. See [behind your own reverse proxy](#behind-your-own-reverse-proxy). |
| `TUNPLOY_TIMEZONE` | the server's time zone | Where days and months begin for data usage and monthly limits, for example `Europe/Istanbul`. |
| `TUNPLOY_UPDATE_CHECK` | `true` | `false` stops the panel from checking GitHub for new releases on its own. |
| `TUNPLOY_BIND` | `0.0.0.0` | Address the panel port listens on. `127.0.0.1` keeps it reachable only over an SSH tunnel. |
| `TUNPLOY_PORT` | `3000` | Panel port. |
| `TUNPLOY_VERSION` | `latest` | Image tag, for example `0.1.0` or `edge`. |
| `TUNPLOY_IMAGE` | `ghcr.io/kwa0x2/tunploy` | Image to install, for forks and mirrors. |
| `TUNPLOY_ADMIN_NAME`, `TUNPLOY_ADMIN_EMAIL`, `TUNPLOY_ADMIN_PASSWORD` | asked | Create the admin account without prompting, for automated installs. |

### Open the ports

The panel listens on **TCP 3000**, plus **TCP 80 and 443** for its HTTPS domain, and each VPN server on its own UDP port: the first on 51820, the next on 51821, and so on. Docker publishes these ports itself, past host firewalls such as `ufw`, so there is nothing to open on the server. Your hosting provider's firewall is separate: in Hetzner, AWS, Oracle Cloud, GCP and most others, add inbound rules for **TCP 3000, 80 and 443** and **UDP 51820** (and each further UDP port you use) in the provider's console.

### Manual install with Docker Compose

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

See [Configuration](#configuration) for the other environment variables.

## Using the panel

### Sign in

Open `http://YOUR_SERVER_IP:3000` and sign in with the account you created during the install. The panel is public, but there is no sign-up page: only the account made on the server can get in.

Out of the box the panel serves plain HTTP, so your password and session travel unencrypted. Settings shows a warning while that is the case. Fix it with one of the options under [HTTPS](#https).

### Create your first VPN

1. Go to **Servers → New server**, give it a name and press **Deploy**.
2. Open the server and choose **Add peer**. Scan the QR code with the WireGuard app on your phone, or download the `.conf` file for a laptop.
3. Connect. The peer shows up as **Online** within a few seconds.

The endpoint that clients connect to comes from **Settings → General**. If the install script detected the wrong address, change it there before creating servers. Existing servers keep their own endpoint, which you can change under each server's settings.

### HTTPS

#### Your own domain, from the panel

The install publishes TCP 80 and 443 next to the panel port, so HTTPS needs no reinstall:

1. At your DNS provider, add an **A record** for a domain or subdomain, such as `panel.example.com`, pointing to the server's public IP.
2. Allow **TCP 80 and 443** in your hosting provider's firewall.
3. In the panel, open **Settings → Domain**, enter the domain (and optionally an email for Let's Encrypt), and press **Save**.

Tunploy checks that the domain resolves, requests a certificate from Let's Encrypt, and shows the result right there: *HTTPS is active* with the expiry date, or the reason it failed. The certificate renews itself and is kept in `/var/lib/tunploy/certs`. Port 80 answers Let's Encrypt's check and sends browsers to `https://panel.example.com`.

The panel stays reachable on `http://SERVER_IP:3000` too, as a way in if DNS ever breaks; its sign-in page then links to the HTTPS address. To close port 3000 to the internet, reinstall with `TUNPLOY_BIND=127.0.0.1` and use an SSH tunnel for that way in.

If the certificate fails, it is almost always an A record that does not point here yet (DNS changes can take a while) or TCP 80 blocked by the provider's firewall. Let's Encrypt allows only a few failed attempts an hour, so fix the cause before pressing **Try again**. **Remove** takes the domain away and returns the panel to plain HTTP.

If another web server already holds 80 or 443, the install warns and runs the panel without them. Free the ports and run the install again, or use the reverse proxy below.

#### Behind your own reverse proxy

If the server already runs Caddy, nginx or Traefik on 80 and 443, point the proxy at `127.0.0.1:3000` and tell Tunploy which addresses the proxy connects from:

```sh
curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh \
  | sudo TUNPLOY_HTTPS=false TUNPLOY_BIND=127.0.0.1 TUNPLOY_TRUSTED_PROXIES=172.16.0.0/12 sh
```

`TUNPLOY_TRUSTED_PROXIES` lists the addresses or CIDRs your proxy connects from; only from those does Tunploy believe `X-Forwarded-For` and `X-Forwarded-Proto`. A proxy on the same host reaches the container through Docker's bridge, so `172.16.0.0/12` covers it. Without this setting, the activity log shows the proxy's address for every sign-in, and session cookies are not marked `Secure`.

#### Over SSH instead

Keep the panel private and reach it through SSH, which encrypts the connection. Install with `TUNPLOY_BIND=127.0.0.1`, then on your own computer run:

```sh
ssh -L 3000:localhost:3000 root@YOUR_SERVER_IP
```

Leave that terminal open and browse to <http://localhost:3000>.

### Two-factor authentication

Under **Settings → Security**, turn on two-factor authentication and scan the QR code with an authenticator app (Google Authenticator, 1Password, Aegis, Bitwarden and so on). From then on, signing in asks for the 6-digit code from the app as well. Turning it on signs out every other device.

### Email notifications

Under **Settings → Notifications**, give the panel an SMTP account and it emails you when servers go down or come back, devices are added, a device uses up its data, someone fails to sign in, and so on; you choose which. Events that happen close together arrive as one email, and at most 30 emails go out an hour.

Any provider that offers SMTP works. Use the address you send from as the username, port **587** with **STARTTLS** (or **465** with **TLS**), and set **From** to an address the account may send as. Gmail and Outlook need an app password rather than your normal one. **Send test email** tries the form as it is, before you save, and shows the mail server's own answer if it fails.

### Backups

A backup is one `.tar.gz` file with every server and device (including their keys), the panel settings, the admin account, traffic history and the HTTPS certificate. Anyone who has it can run your VPN, so keep it private.

Under **Settings → Backups**, **Download backup** saves one to your computer, and **Restore from file** loads one back. A restore replaces everything on the panel, restarts the VPN servers (devices drop for a moment) and signs everyone out; sign in with the account from the backup. A backup from an older Tunploy is upgraded as it is restored; one from a newer Tunploy is refused, so update the panel first.

To keep copies off the server, connect an S3 bucket under **Settings → Backups**. Any S3-compatible storage works:

| Provider | Endpoint | Region | Path-style |
| --- | --- | --- | --- |
| AWS S3 | leave empty | the bucket's region, such as `eu-central-1` | off |
| Cloudflare R2 | `https://<account-id>.r2.cloudflarestorage.com` | `auto` | on |
| Backblaze B2 | `https://s3.<region>.backblazeb2.com` | the region in the endpoint | off |
| MinIO and other self-hosted | `http://host:9000` | leave empty | on |

Create a key that can only read, write, list and delete in that bucket. **Test connection** and **Connect** upload, list and delete a small test file first, and show the storage's own answer if something is wrong. Choose a daily or weekly schedule and how many backups to keep; older ones are deleted after each new backup. A failed scheduled backup is retried every 30 minutes and emailed once, if email notifications are on. The bucket's backups are listed on the same page, where you can download, restore or delete each one.

#### Encryption

**Set passphrase** on the Backups card encrypts every new backup, downloaded or in the bucket (AES-256-GCM, with the key derived from the passphrase by argon2id); encrypted files end in `.tar.gz.enc`. The panel keeps the passphrase so scheduled backups can run, which means encryption protects a leaked file or bucket, not a panel someone already controls. Keep the passphrase in a password manager: without it an encrypted backup cannot be restored by anyone. Changing or turning it off only affects new backups; older ones still need the passphrase they were made with. The panel tries its own passphrase first when restoring, and asks for one when that does not fit.

#### Moving to a new server

Install Tunploy there, connect the same bucket and folder, and restore the newest backup (enter the passphrase if it is encrypted). Point your DNS (or each server's endpoint) at the new address afterwards, since devices still dial the old one.

#### Restoring from the command line

When the panel will not start, restore on the server itself. It works whether the panel is running or not; restart it afterwards and it rebuilds its VPN servers as it starts:

```sh
tunploy backup list
tunploy backup restore --s3 tunploy-backup-20260925-030000.tar.gz
tunploy restart
```

To restore a file on the server instead, pass its path: `tunploy backup restore ./tunploy-backup-20260925-030000.tar.gz`. Without a terminal, add `--yes` and pipe the passphrase on stdin.

If the database itself is damaged, the container keeps restarting and the `tunploy` command cannot reach it. Stop it, move the database aside, and restore from a file with a one-off container on the same data directory:

```sh
docker stop tunploy
mv /var/lib/tunploy/tunploy.db /var/lib/tunploy/tunploy.db.broken
rm -f /var/lib/tunploy/tunploy.db-wal /var/lib/tunploy/tunploy.db-shm
docker run --rm -it -v /var/lib/tunploy:/var/lib/tunploy -v "$PWD":/backup \
  ghcr.io/kwa0x2/tunploy:latest backup restore /backup/tunploy-backup-20260925-030000.tar.gz
docker start tunploy
```

## Maintenance

### Updating

The panel checks GitHub for a new Tunploy release twice a day. When there is one, an **Update to x.y.z** button appears at the top of every page, and **Settings → Updates** shows the version you run, the latest release with a link to its notes, and a **Check now** button.

**Update now** downloads the new image and restarts the panel on it, which takes about a minute:

1. The panel pulls the new image, so a failed download changes nothing.
2. It starts a short-lived `tunploy-updater` container from the new image, because a container can't replace itself.
3. The updater stops the panel, copies the database aside, and starts a new panel container with the same ports, volumes and settings.
4. Once the new panel answers, the updater removes the old container and the copy of the database. If it doesn't answer within 90 seconds, the updater removes it, puts the database back, and starts the old panel again. **Settings → Updates** then shows why.

VPN servers run in their own containers and keep running throughout, so connected devices stay connected. You stay signed in. The activity log records every update and every failed one.

The button only works for a panel started by the install script. A panel started with Docker Compose shows how to update it (`docker compose pull && docker compose up -d`) instead. Running `tunploy update` or the install command again also updates. Set `TUNPLOY_UPDATE_CHECK=false` to stop the panel from contacting GitHub on its own; **Check now** still asks when you press it.

### Server commands

The install script puts a `tunploy` command on the server, for what can't be done from the panel:

| Command | What it does |
| --- | --- |
| `tunploy admin` | Create the admin account, reset its password or turn off two-factor sign-in (see [Manage the admin account](#manage-the-admin-account)). |
| `tunploy backup` | List the backups in S3 and restore one (see [Restoring from the command line](#restoring-from-the-command-line)). |
| `tunploy logs` | Follow the panel's logs. Takes `docker logs` flags, such as `--since 1h`. |
| `tunploy restart` | Restart the panel. VPN servers keep running. |
| `tunploy version` | Print the version the panel runs. |
| `tunploy update [version]` | Update to the newest release, or to the given one, by running the install script again. |
| `tunploy uninstall` | Remove Tunploy (see [Uninstall](#uninstall)). |

The commands run in the panel's container, so they always match the version it runs. They need root and ask for `sudo` on their own, unless you are in the `docker` group.

### Manage the admin account

These ask for the password without echoing it:

```sh
# Create the admin, if the install could not ask (for example, no terminal)
tunploy admin create

# Forgot the password: set a new one and sign out every session
tunploy admin reset-password --email you@example.com

# Lost the phone with the authenticator app: turn two-factor sign-in off
tunploy admin disable-2fa --email you@example.com
```

With Docker Compose there is no `tunploy` command on the server; run the same commands in the container instead, as in `docker exec -it tunploy tunploy admin create`.

Once signed in, you can also change the password under **Settings → Security**.

### Uninstall

```sh
tunploy uninstall
```

or, if the `tunploy` command is missing:

```sh
curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh | sudo sh -s uninstall
```

This removes the panel container, every VPN server it runs, and their images. Connected devices lose their connection. It asks before deleting `/var/lib/tunploy`, which holds every server, device key, backup and the admin account. Keep that directory and a later install picks everything up again. Docker itself stays installed.

Add `--purge` to delete `/var/lib/tunploy` without asking, and `--yes` to skip the confirmation when there is no terminal: `tunploy uninstall --yes --purge`.

## Configuration

The panel container reads these environment variables. With the install script, set them through its [install options](#install-options) instead; with Docker Compose, put them under `environment`.

| Variable | Default | Meaning |
| --- | --- | --- |
| `TUNPLOY_LISTEN` | `:3000` | Address inside the container. |
| `TUNPLOY_DATA_DIR` | `/var/lib/tunploy` | Database and WireGuard configs. Mount it at the same path on the host. |
| `TUNPLOY_PUBLIC_HOST` | empty | Default endpoint for new servers, used while **Settings → General** is empty. |
| `TUNPLOY_SESSION_TTL` | `168h` | How long a sign-in lasts. |
| `TUNPLOY_HTTPS` | `true` | `false` stops the panel from listening on 80 and 443; Settings then can't set a domain. |
| `TUNPLOY_HTTPS_LISTEN`, `TUNPLOY_HTTP_LISTEN` | `:443`, `:80` | Where HTTPS and its redirect listen inside the container. |
| `TUNPLOY_ACME_DIRECTORY` | Let's Encrypt | ACME directory URL, for example the Let's Encrypt staging server while testing. |
| `TUNPLOY_TRUSTED_PROXIES` | empty | Reverse proxies whose `X-Forwarded-For` and `X-Forwarded-Proto` are believed. |
| `TUNPLOY_CONTAINER_PREFIX` | `tunploy-wg-` | Name prefix of the VPN containers, which are named prefix plus server ID. Give each panel sharing one Docker daemon its own, ending in `-`; a panel only ever touches containers with its prefix. Changing it on a running panel leaves the old containers behind. |
| `TUNPLOY_SECURE_COOKIES` | `false` | Force `Secure` cookies. Not needed with a panel domain or a trusted proxy that sends `X-Forwarded-Proto`. |
| `TUNPLOY_UPDATE_CHECK` | `true` | Check GitHub for new releases twice a day. `false` checks only when you press **Check now**. |
| `TUNPLOY_GEOIP` | `true` | Show device countries. Downloads the free [DB-IP Lite](https://db-ip.com) database (about 8 MB) into the data dir and refreshes it monthly. Set to `false` to never contact db-ip.com. |
| `TUNPLOY_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `TZ` | `UTC` | Time zone for daily and monthly data usage, so monthly limits reset at your midnight, for example `Europe/Istanbul`. |

## Troubleshooting

**The panel does not come up.** Check its logs with `tunploy logs` (or `docker logs tunploy`). `permission denied` on `docker.sock` means the socket is not mounted, or a rootless Docker is in use.

**A server shows as stopped or keeps restarting.** Open the server and choose **View logs**. `RTNETLINK answers: Operation not supported` means the host kernel has no WireGuard module. Run `sudo modprobe wireguard`; if that fails, the VPS type (often OpenVZ or LXC) does not support WireGuard.

**The peer never comes online.** The UDP port is almost always blocked by the provider's firewall. Also check that the endpoint in the client config is the server's public address and not `localhost` or a private IP.

**Connected, but no internet.** Check that the server itself has outbound connectivity, and that no host firewall rule drops forwarded traffic.

**HTTPS does not work.** **Settings → Domain** shows why the last certificate request failed. Check that the domain's A record points to the server (`dig +short panel.example.com`) and that TCP 80 and 443 are open in the provider's firewall.

**Forgot the admin password.** Run `tunploy admin reset-password --email you@example.com` on the server. Servers and peers are not affected.

## Development

Needs Go, Node.js and a running Docker daemon.

```sh
make admin  # create the admin account in ./data, once
make dev    # API and Vite with hot reload on http://localhost:5173
make test   # Go tests and a TypeScript type check
make lint   # go vet, gofmt and oxlint
```

Run `make help` for the rest.

## License

[MIT](LICENSE)
