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

package paths

import (
	"path/filepath"
)

var (
	rootdir       = "/"
	targetRootdir = ""

	ManagerSocket        string
	ManagerStateDir      string
	ManagerStateFile     string
	ManagerStateLockFile string
)

func init() {
	reinit()
}

func reinit() {
	ManagerSocket = filepath.Join(rootdir, "run/fdemanagerd.socket")

	ManagerStateDir = filepath.Join(rootdir, "var/lib/fdemanagerd")
	ManagerStateFile = filepath.Join(ManagerStateDir, "state.json")
	ManagerStateLockFile = filepath.Join(ManagerStateDir, "state.lock")

	SetTargetRootDir(targetRootdir)
}

// SetTargetRootDir sets the root directory of the target system
// in which state will be stored during installation.
func SetTargetRootDir(target string) {
	targetRootdir = target
}

func MockRootDir(dir string) (restore func()) {
	if dir == "" {
		dir = "/"
	}

	orig := rootdir
	rootdir = dir
	reinit()

	return func() {
		rootdir = orig
		reinit()
	}
}
