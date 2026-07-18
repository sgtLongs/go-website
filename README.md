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

Once the local stack exists, restart its current image without rebuilding it:

```bash
make restart-local
```

After changing application code or embedded frontend assets, rebuild and
recreate the local app instead. Docker preserves the Go module and compiler
caches between these builds, so unchanged packages do not need to be compiled
again:

```bash
make rebuild-local
```

Local game data is stored in the `go-website_local-game-data` Docker volume.
It is independent from every deployed environment, so the main branch can run
on port 8080 without opening the production database. Its Compose project is
named `go-website-local`, keeping its containers and network separate from the
deployment stack. Do not use
`docker compose down --volumes` unless the data should be permanently deleted.

## Runtime environments

| Git branch | Environment | URL | Compose service | Data volume |
| --- | --- | --- | --- | --- |
| `main` | Development | <https://tinkersplayground.com/dev/> | `dev` | `go-website_dev-game-data` |
| `beta` | Beta | <https://tinkersplayground.com/beta/> | `beta` | `go-website_beta-game-data` |
| `prod` | Production | <https://tinkersplayground.com/> | `production` | `go-website_game-data` |
| any checked-out branch | Laptop/PC local | <http://localhost:8080/> | `app` | `go-website_local-game-data` |

A push to `main`, `beta`, or `prod` updates only its matching environment.
This allows changes to move from development to beta to production without
mixing their databases.

The deployment stack in `deploy/compose.yaml` contains Caddy, development,
beta, and production. Only Caddy publishes host ports 80 and 443; the three Go
services remain on the private Compose network at port 8080. Caddy preserves
the `/dev` and `/beta` prefixes, proxies WebSockets as well as HTTP, redirects
public HTTP traffic to HTTPS, and automatically obtains and renews the TLS
certificate. The workflow in
`.github/workflows/deploy.yml`:

1. runs tests and validates the deployment configuration on a GitHub-hosted
   runner;
2. builds and pushes an immutable
   `ghcr.io/<owner>/<repository>:sha-<commit>` image;
3. sends the deployment job to the LAN runner; and
4. updates only the service selected by the branch, then verifies its health,
   page, static assets, and lobby API.

Actions serializes deployments for each branch and never cancels one in
progress. A host-side `flock` lock also prevents deployment Compose
changes from overlapping, even if multiple matching runners are registered.

## Laptop setup and outside-LAN workflow

This workflow does not require remote desktop, an open SSH port, or direct
access from the laptop to the PC. GitHub is the relay: the laptop pushes code
to GitHub, and the self-hosted Actions runner on the PC makes an outbound
connection to receive and deploy the job.

### 1. Install the laptop tools

Install Git, the Go version declared in `go.mod`, and an editor. Docker Desktop
is optional but recommended because it reproduces the container used on the
PC. Authenticate Git to GitHub using GitHub CLI (`gh auth login`) or the
operating system's Git credential manager. Never put a GitHub token in the
repository or a remote URL.

Clone the repository and select `main`:

```bash
git clone https://github.com/sgtLongs/go-website.git
cd go-website
git switch main
git pull --ff-only origin main
```

### 2. Develop and test locally

With Go installed:

```bash
go test ./...
go run ./cmd/server
```

Open <http://localhost:8080/>. Alternatively, use Docker:

```bash
docker compose up --build
```

Stop the foreground server with `Ctrl+C`; stop Compose with
`docker compose down`. Local data stays on the laptop in its own Docker volume.

### 3. Deploy `main` to `/dev`

Commit only after the local tests pass, then push `main`:

```bash
git status
git add <files-you-changed>
git commit -m "describe the change"
git push origin main
```

In GitHub, open **Actions > Test, publish, and deploy** and watch the run. The
GitHub-hosted job tests and builds the image; the PC's runner deploys only the
`dev` service. When both jobs are green, verify:

```bash
curl --fail --show-error https://tinkersplayground.com/dev/health
```

Then open <https://tinkersplayground.com/dev/>. A failed health or smoke check
automatically rolls `dev` back to its previously running image. Promoting a
tested change to `beta` or `prod` should be done with a pull request or merge;
do not point multiple services at the same data volume.

If the deployment job stays queued, the PC or its Actions runner is offline.
The website can remain online in that state, but new code cannot deploy until
the PC and runner regain internet access. If the build job fails, fix the
reported test/build error on the laptop and push another commit.

## One-time server setup

