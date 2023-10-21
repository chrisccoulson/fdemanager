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
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/snapcore/fdemanager/internal/netutil"
	"github.com/snapcore/fdemanager/internal/overlord"
	"github.com/snapcore/fdemanager/internal/paths"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/standby"
	"github.com/snapcore/snapd/overlord/state"
	"gopkg.in/tomb.v2"
)

const (
	socketRestartMsg = "daemon is stopping to wait for socket activation"
)

var (
	ErrRestartSocket = fmt.Errorf("daemon stop requested to wait for socket activation")

	shutdownTimeout = 25 * time.Second
)

type connTracker struct {
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func newConnTracker() *connTracker {
	return &connTracker{conns: make(map[net.Conn]struct{})}
}

func (ct *connTracker) CanStandby() bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	return len(ct.conns) == 0
}

func (ct *connTracker) TrackConn(conn net.Conn, state http.ConnState) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	// we ignore hijacked connections, if we do things with websockets
	// we'll need custom shutdown handling for them
	if state == http.StateNew || state == http.StateActive {
		ct.conns[conn] = struct{}{}
	} else {
		delete(ct.conns, conn)
	}
}

type wrappedWriter struct {
	w http.ResponseWriter
	s int
}

func (w *wrappedWriter) Header() http.Header {
	return w.w.Header()
}

func (w *wrappedWriter) Write(bs []byte) (int, error) {
	return w.w.Write(bs)
}

func (w *wrappedWriter) WriteHeader(s int) {
	w.w.WriteHeader(s)
	w.s = s
}

func (w *wrappedWriter) Flush() {
	if f, ok := w.w.(http.Flusher); ok {
		f.Flush()
	}
}

func logit(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := &wrappedWriter{w: w}
		t0 := time.Now()
		handler.ServeHTTP(ww, r)
		t := time.Since(t0)
		url := r.URL.String()
		if !strings.Contains(url, "/changes/") {
			logger.Debugf("%s %s %s %s %d", r.RemoteAddr, r.Method, r.URL, t, ww.s)
		}
	})
}

// A Daemon listens for requests and routes them to the right command
type Daemon struct {
	overlord        *overlord.Overlord
	state           *state.State
	router          *mux.Router
	listener        net.Listener
	socketActivated bool
	connTracker     *connTracker
	serve           *http.Server
	standbyOpinions *standby.StandbyOpinions
	tomb            tomb.Tomb

	restartSocket bool

	mu sync.Mutex
}

// New creates a new daemon.
func New() (*Daemon, error) {
	d := &Daemon{}

	ovld, err := overlord.New(d)
	if err != nil {
		return nil, err
	}
	d.overlord = ovld
	d.state = ovld.State()

	d.addRoutes()

	return d, nil
}

func (d *Daemon) initStandbyHandling() {
	d.standbyOpinions = standby.New(d.state)
	d.standbyOpinions.AddOpinion(d.connTracker)
	d.standbyOpinions.AddOpinion(d.overlord)
	d.standbyOpinions.AddOpinion(d)
	d.standbyOpinions.Start()
}

type commandDispatcher struct {
	d *Daemon
	c *command
}

func (cd *commandDispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rsp := cd.c.Run(cd.d, r)
	rsp.Write(w)
}

func (d *Daemon) addRoutes() {
	d.router = mux.NewRouter()

	for _, c := range api {
		dispatcher := &commandDispatcher{d: d, c: c}

		if c.PathPrefix == "" {
			d.router.Handle(c.Path, dispatcher).Name(c.Path)
		} else {
			d.router.PathPrefix(c.PathPrefix).Handler(dispatcher).Name(c.PathPrefix)
		}
	}

	// also maybe add a /favicon.ico handler...

	d.router.NotFoundHandler = statusNotFound("not found")
}

func (d *Daemon) HandleRestart(t restart.RestartType, rebootInfo *boot.RebootInfo) {
	if t != restart.RestartSocket {
		logger.Panicf("unexpected restart type")
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.restartSocket = true

	d.tomb.Kill(nil)
}

func (d *Daemon) RebootAsExpected(st *state.State) error {
	return nil
}

func (d *Daemon) RebootDidNotHappen(st *state.State) error {
	return errors.New("internal error: unexpected RebootDidNotHappen callback")
}

func (d *Daemon) CanStandby() bool {
	return d.socketActivated
}

// Start starts the daemon.
func (d *Daemon) Start() error {
	if d.overlord == nil {
		panic("internal error: no Overlord")
	}

	listener, activated, err := netutil.GetUnixSocketListener(paths.ManagerSocket)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %v", paths.ManagerSocket, err)
	}
	d.listener = &ucrednetListener{Listener: listener}
	d.socketActivated = activated
	if activated {
		logger.Debugf("socket %q was activated", paths.ManagerSocket)
	} else {
		logger.Debugf("socket %q was not activated", paths.ManagerSocket)
	}

	d.connTracker = newConnTracker()
	d.serve = &http.Server{
		Handler:   logit(d.router),
		ConnState: d.connTracker.TrackConn,
	}

	d.initStandbyHandling()

	d.overlord.Loop()

	d.tomb.Go(func() error {
		if err := d.serve.Serve(d.listener); err != http.ErrServerClosed && d.tomb.Err() == tomb.ErrStillAlive {
			return err
		}

		return nil
	})

	return nil
}

// Stop stops the daemon.
func (d *Daemon) Stop() error {
	if d.overlord == nil {
		return errors.New("internal error: no Overlord")
	}

	d.tomb.Kill(nil)

	d.mu.Lock()
	restartSocket := d.restartSocket
	d.mu.Unlock()

	d.listener.Close()
	d.standbyOpinions.Stop()

	// We're using the background context here because the tomb's
	// context will likely already have been cancelled when we are
	// called.
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	d.tomb.Kill(d.serve.Shutdown(ctx))
	cancel()

	if restartSocket {
		// At this point we processed all open requests (and
		// stopped accepting new requests) - before going into
		// socket activated mode we need to check if any of
		// those open requests resulted in something that
		// prevents us from going into socket activation mode.
		//
		// If this is the case we do a "normal" snapd restart
		// to process the new changes.
		if !d.standbyOpinions.CanStandby() {
			d.restartSocket = false
		}
	}
	d.overlord.Stop()

	if err := d.tomb.Wait(); err != nil {
		if err == context.DeadlineExceeded {
			logger.Noticef("WARNING: cannot gracefully shut down in-flight snapd API activity within: %v", shutdownTimeout)
			// the process is shutting down anyway, so we may just
			// as well close the active connections right now
			d.serve.Close()
		} else {
			return err
		}
	}

	if d.restartSocket {
		return ErrRestartSocket
	}

	return nil
}

// Dying returns a channel that is closed when the daemon is
// put into a dying state.
func (d *Daemon) Dying() <-chan struct{} {
	return d.tomb.Dying()
}
