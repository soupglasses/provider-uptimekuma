//go:build integration

// SPDX-FileCopyrightText: 2026 Sofie Finnes <sofie+git@finnes.dev>
// SPDX-License-Identifier: MPL-2.0
package crossplane_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	kuma "github.com/breml/go-uptime-kuma-client"
	api "github.com/breml/terraform-provider-uptimekuma/crossplane/apis/namespaced/uptimekuma/v1alpha1"
	localconfig "github.com/breml/terraform-provider-uptimekuma/crossplane/config"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/controller"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func ptr[T any](v T) *T { return &v }
func TestNativeResourcesAgainstKuma(t *testing.T) {
	endpoint := os.Getenv("KUMA_TEST_ENDPOINT")
	passwordPath := os.Getenv("KUMA_TEST_PASSWORD_FILE")
	if endpoint == "" || passwordPath == "" {
		t.Skip("set disposable Kuma fixture endpoint and password file")
	}
	password, err := os.ReadFile(passwordPath)
	if err != nil {
		t.Fatal(err)
	}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{FrameworkProvider: localconfig.FrameworkProvider(), Configuration: terraform.ProviderConfiguration{"endpoint": endpoint, "username": "crossplane-poc", "password": strings.TrimSpace(string(password))}}, nil
	}
	kube := clientfake.NewClientBuilder().Build()
	connect := func(obj xpresource.Managed, kind string) managed.ExternalClient {
		t.Helper()
		connector := controller.NewTerraformPluginFrameworkConnector(kube, setup, localconfig.Namespaced().Resources[kind], controller.NewOperationStore(logging.NewNopLogger()), controller.WithTerraformPluginFrameworkLogger(logging.NewNopLogger()))
		ec, err := connector.Connect(context.Background(), obj)
		if err != nil {
			t.Fatal(err)
		}
		return ec
	}
	game := &api.GameMonitor{ObjectMeta: metav1.ObjectMeta{Name: "minecraft", Namespace: "status", UID: "crossplane-game-fixture"}}
	game.Spec.ForProvider = api.GameMonitorParameters{Name: ptr("Crossplane Minecraft fixture"), Hostname: ptr("mc.finnes.dev"), Port: ptr(float64(25565)), Game: ptr("minecraft"), Interval: ptr(float64(20)), Active: ptr(true), GamedigGivenPortOnly: ptr(true)}
	ec := connect(game, "uptimekuma_monitor_gamedig")
	if _, err = ec.Observe(t.Context(), game); err != nil {
		t.Fatal(err)
	}
	if _, err = ec.Create(t.Context(), game); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := connect(game, "uptimekuma_monitor_gamedig").Delete(context.Background(), game); err != nil {
			t.Error(err)
		}
	})
	originalID := meta.GetExternalName(game)
	if originalID == "" {
		t.Fatal("created monitor has no external ID")
	}
	for range 2 {
		observation, err := connect(game, "uptimekuma_monitor_gamedig").Observe(t.Context(), game)
		if err != nil {
			t.Fatal(err)
		}
		if !observation.ResourceExists {
			t.Fatal("created monitor not observed")
		}
	}
	// Rebuild the CR from Git plus the external identifier, discarding all status.
	adopted := &api.GameMonitor{ObjectMeta: metav1.ObjectMeta{Name: "adopted", Namespace: "status", UID: "fresh-kubernetes-uid"}}
	adopted.Spec = game.Spec
	meta.SetExternalName(adopted, originalID)
	observation, err := connect(adopted, "uptimekuma_monitor_gamedig").Observe(t.Context(), adopted)
	if err != nil || !observation.ResourceExists {
		t.Fatalf("monitor adoption failed: %+v %v", observation, err)
	}
	if meta.GetExternalName(adopted) != originalID {
		t.Fatal("adoption replaced monitor identity")
	}
	httpMonitor := &api.HTTPMonitor{ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "status", UID: "crossplane-http-fixture"}}
	httpMonitor.Spec.ForProvider = api.HTTPMonitorParameters{Name: ptr("Crossplane HTTP fixture"), URL: ptr("https://auth.finnes.dev"), Interval: ptr(float64(20)), Active: ptr(true)}
	ec = connect(httpMonitor, "uptimekuma_monitor_http")
	if _, err = ec.Observe(t.Context(), httpMonitor); err != nil {
		t.Fatal(err)
	}
	if _, err = ec.Create(t.Context(), httpMonitor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := connect(httpMonitor, "uptimekuma_monitor_http").Delete(context.Background(), httpMonitor); err != nil {
			t.Error(err)
		}
	})
	observation, err = connect(httpMonitor, "uptimekuma_monitor_http").Observe(t.Context(), httpMonitor)
	if err != nil || !observation.ResourceExists {
		t.Fatalf("HTTP monitor read failed: %+v %v", observation, err)
	}

	page := &api.StatusPage{ObjectMeta: metav1.ObjectMeta{Name: "page", Namespace: "status", UID: "crossplane-page-fixture"}}
	page.Spec.ForProvider = api.StatusPageParameters{Slug: ptr("crossplane-fixture"), Title: ptr("Crossplane fixture"), Published: ptr(true), PublicGroupList: []api.PublicGroupListParameters{{Name: ptr("Services"), Weight: ptr(float64(1)), MonitorList: []api.MonitorListParameters{{ID: ptr(mustID(t, originalID)), SendURL: ptr(false)}}}}}
	ec = connect(page, "uptimekuma_status_page")
	if _, err = ec.Observe(t.Context(), page); err != nil {
		t.Fatal(err)
	}
	if _, err = ec.Create(t.Context(), page); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := connect(page, "uptimekuma_status_page").Delete(context.Background(), page); err != nil {
			t.Error(err)
		}
	})
	observation, err = connect(page, "uptimekuma_status_page").Observe(t.Context(), page)
	if err != nil || !observation.ResourceExists {
		t.Fatalf("page read failed: %+v %v", observation, err)
	}
	// Drift the remote page through Kuma's real API, then require native reconciliation to repair it.
	editor, err := kuma.New(t.Context(), endpoint, "crossplane-poc", strings.TrimSpace(string(password)))
	if err != nil {
		t.Fatal(err)
	}
	defer editor.Disconnect()
	remote, err := editor.GetStatusPage(t.Context(), "crossplane-fixture")
	if err != nil {
		t.Fatal(err)
	}
	remote.PublicGroupList = nil
	remote.ShowTags = true
	if _, err = editor.SaveStatusPage(t.Context(), remote); err != nil {
		t.Fatal(err)
	}
	ec = connect(page, "uptimekuma_status_page")
	observation, err = ec.Observe(t.Context(), page)
	if err != nil {
		t.Fatal(err)
	}
	if observation.ResourceUpToDate {
		t.Fatal("deleted monitor groups were incorrectly reported up to date")
	}
	if _, err = ec.Update(t.Context(), page); err != nil {
		t.Fatal(err)
	}
	request, err := http.Get(endpoint + "/api/status-page/crossplane-fixture?verify=crossplane")
	if err != nil {
		t.Fatal(err)
	}
	defer request.Body.Close()
	var public struct {
		Groups []struct {
			Monitors []any `json:"monitorList"`
		} `json:"publicGroupList"`
	}
	if err = json.NewDecoder(request.Body).Decode(&public); err != nil {
		t.Fatal(err)
	}
	if len(public.Groups) != 1 || len(public.Groups[0].Monitors) != 1 {
		t.Fatal("native update did not restore the real public page")
	}
	for range 2 {
		observation, err = connect(page, "uptimekuma_status_page").Observe(t.Context(), page)
		if err != nil {
			t.Fatal(err)
		}
		if !observation.ResourceUpToDate {
			t.Fatalf("page failed to converge after repair: %+v", observation)
		}
	}

	// A server restart must not strand the provider on a dead cached Socket.IO connection.
	if fixture := os.Getenv("KUMA_TEST_CONTAINER"); fixture != "" {
		args := strings.Fields(os.Getenv("CONTAINER_ENGINE"))
		if len(args) == 0 {
			args = []string{"docker"}
		}
		args = append(args, "restart", fixture)
		command := exec.CommandContext(t.Context(), args[0], args[1:]...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("restart disposable fixture: %v %s", err, output)
		}
		ready := false
		for range 60 {
			response, err := http.Get(endpoint)
			if err == nil {
				response.Body.Close()
				if response.StatusCode == 200 {
					ready = true
					break
				}
			}
			time.Sleep(time.Second)
		}
		if !ready {
			t.Fatal("fixture failed to restart")
		}
		observation, err = connect(game, "uptimekuma_monitor_gamedig").Observe(t.Context(), game)
		if err != nil || !observation.ResourceExists {
			t.Fatalf("native monitor failed to recover after Kuma restart: %+v %v", observation, err)
		}
		if meta.GetExternalName(game) != originalID {
			t.Fatal("restart replaced monitor identity")
		}
	}

	t.Logf("Native monitor ID %s survived adoption; public page ID %s read successfully", originalID, meta.GetExternalName(page))
}

func mustID(t *testing.T, s string) float64 {
	t.Helper()
	v, e := strconv.ParseFloat(s, 64)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
