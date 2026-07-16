# Go Website

A Go web application with an embedded frontend and a persistent BoltDB
database.

## Local development

Install the Go version declared in `go.mod`, then run:

```bash
go run ./cmd/server
```

The defaults are `ADDRESS=:8080`, `DATA_PATH=data/game.db`, and an empty
`BASE_PATH`. Open <http://localhost:8080/> and run the tests with:

```bash
go test ./...
```

To run the same application under a path prefix, set `BASE_PATH`:

```bash
BASE_PATH=/beta go run ./cmd/server
```

Open <http://localhost:8080/beta/>. The application serves every route under
that prefix, including `/beta/assets`, `/beta/api`, `/beta/room`, `/beta/ws`,
and `/beta/health`; `/beta` redirects to `/beta/`. Leaving `BASE_PATH` empty
keeps the normal root paths.

### Docker Compose

Install Docker Engine or Docker Desktop with the Compose plugin, then run:

```bash
docker compose up --build
```

Open <http://localhost:8080/>. To run locally at `/beta/` instead:

```bash
BASE_PATH=/beta docker compose up --build
```

Use `PORT=3000` to publish a different host port. For background operation:

```bash
docker compose up --build -d
docker compose logs -f
docker compose down
```

Local game data is stored in the `go-website_local-game-data` Docker volume.
It is independent from both deployed environments, so the main branch can run
on port 8080 without opening the production database. Its Compose project is
named `go-website-local`, keeping its containers and network separate from the
deployment stack. Do not use
`docker compose down --volumes` unless the data should be permanently deleted.

## Runtime environments

| Git branch | Environment | URL | Compose service | Data volume |
| --- | --- | --- | --- | --- |
| `main` | Local | <http://192.168.68.60:8080/> | `app` | `go-website_local-game-data` |
| `prod` | Production | <http://192.168.68.60/> | `production` | `go-website_game-data` |
| `beta` | Beta | <http://192.168.68.60/beta/> | `beta` | `go-website_beta-game-data` |

A push to `beta` updates only beta. A push to `prod` updates only production,
so tested code can be promoted from `beta` to `prod` without mixing their
databases.

The deployment stack in `deploy/compose.yaml` contains Caddy, production, and
beta. Only Caddy publishes host port 80; both Go services remain on the private
Compose network at port 8080. Caddy preserves the `/beta` prefix and proxies
WebSockets as well as HTTP. The workflow in `.github/workflows/deploy.yml`:

1. runs tests and validates the deployment configuration on a GitHub-hosted
   runner;
2. builds and pushes an immutable
   `ghcr.io/<owner>/<repository>:sha-<commit>` image;
3. sends the deployment job to the LAN runner; and
4. updates only the service selected by the branch, then verifies its health,
   page, static assets, and lobby API.

Actions serializes deployments for each branch and never cancels one in
progress. A host-side `flock` lock also prevents production and beta Compose
changes from overlapping, even if multiple matching runners are registered.

## One-time server setup

The address `192.168.68.60` is private LAN space and cannot be reached directly
by a standard GitHub-hosted runner. Register a repository-scoped self-hosted
runner on that server (or another trusted machine on the same LAN).

### 1. Prepare the host

Install Docker Engine and the Docker Compose plugin, ensure port 80 is free,
ensure `flock` is available (normally provided by `util-linux`), and allow
outbound HTTPS access to GitHub, GHCR, and Docker Hub. Reserve
`192.168.68.60` in DHCP (or configure it statically) so the published URL does
not move. Confirm:

```bash
docker info
docker compose version
```

The account that runs GitHub Actions must be able to use Docker without an
interactive `sudo` prompt. For a dedicated runner account, add it to the
Docker group, sign out and back in, and verify `docker info` as that account:

```bash
sudo usermod -aG docker <runner-user>
```

Membership in the Docker group is effectively root access. Use a dedicated,
trusted runner account and host, restrict repository write access, and never
run pull-request code on this runner. This is especially important if the
repository is public.

The deployment stack uses `go-website_game-data` for production and
`go-website_beta-game-data` for beta. The root Compose stack uses the third,
independent `go-website_local-game-data` volume. Back up the production volume
before the first cutover. During migration from an older checkout, the workflow
only stops a legacy local `app` container if it is still attached to the
production volume. If the cutover fails, it runs that previous image behind
Caddy (or restarts the original container as a final fallback). It removes the
legacy container only after a successful cutover or rollback.

### 2. Register the Actions runner

In GitHub, open **Settings > Actions > Runners > New self-hosted runner** and
select the server's Linux architecture. Run the download and configuration
commands GitHub displays, appending the custom label:

```bash
./config.sh --url <repository-url> --token <one-time-token> \
  --no-default-labels --labels go-website --name go-website-prod-01
```

Install and start it as the Docker-enabled runner account:

```bash
sudo ./svc.sh install <runner-user>
sudo ./svc.sh start
sudo ./svc.sh status
```

GitHub should show it online with only the dedicated `go-website` label. The
registration token is one-time setup data, not a repository
secret.

### 3. Configure GitHub

Under **Settings > Environments**, create:

- `beta`, allowing deployments only from `beta`;
- `production`, allowing deployments only from `prod`.

No custom deployment secret is required. The workflow uses its short-lived
`GITHUB_TOKEN` with `packages: write` while publishing and `packages: read`
while deploying. If organization policy overrides token permissions, allow
those package permissions for this repository. Leave required environment
reviewers disabled for fully automatic deployment.

Protect both branches against deletion and force-pushes. For production,
prefer requiring a pull request from `beta`, review changes to workflow and
deployment files, and restrict merging to trusted maintainers. A merge or
allowed push to `prod` still deploys automatically.

The deployment workflow must exist on both branches. For a new installation,
create `prod` from the commit containing `.github/workflows/deploy.yml` and
`deploy/compose.yaml`.

### 4. Bootstrap both services

Bootstrap is performed by normal branch pushes; do not manually create the
application containers. Push `prod` first to start production and Caddy, then
push `beta` to start beta:

```bash
git branch prod
git push -u origin prod
git push origin beta
```

The first branch deployed starts Caddy and its selected service. Until the
other branch has deployed, that other route can return `502`. Later pushes
replace only their matching service.

## Operations

Check both environments after bootstrap or deployment:

```bash
curl --fail http://192.168.68.60/health
curl --fail http://192.168.68.60/
curl --fail http://192.168.68.60/beta/health
curl --fail http://192.168.68.60/beta/
```

Inspect the single deployment stack with:

```bash
docker compose --project-name go-website --file deploy/compose.yaml ps
docker compose --project-name go-website --file deploy/compose.yaml logs -f proxy production beta
```

The workflow records the currently running image before an update. If the new
container or its smoke checks fail, it automatically restores the prior image.
During the first production cutover, the legacy local image serves as that
rollback target.

To roll back manually, select an earlier `sha-<commit>` tag from GHCR and
replace only the affected service. For production:

```bash
export PRODUCTION_IMAGE=ghcr.io/<owner>/<repository>:sha-<commit>
docker compose --project-name go-website --file deploy/compose.yaml pull production
docker compose --project-name go-website --file deploy/compose.yaml \
  up -d --no-deps --wait --wait-timeout 90 production
unset PRODUCTION_IMAGE
```

For beta, use `BETA_IMAGE` and the `beta` service. A private GHCR package
requires `docker login ghcr.io` with a token that has `read:packages` before a
manual pull.

An image rollback does not roll back BoltDB data. Keep the three volumes
independent, take a consistent backup before incompatible persistence changes,
and never add `--volumes` to deployment shutdown or cleanup commands.
