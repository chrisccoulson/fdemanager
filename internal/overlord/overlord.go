// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers
// +build !nomanagers

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

// Package overlord implements the overall control of a snappy system.
package overlord

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/timings"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/fdemanager/internal/overlord/patch"
	"github.com/snapcore/fdemanager/internal/paths"
)

var (
	ensureInterval = 5 * time.Minute
	pruneInterval  = 10 * time.Minute
	pruneWait      = 24 * time.Hour * 1
	abortWait      = 24 * time.Hour * 3

	pruneMaxChanges = 500
)

var pruneTickerC = func(t *time.Ticker) <-chan time.Time {
	return t.C
}

// Overlord is the central manager of a snappy system, keeping
// track of all available state managers and related helpers.
type Overlord struct {
	stateFLock *osutil.FileLock

	stateEng *StateEngine

	// ensure loop
	loopTomb    *tomb.Tomb
	ensureLock  sync.Mutex
	ensureTimer *time.Timer
	ensureNext  time.Time
	ensureRun   int32
	pruneTicker *time.Ticker
	didPrune    bool

	// managers
	inited     bool
	runner     *state.TaskRunner
	restartMgr *restart.RestartManager
}

// New creates a new Overlord with all its state managers.
func New(restartHandler restart.Handler) (*Overlord, error) {
	o := &Overlord{
		loopTomb: new(tomb.Tomb),
		inited:   true,
	}

	backend := &overlordStateBackend{
		path:         paths.ManagerStateFile,
		ensureBefore: o.ensureBefore,
	}
	s, restartMgr, err := o.loadState(backend, restartHandler)
	if err != nil {
		return nil, err
	}

	o.stateEng = NewStateEngine(s)
	o.runner = state.NewTaskRunner(s)

	// any unknown task should be ignored and succeed
	matchAnyUnknownTask := func(_ *state.Task) bool {
		return true
	}
	o.runner.AddOptionalHandler(matchAnyUnknownTask, handleUnknownTask, nil)

	o.restartMgr = restartMgr
	o.addManager(o.restartMgr)

	// the shared task runner should be added last!
	o.addManager(o.runner)

	return o, nil
}

func (o *Overlord) addManager(mgr StateManager) {
	o.stateEng.AddManager(mgr)
}

func initStateFileLock() (*osutil.FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(paths.ManagerStateLockFile), 0755); err != nil {
		return nil, err
	}

	return osutil.NewFileLockWithMode(paths.ManagerStateLockFile, 0644)
}

func (o *Overlord) loadState(backend state.Backend, restartHandler restart.Handler) (*state.State, *restart.RestartManager, error) {
	flock, err := initStateFileLock()
	if err != nil {
		return nil, nil, fmt.Errorf("fatal: error opening lock file: %v", err)
	}
	o.stateFLock = flock

	logger.Noticef("Acquiring state lock file")
	if err := flock.TryLock(); err != nil {
		logger.Noticef("Failed to lock state file")
		return nil, nil, fmt.Errorf("fatal: could not lock state file: %w", err)
	}
	logger.Noticef("Acquired state lock file")

	if !osutil.FileExists(paths.ManagerStateFile) {
		// fail fast, mostly interesting for tests, this dir is setup
		// by the snapd package
		stateDir := filepath.Dir(paths.ManagerStateFile)
		if !osutil.IsDirectory(stateDir) {
			return nil, nil, fmt.Errorf("fatal: directory %q must be present", stateDir)
		}
		s := state.New(backend)

		restartMgr, err := initRestart(s, restartHandler)
		if err != nil {
			return nil, nil, err
		}

		patch.Init(s)
		return s, restartMgr, nil
	}

	r, err := os.Open(paths.ManagerStateFile)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read the state file: %s", err)
	}
	defer r.Close()

	perfTimings := timings.New(map[string]string{"startup": "load-state"})

	var s *state.State
	timings.Run(perfTimings, "read-state", "read fdemanagerd state from disk", func(tm timings.Measurer) {
		s, err = state.ReadState(backend, r)
	})
	if err != nil {
		return nil, nil, err
	}

	restartMgr, err := initRestart(s, restartHandler)
	if err != nil {
		return nil, nil, err
	}

	// one-shot migrations
	err = patch.Apply(s)
	if err != nil {
		return nil, nil, err
	}
	return s, restartMgr, nil
}

