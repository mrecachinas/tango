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
tango --uri 'mongodb+srv://cluster0.example.net/?authSource=admin' -u admin   # prompts for password
tango -u admin --auth-source admin --mongo-host db.example.com
tango --profile staging                          # connection profile (~/.tango.yaml)
TANGO_MONGO_PASSWORD=secret tango -u admin       # password via env var
```

Settings resolve as flag > environment (`TANGO_MONGO_URI`, `TANGO_MONGO_PASSWORD`) > profile.
Profiles accept `uri`, `mongo_url`, `mongo_port`, `mongo_user`, `mongo_password`, `mongo_db`,
`auth_source` and `refresh_rate` (seconds between metrics refreshes; also `--refresh-rate`).

## Querying

The query bar takes a filter for the current collection (`{age: {$gt: 30}}`) or mongo-shell
calls: `find` (with `.sort()`, `.limit()`, `.skip()`, `.projection()`), `findOne`,
`countDocuments`, `estimatedDocumentCount`, `distinct` and `aggregate` (including `$out`/`$merge`).
Shell helpers such as `ObjectId("…")`, `ISODate("…")`, `NumberLong(…)`, `NumberDecimal("…")`,
`UUID("…")` and `/regex/i` are supported.

Documents are shown, copied and edited in the same shell syntax, so editing preserves BSON types
(int32/int64/double, dates, ObjectIds, …) and field order exactly.

## Keybindings

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Cycle panels |
| `/` | Query bar (MongoDB shell syntax with autocomplete) |
| `:` | Command palette (navigation, sort, export, live mode) |
| `?` | Help overlay |
| `Enter` / `Esc` | Select / go back |
| `n` / `p` | Next / previous page |
| `/` in database/collection lists | Filter the list |
| `t` | Toggle JSON / tree view |
| `s` | Sort (or `:sort field -other`) |
| `d` / `e` / `i` | Delete / edit / insert document |
| `c` / `x` / `C` | Copy / export JSON / export CSV (`:export json\|csv [all] [path]`) |
| `S` | Server stats: active ops, top collections, slow queries (`:killop <opid>`) |
| `W` | Wide mode (full-width documents) |
| `L` | Live mode (auto-refresh) |
| `r` / `R` | Refresh / full refresh |
| `q` | Quit |

## Try It

```sh
make example-run    # starts MongoDB in Docker, seeds data, launches tango
make example-down   # tear down
```

## Tests

```sh
make test       # unit tests
make test-e2e   # drives the TUI against a throwaway MongoDB in Docker
```

## License

MIT
