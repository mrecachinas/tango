# 🥭 tango

A k9s-inspired TUI for MongoDB. Browse databases, query collections, manage documents — all from your terminal.

![Go](https://img.shields.io/badge/Go-1.23-blue) ![MongoDB](https://img.shields.io/badge/MongoDB-4.x--8.x-green)

![tango screenshot](docs/screenshot.png)

## Install

```sh
go install github.com/mrecachinas/tango/cmd/tango@latest
```

Or build from source:

```sh
git clone https://github.com/mrecachinas/tango.git
cd tango
make build
```

## Usage

```sh
tango --mongo-host localhost --mongo-port 27017
tango -u admin -p secret --mongo-host db.example.com
tango --profile staging                          # connection profile (~/.tango.yaml)
TANGO_MONGO_PASSWORD=secret tango -u admin       # password via env var
```

## Keybindings

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Cycle panels |
| `/` | Query bar (MongoDB shell syntax with autocomplete) |
| `:` | Command palette (navigation, sort, export, live mode) |
| `?` | Help overlay |
| `Enter` / `Esc` | Select / go back |
| `n` / `p` | Next / previous page |
| `t` | Toggle JSON / tree view |
| `d` / `e` / `i` | Delete / edit / insert document |
| `c` / `x` / `C` | Copy / export JSON / export CSV |
| `W` | Wide mode (full-width documents) |
| `L` | Live mode (auto-refresh) |
| `r` / `R` | Refresh / full refresh |
| `q` | Quit |

## Try It

```sh
make example-run    # starts MongoDB in Docker, seeds data, launches tango
make example-down   # tear down
```

## License

MIT
