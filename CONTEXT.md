# Season Pack Processing

This context describes how seasonpackarr evaluates an announced season pack and
reports the result through its processing and transport boundaries.

## Language

**Processing outcome**:
The result of one candidate, pack, or parse operation. It contains an outcome
kind and a semantic reason. A failure also contains its origin and cause.
_Avoid_: Return code, result error

**Success**:
A processing outcome that completed the requested operation.
_Avoid_: Match status, good code

**Rejection**:
A processing outcome where the request does not meet matching or import policy.
It is expected product behavior, not an operational fault.
_Avoid_: Soft error, expected error

**Failure**:
A processing outcome where an operational fault prevented completion. It has a
cause for logs and notifications. HTTP responses keep the canonical reason text.
_Avoid_: Hard error, error status

**Reason**:
A named explanation for a processing outcome. It is independent of transport
status and notification severity.
_Avoid_: Reason code, status code, severity

**Failure class**:
The origin of a failure: invalid caller input, a seasonpackarr operation, or a
torrent-client dependency.
_Avoid_: HTTP class, error code, severity
