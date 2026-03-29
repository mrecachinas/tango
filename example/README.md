# tango example environment

Spins up a local MongoDB instance with sample data so you can try tango
immediately.

## Prerequisites

- Docker (with Compose v2)
- `mongosh` on your `$PATH` (for the seed step and tango's query bar)

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
db.events.count()
db.products.find({stock: 0})
show dbs
```
