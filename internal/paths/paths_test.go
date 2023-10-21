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

package paths_test

import (
	"testing"

	. "gopkg.in/check.v1"

	. "github.com/snapcore/fdemanager/internal/paths"
)

func Test(t *testing.T) { TestingT(t) }

type pathsSuite struct{}

var _ = Suite(&pathsSuite{})

func (s *pathsSuite) TestDefault(c *C) {
	c.Check(ManagerSocket, Equals, "/run/fdemanagerd.socket")
	c.Check(ManagerStateDir, Equals, "/var/lib/fdemanagerd")
	c.Check(ManagerStateFile, Equals, "/var/lib/fdemanagerd/state.json")
	c.Check(ManagerStateLockFile, Equals, "/var/lib/fdemanagerd/state.lock")
}
