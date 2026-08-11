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

// Outcome is the authoritative result of one processing operation. StatusCode
// preserves the legacy webhook reason contract. Kind controls operational
// handling.
type Outcome struct {
	kind       OutcomeKind
	statusCode StatusCode
	cause      error
}

func Successful(statusCode StatusCode) Outcome {
	return Outcome{kind: OutcomeSuccess, statusCode: statusCode}
}

func Rejected(statusCode StatusCode) Outcome {
	return Outcome{kind: OutcomeRejection, statusCode: statusCode}
}

func Failed(statusCode StatusCode) Outcome {
	return Outcome{
		kind:       OutcomeFailure,
		statusCode: statusCode,
		cause:      errors.New(statusCode.String()),
	}
}

func FailedBecause(statusCode StatusCode, cause error) Outcome {
	if cause == nil {
		return Failed(statusCode)
	}

	return Outcome{kind: OutcomeFailure, statusCode: statusCode, cause: cause}
}

func (o Outcome) Kind() OutcomeKind {
	return o.kind
}

func (o Outcome) StatusCode() StatusCode {
	return o.statusCode
}

func (o Outcome) Cause() error {
	return o.cause
}

func (o Outcome) Validate() error {
	if o.statusCode.String() == "" {
		return fmt.Errorf("unknown status code: %d", o.statusCode)
	}

	switch o.kind {
	case OutcomeSuccess, OutcomeRejection:
		if o.cause != nil {
			return fmt.Errorf("%s outcome must not have a cause", o.kind)
		}
	case OutcomeFailure:
		if o.cause == nil {
			return errors.New("failure outcome must have a cause")
		}
	default:
		return fmt.Errorf("unknown outcome kind: %q", o.kind)
	}

	return nil
}
