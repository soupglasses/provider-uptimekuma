// SPDX-FileCopyrightText: 2026 Sofie Finnes <sofie+git@finnes.dev>
// SPDX-License-Identifier: MPL-2.0
package config

import (
	"fmt"
	"strconv"

	ujconfig "github.com/crossplane/upjet/v2/pkg/config"
)

// Upjet exposes root identifiers as strings, while Kuma's Terraform schemas use numbers.
// Nested monitor and group IDs remain numeric and are not external identifiers.
type numericID struct{}

func (numericID) Convert(params map[string]any, _ *ujconfig.Resource, mode ujconfig.Mode) (map[string]any, error) {
	if mode == ujconfig.FromTerraform && params["id"] != nil {
		switch id := params["id"].(type) {
		case float64:
			params["id"] = strconv.FormatInt(int64(id), 10)
		case int64:
			params["id"] = strconv.FormatInt(id, 10)
		case string:
		default:
			return nil, fmt.Errorf("unexpected numeric ID type %T", id)
		}
	}
	return params, nil
}
