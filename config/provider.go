// SPDX-FileCopyrightText: 2026 Sofie Finnes <sofie+git@finnes.dev>
// SPDX-License-Identifier: MPL-2.0
package config

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"

	"github.com/breml/terraform-provider-uptimekuma/crossplane/internal/bridge"
	upstream "github.com/breml/terraform-provider-uptimekuma/internal/provider"
	ujconfig "github.com/crossplane/upjet/v2/pkg/config"
	"github.com/hashicorp/terraform-plugin-framework/provider"
)

//go:embed schema.json
var providerSchema []byte

const Module = "github.com/breml/terraform-provider-uptimekuma/crossplane"

func FrameworkProvider() provider.Provider {
	return &bridge.Provider{Provider: upstream.New("crossplane")()}
}
func WithSchema(raw []byte, group string) *ujconfig.Provider {
	p := ujconfig.NewProvider(raw, "uptimekuma", Module, []byte("name: uptimekuma\nresources: {}\n"),
		ujconfig.WithRootGroup(group), ujconfig.WithFeaturesPackage("internal/features"), ujconfig.WithIncludeList([]string{}),
		ujconfig.WithTerraformPluginFrameworkIncludeList([]string{"uptimekuma_monitor_http$", "uptimekuma_monitor_gamedig$", "uptimekuma_status_page$"}),
		ujconfig.WithTerraformPluginFrameworkProvider(FrameworkProvider()))
	p.BasePackages = ujconfig.BasePackages{}
	for name, kind := range map[string]string{"monitor_http": "HTTPMonitor", "monitor_gamedig": "GameMonitor", "status_page": "StatusPage"} {
		p.AddResourceConfigurator("uptimekuma_"+name, func(r *ujconfig.Resource) {
			r.Kind, r.ShortGroup, r.UseAsync = kind, "", false
			r.TerraformConversions = []ujconfig.TerraformConversion{numericID{}}
			r.ExternalName = ujconfig.IdentifierFromProvider
			r.ExternalName.GetIDFn = func(_ context.Context, name string, _ map[string]any, _ map[string]any) (string, error) {
				if name == "" {
					return "0", nil
				}
				return name, nil
			}
			r.ExternalName.SetIdentifierArgumentFn = func(args map[string]any, externalName string) {
				if externalName != "" {
					id, err := strconv.ParseInt(externalName, 10, 64)
					if err == nil {
						args["id"] = id
					}
				}
			}
			r.ExternalName.GetExternalNameFn = func(state map[string]any) (string, error) {
				switch id := state["id"].(type) {
				case float64:
					return strconv.FormatInt(int64(id), 10), nil
				case int64:
					return strconv.FormatInt(id, 10), nil
				case string:
					return id, nil
				default:
					return "", fmt.Errorf("unexpected monitor ID type %T", id)
				}
			}
		})
	}
	p.ConfigureResources()
	return p
}
func Namespaced() *ujconfig.Provider { return WithSchema(providerSchema, "uptimekuma.ops.finnes.dev") }
