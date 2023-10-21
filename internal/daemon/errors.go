// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2023 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package daemon

import (
	"fmt"
	"net/http"

	"github.com/snapcore/fdemanager/client"
)

// apiError represents an error meant for returning to the client.
// It can serialize itself to our standard JSON response format.
type apiError struct {
	// Status is the error HTTP status code.
	Status  int
	Message string
	// Kind is the error kind. See client/errors.go
	Kind  client.ErrorKind
	Value errorValue
}

func (ae *apiError) Error() string {
	kindOrStatus := "api"
	if ae.Kind != "" {
		kindOrStatus = fmt.Sprintf("api: %s", ae.Kind)
	} else if ae.Status != 400 {
		kindOrStatus = fmt.Sprintf("api %d", ae.Status)
	}
	return fmt.Sprintf("%s (%s)", ae.Message, kindOrStatus)
}

func (ae *apiError) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	ae.Write(w)
}

func (ae *apiError) Write(w http.ResponseWriter) {
	rsp := &resp{
		Type:   responseTypeError,
		Status: ae.Status,
		Result: &errorResult{
			Message: ae.Message,
			Kind:    ae.Kind,
			Value:   ae.Value,
		},
	}
	rsp.Write(w)
}

// check it implements response
var _ response = (*apiError)(nil)
var _ error = (*apiError)(nil)

type errorValue interface{}

type errorResult struct {
	Message string `json:"message"` // note no omitempty
	// Kind is the error kind. See client/errors.go
	Kind  client.ErrorKind `json:"kind,omitempty"`
	Value errorValue       `json:"value,omitempty"`
}

// errorResponder is a callable that produces an error Response.
// e.g., InternalError("something broke: %v", err), etc.
type errorResponder func(string, ...interface{}) *apiError

// makeErrorResponder builds an errorResponder from the given error status.
func makeErrorResponder(status int) errorResponder {
	return func(format string, v ...interface{}) *apiError {
		var msg string
		if len(v) == 0 {
			msg = format
		} else {
			msg = fmt.Sprintf(format, v...)
		}
		return &apiError{
			Status:  status,
			Message: msg,
		}
	}
}

// standard error responses
var (
	statusUnauthorized     = makeErrorResponder(401)
	statusNotFound         = makeErrorResponder(404)
	statusBadRequest       = makeErrorResponder(400)
	statusMethodNotAllowed = makeErrorResponder(405)
	statusInternalError    = makeErrorResponder(500)
	statusNotImplemented   = makeErrorResponder(501)
	statusForbidden        = makeErrorResponder(403)
)
