# Core Beliefs

## 1. Small Stable Entry Points Beat Giant Manuals

Agents should start from `AGENTS.md`, `ARCHITECTURE.md`, and a relevant index. Do not front-load the whole repo.

## 2. Matching Correctness Is The Product

This repo exists to make season-pack automation more efficient without broadening wrong matches. Any shortcut that increases false positives is product debt.

## 3. Filesystem Side Effects Need Higher Proof

Hardlinks are cheap to create and expensive to clean up once trust is lost. Pathing and target selection changes need tests or concrete verification notes.

## 4. Config Is Part Of The API

`config.yaml`, defaults, runtime parsing, and `schemas/config-schema.json` are one contract surface. Keep them aligned.

## 5. Plans And Docs Are Operational Memory

If a change depends on reasoning that is not obvious from code, capture it in docs or an execution plan. Do not rely on chat-only context.

## 6. External Systems Are Real Constraints

autobrr, qBittorrent, TVDB, and TVMaze shape behavior. Integration assumptions should be explicit and easy to re-check.

## 7. Progressive Disclosure Over Exhaustive Dumping

Indexes should point to the next best document. Specialized details belong in specialized docs.
