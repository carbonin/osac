/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package baremetalhost

import (
	metal3api "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
)

// ResolveHardwareDetails selects the Metal3 hardware inventory to read NIC
// data from, preferring the standalone HardwareData resource over the
// deprecated BareMetalHost.Status.HardwareDetails. It returns
// hd.Spec.HardwareDetails when that carries NIC inventory, otherwise
// bmh.Status.HardwareDetails (which may itself be nil). hd may be nil when no
// companion HardwareData exists, and bmh may be nil when the caller only has
// HardwareData in hand. An empty HardwareData does not shadow a populated
// status: precedence means "prefer HardwareData when it actually has NIC
// data". This is the single place to change if strict presence-precedence is
// ever wanted.
func ResolveHardwareDetails(hd *metal3api.HardwareData, bmh *metal3api.BareMetalHost) *metal3api.HardwareDetails {
	if hd != nil && hd.Spec.HardwareDetails != nil && len(hd.Spec.HardwareDetails.NIC) > 0 {
		return hd.Spec.HardwareDetails
	}
	if bmh != nil {
		return bmh.Status.HardwareDetails
	}
	return nil
}
