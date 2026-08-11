// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import (
	"errors"
	"fmt"
)

type OutcomeKind string

const (
	OutcomeSuccess   OutcomeKind = "success"
	OutcomeRejection OutcomeKind = "rejection"
	OutcomeFailure   OutcomeKind = "failure"
)

type FaultClass string

const (
	FaultRequest    FaultClass = "request"
	FaultInternal   FaultClass = "internal"
	FaultDependency FaultClass = "dependency"
)

// Outcome is the authoritative result of one processing operation. Kind
// controls operational handling. Reason explains the result. FaultClass and
// Cause are present only for failures.
type Outcome struct {
	kind       OutcomeKind
	reason     Reason
	faultClass FaultClass
	cause      error
}

func Successful(reason Reason) Outcome {
	return Outcome{kind: OutcomeSuccess, reason: reason}
}

func Rejected(reason Reason) Outcome {
	return Outcome{kind: OutcomeRejection, reason: reason}
}

func Failed(reason Reason, faultClass FaultClass) Outcome {
	return Outcome{
		kind:       OutcomeFailure,
		reason:     reason,
		faultClass: faultClass,
		cause:      errors.New(reason.Message()),
	}
}

func FailedBecause(reason Reason, faultClass FaultClass, cause error) Outcome {
	if cause == nil {
		return Failed(reason, faultClass)
	}

	return Outcome{kind: OutcomeFailure, reason: reason, faultClass: faultClass, cause: cause}
}

func (o Outcome) Kind() OutcomeKind {
	return o.kind
}

func (o Outcome) Reason() Reason {
	return o.reason
}

func (o Outcome) FaultClass() FaultClass {
	return o.faultClass
}

func (o Outcome) Cause() error {
	return o.cause
}

func (o Outcome) Validate() error {
	if !o.reason.Valid() {
		return fmt.Errorf("unknown outcome reason: %q", o.reason)
	}

	switch o.kind {
	case OutcomeSuccess, OutcomeRejection:
		if o.faultClass != "" {
			return fmt.Errorf("%s outcome must not have a fault class", o.kind)
		}
		if o.cause != nil {
			return fmt.Errorf("%s outcome must not have a cause", o.kind)
		}
	case OutcomeFailure:
		switch o.faultClass {
		case FaultRequest, FaultInternal, FaultDependency:
		default:
			return fmt.Errorf("unknown fault class: %q", o.faultClass)
		}
		if o.cause == nil {
			return errors.New("failure outcome must have a cause")
		}
	default:
		return fmt.Errorf("unknown outcome kind: %q", o.kind)
	}

	return nil
}
