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
5. Waits until the panel answers.
6. Asks for your name, email and password and creates the admin account.
7. Prints how to reach the panel.

The panel has no sign-up page. The admin account can only be created on the server, so nobody who stumbles on the panel can claim it.

To upgrade later, run the same command again. It pulls the newest image and replaces the container; your servers, peers and account stay in `/var/lib/tunploy`, and it does not ask for an account again. The port, bind address and trusted proxies you chose before are kept; the panel domain lives in the database, so it is kept too.

### Options

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
| `TUNPLOY_BIND` | `0.0.0.0` | Address the panel port listens on. `127.0.0.1` keeps it reachable only over an SSH tunnel. |
| `TUNPLOY_PORT` | `3000` | Panel port. |
| `TUNPLOY_VERSION` | `latest` | Image tag, for example `0.1.0` or `edge`. |
| `TUNPLOY_IMAGE` | `ghcr.io/kwa0x2/tunploy` | Image to install, for forks and mirrors. |
| `TUNPLOY_ADMIN_NAME`, `TUNPLOY_ADMIN_EMAIL`, `TUNPLOY_ADMIN_PASSWORD` | asked | Create the admin account without prompting, for automated installs. |

## Sign in

Open `http://YOUR_SERVER_IP:3000` and sign in with the account you created during the install. The panel is public, but there is no sign-up page: only the account made on the server can get in.

Out of the box the panel serves plain HTTP, so your password and session travel unencrypted. Settings shows a warning while that is the case. Fix it with one of the options below.

## HTTPS

### Your own domain, from the panel

The install publishes TCP 80 and 443 next to the panel port, so HTTPS needs no reinstall:

