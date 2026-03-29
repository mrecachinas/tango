// Seed script for tango development / demo.
// Run via:  mongosh --quiet --file /seed/seed.js
//
// Creates two databases with a handful of collections each,
// populated with enough data to exercise pagination, search, etc.

// ── helpers ──────────────────────────────────────────────────────

function randomFrom(arr) {
  return arr[Math.floor(Math.random() * arr.length)];
}

function randomInt(min, max) {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

function randomDate(startYear, endYear) {
  const start = new Date(startYear, 0, 1).getTime();
  const end = new Date(endYear, 11, 31).getTime();
  return new Date(start + Math.random() * (end - start));
}

// ── sample_app database ─────────────────────────────────────────

const appDB = db.getSiblingDB("sample_app");
appDB.dropDatabase();

// users
const firstNames = ["Alice", "Bob", "Carol", "Dave", "Eve", "Frank", "Grace", "Heidi", "Ivan", "Judy"];
const lastNames = ["Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Wilson", "Moore"];
const roles = ["admin", "editor", "viewer"];

const users = [];
for (let i = 0; i < 120; i++) {
  const first = randomFrom(firstNames);
  const last = randomFrom(lastNames);
  users.push({
    name: first + " " + last,
    email: (first + "." + last + i + "@example.com").toLowerCase(),
    role: randomFrom(roles),
    age: randomInt(18, 65),
    active: Math.random() > 0.2,
    created_at: randomDate(2020, 2025),
  });
}
appDB.users.insertMany(users);
print("  ✓ sample_app.users — " + users.length + " docs");

// orders
const statuses = ["pending", "shipped", "delivered", "cancelled"];
const products = ["Widget", "Gadget", "Doohickey", "Thingamajig", "Whatchamacallit"];

const orders = [];
for (let i = 0; i < 200; i++) {
  const items = [];
  const numItems = randomInt(1, 5);
  for (let j = 0; j < numItems; j++) {
    items.push({
      product: randomFrom(products),
      qty: randomInt(1, 10),
      price: parseFloat((Math.random() * 100 + 1).toFixed(2)),
    });
  }
  orders.push({
    user_email: randomFrom(users).email,
    status: randomFrom(statuses),
    items: items,
    total: parseFloat(items.reduce((s, it) => s + it.qty * it.price, 0).toFixed(2)),
    ordered_at: randomDate(2023, 2025),
  });
}
appDB.orders.insertMany(orders);
print("  ✓ sample_app.orders — " + orders.length + " docs");

// events (logs)
const eventTypes = ["page_view", "click", "signup", "purchase", "logout", "error"];
const pages = ["/", "/products", "/cart", "/checkout", "/account", "/help"];

const events = [];
for (let i = 0; i < 500; i++) {
  events.push({
    type: randomFrom(eventTypes),
    page: randomFrom(pages),
    user_email: randomFrom(users).email,
    metadata: { browser: randomFrom(["Chrome", "Firefox", "Safari", "Edge"]), os: randomFrom(["Windows", "macOS", "Linux", "iOS", "Android"]) },
    timestamp: randomDate(2024, 2025),
  });
}
appDB.events.insertMany(events);
print("  ✓ sample_app.events — " + events.length + " docs");

// ── inventory database ──────────────────────────────────────────

const invDB = db.getSiblingDB("inventory");
invDB.dropDatabase();

// products
const categories = ["Electronics", "Clothing", "Home & Garden", "Books", "Toys"];
const tags = ["sale", "new", "clearance", "popular", "limited"];

const prods = [];
for (let i = 0; i < 80; i++) {
  prods.push({
    sku: "SKU-" + String(i + 1).padStart(5, "0"),
    name: randomFrom(products) + " " + randomFrom(["Pro", "Lite", "Max", "Mini", "Ultra"]),
    category: randomFrom(categories),
    price: parseFloat((Math.random() * 500 + 5).toFixed(2)),
    stock: randomInt(0, 200),
    tags: Array.from({ length: randomInt(1, 3) }, () => randomFrom(tags)),
    created_at: randomDate(2022, 2025),
  });
}
invDB.products.insertMany(prods);
print("  ✓ inventory.products — " + prods.length + " docs");

// warehouses
const cities = ["New York", "Los Angeles", "Chicago", "Houston", "Phoenix", "Seattle"];
const warehouses = cities.map((city, i) => ({
  code: "WH-" + String(i + 1).padStart(3, "0"),
  city: city,
  capacity: randomInt(500, 5000),
  utilization: parseFloat((Math.random() * 0.6 + 0.3).toFixed(2)),
}));
invDB.warehouses.insertMany(warehouses);
print("  ✓ inventory.warehouses — " + warehouses.length + " docs");

print("\nSeed complete.");
