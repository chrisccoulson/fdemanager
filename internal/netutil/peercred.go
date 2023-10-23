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

package netutil

import (
	"errors"
	"math"
	"net"
	"syscall"
)

var ErrNoPeerCred = errors.New("connection has no peer credential")

// ConnPeerCred obtains the peer credentials associated with a connection,
// where this is supported. If a connection does not support obtaining peer
// credentials, ErrNoPeerCred is returned.
func ConnPeerCred(conn net.Conn) (*syscall.Ucred, error) {
	sc, ok := conn.(syscall.Conn)
	if !ok {
		return nil, ErrNoPeerCred
	}

	raw, err := sc.SyscallConn()
	if err != nil {
		return nil, err
	}

	var ucred *syscall.Ucred
	scErr := raw.Control(func(fd uintptr) {
		ucred, err = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if scErr != nil {
		return nil, scErr
	}
	if err != nil {
		return nil, err
	}

	if ucred.Pid == 0 || ucred.Uid == math.MaxUint32 || ucred.Gid == math.MaxUint32 {
		return nil, ErrNoPeerCred
	}
	return ucred, nil
}
