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

// Package client provides an API for communicating with the fdemanager service.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/snapcore/fdemanager/api"
	"github.com/snapcore/fdemanager/internal/paths"
)

// Error is returned from a [Client] method when the service responds with an
// error.
type Error struct {
	StatusCode int
	api.ErrorResult
}

func (e *Error) Error() string {
	return e.Message
}

// CommunicationError is returned from a [Client] method when communication
// with the service fails or the response from the service is not valid HTTP.
type CommunicationError struct {
	err error
}

func (e *CommunicationError) Error() string {
	return fmt.Sprintf("cannot communicate with service: %v", e.err)
}

func (e *CommunicationError) Unwrap() error {
	return e.err
}

// InvalidResponseError is returned from a [Client] method when the service
// returns an invalid response.
type InvalidResponseError struct {
	err error
}

func (e *InvalidResponseError) Error() string {
	return fmt.Sprintf("invalid response from service: %v", e.err)
}

func (e *InvalidResponseError) Unwrap() error {
	return e.err
}

// Config provides options for [Client].
type Config struct {
	Interactive      bool
	DisableKeepAlive bool
}

type doer interface {
	Do(r *http.Request) (*http.Response, error)
}

// Client provides an API for communicating with the fdemanager service.
type Client struct {
	doer        doer
	interactive bool
}

// New returns a new Client.
func New(config *Config) *Client {
	if config == nil {
		config = new(Config)
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := new(net.Dialer)
			return d.DialContext(ctx, "unix", paths.ManagerSocket)
		},
		DisableKeepAlives: config.DisableKeepAlive,
	}

	return &Client{
		doer:        &http.Client{Transport: transport},
		interactive: config.Interactive,
	}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, args any) (*api.Response, error) {
	u := url.URL{
		Scheme:   "http",
		Host:     "localhost",
		Path:     path,
		RawQuery: query.Encode(),
	}

	body := new(bytes.Buffer)
	enc := json.NewEncoder(body)
	if err := enc.Encode(args); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("cannot create request: %w", err)
	}

	req.Header.Add("Content-Type", "application/json")
	if c.interactive {
		req.Header.Add(api.AllowInteractionHeader, "1")
	}

	httpRsp, err := c.doer.Do(req)
	if err != nil {
		return nil, &CommunicationError{err: err}
	}
	defer httpRsp.Body.Close()

	if httpRsp.Header.Get("Content-Type") != "application/json" {
		return nil, &InvalidResponseError{fmt.Errorf("invalid Content-Type: %q", httpRsp.Header.Get("Content-Type"))}
	}

	var rsp *api.Response
	dec := json.NewDecoder(httpRsp.Body)
	if err := dec.Decode(&rsp); err != nil {
		return nil, &InvalidResponseError{fmt.Errorf("cannot decode response body: %w", err)}
	}
	rsp.StatusCode = httpRsp.StatusCode

	return rsp, nil
}

func (c *Client) doSync(ctx context.Context, method, path string, query url.Values, args, result any) error {
	rsp, err := c.do(ctx, method, path, query, args)
	if err != nil {
		return err
	}

	if rsp.Type == api.ResponseTypeError {
		var errResult *api.ErrorResult
		if err := json.Unmarshal(rsp.Result, &errResult); err != nil {
			return &InvalidResponseError{fmt.Errorf("cannot decode error result: %w", err)}
		}

		return &Error{
			StatusCode:  rsp.StatusCode,
			ErrorResult: *errResult,
		}
	}

	if rsp.Type != "sync" {
		return &InvalidResponseError{errors.New("invalid response type")}
	}

	if result != nil {
		if err := json.Unmarshal(rsp.Result, &result); err != nil {
			return &InvalidResponseError{err}
		}
	}

	return nil
}
