// SPDX-FileCopyrightText: 2026 Sofie Finnes <sofie+git@finnes.dev>
// SPDX-License-Identifier: MPL-2.0
package bridge

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"

	client "github.com/breml/terraform-provider-uptimekuma/internal/client"
	upstream "github.com/breml/terraform-provider-uptimekuma/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Provider reuses the configured upstream client across reconciles. A runtime is
// restricted to one namespaced ProviderConfig because upstream has one global pool.
type Provider struct{ provider.Provider }

var currentEndpoint string
var session struct {
	sync.Mutex
	key        [32]byte
	configured bool
	response   provider.ConfigureResponse
}

func (p *Provider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	key := sha256.Sum256([]byte(req.Config.Raw.String()))
	session.Lock()
	defer session.Unlock()
	if session.configured && session.key == key && connectionAlive(ctx, req) {
		*resp = session.response
		return
	}
	if ctx.Err() != nil {
		resp.Diagnostics.AddError("Kuma configuration canceled", ctx.Err().Error())
		return
	}

	if session.configured {
		// Pool references count Configure calls, not in-flight operations.
		_ = client.GetGlobalPool().Close()
		session.configured = false
	}
	p.Provider.Configure(ctx, req, resp)
	if !resp.Diagnostics.HasError() {
		var endpoint types.String
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("endpoint"), &endpoint)...)
		currentEndpoint = endpoint.ValueString()
		session.key = key
		session.response = *resp
		session.configured = true
	}
}
func (p *Provider) Resources(ctx context.Context) []func() resource.Resource {
	factories := p.Provider.Resources(ctx)
	out := make([]func() resource.Resource, 0, len(factories))
	for _, factory := range factories {
		out = append(out, func() resource.Resource {
			r := factory()
			m := resource.MetadataResponse{}
			r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "uptimekuma"}, &m)
			if m.TypeName == "uptimekuma_status_page" {
				return &statusPage{Resource: r}
			}
			return r
		})
	}
	return out
}

// Resync is a read-only round trip. The embedded client does not reconnect, so a
// cached pointer alone cannot establish that its Socket.IO session is usable.
func connectionAlive(ctx context.Context, req provider.ConfigureRequest) bool {
	var data upstream.UptimeKumaProviderModel
	if diagnostics := req.Config.Get(ctx, &data); diagnostics.HasError() {
		return false
	}
	duration := func(value types.String) time.Duration { d, _ := time.ParseDuration(value.ValueString()); return d }
	retries := 3
	if !data.MaxRetries.IsNull() {
		retries = int(data.MaxRetries.ValueInt64())
	}
	pool := client.GetGlobalPool()
	c, err := pool.GetOrCreate(context.Background(), &client.Config{
		Endpoint: data.Endpoint.ValueString(), Username: data.Username.ValueString(), Password: data.Password.ValueString(),
		ConnectTimeout: duration(data.Timeout), PerAttemptTimeout: duration(data.PerAttemptTimeout), OperationTimeout: duration(data.OperationTimeout), MaxRetries: retries,
	})
	if err != nil {
		return false
	}
	pool.Release()
	healthCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err = c.Resync(healthCtx)
	return err == nil
}