1. At your DNS provider, add an **A record** for a domain or subdomain, such as `panel.example.com`, pointing to the server's public IP.
2. Allow **TCP 80 and 443** in your hosting provider's firewall.
3. In the panel, open **Settings → Domain**, enter the domain (and optionally an email for Let's Encrypt), and press **Save**.

Tunploy checks that the domain resolves, requests a certificate from Let's Encrypt, and shows the result right there: *HTTPS is active* with the expiry date, or the reason it failed. The certificate renews itself and is kept in `/var/lib/tunploy/certs`. Port 80 answers Let's Encrypt's check and sends browsers to `https://panel.example.com`.

The panel stays reachable on `http://SERVER_IP:3000` too, as a way in if DNS ever breaks; its sign-in page then links to the HTTPS address. To close port 3000 to the internet, reinstall with `TUNPLOY_BIND=127.0.0.1` and use an SSH tunnel for that way in.

If the certificate fails, it is almost always an A record that does not point here yet (DNS changes can take a while) or TCP 80 blocked by the provider's firewall. Let's Encrypt allows only a few failed attempts an hour, so fix the cause before pressing **Try again**. **Remove** takes the domain away and returns the panel to plain HTTP.

If another web server already holds 80 or 443, the install warns and runs the panel without them. Free the ports and run the install again, or use the reverse proxy below.

### Behind your own reverse proxy

If the server already runs Caddy, nginx or Traefik on 80 and 443, point the proxy at `127.0.0.1:3000` and tell Tunploy which addresses the proxy connects from:

```sh
curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh \
  | sudo TUNPLOY_HTTPS=false TUNPLOY_BIND=127.0.0.1 TUNPLOY_TRUSTED_PROXIES=172.16.0.0/12 sh
```

`TUNPLOY_TRUSTED_PROXIES` lists the addresses or CIDRs your proxy connects from; only from those does Tunploy believe `X-Forwarded-For` and `X-Forwarded-Proto`. A proxy on the same host reaches the container through Docker's bridge, so `172.16.0.0/12` covers it. Without this setting, the activity log shows the proxy's address for every sign-in, and session cookies are not marked `Secure`.

### Over SSH instead

Keep the panel private and reach it through SSH, which encrypts the connection. Install with `TUNPLOY_BIND=127.0.0.1`, then on your own computer run:

```sh
ssh -L 3000:localhost:3000 root@YOUR_SERVER_IP
```

Leave that terminal open and browse to <http://localhost:3000>.

## Two-factor authentication

Under **Settings → Security**, turn on two-factor authentication and scan the QR code with an authenticator app (Google Authenticator, 1Password, Aegis, Bitwarden and so on). From then on, signing in asks for the 6-digit code from the app as well. Turning it on signs out every other device.

## Email notifications

Under **Settings → Email notifications**, give the panel an SMTP account and it emails you when servers go down or come back, devices are added, a device uses up its data, someone fails to sign in, and so on; you choose which. Events that happen close together arrive as one email, and at most 30 emails go out an hour.

Any provider that offers SMTP works. Use the address you send from as the username, port **587** with **STARTTLS** (or **465** with **TLS**), and set **From** to an address the account may send as. Gmail and Outlook need an app password rather than your normal one. **Send test email** tries the form as it is, before you save, and shows the mail server's own answer if it fails.

## Manage the admin account

These run inside the panel's container and ask for the password without echoing it:

```sh
# Create the admin, if the install could not ask (for example, no terminal)
docker exec -it tunploy tunploy admin create

# Forgot the password: set a new one and sign out every session
docker exec -it tunploy tunploy admin reset-password --email you@example.com

# Lost the phone with the authenticator app: turn two-factor sign-in off
docker exec -it tunploy tunploy admin disable-2fa --email you@example.com
```

Once signed in, you can also change the password under **Settings**.

## Open the ports

The panel listens on **TCP 3000**, plus **TCP 80 and 443** for its HTTPS domain, and each VPN server on its own UDP port: the first on 51820, the next on 51821, and so on. Docker publishes these ports itself, past host firewalls such as `ufw`, so there is nothing to open on the server. Your hosting provider's firewall is separate: in Hetzner, AWS, Oracle Cloud, GCP and most others, add inbound rules for **TCP 3000, 80 and 443** and **UDP 51820** (and each further UDP port you use) in the provider's console.

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

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `TUNPLOY_LISTEN` | `:3000` | Address inside the container. |
| `TUNPLOY_DATA_DIR` | `/var/lib/tunploy` | Database and WireGuard configs. Mount it at the same path on the host. |
| `TUNPLOY_PUBLIC_HOST` | empty | Default endpoint for new servers, used while **Settings → Public host** is empty. |
| `TUNPLOY_SESSION_TTL` | `168h` | How long a sign-in lasts. |
| `TUNPLOY_HTTPS` | `true` | `false` stops the panel from listening on 80 and 443; Settings then can't set a domain. |
| `TUNPLOY_HTTPS_LISTEN`, `TUNPLOY_HTTP_LISTEN` | `:443`, `:80` | Where HTTPS and its redirect listen inside the container. |
| `TUNPLOY_ACME_DIRECTORY` | Let's Encrypt | ACME directory URL, for example the Let's Encrypt staging server while testing. |
| `TUNPLOY_TRUSTED_PROXIES` | empty | Reverse proxies whose `X-Forwarded-For` and `X-Forwarded-Proto` are believed. |
| `TUNPLOY_SECURE_COOKIES` | `false` | Force `Secure` cookies. Not needed with a panel domain or a trusted proxy that sends `X-Forwarded-Proto`. |
| `TUNPLOY_GEOIP` | `true` | Show device countries. Downloads the free [DB-IP Lite](https://db-ip.com) database (about 8 MB) into the data dir and refreshes it monthly. Set to `false` to never contact db-ip.com. |
| `TUNPLOY_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `TZ` | `UTC` | Time zone for daily and monthly data usage, so monthly limits reset at your midnight, for example `Europe/Istanbul`. |

## Troubleshooting

**The panel does not come up.** Check its logs with `docker logs tunploy`. `permission denied` on `docker.sock` means the socket is not mounted, or a rootless Docker is in use.

**A server shows as stopped or keeps restarting.** Open the server and choose **View logs**. `RTNETLINK answers: Operation not supported` means the host kernel has no WireGuard module. Run `sudo modprobe wireguard`; if that fails, the VPS type (often OpenVZ or LXC) does not support WireGuard.

**The peer never comes online.** The UDP port is almost always blocked by the provider's firewall. Also check that the endpoint in the client config is the server's public address and not `localhost` or a private IP.

**Connected, but no internet.** Check that the server itself has outbound connectivity, and that no host firewall rule drops forwarded traffic.

**HTTPS does not work.** **Settings → Domain** shows why the last certificate request failed. Check that the domain's A record points to the server (`dig +short panel.example.com`) and that TCP 80 and 443 are open in the provider's firewall.

**Forgot the admin password.** Run `docker exec -it tunploy tunploy admin reset-password --email you@example.com` on the server. Servers and peers are not affected.
