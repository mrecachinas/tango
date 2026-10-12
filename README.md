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

### Theme

The Bubble Tea interface uses the terminal's background, thin focused panes, and
a compact header/footer. Put a global `theme:` block in `~/.tango.yaml` (or
override it inside a connection profile):

```yaml
theme:
  name: auto # dark (default), light, mono, or auto
  colors:
    accent: "#89b4fa"
    border: "238"
profiles:
  local:
    uri: mongodb://localhost:27017
```

`colors` accepts hex colors or ANSI color numbers for `text`, `muted`, `faint`,
`border`, `accent`, `onaccent`, `cursor`, `select`, `match`, `onmatch`, `ok`, `warn`,
`err`, `info`, `key`, `string`, `number`, `bool`, `null`, `helper`, and `punct`.
`NO_COLOR` forces monochrome. The sidebar hides automatically below 90 columns;
`W` can still show it, and `Tab` reveals it when moving focus back to databases.

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
| `Tab` / `Shift+Tab` | Cycle sidebar ↔ content |
| `/` | Query bar (MongoDB shell syntax with autocomplete) |
| `:` | Command palette (navigation, sort, export, live mode) |
| `?` | Help overlay |
| `Enter` / `Esc` | Select / go back |
| `n` / `p` | Next / previous page |
| `/` in database/collection lists | Filter the list |
| `t` | Toggle JSON / tree view |
| `v` | Toggle table / JSON document rows |
| `s` | Prefilled sort prompt (`:sort field -other` or `:sort field -1`) |
| `d` / `e` / `i` | Delete / edit / insert document |
| `c` / `x` / `C` | Copy / export JSON / export CSV (`:export json\|csv [all] [path]`) |
| `S` | Server stats: active ops, top collections, slow queries (`:killop <opid>`) |
| `W` | Toggle sidebar (full-width documents when hidden) |
| `M` | Toggle expanded metrics header |
| `I` / `D` (sidebar) | Indexes / drop collection (or database row) |
| `X` (indexes) | Drop index |
| `Space` / `a` / `A` | Toggle selection + advance / select all / clear |
| `j` / `k`, `g` / `G`, `Ctrl+d` / `Ctrl+u` | Move, first/last, half-page |
| `L` | Live mode (auto-refresh) |
| `r` / `R` | Refresh / full refresh |
| `q` / `Ctrl+c` | Quit (`Ctrl+c` always works, including in inputs) |

Enter opens a document in the collapsible tree; `t` switches to syntax-highlighted
JSON. `/` searches either detail view, and `n` / `N` steps through JSON matches.
Delete prompts require lowercase `y` (any other key cancels). Dropping a database
or collection requires typing its exact name; `:drop database` is also available.
The command palette accepts fuzzy choices and raw commands such as `:db foo`,
`:sort ts -1`, and `:q`. Inputs capture printable shortcut keys until closed.

## Try It

```sh
make example-run    # starts MongoDB in Docker, seeds data, launches tango
make example-down   # tear down
```

## Tests

```sh
make test       # unit tests
make test-e2e   # drives the root model against a throwaway MongoDB in Docker
```

The end-to-end suite pumps Bubble Tea commands and `ApplyMsg` closures directly,
without a terminal, and is skipped unless `TANGO_E2E_MONGO_URI` is set.

## License

MIT
