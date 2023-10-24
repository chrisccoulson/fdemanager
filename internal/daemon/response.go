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
	"github.com/snapcore/snapd/logger"
)

// response represents a response.
type response interface {
	Write(w http.ResponseWriter)
}

// resp is the standard response implementation
type resp struct {
	Type api.ResponseType

	// Status is the HTTP status code.
	Status int

	// Result is a free-form optional result object.
	Result interface{}

	// Change is the change ID for an async response.
	Change string
}

func (r *resp) MarshalJSON() ([]byte, error) {
	result, err := json.Marshal(r.Result)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal result: %w", err)
	}
	return json.Marshal(&api.Response{
		Type:       r.Type,
		StatusCode: r.Status,
		StatusText: http.StatusText(r.Status),
		Result:     result,
		Change:     r.Change,
	})
}

func (r *resp) Write(w http.ResponseWriter) {
	status := r.Status
	b, err := json.Marshal(r)
	if err != nil {
		logger.Noticef("cannot marshal %#v to JSON: %v", *r, err)
		b = nil
		status = http.StatusInternalServerError
	}

	hdr := w.Header()
	if r.Status == http.StatusAccepted || r.Status == http.StatusCreated {
		if m, ok := r.Result.(map[string]interface{}); ok {
			if location, ok := m["resource"]; ok {
				if location, ok := location.(string); ok && location != "" {
					hdr.Set("Location", location)
				}
			}
		}
	}

	hdr.Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(b)
}

var _ response = (*resp)(nil)

// syncResponse builds a "sync" response from the given result.
func syncResponse(result interface{}) response {
	return &resp{
		Type:   api.ResponseTypeSync,
		Status: http.StatusOK,
		Result: result,
	}
}

// asyncResponse builds an "async" response for a created change
func asyncResponse(result interface{}, change string) response {
	return &resp{
		Type:   api.ResponseTypeAsync,
		Status: http.StatusAccepted,
		Result: result,
		Change: change,
	}
}
