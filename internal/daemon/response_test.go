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

package daemon_test

import (
	"net/http/httptest"

	. "gopkg.in/check.v1"

	. "github.com/snapcore/fdemanager/internal/daemon"
)

type responseSuite struct{}

var _ = Suite(&responseSuite{})

type mockResult struct {
	A string `json:"a"`
	B int    `json:"b"`
}

func (s *responseSuite) TestSyncResponse(c *C) {
	rec := httptest.NewRecorder()
	rsp := SyncResponse(mockResult{A: "foo", B: 10})
	rsp.Write(rec)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")
	c.Check(rec.Body.Bytes(), DeepEquals, []byte("{\"type\":\"sync\",\"status-code\":200,\"status\":\"OK\",\"result\":{\"a\":\"foo\",\"b\":10}}"))
}

func (s *responseSuite) TestAsyncResponse(c *C) {
	rec := httptest.NewRecorder()
	rsp := AsyncResponse(mockResult{A: "bar", B: 5}, "15")
	rsp.Write(rec)
	c.Check(rec.Code, Equals, 202)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")
	c.Check(rec.Body.Bytes(), DeepEquals, []byte("{\"type\":\"async\",\"status-code\":202,\"status\":\"Accepted\",\"result\":{\"a\":\"bar\",\"b\":5},\"change\":\"15\"}"))
}
