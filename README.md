# Go Website

A Go web application with an embedded frontend and a persistent BoltDB database.

## Run with Docker

Install Docker Desktop (Windows/macOS) or Docker Engine with the Compose plugin
(Linux), then run:

```bash
git clone https://github.com/sgtLongs/go-website.git
cd go-website
docker compose up --build
```

Open <http://localhost:8080>. Stop the application with `Ctrl+C`.

Later starts can use:

```bash
docker compose up
```

To run in the background:

```bash
docker compose up -d
docker compose logs -f
```

Stop a background instance with `docker compose down`. Game data is kept in the
named Docker volume `go-website_game-data`, so `docker compose down` and image
rebuilds do not delete it. Do not use `docker compose down --volumes` unless you
want to permanently delete the stored game data.

To use a different host port:

```bash
PORT=3000 docker compose up
```

On PowerShell:

```powershell
$env:PORT = "3000"
docker compose up
```

## Update on another computer

```bash
git pull
docker compose up --build -d
```

The database is not stored in Git. Each computer gets its own Docker volume.
To migrate existing game data, stop the application and copy or restore the
database separately.

## Run without Docker

Install the Go version declared in `go.mod`, then run:

```bash
go run ./cmd/server
```

The native defaults are `ADDRESS=:8080` and `DATA_PATH=data/game.db`.

## Test

```bash
go test ./...
```
