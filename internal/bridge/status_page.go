// SPDX-FileCopyrightText: 2026 Sofie Finnes <sofie+git@finnes.dev>
// SPDX-License-Identifier: MPL-2.0
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	upstream "github.com/breml/terraform-provider-uptimekuma/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type statusPage struct {
	resource.Resource
	endpoint string
}

func (r *statusPage) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.Resource.Schema(ctx, req, resp)
	resp.Schema.Attributes["published"] = schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), MarkdownDescription: "Public status page. Kuma's save handler does not support unpublishing; false is rejected."}
}

func (r *statusPage) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if configured, ok := r.Resource.(resource.ResourceWithConfigure); ok {
		configured.Configure(ctx, req, resp)
	}
	session.Lock()
	defer session.Unlock()
	r.endpoint = currentEndpoint
}

// Upstream's private Socket.IO response omits flags and monitor groups. Fetch
// their authoritative public representation instead of echoing desired state.
func (r *statusPage) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.Resource.Read(ctx, req, resp)
	if resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
		return
	}
	var model upstream.StatusPageResourceModel
	resp.Diagnostics.Append(resp.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	snapshot, err := readPage(ctx, r.endpoint, model.Slug.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read Kuma public status page", err.Error())
		return
	}
	model.Published = types.BoolValue(*snapshot.Config.Published)
	if *snapshot.Config.Theme != "" || !model.Theme.IsNull() {
		model.Theme = types.StringValue(*snapshot.Config.Theme)
	}
	model.ShowTags = types.BoolValue(*snapshot.Config.ShowTags)
	model.ShowPoweredBy = types.BoolValue(*snapshot.Config.ShowPoweredBy)
	model.ShowCertificateExpiry = types.BoolValue(*snapshot.Config.ShowCertificateExpiry)
	groups := make([]upstream.PublicGroupModel, 0, len(snapshot.Groups))
	for _, group := range snapshot.Groups {
		monitors := make([]upstream.PublicMonitorModel, 0, len(group.Monitors))
		for _, m := range group.Monitors {
			link := types.StringNull()
			if m.URL != nil {
				link = types.StringValue(*m.URL)
			}
			monitors = append(monitors, upstream.PublicMonitorModel{ID: types.Int64Value(m.ID), SendURL: types.BoolValue(bool(m.SendURL)), URL: link})
		}
		list, diags := types.ListValueFrom(ctx, monitorType, monitors)
		resp.Diagnostics.Append(diags...)
		groups = append(groups, upstream.PublicGroupModel{ID: types.Int64Value(group.ID), Name: types.StringValue(group.Name), Weight: types.Int64Value(group.Weight), MonitorList: list})
	}
	list, diags := types.ListValueFrom(ctx, groupType, groups)
	resp.Diagnostics.Append(diags...)
	model.PublicGroupList = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
func (r *statusPage) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var published types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("published"), &published)...)
	if !published.IsNull() && !published.IsUnknown() && !published.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("published"), "Unpublishing is unsupported", "Kuma's save-status-page API does not implement changing published. Use published=true.")
	}

	if validator, ok := r.Resource.(resource.ResourceWithValidateConfig); ok {
		validator.ValidateConfig(ctx, req, resp)
	}
}

var monitorType = types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.Int64Type, "send_url": types.BoolType, "url": types.StringType}}
var groupType = types.ObjectType{AttrTypes: map[string]attr.Type{"id": types.Int64Type, "name": types.StringType, "weight": types.Int64Type, "monitor_list": types.ListType{ElemType: monitorType}}}

// Kuma encodes sendUrl as 0/1 in its public response, rather than JSON booleans.
type publicBool bool

func (b *publicBool) UnmarshalJSON(raw []byte) error {
	switch string(raw) {
	case "true", "1":
		*b = true
	case "false", "0", "null":
		*b = false
	default:
		return fmt.Errorf("invalid public monitor visibility flag")
	}
	return nil
}

type pageSnapshot struct {
	Config *struct {
		Published             *bool   `json:"published"`
		Theme                 *string `json:"theme"`
		ShowTags              *bool   `json:"showTags"`
		ShowPoweredBy         *bool   `json:"showPoweredBy"`
		ShowCertificateExpiry *bool   `json:"showCertificateExpiry"`
	} `json:"config"`
	Groups []struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Weight   int64  `json:"weight"`
		Monitors []struct {
			ID      int64      `json:"id"`
			SendURL publicBool `json:"sendUrl"`
			URL     *string    `json:"url"`
		} `json:"monitorList"`
	} `json:"publicGroupList"`
}

func readPage(ctx context.Context, endpoint, slug string) (pageSnapshot, error) {
	var result pageSnapshot
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/api/status-page/"+url.PathEscape(slug)+"?crossplane="+fmt.Sprint(time.Now().UnixNano()), nil)
	if err != nil {
		return result, fmt.Errorf("build public page request: %w", err)
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return result, fmt.Errorf("fetch public page: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("public page returned HTTP %d", response.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&result); err != nil {
		return result, fmt.Errorf("decode public page: %w", err)
	}
	if result.Config == nil || result.Config.Theme == nil || result.Config.Published == nil || result.Config.ShowTags == nil || result.Config.ShowPoweredBy == nil || result.Config.ShowCertificateExpiry == nil {
		return result, fmt.Errorf("public page response omitted required configuration flags")
	}
	if result.Groups == nil {
		return result, fmt.Errorf("public page response omitted monitor groups")
	}
	return result, nil
}
