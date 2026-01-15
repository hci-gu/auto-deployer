# auto-deployer

Preview controller for creating per-PR deployments in OpenShift.

## Local run

```
export LISTEN_ADDR=":8080"
export GITHUB_WEBHOOK_SECRET=... 
export GITHUB_ALLOWED_REPOS="org1/repoA,org1/repoB"
export GITHUB_TOKEN="..."                              # optional; enables PR comments
export GITHUB_API_BASE_URL="https://api.github.com"    # optional for GHE
export PREVIEW_NAMESPACE_MODE="single"         # single|per-app|per-pr
export PREVIEW_BASE_NAMESPACE="previews"       # required for single/per-app
export ROUTE_DOMAIN_TEMPLATE="{app}-pr-{pr}.apps.internal.example.com"
export IMAGE_REF_TEMPLATE="registry.local/{app}:{tag}"
export IMAGE_TAG_STRATEGY="pr-sha"             # sha|pr|pr-sha
export DEFAULT_CONTAINER_PORT="8080"
export APP_MAPPING_FILE="config/app-mapping.json"
export IMAGE_BUILD_ENABLED="true"              # run docker build + push
export IMAGE_BUILD_DOCKERFILE="Dockerfile"     # optional
export IMAGE_BUILD_PLATFORM="linux/amd64"      # for buildx
export IMAGE_BUILD_USE_BUILDX="true"           # default true

export OPENSHIFT_API_URL="https://api.example.com:6443"  # optional (out-of-cluster)
export OPENSHIFT_TOKEN="..."                             # required if OPENSHIFT_API_URL set
export OPENSHIFT_CA_CERT="/path/to/ca.crt"               # optional
export OPENSHIFT_INSECURE_SKIP_TLS_VERIFY="false"        # optional

export GITHUB_REJECT_FORKS="true"                        # optional
export KEEP_ON_MERGE="false"                             # optional

go run ./cmd/preview-controller
```

## Config: app mapping

`config/app-mapping.json` maps `repo full_name` to app settings.

Example:

```
{
  "org1/repoA": {
    "appName": "myapp",
    "containerPort": 8080,
    "routePath": "",
    "env": {
      "EXAMPLE": "value"
    }
  }
}
```

## Webhook endpoint

`POST /webhook/github` expects GitHub webhooks.

- `pull_request` events (opened/reopened/synchronize/closed) create/update/delete previews.
- `repository` events (created) can notify Slack so an agent can review the new repo and decide whether to add it to `GITHUB_ALLOWED_REPOS` + `config/app-mapping.json`.

### Repository-created notifications

Enable repo-created notifications:

- `GITHUB_REPO_EVENTS_ENABLED=true`
- `GITHUB_REPO_EVENTS_ALLOWED_ORGS=hci-gu` (csv)

Configure Slack notification (pick one):

- Slack bot: `SLACK_BOT_TOKEN=...` + `SLACK_CHANNEL_ID=C0A8ZSYDHJ8`
- Incoming webhook: `SLACK_WEBHOOK_URL=...`

## Stale cleanup

The controller periodically cleans up previews that haven't been updated recently (to handle stale PRs).

- `STALE_CLEANUP_ENABLED=true` (optional; defaults to true)
- `STALE_CLEANUP_INTERVAL=24h` (optional)
- `STALE_MAX_AGE=168h` (optional; defaults to 7 days)

Cleanup deletes previews based on the deployment annotation `preview-controller/last-updated-at` (falls back to `preview-controller/created-at`).

## Health

- `GET /healthz`
- `GET /readyz`
