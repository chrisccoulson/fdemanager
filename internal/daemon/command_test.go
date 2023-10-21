// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2023 Canonical Ltd
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

package daemon_test

import (
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/snapcore/fdemanager/client"
	. "github.com/snapcore/fdemanager/internal/daemon"
)

type commandSuite struct{}

var _ = Suite(&commandSuite{})

type mockResponse struct {
	status int
}

func (r *mockResponse) Write(w http.ResponseWriter) {
	hdr := w.Header()
	hdr.Set("Content-Type", "application/json")
	w.WriteHeader(r.status)
	w.Write([]byte("{}"))
}

type mockAccessCheckerFunc func(*Daemon, *Ucrednet, bool) *ApiError

func (fn mockAccessCheckerFunc) CheckAccess(d *Daemon, ucred *Ucrednet, allowInteractive bool) *ApiError {
	return fn(d, ucred, allowInteractive)
}

type testCommandMethodDispatchData struct {
	rsp                      Response
	accessErr                *ApiError
	prepareCmd               func(*Command)
	method                   string
	remoteAddr               string
	headers                  http.Header
	expectedUcred            *Ucrednet
	expectedAllowInteraction bool
	expectedMethod           string
	expectedRsp              Response
	expectedStatus           int
}

func (s *commandSuite) testCommandMethodDispatch(c *C, data *testCommandMethodDispatchData) {
	d := new(Daemon)

	var req *http.Request
	var access string
	var method string

	cmd := new(Command)
	cmd.GET = func(innerDaemon *Daemon, innerReq *http.Request) Response {
		c.Assert(method, Equals, "")
		c.Check(access, Equals, "read")
		c.Check(innerDaemon, Equals, d)
		c.Check(innerReq, Equals, req)
		method = "GET"
		return data.rsp
	}
	cmd.PUT = func(innerDaemon *Daemon, innerReq *http.Request) Response {
		c.Assert(method, Equals, "")
		c.Check(access, Equals, "write")
		c.Check(innerDaemon, Equals, d)
		c.Check(innerReq, Equals, req)
		method = "PUT"
		return data.rsp
	}
	cmd.POST = func(innerDaemon *Daemon, innerReq *http.Request) Response {
		c.Assert(method, Equals, "")
		c.Check(access, Equals, "write")
		c.Check(innerDaemon, Equals, d)
		c.Check(innerReq, Equals, req)
		method = "POST"
		return data.rsp
	}
	cmd.ReadAccess = mockAccessCheckerFunc(func(innerDaemon *Daemon, ucred *Ucrednet, allowInteraction bool) *ApiError {
		c.Assert(access, Equals, "")
		c.Check(innerDaemon, Equals, d)
		c.Check(ucred, DeepEquals, data.expectedUcred)
		c.Check(allowInteraction, Equals, data.expectedAllowInteraction)
		access = "read"
		return data.accessErr
	})
	cmd.WriteAccess = mockAccessCheckerFunc(func(innerDaemon *Daemon, ucred *Ucrednet, allowInteraction bool) *ApiError {
		c.Assert(access, Equals, "")
		c.Check(innerDaemon, Equals, d)
		c.Check(ucred, DeepEquals, data.expectedUcred)
		c.Check(allowInteraction, Equals, data.expectedAllowInteraction)
		access = "write"
		return data.accessErr
	})
	if data.prepareCmd != nil {
		data.prepareCmd(cmd)
	}

	var err error
	req, err = http.NewRequest(data.method, "", nil)
	c.Assert(err, IsNil)

	req.RemoteAddr = data.remoteAddr
	req.Header = data.headers

	rsp := cmd.Run(d, req)
	c.Check(rsp, DeepEquals, data.expectedRsp)
	c.Check(method, Equals, data.expectedMethod)
}

