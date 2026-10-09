/*
Copyright (c) 2026 Red Hat Inc.

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
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/osac-project/osac/fulfillment-service/internal/uuid"
	privatev1 "github.com/osac-project/osac/proto/gen/osac/private/v1"
)

func architectureTypeInput() *privatev1.BareMetalInstanceType {
	return privatev1.BareMetalInstanceType_builder{
		Metadata: privatev1.Metadata_builder{
			Name:        "it-bm-arch-" + uuid.New(),
			Labels:      map[string]string{"profile": "architecture-test"},
			Annotations: map[string]string{"example.test/note": "preserve"},
		}.Build(),
		Spec: privatev1.BareMetalInstanceTypeSpec_builder{
			Description:       "Architecture contract fixture",
			HostLabelSelector: privatev1.BareMetalLabelSelector_builder{MatchLabels: map[string]string{"profile": "architecture-test"}}.Build(),
			Hardware: privatev1.BareMetalHardwareSpec_builder{
				Cpu:          privatev1.BareMetalCPUSpec_builder{Cores: 8, ThreadsPerCore: 2, Architecture: "amd64", Model: "fixture-cpu"}.Build(),
				Memory:       privatev1.BareMetalMemorySpec_builder{TotalGb: 32, Type: "DDR5"}.Build(),
				Disks:        []*privatev1.BareMetalDiskSpec{privatev1.BareMetalDiskSpec_builder{Type: "SSD", CapacityGb: 512, Interface: "SATA"}.Build()},
				Accelerators: []*privatev1.BareMetalAcceleratorSpec{privatev1.BareMetalAcceleratorSpec_builder{Type: "GPU", Model: "fixture-gpu"}.Build()},
				NetworkPorts: []*privatev1.BareMetalNetworkPortSpec{privatev1.BareMetalNetworkPortSpec_builder{Name: "data-0", Role: "fabric", Type: "Ethernet", Speed: "25Gbps"}.Build()},
				Capabilities: map[string]string{"boot": "uefi"},
			}.Build(),
		}.Build(),
	}.Build()
}

func createLegacyArchitectureType(ctx context.Context, client privatev1.BareMetalInstanceTypesClient, architecture string) *privatev1.BareMetalInstanceType {
	GinkgoHelper()
	response, err := client.Create(ctx, privatev1.BareMetalInstanceTypesCreateRequest_builder{Object: architectureTypeInput()}.Build())
	Expect(err).ToNot(HaveOccurred())
	id := response.GetObject().GetId()
	DeferCleanup(func() {
		_, err := client.Delete(context.Background(), privatev1.BareMetalInstanceTypesDeleteRequest_builder{Id: id}.Build())
		Expect(err).ToNot(HaveOccurred())
	})
	Expect(seedLegacyArchitecture(ctx, response.GetObject(), architecture)).To(Succeed())
	stored, err := client.Get(ctx, privatev1.BareMetalInstanceTypesGetRequest_builder{Id: id}.Build())
	Expect(err).ToNot(HaveOccurred())
	Expect(stored.GetObject().GetSpec().GetHardware().GetCpu().GetArchitecture()).To(Equal(architecture))
	return stored.GetObject()
}

// Legacy fixtures bypass new-write validation only for freshly API-created synthetic rows
// in the explicitly selected test namespace's bundled PostgreSQL database.
func seedLegacyArchitecture(ctx context.Context, object *privatev1.BareMetalInstanceType, architecture string) error {
	namespace := os.Getenv("OSAC_IT_NAMESPACE")
	if namespace == "" {
		return fmt.Errorf("legacy type fixtures require OSAC_IT_NAMESPACE and a dedicated Kind database")
	}
	id := object.GetId()
	if !regexp.MustCompile(`^it-bm-arch-[0-9a-f-]{36}$`).MatchString(id) || object.GetMetadata().GetName() != id {
		return fmt.Errorf("legacy fixture requires a freshly created architecture test type")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	secret, err := tool.kubeClientSet.CoreV1().Secrets(namespace).Get(ctx, "fulfillment-db", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("legacy fixture requires access to bundled fulfillment-db in the test namespace: %w", err)
	}
	connection, err := url.Parse(string(secret.Data["url"]))
	if err != nil || connection.Hostname() != "postgres" || (connection.Scheme != "postgres" && connection.Scheme != "postgresql") {
		return fmt.Errorf("legacy fixture requires the bundled PostgreSQL connection")
	}
	databaseName := strings.TrimPrefix(connection.Path, "/")
	if !regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(databaseName) {
		return fmt.Errorf("legacy fixture could not determine the bundled database name")
	}
	// psql quotes variable values as SQL literals; no shell interprets fixture data.
	cmd := exec.CommandContext(ctx, kubectlCmd, "--kubeconfig", tool.kcFile, "--namespace", namespace,
		"exec", "-i", "deployment/postgres", "--container", "server", "--",
		"psql", "--no-psqlrc", "--quiet", "--tuples-only", "--no-align", "--set=ON_ERROR_STOP=1",
		"--host=/var/run/postgresql", "--username=postgres", "--dbname="+databaseName,
		"--set=id="+id, "--set=architecture="+architecture,
		"--set=original="+object.GetSpec().GetHardware().GetCpu().GetArchitecture())
	cmd.Stdin = strings.NewReader(`with seeded as (
		update bare_metal_instance_types
		set data = jsonb_set(data, '{spec,hardware,cpu,architecture}', to_jsonb(:'architecture'::text))
		where id = :'id' and name = :'id' and tenant = 'shared'
		  and data #>> '{spec,hardware,cpu,architecture}' = :'original'
		returning id
	) select count(*) from seeded;`)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("legacy fixture requires kubectl exec and local psql access to bundled PostgreSQL: %w", err)
	}
	if strings.TrimSpace(string(output)) != "1" {
		return fmt.Errorf("legacy fixture did not update exactly one fresh type")
	}
	return nil
}
