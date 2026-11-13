# RELIABILITY.md

## What Reliable Means Here

Reliable does not mean perfect acceptance. It means predictable outcomes when dealing with noisy release names, torrent-client state, and filesystem state.

## Reliability Priorities

1. never create misleading hardlinks silently
2. authenticate and reject malformed requests cleanly
3. fail closed when exact torrent coverage cannot be established
4. keep config reload/startup behavior legible in logs

## Current Reliability Mechanisms

- API token middleware
- health endpoints
- structured logging
- validated, immutable config snapshots that keep the last valid config after a rejected reload
- environment precedence reapplied on every config reload
- release comparison gates
- a 30-second client inventory cache shared by candidate and pack checks
- a 2-minute accepted-plan cache with safe recomputation on a miss
- distinct torrent-target accounting that caps smart-mode coverage at 100 percent
- hardlink creation isolated from matching logic
- authenticated HTTP component coverage across candidate, pack, parse, cache invalidation, and filesystem failure paths
- safe stack capture for unexpected filesystem, torrent-client, notification, and server errors; expected rejections and cancellation stay stack-free
- request cancellation propagated through processing and context-aware torrent-client calls
- bounded signal shutdown that drains HTTP handlers and tracked notification tasks

## Known Reliability Gaps

- no live autobrr-to-real-client end-to-end harness
- expected filter rejections and operational errors do not have separate outcome classes
- torrent-derived target paths do not enforce containment beneath the import root
- no automated stale-doc detection for operational procedures

## Change Checklist

- Did request validation stay strict?
- Does cancellation stop new client work and polling promptly?
- Can shutdown finish within its fixed deadline?
- Did matching become broader or narrower? Why?
- Can duplicate or unrelated client episodes broaden acceptance?
- What log line would an operator use to debug this?
- Can the new behavior be exercised with `go run . test candidate`, `test pack`, or `test parse`?
