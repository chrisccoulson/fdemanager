// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"errors"
	"net"
	"path/filepath"
	sys "syscall"

	. "gopkg.in/check.v1"

	. "github.com/snapcore/fdemanager/internal/daemon"
)

type ucrednetSuite struct {
	restoreGetUcred func()
	ucred           *sys.Ucred
	err             error
}

var _ = Suite(&ucrednetSuite{})

func (s *ucrednetSuite) getUcred(fd, level, opt int) (*sys.Ucred, error) {
	return s.ucred, s.err
}

func (s *ucrednetSuite) SetUpSuite(c *C) {
	s.restoreGetUcred = MockGetUcred(s.getUcred)
}

func (s *ucrednetSuite) TearDownTest(c *C) {
	s.ucred = nil
	s.err = nil
}
func (s *ucrednetSuite) TearDownSuite(c *C) {
	s.restoreGetUcred()
}

func (s *ucrednetSuite) TestAcceptConnRemoteAddrString(c *C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, IsNil)
	wl := &UcrednetListener{Listener: l}

	defer wl.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, IsNil)
		cli.Close()
	}()

	conn, err := wl.Accept()
	c.Assert(err, IsNil)
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	c.Check(remoteAddr, Matches, "pid=100;uid=42;.*")
	u, err := UcrednetGet(remoteAddr)
	c.Assert(err, IsNil)
	c.Check(u.Pid, Equals, int32(100))
	c.Check(u.Uid, Equals, uint32(42))
}

func (s *ucrednetSuite) TestNonUnix(c *C) {
	l, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, IsNil)

	wl := &UcrednetListener{Listener: l}
	defer wl.Close()

	addr := l.Addr().String()

	go func() {
		cli, err := net.Dial("tcp", addr)
		c.Assert(err, IsNil)
		cli.Close()
	}()

	conn, err := wl.Accept()
	c.Assert(err, IsNil)
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	c.Check(remoteAddr, Matches, "pid=;uid=;.*")
	u, err := UcrednetGet(remoteAddr)
	c.Check(u, IsNil)
	c.Check(err, Equals, ErrNoID)
}

func (s *ucrednetSuite) TestAcceptErrors(c *C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, IsNil)
	c.Assert(l.Close(), IsNil)

	wl := &UcrednetListener{Listener: l}

	_, err = wl.Accept()
	c.Assert(err, NotNil)
}

func (s *ucrednetSuite) TestUcredErrors(c *C) {
	s.err = errors.New("oopsie")
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, IsNil)

	wl := &UcrednetListener{Listener: l}
	defer wl.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, IsNil)
		cli.Close()
	}()

	_, err = wl.Accept()
	c.Assert(err, Equals, s.err)
}

func (s *ucrednetSuite) TestIdempotentClose(c *C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, IsNil)
	wl := &UcrednetListener{Listener: l}

	c.Assert(wl.Close(), IsNil)
	c.Assert(wl.Close(), IsNil)
}

func (s *ucrednetSuite) TestGetNoUid(c *C) {
	u, err := UcrednetGet("pid=100;uid=;socket=;")
	c.Check(err, Equals, ErrNoID)
	c.Check(u, IsNil)
}

func (s *ucrednetSuite) TestGetBadUid(c *C) {
	u, err := UcrednetGet("pid=100;uid=4294967296;socket=;")
	c.Check(err, Equals, ErrNoID)
	c.Check(u, IsNil)
}

func (s *ucrednetSuite) TestGetNonUcrednet(c *C) {
	u, err := UcrednetGet("hello")
	c.Check(err, Equals, ErrNoID)
	c.Check(u, IsNil)
}

func (s *ucrednetSuite) TestGetNothing(c *C) {
	u, err := UcrednetGet("")
	c.Check(err, Equals, ErrNoID)
	c.Check(u, IsNil)
}

func (s *ucrednetSuite) TestGet(c *C) {
	u, err := UcrednetGet("pid=100;uid=42;socket=/run/snap.socket;")
	c.Assert(err, IsNil)
	c.Check(u.Pid, Equals, int32(100))
	c.Check(u.Uid, Equals, uint32(42))
	c.Check(u.Socket, Equals, "/run/snap.socket")
}

func (s *ucrednetSuite) TestGetSneak(c *C) {
	u, err := UcrednetGet("pid=100;uid=42;socket=/run/snap.socket;pid=0;uid=0;socket=/tmp/my.socket")
	c.Check(err, Equals, ErrNoID)
	c.Check(u, IsNil)
}
