//go:build ignore

// SPDX-FileCopyrightText: 2026 Sofie Finnes <sofie+git@finnes.dev>
// SPDX-License-Identifier: MPL-2.0
package main

import (
	"context"
	"os"
	"strings"
	"time"

	kuma "github.com/breml/go-uptime-kuma-client"
)

func main() {
	p, e := os.ReadFile(os.Getenv("KUMA_TEST_PASSWORD_FILE"))
	if e != nil {
		panic(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, e := kuma.New(ctx, os.Getenv("KUMA_TEST_ENDPOINT"), "crossplane-poc", strings.TrimSpace(string(p)), kuma.WithAutosetup())
	if e != nil {
		panic("cannot initialize disposable Kuma")
	}
	if e = c.Disconnect(); e != nil {
		panic("cannot disconnect disposable Kuma")
	}
}
