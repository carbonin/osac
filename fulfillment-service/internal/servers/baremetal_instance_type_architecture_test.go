/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.
*/

package servers

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/osac-project/osac/fulfillment-service/internal/auth"
	"github.com/osac-project/osac/fulfillment-service/internal/collections"
	"github.com/osac-project/osac/fulfillment-service/internal/database"
	"github.com/osac-project/osac/fulfillment-service/internal/database/dao"
	"github.com/osac-project/osac/fulfillment-service/internal/validation"
	privatev1 "github.com/osac-project/osac/proto/gen/osac/private/v1"
	publicv1 "github.com/osac-project/osac/proto/gen/osac/public/v1"
)

func architectureTypeFixture(architecture string) *privatev1.BareMetalInstanceType {
	return privatev1.BareMetalInstanceType_builder{
		Id: "architecture-type",
		Metadata: privatev1.Metadata_builder{
			Name: "architecture-type", Tenant: auth.SharedTenant, Creator: "system",
			Labels:      map[string]string{"profile": "compute"},
			Annotations: map[string]string{"example.test/note": "preserve"},
		}.Build(),
		Spec: privatev1.BareMetalInstanceTypeSpec_builder{
			Description:       "Catalog architecture fixture",
			HostLabelSelector: privatev1.BareMetalLabelSelector_builder{MatchLabels: map[string]string{"profile": "compute"}}.Build(),
			Hardware: privatev1.BareMetalHardwareSpec_builder{
				Cpu:          privatev1.BareMetalCPUSpec_builder{Cores: 8, ThreadsPerCore: 2, Architecture: architecture, Model: "fixture-cpu"}.Build(),
				Memory:       privatev1.BareMetalMemorySpec_builder{TotalGb: 32, Type: "DDR5"}.Build(),
				Disks:        []*privatev1.BareMetalDiskSpec{privatev1.BareMetalDiskSpec_builder{Type: "SSD", CapacityGb: 512, Interface: "SATA"}.Build()},
				Accelerators: []*privatev1.BareMetalAcceleratorSpec{privatev1.BareMetalAcceleratorSpec_builder{Type: "GPU", Model: "fixture-gpu"}.Build()},
				NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{privatev1.BareMetalNetworkPortSpec_builder{Name: "data-0", Role: "fabric", Type: "Ethernet", Speed: "25Gbps"}.Build()},
				Capabilities: map[string]string{"boot": "uefi"},
			}.Build(),
		}.Build(),
	}.Build()
}

