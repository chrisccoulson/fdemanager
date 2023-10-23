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
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	. "github.com/snapcore/fdemanager/internal/daemon"
	"github.com/snapcore/fdemanager/internal/paths"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"
)

func Test(t *testing.T) { TestingT(t) }

type daemonSuite struct {
	testutil.BaseTest
}

func (s *daemonSuite) SetUpTest(c *C) {
	dir := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(dir, "run"), 0755), IsNil)
	s.AddCleanup(paths.MockRootDir(dir))
}

var _ = Suite(&daemonSuite{})

func (s *daemonSuite) TestConnTrackerCanStandby(c *C) {
	t := NewConnTracker()
	c.Check(t.CanStandby(), Equals, true)

	conn1 := new(net.UnixConn)
	t.TrackConn(conn1, http.StateNew)
	c.Check(t.CanStandby(), Equals, false)

	conn2 := new(net.UnixConn)
	t.TrackConn(conn2, http.StateNew)
	c.Check(t.CanStandby(), Equals, false)

	t.TrackConn(conn1, http.StateIdle)
	c.Check(t.CanStandby(), Equals, false)

	t.TrackConn(conn2, http.StateIdle)
	c.Check(t.CanStandby(), Equals, true)
}

func (s *daemonSuite) TestNew(c *C) {
	d, err := New()
	c.Check(err, IsNil)
	c.Check(d, NotNil)
}

func (s *daemonSuite) TestTrivialStartStop(c *C) {
	d, err := New()
	c.Check(err, IsNil)

	c.Check(d.Start(), IsNil)

	ctx, _ := context.WithTimeout(context.Background(), 2*time.Second)
	tmb, _ := tomb.WithContext(ctx)
	tmb.Go(func() error {
		select {
		case <-d.Dying():
		case <-tmb.Dying():
		}
		return nil
	})

	c.Check(d.CanStandby(), Equals, false)

	c.Check(d.Stop(), IsNil)
	c.Check(tmb.Wait(), IsNil)
}

func (s *daemonSuite) TestStartAndStopActivated(c *C) {
	addr, err := net.ResolveUnixAddr("unix", paths.ManagerSocket)
	c.Assert(err, IsNil)

	l, err := net.ListenUnix("unix", addr)
	c.Assert(err, IsNil)
	defer l.Close()

	f, err := l.File()
	c.Assert(err, IsNil)

	c.Check(syscall.Dup2(int(f.Fd()), 3), IsNil)
	defer syscall.Close(3)

	c.Check(os.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid())), IsNil)
	c.Check(os.Setenv("LISTEN_FDS", "1"), IsNil)

	d, err := New()
	c.Check(err, IsNil)

	c.Check(d.Start(), IsNil)

	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	tmb, _ := tomb.WithContext(ctx)
	tmb.Go(func() error {
		select {
		case <-d.Dying():
		case <-tmb.Dying():
		}
		return nil
	})

	c.Check(d.CanStandby(), Equals, true)

	c.Check(tmb.Wait(), IsNil)
	c.Check(d.Stop(), Equals, ErrRestartSocket)
}

func (s *daemonSuite) TestNotFoundHandler(c *C) {
	d, err := New()
	c.Check(err, IsNil)

	c.Check(d.Start(), IsNil)

	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(_, _ string) (net.Conn, error) {
				return net.Dial("unix", paths.ManagerSocket)
			},
		},
	}

	rsp, err := client.Get("http://localhost/v1/foo")
	c.Assert(err, IsNil)
	c.Check(rsp.StatusCode, Equals, 404)

	c.Check(d.Stop(), IsNil)
}

func (s *daemonSuite) TestBasicCommandRouting(c *C) {
	restore := MockApi([]*Command{
		{
			Path: "/v1/foo",
			GET: func(innerDaemon *Daemon, r *http.Request) Response {
				return SyncResponse(nil)
			},
			ReadAccess: OpenAccess,
		},
	})
	defer restore()

	d, err := New()
	c.Check(err, IsNil)

	c.Check(d.Start(), IsNil)

	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(_, _ string) (net.Conn, error) {
				return net.Dial("unix", paths.ManagerSocket)
			},
		},
	}

	rsp, err := client.Get("http://localhost/v1/foo")
	c.Assert(err, IsNil)
	c.Check(rsp.StatusCode, Equals, 200)
	b, err := io.ReadAll(rsp.Body)
	c.Check(err, IsNil)
	c.Check(b, DeepEquals, []byte("{\"type\":\"sync\",\"status-code\":200,\"status\":\"OK\",\"result\":null}"))

	c.Check(d.Stop(), IsNil)
}

func (s *daemonSuite) TestConnectionRequestBinding(c *C) {
	var conns []net.Conn

	var wg sync.WaitGroup
	wg.Add(2)

	complete := make(chan struct{})
	restore := MockApi([]*Command{
		{
			Path: "/v1/foo",
			GET: func(innerDaemon *Daemon, r *http.Request) Response {
				conns = append(conns, r.Context().Value(ConnectionKey).(net.Conn))
				wg.Done()
				<-complete
				return SyncResponse(nil)
			},
			ReadAccess: OpenAccess,
		},
	})
	defer restore()

	d, err := New()
	c.Check(err, IsNil)

	c.Check(d.Start(), IsNil)

	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(_, _ string) (net.Conn, error) {
				return net.Dial("unix", paths.ManagerSocket)
			},
		},
	}

	var tmb tomb.Tomb
	tmb.Go(func() error {
		communicate := func() error {
			rsp, err := client.Get("http://localhost/v1/foo")
			c.Assert(err, IsNil)
			c.Check(rsp.StatusCode, Equals, 200)
			b, err := io.ReadAll(rsp.Body)
			c.Check(err, IsNil)
			c.Check(b, DeepEquals, []byte("{\"type\":\"sync\",\"status-code\":200,\"status\":\"OK\",\"result\":null}"))
			return nil
		}
		tmb.Go(communicate)
		tmb.Go(communicate)
		return nil
	})

	wg.Wait()
	c.Assert(conns, HasLen, 2)
	c.Check(conns[0], NotNil)
	c.Check(conns[1], NotNil)
	c.Check(conns[0], Not(Equals), conns[1])

	close(complete)
	c.Check(tmb.Wait(), IsNil)

	c.Check(d.Stop(), IsNil)
}
