# FRONTEND.md

This repo does not currently ship a browser UI or frontend application.

User-facing surfaces today:

- CLI commands under `cmd/`
- authenticated HTTP endpoints under `/api`
- config file authoring
- logs and Discord notifications

Implication for agents:

- do not invent frontend architecture docs or design-system guidance
- if a future UI is added, create a dedicated frontend section and move UI-specific beliefs there
- until then, treat API clarity, CLI ergonomics, config legibility, and notification quality as the effective user interface