func (s *commandSuite) TestCommandMethodDispatchGet(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		rsp:            &mockResponse{200},
		method:         "GET",
		remoteAddr:     "pid=100;uid=1001;socket=/run/foo.socket;",
		expectedUcred:  &Ucrednet{Pid: 100, Uid: 1001, Socket: "/run/foo.socket"},
		expectedMethod: "GET",
		expectedRsp:    &mockResponse{200},
	})
}

func (s *commandSuite) TestCommandMethodDispatchGetDenied(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		accessErr:     StatusUnathorized("some error"),
		method:        "GET",
		remoteAddr:    "pid=100;uid=1001;socket=/run/foo.socket;",
		expectedUcred: &Ucrednet{Pid: 100, Uid: 1001, Socket: "/run/foo.socket"},
		expectedRsp:   StatusUnathorized("some error"),
	})
}

func (s *commandSuite) TestCommandMethodDispatchPost(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		rsp:            &mockResponse{200},
		method:         "POST",
		remoteAddr:     "pid=100;uid=1001;socket=/run/foo.socket;",
		expectedUcred:  &Ucrednet{Pid: 100, Uid: 1001, Socket: "/run/foo.socket"},
		expectedMethod: "POST",
		expectedRsp:    &mockResponse{200},
	})
}

func (s *commandSuite) TestCommandMethodDispatchPostDenied(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		accessErr:     StatusUnathorized("some error"),
		method:        "POST",
		remoteAddr:    "pid=100;uid=1001;socket=/run/foo.socket;",
		expectedUcred: &Ucrednet{Pid: 100, Uid: 1001, Socket: "/run/foo.socket"},
		expectedRsp:   StatusUnathorized("some error"),
	})
}

func (s *commandSuite) TestCommandMethodDispatchPut(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		rsp:            &mockResponse{200},
		method:         "PUT",
		remoteAddr:     "pid=100;uid=1001;socket=/run/foo.socket;",
		expectedUcred:  &Ucrednet{Pid: 100, Uid: 1001, Socket: "/run/foo.socket"},
		expectedMethod: "PUT",
		expectedRsp:    &mockResponse{200},
	})
}

func (s *commandSuite) TestCommandMethodDispatchPutDenied(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		accessErr:     StatusUnathorized("some error"),
		method:        "PUT",
		remoteAddr:    "pid=100;uid=1001;socket=/run/foo.socket;",
		expectedUcred: &Ucrednet{Pid: 100, Uid: 1001, Socket: "/run/foo.socket"},
		expectedRsp:   StatusUnathorized("some error"),
	})
}

func (s *commandSuite) TestCommandMethodDispatchDifferentCred(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		rsp:            &mockResponse{200},
		method:         "GET",
		remoteAddr:     "pid=5;uid=1;socket=/run/foo.socket;",
		expectedUcred:  &Ucrednet{Pid: 5, Uid: 1, Socket: "/run/foo.socket"},
		expectedMethod: "GET",
		expectedRsp:    &mockResponse{200},
	})
}

func (s *commandSuite) TestCommandMethodDispatchMethodNotAllowed(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		method:      "POST",
		prepareCmd:  func(cmd *Command) { cmd.POST = nil },
		expectedRsp: StatusMethodNotAllowed("method \"POST\" not allowed"),
	})
}

func (s *commandSuite) TestCommandMethodDispatchMissingAccessCheck(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		method:      "GET",
		prepareCmd:  func(cmd *Command) { cmd.ReadAccess = nil },
		expectedRsp: StatusInternalError("no access checker for method \"GET\""),
	})
}

func (s *commandSuite) TestCommandMethodDispatchAllowInteractionAuth(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		rsp:                      &mockResponse{200},
		method:                   "GET",
		remoteAddr:               "pid=100;uid=1001;socket=/run/foo.socket;",
		headers:                  http.Header{client.AllowInteractionHeader: []string{"1"}},
		expectedUcred:            &Ucrednet{Pid: 100, Uid: 1001, Socket: "/run/foo.socket"},
		expectedAllowInteraction: true,
		expectedMethod:           "GET",
		expectedRsp:              &mockResponse{200},
	})
}
