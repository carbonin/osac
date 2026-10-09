/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"context"
	"errors"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/osac-project/osac/fulfillment-service/internal/auth"
	privatev1 "github.com/osac-project/osac/proto/gen/osac/private/v1"
)

type PrivateBareMetalInstanceTypesServerBuilder struct {
	logger            *slog.Logger
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
	filterDesc        protoreflect.MessageDescriptor
}

var _ privatev1.BareMetalInstanceTypesServer = (*PrivateBareMetalInstanceTypesServer)(nil)

type PrivateBareMetalInstanceTypesServer struct {
	privatev1.UnimplementedBareMetalInstanceTypesServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.BareMetalInstanceType]
}

func NewPrivateBareMetalInstanceTypesServer() *PrivateBareMetalInstanceTypesServerBuilder {
	return &PrivateBareMetalInstanceTypesServerBuilder{}
}

func (b *PrivateBareMetalInstanceTypesServerBuilder) SetLogger(value *slog.Logger) *PrivateBareMetalInstanceTypesServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateBareMetalInstanceTypesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateBareMetalInstanceTypesServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateBareMetalInstanceTypesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateBareMetalInstanceTypesServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivateBareMetalInstanceTypesServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateBareMetalInstanceTypesServerBuilder {
	b.metricsRegisterer = value
	return b
}

// SetFilterDesc sets the protobuf message descriptor used to validate and translate CEL filter
// expressions. This is optional. When unset, the descriptor of this server's own private message type is used.
func (b *PrivateBareMetalInstanceTypesServerBuilder) SetFilterDesc(value protoreflect.MessageDescriptor) *PrivateBareMetalInstanceTypesServerBuilder {
	b.filterDesc = value
	return b
}

func (b *PrivateBareMetalInstanceTypesServerBuilder) Build() (result *PrivateBareMetalInstanceTypesServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the generic server:
	generic, err := NewGenericServer[*privatev1.BareMetalInstanceType]().
		SetLogger(b.logger).
		SetService(privatev1.BareMetalInstanceTypes_ServiceDesc.ServiceName).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		SetFilterDesc(b.filterDesc).
		SetAllowedTenants(auth.SharedTenant).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateBareMetalInstanceTypesServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateBareMetalInstanceTypesServer) List(ctx context.Context,
	request *privatev1.BareMetalInstanceTypesListRequest) (response *privatev1.BareMetalInstanceTypesListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstanceTypesServer) Get(ctx context.Context,
	request *privatev1.BareMetalInstanceTypesGetRequest) (response *privatev1.BareMetalInstanceTypesGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstanceTypesServer) Create(ctx context.Context,
	request *privatev1.BareMetalInstanceTypesCreateRequest) (response *privatev1.BareMetalInstanceTypesCreateResponse, err error) {
	if tenant := request.GetObject().GetMetadata().GetTenant(); tenant != "" && tenant != auth.SharedTenant {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'metadata.tenant' must be '%s' or empty for bare metal instance types",
			auth.SharedTenant)
		return
	}
	if project := request.GetObject().GetMetadata().GetProject(); project != "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'metadata.project' must be empty for bare metal instance types")
		return
	}
	// BareMetalInstanceType is platform-scoped and always lives in the shared tenant. The generic
	// layer does not default to shared on its own: an omitted tenant falls back to the caller's
	// default tenant, which the allowed-tenants check then rejects. Force it to shared here, the same
	// way PrivateStorageTiersServer does for its platform-scoped objects. Any explicit non-shared
	// tenant was already rejected above.
	metadata := request.GetObject().GetMetadata()
	if metadata == nil {
		metadata = &privatev1.Metadata{}
		request.GetObject().SetMetadata(metadata)
	}
	metadata.SetTenant(auth.SharedTenant)
	request.GetObject().SetId(metadata.GetName())
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstanceTypesServer) Update(ctx context.Context,
	request *privatev1.BareMetalInstanceTypesUpdateRequest) (response *privatev1.BareMetalInstanceTypesUpdateResponse, err error) {
	// Get the object identifier:
	id := request.GetObject().GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	if request.GetUpdateMask() == nil {
		// Full updates preserve omitted metadata while replacing the complete specification.
		// Express the supplied metadata fields as paths so they merge with the locked row.
		request = proto.Clone(request).(*privatev1.BareMetalInstanceTypesUpdateRequest)
		paths := []string{"spec"}
		if request.GetObject().HasStatus() {
			paths = append(paths, "status")
		}
		request.GetObject().GetMetadata().ProtoReflect().Range(func(field protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
			if field.Name() != "version" {
				paths = append(paths, "metadata."+string(field.Name()))
			}
			return true
		})
		request.SetUpdateMask(&fieldmaskpb.FieldMask{Paths: paths})
	}
	if request.GetLock() && request.GetObject().GetMetadata() == nil {
		// An omitted client version must still participate in the optimistic check.
		request = proto.Clone(request).(*privatev1.BareMetalInstanceTypesUpdateRequest)
		request.GetObject().SetMetadata(&privatev1.Metadata{})
	}
	err = s.generic.UpdateWithCandidatePreparation(ctx, request, &response,
		func(_ context.Context, current, candidate *privatev1.BareMetalInstanceType) error {
			return validateBareMetalInstanceTypeImmutability(candidate, current)
		})
	return
}

func (s *PrivateBareMetalInstanceTypesServer) Delete(ctx context.Context,
	request *privatev1.BareMetalInstanceTypesDeleteRequest) (response *privatev1.BareMetalInstanceTypesDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstanceTypesServer) Signal(ctx context.Context,
	request *privatev1.BareMetalInstanceTypesSignalRequest) (response *privatev1.BareMetalInstanceTypesSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

func validateBareMetalInstanceTypeImmutability(merged, existing *privatev1.BareMetalInstanceType) error {
	// Validate immutable metadata fields:
	if merged.GetMetadata().GetName() != existing.GetMetadata().GetName() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'name' is immutable and cannot be changed from '%s' to '%s'",
			existing.GetMetadata().GetName(), merged.GetMetadata().GetName())
	}

	storedHardware := existing.GetSpec().GetHardware()
	candidateHardware := merged.GetSpec().GetHardware()
	if proto.Equal(storedHardware, candidateHardware) {
		return nil
	}
	if !isCanonicalBareMetalArchitecture(storedHardware.GetCpu().GetArchitecture()) &&
		isCanonicalBareMetalArchitecture(candidateHardware.GetCpu().GetArchitecture()) {
		corrected := proto.Clone(storedHardware).(*privatev1.BareMetalHardwareSpec)
		corrected.GetCpu().SetArchitecture(candidateHardware.GetCpu().GetArchitecture())
		if proto.Equal(corrected, candidateHardware) {
			return nil
		}
	}
	return grpcstatus.Errorf(grpccodes.InvalidArgument,
		"field 'spec.hardware' is immutable and cannot be changed after creation")
}

func isCanonicalBareMetalArchitecture(value string) bool {
	return value == "amd64" || value == "arm64" || value == "s390x"
}
