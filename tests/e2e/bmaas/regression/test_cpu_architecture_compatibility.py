from __future__ import annotations

import json
import subprocess
from typing import Any

import pytest

from tests.e2e.bmaas.conftest import BMI_DISK_IMAGE_SOURCE_REF
from tests.e2e.core.grpc_client import PRIVATE_API, PUBLIC_API, GRPCClient
from tests.e2e.core.helpers import unique_name, wait_for_bmi_grpc_removal
from tests.e2e.core.k8s_client import K8sClient
from tests.e2e.core.osac_cli import OsacCLI

pytestmark = [pytest.mark.regression, pytest.mark.requires_bmaas, pytest.mark.serial]


def _inventory(k8s: K8sClient, bmh_namespace: str) -> tuple[set[str], dict[str, Any]]:
    crs = k8s.list_json(resource="baremetalinstance", namespace=k8s.namespace).get("items", [])
    hosts = k8s.list_json(resource="baremetalhost", namespace=bmh_namespace).get("items", [])
    return (
        {cr["metadata"]["uid"] for cr in crs},
        {host["metadata"]["name"]: host.get("spec", {}).get("consumerRef") for host in hosts},
    )


def _instances_by_name(grpc: GRPCClient, name: str) -> list[dict[str, Any]]:
    try:
        return grpc.call(
            service=f"{PRIVATE_API}.BareMetalInstances/List",
            data={"filter": f"this.metadata.name == {json.dumps(name)}"},
        ).get("items", [])
    except Exception:
        raise RuntimeError("Unable to inspect BareMetalInstances") from None


@pytest.mark.parametrize("request_path", ["cli", "public", "private"])
def test_architecture_mismatch_has_no_side_effects(
    request_path: str,
    request: pytest.FixtureRequest,
    bmi_instance_type: str,
    bmi_template: str,
    catalog_item: str,
    jwt_cli_user: OsacCLI,
    jwt_grpc_tenant1: GRPCClient,
    private_grpc: GRPCClient,
    k8s_hub_client: K8sClient,
    bmh_namespace: str,
    ssh_public_key: str,
) -> None:
    image_name = unique_name("e2e-bmi-arm64-image")
    try:
        image_id = private_grpc.create_disk_image(
            name=image_name, source_ref=BMI_DISK_IMAGE_SOURCE_REF, architecture=["ARCHITECTURE_ARM64"], api=PRIVATE_API
        )
    except Exception:
        raise RuntimeError("Mismatching DiskImage creation failed") from None

    def cleanup_image() -> None:
        try:
            private_grpc.delete_disk_image(disk_image_id=image_id, api=PRIVATE_API)
        except Exception:
            raise RuntimeError("Mismatching DiskImage cleanup failed") from None

    request.addfinalizer(cleanup_image)
    name = unique_name("e2e-bmi-architecture-rejected")
    before = _inventory(k8s_hub_client, bmh_namespace)
    try:
        if request_path == "cli":
            with pytest.raises(subprocess.CalledProcessError) as rejected:
                jwt_cli_user.create_baremetal_instance(
                    name=name, catalog_item=catalog_item, disk_image=image_name, ssh_key=ssh_public_key
                )
            output = (rejected.value.stdout or "") + (rejected.value.stderr or "")
        else:
            spec: dict[str, Any] = {
                "disk_image": {"id": image_id},
                "instance_type": {"name": bmi_instance_type, "shared": True},
                "ssh_public_key": ssh_public_key,
                "run_strategy": "BARE_METAL_INSTANCE_RUN_STRATEGY_ALWAYS",
            }
            metadata = {"name": name}
            client, api = jwt_grpc_tenant1, PUBLIC_API
            if request_path == "private":
                client, api = private_grpc, PRIVATE_API
                metadata["tenant"] = "tenant1"
                spec["template"] = {"name": bmi_template, "shared": True}
            else:
                spec["catalog_item"] = {"id": catalog_item}
            try:
                output, code = client.call_unchecked(
                    service=f"{api}.BareMetalInstances/Create", data={"object": {"metadata": metadata, "spec": spec}}
                )
            except Exception:
                raise RuntimeError("BareMetalInstance creation request failed") from None
            assert code != 0, "Mismatching request was admitted"

        assert "InvalidArgument" in output
        assert "INCOMPATIBLE_DISK_IMAGE_ARCHITECTURE" in output
        assert BMI_DISK_IMAGE_SOURCE_REF not in output
        assert not _instances_by_name(private_grpc, name)
        assert _inventory(k8s_hub_client, bmh_namespace) == before
    finally:
        for instance in _instances_by_name(private_grpc, name):
            try:
                private_grpc.call(service=f"{PRIVATE_API}.BareMetalInstances/Delete", data={"id": instance["id"]})
                wait_for_bmi_grpc_removal(grpc=private_grpc, uuid=instance["id"])
            except Exception:
                raise RuntimeError("BareMetalInstance cleanup failed") from None
