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
	"io"
	"net"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/snapcore/fdemanager/api"
	"github.com/snapcore/fdemanager/internal/netutil"
	"github.com/snapcore/snapd/logger"
)

var (
	muxVars             = mux.Vars
	netutilConnPeerCred = netutil.ConnPeerCred
)

// A responseFunc handles one of the individual verbs for a method
type responseFunc func(*Daemon, map[string]string, io.Reader) response

// A command routes a request to an individual per-verb responseFUnc
type command struct {
	Path       string
	PathPrefix string
	//
	GET  responseFunc
	PUT  responseFunc
	POST responseFunc

	// Access control.
	ReadAccess  accessChecker
	WriteAccess accessChecker
}

func (c *command) Run(d *Daemon, r *http.Request) response {
	// obtain the connection associated with the context attached to this
	// request (see Daemon.Start).
	conn, ok := r.Context().Value(connectionKey).(net.Conn)
	if !ok {
		logger.Panicf("no connection associated with request")
	}
	ucred, err := netutilConnPeerCred(conn)
	if err != nil {
		logger.Noticef("unexpected error when attempting to obtain peer credentials: %v", err)
		return statusInternalError(err.Error())
	}

	var rspf responseFunc
	var access accessChecker

	switch r.Method {
	case http.MethodGet:
		rspf = c.GET
		access = c.ReadAccess
	case http.MethodPut:
		rspf = c.PUT
		access = c.WriteAccess
	case http.MethodPost:
		rspf = c.POST
		access = c.WriteAccess
	}

	if rspf == nil {
		return statusMethodNotAllowed("method %q not allowed", r.Method)
	}
	if access == nil {
		return statusInternalError("no access checker for method %q", r.Method)
	}

	allowInteraction := false
	allowHeader := r.Header.Get(api.AllowInteractionHeader)
	if allowHeader != "" {
		var err error
		allowInteraction, err = strconv.ParseBool(allowHeader)
		if err != nil {
			logger.Noticef("error parsing %s header: %s", api.AllowInteractionHeader, err)
		}
	}
	if err := access.CheckAccess(d, ucred, allowInteraction); err != nil {
		return err
	}

	return rspf(d, muxVars(r), r.Body)
}
