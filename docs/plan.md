# Klo-Ampel - Implementation Plan

## Goal

Exams run in several lecture halls at once.
Staff in each hall need to see whether the shared toilet is free before letting a student leave.
Klo-Ampel shows a big red/green status per toilet ("room") that every hall can put on its projector and anyone with access can toggle from a laptop or phone.

## Decisions

| Topic | Decision |
| --- | --- |
| State | Two states per room: free / occupied. Shows who occupied it (login name) and since when (small). |
| Toggling | Anyone with access can occupy, release and delete any room. No locking. |
| Persistence | In memory only. A restart clears all rooms and sessions. Exactly one replica. |
| Auth | OIDC Authorization Code Flow with PKCE against Keycloak, done by the Go server. Access requires a configurable realm or client role. |
| Session | In-memory session, HttpOnly cookie, 12 h lifetime (configurable). No refresh: the role is checked at login. |
| Live updates | One Server-Sent Events stream. Every change pushes the full room list, so a reconnecting client is always consistent. Heartbeat every 20 s. |
| UI | React + TypeScript (Vite), English only, no UI framework. |
| Image | One multi-arch image (amd64, arm64) on `gcr.io/distroless/static-debian13:nonroot`. The Go binary serves the built UI from `/web`. |
| Deployment | Helm chart with Deployment, Service, Secret and a Gateway API `HTTPRoute`. TLS terminates at the existing Gateway (cert-manager). |
| CI | Pull requests and `main`: tests, UI build, Helm lint, Docker build. Tags `v*`: push image and Helm chart (OCI) to GHCR. |

## API

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/auth/login?next=/path` | Start OIDC login |
| GET | `/auth/callback` | OIDC redirect target |
| POST | `/auth/logout` | End session (also at Keycloak) |
| GET | `/healthz` | Liveness/readiness, no auth |
| GET | `/api/me` | Current user name |
| GET | `/api/events` | SSE stream, event `rooms` with the full room list |
| POST | `/api/rooms` | Create room `{"name": "..."}` |
| DELETE | `/api/rooms/{id}` | Delete room |
| PUT | `/api/rooms/{id}/occupied` | Set state `{"occupied": true}` |

Setting the state explicitly (instead of a toggle endpoint) means two people pressing at the same time cannot flip it back by accident.

## Found during local testing

- Several tabs re-login at the same time after a restart. A single login cookie made all but one fail, so each login attempt now gets its own cookie (`klo_login_<state>`).
- A user without the role was stuck: "Try again" silently reused their Keycloak session. The error page now links to the Keycloak logout to switch accounts.

## Future ideas

- Waiting queue per toilet with a notification when it is your hall's turn.