var _ = Describe("Bare metal type architecture", func() {
	var privateServer *PrivateBareMetalInstanceTypesServer
	var publicServer *BareMetalInstanceTypesServer
	var inputValidator *validation.ProtovalidateInterceptor

	BeforeEach(func() {
		Expect(suiteTx.End(ctx)).To(Succeed())
		suiteTx = nil
		ctx = database.TxManagerIntoContext(context.Background(), tm)
		var err error
		privateServer, err = NewPrivateBareMetalInstanceTypesServer().SetLogger(logger).
			SetAttributionLogic(attribution).SetTenancyLogic(tenancy).Build()
		Expect(err).ToNot(HaveOccurred())
		inputValidator, err = validation.NewProtovalidateInterceptor().SetLogger(logger).Build()
		Expect(err).ToNot(HaveOccurred())
		publicServer, err = NewBareMetalInstanceTypesServer().SetLogger(logger).
			SetAttributionLogic(attribution).SetTenancyLogic(tenancy).Build()
		Expect(err).ToNot(HaveOccurred())
	})

	seed := func(architecture string) *privatev1.BareMetalInstanceType {
		GinkgoHelper()
		object, err := database.WithNewTx(ctx, func(txCtx context.Context) (*privatev1.BareMetalInstanceType, error) {
			types, err := dao.NewGenericDAO[*privatev1.BareMetalInstanceType]().SetLogger(logger).SetTenancyLogic(tenancy).Build()
			if err != nil {
				return nil, err
			}
			response, err := types.Create().SetObject(architectureTypeFixture(architecture)).Do(txCtx)
			if err != nil {
				return nil, err
			}
			return response.GetObject(), nil
		})
		Expect(err).ToNot(HaveOccurred())
		return object
	}
	get := func() *privatev1.BareMetalInstanceType {
		GinkgoHelper()
		response, err := database.WithNewTx(ctx, func(txCtx context.Context) (*privatev1.BareMetalInstanceTypesGetResponse, error) {
			return privateServer.Get(txCtx, privatev1.BareMetalInstanceTypesGetRequest_builder{Id: "architecture-type"}.Build())
		})
		Expect(err).ToNot(HaveOccurred())
		return response.GetObject()
	}
	update := func(object *privatev1.BareMetalInstanceType, mask *fieldmaskpb.FieldMask, lock bool) (*privatev1.BareMetalInstanceType, error) {
		response, err := database.WithNewTx(ctx, func(txCtx context.Context) (*privatev1.BareMetalInstanceTypesUpdateResponse, error) {
			return privateServer.Update(txCtx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{Object: object, UpdateMask: mask, Lock: lock}.Build())
		})
		if err != nil {
			return nil, err
		}
		return response.GetObject(), nil
	}
	correction := func(object *privatev1.BareMetalInstanceType, architecture string) *privatev1.BareMetalInstanceType {
		candidate := proto.Clone(object).(*privatev1.BareMetalInstanceType)
		candidate.GetSpec().GetHardware().GetCpu().SetArchitecture(architecture)
		return candidate
	}
	architectureMask := &fieldmaskpb.FieldMask{Paths: []string{"spec.hardware.cpu.architecture"}}
	invalid := []string{"x86_64", "aarch64", "AMD64", "ARM64", "Amd64", " amd64", "amd64 ", "", "ppc64le"}

	It("creates each canonical architecture and rejects invalid writes without persistence", func() {
		for i, architecture := range append([]string{"amd64", "arm64", "s390x"}, invalid...) {
			for _, dryRun := range []bool{false, true} {
				object := architectureTypeFixture(architecture)
				name := fmt.Sprintf("architecture-%d-%t", i, dryRun)
				object.SetId(name)
				object.GetMetadata().SetName(name)
				operationCtx := ctx
				if dryRun {
					operationCtx = metadata.NewIncomingContext(operationCtx, metadata.Pairs(DryRunMetadataKey, "true"))
				}
				response, err := database.WithNewTx(operationCtx, func(txCtx context.Context) (*privatev1.BareMetalInstanceTypesCreateResponse, error) {
					request := privatev1.BareMetalInstanceTypesCreateRequest_builder{Object: object}.Build()
					response, err := inputValidator.UnaryServer(txCtx, request, &grpc.UnaryServerInfo{},
						func(handlerCtx context.Context, _ any) (any, error) { return privateServer.Create(handlerCtx, request) })
					if err != nil {
						return nil, err
					}
					return response.(*privatev1.BareMetalInstanceTypesCreateResponse), nil
				})
				if i < 3 {
					Expect(err).ToNot(HaveOccurred())
					Expect(response.GetObject().GetSpec().GetHardware().GetCpu().GetArchitecture()).To(Equal(architecture))
				} else {
					Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
					Expect(err.Error()).To(And(ContainSubstring("architecture"), ContainSubstring("amd64"), ContainSubstring("arm64"), ContainSubstring("s390x")))
				}
				stored, readErr := database.WithNewTx(ctx, func(txCtx context.Context) (*privatev1.BareMetalInstanceTypesGetResponse, error) {
					return privateServer.Get(txCtx, privatev1.BareMetalInstanceTypesGetRequest_builder{Id: name}.Build())
				})
				if i < 3 && !dryRun {
					Expect(readErr).ToNot(HaveOccurred())
					Expect(stored.GetObject().GetId()).To(Equal(name))
				} else {
					Expect(status.Code(readErr)).To(Equal(codes.NotFound))
				}
			}
		}
	})

	DescribeTable("retains legacy strings and identity through public and private reads",
		func(architecture string) {
			original := seed(architecture)
			Expect(proto.Equal(get(), original)).To(BeTrue())
			_, err := database.WithNewTx(ctx, func(txCtx context.Context) (bool, error) {
				privateList, err := privateServer.List(txCtx, privatev1.BareMetalInstanceTypesListRequest_builder{}.Build())
				if err != nil {
					return false, err
				}
				Expect(privateList.GetItems()).To(HaveLen(1))
				Expect(proto.Equal(privateList.GetItems()[0], original)).To(BeTrue())
				publicGet, err := publicServer.Get(txCtx, publicv1.BareMetalInstanceTypesGetRequest_builder{Id: original.GetId()}.Build())
				if err != nil {
					return false, err
				}
				publicList, err := publicServer.List(txCtx, publicv1.BareMetalInstanceTypesListRequest_builder{}.Build())
				if err != nil {
					return false, err
				}
				Expect(publicList.GetItems()).To(HaveLen(1))
				for _, object := range []*publicv1.BareMetalInstanceType{publicGet.GetObject(), publicList.GetItems()[0]} {
					Expect(object.GetId()).To(Equal(original.GetId()))
					Expect(object.GetMetadata().GetName()).To(Equal(original.GetMetadata().GetName()))
					Expect(object.GetSpec().GetHardware().GetCpu().GetArchitecture()).To(Equal(architecture))
					Expect(object.GetSpec().GetHardware().GetCpu().GetCores()).To(Equal(int32(8)))
				}
				return true, nil
			})
			Expect(err).ToNot(HaveOccurred())
		},
		Entry("x86 alias", "x86_64"), Entry("arm alias", "aarch64"), Entry("uppercase", "AMD64"),
		Entry("whitespace", " amd64 "), Entry("empty", ""), Entry("unknown", "ppc64le"),
	)

	DescribeTable("corrects only architecture through nested and ancestor masks or a full update",
		func(mask *fieldmaskpb.FieldMask) {
			for _, architecture := range []string{"amd64", "arm64", "s390x"} {
				if architecture != "amd64" {
					_, err := database.WithNewTx(ctx, func(txCtx context.Context) (*privatev1.BareMetalInstanceTypesDeleteResponse, error) {
						return privateServer.Delete(txCtx, privatev1.BareMetalInstanceTypesDeleteRequest_builder{Id: "architecture-type"}.Build())
					})
					Expect(err).ToNot(HaveOccurred())
				}
				original := seed("x86_64")
				expected := correction(original, architecture)
				response, err := update(expected, mask, true)
				Expect(err).ToNot(HaveOccurred())
				stored := get()
				Expect(proto.Equal(stored.GetSpec(), expected.GetSpec())).To(BeTrue())
				Expect(stored.GetId()).To(Equal(original.GetId()))
				expected.GetMetadata().SetVersion(response.GetMetadata().GetVersion())
				Expect(proto.Equal(stored.GetMetadata(), expected.GetMetadata())).To(BeTrue())
			}
		},
		Entry("architecture", &fieldmaskpb.FieldMask{Paths: []string{"spec.hardware.cpu.architecture"}}),
		Entry("cpu", &fieldmaskpb.FieldMask{Paths: []string{"spec.hardware.cpu"}}),
		Entry("hardware", &fieldmaskpb.FieldMask{Paths: []string{"spec.hardware"}}),
		Entry("spec", &fieldmaskpb.FieldMask{Paths: []string{"spec"}}),
		Entry("full", (*fieldmaskpb.FieldMask)(nil)),
	)

	It("rejects invalid replacements and excluded corrections after mask merging", func() {
		original := seed("x86_64")
		for _, architecture := range invalid {
			_, err := update(correction(original, architecture), architectureMask, false)
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(err.Error()).To(And(ContainSubstring("architecture"), ContainSubstring("amd64"), ContainSubstring("arm64"), ContainSubstring("s390x")))
			Expect(proto.Equal(get(), original)).To(BeTrue())
		}
		for _, mask := range []*fieldmaskpb.FieldMask{
			{}, {Paths: []string{"spec.description"}}, {Paths: []string{"metadata.labels"}},
		} {
			_, err := update(correction(original, "amd64"), mask, false)
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(proto.Equal(get(), original)).To(BeTrue())
		}
		_, err := update(correction(original, "amd64"), &fieldmaskpb.FieldMask{Paths: []string{"spec.unknown"}}, false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unknown"))
		Expect(proto.Equal(get(), original)).To(BeTrue())
	})

	It("rejects uncorrected legacy no-op and metadata writes", func() {
		original := seed("aarch64")
		candidate := proto.Clone(original).(*privatev1.BareMetalInstanceType)
		candidate.GetMetadata().SetLabels(map[string]string{"changed": "true"})
		for _, mask := range []*fieldmaskpb.FieldMask{nil, {}, architectureMask, {Paths: []string{"metadata.labels"}}} {
			_, err := update(candidate, mask, false)
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(proto.Equal(get(), original)).To(BeTrue())
		}
	})

	DescribeTable("rejects correction combined with another hardware change",
		func(change func(*privatev1.BareMetalHardwareSpec)) {
			original := seed("x86_64")
			candidate := correction(original, "amd64")
			change(candidate.GetSpec().GetHardware())
			for _, mask := range []*fieldmaskpb.FieldMask{nil, {Paths: []string{"spec.hardware"}}} {
				_, err := update(candidate, mask, false)
				Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("immutable"))
				Expect(proto.Equal(get(), original)).To(BeTrue())
			}
		},
		Entry("cores", func(h *privatev1.BareMetalHardwareSpec) { h.GetCpu().SetCores(16) }),
		Entry("threads", func(h *privatev1.BareMetalHardwareSpec) { h.GetCpu().SetThreadsPerCore(1) }),
		Entry("cpu model", func(h *privatev1.BareMetalHardwareSpec) { h.GetCpu().SetModel("different") }),
		Entry("memory", func(h *privatev1.BareMetalHardwareSpec) { h.GetMemory().SetTotalGb(64) }),
		Entry("memory type", func(h *privatev1.BareMetalHardwareSpec) { h.GetMemory().SetType("DDR4") }),
		Entry("disks", func(h *privatev1.BareMetalHardwareSpec) { h.GetDisks()[0].SetCapacityGb(1024) }),
		Entry("accelerators", func(h *privatev1.BareMetalHardwareSpec) { h.GetAccelerators()[0].SetModel("different") }),
		Entry("ports", func(h *privatev1.BareMetalHardwareSpec) { h.GetNetworkPorts()[0].SetSpeed("100Gbps") }),
		Entry("capabilities", func(h *privatev1.BareMetalHardwareSpec) { h.SetCapabilities(map[string]string{"boot": "bios"}) }),
	)

	It("retains canonical no-ops and mutable fields but rejects a different canonical value", func() {
		original := seed("amd64")
		for _, mask := range []*fieldmaskpb.FieldMask{nil, {}, architectureMask} {
			response, err := update(original, mask, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(response, original)).To(BeTrue())
			Expect(proto.Equal(get(), original)).To(BeTrue())
		}
		_, err := update(correction(original, "arm64"), architectureMask, true)
		Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
		Expect(err.Error()).To(ContainSubstring("immutable"))
		Expect(proto.Equal(get(), original)).To(BeTrue())
		candidate := proto.Clone(original).(*privatev1.BareMetalInstanceType)
		candidate.GetSpec().SetDescription("changed")
		candidate.GetSpec().GetHostLabelSelector().SetMatchLabels(map[string]string{"profile": "updated"})
		candidate.GetMetadata().SetAnnotations(map[string]string{"example.test/note": "updated"})
		_, err = update(candidate, &fieldmaskpb.FieldMask{Paths: []string{"spec.description", "spec.host_label_selector", "metadata.annotations"}}, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(proto.Equal(get().GetSpec(), candidate.GetSpec())).To(BeTrue())
	})

	It("checks the original client version against independently committed corrections", func() {
		original := seed("x86_64")
		_, err := update(correction(original, "arm64"), architectureMask, true)
		Expect(err).ToNot(HaveOccurred())
		advanced := get()
		_, err = update(correction(original, "amd64"), architectureMask, true)
		Expect(status.Code(err)).To(Equal(codes.Aborted))
		Expect(proto.Equal(get(), advanced)).To(BeTrue())
		_, err = update(advanced, architectureMask, true)
		Expect(err).ToNot(HaveOccurred())
	})

	It("permits a fresh locked correction after a committed version change", func() {
		original := seed("x86_64")
		_, err := database.WithNewTx(ctx, func(txCtx context.Context) (bool, error) {
			tx, err := database.TxFromContext(txCtx)
			if err != nil {
				return false, err
			}
			_, err = tx.Exec(txCtx, "update bare_metal_instance_types set version = version + 1 where id = $1", original.GetId())
			return true, err
		})
		Expect(err).ToNot(HaveOccurred())
		_, err = update(correction(original, "amd64"), architectureMask, true)
		Expect(status.Code(err)).To(Equal(codes.Aborted))
		fresh := get()
		_, err = update(correction(fresh, "s390x"), architectureMask, true)
		Expect(err).ToNot(HaveOccurred())
		Expect(get().GetSpec().GetHardware().GetCpu().GetArchitecture()).To(Equal("s390x"))
	})

	It("checks an omitted client version against an advanced stored version", func() {
		seed("x86_64")
		_, err := database.WithNewTx(ctx, func(txCtx context.Context) (bool, error) {
			tx, err := database.TxFromContext(txCtx)
			if err != nil {
				return false, err
			}
			_, err = tx.Exec(txCtx, "update bare_metal_instance_types set version = version + 1 where id = $1", "architecture-type")
			return true, err
		})
		Expect(err).ToNot(HaveOccurred())
		original := get()
		for _, mask := range []*fieldmaskpb.FieldMask{nil, architectureMask} {
			candidate := correction(original, "amd64")
			candidate.SetMetadata(nil)
			_, err := update(candidate, mask, true)
			Expect(status.Code(err)).To(Equal(codes.Aborted))
			Expect(proto.Equal(get(), original)).To(BeTrue())
		}
	})

	It("preserves omitted full-update metadata and applies explicitly supplied labels", func() {
		original := seed("x86_64")
		candidate := correction(original, "amd64")
		candidate.SetMetadata(privatev1.Metadata_builder{Version: original.GetMetadata().GetVersion(), Labels: map[string]string{"new": "label"}}.Build())
		_, err := update(candidate, nil, true)
		Expect(err).ToNot(HaveOccurred())
		stored := get()
		Expect(stored.GetMetadata().GetName()).To(Equal(original.GetMetadata().GetName()))
		Expect(stored.GetMetadata().GetTenant()).To(Equal(auth.SharedTenant))
		Expect(stored.GetMetadata().GetCreator()).To(Equal(original.GetMetadata().GetCreator()))
		Expect(proto.Equal(stored.GetMetadata().GetCreationTimestamp(), original.GetMetadata().GetCreationTimestamp())).To(BeTrue())
		Expect(stored.GetMetadata().GetLabels()).To(HaveKeyWithValue("new", "label"))
		Expect(stored.GetMetadata().GetAnnotations()).To(Equal(original.GetMetadata().GetAnnotations()))
	})

	It("preserves server metadata when omitted entirely from a full correction", func() {
		original := seed("x86_64")
		candidate := correction(original, "amd64")
		candidate.SetMetadata(nil)
		_, err := update(candidate, nil, false)
		Expect(err).ToNot(HaveOccurred())
		stored := get()
		expected := proto.Clone(original.GetMetadata()).(*privatev1.Metadata)
		expected.SetVersion(stored.GetMetadata().GetVersion())
		Expect(proto.Equal(stored.GetMetadata(), expected)).To(BeTrue())
	})

	DescribeTable("rejects immutable metadata while preserving the committed legacy row",
		func(path string, change func(*privatev1.Metadata)) {
			original := seed("x86_64")
			for _, full := range []bool{false, true} {
				candidate := correction(original, "amd64")
				change(candidate.GetMetadata())
				mask := &fieldmaskpb.FieldMask{Paths: []string{"spec.hardware.cpu.architecture", path}}
				if full {
					mask = nil
				}
				_, err := update(candidate, mask, true)
				Expect(err).To(HaveOccurred())
				Expect(proto.Equal(get(), original)).To(BeTrue())
			}
		},
		Entry("name", "metadata.name", func(m *privatev1.Metadata) { m.SetName("different-name") }),
		Entry("tenant", "metadata.tenant", func(m *privatev1.Metadata) { m.SetTenant(testTenant) }),
		Entry("project", "metadata.project", func(m *privatev1.Metadata) { m.SetProject("different-project") }),
	)

	DescribeTable("rejects invalid metadata on a correction",
		func(path string, change func(*privatev1.Metadata)) {
			original := seed("x86_64")
			candidate := correction(original, "amd64")
			change(candidate.GetMetadata())
			_, err := update(candidate, &fieldmaskpb.FieldMask{Paths: []string{"spec.hardware.cpu.architecture", path}}, true)
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(proto.Equal(get(), original)).To(BeTrue())
		},
		Entry("labels", "metadata.labels", func(m *privatev1.Metadata) { m.SetLabels(map[string]string{"!invalid": "value"}) }),
		Entry("annotations", "metadata.annotations", func(m *privatev1.Metadata) { m.SetAnnotations(map[string]string{"!invalid": "value"}) }),
	)

	It("retains write authority for a caller who can read the shared catalog", func() {
		original := seed("x86_64")
		denied := auth.NewMockTenancyLogic(ctrl)
		denied.EXPECT().DetermineVisibility(gomock.Any()).Return(auth.TotalVisibility(), nil).AnyTimes()
		denied.EXPECT().DetermineAssignableTenants(gomock.Any()).Return(collections.NewSet[string](), nil).AnyTimes()
		deniedServer, err := NewPrivateBareMetalInstanceTypesServer().SetLogger(logger).
			SetAttributionLogic(attribution).SetTenancyLogic(denied).Build()
		Expect(err).ToNot(HaveOccurred())
		_, err = database.WithNewTx(ctx, func(txCtx context.Context) (*privatev1.BareMetalInstanceTypesUpdateResponse, error) {
			return deniedServer.Update(txCtx, privatev1.BareMetalInstanceTypesUpdateRequest_builder{
				Object: correction(original, "amd64"), UpdateMask: architectureMask,
			}.Build())
		})
		Expect(status.Code(err)).To(Equal(codes.PermissionDenied))
		Expect(proto.Equal(get(), original)).To(BeTrue())
	})

	It("rolls back a correction when the database rejects the write", func() {
		original := seed("x86_64")
		_, err := database.WithNewTx(ctx, func(txCtx context.Context) (bool, error) {
			tx, err := database.TxFromContext(txCtx)
			if err != nil {
				return false, err
			}
			_, err = tx.Exec(txCtx, `create function reject_type_correction() returns trigger language plpgsql as $$
				begin raise exception 'fixture rejects correction' using errcode = '23514'; end; $$;
				create trigger reject_type_correction before update on bare_metal_instance_types
				for each row execute function reject_type_correction()`)
			return true, err
		})
		Expect(err).ToNot(HaveOccurred())
		_, err = update(correction(original, "amd64"), architectureMask, true)
		Expect(err).To(HaveOccurred())
		Expect(proto.Equal(get(), original)).To(BeTrue())
	})

	It("retains existing instance and catalog item references after correction", func() {
		original := seed("x86_64")
		reference := privatev1.BareMetalInstanceTypeReference_builder{Id: original.GetId(), Name: original.GetMetadata().GetName(), Shared: true}.Build()
		_, err := database.WithNewTx(ctx, func(txCtx context.Context) (bool, error) {
			instances, err := dao.NewGenericDAO[*privatev1.BareMetalInstance]().SetLogger(logger).SetTenancyLogic(tenancy).Build()
			if err != nil {
				return false, err
			}
			_, err = instances.Create().SetObject(privatev1.BareMetalInstance_builder{
				Id: "referencing-instance", Metadata: privatev1.Metadata_builder{Name: "referencing-instance", Tenant: testTenant}.Build(),
				Spec: privatev1.BareMetalInstanceSpec_builder{InstanceType: reference}.Build(),
			}.Build()).Do(txCtx)
			if err != nil {
				return false, err
			}
			items, err := dao.NewGenericDAO[*privatev1.BareMetalInstanceCatalogItem]().SetLogger(logger).SetTenancyLogic(tenancy).Build()
			if err != nil {
				return false, err
			}
			_, err = items.Create().SetObject(privatev1.BareMetalInstanceCatalogItem_builder{
				Id: "referencing-item", Metadata: privatev1.Metadata_builder{Name: "referencing-item", Tenant: testTenant}.Build(),
				Fields: privatev1.BareMetalInstanceCatalogItemFields_builder{InstanceType: privatev1.BareMetalInstanceTypeReferenceFieldPolicy_builder{Locked: reference}.Build()}.Build(),
			}.Build()).Do(txCtx)
			return true, err
		})
		Expect(err).ToNot(HaveOccurred())
		_, err = update(correction(original, "amd64"), architectureMask, true)
		Expect(err).ToNot(HaveOccurred())
		instances, err := NewPrivateBareMetalInstancesServer().SetLogger(logger).SetAttributionLogic(attribution).SetTenancyLogic(tenancy).Build()
		Expect(err).ToNot(HaveOccurred())
		items, err := NewPrivateBareMetalInstanceCatalogItemsServer().SetLogger(logger).SetAttributionLogic(attribution).SetTenancyLogic(tenancy).Build()
		Expect(err).ToNot(HaveOccurred())
		_, err = database.WithNewTx(ctx, func(txCtx context.Context) (bool, error) {
			instance, err := instances.Get(txCtx, privatev1.BareMetalInstancesGetRequest_builder{Id: "referencing-instance"}.Build())
			if err != nil {
				return false, err
			}
			item, err := items.Get(txCtx, privatev1.BareMetalInstanceCatalogItemsGetRequest_builder{Id: "referencing-item"}.Build())
			if err != nil {
				return false, err
			}
			Expect(proto.Equal(instance.GetObject().GetSpec().GetInstanceType(), reference)).To(BeTrue())
			Expect(proto.Equal(item.GetObject().GetFields().GetInstanceType().GetLocked(), reference)).To(BeTrue())
			return true, nil
		})
		Expect(err).ToNot(HaveOccurred())
	})
})
