# tunploy
Tunploy is a self-hosted control panel for your own VPN infrastructure. Spin up VPN servers, manage peers and access policies, and deploy to any machine in one click.

## Install

On a Linux server with a public IP:

```sh
curl -fsSL https://raw.githubusercontent.com/kwa0x2/tunploy/main/install.sh | sudo sh
```

The script installs Docker if needed, starts Tunploy, asks for your admin account and prints how to open the panel. Then create a server, add a peer and scan its QR code with the WireGuard app.

See [docs/install.md](docs/install.md) for options, the SSH tunnel for the first sign-in, firewall ports and troubleshooting.

## Development

Needs Go, Node.js and a running Docker daemon.

```sh
make admin  # create the admin account in ./data, once
make dev    # API and Vite with hot reload on http://localhost:5173
make test   # Go tests and a TypeScript type check
make lint   # go vet, gofmt and oxlint
```

Run `make help` for the rest.
