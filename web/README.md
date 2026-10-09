# Tunploy panel UI

React, TypeScript and Vite, with shadcn/ui on Base UI. `npm run build` writes
into `../internal/web/dist`, which the Go binary embeds.

```
src/
  app/          routes, the signed-in shell, 404
  features/     one folder per screen: auth, overview, servers, peers,
                nodes, activity, settings, share
  api/          the HTTP API, one module per resource, like the Go packages
  components/   shared pieces; ui/ is shadcn
  hooks/        shared hooks
  lib/          formatting, theme, cn
```

Imports go one way: `app` → `features` → `components`, `hooks`, `lib`, `api`.
A feature may use another's exported pieces, such as `useAuth`, but shared
code never imports a feature.

`npm run dev` serves the UI and proxies `/api` to a panel on port 3000
(`make dev` runs both).
