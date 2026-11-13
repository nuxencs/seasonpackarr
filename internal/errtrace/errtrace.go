// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package errtrace adds stack traces to unexpected operational errors.
package errtrace

import (
	"context"
	"errors"

	pkgerrors "github.com/pkg/errors"
)

type stackTracer interface {
	error
	StackTrace() pkgerrors.StackTrace
}

// WithStack adds a stack trace unless err is nil, is a normal context
// cancellation, or already contains one. Standard errors.Is and errors.As
// traversal continues through the returned error.
func WithStack(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if _, ok := errors.AsType[stackTracer](err); ok {
		return err
	}

	return pkgerrors.WithStack(err)
}
