# Klo-Ampel

Live toilet availability for exams spread over several lecture halls.

Each "room" in Klo-Ampel stands for one toilet and is either **free** (green) or **occupied** (red).
Put a room on the projector in every lecture hall, and staff can occupy or release it from the lectern laptop or their phone.
All screens update instantly.

- Create, open and delete rooms from the start page.
- The room view is a full-screen red/green display meant for projectors, with a big **Occupy / Release** button. The space bar toggles too.
- A small line shows who occupied the toilet and since when.
- Anyone with access can toggle or delete any room. Access is limited to Keycloak users with a specific role.
- If the live connection drops, a yellow banner says so, and the client reconnects on its own.

State lives in memory only: restarting the server clears all rooms and logs everyone out.
Do not deploy during a running exam.

## Architecture

| Part | Tech |
| --- | --- |
| Server | Go, stdlib `net/http`, [go-oidc](https://github.com/coreos/go-oidc) |
| UI | React + TypeScript (Vite) in [`web/`](web) |
| Live updates | Server-Sent Events (`/api/events`), every change pushes the full room list |
| Auth | OIDC Authorization Code Flow with PKCE, done by the server, HttpOnly session cookie |
| Image | One multi-arch image on `gcr.io/distroless/static-debian13:nonroot` |
| Deployment | Helm chart in [`charts/klo-ampel`](charts/klo-ampel) with a Gateway API `HTTPRoute` |

See [docs/plan.md](docs/plan.md) for the decisions and the API.

## Keycloak setup

1. Create a **confidential** OpenID Connect client, e.g. `klo-ampel`, with the standard flow enabled.
2. Set the valid redirect URI to `https://<host>/auth/callback` and the valid post logout redirect URI to `https://<host>/*`.
3. Create a role, e.g. the realm role `klo-ampel`, and assign it to everyone who may use the app (directly or via a group).
   A client role works too: set `oidc.roleClient` to the client that owns it.

The server reads the roles from the access token (`realm_access` / `resource_access`), which Keycloak includes by default.
Users without the role see a message and a link to log in with another account.

## Deployment

The image and the chart are published to GHCR on every release.

```bash
helm install klo-ampel oci://ghcr.io/ls1intum/charts/klo-ampel \
  --namespace klo-ampel --create-namespace \
  --set host=klo-ampel.example.com \
  --set oidc.issuerUrl=https://keycloak.example.com/realms/myrealm \
  --set oidc.existingSecret=klo-ampel-oidc \
  --set httpRoute.parentRefs[0].name=my-gateway \
  --set httpRoute.parentRefs[0].namespace=gateway
```

The Secret given in `oidc.existingSecret` needs the key `client-secret`.
Alternatively, set `oidc.clientSecret` and the chart creates the Secret.

| Value | Default | Description |
| --- | --- | --- |
| `host` | `klo-ampel.example.com` | Public hostname, served via HTTPS |
| `oidc.issuerUrl` | | Keycloak realm URL (required) |
| `oidc.clientId` | `klo-ampel` | OIDC client ID |
| `oidc.clientSecret` / `oidc.existingSecret` | | Client secret, inline or from an existing Secret |
| `oidc.requiredRole` | `klo-ampel` | Role a user needs |
| `oidc.roleClient` | `""` | Empty for a realm role, otherwise the client that owns the role |
| `sessionTtl` | `12h` | Session lifetime |
| `httpRoute.parentRefs` | `gateway/gateway` | Gateway(s) to attach to. TLS terminates at the Gateway (cert-manager). |
| `httpRoute.requestTimeout` | `"0s"` | Disables the route timeout for long SSE streams. Set to `""` if your Gateway does not support it. |

The chart always runs exactly one replica with the `Recreate` strategy, since all state is in memory.
The pod runs as non-root with a read-only root filesystem and no capabilities.

GHCR packages of a new repository start out private.
Either make `klo-ampel` and `charts/klo-ampel` public in the organization's package settings, or add an image pull secret.

## Development

Requirements: Go 1.27, Node 24, Docker.

Start a local Keycloak with a ready-made realm (users `staff`/`staff` with the role, `student`/`student` without):

```bash
docker compose up -d
```

Run the server (it serves the built UI from `web/dist`):

```bash
cd web && npm ci && npm run build && cd ..
```

```bash
PUBLIC_URL=http://127.0.0.1:8080 OIDC_ISSUER_URL=http://127.0.0.1:8081/realms/klo-ampel OIDC_CLIENT_ID=klo-ampel OIDC_CLIENT_SECRET=dev-secret OIDC_REQUIRED_ROLE=klo-ampel go run .
```

Then open http://127.0.0.1:8080.

For UI work with hot reload, start the server with `PUBLIC_URL=http://127.0.0.1:5173` and run `npm run dev` in `web/`.
Vite proxies `/api` and `/auth` to the server.

Tests:

```bash
go test -race ./...
```

```bash
cd web && npm test
```

### Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `PUBLIC_URL` | | External URL, used for the OIDC redirect (required) |
| `OIDC_ISSUER_URL` | | Keycloak realm URL (required) |
| `OIDC_CLIENT_ID` | | Client ID (required) |
| `OIDC_CLIENT_SECRET` | | Client secret |
| `OIDC_REQUIRED_ROLE` | | Role a user needs (required) |
| `OIDC_ROLE_CLIENT` | | Empty for a realm role, otherwise the client that owns the role |
| `SESSION_TTL` | `12h` | Session lifetime |
| `LISTEN_ADDR` | `:8080` | Listen address |
| `STATIC_DIR` | `web/dist` | Built UI |

## Releasing

Push a tag like `v1.2.3`.
The release workflow runs all checks, then pushes `ghcr.io/ls1intum/klo-ampel:1.2.3` (amd64 + arm64), the chart `oci://ghcr.io/ls1intum/charts/klo-ampel` in version `1.2.3`, and creates a GitHub release.

## Future ideas

- A waiting queue per toilet, with a notification when it is a lecture hall's turn.

## License

[MIT](LICENSE)
