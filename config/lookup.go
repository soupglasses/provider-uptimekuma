// SPDX-FileCopyrightText: 2026 Sofie Finnes <sofie+git@finnes.dev>
// SPDX-License-Identifier: MPL-2.0
package config

import (
	"context"
	"fmt"
	"strconv"
	"time"

	kuma "github.com/breml/go-uptime-kuma-client"
	"github.com/crossplane/upjet/v2/pkg/terraform"
)

// Resolve an unbound declaration by its stable application name before creating
// anything. This also recovers ownership after Kubernetes status is lost.
func lookupID(ctx context.Context, resourceName string, parameters, setup map[string]any) (string, error) {
	configuration, ok := setup["configuration"].(terraform.ProviderConfiguration)
	if !ok {
		return "", fmt.Errorf("provider configuration missing during Kuma adoption")
	}
	text := func(key string) string { s, _ := configuration[key].(string); return s }
	c, err := kuma.New(ctx, text("endpoint"), text("username"), text("password"), kuma.WithConnectTimeout(15*time.Second), kuma.WithOperationTimeout(15*time.Second))
	if err != nil {
		return "", fmt.Errorf("cannot inspect Kuma for adoption")
	}
	defer c.Disconnect()
	if resourceName == "status_page" {
		slug, _ := parameters["slug"].(string)
		pages, err := c.GetStatusPages(ctx)
		if err != nil {
			return "", fmt.Errorf("cannot list Kuma status pages for adoption")
		}
		for _, page := range pages {
			if page.Slug == slug {
				return strconv.FormatInt(page.ID, 10), nil
			}
		}
		return "0", nil
	}
	expected, _ := parameters["name"].(string)
	desiredType := "http"
	if resourceName == "monitor_gamedig" {
		desiredType = "gamedig"
	}
	monitors, err := c.GetMonitors(ctx)
	if err != nil {
		return "", fmt.Errorf("cannot list Kuma monitors for adoption")
	}
	var id int64
	count := 0
	for _, monitor := range monitors {
		if monitor.Name == expected {
			count++
			id = monitor.ID
			if monitor.Type() != desiredType {
				return "", fmt.Errorf("monitor name exists with a different type; specify an explicit external identifier")
			}
		}
	}
	if count > 1 {
		return "", fmt.Errorf("monitor name is ambiguous; specify an explicit external identifier")
	}
	if count == 1 {
		return strconv.FormatInt(id, 10), nil
	}
	return "0", nil
}
