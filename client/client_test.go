// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/snapcore/fdemanager/api"
	. "github.com/snapcore/fdemanager/client"
	"github.com/snapcore/fdemanager/internal/paths"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type clientSuite struct {
	testutil.BaseTest
}

func (s *clientSuite) SetUpTest(c *C) {
	dir := c.MkDir()
	s.AddCleanup(paths.MockRootDir(dir))
	c.Check(os.MkdirAll(filepath.Dir(paths.ManagerSocket), 0755), IsNil)
}

func (s *clientSuite) mockHttpServer(c *C, handler http.Handler) *httptest.Server {
	l, err := net.Listen("unix", paths.ManagerSocket)
	c.Assert(err, IsNil)

	srv := &httptest.Server{
		Listener: l,
		Config:   &http.Server{Handler: handler},
	}
	srv.Start()
	return srv
}

var _ = Suite(&clientSuite{})

func (s *clientSuite) TestDo(c *C) {
	srv := s.mockHttpServer(c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, http.MethodGet)
		c.Check(r.URL, DeepEquals, &url.URL{Path: "/v1/foo"})
		c.Check(r.Header.Get("Content-Type"), Equals, "application/json")
		c.Check(r.Header.Get(api.AllowInteractionHeader), Equals, "")
		c.Check(r.ContentLength, Equals, int64(5))
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte(`null
`))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"type":"sync","status-code":200,"status":"OK","result":null}`))
	}))
	defer srv.Close()

	client := New(nil)
	c.Assert(client, NotNil)

	rsp, err := client.Do(context.Background(), http.MethodGet, "/v1/foo", nil, nil)
	c.Assert(err, IsNil)

	c.Check(rsp.Type, Equals, api.ResponseTypeSync)
	c.Check(rsp.StatusCode, Equals, http.StatusOK)
	c.Check(rsp.Result, DeepEquals, json.RawMessage(`null`))
	c.Check(rsp.Change, Equals, "")
}

func (s *clientSuite) TestDoDifferentArguments(c *C) {
	srv := s.mockHttpServer(c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, http.MethodPost)
		c.Check(r.URL, DeepEquals, &url.URL{Path: "/v1/bar"})
		c.Check(r.Header.Get("Content-Type"), Equals, "application/json")
		c.Check(r.Header.Get(api.AllowInteractionHeader), Equals, "")
		c.Check(r.ContentLength, Equals, int64(22))
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte(`{"foo":1,"bar":"abc"}
`))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"type":"sync","status-code":200,"status":"OK","result":null}`))
	}))
	defer srv.Close()

	client := New(nil)
	c.Assert(client, NotNil)

	rsp, err := client.Do(context.Background(), http.MethodPost, "/v1/bar", nil, json.RawMessage(`{"foo":1,"bar":"abc"}`))
	c.Assert(err, IsNil)

	c.Check(rsp.Type, Equals, api.ResponseTypeSync)
	c.Check(rsp.StatusCode, Equals, http.StatusOK)
	c.Check(rsp.Result, DeepEquals, json.RawMessage(`null`))
	c.Check(rsp.Change, Equals, "")
}

func (s *clientSuite) TestDoDifferentResponse(c *C) {
	srv := s.mockHttpServer(c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, http.MethodGet)
		c.Check(r.URL, DeepEquals, &url.URL{Path: "/v1/foo"})
		c.Check(r.Header.Get("Content-Type"), Equals, "application/json")
		c.Check(r.Header.Get(api.AllowInteractionHeader), Equals, "")
		c.Check(r.ContentLength, Equals, int64(5))
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte(`null
`))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"type":"error","status-code":401,"status":"Unauthorized","result":{"message":"cannot access resource"}}`))
	}))
	defer srv.Close()

	client := New(nil)
	c.Assert(client, NotNil)

	rsp, err := client.Do(context.Background(), http.MethodGet, "/v1/foo", nil, nil)
	c.Assert(err, IsNil)

	c.Check(rsp.Type, Equals, api.ResponseTypeError)
	c.Check(rsp.StatusCode, Equals, http.StatusUnauthorized)
	c.Check(rsp.Result, DeepEquals, json.RawMessage(`{"message":"cannot access resource"}`))
	c.Check(rsp.Change, Equals, "")
}

func (s *clientSuite) TestDoInteractive(c *C) {
	srv := s.mockHttpServer(c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, http.MethodGet)
		c.Check(r.URL, DeepEquals, &url.URL{Path: "/v1/foo"})
		c.Check(r.Header.Get("Content-Type"), Equals, "application/json")
		c.Check(r.Header.Get(api.AllowInteractionHeader), Equals, "1")
		c.Check(r.ContentLength, Equals, int64(5))
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte(`null
`))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"type":"sync","status-code":200,"status":"OK","result":null}`))
	}))
	defer srv.Close()

	client := New(&Config{Interactive: true})
	c.Assert(client, NotNil)

	rsp, err := client.Do(context.Background(), http.MethodGet, "/v1/foo", nil, nil)
	c.Assert(err, IsNil)

	c.Check(rsp.Type, Equals, api.ResponseTypeSync)
	c.Check(rsp.StatusCode, Equals, http.StatusOK)
	c.Check(rsp.Result, DeepEquals, json.RawMessage(`null`))
	c.Check(rsp.Change, Equals, "")
}