func initRestart(s *state.State, restartHandler restart.Handler) (*restart.RestartManager, error) {
	s.Lock()
	defer s.Unlock()
	return restart.Manager(s, "", restartHandler)
}

func (o *Overlord) ensureTimerSetup() {
	o.ensureLock.Lock()
	defer o.ensureLock.Unlock()
	o.ensureTimer = time.NewTimer(ensureInterval)
	o.ensureNext = time.Now().Add(ensureInterval)
	o.pruneTicker = time.NewTicker(pruneInterval)
}

func (o *Overlord) ensureTimerReset() time.Time {
	o.ensureLock.Lock()
	defer o.ensureLock.Unlock()
	now := time.Now()
	o.ensureTimer.Reset(ensureInterval)
	o.ensureNext = now.Add(ensureInterval)
	return o.ensureNext
}

func (o *Overlord) ensureBefore(d time.Duration) {
	o.ensureLock.Lock()
	defer o.ensureLock.Unlock()
	if o.ensureTimer == nil {
		panic("cannot use EnsureBefore before Overlord.Loop")
	}
	now := time.Now()
	next := now.Add(d)
	if next.Before(o.ensureNext) {
		o.ensureTimer.Reset(d)
		o.ensureNext = next
		return
	}

	if o.ensureNext.Before(now) {
		// timer already expired, it will be reset in Loop() and
		// next Ensure() will be called shortly.
		if !o.ensureTimer.Stop() {
			return
		}
		o.ensureTimer.Reset(0)
		o.ensureNext = now
	}
}

func (o *Overlord) prune() {
	st := o.State()
	st.Lock()
	st.Prune(time.Time{}, pruneWait, abortWait, pruneMaxChanges)
	st.Unlock()
	o.didPrune = true
}

// Loop runs a loop in a goroutine to ensure the current state regularly through StateEngine Ensure.
func (o *Overlord) Loop() {
	o.ensureTimerSetup()
	o.loopTomb.Go(func() error {
		for {
			// TODO: pass a proper context into Ensure
			o.ensureTimerReset()
			// in case of errors engine logs them,
			// continue to the next Ensure() try for now
			o.stateEng.Ensure()
			o.ensureDidRun()

			select {
			case <-o.loopTomb.Dying():
				return nil
			case <-o.ensureTimer.C:
			case <-pruneTickerC(o.pruneTicker):
				o.prune()
			}
		}
	})
}

func (o *Overlord) ensureDidRun() {
	atomic.StoreInt32(&o.ensureRun, 1)
}

func (o *Overlord) CanStandby() bool {
	run := atomic.LoadInt32(&o.ensureRun)
	return run != 0
}

// Stop stops the ensure loop and the managers under the StateEngine.
func (o *Overlord) Stop() error {
	var err error
	if o.loopTomb != nil {
		o.loopTomb.Kill(nil)
		err = o.loopTomb.Wait()
	}
	if !o.didPrune {
		// in most cases, we expect to be a short-running, socket-activated service,
		// so make sure that this actually runs
		o.prune()
	}
	o.stateEng.Stop()
	if o.stateFLock != nil {
		// This will also unlock the file
		o.stateFLock.Close()
		logger.Noticef("Released state lock file")
	}
	return err
}

