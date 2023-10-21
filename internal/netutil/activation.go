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
	"fmt"
	"net"
	"os"
	"runtime"

	"github.com/coreos/go-systemd/activation"
	"golang.org/x/sys/unix"
)

// GetUnixSocketListener returns a new net.Listener for the unix socket with
// the specified path, consuming the one passed from systemd if this process
// was socket activated and the requested unix socket is already open.
func GetUnixSocketListener(path string) (listener net.Listener, activated bool, err error) {
	files := activation.Files(false)
	for _, f := range files {
		listener, err = net.FileListener(f)
		if err != nil {
			// skip non stream sockets
			continue
		}
		if _, isUnix := listener.(*net.UnixListener); !isUnix {
			// skip non unix domain sockets
			continue
		}
		if listener.Addr().String() == path {
			// the socket was already opened for us by systemd
			return listener, true, nil
		}
	}

	// We need to create and open the socket ourselves.

	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return nil, false, fmt.Errorf("socket %q is already in use", path)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, false, err
	}

	address, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		return nil, false, err
	}

	runtime.LockOSThread()
	oldmask := unix.Umask(0111)
	listener, err = net.ListenUnix("unix", address)
	unix.Umask(oldmask)
	runtime.UnlockOSThread()
	if err != nil {
		return nil, false, err
	}

	return listener, false, nil
}
