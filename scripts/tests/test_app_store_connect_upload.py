import base64
import hashlib
import importlib.util
import json
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock


SCRIPT = pathlib.Path(__file__).parents[1] / "app-store-connect-upload.py"
SPEC = importlib.util.spec_from_file_location("asc_upload", SCRIPT)
asc_upload = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(asc_upload)


def decode_segment(segment):
    return json.loads(base64.urlsafe_b64decode(segment + "=" * (-len(segment) % 4)))


def raw_to_der(signature):
    def integer(value):
        value = value.lstrip(b"\x00") or b"\x00"
        if value[0] & 0x80:
            value = b"\x00" + value
        return b"\x02" + bytes([len(value)]) + value

    body = integer(signature[:32]) + integer(signature[32:])
    return b"\x30" + bytes([len(body)]) + body


class Response:
    def __init__(self, body=b""):
        self.body = body

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self):
        return self.body


class BuildUploadTests(unittest.TestCase):
    def setUp(self):
        self.temp_dir = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.temp_dir.name)
        self.key = self.root / "synthetic.p8"
        subprocess.run(
            ["openssl", "ecparam", "-name", "prime256v1", "-genkey", "-noout", "-out", self.key],
            check=True,
            capture_output=True,
        )
        self.ipa = self.root / "Zen.ipa"
        self.ipa_bytes = b"synthetic ipa bytes"
        self.ipa.write_bytes(self.ipa_bytes)
        self.requests = []

    def tearDown(self):
        self.temp_dir.cleanup()

    def open(self, request, timeout):
        self.requests.append(request)
        if request.full_url.endswith("/buildUploads"):
            return Response(b'{"data":{"id":"upload-1"}}')
        if request.full_url.endswith("/buildUploadFiles"):
            body = {
                "data": {
                    "id": "file-1",
                    "attributes": {
                        "uploadOperations": [{
                            "url": "https://upload.example/part",
                            "method": "PUT",
                            "offset": 0,
                            "length": self.ipa.stat().st_size,
                            "requestHeaders": [{"name": "x-apple-test", "value": "reserved"}],
                        }]
                    },
                }
            }
            return Response(json.dumps(body).encode())
        if request.full_url == "https://upload.example/part":
            return Response()
        if request.full_url.endswith("/buildUploadFiles/file-1"):
            return Response(b'{"data":{"id":"file-1"}}')
        if request.full_url.endswith("/buildUploads/upload-1"):
            return Response(b'{"data":{"attributes":{"state":{"state":"COMPLETE"}}}}')
        raise AssertionError(request.full_url)

    def test_individual_jwt_has_no_issuer_and_valid_raw_es256_signature(self):
        token = asc_upload.make_individual_jwt("SYNTHETIC", self.key, now=1_700_000_000)
        encoded_header, encoded_payload, encoded_signature = token.split(".")
        header, payload = decode_segment(encoded_header), decode_segment(encoded_payload)
        signature = base64.urlsafe_b64decode(encoded_signature + "=" * (-len(encoded_signature) % 4))
        self.assertEqual(header, {"alg": "ES256", "kid": "SYNTHETIC", "typ": "JWT"})
        self.assertEqual(payload["sub"], "user")
        self.assertNotIn("iss", payload)
        self.assertEqual(payload["aud"], "appstoreconnect-v1")
        self.assertEqual(payload["exp"] - payload["iat"], 600)
        self.assertEqual(len(signature), 64)

        public_key, signature_path, message_path = self.root / "pub", self.root / "sig", self.root / "msg"
        subprocess.run(["openssl", "pkey", "-in", self.key, "-pubout", "-out", public_key], check=True)
        signature_path.write_bytes(raw_to_der(signature))
        message_path.write_bytes(f"{encoded_header}.{encoded_payload}".encode())
        verified = subprocess.run(
            ["openssl", "dgst", "-sha256", "-verify", public_key, "-signature", signature_path, message_path],
            capture_output=True,
            text=True,
        )
        self.assertEqual(verified.returncode, 0, verified.stderr)

    def test_official_build_upload_requests_and_streamed_md5_commit(self):
        client = asc_upload.AppStoreConnectClient("SYNTHETIC", self.key, opener=self.open)
        with mock.patch.object(pathlib.Path, "read_bytes", side_effect=AssertionError("not streaming")):
            upload_id = asc_upload.upload_build(
                client, self.ipa, "6790486708", "0.1.0", "42", poll_seconds=0, max_wait_seconds=1
            )
        self.assertEqual(upload_id, "upload-1")
        api_requests = [request for request in self.requests if request.full_url.startswith(asc_upload.API_ROOT)]
        binary = next(request for request in self.requests if request.full_url.startswith("https://upload.example"))
        for request in api_requests:
            token = request.get_header("Authorization").removeprefix("Bearer ")
            payload = decode_segment(token.split(".")[1])
            self.assertEqual(payload["sub"], "user")
            self.assertNotIn("iss", payload)
        self.assertIsNone(binary.get_header("Authorization"))
        self.assertEqual(binary.get_header("X-apple-test"), "reserved")
        self.assertEqual(binary.data, self.ipa_bytes)
        create_upload = json.loads(api_requests[0].data)["data"]
        self.assertEqual(create_upload["relationships"]["app"]["data"]["id"], "6790486708")
        self.assertEqual(create_upload["attributes"]["cfBundleShortVersionString"], "0.1.0")
        self.assertEqual(create_upload["attributes"]["cfBundleVersion"], "42")
        self.assertEqual(create_upload["attributes"]["platform"], "IOS")
        create_file = json.loads(api_requests[1].data)["data"]["attributes"]
        self.assertEqual(create_file["assetType"], "ASSET")
        self.assertEqual(create_file["uti"], "com.apple.ipa")
        commit = json.loads(api_requests[2].data)["data"]["attributes"]["sourceFileChecksums"]["file"]
        self.assertEqual(commit["algorithm"], "MD5")
        self.assertEqual(commit["hash"], hashlib.md5(self.ipa_bytes, usedforsecurity=False).hexdigest())

    def test_rejects_incomplete_byte_ranges(self):
        with self.assertRaisesRegex(RuntimeError, "complete IPA"):
            asc_upload.validate_operations(
                [{"url": "https://upload", "method": "PUT", "offset": 0, "length": 3}], 4
            )

    def test_prepares_valid_build_for_public_group_and_beta_review(self):
        requests = []

        def open_prepare(request, timeout):
            requests.append(request)
            url = request.full_url
            if "/betaGroups/group-preview/builds?" in url:
                return Response(b'{"data":[]}')
            if "/builds?" in url:
                return Response(
                    b'{"data":[{"type":"builds","id":"build-5","attributes":{"processingState":"VALID","usesNonExemptEncryption":false}}]}'
                )
            if "/betaAppReviewSubmissions?" in url:
                return Response(b'{"data":[]}')
            if url.endswith("/builds/build-5"):
                return Response(b'{"data":{"type":"builds","id":"build-5"}}')
            if "/apps/6790486708/betaGroups?" in url:
                return Response(
                    b'{"data":[{"type":"betaGroups","id":"group-preview","attributes":{"name":"Zen Preview","publicLinkEnabled":true,"publicLink":"https://testflight.apple.com/join/rTKCDzMt"}}]}'
                )
            if url.endswith("/betaGroups/group-preview/relationships/builds"):
                return Response()
            if url.endswith("/betaAppReviewSubmissions"):
                return Response(b'{"data":{"type":"betaAppReviewSubmissions","id":"review-5"}}')
            raise AssertionError(url)

        client = asc_upload.AppStoreConnectClient("SYNTHETIC", self.key, opener=open_prepare)
        result = asc_upload.prepare_testflight_build(
            client,
            "6790486708",
            "0.1.0",
            "5",
            "Zen Preview",
            submit_beta_review=True,
            poll_seconds=0,
            max_wait_seconds=1,
        )
        self.assertEqual(
            result,
            {"build_id": "build-5", "beta_group_id": "group-preview", "review": "review-5"},
        )
        self.assertFalse(
            any(request.full_url.endswith("/builds/build-5") for request in requests),
            "an already-set export compliance value must not be patched again",
        )
        attach = next(
            request
            for request in requests
            if request.full_url.endswith("/betaGroups/group-preview/relationships/builds")
        )
        self.assertEqual(json.loads(attach.data)["data"], [{"type": "builds", "id": "build-5"}])
        review = next(
            request for request in requests if request.full_url.endswith("/betaAppReviewSubmissions")
        )
        self.assertEqual(
            json.loads(review.data)["data"]["relationships"]["build"]["data"]["id"],
            "build-5",
        )

    def test_reuses_existing_review_submission(self):
        requests = []

        def open_existing(request, timeout):
            requests.append(request)
            url = request.full_url
            if "/builds?" in url:
                return Response(
                    b'{"data":[{"type":"builds","id":"build-5","attributes":{"processingState":"VALID","usesNonExemptEncryption":false}}]}'
                )
            if "/apps/6790486708/betaGroups?" in url:
                return Response(
                    b'{"data":[{"type":"betaGroups","id":"group-preview","attributes":{"name":"Zen Preview","publicLinkEnabled":true}}]}'
                )
            if "/betaGroups/group-preview/builds?" in url:
                return Response(b'{"data":[{"type":"builds","id":"build-5"}]}')
            if "/betaAppReviewSubmissions?" in url:
                return Response(
                    b'{"data":[{"type":"betaAppReviewSubmissions","id":"review-existing","attributes":{"betaReviewState":"IN_REVIEW"}}]}'
                )
            raise AssertionError(url)

        result = asc_upload.prepare_testflight_build(
            asc_upload.AppStoreConnectClient("SYNTHETIC", self.key, opener=open_existing),
            "6790486708",
            "0.1.0",
            "5",
            "Zen Preview",
            submit_beta_review=True,
            poll_seconds=0,
            max_wait_seconds=1,
        )
        self.assertEqual(result["review"], "review-existing")
        self.assertFalse(
            any(
                request.method == "POST" and request.full_url.endswith("/betaAppReviewSubmissions")
                for request in requests
            )
        )

    def test_stops_on_rejected_existing_review_submission(self):
        def open_rejected(request, timeout):
            url = request.full_url
            if "/builds?" in url:
                return Response(
                    b'{"data":[{"type":"builds","id":"build-5","attributes":{"processingState":"VALID","usesNonExemptEncryption":false}}]}'
                )
            if "/apps/6790486708/betaGroups?" in url:
                return Response(
                    b'{"data":[{"type":"betaGroups","id":"group-preview","attributes":{"name":"Zen Preview","publicLinkEnabled":true}}]}'
                )
            if "/betaGroups/group-preview/builds?" in url:
                return Response(b'{"data":[{"type":"builds","id":"build-5"}]}')
            if "/betaAppReviewSubmissions?" in url:
                return Response(
                    b'{"data":[{"id":"review-rejected","attributes":{"betaReviewState":"REJECTED"}}]}'
                )
            raise AssertionError(url)

        with self.assertRaisesRegex(RuntimeError, "REJECTED"):
            asc_upload.prepare_testflight_build(
                asc_upload.AppStoreConnectClient("SYNTHETIC", self.key, opener=open_rejected),
                "6790486708",
                "0.1.0",
                "5",
                "Zen Preview",
                submit_beta_review=True,
                poll_seconds=0,
                max_wait_seconds=1,
            )

    def test_finds_one_exact_build_and_rejects_ambiguous_identity(self):
        def opener(body):
            return lambda _request, timeout: Response(body)

        client = asc_upload.AppStoreConnectClient(
            "SYNTHETIC",
            self.key,
            opener=opener(b'{"data":[{"id":"build-5"}]}'),
        )
        self.assertEqual(
            asc_upload.exact_build(client, "6790486708", "0.1.0", "5")["id"],
            "build-5",
        )
        ambiguous = asc_upload.AppStoreConnectClient(
            "SYNTHETIC",
            self.key,
            opener=opener(b'{"data":[{"id":"one"},{"id":"two"}]}'),
        )
        with self.assertRaisesRegex(RuntimeError, "multiple builds"):
            asc_upload.exact_build(ambiguous, "6790486708", "0.1.0", "5")

    def test_finds_exact_completed_build_upload(self):
        requests = []

        def open_upload(request, timeout):
            requests.append(request)
            return Response(
                b'{"data":[{"id":"upload-5","attributes":{"state":{"state":"COMPLETE"}}}]}'
            )

        upload = asc_upload.exact_build_upload(
            asc_upload.AppStoreConnectClient("SYNTHETIC", self.key, opener=open_upload),
            "6790486708",
            "0.1.0",
            "5",
        )
        self.assertEqual(upload["id"], "upload-5")
        url = requests[0].full_url
        self.assertIn("/apps/6790486708/buildUploads?", url)
        self.assertIn("filter%5BcfBundleShortVersionString%5D=0.1.0", url)
        self.assertIn("filter%5BcfBundleVersion%5D=5", url)


if __name__ == "__main__":
    unittest.main()
