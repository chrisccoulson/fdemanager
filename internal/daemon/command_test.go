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
	"context"
	"net"
	"net/http"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/fdemanager/api"
	. "github.com/snapcore/fdemanager/internal/daemon"
	"github.com/snapcore/fdemanager/internal/netutil"
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

type mockAccessCheckerFunc func(*Daemon, *syscall.Ucred, bool) *ApiError

func (fn mockAccessCheckerFunc) CheckAccess(d *Daemon, ucred *syscall.Ucred, allowInteractive bool) *ApiError {
	return fn(d, ucred, allowInteractive)
}

type testCommandMethodDispatchData struct {
	rsp                      Response
	accessErr                *ApiError
	prepareCmd               func(*Command)
	method                   string
	headers                  http.Header
	peerCred                 *syscall.Ucred
	peerCredErr              error
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
		method = http.MethodGet
		return data.rsp
	}
	cmd.PUT = func(innerDaemon *Daemon, innerReq *http.Request) Response {
		c.Assert(method, Equals, "")
		c.Check(access, Equals, "write")
		c.Check(innerDaemon, Equals, d)
		c.Check(innerReq, Equals, req)
		method = http.MethodPut
		return data.rsp
	}
	cmd.POST = func(innerDaemon *Daemon, innerReq *http.Request) Response {
		c.Assert(method, Equals, "")
		c.Check(access, Equals, "write")
		c.Check(innerDaemon, Equals, d)
		c.Check(innerReq, Equals, req)
		method = http.MethodPost
		return data.rsp
	}
	cmd.ReadAccess = mockAccessCheckerFunc(func(innerDaemon *Daemon, ucred *syscall.Ucred, allowInteraction bool) *ApiError {
		c.Assert(access, Equals, "")
		c.Check(innerDaemon, Equals, d)
		c.Check(ucred, DeepEquals, data.peerCred)
		c.Check(allowInteraction, Equals, data.expectedAllowInteraction)
		access = "read"
		return data.accessErr
	})
	cmd.WriteAccess = mockAccessCheckerFunc(func(innerDaemon *Daemon, ucred *syscall.Ucred, allowInteraction bool) *ApiError {
		c.Assert(access, Equals, "")
		c.Check(innerDaemon, Equals, d)
		c.Check(ucred, DeepEquals, data.peerCred)
		c.Check(allowInteraction, Equals, data.expectedAllowInteraction)
		access = "write"
		return data.accessErr
	})
	if data.prepareCmd != nil {
		data.prepareCmd(cmd)
	}

	conn := new(net.UnixConn)
	ctx := context.WithValue(context.Background(), ConnectionKey, conn)

	var err error
	req, err = http.NewRequestWithContext(ctx, data.method, "", nil)
	c.Assert(err, IsNil)

	req.Header = data.headers

	restore := MockNetutilConnPeerCred(func(innerConn net.Conn) (*syscall.Ucred, error) {
		c.Check(innerConn, Equals, conn)
		if data.peerCredErr != nil {
			return nil, data.peerCredErr
		}
		return data.peerCred, nil
	})
	defer restore()

	rsp := cmd.Run(d, req)
	c.Check(rsp, DeepEquals, data.expectedRsp)
	c.Check(method, Equals, data.expectedMethod)
}

func (s *commandSuite) TestCommandMethodDispatchGet(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		rsp:            &mockResponse{200},
		method:         http.MethodGet,
		peerCred:       &syscall.Ucred{Pid: 100, Uid: 1001, Gid: 1001},
		expectedMethod: http.MethodGet,
		expectedRsp:    &mockResponse{200},
	})
}

func (s *commandSuite) TestCommandMethodDispatchGetDenied(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		accessErr:   StatusUnathorized("some error"),
		method:      http.MethodGet,
		peerCred:    &syscall.Ucred{Pid: 100, Uid: 1001, Gid: 1001},
		expectedRsp: StatusUnathorized("some error"),
	})
}

func (s *commandSuite) TestCommandMethodDispatchPost(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		rsp:            &mockResponse{200},
		method:         http.MethodPost,
		peerCred:       &syscall.Ucred{Pid: 100, Uid: 1001, Gid: 1001},
		expectedMethod: http.MethodPost,
		expectedRsp:    &mockResponse{200},
	})
}

func (s *commandSuite) TestCommandMethodDispatchPostDenied(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		accessErr:   StatusUnathorized("some error"),
		method:      http.MethodPost,
		peerCred:    &syscall.Ucred{Pid: 100, Uid: 1001, Gid: 1001},
		expectedRsp: StatusUnathorized("some error"),
	})
}

func (s *commandSuite) TestCommandMethodDispatchPut(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		rsp:            &mockResponse{200},
		method:         http.MethodPut,
		peerCred:       &syscall.Ucred{Pid: 100, Uid: 1001, Gid: 1001},
		expectedMethod: http.MethodPut,
		expectedRsp:    &mockResponse{200},
	})
}

func (s *commandSuite) TestCommandMethodDispatchPutDenied(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		accessErr:   StatusUnathorized("some error"),
		method:      http.MethodPut,
		peerCred:    &syscall.Ucred{Pid: 100, Uid: 1001, Gid: 1001},
		expectedRsp: StatusUnathorized("some error"),
	})
}

func (s *commandSuite) TestCommandMethodDispatchDifferentCred(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		rsp:            &mockResponse{200},
		method:         http.MethodGet,
		peerCred:       &syscall.Ucred{Pid: 5, Uid: 1, Gid: 1},
		expectedMethod: http.MethodGet,
		expectedRsp:    &mockResponse{200},
	})
}

func (s *commandSuite) TestCommandMethodDispatchMethodNotAllowed(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		method:      http.MethodPost,
		prepareCmd:  func(cmd *Command) { cmd.POST = nil },
		expectedRsp: StatusMethodNotAllowed("method \"POST\" not allowed"),
	})
}

func (s *commandSuite) TestCommandMethodDispatchMissingAccessCheck(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		method:      http.MethodGet,
		prepareCmd:  func(cmd *Command) { cmd.ReadAccess = nil },
		expectedRsp: StatusInternalError("no access checker for method \"GET\""),
	})
}

func (s *commandSuite) TestCommandMethodDispatchAllowInteractionAuth(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		rsp:                      &mockResponse{200},
		method:                   http.MethodGet,
		headers:                  http.Header{api.AllowInteractionHeader: []string{"1"}},
		peerCred:                 &syscall.Ucred{Pid: 100, Uid: 1001, Gid: 1001},
		expectedAllowInteraction: true,
		expectedMethod:           http.MethodGet,
		expectedRsp:              &mockResponse{200},
	})
}

func (s *commandSuite) TestCommandMethodDispatchPeerCredErr(c *C) {
	s.testCommandMethodDispatch(c, &testCommandMethodDispatchData{
		method:      http.MethodGet,
		peerCredErr: netutil.ErrNoPeerCred,
		expectedRsp: &ApiError{
			Status:  500,
			Message: "connection has no peer credential",
		},
	})
}

func (s *commandSuite) TestCommandMethodDispatchNoLinkedConnection(c *C) {
	d := new(Daemon)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "", nil)
	c.Assert(err, IsNil)

	cmd := new(Command)
	c.Check(func() { cmd.Run(d, req) }, PanicMatches, `no connection associated with request`)
}
