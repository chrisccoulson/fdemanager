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

package daemon

import (
	"net"
	"syscall"
)

type (
	AccessChecker = accessChecker
	ApiError      = apiError
	Command       = command
	ConnTracker   = connTracker
	Response      = response
)

var (
	AsyncResponse          = asyncResponse
	ConnectionKey          = connectionKey
	NewConnTracker         = newConnTracker
	OpenAccess             = openAccess
	StatusUnathorized      = statusUnauthorized
	StatusMethodNotAllowed = statusMethodNotAllowed
	StatusInternalError    = statusInternalError
	SyncResponse           = syncResponse
)

func MockApiCommands(mockApi []*Command) (restore func()) {
	orig := apiCommands
	apiCommands = mockApi
	return func() {
		apiCommands = orig
	}
}

func MockNetutilConnPeerCred(fn func(net.Conn) (*syscall.Ucred, error)) (restore func()) {
	orig := netutilConnPeerCred
	netutilConnPeerCred = fn
	return func() {
		netutilConnPeerCred = orig
	}
}
