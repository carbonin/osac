/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package it

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/osac-project/osac/fulfillment-service/internal/uuid"
	privatev1 "github.com/osac-project/osac/proto/gen/osac/private/v1"
)

var _ = Describe("Private bare metal instance types", func() {
	var (
		ctx    context.Context
		client privatev1.BareMetalInstanceTypesClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewBareMetalInstanceTypesClient(tool.InternalView().AdminConn())
	})

	// Happy path lifecycle

	It("Can create a bare metal instance type", func() {
		name := fmt.Sprintf("it-bm-type-%s", uuid.New())
		response, err := client.Create(ctx, privatev1.BareMetalInstanceTypesCreateRequest_builder{
			Object: privatev1.BareMetalInstanceType_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: privatev1.BareMetalInstanceTypeSpec_builder{
					Hardware: privatev1.BareMetalHardwareSpec_builder{
						Cpu: privatev1.BareMetalCPUSpec_builder{
							Cores:          32,
							Architecture:   "amd64",
							Model:          "Intel Xeon Gold",
							ThreadsPerCore: 2,
						}.Build(),
						Memory: privatev1.BareMetalMemorySpec_builder{
							TotalGb: 128,
							Type:    "DDR4",
						}.Build(),
						Disks: []*privatev1.BareMetalDiskSpec{
							privatev1.BareMetalDiskSpec_builder{
								Type:       "NVMe",
								CapacityGb: 1000,
								Interface:  "PCIe",
							}.Build(),
						},
						NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{
							privatev1.BareMetalNetworkPortSpec_builder{
								Name:  "eth0",
								Role:  "fabric",
								Type:  "Ethernet",
								Speed: "10Gbps",
							}.Build(),
						},
						Capabilities: map[string]string{
							"virtualization": "enabled",
							"tpm":            "2.0",
						},
					}.Build(),
					HostLabelSelector: privatev1.BareMetalLabelSelector_builder{
						MatchLabels: map[string]string{
							"hardware.profile": "compute-large",
							"location.rack":    "rack-01",
						},
					}.Build(),
					Description: "Large compute bare metal instance type for integration testing.",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
				Id: name,
			}.Build())
		})
		Expect(response).ToNot(BeNil())
		object := response.GetObject()
		Expect(object).ToNot(BeNil())
		Expect(object.GetId()).To(Equal(name))

		// Verify hardware specs:
		hardware := object.GetSpec().GetHardware()
		Expect(hardware).ToNot(BeNil())
		cpu := hardware.GetCpu()
		Expect(cpu).ToNot(BeNil())
		Expect(cpu.GetCores()).To(Equal(int32(32)))
		Expect(cpu.GetArchitecture()).To(Equal("amd64"))
		Expect(cpu.GetModel()).To(Equal("Intel Xeon Gold"))
		Expect(cpu.GetThreadsPerCore()).To(Equal(int32(2)))

		memory := hardware.GetMemory()
		Expect(memory).ToNot(BeNil())
		Expect(memory.GetTotalGb()).To(Equal(int64(128)))
		Expect(memory.GetType()).To(Equal("DDR4"))

		disks := hardware.GetDisks()
		Expect(disks).To(HaveLen(1))
		Expect(disks[0].GetType()).To(Equal("NVMe"))
		Expect(disks[0].GetCapacityGb()).To(Equal(int64(1000)))
		Expect(disks[0].GetInterface()).To(Equal("PCIe"))

		networkPorts := hardware.GetNetworkPorts()
		Expect(networkPorts).To(HaveLen(1))
		Expect(networkPorts[0].GetName()).To(Equal("eth0"))
		Expect(networkPorts[0].GetRole()).To(Equal("fabric"))
		Expect(networkPorts[0].GetType()).To(Equal("Ethernet"))
		Expect(networkPorts[0].GetSpeed()).To(Equal("10Gbps"))

		capabilities := hardware.GetCapabilities()
		Expect(capabilities).To(HaveKeyWithValue("virtualization", "enabled"))
		Expect(capabilities).To(HaveKeyWithValue("tpm", "2.0"))

		// Verify host label selector:
		hostLabels := object.GetSpec().GetHostLabelSelector().GetMatchLabels()
		Expect(hostLabels).To(HaveKeyWithValue("hardware.profile", "compute-large"))
		Expect(hostLabels).To(HaveKeyWithValue("location.rack", "rack-01"))

		// Verify metadata:
		metadata := object.GetMetadata()
		Expect(metadata).ToNot(BeNil())
		Expect(metadata.HasCreationTimestamp()).To(BeTrue())
		Expect(metadata.HasDeletionTimestamp()).To(BeFalse())
	})

	It("Can list bare metal instance types", func() {
		// Create 3 bare metal instance types with unique names:
		for i := range 3 {
			name := fmt.Sprintf("it-bm-list-%d-%s", i, uuid.New())
			_, err := client.Create(ctx, privatev1.BareMetalInstanceTypesCreateRequest_builder{
				Object: privatev1.BareMetalInstanceType_builder{
					Metadata: privatev1.Metadata_builder{
						Name: name,
					}.Build(),
					Spec: privatev1.BareMetalInstanceTypeSpec_builder{
						Hardware: privatev1.BareMetalHardwareSpec_builder{
							Cpu: privatev1.BareMetalCPUSpec_builder{
								Cores:          16,
								Architecture:   "amd64",
								ThreadsPerCore: 2,
							}.Build(),
							Memory: privatev1.BareMetalMemorySpec_builder{
								TotalGb: 64,
							}.Build(),
							NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{
								privatev1.BareMetalNetworkPortSpec_builder{
									Name:  "data-0",
									Role:  "fabric",
									Type:  "Ethernet",
									Speed: "25Gbps",
								}.Build(),
							},
						}.Build(),
						HostLabelSelector: privatev1.BareMetalLabelSelector_builder{
							MatchLabels: map[string]string{
								"test.group": "list-test",
							},
						}.Build(),
						Description: fmt.Sprintf("List test bare metal type %d.", i),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() {
				_, _ = client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
					Id: name,
				}.Build())
			})
		}

		// List all bare metal instance types:
		response, err := client.List(ctx, privatev1.BareMetalInstanceTypesListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		items := response.GetItems()
		Expect(len(items)).To(BeNumerically(">=", 3))
	})

	It("Can get a bare metal instance type", func() {
		name := fmt.Sprintf("it-bm-get-%s", uuid.New())
		createResponse, err := client.Create(ctx, privatev1.BareMetalInstanceTypesCreateRequest_builder{
			Object: privatev1.BareMetalInstanceType_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: privatev1.BareMetalInstanceTypeSpec_builder{
					Hardware: privatev1.BareMetalHardwareSpec_builder{
						Cpu: privatev1.BareMetalCPUSpec_builder{
							Cores:          24,
							Architecture:   "amd64",
							ThreadsPerCore: 2,
						}.Build(),
						Memory: privatev1.BareMetalMemorySpec_builder{
							TotalGb: 96,
						}.Build(),
						Accelerators: []*privatev1.BareMetalAcceleratorSpec{
							privatev1.BareMetalAcceleratorSpec_builder{
								Type:     "GPU",
								Model:    "A100",
								Vendor:   stringPtr("NVIDIA"),
								MemoryGb: int32Ptr(40),
							}.Build(),
						},
						NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{
							privatev1.BareMetalNetworkPortSpec_builder{
								Name:  "data-0",
								Role:  "fabric",
								Type:  "Ethernet",
								Speed: "25Gbps",
							}.Build(),
						},
					}.Build(),
					HostLabelSelector: privatev1.BareMetalLabelSelector_builder{
						MatchLabels: map[string]string{
							"gpu.type": "nvidia-a100",
						},
					}.Build(),
					Description: "Get test GPU bare metal type.",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
				Id: name,
			}.Build())
		})

		// Get the bare metal instance type and verify fields match:
		getResponse, err := client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{
			Id: name,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResponse).ToNot(BeNil())
		object := getResponse.GetObject()
		Expect(object).ToNot(BeNil())
		Expect(object.GetId()).To(Equal(name))
		Expect(object.GetSpec().GetHardware().GetCpu().GetCores()).To(Equal(createResponse.GetObject().GetSpec().GetHardware().GetCpu().GetCores()))
		Expect(object.GetSpec().GetHardware().GetMemory().GetTotalGb()).To(Equal(createResponse.GetObject().GetSpec().GetHardware().GetMemory().GetTotalGb()))
		Expect(object.GetSpec().GetDescription()).To(Equal("Get test GPU bare metal type."))

		// Verify accelerator is preserved:
		accelerators := object.GetSpec().GetHardware().GetAccelerators()
		Expect(accelerators).To(HaveLen(1))
		Expect(accelerators[0].GetType()).To(Equal("GPU"))
		Expect(accelerators[0].GetModel()).To(Equal("A100"))
		Expect(accelerators[0].GetVendor()).To(Equal("NVIDIA"))
		Expect(accelerators[0].GetMemoryGb()).To(Equal(int32(40)))
	})

	It("Can update a bare metal instance type", func() {
		name := fmt.Sprintf("it-bm-update-%s", uuid.New())
		_, err := client.Create(ctx, privatev1.BareMetalInstanceTypesCreateRequest_builder{
			Object: privatev1.BareMetalInstanceType_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: privatev1.BareMetalInstanceTypeSpec_builder{
					Hardware: privatev1.BareMetalHardwareSpec_builder{
						Cpu: privatev1.BareMetalCPUSpec_builder{
							Cores:          16,
							Architecture:   "amd64",
							ThreadsPerCore: 2,
						}.Build(),
						Memory: privatev1.BareMetalMemorySpec_builder{
							TotalGb: 64,
						}.Build(),
						NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{
							privatev1.BareMetalNetworkPortSpec_builder{
								Name:  "data-0",
								Role:  "fabric",
								Type:  "Ethernet",
								Speed: "25Gbps",
							}.Build(),
						},
					}.Build(),
					HostLabelSelector: privatev1.BareMetalLabelSelector_builder{
						MatchLabels: map[string]string{
							"profile": "standard",
						},
					}.Build(),
					Description: "Original description.",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
				Id: name,
			}.Build())
		})

		// Get the existing object:
		getResponse, err := client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{
			Id: name,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		existingObject := getResponse.GetObject()

		// Update the description:
		updatedObject := privatev1.BareMetalInstanceType_builder{
			Id:       existingObject.GetId(),
			Metadata: existingObject.GetMetadata(),
			Spec: privatev1.BareMetalInstanceTypeSpec_builder{
				Hardware:          existingObject.GetSpec().GetHardware(),
				HostLabelSelector: existingObject.GetSpec().GetHostLabelSelector(),
				Description:       "Updated description.",
			}.Build(),
			Status: existingObject.GetStatus(),
		}.Build()

		updateResponse, err := client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{
			Object:     updatedObject,
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.description"}},
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResponse).ToNot(BeNil())
		Expect(updateResponse.GetObject().GetSpec().GetDescription()).To(Equal("Updated description."))

		// Get and verify persisted:
		getResponse, err = client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{
			Id: name,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResponse.GetObject().GetSpec().GetDescription()).To(Equal("Updated description."))

		// Verify core hardware specs remain unchanged:
		Expect(getResponse.GetObject().GetSpec().GetHardware().GetCpu().GetCores()).To(Equal(int32(16)))
		Expect(getResponse.GetObject().GetSpec().GetHardware().GetMemory().GetTotalGb()).To(Equal(int64(64)))
	})

	It("Can update host label selector", func() {
		name := fmt.Sprintf("it-bm-update-labels-%s", uuid.New())
		_, err := client.Create(ctx, privatev1.BareMetalInstanceTypesCreateRequest_builder{
			Object: privatev1.BareMetalInstanceType_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: privatev1.BareMetalInstanceTypeSpec_builder{
					Hardware: privatev1.BareMetalHardwareSpec_builder{
						Cpu: privatev1.BareMetalCPUSpec_builder{
							Cores:          8,
							Architecture:   "amd64",
							ThreadsPerCore: 2,
						}.Build(),
						Memory: privatev1.BareMetalMemorySpec_builder{
							TotalGb: 32,
						}.Build(),
						NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{
							privatev1.BareMetalNetworkPortSpec_builder{
								Name:  "data-0",
								Role:  "fabric",
								Type:  "Ethernet",
								Speed: "25Gbps",
							}.Build(),
						},
					}.Build(),
					HostLabelSelector: privatev1.BareMetalLabelSelector_builder{
						MatchLabels: map[string]string{
							"original": "selector",
						},
					}.Build(),
					Description: "Label selector update test.",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
				Id: name,
			}.Build())
		})

		// Get the existing object:
		getResponse, err := client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{
			Id: name,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		existingObject := getResponse.GetObject()

		// Update the host label selector:
		updatedObject := privatev1.BareMetalInstanceType_builder{
			Id:       existingObject.GetId(),
			Metadata: existingObject.GetMetadata(),
			Spec: privatev1.BareMetalInstanceTypeSpec_builder{
				Hardware: existingObject.GetSpec().GetHardware(),
				HostLabelSelector: privatev1.BareMetalLabelSelector_builder{
					MatchLabels: map[string]string{
						"updated": "selector",
						"zone":    "west-1",
					},
				}.Build(),
				Description: existingObject.GetSpec().GetDescription(),
			}.Build(),
			Status: existingObject.GetStatus(),
		}.Build()

		_, err = client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{
			Object:     updatedObject,
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.host_label_selector"}},
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Get and verify updated labels:
		getResponse, err = client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{
			Id: name,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		hostLabels := getResponse.GetObject().GetSpec().GetHostLabelSelector().GetMatchLabels()
		Expect(hostLabels).To(HaveKeyWithValue("updated", "selector"))
		Expect(hostLabels).To(HaveKeyWithValue("zone", "west-1"))
		Expect(hostLabels).ToNot(HaveKey("original"))
	})

	It("Can delete a bare metal instance type", func() {
		name := fmt.Sprintf("it-bm-delete-%s", uuid.New())
		_, err := client.Create(ctx, privatev1.BareMetalInstanceTypesCreateRequest_builder{
			Object: privatev1.BareMetalInstanceType_builder{
				Metadata: privatev1.Metadata_builder{
					Name:       name,
					Finalizers: []string{"test-finalizer"},
				}.Build(),
				Spec: privatev1.BareMetalInstanceTypeSpec_builder{
					Hardware: privatev1.BareMetalHardwareSpec_builder{
						Cpu: privatev1.BareMetalCPUSpec_builder{
							Cores:          4,
							Architecture:   "amd64",
							ThreadsPerCore: 2,
						}.Build(),
						Memory: privatev1.BareMetalMemorySpec_builder{
							TotalGb: 16,
						}.Build(),
						NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{
							privatev1.BareMetalNetworkPortSpec_builder{
								Name:  "data-0",
								Role:  "fabric",
								Type:  "Ethernet",
								Speed: "25Gbps",
							}.Build(),
						},
					}.Build(),
					HostLabelSelector: privatev1.BareMetalLabelSelector_builder{
						MatchLabels: map[string]string{
							"delete": "me",
						},
					}.Build(),
					Description: "Delete test bare metal type.",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
				Id: name,
			}.Build())
		})

		// Delete it:
		deleteResponse, err := client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
			Id: name,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(deleteResponse).ToNot(BeNil())

		// Get and verify deletion timestamp:
		getResponse, err := client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{
			Id: name,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResponse).ToNot(BeNil())
		object := getResponse.GetObject()
		Expect(object).ToNot(BeNil())
		metadata := object.GetMetadata()
		Expect(metadata).ToNot(BeNil())
		Expect(metadata.HasDeletionTimestamp()).To(BeTrue())
	})

	It("Can signal a bare metal instance type", func() {
		name := fmt.Sprintf("it-bm-signal-%s", uuid.New())
		_, err := client.Create(ctx, privatev1.BareMetalInstanceTypesCreateRequest_builder{
			Object: privatev1.BareMetalInstanceType_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: privatev1.BareMetalInstanceTypeSpec_builder{
					Hardware: privatev1.BareMetalHardwareSpec_builder{
						Cpu: privatev1.BareMetalCPUSpec_builder{
							Cores:          2,
							Architecture:   "amd64",
							ThreadsPerCore: 2,
						}.Build(),
						Memory: privatev1.BareMetalMemorySpec_builder{
							TotalGb: 8,
						}.Build(),
						NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{
							privatev1.BareMetalNetworkPortSpec_builder{
								Name:  "data-0",
								Role:  "fabric",
								Type:  "Ethernet",
								Speed: "25Gbps",
							}.Build(),
						},
					}.Build(),
					HostLabelSelector: privatev1.BareMetalLabelSelector_builder{
						MatchLabels: map[string]string{
							"signal": "test",
						},
					}.Build(),
					Description: "Signal test bare metal type.",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
				Id: name,
			}.Build())
		})

		// Signal it:
		signalResponse, err := client.Signal(ctx, privatev1.BareMetalInstanceTypesSignalRequest_builder{
			Id: name,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(signalResponse).ToNot(BeNil())
	})

	// Error scenarios

	It("Rejects update of immutable CPU cores", func() {
		name := fmt.Sprintf("it-bm-immut-cores-%s", uuid.New())
		_, err := client.Create(ctx, privatev1.BareMetalInstanceTypesCreateRequest_builder{
			Object: privatev1.BareMetalInstanceType_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: privatev1.BareMetalInstanceTypeSpec_builder{
					Hardware: privatev1.BareMetalHardwareSpec_builder{
						Cpu: privatev1.BareMetalCPUSpec_builder{
							Cores:          16,
							Architecture:   "amd64",
							ThreadsPerCore: 2,
						}.Build(),
						Memory: privatev1.BareMetalMemorySpec_builder{
							TotalGb: 64,
						}.Build(),
						NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{
							privatev1.BareMetalNetworkPortSpec_builder{
								Name:  "data-0",
								Role:  "fabric",
								Type:  "Ethernet",
								Speed: "25Gbps",
							}.Build(),
						},
					}.Build(),
					HostLabelSelector: privatev1.BareMetalLabelSelector_builder{
						MatchLabels: map[string]string{
							"profile": "test",
						},
					}.Build(),
					Description: "CPU cores immutability test.",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
				Id: name,
			}.Build())
		})

		// Get the existing object:
		getResponse, err := client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{
			Id: name,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		existingObject := getResponse.GetObject()

		// Attempt to change CPU cores:
		updatedHardware := privatev1.BareMetalHardwareSpec_builder{
			Cpu: privatev1.BareMetalCPUSpec_builder{
				Cores:          32, // Different value - should be rejected as immutable
				Architecture:   existingObject.GetSpec().GetHardware().GetCpu().GetArchitecture(),
				ThreadsPerCore: existingObject.GetSpec().GetHardware().GetCpu().GetThreadsPerCore(),
			}.Build(),
			Memory:       existingObject.GetSpec().GetHardware().GetMemory(),
			NetworkPorts: existingObject.GetSpec().GetHardware().GetNetworkPorts(),
		}.Build()

		updatedObject := privatev1.BareMetalInstanceType_builder{
			Id:       existingObject.GetId(),
			Metadata: existingObject.GetMetadata(),
			Spec: privatev1.BareMetalInstanceTypeSpec_builder{
				Hardware:          updatedHardware,
				HostLabelSelector: existingObject.GetSpec().GetHostLabelSelector(),
				Description:       existingObject.GetSpec().GetDescription(),
			}.Build(),
			Status: existingObject.GetStatus(),
		}.Build()

		_, err = client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{
			Object: updatedObject,
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(ContainSubstring("spec.hardware"))
		Expect(status.Message()).To(ContainSubstring("immutable"))
	})

	It("Rejects update of immutable CPU architecture", func() {
		name := fmt.Sprintf("it-bm-immut-arch-%s", uuid.New())
		_, err := client.Create(ctx, privatev1.BareMetalInstanceTypesCreateRequest_builder{
			Object: privatev1.BareMetalInstanceType_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: privatev1.BareMetalInstanceTypeSpec_builder{
					Hardware: privatev1.BareMetalHardwareSpec_builder{
						Cpu: privatev1.BareMetalCPUSpec_builder{
							Cores:          16,
							Architecture:   "amd64",
							ThreadsPerCore: 2,
						}.Build(),
						Memory: privatev1.BareMetalMemorySpec_builder{
							TotalGb: 64,
						}.Build(),
						NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{
							privatev1.BareMetalNetworkPortSpec_builder{
								Name:  "data-0",
								Role:  "fabric",
								Type:  "Ethernet",
								Speed: "25Gbps",
							}.Build(),
						},
					}.Build(),
					HostLabelSelector: privatev1.BareMetalLabelSelector_builder{
						MatchLabels: map[string]string{
							"profile": "test",
						},
					}.Build(),
					Description: "CPU architecture immutability test.",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
				Id: name,
			}.Build())
		})

		// Get the existing object:
		getResponse, err := client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{
			Id: name,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		existingObject := getResponse.GetObject()

		// Attempt to change CPU architecture:
		updatedHardware := privatev1.BareMetalHardwareSpec_builder{
			Cpu: privatev1.BareMetalCPUSpec_builder{
				Cores:          existingObject.GetSpec().GetHardware().GetCpu().GetCores(),
				Architecture:   "arm64", // Different value - should be rejected as immutable
				ThreadsPerCore: existingObject.GetSpec().GetHardware().GetCpu().GetThreadsPerCore(),
			}.Build(),
			Memory:       existingObject.GetSpec().GetHardware().GetMemory(),
			NetworkPorts: existingObject.GetSpec().GetHardware().GetNetworkPorts(),
		}.Build()

		updatedObject := privatev1.BareMetalInstanceType_builder{
			Id:       existingObject.GetId(),
			Metadata: existingObject.GetMetadata(),
			Spec: privatev1.BareMetalInstanceTypeSpec_builder{
				Hardware:          updatedHardware,
				HostLabelSelector: existingObject.GetSpec().GetHostLabelSelector(),
				Description:       existingObject.GetSpec().GetDescription(),
			}.Build(),
			Status: existingObject.GetStatus(),
		}.Build()

		_, err = client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{
			Object: updatedObject,
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(ContainSubstring("spec.hardware"))
		Expect(status.Message()).To(ContainSubstring("immutable"))
	})

	It("Rejects update of immutable memory total", func() {
		name := fmt.Sprintf("it-bm-immut-mem-%s", uuid.New())
		_, err := client.Create(ctx, privatev1.BareMetalInstanceTypesCreateRequest_builder{
			Object: privatev1.BareMetalInstanceType_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: privatev1.BareMetalInstanceTypeSpec_builder{
					Hardware: privatev1.BareMetalHardwareSpec_builder{
						Cpu: privatev1.BareMetalCPUSpec_builder{
							Cores:          16,
							Architecture:   "amd64",
							ThreadsPerCore: 2,
						}.Build(),
						Memory: privatev1.BareMetalMemorySpec_builder{
							TotalGb: 64,
						}.Build(),
						NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{
							privatev1.BareMetalNetworkPortSpec_builder{
								Name:  "data-0",
								Role:  "fabric",
								Type:  "Ethernet",
								Speed: "25Gbps",
							}.Build(),
						},
					}.Build(),
					HostLabelSelector: privatev1.BareMetalLabelSelector_builder{
						MatchLabels: map[string]string{
							"profile": "test",
						},
					}.Build(),
					Description: "Memory total immutability test.",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
				Id: name,
			}.Build())
		})

		// Get the existing object:
		getResponse, err := client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{
			Id: name,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		existingObject := getResponse.GetObject()

		// Attempt to change memory total:
		updatedHardware := privatev1.BareMetalHardwareSpec_builder{
			Cpu: existingObject.GetSpec().GetHardware().GetCpu(),
			Memory: privatev1.BareMetalMemorySpec_builder{
				TotalGb: 128, // Different value - should be rejected as immutable
			}.Build(),
			NetworkPorts: existingObject.GetSpec().GetHardware().GetNetworkPorts(),
		}.Build()

		updatedObject := privatev1.BareMetalInstanceType_builder{
			Id:       existingObject.GetId(),
			Metadata: existingObject.GetMetadata(),
			Spec: privatev1.BareMetalInstanceTypeSpec_builder{
				Hardware:          updatedHardware,
				HostLabelSelector: existingObject.GetSpec().GetHostLabelSelector(),
				Description:       existingObject.GetSpec().GetDescription(),
			}.Build(),
			Status: existingObject.GetStatus(),
		}.Build()

		_, err = client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{
			Object: updatedObject,
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(ContainSubstring("spec.hardware"))
		Expect(status.Message()).To(ContainSubstring("immutable"))
	})

	It("Can delete an unused bare metal instance type", func() {
		name := fmt.Sprintf("it-bm-delprotect-%s", uuid.New())
		_, err := client.Create(ctx, privatev1.BareMetalInstanceTypesCreateRequest_builder{
			Object: privatev1.BareMetalInstanceType_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: privatev1.BareMetalInstanceTypeSpec_builder{
					Hardware: privatev1.BareMetalHardwareSpec_builder{
						Cpu: privatev1.BareMetalCPUSpec_builder{
							Cores:          8,
							Architecture:   "amd64",
							ThreadsPerCore: 2,
						}.Build(),
						Memory: privatev1.BareMetalMemorySpec_builder{
							TotalGb: 32,
						}.Build(),
						NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{
							privatev1.BareMetalNetworkPortSpec_builder{
								Name:  "data-0",
								Role:  "fabric",
								Type:  "Ethernet",
								Speed: "25Gbps",
							}.Build(),
						},
					}.Build(),
					HostLabelSelector: privatev1.BareMetalLabelSelector_builder{
						MatchLabels: map[string]string{
							"deletion.test": "allowed",
						},
					}.Build(),
					Description: "Deletion protection test bare metal type.",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
				Id: name,
			}.Build())
		})

		// Deletion succeeds because no instances reference this type:
		_, err = client.Delete(ctx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{
			Id: name,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
	})

	It("Returns not found for non-existent bare metal instance type", func() {
		nonExistentID := fmt.Sprintf("non-existent-%s", uuid.New())

		_, err := client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{
			Id: nonExistentID,
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.NotFound))
	})
})

// Helper functions for optional fields
func stringPtr(s string) *string {
	return &s
}

func int32Ptr(i int32) *int32 {
	return &i
}

var _ = Describe("Private bare metal type architecture", func() {
	var ctx context.Context
	var client privatev1.BareMetalInstanceTypesClient
	architectureMask := &fieldmaskpb.FieldMask{Paths: []string{"spec.hardware.cpu.architecture"}}
	invalid := []string{"x86_64", "aarch64", "AMD64", "ARM64", "Amd64", " amd64", "amd64 ", "", "ppc64le"}

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewBareMetalInstanceTypesClient(tool.InternalView().AdminConn())
	})
	get := func(id string) *privatev1.BareMetalInstanceType {
		GinkgoHelper()
		response, err := client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{Id: id}.Build())
		Expect(err).ToNot(HaveOccurred())
		return response.GetObject()
	}
	assertUnchanged := func(original *privatev1.BareMetalInstanceType) {
		GinkgoHelper()
		Expect(proto.Equal(get(original.GetId()), original)).To(BeTrue())
	}

	It("accepts exact canonical Create inputs and validates dry-run without persistence", func() {
		for _, architecture := range append([]string{"amd64", "arm64", "s390x"}, invalid...) {
			for _, dryRun := range []bool{false, true} {
				object := architectureTypeInput()
				object.GetSpec().GetHardware().GetCpu().SetArchitecture(architecture)
				id := object.GetMetadata().GetName()
				operationCtx := ctx
				if dryRun {
					operationCtx = metadata.AppendToOutgoingContext(ctx, "x-dry-run", "true")
				}
				response, err := client.Create(operationCtx, privatev1.BareMetalInstanceTypesCreateRequest_builder{Object: object}.Build())
				canonical := architecture == "amd64" || architecture == "arm64" || architecture == "s390x"
				if canonical {
					Expect(err).ToNot(HaveOccurred())
					Expect(response.GetObject().GetId()).To(Equal(id))
					Expect(response.GetObject().GetSpec().GetHardware().GetCpu().GetArchitecture()).To(Equal(architecture))
				} else {
					Expect(grpcstatus.Code(err)).To(Equal(grpccodes.InvalidArgument))
					Expect(err.Error()).To(ContainSubstring("architecture"))
					for _, accepted := range []string{"amd64", "arm64", "s390x"} {
						Expect(err.Error()).To(ContainSubstring(accepted))
					}
				}
				if canonical && !dryRun {
					DeferCleanup(func() {
						_, err := client.Delete(context.Background(), privatev1.BareMetalInstanceTypesDeleteRequest_builder{Id: id}.Build())
						Expect(err).ToNot(HaveOccurred())
					})
					Expect(get(id).GetSpec().GetHardware().GetCpu().GetArchitecture()).To(Equal(architecture))
				} else {
					_, err = client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{Id: id}.Build())
					Expect(grpcstatus.Code(err)).To(Equal(grpccodes.NotFound))
					filter := fmt.Sprintf("this.id == '%s'", id)
					listed, err := client.List(ctx, privatev1.BareMetalInstanceTypesListRequest_builder{Filter: &filter}.Build())
					Expect(err).ToNot(HaveOccurred())
					Expect(listed.GetItems()).To(BeEmpty())
				}
			}
		}
	})

	It("corrects only legacy architecture through nested, ancestor and full updates", func() {
		for _, architecture := range []string{"amd64", "arm64", "s390x"} {
			for _, path := range []string{"spec.hardware.cpu.architecture", "spec.hardware.cpu", "spec.hardware", "spec", ""} {
				original := createLegacyArchitectureType(ctx, client, "x86_64")
				candidate := proto.Clone(original).(*privatev1.BareMetalInstanceType)
				candidate.GetSpec().GetHardware().GetCpu().SetArchitecture(architecture)
				var mask *fieldmaskpb.FieldMask
				if path != "" {
					mask = &fieldmaskpb.FieldMask{Paths: []string{path}}
				} else {
					candidate.SetMetadata(privatev1.Metadata_builder{Version: original.GetMetadata().GetVersion()}.Build())
				}
				_, err := client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{Object: candidate, UpdateMask: mask, Lock: true}.Build())
				Expect(err).ToNot(HaveOccurred())
				stored := get(original.GetId())
				Expect(stored.GetId()).To(Equal(original.GetId()))
				Expect(proto.Equal(stored.GetSpec(), candidate.GetSpec())).To(BeTrue())
				expectedMetadata := proto.Clone(original.GetMetadata()).(*privatev1.Metadata)
				expectedMetadata.SetVersion(stored.GetMetadata().GetVersion())
				Expect(proto.Equal(stored.GetMetadata(), expectedMetadata)).To(BeTrue())
			}
		}
	})

	It("preserves legacy storage after invalid replacements, excluded corrections and immutable edits", func() {
		original := createLegacyArchitectureType(ctx, client, "aarch64")
		for _, architecture := range invalid {
			for _, path := range []string{"spec.hardware.cpu.architecture", "spec.hardware.cpu", "spec.hardware", "spec", ""} {
				candidate := proto.Clone(original).(*privatev1.BareMetalInstanceType)
				candidate.GetSpec().GetHardware().GetCpu().SetArchitecture(architecture)
				var mask *fieldmaskpb.FieldMask
				if path != "" {
					mask = &fieldmaskpb.FieldMask{Paths: []string{path}}
				}
				_, err := client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{Object: candidate, UpdateMask: mask, Lock: true}.Build())
				Expect(grpcstatus.Code(err)).To(Equal(grpccodes.InvalidArgument))
				for _, accepted := range []string{"amd64", "arm64", "s390x"} {
					Expect(err.Error()).To(ContainSubstring(accepted))
				}
				assertUnchanged(original)
			}
		}
		for _, mask := range []*fieldmaskpb.FieldMask{{}, {Paths: []string{"metadata.labels"}}, {Paths: []string{"spec.description"}}} {
			candidate := proto.Clone(original).(*privatev1.BareMetalInstanceType)
			candidate.GetSpec().GetHardware().GetCpu().SetArchitecture("arm64")
			_, err := client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{Object: candidate, UpdateMask: mask}.Build())
			Expect(grpcstatus.Code(err)).To(Equal(grpccodes.InvalidArgument))
			assertUnchanged(original)
		}
		for _, field := range []string{"cores", "disks", "capabilities", "name", "tenant", "project"} {
			for _, full := range []bool{false, true} {
				candidate := proto.Clone(original).(*privatev1.BareMetalInstanceType)
				candidate.GetSpec().GetHardware().GetCpu().SetArchitecture("arm64")
				path := "spec.hardware"
				switch field {
				case "cores":
					candidate.GetSpec().GetHardware().GetCpu().SetCores(16)
				case "disks":
					candidate.GetSpec().GetHardware().GetDisks()[0].SetCapacityGb(1024)
				case "capabilities":
					candidate.GetSpec().GetHardware().SetCapabilities(map[string]string{"boot": "different"})
				case "name":
					candidate.GetMetadata().SetName("different-name")
					path = "metadata.name"
				case "tenant":
					candidate.GetMetadata().SetTenant("different-tenant")
					path = "metadata.tenant"
				case "project":
					candidate.GetMetadata().SetProject("different-project")
					path = "metadata.project"
				}
				mask := &fieldmaskpb.FieldMask{Paths: []string{"spec.hardware.cpu.architecture", path}}
				if full {
					mask = nil
				}
				_, err := client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{Object: candidate, UpdateMask: mask, Lock: true}.Build())
				Expect(err).To(HaveOccurred())
				assertUnchanged(original)
			}
		}
	})

	It("retains provider authority, original client versions and canonical immutability", func() {
		original := createLegacyArchitectureType(ctx, client, "x86_64")
		candidate := proto.Clone(original).(*privatev1.BareMetalInstanceType)
		candidate.GetSpec().GetHardware().GetCpu().SetArchitecture("arm64")
		tenantClient := privatev1.NewBareMetalInstanceTypesClient(tool.InternalView().UserConn())
		_, err := tenantClient.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{Object: candidate, UpdateMask: architectureMask, Lock: true}.Build())
		Expect(grpcstatus.Code(err)).To(Equal(grpccodes.PermissionDenied))
		assertUnchanged(original)
		_, err = client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{Object: candidate, UpdateMask: architectureMask, Lock: true}.Build())
		Expect(err).ToNot(HaveOccurred())
		advanced := get(original.GetId())
		candidate.GetSpec().GetHardware().GetCpu().SetArchitecture("amd64")
		_, err = client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{Object: candidate, UpdateMask: architectureMask, Lock: true}.Build())
		Expect(grpcstatus.Code(err)).To(Equal(grpccodes.Aborted))
		assertUnchanged(advanced)
		candidate = proto.Clone(advanced).(*privatev1.BareMetalInstanceType)
		candidate.GetSpec().GetHardware().GetCpu().SetArchitecture("s390x")
		_, err = client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{Object: candidate, UpdateMask: architectureMask, Lock: true}.Build())
		Expect(grpcstatus.Code(err)).To(Equal(grpccodes.InvalidArgument))
		Expect(err.Error()).To(ContainSubstring("immutable"))
		assertUnchanged(advanced)
		candidate.SetMetadata(nil)
		_, err = client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{Object: candidate, UpdateMask: architectureMask, Lock: true}.Build())
		Expect(grpcstatus.Code(err)).To(Equal(grpccodes.Aborted))
		assertUnchanged(advanced)
		_, err = client.Update(ctx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{Object: advanced, UpdateMask: architectureMask, Lock: true}.Build())
		Expect(err).ToNot(HaveOccurred())
		assertUnchanged(advanced)
	})
})
