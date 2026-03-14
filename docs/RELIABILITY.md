# RELIABILITY.md

## What Reliable Means Here

Reliable does not mean perfect acceptance. It means predictable outcomes when dealing with noisy release names, external APIs, and filesystem state.

## Reliability Priorities

1. never create misleading hardlinks silently
2. authenticate and reject malformed requests cleanly
3. degrade safely when metadata providers fail
4. keep config reload/startup behavior legible in logs

## Current Reliability Mechanisms

- API token middleware
- health endpoints
- structured logging
- TVDB/TVMaze fallback strategy
- release comparison gates
- hardlink creation isolated from matching logic

## Known Reliability Gaps

- no explicit end-to-end test harness for real webhook payloads plus filesystem assertions
- processor flow is the main complexity hotspot
- no automated stale-doc detection for operational procedures

## Change Checklist

- Did request validation stay strict?
- Did matching become broader or narrower? Why?
- What happens if TVDB and TVMaze disagree?
- What log line would an operator use to debug this?
- Can the new behavior be exercised with `go run . test pack` or `test parse`?
