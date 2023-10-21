// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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

package patch

import (
	"errors"

	"github.com/snapcore/snapd/overlord/state"
)

var Level = 1

// Sublevel is the current implemented sublevel for the Level.
// Sublevel 0 is the first patch for the new Level, rollback below x.0 is not possible.
// Sublevel patches > 0 do not prevent rollbacks.
var Sublevel = 0

type PatchFunc func(s *state.State) error

// patches maps from patch level L to the list of sublevel patches.
var patches = make(map[int][]PatchFunc)

// Init initializes an empty state to the current implemented patch level.
func Init(s *state.State) {
	s.Lock()
	defer s.Unlock()
	if err := s.Get("patch-level", new(int)); !errors.Is(err, state.ErrNoState) {
		panic("internal error: expected empty state, attempting to override patch-level without actual patching")
	}
	s.Set("patch-level", Level)

	if err := s.Get("patch-sublevel", new(int)); !errors.Is(err, state.ErrNoState) {
		panic("internal error: expected empty state, attempting to override patch-sublevel without actual patching")
	}
	s.Set("patch-sublevel", Sublevel)
}

// Apply applies any necessary patches to update the provided state to
// conventions required by the current patch level of the system.
func Apply(st *state.State) error {
	return nil
}

// Mock mocks the current patch level and available patches.
func Mock(level int, sublevel int, p map[int][]PatchFunc) (restore func()) {
	oldLevel := Level
	oldPatches := patches
	Level = level
	patches = p

	oldSublevel := Sublevel
	Sublevel = sublevel

	return func() {
		Level = oldLevel
		patches = oldPatches
		Sublevel = oldSublevel
	}
}
