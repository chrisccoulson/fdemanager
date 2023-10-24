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

package client

import (
	"context"
	"net/url"

	"github.com/snapcore/fdemanager/api"
)

func (c *Client) Do(ctx context.Context, method, path string, query url.Values, args any) (*api.Response, error) {
	return c.do(ctx, method, path, query, args)
}

func (c *Client) DoSync(ctx context.Context, method, path string, query url.Values, args, result any) error {
	return c.doSync(ctx, method, path, query, args, result)
}
