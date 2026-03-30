# 🥭 tango

A k9s-inspired TUI for MongoDB. Browse databases, query collections, manage documents — all from your terminal.

![Go](https://img.shields.io/badge/Go-1.23-blue) ![MongoDB](https://img.shields.io/badge/MongoDB-4.x--8.x-green)

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

# With auth
tango -u admin -p secret --mongo-host db.example.com

# With connection profile
tango --profile staging

# Password via env var (recommended)
TANGO_MONGO_PASSWORD=secret tango -u admin

# Custom timeout
tango --timeout 30
```

## Layout

![tango screenshot](docs/screenshot.png)

**Wide mode** (`W`): collapses the side panels for full-width documents with breadcrumb navigation. Esc walks back through docs → collections → databases.

## Keybindings

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Cycle panels |
| `/` | Focus query bar |
| `:` | Command palette |
| `?` | Help overlay |
| `Enter` | Select / expand document |
| `Esc` | Go back |
| `n` / `p` | Next / previous page |
| `t` | Toggle JSON / tree view |
| `d` | Delete document |
| `e` | Edit in $EDITOR |
| `i` | Insert new document |
| `c` | Copy to clipboard |
| `x` / `C` | Export JSON / CSV |
| `s` | Sort documents |
| `Space` / `a` / `A` | Select / select all / deselect all |
| `I` | View indexes |
| `D` | Drop collection |
| `S` | Server stats |
| `L` | Toggle live mode |
| `W` | Toggle wide mode |
| `r` / `R` | Refresh / full refresh |
| `q` | Quit |

## Query Bar

Supports full MongoDB shell syntax with autocomplete:

```
db.users.find({age: {$gt: 25}})
db.orders.findOne({status: "shipped"})
db.events.aggregate([{$group: {_id: "$type", count: {$sum: 1}}}])
db.users.distinct("role")
db.logs.count({level: "error"})
```

Relaxed JSON — unquoted keys, `$` operators, single quotes all work.

## Command Palette

Press `:` to enter command mode (with autocomplete):

| Command | Action |
|---------|--------|
| `:databases` / `:dbs` | Navigate to database list |
| `:database <name>` / `:use <name>` | Switch to a database |
| `:collections` / `:colls` | Navigate to collection list |
| `:collection <name>` | Switch to a collection |
| `:indexes` / `:idx` | View collection indexes |
| `:stats` | Server stats (currentOp, slow queries, top collections) |
| `:sort <field>` | Sort by field (`-field` for descending) |
| `:limit <n>` | Change page size |
| `:export json` / `:export csv` | Export current results |
| `:watch <interval>` | Live mode (e.g. `:watch 5s`) |
| `:refresh` / `:refresh!` | Reload current view / full reload |

## Live Metrics

The metrics bar shows real-time sparklines and gauges:

- **ops/s**, **connections**, **ping latency** — sparklines with rolling history
- **memory**, **network I/O**, **queue depth**, **open cursors** — sparklines
- **cache utilization**, **WiredTiger tickets** — gauge bars
- **replication lag** — shown when connected to a replica set (color-coded)

## Features

- **Snapshot pagination** — pages are stable; new data only appears on refresh
- **Live mode** (`L`) — auto-refresh with configurable interval
- **RBAC-aware** — auto-detects read-only connections, disables write operations
- **State persistence** — restores last DB/collection, page size, query history on relaunch
- **Multi-select** — batch delete and export
- **In-document search** — `/` in detail view highlights matches

## Connection Profiles

Create `~/.tango.yaml`:

```yaml
profiles:
  local:
    mongo_url: localhost
    mongo_port: 27017
  staging:
    mongo_url: staging.example.com
    mongo_port: 27017
    mongo_user: readonly
    mongo_password: secret
    mongo_db: myapp
```

Then: `tango --profile staging`

## Example Environment

```sh
make example-run    # starts MongoDB in Docker, seeds data, launches tango
make example-down   # tear down
```

## License

MIT
