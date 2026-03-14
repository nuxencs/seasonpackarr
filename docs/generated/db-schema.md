# Generated Durable Surfaces

## Database Status

`seasonpackarr` does not currently maintain an application database schema.

This file exists as a stable generated-artifact slot because agents often expect one. Today, the durable operational surfaces are:

- `config.yaml`
- `schemas/config-schema.json`
- log files
- Docker/systemd packaging artifacts

## Guidance

If the project later adds SQLite, Postgres, BoltDB, or similar persistence, replace this file with generated schema documentation and record how it is produced.
