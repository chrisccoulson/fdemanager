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
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/snapcore/fdemanager/api"
)

// apiError represents an error meant for returning to the client.
// It can serialize itself to our standard JSON response format.
type apiError struct {
	// Status is the error HTTP status code.
	Status  int
	Message string
	// Kind is the error kind. See client/errors.go
	Kind  api.ErrorKind
	Value any
}

func (ae *apiError) Error() string {
	kindOrStatus := "api"
	if ae.Kind != "" {
		kindOrStatus = fmt.Sprintf("api: %s", ae.Kind)
	} else if ae.Status != http.StatusBadRequest {
		kindOrStatus = fmt.Sprintf("api %d", ae.Status)
	}
	return fmt.Sprintf("%s (%s)", ae.Message, kindOrStatus)
}

func (ae *apiError) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	ae.Write(w)
}

func (ae *apiError) Write(w http.ResponseWriter) {
	value, err := json.Marshal(ae.Value)
	if err != nil {
		value, _ = json.Marshal(fmt.Sprintf("cannot encode value: %v", err))
	}
	rsp := &resp{
		Type:   api.ResponseTypeError,
		Status: ae.Status,
		Result: &api.ErrorResult{
			Message: ae.Message,
			Kind:    ae.Kind,
			Value:   value,
		},
	}
	rsp.Write(w)
}

// check it implements response
var _ response = (*apiError)(nil)
var _ error = (*apiError)(nil)

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
	statusUnauthorized     = makeErrorResponder(http.StatusUnauthorized)
	statusNotFound         = makeErrorResponder(http.StatusNotFound)
	statusBadRequest       = makeErrorResponder(http.StatusBadRequest)
	statusMethodNotAllowed = makeErrorResponder(http.StatusMethodNotAllowed)
	statusInternalError    = makeErrorResponder(http.StatusInternalServerError)
	statusNotImplemented   = makeErrorResponder(http.StatusNotImplemented)
	statusForbidden        = makeErrorResponder(http.StatusForbidden)
)
