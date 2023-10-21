// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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

package overlord_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"

	. "github.com/snapcore/fdemanager/internal/overlord"
	"github.com/snapcore/fdemanager/internal/overlord/patch"
	"github.com/snapcore/fdemanager/internal/paths"
)

func TestOverlord(t *testing.T) { TestingT(t) }

type overlordSuite struct {
	testutil.BaseTest
}

var _ = Suite(&overlordSuite{})

type ticker struct {
	tickerChannel chan time.Time
}

func (w *ticker) tick(n int) {
	for i := 0; i < n; i++ {
		w.tickerChannel <- time.Now()
	}
}

func fakePruneTicker() (w *ticker, restore func()) {
	w = &ticker{
		tickerChannel: make(chan time.Time),
	}
	restore = MockPruneTicker(func(t *time.Ticker) <-chan time.Time {
		return w.tickerChannel
	})
	return w, restore
}

func (s *overlordSuite) SetUpTest(c *C) {
	// TODO: temporary: skip due to timeouts on riscv64
	if runtime.GOARCH == "riscv64" || os.Getenv("SNAPD_SKIP_SLOW_TESTS") != "" {
		c.Skip("skipping slow test")
	}

	tmpdir := c.MkDir()
	s.AddCleanup(paths.MockRootDir(tmpdir))
	c.Check(os.MkdirAll(paths.ManagerStateDir, 0755), IsNil)
}

func (s *overlordSuite) TestNew(c *C) {
	restore := patch.Mock(42, 2, nil)
	defer restore()

	o, err := New(nil)
	c.Assert(err, IsNil)
	c.Check(o, NotNil)

	c.Check(o.StateEngine(), NotNil)
	c.Check(o.TaskRunner(), NotNil)
	c.Check(o.RestartManager(), NotNil)

	st := o.State()
	c.Check(st, NotNil)
	c.Check(o.StateEngine().State(), Equals, st)

	st.Lock()
	defer st.Unlock()
	var patchLevel, patchSublevel int
	st.Get("patch-level", &patchLevel)
	c.Check(patchLevel, Equals, 42)
	st.Get("patch-sublevel", &patchSublevel)
	c.Check(patchSublevel, Equals, 2)
}

