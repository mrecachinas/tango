# tango example environment

Spins up a local MongoDB instance with sample data so you can try tango
immediately.

## Prerequisites

- Docker (with Compose v2)
- Go (to build tango)

Seeding runs `mongosh` inside the MongoDB container; no host `mongosh` is needed.
tango parses shell-style queries itself.

## Quick start

From the **repo root**:

```sh
# 1. Start Mongo, seed data, and launch tango — one command
make example-run

# 2. When finished, tear everything down (including the volume)
make example-down
```

## What gets seeded

| Database     | Collection   | Docs | Notes                                 |
|--------------|-------------|------|---------------------------------------|
| sample_app   | users       | 120  | names, emails, roles, ages            |
| sample_app   | orders      | 200  | nested items array, totals, statuses  |
| sample_app   | events      | 500  | clickstream / log-style documents     |
| inventory    | products    | 80   | SKUs, categories, tags, stock levels  |
| inventory    | warehouses  | 6    | city, capacity, utilization           |

## Example queries to try in tango

```
db.users.find({role: "admin"})
db.orders.findOne({status: "shipped"})
db.events.countDocuments({})
db.products.find({stock: 0}).sort({sku: 1}).limit(10)
db.users.find({_id: ObjectId("…"), createdAt: {$gte: ISODate("2024-01-01")}})
:dbs
```

Use `/` in the document list to enter queries; use `:` for commands such as
`:db sample_app`, `:coll users` and `:sort age -1`.

For connection profiles and a commented theme example, see [tango.yaml](tango.yaml).
Copy it to `~/.tango.yaml`, restrict its permissions with `chmod 600 ~/.tango.yaml`,
then run `./tango --profile local`.
