# auto-deployer

Minimal GitHub webhook listener: on PR opened, clone the repo to disk and notify Slack with the local path, deployment instructions, and PR context.

## Behavior

- Listens to `pull_request` webhooks.
- Only reacts to `action = opened`.
- Clones the repo into a unique per-PR folder under `CLONE_ROOT`.
- Sends a Slack message telling the agent where the repo lives and to follow `DEPLOYMENT_INSTRUCTIONS.md`.

## Webhook endpoint

`POST /webhook/github`

## Configuration

- `LISTEN_ADDR` (default `:8080`)
- `ENV_FILE` (default `.env`)
- `GITHUB_WEBHOOK_SECRET` (required)
- `GITHUB_TOKEN` (optional; required for private repos)
- `GITHUB_ORGS` (csv allowlist; empty = allow all orgs)
- `CLONE_ROOT` (default `/tmp/auto-deployer`)
- Slack (choose one):
  - `SLACK_WEBHOOK_URL`
  - or `SLACK_BOT_TOKEN` + `SLACK_CHANNEL_ID`

## Local run

```
export LISTEN_ADDR=":8080"
export GITHUB_WEBHOOK_SECRET=...
export GITHUB_TOKEN=...            # optional
export GITHUB_ORGS=""              # empty = allow all
export CLONE_ROOT="/tmp/auto-deployer"
export SLACK_WEBHOOK_URL=...        # or SLACK_BOT_TOKEN + SLACK_CHANNEL_ID

go run ./cmd/preview-controller
```

## Health

- `GET /healthz`
- `GET /readyz`
