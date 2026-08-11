# Season Pack Processing

This context describes how seasonpackarr evaluates an announced season pack and
reports the result without changing its established legacy webhook reason
contract.

## Language

**Processing outcome**:
The result of one candidate, pack, or parse operation. It contains an outcome
kind and a stable legacy webhook reason code, plus a cause only when the outcome
is a failure.
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

**Legacy webhook reason code**:
The stable numeric and text reason defined by the existing webhook contract. It
does not determine whether an outcome is a success, rejection, or failure.
_Avoid_: Status code, severity, outcome kind
