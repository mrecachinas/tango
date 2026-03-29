# Tango - MongoDB TUI (k9s-inspired)

## Current State

**What exists:**
- 3-panel layout: Databases → Collections → Documents (compact JSON list, expandable detail)
- Query bar with mongo shell syntax (`db.collection.find({...})`) and autocomplete
- Relaxed JSON parser (unquoted keys, `$` operators, single quotes)
- Queries execute over the wire via Go driver — no `mongosh` dependency
- Supported methods: `find`, `findOne`, `count`/`countDocuments`, `aggregate`, `distinct`
- Pagination (50 docs/page, `n`/`p` to navigate)
- Persistent single MongoDB connection
- MongoDB connection with CLI flags (`--mongo-host`, `--mongo-port`, `-d`, `-u`, `-p`)
- Inline db/collection counts in gray
- Status bar with keybinding hints
- Tab/Shift+Tab panel cycling, `/` to search, `q` to quit, `Esc` to go back
- Makefile with `fmt`, `vet`, `lint`, `test`, `build`, `clean`, `run`
- Example environment: Docker Compose + seed script (2 databases, 5 collections, ~900 docs)
- Uses `tview` for TUI, `pflag` for CLI flags

---

## Phased Plan

### Phase 1: Foundation & Navigation ✅
**Goal:** Solid k9s-like navigation and visual structure

- [x] Add header bar with breadcrumbs (e.g., `db > collection > document`)
- [x] Add footer/status bar with connection info and keybindings hint
- [x] Add vim-style navigation (`j/k` for up/down, `Enter` to select, `Esc` to go back)
- [x] Add `q` to quit
- [x] Refactor: extract UI components into separate files

### Phase 2: Styling & Polish ✅
**Goal:** Look and feel like k9s

- [x] Add k9s-inspired color scheme (dark theme, colored borders, highlights)
- [x] Style the header/footer
- [x] Add panel focus indicators (highlight active panel)
- [x] Add loading indicators for async operations
- [x] Better document preview formatting (pretty-print JSON, inline gray counts)

### Phase 3: Search & Filtering ✅
**Goal:** Fast data exploration

- [x] Add `/` to focus search bar
- [x] Mongo shell query syntax (`db.collection.find({...})`) with autocomplete
- [x] Relaxed JSON filter support (unquoted keys, `$` operators, single quotes)
- [x] Queries run over the wire via Go driver (no mongosh needed)
- [x] Pagination for large document sets (50/page, `n`/`p` keys)
- [x] Inline type-to-filter for databases/collections lists

### Phase 4: Document Viewer ✅
**Goal:** Rich document inspection

- [x] Full-screen document detail view (`Enter` to expand, `Esc` to collapse)
- [x] Pretty-printed JSON in detail view
- [x] Syntax-highlighted JSON (colors for keys, strings, numbers, etc.)
- [x] Collapsible nested objects/arrays (tree view with expand/collapse)
- [x] Copy document/field to clipboard (`c` in detail view)

### Phase 5: CRUD Operations ✅
**Goal:** Actually manage MongoDB data

- [x] View document (`Enter`)
- [x] Delete document (`d` with confirmation modal)
- [x] Edit document (`e` — open in `$EDITOR`)
- [x] Insert new document (`i` — open `$EDITOR` with template)
- [x] Drop collection (`D` with confirmation modal)

### Phase 6: Advanced Features ✅
**Goal:** Power-user functionality

- [x] Multiple connection profiles (`~/.tango.yaml` + `--profile` flag)
- [x] Aggregation pipeline support (via query bar)
- [x] `distinct` support (via query bar)
- [x] Index management view (`I` to view, `X` to drop with confirmation)
- [x] Server stats/monitoring (live sparklines + gauge bars)
- [x] Export documents (`x` — JSON file)

### Build & Dev Environment ✅ (new)
- [x] Makefile (`fmt`, `vet`, `lint`, `test`, `build`, `clean`, `run`)
- [x] Example Docker Compose environment with MongoDB 7
- [x] Seed script (2 databases, 5 collections, ~900 docs)
- [x] `make example-run` one-command demo

---

## Tech Debt / Refactoring Needed
- [x] Persistent MongoDB connection (single client, reused)
- [x] Error handling (errors display in UI, no more panics)
- [x] Context management (configurable `--timeout` flag, `newCtx()` helper)
- [x] Upgrade dependencies (Go 1.23, tview v0.42.0, mongo-driver v1.17.9)

---

## Status: All phases complete ✅

