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

package netutil_test

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	. "github.com/snapcore/fdemanager/internal/netutil"
)

type peercredSuite struct{}

var _ = Suite(&peercredSuite{})

func (s *peercredSuite) TestUnixSocket(c *C) {
	dir := c.MkDir()
	socket := filepath.Join(dir, "socket")

	l, err := net.Listen("unix", socket)
	c.Assert(err, IsNil)
	defer l.Close()

	go func() {
		conn, err := net.Dial("unix", socket)
		c.Assert(err, IsNil)
		conn.Close()
	}()

	conn, err := l.Accept()
	c.Assert(err, IsNil)
	defer conn.Close()

	pid := os.Getpid()
	uid := os.Getuid()
	gid := os.Getgid()

	cred, err := ConnPeerCred(conn)
	c.Assert(err, IsNil)
	c.Check(cred, DeepEquals, &syscall.Ucred{
		Pid: int32(pid),
		Uid: uint32(uid),
		Gid: uint32(gid),
	})
}

func (s *peercredSuite) TestNonUnixSocket(c *C) {
	l, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, IsNil)
	defer l.Close()

	addr := l.Addr().String()

	go func() {
		conn, err := net.Dial("tcp", addr)
		c.Assert(err, IsNil)
		conn.Close()
	}()

	conn, err := l.Accept()
	c.Assert(err, IsNil)
	defer conn.Close()

	_, err = ConnPeerCred(conn)
	c.Check(err, Equals, ErrNoPeerCred)
}

type nonSyscallConn struct{}

func (*nonSyscallConn) Read(b []byte) (int, error)         { return 0, nil }
func (*nonSyscallConn) Write(b []byte) (int, error)        { return 0, nil }
func (*nonSyscallConn) Close() error                       { return nil }
func (*nonSyscallConn) LocalAddr() net.Addr                { return nil }
func (*nonSyscallConn) RemoteAddr() net.Addr               { return nil }
func (*nonSyscallConn) SetDeadline(t time.Time) error      { return nil }
func (*nonSyscallConn) SetReadDeadline(t time.Time) error  { return nil }
func (*nonSyscallConn) SetWriteDeadline(t time.Time) error { return nil }

func (s *peercredSuite) TestNonSyscallConn(c *C) {
	_, err := ConnPeerCred(new(nonSyscallConn))
	c.Check(err, Equals, ErrNoPeerCred)
}
