/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// applySSA server-side applies obj using the operator's field manager and
// takes ForceOwnership so we reclaim fields from previous owners on upgrade.
//
// Wire-equivalent to the deprecated `client.Apply` sentinel (which is
// `json.Marshal(obj)` + `ApplyPatchType` under the hood) but goes through
// `client.RawPatch` instead. Controller-runtime marks `client.Apply` with
// SA1019 in favor of the typed `Client.Apply()` method, which requires
// generated ApplyConfigurations for every owned type — too much scaffolding
// for v1alpha1. `RawPatch(ApplyPatchType, ...)` is the supported escape
// hatch and isn't deprecated.
//
// Requires every builder to populate TypeMeta on its output so apiVersion
// and kind land in the request body — SSA rejects requests without them.
func applySSA(ctx context.Context, c client.Client, obj client.Object) error {
	body, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshalling %T %s: %w", obj, obj.GetName(), err)
	}
	return c.Patch(ctx, obj,
		client.RawPatch(types.ApplyPatchType, body),
		client.ForceOwnership,
		client.FieldOwner(fieldManager),
	)
}
