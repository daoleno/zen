#!/usr/bin/env python3
"""Upload an IPA using Apple's App Store Connect Build Upload API."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import pathlib
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Callable


API_ROOT = "https://api.appstoreconnect.apple.com/v1"


class AppStoreConnectHTTPError(RuntimeError):
    def __init__(self, status: int, detail: str) -> None:
        self.status = status
        self.detail = detail
        super().__init__(f"App Store Connect returned HTTP {status}: {detail}")


def base64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")


def md5_file(path: pathlib.Path) -> str:
    digest = hashlib.md5(usedforsecurity=False)
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def der_to_raw_es256(signature: bytes) -> bytes:
    """Convert OpenSSL's DER ECDSA signature to JWT's fixed-width R || S."""

    def read_length(offset: int) -> tuple[int, int]:
        if offset >= len(signature):
            raise ValueError("truncated DER signature")
        first = signature[offset]
        if first < 0x80:
            return first, offset + 1
        count = first & 0x7F
        if count == 0 or count > 2 or offset + 1 + count > len(signature):
            raise ValueError("invalid DER length")
        return int.from_bytes(signature[offset + 1 : offset + 1 + count], "big"), offset + 1 + count

    if not signature or signature[0] != 0x30:
        raise ValueError("ECDSA signature is not a DER sequence")
    sequence_length, offset = read_length(1)
    if offset + sequence_length != len(signature):
        raise ValueError("invalid DER sequence length")
    values = []
    for _ in range(2):
        if offset >= len(signature) or signature[offset] != 0x02:
            raise ValueError("ECDSA signature is missing an integer")
        integer_length, value_offset = read_length(offset + 1)
        value = signature[value_offset : value_offset + integer_length]
        if len(value) != integer_length or not value:
            raise ValueError("truncated DER integer")
        value = value.lstrip(b"\x00") or b"\x00"
        if len(value) > 32:
            raise ValueError("ES256 integer exceeds 32 bytes")
        values.append(value.rjust(32, b"\x00"))
        offset = value_offset + integer_length
    if offset != len(signature):
        raise ValueError("unexpected data after DER signature")
    return b"".join(values)


def make_individual_jwt(key_id: str, key_path: pathlib.Path, now: int | None = None) -> str:
    issued_at = int(time.time()) if now is None else now
    header = {"alg": "ES256", "kid": key_id, "typ": "JWT"}
    payload = {
        "sub": "user",
        "iat": issued_at,
        "exp": issued_at + 600,
        "aud": "appstoreconnect-v1",
    }
    signing_input = ".".join(
        base64url(json.dumps(part, separators=(",", ":"), sort_keys=True).encode())
        for part in (header, payload)
    )
    result = subprocess.run(
        ["openssl", "dgst", "-sha256", "-sign", str(key_path)],
        input=signing_input.encode("ascii"),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if result.returncode:
        detail = result.stderr.decode(errors="replace").strip()
        raise RuntimeError(f"openssl could not sign the JWT: {detail}")
    return f"{signing_input}.{base64url(der_to_raw_es256(result.stdout))}"


class AppStoreConnectClient:
    def __init__(
        self,
        key_id: str,
        key_path: pathlib.Path,
        opener: Callable[..., Any] = urllib.request.urlopen,
    ) -> None:
        self.key_id = key_id
        self.key_path = key_path
        self.opener = opener

    def api_request(self, method: str, path: str, body: dict[str, Any] | None = None) -> dict[str, Any]:
        data = None if body is None else json.dumps(body, separators=(",", ":")).encode()
        request = urllib.request.Request(
            f"{API_ROOT}{path}",
            data=data,
            method=method,
            headers={
                "Accept": "application/json",
                "Authorization": f"Bearer {make_individual_jwt(self.key_id, self.key_path)}",
                **({"Content-Type": "application/json"} if data is not None else {}),
            },
        )
        try:
            with self.opener(request, timeout=120) as response:
                response_data = response.read()
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode(errors="replace")
            exc.close()
            raise AppStoreConnectHTTPError(exc.code, detail) from exc
        except urllib.error.URLError as exc:
            raise RuntimeError(f"App Store Connect request failed: {exc.reason}") from exc
        if not response_data:
            return {}
        try:
            return json.loads(response_data)
        except json.JSONDecodeError as exc:
            raise RuntimeError("App Store Connect returned invalid JSON") from exc

    def upload_part(self, operation: dict[str, Any], ipa_path: pathlib.Path) -> None:
        offset, length = operation["offset"], operation["length"]
        with ipa_path.open("rb") as ipa:
            ipa.seek(offset)
            data = ipa.read(length)
        if len(data) != length:
            raise RuntimeError(f"could not read IPA byte range {offset}:{offset + length}")
        headers = {item["name"]: item["value"] for item in operation.get("requestHeaders", [])}
        request = urllib.request.Request(
            operation["url"], data=data, method=operation["method"], headers=headers
        )
        try:
            with self.opener(request, timeout=300) as response:
                response.read()
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode(errors="replace")
            raise RuntimeError(
                f"IPA part {operation.get('partNumber', '?')} returned HTTP {exc.code}: {detail}"
            ) from exc
        except urllib.error.URLError as exc:
            raise RuntimeError(
                f"IPA part {operation.get('partNumber', '?')} upload failed: {exc.reason}"
            ) from exc


def validate_operations(operations: list[dict[str, Any]], file_size: int) -> list[dict[str, Any]]:
    if not operations:
        raise RuntimeError("Apple returned no IPA upload operations")
    ordered = sorted(operations, key=lambda operation: operation.get("offset", -1))
    expected_offset = 0
    for operation in ordered:
        if any(key not in operation for key in ("url", "method", "offset", "length")):
            raise RuntimeError("Apple returned an incomplete IPA upload operation")
        if operation["offset"] != expected_offset or operation["length"] <= 0:
            raise RuntimeError("Apple returned overlapping or incomplete IPA byte ranges")
        expected_offset += operation["length"]
    if expected_offset != file_size:
        raise RuntimeError("Apple upload operations do not cover the complete IPA")
    return ordered


def upload_build(
    client: AppStoreConnectClient,
    ipa_path: pathlib.Path,
    app_id: str,
    version: str,
    build_number: str,
    poll_seconds: int = 15,
    max_wait_seconds: int = 3600,
) -> str:
    file_size = ipa_path.stat().st_size
    if file_size <= 0:
        raise ValueError("IPA must not be empty")
    upload = client.api_request(
        "POST",
        "/buildUploads",
        {
            "data": {
                "type": "buildUploads",
                "attributes": {
                    "cfBundleShortVersionString": version,
                    "cfBundleVersion": build_number,
                    "platform": "IOS",
                },
                "relationships": {"app": {"data": {"type": "apps", "id": app_id}}},
            }
        },
    )
    upload_id = upload["data"]["id"]
    reservation = client.api_request(
        "POST",
        "/buildUploadFiles",
        {
            "data": {
                "type": "buildUploadFiles",
                "attributes": {
                    "assetType": "ASSET",
                    "fileName": ipa_path.name,
                    "fileSize": file_size,
                    "uti": "com.apple.ipa",
                },
                "relationships": {
                    "buildUpload": {"data": {"type": "buildUploads", "id": upload_id}}
                },
            }
        },
    )
    upload_file = reservation["data"]
    operations = validate_operations(upload_file["attributes"].get("uploadOperations", []), file_size)
    for operation in operations:
        client.upload_part(operation, ipa_path)
    client.api_request(
        "PATCH",
        f"/buildUploadFiles/{upload_file['id']}",
        {
            "data": {
                "type": "buildUploadFiles",
                "id": upload_file["id"],
                "attributes": {
                    "uploaded": True,
                    "sourceFileChecksums": {
                        "file": {"algorithm": "MD5", "hash": md5_file(ipa_path)}
                    },
                },
            }
        },
    )

    deadline = time.monotonic() + max_wait_seconds
    while True:
        status = client.api_request("GET", f"/buildUploads/{upload_id}")
        state_value = status["data"]["attributes"].get("state", {})
        state = state_value.get("state") if isinstance(state_value, dict) else state_value
        if state == "COMPLETE":
            return upload_id
        if state == "FAILED":
            errors = state_value.get("errors", []) if isinstance(state_value, dict) else []
            raise RuntimeError(f"App Store Connect build upload failed: {json.dumps(errors)}")
        if time.monotonic() >= deadline:
            raise TimeoutError(f"build upload {upload_id} is still {state or 'UNKNOWN'}")
        time.sleep(poll_seconds)


def exact_build(
    client: AppStoreConnectClient,
    app_id: str,
    version: str,
    build_number: str,
) -> dict[str, Any] | None:
    """Return the one exact iOS build, or None before Apple creates it."""

    query = urllib.parse.urlencode(
        [
            ("filter[app]", app_id),
            ("filter[version]", build_number),
            ("filter[preReleaseVersion.version]", version),
            ("filter[preReleaseVersion.platform]", "IOS"),
            ("limit", "10"),
        ]
    )
    builds = client.api_request("GET", f"/builds?{query}").get("data", [])
    if len(builds) > 1:
        raise RuntimeError(
            f"App Store Connect returned multiple builds for {version} ({build_number})"
        )
    return builds[0] if builds else None


def exact_build_upload(
    client: AppStoreConnectClient,
    app_id: str,
    version: str,
    build_number: str,
) -> dict[str, Any] | None:
    """Return the one upload reservation for an exact iOS build identity."""

    query = urllib.parse.urlencode(
        [
            ("filter[cfBundleShortVersionString]", version),
            ("filter[cfBundleVersion]", build_number),
            ("filter[platform]", "IOS"),
            ("limit", "2"),
        ]
    )
    uploads = client.api_request(
        "GET", f"/apps/{app_id}/buildUploads?{query}"
    ).get("data", [])
    if len(uploads) > 1:
        raise RuntimeError(
            f"App Store Connect returned multiple build uploads for {version} ({build_number})"
        )
    return uploads[0] if uploads else None


def prepare_testflight_build(
    client: AppStoreConnectClient,
    app_id: str,
    version: str,
    build_number: str,
    beta_group_name: str,
    submit_beta_review: bool = False,
    poll_seconds: int = 30,
    max_wait_seconds: int = 3600,
) -> dict[str, str]:
    """Wait for App Store processing, handle compliance, and distribute a build."""

    deadline = time.monotonic() + max_wait_seconds
    build: dict[str, Any] | None = None
    while True:
        candidate = exact_build(client, app_id, version, build_number)
        if candidate:
            build = candidate
            state = candidate.get("attributes", {}).get("processingState")
            if state == "VALID":
                break
            if state in ("FAILED", "INVALID"):
                raise RuntimeError(f"App Store Connect build processing ended in {state}")
        if time.monotonic() >= deadline:
            state = (build or {}).get("attributes", {}).get("processingState", "NOT_FOUND")
            raise TimeoutError(f"build {version} ({build_number}) is still {state}")
        time.sleep(poll_seconds)

    build_id = build["id"]
    if build.get("attributes", {}).get("usesNonExemptEncryption") is not False:
        try:
            client.api_request(
                "PATCH",
                f"/builds/{build_id}",
                {
                    "data": {
                        "type": "builds",
                        "id": build_id,
                        "attributes": {"usesNonExemptEncryption": False},
                    }
                },
            )
        except AppStoreConnectHTTPError as exc:
            if exc.status != 409:
                raise
            verified = exact_build(client, app_id, version, build_number)
            if (
                verified is None
                or verified.get("id") != build_id
                or verified.get("attributes", {}).get("usesNonExemptEncryption") is not False
            ):
                raise
            print(
                "App Store Connect returned HTTP 409 after export compliance was "
                "already set to the required false value; continuing"
            )

    group_query = urllib.parse.urlencode(
        {
            "limit": "200",
            "fields[betaGroups]": "name,publicLinkEnabled,publicLink",
        }
    )
    groups = client.api_request("GET", f"/apps/{app_id}/betaGroups?{group_query}").get("data", [])
    matches = [group for group in groups if group.get("attributes", {}).get("name") == beta_group_name]
    if len(matches) != 1:
        raise RuntimeError(
            f"expected exactly one beta group named {beta_group_name!r}; found {len(matches)}"
        )
    group = matches[0]
    if group.get("attributes", {}).get("publicLinkEnabled") is not True:
        raise RuntimeError(f"beta group {beta_group_name!r} does not have a public link enabled")
    group_id = group["id"]

    members = client.api_request("GET", f"/betaGroups/{group_id}/builds?limit=200").get(
        "data", []
    )
    if not any(item.get("id") == build_id for item in members):
        client.api_request(
            "POST",
            f"/betaGroups/{group_id}/relationships/builds",
            {"data": [{"type": "builds", "id": build_id}]},
        )

    result = {"build_id": build_id, "beta_group_id": group_id, "review": "not_requested"}
    if submit_beta_review:
        review_query = urllib.parse.urlencode(
            [("filter[build]", build_id), ("limit", "2")]
        )
        reviews = client.api_request(
            "GET", f"/betaAppReviewSubmissions?{review_query}"
        ).get("data", [])
        if len(reviews) > 1:
            raise RuntimeError(
                f"App Store Connect returned multiple beta review submissions for build {build_id}"
            )
        if reviews:
            review_state = reviews[0].get("attributes", {}).get("betaReviewState")
            if review_state == "REJECTED":
                raise RuntimeError(
                    f"existing Beta App Review submission for build {build_id} is REJECTED"
                )
            result["review"] = reviews[0]["id"]
        else:
            review = client.api_request(
                "POST",
                "/betaAppReviewSubmissions",
                {
                    "data": {
                        "type": "betaAppReviewSubmissions",
                        "relationships": {
                            "build": {"data": {"type": "builds", "id": build_id}}
                        },
                    }
                },
            )
            result["review"] = review["data"]["id"]
    return result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--ipa", type=pathlib.Path)
    parser.add_argument("--app-id", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--build-number", required=True)
    parser.add_argument("--key-id", required=True)
    parser.add_argument("--key-path", required=True, type=pathlib.Path)
    parser.add_argument("--max-wait-seconds", type=int, default=3600)
    parser.add_argument("--beta-group-name")
    parser.add_argument("--submit-beta-review", action="store_true")
    parser.add_argument("--postprocess-only", action="store_true")
    parser.add_argument("--reuse-existing-build", action="store_true")
    args = parser.parse_args()
    if not args.key_path.is_file():
        raise SystemExit("error: --key-path must be a file")
    if not args.postprocess_only:
        if args.ipa is None or not args.ipa.is_file():
            raise SystemExit("error: --ipa must be a file unless --postprocess-only is used")
        client = AppStoreConnectClient(args.key_id, args.key_path)
        existing = (
            exact_build(client, args.app_id, args.version, args.build_number)
            if args.reuse_existing_build
            else None
        )
        if existing:
            print(
                "App Store Connect already has exact build "
                f"{args.version} ({args.build_number}); resuming post-processing"
            )
        else:
            existing_upload = (
                exact_build_upload(client, args.app_id, args.version, args.build_number)
                if args.reuse_existing_build
                else None
            )
            if existing_upload:
                state_value = existing_upload.get("attributes", {}).get("state", {})
                state = (
                    state_value.get("state")
                    if isinstance(state_value, dict)
                    else state_value
                )
                if state != "COMPLETE":
                    raise RuntimeError(
                        "exact App Store Connect build upload already exists in state "
                        f"{state or 'UNKNOWN'}; refusing a duplicate upload"
                    )
                print(
                    "App Store Connect already completed the exact build upload "
                    f"{args.version} ({args.build_number}); resuming post-processing"
                )
            else:
                upload_id = upload_build(
                    client,
                    args.ipa,
                    args.app_id,
                    args.version,
                    args.build_number,
                    max_wait_seconds=args.max_wait_seconds,
                )
                print(f"App Store Connect accepted build upload {upload_id}")
    elif not args.beta_group_name:
        raise SystemExit("error: --postprocess-only requires --beta-group-name")
    if args.beta_group_name:
        prepared = prepare_testflight_build(
            AppStoreConnectClient(args.key_id, args.key_path),
            args.app_id,
            args.version,
            args.build_number,
            args.beta_group_name,
            submit_beta_review=args.submit_beta_review,
            max_wait_seconds=args.max_wait_seconds,
        )
        print(
            "TestFlight build is VALID, export compliance is handled, "
            f"and beta group is attached (build {prepared['build_id']}, review {prepared['review']})"
        )
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (KeyError, RuntimeError, TimeoutError, ValueError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        sys.exit(1)