The address `192.168.68.60` is private LAN space and cannot be reached directly
by a standard GitHub-hosted runner. Register a repository-scoped self-hosted
runner on that server (or another trusted machine on the same LAN).

### 1. Prepare the host

Install Docker Engine and the Docker Compose plugin, ensure ports 80 and 443
are free,
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

The deployment stack uses `go-website_game-data` for production,
`go-website_beta-game-data` for beta, and `go-website_dev-game-data` for
development. The root Compose stack uses another independent
`go-website_local-game-data` volume. Back up the production volume
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

Under **Settings > Secrets and variables > Actions > Variables**, add the
repository variable `SITE_DOMAIN`. Its value must be the public hostname only,
without a scheme or path; for this site, use `tinkersplayground.com`. The
deployment uses `localhost` when this variable is absent, which keeps local
testing available but does not request a public certificate.

Under **Settings > Environments**, create:

- `dev`, allowing deployments only from `main`;
- `beta`, allowing deployments only from `beta`;
- `production`, allowing deployments only from `prod`.

No custom deployment secret is required. The workflow uses its short-lived
`GITHUB_TOKEN` with `packages: write` while publishing and `packages: read`
while deploying. If organization policy overrides token permissions, allow
those package permissions for this repository. Leave required environment
reviewers disabled for fully automatic deployment.

Protect all three deployment branches against deletion and force-pushes. For
production, prefer requiring a pull request from `beta`, review changes to
workflow and deployment files, and restrict merging to trusted maintainers. A
merge or allowed push to `prod` still deploys automatically.

The deployment workflow must exist on all three branches. For a new
installation, create `beta` and `prod` from the commit containing
`.github/workflows/deploy.yml` and `deploy/compose.yaml`.

### 4. Configure Namecheap and the router

In Namecheap's **Advanced DNS** page, create an `A Record` for the chosen host
(`@` for the bare domain or, for example, `game` for `game.example.com`) and
set its value to the router's public IPv4 address. Remove any conflicting
parking, redirect, or `A`/`AAAA` records for that same host. A short TTL such
as 5 minutes is useful during setup.

On the internet router, forward TCP ports 80 and 443 and UDP port 443 to this
server at `192.168.68.60`. Also allow inbound TCP 80/443 and UDP 443 in the
server firewall, if one is enabled. TCP 80 must remain reachable because the
certificate authority can use it to validate the hostname; TCP 443 serves
HTTPS, and UDP 443 enables HTTP/3.

The DNS record must resolve to the same address shown as the router's WAN
address. If those addresses differ, the connection may be behind carrier-grade
NAT (CGNAT), and ordinary Namecheap DNS plus port forwarding cannot expose the
site; request a public IPv4 address from the ISP or use a tunnel/VPS instead.

After DNS has propagated and the ports are forwarded, deploy the branches and
verify from a device not connected to the home Wi-Fi:

```bash
curl --fail --show-error https://tinkersplayground.com/health
curl --fail --show-error https://tinkersplayground.com/beta/health
curl --fail --show-error https://tinkersplayground.com/dev/health
```

### 5. Bootstrap all services

Bootstrap is performed by normal branch pushes; do not manually create the
application containers. Push `prod` first to start production and Caddy, then
push `beta` and `main`:

```bash
git branch prod
git push -u origin prod
git push origin beta
git push origin main
```

The first branch deployed starts Caddy and its selected service. Until the
other branches have deployed, their routes can return `502`. Later pushes
replace only their matching service.

## Operations

Check all environments after bootstrap or deployment:

```bash
curl --fail http://192.168.68.60/health
curl --fail http://192.168.68.60/
curl --fail http://192.168.68.60/beta/health
curl --fail http://192.168.68.60/beta/
curl --fail http://192.168.68.60/dev/health
curl --fail http://192.168.68.60/dev/
```

Inspect the single deployment stack with:

```bash
docker compose --project-name go-website --file deploy/compose.yaml ps
docker compose --project-name go-website --file deploy/compose.yaml logs -f proxy production beta dev
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

For beta, use `BETA_IMAGE` and the `beta` service; for development, use
`DEV_IMAGE` and `dev`. A private GHCR package
requires `docker login ghcr.io` with a token that has `read:packages` before a
manual pull.

An image rollback does not roll back BoltDB data. Keep all data volumes
independent, take a consistent backup before incompatible persistence changes,
and never add `--volumes` to deployment shutdown or cleanup commands.