func (s *overlordSuite) TestNewWithGoodState(c *C) {
	// ensure we don't write state load timing in the state on really
	// slow architectures (e.g. risc-v)
	oldDurationThreshold := timings.DurationThreshold
	timings.DurationThreshold = time.Second * 30
	defer func() { timings.DurationThreshold = oldDurationThreshold }()

	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"patch-sublevel":%d,"patch-sublevel-last-version":0.1,"some":"data"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0,"last-lane-id":0}`, patch.Level, patch.Sublevel))
	err := ioutil.WriteFile(paths.ManagerStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	o, err := New(nil)
	c.Assert(err, IsNil)
	c.Check(o.RestartManager(), NotNil)

	state := o.State()
	c.Assert(err, IsNil)
	state.Lock()
	defer state.Unlock()

	d, err := state.MarshalJSON()
	c.Assert(err, IsNil)

	var got, expected map[string]interface{}
	err = json.Unmarshal(d, &got)
	c.Assert(err, IsNil)
	err = json.Unmarshal(fakeState, &expected)
	c.Assert(err, IsNil)

	data, _ := got["data"].(map[string]interface{})
	c.Assert(data, NotNil)

	c.Check(got, DeepEquals, expected)
}

func (s *overlordSuite) TestNewWithInvalidState(c *C) {
	fakeState := []byte(``)
	err := ioutil.WriteFile(paths.ManagerStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	_, err = New(nil)
	c.Assert(err, ErrorMatches, "cannot read state: EOF")
}

type witnessManager struct {
	state          *state.State
	expectedEnsure int
	ensureCalled   chan struct{}
	ensureCallback func(s *state.State) error
}

func (wm *witnessManager) Ensure() error {
	if wm.expectedEnsure--; wm.expectedEnsure == 0 {
		close(wm.ensureCalled)
		return nil
	}
	if wm.ensureCallback != nil {
		return wm.ensureCallback(wm.state)
	}
	return nil
}

func (s *overlordSuite) TestTrivialRunAndStop(c *C) {
	o, err := New(nil)
	c.Assert(err, IsNil)

	o.Loop()

	err = o.Stop()
	c.Assert(err, IsNil)
}

func (s *overlordSuite) TestUnknownTasks(c *C) {
	o, err := New(nil)
	c.Assert(err, IsNil)

	// unknown tasks are ignored and succeed
	st := o.State()
	st.Lock()
	defer st.Unlock()
	t := st.NewTask("unknown", "...")
	chg := st.NewChange("change-w-unknown", "...")
	chg.AddTask(t)

	st.Unlock()
	err = o.Settle(1 * time.Second)
	st.Lock()
	c.Assert(err, IsNil)

	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (s *overlordSuite) TestEnsureLoopRunAndStop(c *C) {
	restoreIntv := MockEnsureInterval(10 * time.Millisecond)
	defer restoreIntv()
	o := Mock()

	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 3,
		ensureCalled:   make(chan struct{}),
	}
	o.AddManager(witness)

	o.Loop()
	defer o.Stop()

	t0 := time.Now()
	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
	c.Check(time.Since(t0) >= 10*time.Millisecond, Equals, true)

	err := o.Stop()
	c.Assert(err, IsNil)
}

func (s *overlordSuite) TestEnsureLoopMediatedEnsureBeforeImmediate(c *C) {
	restoreIntv := MockEnsureInterval(10 * time.Minute)
	defer restoreIntv()
	o := Mock()

	ensure := func(st *state.State) error {
		st.EnsureBefore(0)
		return nil
	}

	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 2,
		ensureCalled:   make(chan struct{}),
		ensureCallback: ensure,
	}
	o.AddManager(witness)

	o.Loop()
	defer o.Stop()

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
}

func (s *overlordSuite) TestEnsureLoopMediatedEnsureBefore(c *C) {
	restoreIntv := MockEnsureInterval(10 * time.Minute)
	defer restoreIntv()
	o := Mock()

	ensure := func(st *state.State) error {
		st.EnsureBefore(10 * time.Millisecond)
		return nil
	}

	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 2,
		ensureCalled:   make(chan struct{}),
		ensureCallback: ensure,
	}
	o.AddManager(witness)

	o.Loop()
	defer o.Stop()

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
}

func (s *overlordSuite) TestEnsureBeforeSleepy(c *C) {
	restoreIntv := MockEnsureInterval(10 * time.Minute)
	defer restoreIntv()
	o := Mock()

	ensure := func(st *state.State) error {
		MockEnsureNext(o, time.Now().Add(-10*time.Hour))
		st.EnsureBefore(0)
		return nil
	}

	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 2,
		ensureCalled:   make(chan struct{}),
		ensureCallback: ensure,
	}
	o.AddManager(witness)

	o.Loop()
	defer o.Stop()

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
}

func (s *overlordSuite) TestEnsureBeforeLater(c *C) {
	restoreIntv := MockEnsureInterval(10 * time.Minute)
	defer restoreIntv()
	o := Mock()

	ensure := func(st *state.State) error {
		MockEnsureNext(o, time.Now().Add(-10*time.Hour))
		st.EnsureBefore(time.Second * 5)
		return nil
	}

	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 2,
		ensureCalled:   make(chan struct{}),
		ensureCallback: ensure,
	}
	o.AddManager(witness)

	o.Loop()
	defer o.Stop()

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
}

func (s *overlordSuite) TestEnsureLoopMediatedEnsureBeforeOutsideEnsure(c *C) {
	restoreIntv := MockEnsureInterval(10 * time.Minute)
	defer restoreIntv()
	o := Mock()

	ch := make(chan struct{})
	ensure := func(st *state.State) error {
		close(ch)
		return nil
	}

	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 2,
		ensureCalled:   make(chan struct{}),
		ensureCallback: ensure,
	}
	o.AddManager(witness)

	o.Loop()
	defer o.Stop()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}

	o.State().EnsureBefore(0)

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
}

func (ovs *overlordSuite) TestEnsureLoopPrune(c *C) {
	restoreIntv := MockPruneInterval(200*time.Millisecond, 1000*time.Millisecond, 1000*time.Millisecond)
	defer restoreIntv()
	o := Mock()

	st := o.State()
	st.Lock()
	t1 := st.NewTask("foo", "...")
	chg1 := st.NewChange("abort", "...")
	chg1.AddTask(t1)
	chg2 := st.NewChange("prune", "...")
	chg2.SetStatus(state.DoneStatus)
	t0 := chg2.ReadyTime()
	st.Unlock()

	// observe the loop cycles to detect when prune should have happened
	pruneHappened := make(chan struct{})
	cycles := -1
	waitForPrune := func(_ *state.State) error {
		if cycles == -1 {
			if time.Since(t0) > 1000*time.Millisecond {
				cycles = 2 // wait a couple more loop cycles
			}
			return nil
		}
		if cycles > 0 {
			cycles--
			if cycles == 0 {
				close(pruneHappened)
			}
		}
		return nil
	}
	witness := &witnessManager{
		ensureCallback: waitForPrune,
	}
	o.AddManager(witness)

	o.Loop()

	select {
	case <-pruneHappened:
	case <-time.After(2 * time.Second):
		c.Fatal("Pruning should have happened by now")
	}

	err := o.Stop()
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	c.Assert(st.Change(chg1.ID()), Equals, chg1)
	c.Assert(st.Change(chg2.ID()), IsNil)

	c.Assert(t1.Status(), Equals, state.HoldStatus)
}

func (s *overlordSuite) TestEnsureLoopPruneRunsMultipleTimes(c *C) {
	restoreIntv := MockPruneInterval(100*time.Millisecond, 5*time.Millisecond, 1*time.Hour)
	defer restoreIntv()
	o := Mock()

	// create two changes, one that can be pruned now, one in progress
	st := o.State()
	st.Lock()
	t1 := st.NewTask("foo", "...")
	chg1 := st.NewChange("pruneNow", "...")
	chg1.AddTask(t1)
	t1.SetStatus(state.DoneStatus)
	t2 := st.NewTask("foo", "...")
	chg2 := st.NewChange("pruneNext", "...")
	chg2.AddTask(t2)
	t2.SetStatus(state.DoStatus)
	c.Check(st.Changes(), HasLen, 2)
	st.Unlock()

	w, restoreTicker := fakePruneTicker()
	defer restoreTicker()

	// start the loop that runs the prune ticker
	o.Loop()

	// this needs to be more than pruneWait=5ms mocked above
	time.Sleep(10 * time.Millisecond)
	w.tick(2)

	st.Lock()
	c.Check(st.Changes(), HasLen, 1)
	chg2.SetStatus(state.DoneStatus)
	st.Unlock()

	// this needs to be more than pruneWait=5ms mocked above
	time.Sleep(10 * time.Millisecond)
	// tick twice for extra Ensure
	w.tick(2)

	st.Lock()
	c.Check(st.Changes(), HasLen, 0)
	st.Unlock()

	// cleanup loop ticker
	err := o.Stop()
	c.Assert(err, IsNil)
}

func (s *overlordSuite) TestEnsureLoopPruneAbortsOld(c *C) {
	// Ensure interval is not relevant for this test
	restoreEnsureIntv := MockEnsureInterval(10 * time.Hour)
	defer restoreEnsureIntv()

	w, restoreTicker := fakePruneTicker()
	defer restoreTicker()

	o := Mock()

	// avoid immediate transition to Done due to having unknown kind
	o.TaskRunner().AddHandler("bar", func(t *state.Task, _ *tomb.Tomb) error {
		return &state.Retry{}
	}, nil)

	st := o.State()
	st.Lock()

	// spawn time one month ago
	spawnTime := time.Now().AddDate(0, -1, 0)
	restoreTimeNow := state.MockTime(spawnTime)
	t := st.NewTask("bar", "...")
	chg := st.NewChange("other-change", "...")
	chg.AddTask(t)

	restoreTimeNow()

	// validity
	c.Check(st.Changes(), HasLen, 1)
	st.Unlock()

	// start the loop that runs the prune ticker
	o.Loop()
	w.tick(2)

	c.Assert(o.Stop(), IsNil)

	st.Lock()
	defer st.Unlock()

	c.Assert(st.Changes(), HasLen, 1)
	// change was aborted
	c.Check(chg.Status(), Equals, state.HoldStatus)
}

func (s *overlordSuite) TestCheckpoint(c *C) {
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	o, err := New(nil)
	c.Assert(err, IsNil)

	st := o.State()
	st.Lock()
	st.Set("mark", 1)
	st.Unlock()

	info, err := os.Stat(paths.ManagerStateFile)
	c.Assert(err, IsNil)
	c.Assert(info.Mode(), Equals, os.FileMode(0600))

	c.Check(paths.ManagerStateFile, testutil.FileContains, `"mark":1`)
}

type sampleManager struct {
	ensureCallback func()
}

func newSampleManager(runner *state.TaskRunner) *sampleManager {
	sm := &sampleManager{}

	runner.AddHandler("runMgr1", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.Set("runMgr1Mark", 1)
		return nil
	}, nil)
	runner.AddHandler("runMgr2", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.Set("runMgr2Mark", 1)
		return nil
	}, nil)
	runner.AddHandler("runMgrEnsureBefore", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.EnsureBefore(20 * time.Millisecond)
		return nil
	}, nil)
	runner.AddHandler("runMgrForever", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.EnsureBefore(20 * time.Millisecond)
		return &state.Retry{}
	}, nil)
	runner.AddHandler("runMgrWCleanup", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.Set("runMgrWCleanupMark", 1)
		return nil
	}, nil)
	runner.AddCleanup("runMgrWCleanup", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.Set("runMgrWCleanupCleanedUp", 1)
		return nil
	})

	return sm
}

func (sm *sampleManager) Ensure() error {
	if sm.ensureCallback != nil {
		sm.ensureCallback()
	}
	return nil
}

func (s *overlordSuite) TestTrivialSettle(c *C) {
	restoreIntv := MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o := Mock()

	sm1 := newSampleManager(o.TaskRunner())
	o.AddManager(sm1)
	o.AddManager(o.TaskRunner())

	defer o.StateEngine().Stop()

	st := o.State()
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("chg", "...")
	t1 := st.NewTask("runMgr1", "1...")
	chg.AddTask(t1)

	st.Unlock()
	o.Settle(5 * time.Second)
	st.Lock()
	c.Check(t1.Status(), Equals, state.DoneStatus)

	var v int
	err := st.Get("runMgr1Mark", &v)
	c.Check(err, IsNil)
}

func (s *overlordSuite) TestSettleNotConverging(c *C) {
	restoreIntv := MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o := Mock()

	sm1 := newSampleManager(o.TaskRunner())
	o.AddManager(sm1)
	o.AddManager(o.TaskRunner())

	defer o.StateEngine().Stop()

	st := o.State()
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("chg", "...")
	t1 := st.NewTask("runMgrForever", "1...")
	chg.AddTask(t1)

	st.Unlock()
	err := o.Settle(250 * time.Millisecond)
	st.Lock()

	c.Check(err, ErrorMatches, `Settle is not converging`)

}

func (s *overlordSuite) TestSettleChain(c *C) {
	restoreIntv := MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o := Mock()

	sm1 := newSampleManager(o.TaskRunner())
	o.AddManager(sm1)
	o.AddManager(o.TaskRunner())

	defer o.StateEngine().Stop()

	st := o.State()
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("chg", "...")
	t1 := st.NewTask("runMgr1", "1...")
	t2 := st.NewTask("runMgr2", "2...")
	t2.WaitFor(t1)
	chg.AddAll(state.NewTaskSet(t1, t2))

	st.Unlock()
	o.Settle(5 * time.Second)
	st.Lock()
	c.Check(t1.Status(), Equals, state.DoneStatus)
	c.Check(t2.Status(), Equals, state.DoneStatus)

	var v int
	err := st.Get("runMgr1Mark", &v)
	c.Check(err, IsNil)
	err = st.Get("runMgr2Mark", &v)
	c.Check(err, IsNil)
}

func (s *overlordSuite) TestSettleChainWCleanup(c *C) {
	restoreIntv := MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o := Mock()

	sm1 := newSampleManager(o.TaskRunner())
	o.AddManager(sm1)
	o.AddManager(o.TaskRunner())

	defer o.StateEngine().Stop()

	st := o.State()
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("chg", "...")
	t1 := st.NewTask("runMgrWCleanup", "1...")
	t2 := st.NewTask("runMgr2", "2...")
	t2.WaitFor(t1)
	chg.AddAll(state.NewTaskSet(t1, t2))

	st.Unlock()
	o.Settle(5 * time.Second)
	st.Lock()
	c.Check(t1.Status(), Equals, state.DoneStatus)
	c.Check(t2.Status(), Equals, state.DoneStatus)

	var v int
	err := st.Get("runMgrWCleanupMark", &v)
	c.Check(err, IsNil)
	err = st.Get("runMgr2Mark", &v)
	c.Check(err, IsNil)

	err = st.Get("runMgrWCleanupCleanedUp", &v)
	c.Check(err, IsNil)
}

func (s *overlordSuite) TestSettleExplicitEnsureBefore(c *C) {
	restoreIntv := MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o := Mock()

	st := o.State()
	sm1 := newSampleManager(o.TaskRunner())
	sm1.ensureCallback = func() {
		st.Lock()
		defer st.Unlock()
		v := 0
		st.Get("ensureCount", &v)
		st.Set("ensureCount", v+1)
	}

	o.AddManager(sm1)
	o.AddManager(o.TaskRunner())

	defer o.StateEngine().Stop()

	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("chg", "...")
	t := st.NewTask("runMgrEnsureBefore", "...")
	chg.AddTask(t)

	st.Unlock()
	o.Settle(5 * time.Second)
	st.Lock()
	c.Check(t.Status(), Equals, state.DoneStatus)

	var v int
	err := st.Get("ensureCount", &v)
	c.Check(err, IsNil)
	c.Check(v, Equals, 2)
}

func (s *overlordSuite) TestRequestRestartNoHandler(c *C) {
	o, err := New(nil)
	c.Assert(err, IsNil)

	st := o.State()
	st.Lock()
	defer st.Unlock()

	restart.Request(st, restart.RestartDaemon, nil)
}

type testRestartHandler struct {
	restartRequested  restart.RestartType
	rebootState       string
	rebootVerifiedErr error
}

func (rb *testRestartHandler) HandleRestart(t restart.RestartType, ri *boot.RebootInfo) {
	rb.restartRequested = t
}

func (rb *testRestartHandler) RebootAsExpected(_ *state.State) error {
	rb.rebootState = "as-expected"
	return rb.rebootVerifiedErr
}

func (rb *testRestartHandler) RebootDidNotHappen(_ *state.State) error {
	rb.rebootState = "did-not-happen"
	return rb.rebootVerifiedErr
}

func (s *overlordSuite) TestRequestRestartHandler(c *C) {
	rb := &testRestartHandler{}

	o, err := New(rb)
	c.Assert(err, IsNil)

	st := o.State()
	st.Lock()
	defer st.Unlock()

	restart.Request(st, restart.RestartDaemon, nil)

	c.Check(rb.restartRequested, Equals, restart.RestartDaemon)
}

func (s *overlordSuite) TestOverlordCanStandby(c *C) {
	restoreIntv := MockEnsureInterval(10 * time.Millisecond)
	defer restoreIntv()
	o := Mock()
	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 3,
		ensureCalled:   make(chan struct{}),
	}
	o.AddManager(witness)

	// can only standby after loop ran once
	c.Assert(o.CanStandby(), Equals, false)

	o.Loop()
	defer o.Stop()

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}

	c.Assert(o.CanStandby(), Equals, true)
}

func (s *overlordSuite) TestLockFailed(c *C) {
	f, err := osutil.NewFileLockWithMode(paths.ManagerStateLockFile, 0644)
	c.Assert(err, IsNil)
	defer func() {
		f.Close()
		os.Remove(paths.ManagerStateLockFile)
	}()

	c.Check(f.Lock(), IsNil)

	_, err = New(nil)
	c.Check(err, ErrorMatches, `fatal: could not lock state file: cannot acquire lock, already locked`)
}