func (s *clientSuite) TestDoSync(c *C) {
	srv := s.mockHttpServer(c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, http.MethodGet)
		c.Check(r.URL, DeepEquals, &url.URL{Path: "/v1/foo"})
		c.Check(r.Header.Get("Content-Type"), Equals, "application/json")
		c.Check(r.Header.Get(api.AllowInteractionHeader), Equals, "")
		c.Check(r.ContentLength, Equals, int64(5))
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte(`null
`))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"type":"sync","status-code":200,"status":"OK","result":null}`))
	}))
	defer srv.Close()

	client := New(nil)
	c.Assert(client, NotNil)

	var result json.RawMessage
	c.Check(client.DoSync(context.Background(), http.MethodGet, "/v1/foo", nil, nil, &result), IsNil)
	c.Check(result, DeepEquals, json.RawMessage(nil))
}

func (s *clientSuite) TestDoSyncDifferentArguments(c *C) {
	srv := s.mockHttpServer(c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, http.MethodPost)
		c.Check(r.URL, DeepEquals, &url.URL{Path: "/v1/bar"})
		c.Check(r.Header.Get("Content-Type"), Equals, "application/json")
		c.Check(r.Header.Get(api.AllowInteractionHeader), Equals, "")
		c.Check(r.ContentLength, Equals, int64(22))
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte(`{"foo":1,"bar":"abc"}
`))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"type":"sync","status-code":200,"status":"OK","result":null}`))
	}))
	defer srv.Close()

	client := New(nil)
	c.Assert(client, NotNil)

	var result json.RawMessage
	c.Check(client.DoSync(context.Background(), http.MethodPost, "/v1/bar", nil, json.RawMessage(`{"foo":1,"bar":"abc"}`), &result), IsNil)
	c.Check(result, DeepEquals, json.RawMessage(nil))
}

func (s *clientSuite) TestDoSyncDifferentResponse(c *C) {
	srv := s.mockHttpServer(c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, http.MethodGet)
		c.Check(r.URL, DeepEquals, &url.URL{Path: "/v1/foo"})
		c.Check(r.Header.Get("Content-Type"), Equals, "application/json")
		c.Check(r.Header.Get(api.AllowInteractionHeader), Equals, "")
		c.Check(r.ContentLength, Equals, int64(5))
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte(`null
`))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"type":"sync","status-code":200,"status":"OK","result":{"foo":2,"bar":"1234"}}`))
	}))
	defer srv.Close()

	client := New(nil)
	c.Assert(client, NotNil)

	var result json.RawMessage
	c.Check(client.DoSync(context.Background(), http.MethodGet, "/v1/foo", nil, nil, &result), IsNil)
	c.Check(result, DeepEquals, json.RawMessage(`{"foo":2,"bar":"1234"}`))
}

func (s *clientSuite) TestDoSyncNilResult(c *C) {
	srv := s.mockHttpServer(c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, http.MethodGet)
		c.Check(r.URL, DeepEquals, &url.URL{Path: "/v1/foo"})
		c.Check(r.Header.Get("Content-Type"), Equals, "application/json")
		c.Check(r.Header.Get(api.AllowInteractionHeader), Equals, "")
		c.Check(r.ContentLength, Equals, int64(5))
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte(`null
`))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"type":"sync","status-code":200,"status":"OK","result":{"foo":2,"bar":"1234"}}`))
	}))
	defer srv.Close()

	client := New(nil)
	c.Assert(client, NotNil)

	c.Check(client.DoSync(context.Background(), http.MethodGet, "/v1/foo", nil, nil, nil), IsNil)
}

func (s *clientSuite) TestDoSyncErrorResult(c *C) {
	srv := s.mockHttpServer(c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, http.MethodGet)
		c.Check(r.URL, DeepEquals, &url.URL{Path: "/v1/foo"})
		c.Check(r.Header.Get("Content-Type"), Equals, "application/json")
		c.Check(r.Header.Get(api.AllowInteractionHeader), Equals, "")
		c.Check(r.ContentLength, Equals, int64(5))
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte(`null
`))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"type":"error","status-code":401,"status":"Unauthorized","result":{"message":"cannot access resource"}}`))
	}))
	defer srv.Close()

	client := New(nil)
	c.Assert(client, NotNil)

	var result json.RawMessage
	c.Check(client.DoSync(context.Background(), http.MethodGet, "/v1/foo", nil, nil, &result), DeepEquals, &Error{
		StatusCode: http.StatusUnauthorized,
		ErrorResult: api.ErrorResult{
			Message: "cannot access resource",
		},
	})
	c.Check(result, DeepEquals, json.RawMessage(nil))
}
