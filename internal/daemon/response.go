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
	"net/http"

	"github.com/snapcore/snapd/logger"
)

// responseType is the response type
type responseType string

// "there are three standard return types: Standard return value,
// Background operation, Error", each returning a JSON object with the
// following "type" field:
const (
	responseTypeSync  responseType = "sync"
	responseTypeAsync responseType = "async"
	responseTypeError responseType = "error"
)

// response represents a response.
type response interface {
	Write(w http.ResponseWriter)
}

// respJSON represents our standard JSON response format.
type respJSON struct {
	Type       responseType `json:"type"`
	Status     int          `json:"status-code"`
	StatusText string       `json:"status"`
	Result     interface{}  `json:"result"`
	Change     string       `json:"change,omitempty"`
}

// resp is the standard response implementation
type resp struct {
	Type responseType

	// Status is the HTTP status code.
	Status int

	// Result is a free-form optional result object.
	Result interface{}

	// Change is the change ID for an async response.
	Change string
}

func (r *resp) MarshalJSON() ([]byte, error) {
	return json.Marshal(&respJSON{
		Type:       r.Type,
		Status:     r.Status,
		StatusText: http.StatusText(r.Status),
		Result:     r.Result,
		Change:     r.Change,
	})
}

func (r *resp) Write(w http.ResponseWriter) {
	status := r.Status
	b, err := json.Marshal(r)
	if err != nil {
		logger.Noticef("cannot marshal %#v to JSON: %v", *r, err)
		b = nil
		status = 500
	}

	hdr := w.Header()
	if r.Status == 202 || r.Status == 201 {
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
		Type:   responseTypeSync,
		Status: 200,
		Result: result,
	}
}

// asyncResponse builds an "async" response for a created change
func asyncResponse(result interface{}, change string) response {
	return &resp{
		Type:   responseTypeAsync,
		Status: 202,
		Result: result,
		Change: change,
	}
}
