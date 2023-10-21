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

package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/snapcore/fdemanager/internal/daemon"
	"github.com/snapcore/fdemanager/internal/paths"
	"github.com/snapcore/snapd/logger"
)

func init() {
	err := logger.SimpleSetup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to activate logging: %s\n", err)
	}
	paths.SetTargetRootDir(os.Getenv("FDEMANAGERD_TARGET_ROOT"))
}

func run(ch chan os.Signal) error {
	d, err := daemon.New()
	if err != nil {
		return err
	}

	if err := d.Start(); err != nil {
		return err
	}

	logger.Noticef("started daemon.")

out:
	for {
		select {
		case sig := <-ch:
			logger.Noticef("Exiting on %s signal.\n", sig)
			break out
		case <-d.Dying():
			// something called Stop()
			break out
		}
	}

	return d.Stop()
}

func main() {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	if err := run(ch); err != nil {
		if err == daemon.ErrRestartSocket {
			// Note that we don't prepend: "error: " here because
			// ErrRestartSocket is not an error as such.
			fmt.Fprintf(os.Stdout, "%v\n", err)
			// the exit code must be in sync with
			// data/systemd/snapd.service.in:SuccessExitStatus=
			os.Exit(42)
		}
		fmt.Fprintf(os.Stderr, "cannot run daemon: %v\n", err)
		os.Exit(1)
	}
}