func (o *Overlord) settle(timeout time.Duration, beforeCleanups func()) error {
	func() {
		o.ensureLock.Lock()
		defer o.ensureLock.Unlock()
		if o.ensureTimer != nil {
			panic("cannot use Settle concurrently with other Settle or Loop calls")
		}
		o.ensureTimer = time.NewTimer(0)
	}()

	defer func() {
		o.ensureLock.Lock()
		defer o.ensureLock.Unlock()
		o.ensureTimer.Stop()
		o.ensureTimer = nil
	}()

	t0 := time.Now()
	done := false
	var errs []error
	for !done {
		if timeout > 0 && time.Since(t0) > timeout {
			err := fmt.Errorf("Settle is not converging")
			if len(errs) != 0 {
				return &ensureError{append(errs, err)}
			}
			return err
		}
		next := o.ensureTimerReset()
		err := o.stateEng.Ensure()
		switch ee := err.(type) {
		case nil:
		case *ensureError:
			errs = append(errs, ee.errs...)
		default:
			errs = append(errs, err)
		}
		o.stateEng.Wait()
		o.ensureLock.Lock()
		done = o.ensureNext.Equal(next)
		o.ensureLock.Unlock()
		if done {
			if beforeCleanups != nil {
				beforeCleanups()
				beforeCleanups = nil
			}
			// we should wait also for cleanup handlers
			st := o.State()
			st.Lock()
			for _, chg := range st.Changes() {
				if chg.IsReady() && !chg.IsClean() {
					done = false
					break
				}
			}
			st.Unlock()
		}
	}
	if len(errs) != 0 {
		return &ensureError{errs}
	}
	return nil
}

// Settle runs first a state engine Ensure and then wait for
// activities to settle. That's done by waiting for all managers'
// activities to settle while making sure no immediate further Ensure
// is scheduled. It then waits similarly for all ready changes to
// reach the clean state. Chiefly for tests. Cannot be used in
// conjunction with Loop. If timeout is non-zero and settling takes
// longer than timeout, returns an error. Calls StartUp as well.
func (o *Overlord) Settle(timeout time.Duration) error {
	return o.settle(timeout, nil)
}

// SettleObserveBeforeCleanups runs first a state engine Ensure and
// then wait for activities to settle. That's done by waiting for all
// managers' activities to settle while making sure no immediate
// further Ensure is scheduled. It then waits similarly for all ready
// changes to reach the clean state, but calls once the provided
// callback before doing that. Chiefly for tests. Cannot be used in
// conjunction with Loop. If timeout is non-zero and settling takes
// longer than timeout, returns an error. Calls StartUp as well.
func (o *Overlord) SettleObserveBeforeCleanups(timeout time.Duration, beforeCleanups func()) error {
	return o.settle(timeout, beforeCleanups)
}

// State returns the system state managed by the overlord.
func (o *Overlord) State() *state.State {
	return o.stateEng.State()
}

// StateEngine returns the stage engine used by overlord.
func (o *Overlord) StateEngine() *StateEngine {
	return o.stateEng
}

// TaskRunner returns the shared task runner responsible for running
// tasks for all managers under the overlord.
func (o *Overlord) TaskRunner() *state.TaskRunner {
	return o.runner
}

// RestartManager returns the manager responsible for restart state.
func (o *Overlord) RestartManager() *restart.RestartManager {
	return o.restartMgr
}

// Mock creates an Overlord without any managers and with a backend
// not using disk. Managers can be added with AddManager. For testing.
func Mock() *Overlord {
	return MockWithState(nil)
}

// MockWithState creates an Overlord with the given state
// unless it is nil in which case it uses a state backend not using
// disk. Managers can be added with AddManager. For testing.
func MockWithState(s *state.State) *Overlord {
	o := &Overlord{
		loopTomb: new(tomb.Tomb),
		inited:   false,
	}
	if s == nil {
		s = state.New(mockBackend{o: o})
	}
	o.stateEng = NewStateEngine(s)
	o.runner = state.NewTaskRunner(s)

	return o
}

// AddManager adds a manager to the overlord created with Mock. For
// testing.
func (o *Overlord) AddManager(mgr StateManager) {
	if o.inited {
		panic("internal error: cannot add managers to a fully initialized Overlord")
	}
	o.addManager(mgr)
}

type mockBackend struct {
	o *Overlord
}

func (mb mockBackend) Checkpoint(data []byte) error {
	return nil
}

func (mb mockBackend) EnsureBefore(d time.Duration) {
	mb.o.ensureLock.Lock()
	timer := mb.o.ensureTimer
	mb.o.ensureLock.Unlock()
	if timer == nil {
		return
	}

	mb.o.ensureBefore(d)
}
