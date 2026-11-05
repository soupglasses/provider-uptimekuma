// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/upjet/v2/pkg/controller"

	gamemonitor "github.com/breml/terraform-provider-uptimekuma/crossplane/internal/controller/namespaced/uptimekuma/gamemonitor"
	httpmonitor "github.com/breml/terraform-provider-uptimekuma/crossplane/internal/controller/namespaced/uptimekuma/httpmonitor"
	statuspage "github.com/breml/terraform-provider-uptimekuma/crossplane/internal/controller/namespaced/uptimekuma/statuspage"
)

// Setup creates all controllers with the supplied logger and adds them to
// the supplied manager.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		gamemonitor.Setup,
		httpmonitor.Setup,
		statuspage.Setup,
	} {
		if err := setup(mgr, o); err != nil {
			return err
		}
	}
	return nil
}

// SetupGated creates all controllers with the supplied logger and adds them to
// the supplied manager gated.
func SetupGated(mgr ctrl.Manager, o controller.Options) error {
	for _, setup := range []func(ctrl.Manager, controller.Options) error{
		gamemonitor.SetupGated,
		httpmonitor.SetupGated,
		statuspage.SetupGated,
	} {
		if err := setup(mgr, o); err != nil {
			return err
		}
	}
	return nil
}

// SetupWebhookWithManager registers conversion webhooks for all resource kinds in the group.
func SetupWebhookWithManager(mgr ctrl.Manager) error {
	for _, setup := range []func(ctrl.Manager) error{
		gamemonitor.SetupWebhookWithManager,
		httpmonitor.SetupWebhookWithManager,
		statuspage.SetupWebhookWithManager,
	} {
		if err := setup(mgr); err != nil {
			return err
		}
	}
	return nil
}
