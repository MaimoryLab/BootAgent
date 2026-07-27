from __future__ import annotations

import io
import json
import socket
import subprocess
import tempfile
import tomllib
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch
from urllib.error import HTTPError, URLError

from oneagent import catalog, installer as installer_module
from oneagent.errors import OneAgentError
from oneagent.installer import (
    InstallOptions,
    Runtime,
    _load_json,
    _merge_codex_toml,
    _next_step,
    _python_312_for_uv,
    _run_acl,
    _write_agent_config,
    _version_from_output,
    active_profile_id,
    atomic_write,
    backup_file,
    install_locked_agent,
    install_many,
    installed_version,
    list_profiles,
    load_profile,
    save_profile,
    profile_store_path,
    secrets_path,
    status_payload,
    validate_profile_id,
    write_aider_config,
    write_claude_config,
    write_codex_config,
    write_openai_compatible_config,
    write_profile,
)
from oneagent.providers import (
    _protocol_request,
    _unsupported_protocol,
    agent_protocol,
    anthropic_messages_url,
    chat_probe,
    list_models,
    openai_base_url,
    pick_chat_model,
    protocol_probe,
    provider_base,
    provider_config_base,
    provider_home,
    resolve_probe_model,
    validate_base_url,
)


class FakeResponse:
    def __init__(self, data: bytes = b"{}", status: int = 200):
        self.data = data
        self.status = status

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self):
        return self.data


class ProfileStoreTests(unittest.TestCase):
    def _runtime(self, tmp: str) -> Runtime:
        return Runtime.create(home=Path(tmp), os_id="linux", which=lambda _name: None)

    def test_validate_profile_id_rules(self):
        self.assertEqual(validate_profile_id("ppio-deepseek_v3"), "ppio-deepseek_v3")
        for bad in ("", "Upper", "-lead", "a" * 65, "sp ace", "../escape", None, 42):
            with self.subTest(bad=bad):
                with self.assertRaises(OneAgentError):
                    validate_profile_id(bad)

    def test_profile_state_starts_empty(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            self.assertEqual(load_profile(runtime), (None, None))
            self.assertEqual(list_profiles(runtime), [])
            self.assertIsNone(active_profile_id(runtime))

    def test_legacy_v1_profile_migrates_to_the_default_profile(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            legacy = {
                "schema_version": 1,
                "provider": "ppio",
                "base_url": "https://api.ppio.com/openai",
                "model": "deepseek-v3",
                "config_mode": "provider",
                "agent_ids": ["codex"],
                "activated_at": "2026-07-22T00:00:00Z",
            }
            oneagent_dir = runtime.home / ".oneagent"
            oneagent_dir.mkdir(parents=True)
            (oneagent_dir / "profile.json").write_text(json.dumps(legacy), encoding="utf-8")

            profile, error = load_profile(runtime)
            self.assertIsNone(error)
            self.assertEqual(profile["id"], "default")
            self.assertEqual(profile["provider"], "ppio")
            self.assertEqual(profile["created_at"], "2026-07-22T00:00:00Z")
            # The pointer replaced the legacy file, which survives as a backup.
            pointer = json.loads((oneagent_dir / "profile.json").read_text(encoding="utf-8"))
            self.assertEqual(pointer, {"schema_version": 2, "active": "default"})
            self.assertTrue(list(oneagent_dir.glob("profile.json.backup-*")))
            # Reading again takes the v2 path, not the migration.
            again, _ = load_profile(runtime)
            self.assertEqual(again["model"], "deepseek-v3")
            self.assertEqual(active_profile_id(runtime), "default")

    def test_migration_failure_is_reported_and_the_original_survives(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            legacy = {
                "schema_version": 1,
                "provider": "ppio",
                "base_url": None,
                "model": "m",
                "config_mode": "provider",
                "agent_ids": [],
                "activated_at": "x",
            }
            oneagent_dir = runtime.home / ".oneagent"
            oneagent_dir.mkdir(parents=True)
            original = oneagent_dir / "profile.json"
            original.write_text(json.dumps(legacy), encoding="utf-8")
            with patch(
                "oneagent.installer._write_profile_store",
                side_effect=OneAgentError("CONFIG_WRITE_FAILED", "read-only"),
            ):
                profile, error = load_profile(runtime)
            self.assertIsNone(profile)
            self.assertIn("Cannot migrate", error)
            # The pointer was never written: the v1 file stays untouched.
            self.assertEqual(json.loads(original.read_text(encoding="utf-8"))["schema_version"], 1)

    def test_profile_schema_failures_are_reported_not_raised(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            path = runtime.home / ".oneagent" / "profile.json"
            path.parent.mkdir(parents=True)
            for content, expected in (
                ("{", "invalid-json"),
                ("[]", "Unsupported environment profile schema"),
                ('{"schema_version":9}', "Unsupported environment profile schema"),
            ):
                with self.subTest(content=content):
                    path.write_text(content, encoding="utf-8")
                    _, error = load_profile(runtime)
                    if expected == "invalid-json":
                        self.assertTrue(error)
                    else:
                        self.assertEqual(error, expected)
            # v1 without a timestamp still migrates.
            path.write_text('{"schema_version":1,"provider":"x"}', encoding="utf-8")
            profile, error = load_profile(runtime)
            self.assertIsNone(error)
            self.assertEqual(profile["id"], "default")
            # A v2 pointer to a missing store entry names the profile.
            path.write_text('{"schema_version":2,"active":"ghost"}', encoding="utf-8")
            _, error = load_profile(runtime)
            self.assertIn("ghost", error)
            self.assertEqual(active_profile_id(runtime), "ghost")
            # Corrupt and non-dict store files are reported, not raised.
            store = runtime.home / ".oneagent" / "profiles"
            store.mkdir(parents=True, exist_ok=True)
            (store / "ghost.json").write_text("{", encoding="utf-8")
            _, error = load_profile(runtime)
            self.assertTrue(error)
            (store / "ghost.json").write_text("[]", encoding="utf-8")
            _, error = load_profile(runtime)
            self.assertIn("corrupt", error)

    def test_active_profile_id_survives_unreadable_pointers(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            path = runtime.home / ".oneagent" / "profile.json"
            path.parent.mkdir(parents=True)
            path.write_text("{", encoding="utf-8")
            self.assertIsNone(active_profile_id(runtime))
            path.write_text("[]", encoding="utf-8")
            self.assertIsNone(active_profile_id(runtime))
            path.write_text('{"schema_version":1}', encoding="utf-8")
            self.assertIsNone(active_profile_id(runtime))

    def test_a_tampered_pointer_cannot_escape_the_profile_store(self):
        # profile.json is a file on disk, so its "active" value is untrusted
        # even though OneAgent writes it. Without validation the pointer names
        # any path: load_profile would read an arbitrary .json as a profile and
        # secrets_path would place a plaintext key outside secrets/.
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            store = runtime.home / ".oneagent" / "profiles"
            store.mkdir(parents=True)
            outside = runtime.home / "elsewhere"
            outside.mkdir()
            (outside / "stolen.json").write_text(
                json.dumps({"schema_version": 2, "id": "stolen", "model": "LEAKED"}),
                encoding="utf-8",
            )
            pointer = runtime.home / ".oneagent" / "profile.json"
            pointer.write_text(
                json.dumps({"schema_version": 2, "active": "../../elsewhere/stolen"}),
                encoding="utf-8",
            )

            profile, error = load_profile(runtime)
            self.assertIsNone(profile)
            self.assertIn("Profile ID must", error)
            self.assertIsNone(active_profile_id(runtime))

            for bad in ("../../evil", "a/b", "..", "/abs"):
                with self.subTest(bad=bad):
                    with self.assertRaises(OneAgentError):
                        secrets_path(runtime, bad)
                    with self.assertRaises(OneAgentError):
                        profile_store_path(runtime, bad)

    def test_list_profiles_skips_unreadable_and_id_less_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            store = runtime.home / ".oneagent" / "profiles"
            store.mkdir(parents=True)
            (store / "junk.json").write_text("not-json", encoding="utf-8")
            (store / "noid.json").write_text('{"provider":"ppio"}', encoding="utf-8")
            save_profile(runtime, profile_id="real", provider="ppio", model="m", agent_ids=["codex"])
            listing = list_profiles(runtime)
            self.assertEqual([item["id"] for item in listing], ["real"])

    def test_save_profile_isolates_key_material_and_keeps_history(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            stored = save_profile(
                runtime,
                profile_id="ppio-deepseek",
                label="PPIO DeepSeek",
                provider="ppio",
                model="deepseek-v3",
                agent_ids=["opencode", "codex"],
                api_key="sk-secret",
            )
            self.assertEqual(stored["agent_ids"], ["codex", "opencode"])
            entry = list_profiles(runtime)[0]
            self.assertTrue(entry["has_key"])
            # The profile file itself must not carry the key.
            profile_text = (runtime.home / ".oneagent" / "profiles" / "ppio-deepseek.json").read_text(encoding="utf-8")
            self.assertNotIn("sk-secret", profile_text)
            secret = secrets_path(runtime, "ppio-deepseek").read_text(encoding="utf-8")
            self.assertIn("sk-secret", secret)
            # Saving again keeps the label and creation time, replaces the model,
            # and leaves the stored key in place when none is supplied.
            first_created = entry["created_at"]
            save_profile(runtime, profile_id="ppio-deepseek", provider="ppio", model="model-b", agent_ids=["codex"])
            again = list_profiles(runtime)[0]
            self.assertEqual(again["label"], "PPIO DeepSeek")
            self.assertEqual(again["created_at"], first_created)
            self.assertEqual(again["model"], "model-b")
            self.assertTrue(again["has_key"])

    def test_save_profile_validates_provider_and_id(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            with self.assertRaises(OneAgentError):
                save_profile(runtime, profile_id="ok-id", provider="nope", model="m", agent_ids=["codex"])
            with self.assertRaises(OneAgentError):
                save_profile(runtime, profile_id="../bad", provider="ppio", model="m", agent_ids=["codex"])
            with self.assertRaises(OneAgentError):
                save_profile(runtime, profile_id="custom-naked", provider="custom", model="m", agent_ids=["codex"])
            # A custom provider with a valid base is fine.
            stored = save_profile(
                runtime,
                profile_id="custom-local",
                provider="custom",
                api_base_url="http://127.0.0.1:9000",
                model="m",
                agent_ids=["codex"],
            )
            self.assertEqual(stored["base_url"], "http://127.0.0.1:9000")

    def test_write_profile_upserts_the_active_profile_and_merges_agents(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            path = write_profile(
                runtime,
                agents=["codex"],
                configure=True,
                provider="ppio",
                base_url="https://api.ppio.com/openai",
                model="deepseek-v3",
                api_key="sk-a",
            )
            self.assertEqual(json.loads(path.read_text(encoding="utf-8")), {"schema_version": 2, "active": "default"})
            # Same provider+model merges agents instead of replacing them.
            write_profile(
                runtime,
                agents=["opencode"],
                configure=True,
                provider="ppio",
                base_url="https://api.ppio.com/openai",
                model="deepseek-v3",
            )
            profile, error = load_profile(runtime)
            self.assertIsNone(error)
            self.assertEqual(profile["agent_ids"], ["codex", "opencode"])
            self.assertEqual(profile["schema_version"], 2)
            # A different provider replaces the environment without merging.
            write_profile(runtime, agents=["aider"], configure=True, provider="novita", base_url="", model="model-x")
            profile, _ = load_profile(runtime)
            self.assertEqual(profile["provider"], "novita")
            self.assertEqual(profile["agent_ids"], ["aider"])
            # The existing-account path writes neither a model nor secrets.
            write_profile(runtime, agents=["codex"], configure=False, provider="ppio", base_url="", model="")
            profile, _ = load_profile(runtime)
            self.assertEqual(profile["config_mode"], "existing-account")
            self.assertIsNone(profile["model"])
            # Key material lives only in the secret store, never the profile.
            stored_text = (runtime.home / ".oneagent" / "profiles" / "default.json").read_text(encoding="utf-8")
            self.assertNotIn("sk-a", stored_text)
            self.assertIn("sk-a", secrets_path(runtime, "default").read_text(encoding="utf-8"))

    def test_status_payload_exposes_profiles_without_key_material(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            write_profile(
                runtime,
                agents=["codex"],
                configure=True,
                provider="ppio",
                base_url="https://api.ppio.com/openai",
                model="deepseek-v3",
                api_key="sk-a",
            )
            payload = status_payload(runtime)
            self.assertEqual(payload["activeProfile"], "default")
            summary = payload["profiles"]
            self.assertEqual(len(summary), 1)
            self.assertEqual(summary[0]["id"], "default")
            self.assertEqual(summary[0]["agentIds"], ["codex"])
            self.assertTrue(summary[0]["hasKey"])
            self.assertNotIn("sk-a", json.dumps(payload))


class CatalogEdgeTests(unittest.TestCase):
    def test_resource_root_and_manifest_failures(self):
        with patch.object(catalog.sys, "_MEIPASS", "/tmp/oneagent-bundle", create=True):
            self.assertEqual(catalog.resource_root(), Path("/tmp/oneagent-bundle"))
        with tempfile.TemporaryDirectory() as tmp:
            missing = Path(tmp) / "missing.json"
            with self.assertRaises(OneAgentError):
                catalog.load_manifest(missing)
            invalid = Path(tmp) / "invalid.json"
            invalid.write_text("{", encoding="utf-8")
            with self.assertRaises(OneAgentError):
                catalog.load_manifest(invalid)
            invalid.write_text('{"schema_version":2,"agents":[]}', encoding="utf-8")
            with self.assertRaises(OneAgentError):
                catalog.load_manifest(invalid)

    def test_platform_and_home_variants(self):
        with patch("oneagent.catalog.platform.system", return_value="Darwin"), patch("oneagent.catalog.platform.machine", return_value="arm64"):
            self.assertEqual(catalog.current_platform(), {"os": "macos", "arch": "arm64", "shell": "bash"})
        with patch("oneagent.catalog.platform.system", return_value="Windows"), patch("oneagent.catalog.platform.machine", return_value="AMD64"):
            self.assertEqual(catalog.current_platform(), {"os": "windows", "arch": "x64", "shell": "powershell"})
        with patch("oneagent.catalog.platform.system", return_value="Linux"), patch("oneagent.catalog.platform.machine", return_value="x86_64"):
            self.assertEqual(catalog.current_platform()["os"], "linux")

        self.assertEqual(catalog.resolve_home({"ONEAGENT_HOME": "~/oneagent-test"}, "linux"), Path("~/oneagent-test").expanduser())
        self.assertEqual(catalog.resolve_home({"HOMEDRIVE": "C:", "HOMEPATH": "\\Users\\Name"}, "windows"), Path("C:\\Users\\Name"))
        self.assertEqual(catalog.resolve_home({"HOMEDRIVE": "C:", "HOME": "/fallback-home"}, "windows"), Path("/fallback-home"))
        self.assertEqual(catalog.resolve_home({"HOME": "/tmp/home"}, "linux"), Path("/tmp/home"))
        with patch("oneagent.catalog.Path.home", return_value=Path("/fallback")):
            self.assertEqual(catalog.resolve_home({}, "linux"), Path("/fallback"))

    def test_public_catalog_exposes_windows_note_only_on_windows(self):
        with patch("oneagent.catalog.current_platform", return_value={"os": "windows", "arch": "x64", "shell": "powershell"}):
            by_id = {item["id"]: item for item in catalog.public_catalog()}
        self.assertTrue(by_id["opencode"]["platformNote"])

    def test_public_providers_omits_anthropic_base_when_absent(self):
        # Both built-in providers publish one, so this branch is only reachable
        # with a provider that does not — and it must omit the key rather than
        # emit null, which the frontend declares as optional.
        with patch.dict(
            catalog.PROVIDERS,
            {"openai-only": {"name": "X", "home": "https://x.invalid/", "base_url": "https://x.invalid/v1"}},
            clear=False,
        ):
            public = catalog.public_providers()
        self.assertNotIn("anthropic_base_url", public["openai-only"])
        self.assertIn("anthropic_base_url", public["ppio"])

    def test_catalog_ranks_widely_used_agents_ahead_of_niche_ones(self):
        # The overview leads with this order, so it is a product decision rather
        # than an accident of dict insertion order in agents.lock.json.
        order = [item["id"] for item in catalog.public_catalog()]
        self.assertEqual(order[:4], ["codex", "claude-code", "cursor", "opencode"])
        for niche in ("kilo-cli", "aider"):
            with self.subTest(agent=niche):
                self.assertLess(order.index("cursor"), order.index(niche))
                self.assertLess(order.index("openclaw"), order.index(niche))
        # Every Agent carries an explicit rank; a missing one would silently
        # collapse to the tail.
        self.assertTrue(all(item["rank"] < 99 for item in catalog.public_catalog()))


class ProtocolEdgeTests(unittest.TestCase):
    def test_agent_protocol_mapping_covers_every_auto_adapter(self):
        adapters = {
            meta["config_adapter"]: agent_id
            for agent_id, meta in catalog.agent_catalog().items()
            if meta["config_mode"] == "auto"
        }
        self.assertEqual(
            {adapter: agent_protocol(adapter) for adapter in adapters},
            {
                "codex": "responses",
                "claude-code": "anthropic",
                "opencode": "openai",
                "kilo-cli": "openai",
                "aider": "openai",
            },
        )
        # An adapter added without a protocol entry must not silently probe a
        # protocol it does not speak; OpenAI-compatible is the documented default.
        self.assertEqual(agent_protocol("brand-new-adapter"), "openai")

    def test_anthropic_messages_url_normalises_both_base_shapes(self):
        cases = {
            "https://api.ppio.com/anthropic": "https://api.ppio.com/anthropic/v1/messages",
            "https://api.novita.ai/anthropic/": "https://api.novita.ai/anthropic/v1/messages",
            "https://proxy.test/v1": "https://proxy.test/v1/messages",
            "https://proxy.test/v1/messages": "https://proxy.test/v1/messages",
            "https://proxy.test/messages": "https://proxy.test/v1/messages",
        }
        for base, expected in cases.items():
            with self.subTest(base=base):
                self.assertEqual(anthropic_messages_url(base), expected)

    def test_unsupported_protocol_classification(self):
        # Permanent: the endpoint says the pair cannot work.
        self.assertTrue(_unsupported_protocol(404, ""))
        self.assertTrue(_unsupported_protocol(405, ""))
        self.assertTrue(_unsupported_protocol(501, ""))
        self.assertTrue(_unsupported_protocol(400, '{"message":"model: x does not support endpoint: responses"}'))
        self.assertTrue(_unsupported_protocol(500, '{"error":{"message":"not implemented"}}'))
        self.assertTrue(_unsupported_protocol(422, "Unsupported endpoint"))
        # Transient: a bare 500 or an overloaded upstream must stay retryable.
        self.assertFalse(_unsupported_protocol(500, '{"error":{"message":"internal server error"}}'))
        self.assertFalse(_unsupported_protocol(503, "the upstream load is saturated"))
        self.assertFalse(_unsupported_protocol(429, "quota exceeded"))
        self.assertFalse(_unsupported_protocol(400, '{"message":"invalid api key"}'))

    def test_each_protocol_builds_its_own_endpoint_and_payload(self):
        built = {
            protocol: _protocol_request(
                protocol, provider="custom", custom_base="https://proxy.test/v1", api_key="k", model="m"
            )
            for protocol in ("openai", "responses", "anthropic")
        }
        self.assertEqual(built["openai"].full_url, "https://proxy.test/v1/chat/completions")
        self.assertEqual(built["responses"].full_url, "https://proxy.test/v1/responses")
        self.assertEqual(built["anthropic"].full_url, "https://proxy.test/v1/messages")

        # Responses takes `input`, not `messages`; Anthropic needs its own headers.
        self.assertIn("input", json.loads(built["responses"].data))
        self.assertIn("messages", json.loads(built["openai"].data))
        self.assertEqual(built["anthropic"].get_header("X-api-key"), "k")
        self.assertEqual(built["anthropic"].get_header("Anthropic-version"), "2023-06-01")
        self.assertIsNone(built["openai"].get_header("X-api-key"))

    def test_protocol_probe_rejects_unknown_protocol_and_missing_key(self):
        with self.assertRaises(OneAgentError):
            protocol_probe(protocol="openai", provider="custom", api_key="", model="m", custom_base="https://x.test/v1")
        with self.assertRaises(OneAgentError):
            protocol_probe(protocol="grpc", provider="custom", api_key="k", model="m", custom_base="https://x.test/v1")

    def test_protocol_probe_reports_unsupported_pairs_as_permanent(self):
        error = HTTPError(
            "https://proxy.test/v1/responses", 400, "Bad Request", {},
            io.BytesIO(b'{"code":400,"reason":"INVALID_REQUEST_BODY","message":"model: glm does not support endpoint: responses"}'),
        )
        with patch("oneagent.providers.urlopen", side_effect=error):
            result = protocol_probe(
                protocol="responses", provider="custom", api_key="k", model="glm", custom_base="https://proxy.test/v1"
            )
        self.assertFalse(result["ok"])
        self.assertEqual(result["error_code"], "PROTOCOL_UNSUPPORTED")
        self.assertFalse(result["retryable"])
        self.assertEqual(result["protocol"], "responses")
        self.assertIn("OpenAI Responses", result["message"])

    def test_protocol_probe_keeps_transient_failures_retryable(self):
        error = HTTPError(
            "https://proxy.test/v1/responses", 503, "Service Unavailable", {},
            io.BytesIO(b'{"error":{"message":"the upstream load is saturated"}}'),
        )
        with patch("oneagent.providers.urlopen", side_effect=error):
            result = protocol_probe(
                protocol="responses", provider="custom", api_key="k", model="m", custom_base="https://proxy.test/v1"
            )
        self.assertFalse(result["ok"])
        self.assertNotEqual(result["error_code"], "PROTOCOL_UNSUPPORTED")
        self.assertTrue(result["retryable"])
        self.assertEqual(result["protocol"], "responses")

    def test_protocol_probe_success_reports_the_protocol_label(self):
        with patch("oneagent.providers.urlopen", return_value=FakeResponse(b"{}", 200)):
            result = protocol_probe(
                protocol="anthropic", provider="custom", api_key="k", model="m", custom_base="https://proxy.test/v1"
            )
        self.assertTrue(result["ok"])
        self.assertEqual(result["protocol"], "anthropic")
        self.assertIn("Anthropic Messages", result["message"])

    def test_unreadable_error_body_falls_back_to_generic_handling(self):
        class Unreadable(io.BytesIO):
            def read(self, *_args):
                raise OSError("stream closed")

        error = HTTPError("https://proxy.test/v1/responses", 400, "Bad Request", {}, Unreadable(b""))
        with patch("oneagent.providers.urlopen", side_effect=error):
            result = protocol_probe(
                protocol="responses", provider="custom", api_key="k", model="m", custom_base="https://proxy.test/v1"
            )
        self.assertFalse(result["ok"])
        self.assertNotEqual(result["error_code"], "PROTOCOL_UNSUPPORTED")


class ProviderEdgeTests(unittest.TestCase):
    def test_url_and_provider_validation_edges(self):
        for value in ("", "ftp://example.com", "https:///missing-host"):
            with self.subTest(value=value), self.assertRaises(OneAgentError):
                validate_base_url(value)
        self.assertEqual(provider_base("ppio"), "https://api.ppio.com/openai")
        self.assertEqual(provider_base("custom", "http://127.0.0.1:9000/"), "http://127.0.0.1:9000")
        with self.assertRaises(OneAgentError):
            provider_base("custom")
        self.assertEqual(provider_config_base("ppio", "", "claude-code"), "https://api.ppio.com/anthropic")
        self.assertEqual(provider_config_base("novita", "", "claude-code"), "https://api.novita.ai/anthropic")
        self.assertEqual(provider_config_base("ppio", "https://override.example.com", "claude-code"), "https://override.example.com")
        self.assertEqual(provider_config_base("custom", "http://127.0.0.1:9000", "claude-code"), "http://127.0.0.1:9000")
        with self.assertRaises(OneAgentError):
            provider_base("unknown")
        with self.assertRaises(OneAgentError):
            provider_home("custom")
        self.assertEqual(openai_base_url("https://example.com/v1/responses"), "https://example.com/v1")
        self.assertEqual(openai_base_url("https://example.com/v1/models"), "https://example.com/v1")

    def test_fallback_probe_model_is_provider_specific_and_never_a_foreign_id(self):
        # The probe gates the wizard, so the fallback model must be one the
        # provider actually serves. It used to be a hardcoded "gpt-4.1", which
        # exists on neither built-in provider: every connection test failed with
        # an auth error and no correct key could get past the provider step.
        # The IDs also differ per provider, so one shared constant cannot work.
        # The fallback is now only the narrow path behind resolve_probe_model.
        self.assertNotEqual(catalog.fallback_probe_model("ppio"), catalog.fallback_probe_model("novita"))
        for provider in ("ppio", "novita"):
            with self.subTest(provider=provider):
                model = catalog.fallback_probe_model(provider)
                self.assertTrue(model)
                self.assertNotIn("gpt-", model)
                self.assertEqual(model, catalog.PROVIDERS[provider]["fallback_probe_model"])
        # An unknown provider still yields a usable ID rather than an empty
        # model, which would send a request no endpoint can answer.
        self.assertTrue(catalog.fallback_probe_model("custom"))

    def test_install_resolves_an_empty_model_before_writing_configs(self):
        # InstallOptions defaults to no model; writing "" into an Agent config
        # would leave a file that looks configured but cannot serve a request.
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            runtime = Runtime(
                home=home,
                os_id="macos",
                runner=lambda *a, **k: subprocess.CompletedProcess(a[0] if a else [], 0, "1.0.0", ""),
                which=lambda name: f"/usr/bin/{name}",
                env={},
            )
            result = install_many(
                InstallOptions(agents=["codex"], provider="ppio", api_key="sk-test", skip_test=True, home=home),
                runtime,
            )
            self.assertTrue(result["ok"], result)
            written = tomllib.loads((home / ".codex" / "config.toml").read_text(encoding="utf-8"))
        self.assertEqual(written["model"], catalog.fallback_probe_model("ppio"))

    def test_probe_success_default_model_and_network_error(self):
        with patch("oneagent.providers.urlopen", return_value=FakeResponse(status=204)) as request:
            result = chat_probe(provider="ppio", api_key="key", model="")
        self.assertTrue(result["ok"])
        sent = json.loads(request.call_args.args[0].data.decode())
        self.assertEqual(sent["model"], catalog.fallback_probe_model("ppio"))
        with patch("oneagent.providers.urlopen", side_effect=URLError("connection refused")):
            result = chat_probe(provider="ppio", api_key="key", model="m")
        self.assertEqual(result["error_code"], "PROVIDER_UNREACHABLE")
        with patch("oneagent.providers.urlopen", side_effect=URLError(socket.timeout("timed out"))):
            self.assertEqual(chat_probe(provider="ppio", api_key="key", model="m")["error_code"], "TIMEOUT")
        with self.assertRaises(OneAgentError):
            chat_probe(provider="ppio", api_key="", model="m")

    def test_pick_chat_model_prefers_chat_capable_ids(self):
        mixed = [
            "whisper-large-v3",
            "bge-reranker-v2",
            "text-embeddings-large",
            "qwen-vl-max",
            "deepseek-v3",
            "gpt-5.6-terra",
        ]
        self.assertEqual(pick_chat_model(mixed), "deepseek-v3")
        # Markers match on separators only, so IDs that merely contain a
        # marker substring stay eligible.
        self.assertEqual(pick_chat_model(["resolver-1", "evolve-chat"]), "resolver-1")
        # When every listed model looks non-chat, the first entry is the least
        # surprising choice; the probe failure will explain itself.
        self.assertEqual(pick_chat_model(["text-embedding-3-small", "whisper-1"]), "text-embedding-3-small")

    def test_resolve_probe_model_prefers_live_ids_without_extra_round_trips(self):
        # A user-chosen model short-circuits discovery: once the user has
        # decided, probing must not cost an extra request.
        with patch("oneagent.providers.list_models", side_effect=AssertionError("must not run")):
            self.assertEqual(resolve_probe_model(provider="ppio", api_key="k", model="chosen"), "chosen")
        # Without a key, discovery is skipped outright (list_models refuses it).
        with patch("oneagent.providers.list_models", side_effect=AssertionError("must not run")):
            self.assertEqual(resolve_probe_model(provider="ppio", api_key=""), catalog.fallback_probe_model("ppio"))
        # Discovery success: the first chat-capable model, not the list head.
        listing = {"ok": True, "models": ["text-embedding-3-small", "deepseek-v3"]}
        with patch("oneagent.providers.list_models", return_value=listing) as discovery:
            self.assertEqual(resolve_probe_model(provider="ppio", api_key="k"), "deepseek-v3")
        discovery.assert_called_once()
        # Discovery failing or empty falls back to the catalog ID, never raises.
        for stale in ({"ok": False, "models": []}, {"ok": True, "models": []}):
            with self.subTest(stale=stale), patch("oneagent.providers.list_models", return_value=stale):
                self.assertEqual(
                    resolve_probe_model(provider="novita", api_key="k"), catalog.fallback_probe_model("novita")
                )

    def test_install_resolves_an_empty_model_from_live_discovery(self):
        ok_probe = {"ok": True, "message": "ok", "error_code": None, "retryable": False, "protocol": "responses"}
        listing = {"ok": True, "models": ["text-embedding-3-small", "moonshot-v1"]}
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            runtime = Runtime(
                home=home,
                os_id="macos",
                runner=lambda *a, **k: subprocess.CompletedProcess(a[0] if a else [], 0, "1.0.0", ""),
                which=lambda name: f"/usr/bin/{name}",
                env={},
            )
            with (
                patch("oneagent.installer.protocol_probe", return_value=ok_probe),
                patch("oneagent.providers.list_models", return_value=listing) as discovery,
            ):
                result = install_many(
                    InstallOptions(agents=["codex"], provider="ppio", api_key="sk-test", home=home), runtime
                )
            self.assertTrue(result["ok"], result)
            written = tomllib.loads((home / ".codex" / "config.toml").read_text(encoding="utf-8"))
        discovery.assert_called_once()
        self.assertEqual(written["model"], "moonshot-v1")

    def test_install_falls_back_when_discovery_fails_and_never_under_skip_test(self):
        # Discovery failure keeps the install possible: the catalog fallback
        # goes into the config, and the probe (patched) decides the outcome.
        # Under skip_test no discovery attempt may happen at all.
        ok_probe = {"ok": True, "message": "ok", "error_code": None, "retryable": False, "protocol": "responses"}
        for skip_test in (False, True):
            with self.subTest(skip_test=skip_test):
                with tempfile.TemporaryDirectory() as tmp:
                    home = Path(tmp)
                    runtime = Runtime(
                        home=home,
                        os_id="macos",
                        runner=lambda *a, **k: subprocess.CompletedProcess(a[0] if a else [], 0, "1.0.0", ""),
                        which=lambda name: f"/usr/bin/{name}",
                        env={},
                    )
                    with (
                        patch("oneagent.installer.protocol_probe", return_value=ok_probe),
                        patch(
                            "oneagent.providers.list_models",
                            side_effect=AssertionError("discovery must not run") if skip_test else None,
                            return_value={"ok": False, "models": []},
                        ),
                    ):
                        result = install_many(
                            InstallOptions(
                                agents=["codex"], provider="ppio", api_key="sk-test", home=home, skip_test=skip_test
                            ),
                            runtime,
                        )
                    self.assertTrue(result["ok"], result)
                    written = tomllib.loads((home / ".codex" / "config.toml").read_text(encoding="utf-8"))
                self.assertEqual(written["model"], catalog.fallback_probe_model("ppio"))

    def test_model_response_shapes_and_errors(self):
        with patch("oneagent.providers.urlopen", return_value=FakeResponse(b'["a",{"id":"b"},{"ignored":true}]')):
            self.assertEqual(list_models(provider="ppio", api_key="key")["models"], ["a", "b"])
        with patch("oneagent.providers.urlopen", return_value=FakeResponse(b'{"data":{"id":"bad"}}')):
            result = list_models(provider="ppio", api_key="key")
        self.assertEqual(result["error_code"], "MODELS_UNSUPPORTED")
        with patch("oneagent.providers.urlopen", return_value=FakeResponse(b"not-json")):
            result = list_models(provider="ppio", api_key="key")
        self.assertEqual(result["error_code"], "MODELS_UNSUPPORTED")
        with self.assertRaises(OneAgentError):
            list_models(provider="ppio", api_key="")

        error = HTTPError("https://example.com", 401, "Unauthorized", {}, io.BytesIO())
        with patch("oneagent.providers.urlopen", side_effect=error):
            result = list_models(provider="ppio", api_key="key")
        self.assertIn("Enter model ID manually", result["message"])


class InstallerEdgeTests(unittest.TestCase):
    def test_backup_collision_and_profile_corruption(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "config.json"
            path.write_text("one", encoding="utf-8")
            (Path(tmp) / "config.json.backup-fixed").write_text("old", encoding="utf-8")
            with patch("oneagent.installer._timestamp", return_value="fixed"):
                backup = backup_file(path)
            self.assertEqual(backup.name, "config.json.backup-fixed-1")

            runtime = Runtime.create(home=Path(tmp), os_id="linux")
            profile = Path(tmp) / ".oneagent" / "profile.json"
            profile.parent.mkdir()
            profile.write_text("{", encoding="utf-8")
            self.assertIsNotNone(load_profile(runtime)[1])
            profile.write_text('{"schema_version":2}', encoding="utf-8")
            self.assertEqual(load_profile(runtime)[1], "Unsupported environment profile schema")

    def test_acl_and_atomic_write_fail_before_publish(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="windows", which=lambda _name: None)
            with self.assertRaises(OneAgentError):
                _run_acl(runtime, Path(tmp), directory=True)

            calls = 0

            def runner(args, **_kwargs):
                nonlocal calls
                calls += 1
                return subprocess.CompletedProcess(args, 1 if calls >= 6 else 0, "", "")

            runtime = Runtime.create(
                home=Path(tmp),
                os_id="windows",
                runner=runner,
                which=lambda name: "icacls.exe" if name == "icacls" else None,
                env={"USERNAME": "tester"},
            )
            target = Path(tmp) / "secret.txt"
            target.write_text("original", encoding="utf-8")
            with self.assertRaises(OneAgentError):
                atomic_write(runtime, target, "replacement", secret=True)
            self.assertEqual(target.read_text(encoding="utf-8"), "original")
            leaked = [path for path in Path(tmp).iterdir() if path.is_file() and path.read_text(encoding="utf-8") == "replacement"]
            self.assertEqual(leaked, [])

            unix = Runtime.create(home=Path(tmp), os_id="linux")
            with patch("oneagent.installer.tempfile.NamedTemporaryFile", side_effect=OSError("disk full")):
                with self.assertRaises(OneAgentError) as error:
                    atomic_write(unix, Path(tmp) / "failed.txt", "value")
            self.assertEqual(error.exception.code, "CONFIG_WRITE_FAILED")

    def test_secret_backup_and_temporary_cleanup_failures_are_structured(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="linux")
            target = Path(tmp) / ".oneagent" / "secret"
            target.parent.mkdir()
            target.write_text("old", encoding="utf-8")
            real_secure_path = installer_module.secure_path

            def fail_backup(runtime_arg, path, *, directory):
                if ".backup-" in path.name:
                    raise OneAgentError("CONFIG_WRITE_FAILED", "backup ACL failed")
                return real_secure_path(runtime_arg, path, directory=directory)

            with patch("oneagent.installer.secure_path", side_effect=fail_backup):
                with self.assertRaisesRegex(OneAgentError, "backup ACL failed"):
                    atomic_write(runtime, target, "new", secret=True)
            self.assertEqual(list(target.parent.glob("secret.backup-*")), [])

            with patch("oneagent.installer.secure_path", side_effect=fail_backup), patch.object(
                Path,
                "unlink",
                side_effect=OSError("backup locked"),
            ):
                with self.assertRaisesRegex(OneAgentError, "Cannot remove insecure secret backup"):
                    atomic_write(runtime, target, "new", secret=True)

        for secret in [False, True]:
            with self.subTest(secret=secret), tempfile.TemporaryDirectory() as tmp:
                runtime = Runtime.create(home=Path(tmp), os_id="linux")
                calls = 0

                def fail_temporary(runtime_arg, path, *, directory):
                    nonlocal calls
                    calls += 1
                    if calls == 2:
                        raise OneAgentError("CONFIG_WRITE_FAILED", "temporary ACL failed")
                    return real_secure_path(runtime_arg, path, directory=directory)

                with patch("oneagent.installer.secure_path", side_effect=fail_temporary), patch.object(
                    Path,
                    "unlink",
                    side_effect=OSError("temporary locked"),
                ):
                    expected = "temporary secret file" if secret else "temporary file"
                    with self.assertRaisesRegex(OneAgentError, expected):
                        atomic_write(runtime, Path(tmp) / ".oneagent" / "new", "value", secret=secret)

    def test_install_requires_a_known_provider_and_preserves_explicit_base_override(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="linux", which=lambda _name: None)
            with self.assertRaisesRegex(OneAgentError, "Provider must be"):
                install_many(
                    InstallOptions(
                        agents=["codex"],
                        provider="unknown",
                        api_base_url="https://example.com/openai",
                        api_key="key",
                        skip_test=True,
                    ),
                    runtime,
                )

            install_many(
                InstallOptions(
                    agents=["codex"],
                    provider="ppio",
                    api_base_url="https://stale-custom.example.com/openai",
                    api_key="key",
                    skip_test=True,
                ),
                runtime,
            )
            config = (Path(tmp) / ".codex" / "config.toml").read_text(encoding="utf-8")
            self.assertIn("https://stale-custom.example.com/openai", config)

    def test_json_config_merge_and_validation(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "config.json"
            path.write_text("[]", encoding="utf-8")
            with self.assertRaises(OneAgentError):
                _load_json(path)
            path.write_text("{", encoding="utf-8")
            with self.assertRaises(OneAgentError):
                _load_json(path)
            path.write_text('{"keep":true,"provider":{"other":{}}}', encoding="utf-8")
            runtime = Runtime.create(home=Path(tmp), os_id="linux")
            meta = {"config_path": "config.json"}
            write_openai_compatible_config(runtime, meta, "Provider", "https://example.com/openai", "model-a", "schema")
            data = json.loads(path.read_text(encoding="utf-8"))
            self.assertTrue(data["keep"])
            self.assertIn("other", data["provider"])
            self.assertEqual(data["model"], "oneagent/model-a")

            path.write_text('{"env":[]}', encoding="utf-8")
            with self.assertRaises(OneAgentError):
                write_claude_config(runtime, {"config_path": "config.json"}, "https://example.com", "key", "model")
            path.write_text('{"provider":[]}', encoding="utf-8")
            with self.assertRaises(OneAgentError):
                write_openai_compatible_config(runtime, meta, "Provider", "https://example.com/openai", "model-a", "schema")

    def test_codex_toml_merge_preserves_unmanaged_settings(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="linux")
            path = Path(tmp) / ".codex" / "config.toml"
            path.parent.mkdir()
            path.write_text(
                'approval_policy = "on-request"\n'
                'model_provider = "old"\n'
                'model = "old-model"\n\n'
                '[model_providers.old]\n'
                'name = "Keep Me"\n\n'
                '[model_providers.oneagent]\n'
                'name = "Old OneAgent"\n'
                'base_url = "https://old.example.com"\n',
                encoding="utf-8",
            )
            write_codex_config(
                runtime,
                {"config_path": ".codex/config.toml"},
                "PPIO",
                "https://api.ppio.com/openai",
                "model-a",
            )
            merged = tomllib.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(merged["approval_policy"], "on-request")
            self.assertEqual(merged["model"], "model-a")
            self.assertEqual(merged["model_providers"]["old"]["name"], "Keep Me")
            self.assertEqual(merged["model_providers"]["oneagent"]["base_url"], "https://api.ppio.com/openai")

            invalid = 'model = "unterminated\n'
            path.write_text(invalid, encoding="utf-8")
            with self.assertRaises(OneAgentError):
                write_codex_config(runtime, {"config_path": ".codex/config.toml"}, "PPIO", "https://example.com", "m")
            self.assertEqual(path.read_text(encoding="utf-8"), invalid)

            path.write_text('model = "old"\n', encoding="utf-8")
            with patch.object(Path, "read_text", side_effect=OSError("blocked")):
                with self.assertRaisesRegex(OneAgentError, "Cannot read existing TOML"):
                    write_codex_config(runtime, {"config_path": ".codex/config.toml"}, "PPIO", "https://example.com", "m")

            for unsupported in [
                '["model_providers"."oneagent"]\nname = "quoted table"\n',
                '"model" = "quoted key"\n',
            ]:
                path.write_text(unsupported, encoding="utf-8")
                with self.subTest(toml=unsupported), self.assertRaises(OneAgentError):
                    write_codex_config(runtime, {"config_path": ".codex/config.toml"}, "PPIO", "https://example.com", "m")

            with self.assertRaisesRegex(OneAgentError, "Cannot merge TOML"):
                _merge_codex_toml('keep = true\n', 'model = "unterminated\n', path)

    def test_windows_aider_config_and_unsupported_adapter(self):
        with tempfile.TemporaryDirectory() as tmp:
            runner = lambda args, **_kwargs: subprocess.CompletedProcess(args, 0, "", "")
            runtime = Runtime.create(
                home=Path(tmp),
                os_id="windows",
                runner=runner,
                which=lambda name: "icacls.exe" if name == "icacls" else None,
                env={"USERNAME": "tester"},
            )
            path = write_aider_config(
                runtime,
                {"config_path": ".oneagent/aider.env", "windows_config_path": ".oneagent/aider.ps1"},
                "https://example.com/openai",
                "key'quoted",
            )
            text = path.read_text(encoding="utf-8")
            self.assertIn("$env:OPENAI_API_BASE", text)
            self.assertIn("key''quoted", text)
            with self.assertRaises(OneAgentError):
                _write_agent_config(runtime, "unsupported", {"config_path": "x"}, "P", "https://example.com", "key", "m")

    def test_version_and_install_manager_edges(self):
        self.assertIsNone(_version_from_output("no version"))
        meta = {"name": "Agent", "command": "agent", "version_args": ["--version"], "package": {"manager": "npm", "name": "agent", "version": "1.2.3"}}
        runtime = Runtime.create(home=Path("/tmp"), os_id="linux", which=lambda name: "/bin/agent" if name == "agent" else "/bin/npm" if name == "npm" else None)
        runtime.runner = lambda *_args, **_kwargs: (_ for _ in ()).throw(OSError("boom"))
        self.assertIsNone(installed_version(runtime, meta))

        runtime.runner = lambda args, **_kwargs: subprocess.CompletedProcess(args, 0, "Agent 1.2.3", "")
        self.assertFalse(install_locked_agent(runtime, "agent", meta, enforce_locked=False, latest=False, timeout=1)["installed"])
        self.assertFalse(install_locked_agent(runtime, "agent", meta, enforce_locked=True, latest=False, timeout=1)["installed"])

        uv_meta = {"name": "Aider", "command": "", "package": {"manager": "uv", "name": "aider-chat", "version": "0.1.0"}}
        uv_runtime = Runtime.create(
            home=Path("/tmp"),
            os_id="windows",
            which=lambda name: {"uv": "uv.exe", "python3.12": "python3.12.exe"}.get(name),
        )
        calls = []
        uv_runtime.runner = lambda args, **_kwargs: calls.append(args) or subprocess.CompletedProcess(args, 0, "", "")
        result = install_locked_agent(uv_runtime, "aider", uv_meta, enforce_locked=False, latest=True, timeout=1)
        self.assertTrue(result["installed"])
        self.assertEqual(
            calls[0],
            [
                "uv.exe",
                "tool",
                "install",
                "--force",
                "--python",
                "python3.12.exe",
                "--no-python-downloads",
                "aider-chat",
            ],
        )
        self.assertIsNone(result["version"])

        with self.assertRaises(OneAgentError):
            _python_312_for_uv(Runtime.create(home=Path("/tmp"), os_id="linux", which=lambda _name: None))
        with self.assertRaisesRegex(OneAgentError, "uv is required"):
            install_locked_agent(
                Runtime.create(home=Path("/tmp"), os_id="linux", which=lambda _name: None),
                "aider",
                uv_meta,
                enforce_locked=False,
                latest=False,
                timeout=1,
            )
        bad_meta = {"name": "Bad", "command": "", "package": {"manager": "curl", "name": "bad", "version": "1.0.0"}}
        with self.assertRaises(OneAgentError):
            install_locked_agent(runtime, "bad", bad_meta, enforce_locked=False, latest=False, timeout=1)

    def test_uv_python_312_resolution_paths(self):
        generic = Runtime.create(
            home=Path("/tmp"),
            os_id="linux",
            which=lambda name: "/bin/python3" if name == "python3" else None,
        )
        generic.runner = lambda args, **_kwargs: subprocess.CompletedProcess(args, 0, "Python 3.12.9", "")
        self.assertEqual(_python_312_for_uv(generic), "/bin/python3")

        fallback = Runtime.create(
            home=Path("/tmp"),
            os_id="linux",
            which=lambda name: {"python3": "/bin/python3", "python": "/bin/python"}.get(name),
        )
        fallback.runner = lambda args, **_kwargs: subprocess.CompletedProcess(
            args,
            1 if args[0] == "/bin/python3" else 0,
            "Python 3.14.0" if args[0] == "/bin/python3" else "Python 3.12.8",
            "",
        )
        self.assertEqual(_python_312_for_uv(fallback), "/bin/python")

        windows = Runtime.create(
            home=Path("C:/Users/Test"),
            os_id="windows",
            which=lambda name: {"python3": "python3.exe", "py": "py.exe"}.get(name),
        )

        def windows_runner(args, **_kwargs):
            if args[0] == "python3.exe":
                raise OSError("not runnable")
            return subprocess.CompletedProcess(args, 0, "Python 3.12.7", "")

        windows.runner = windows_runner
        self.assertEqual(_python_312_for_uv(windows), "3.12")

        windows.runner = lambda *_args, **_kwargs: (_ for _ in ()).throw(subprocess.TimeoutExpired("py", 1))
        with self.assertRaises(OneAgentError):
            _python_312_for_uv(windows)

        no_launcher = Runtime.create(home=Path("C:/Users/Test"), os_id="windows", which=lambda _name: None)
        with self.assertRaises(OneAgentError):
            _python_312_for_uv(no_launcher)

    def test_installer_timeout_oserror_and_nonzero(self):
        meta = {"name": "Agent", "command": "", "package": {"manager": "npm", "name": "agent", "version": "1.0.0"}}
        runtime = Runtime.create(home=Path("/tmp"), os_id="linux", which=lambda name: "/bin/npm" if name == "npm" else None)
        for error, code in [(subprocess.TimeoutExpired("npm", 1), "TIMEOUT"), (OSError("boom"), "AGENT_INSTALL_FAILED")]:
            runtime.runner = lambda *_args, error=error, **_kwargs: (_ for _ in ()).throw(error)
            with self.subTest(code=code), self.assertRaises(OneAgentError) as raised:
                install_locked_agent(runtime, "agent", meta, enforce_locked=False, latest=False, timeout=1)
            self.assertEqual(raised.exception.code, code)
        runtime.runner = lambda args, **_kwargs: subprocess.CompletedProcess(args, 9, "", "failed")
        with self.assertRaises(OneAgentError) as raised:
            install_locked_agent(runtime, "agent", meta, enforce_locked=False, latest=False, timeout=1)
        self.assertEqual(raised.exception.code, "AGENT_INSTALL_FAILED")
        self.assertIn("failed", raised.exception.message)

        runtime.runner = lambda args, **_kwargs: subprocess.CompletedProcess(args, 9, "", "")
        with self.assertRaises(OneAgentError) as empty_error:
            install_locked_agent(runtime, "agent", meta, enforce_locked=False, latest=False, timeout=1)
        self.assertEqual(empty_error.exception.message, "Installing Agent failed with exit code 9")

        secret_runtime = Runtime.create(
            home=Path("/tmp"),
            os_id="linux",
            which=lambda name: "/bin/npm" if name == "npm" else None,
            env={"OPENAI_API_KEY": "sentinel-install-key"},
        )
        secret_runtime.runner = lambda args, **_kwargs: subprocess.CompletedProcess(
            args,
            7,
            "",
            "install failed for sentinel-install-key",
        )
        with self.assertRaises(OneAgentError) as secret_error:
            install_locked_agent(secret_runtime, "agent", meta, enforce_locked=False, latest=False, timeout=1)
        self.assertNotIn("sentinel-install-key", secret_error.exception.message)
        self.assertIn("[redacted]", secret_error.exception.message)

    def test_uv_install_hint_and_status_prerequisite_failure(self):
        with tempfile.TemporaryDirectory() as tmp:
            missing = Runtime.create(home=Path(tmp), os_id="linux", which=lambda _name: None)
            result = install_many(InstallOptions(agents=["aider"], configure=False), missing)
            self.assertIn("uv tool install", result["log"])
            self.assertIn("--no-python-downloads", result["log"])

            uv_only = Runtime.create(
                home=Path(tmp),
                os_id="linux",
                which=lambda name: "/bin/uv" if name == "uv" else None,
            )
            status = status_payload(uv_only)
            self.assertFalse(status["agents"]["aider"]["canInstall"])

            unsupported = {
                "custom": {
                    "name": "Custom",
                    "group": "auto",
                    "command": "custom",
                    "config_mode": "auto",
                    "config_path": ".custom/config",
                    "platforms": ["linux"],
                    "package": {"manager": "unsupported", "name": "custom", "version": "1.0.0"},
                }
            }
            with patch("oneagent.installer.agent_catalog", return_value=unsupported):
                unsupported_result = install_many(
                    InstallOptions(agents=["custom"], configure=False),
                    missing,
                )
            self.assertIn("Unsupported package manager", unsupported_result["log"])

    def test_all_auto_config_adapters_and_check_only_paths(self):
        with tempfile.TemporaryDirectory() as tmp:
            commands = {"codex": "/bin/codex", "claude": "/bin/claude", "opencode": "/bin/opencode", "kilo": "/bin/kilo", "aider": "/bin/aider"}
            runtime = Runtime.create(home=Path(tmp), os_id="linux", which=lambda name: commands.get(name))
            runtime.runner = lambda args, **_kwargs: subprocess.CompletedProcess(args, 0, "version 1.0.0", "")
            result = install_many(
                InstallOptions(
                    agents=["codex", "claude-code", "opencode", "kilo-cli", "aider"],
                    provider="ppio",
                    api_key="secret",
                    model="model-a",
                    skip_test=True,
                ),
                runtime,
            )
            self.assertTrue(result["ok"])
            self.assertEqual({item["status"] for item in result["results"]}, {"configured"})
            claude = json.loads((Path(tmp) / ".claude" / "settings.json").read_text(encoding="utf-8"))
            self.assertEqual(claude["env"]["ANTHROPIC_BASE_URL"], "https://api.ppio.com/anthropic")
            self.assertEqual(claude["env"]["ANTHROPIC_SMALL_FAST_MODEL"], "model-a")
            self.assertTrue((Path(tmp) / ".config" / "kilo" / "kilo.jsonc").exists())
            self.assertTrue((Path(tmp) / ".oneagent" / "aider.env").exists())

            checked = install_many(InstallOptions(agents=["codex"], configure=False, check_agent_only=True), runtime)
            self.assertEqual(checked["results"][0]["status"], "installed")
            missing_runtime = Runtime.create(home=Path(tmp), os_id="linux", which=lambda _name: None)
            checked = install_many(InstallOptions(agents=["codex"], configure=False, check_agent_only=True), missing_runtime)
            self.assertEqual(checked["results"][0]["status"], "skipped")

    def test_jsonc_comments_are_reported_as_comments_not_invalid_json(self):
        # OpenCode and Kilo ship .jsonc, where comments are legal. OneAgent
        # rewrites with json.dumps and cannot keep them, so it must refuse --
        # but calling a valid JSONC file "invalid JSON" tells the user nothing
        # actionable.
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            config = home / ".config" / "opencode" / "opencode.jsonc"
            config.parent.mkdir(parents=True)
            original = '{\n  // keep my own settings\n  "theme": "dark"\n}\n'
            config.write_text(original, encoding="utf-8")
            runtime = Runtime.create(home=home, os_id="linux", which=lambda _name: None)
            probe = {"ok": True, "message": "ok", "error_code": None, "retryable": False, "protocol": "openai"}
            with patch("oneagent.installer.protocol_probe", return_value=probe):
                result = install_many(
                    InstallOptions(agents=["opencode"], api_key="k", model="m"), runtime
                )
            failure = result["results"][0]
            self.assertEqual(failure["error_code"], "CONFIG_WRITE_FAILED")
            self.assertIn("JSONC comments", failure["message"])
            # The user's file must survive untouched.
            self.assertEqual(config.read_text(encoding="utf-8"), original)

            # A .jsonc that is broken for some other reason keeps the generic message.
            config.write_text('{"theme": "dark",,}', encoding="utf-8")
            with patch("oneagent.installer.protocol_probe", return_value=probe):
                result = install_many(
                    InstallOptions(agents=["opencode"], api_key="k", model="m"), runtime
                )
            self.assertIn("invalid", result["results"][0]["message"])
            self.assertNotIn("JSONC comments", result["results"][0]["message"])

            # An existing but empty config is a fresh start, not a parse error.
            config.write_text("   \n", encoding="utf-8")
            with patch("oneagent.installer.protocol_probe", return_value=probe):
                result = install_many(
                    InstallOptions(agents=["opencode"], api_key="k", model="m"), runtime
                )
            self.assertEqual(result["results"][0]["status"], "configured")
            self.assertEqual(json.loads(config.read_text(encoding="utf-8"))["model"], "oneagent/m")

    def test_protocol_mismatch_refuses_to_write_agent_config(self):
        # A model that answers Chat Completions can still reject Responses. If
        # OneAgent wrote the Codex config anyway, the failure would surface as an
        # opaque error inside Codex on the user's first real request.
        unsupported = {
            "ok": False,
            "reachable": True,
            "protocol": "responses",
            "status": 400,
            "message": "Model 'glm-5.2' does not support OpenAI Responses.",
            "error_code": "PROTOCOL_UNSUPPORTED",
            "retryable": False,
        }
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            runtime = Runtime.create(home=home, os_id="linux", which=lambda _name: None)
            with patch("oneagent.installer.protocol_probe", return_value=unsupported), patch(
                "oneagent.installer.list_models", return_value={"ok": False, "models": []}
            ):
                result = install_many(
                    InstallOptions(agents=["codex"], api_key="key", model="glm-5.2"), runtime
                )
        self.assertFalse(result["ok"])
        self.assertEqual(result["results"][0]["error_code"], "PROTOCOL_UNSUPPORTED")
        self.assertFalse(result["results"][0]["retryable"])
        self.assertEqual(result["code"], 7)
        self.assertFalse((home / ".codex" / "config.toml").exists())
        self.assertFalse((home / ".oneagent" / "profile.json").exists())

    def test_each_agent_is_probed_over_its_own_protocol(self):
        seen: list[str] = []

        def fake_probe(*, protocol, **_kwargs):
            seen.append(protocol)
            return {"ok": True, "message": "ok", "error_code": None, "retryable": False, "protocol": protocol}

        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="linux", which=lambda _name: None)
            with patch("oneagent.installer.protocol_probe", side_effect=fake_probe):
                result = install_many(
                    InstallOptions(
                        agents=["codex", "claude-code", "opencode"], api_key="key", model="m"
                    ),
                    runtime,
                )
        # Codex speaks Responses, Claude Code speaks Anthropic Messages, OpenCode
        # speaks Chat Completions; shared protocols are probed once.
        self.assertEqual(sorted(seen), ["anthropic", "openai", "responses"])
        self.assertEqual(sorted(result["probes"]), ["anthropic", "openai", "responses"])
        self.assertTrue(result["ok"])

    def test_status_payload_can_install_edges(self):
        # On Windows, claude-code additionally requires git; both sides of that
        # `and` need exercising or the branch report stays partial.
        tools = {"npm": r"C:\node\npm.cmd", "git": r"C:\git\git.exe"}
        with_tools = Runtime.create(
            home=Path("/tmp"), os_id="windows", which=lambda name: tools.get(name)
        )
        self.assertTrue(status_payload(with_tools)["capabilities"]["canInstall"]["claude-code"])
        without_tools = Runtime.create(home=Path("/tmp"), os_id="windows", which=lambda _name: None)
        self.assertFalse(status_payload(without_tools)["capabilities"]["canInstall"]["claude-code"])

        # An OS no Agent declares support for must not claim any compatibility.
        unknown_os = Runtime.create(home=Path("/tmp"), os_id="freebsd", which=lambda _name: None)
        self.assertEqual(status_payload(unknown_os)["capabilities"]["supportedAgentIds"], [])

    def test_unknown_model_is_relabeled_from_model_discovery(self):
        # Endpoints refuse an unknown model with the same shapes they use for
        # an unsupported protocol, so a bare probe verdict reads "model does
        # not support <protocol>". When discovery works, the verdict must name
        # the real problem and show valid IDs instead.
        refused = {
            "ok": False,
            "reachable": True,
            "protocol": "responses",
            "status": 404,
            "message": "Model 'no-such' does not support OpenAI Responses.",
            "error_code": "PROTOCOL_UNSUPPORTED",
            "retryable": False,
        }
        listing = {"ok": True, "models": ["gpt-5.6-terra", "qwen3-coder", "model-a", "model-b", "model-c", "model-d"]}
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            runtime = Runtime.create(home=home, os_id="linux", which=lambda _name: None)
            with (
                patch("oneagent.installer.protocol_probe", return_value=refused),
                patch("oneagent.installer.list_models", return_value=listing) as discovery,
            ):
                result = install_many(
                    InstallOptions(agents=["codex"], api_key="key", model="no-such"), runtime
                )
        discovery.assert_called_once()
        failure = result["results"][0]
        self.assertEqual(failure["error_code"], "MODELS_UNSUPPORTED")
        self.assertEqual(result["code"], 6)
        self.assertIn("'no-such'", failure["message"])
        self.assertIn("gpt-5.6-terra", failure["message"])
        # The sample is capped so a huge model list cannot flood the message.
        self.assertNotIn("model-d", failure["message"])
        self.assertFalse((home / ".codex" / "config.toml").exists())

    def test_model_discovery_doubt_keeps_the_probe_verdict(self):
        # Discovery needs the network too. When it fails, finds nothing, or
        # lists the model, "wrong model" cannot be told from "unreachable
        # endpoint" or a genuine protocol gap -- so the original verdict stands.
        refused = {
            "ok": False,
            "reachable": False,
            "protocol": "openai",
            "status": 0,
            "message": "Cannot reach endpoint: connection refused",
            "error_code": "PROVIDER_UNREACHABLE",
            "retryable": True,
        }
        cases = [
            {"ok": False, "models": []},          # discovery itself failed
            {"ok": True, "models": []},           # endpoint lists no models
            {"ok": True, "models": ["model-a"]},  # model IS listed: not a typo
        ]
        for listing in cases:
            with self.subTest(listing=listing):
                with tempfile.TemporaryDirectory() as tmp:
                    runtime = Runtime.create(home=Path(tmp), os_id="linux", which=lambda _name: None)
                    with (
                        patch("oneagent.installer.protocol_probe", return_value=dict(refused)),
                        patch("oneagent.installer.list_models", return_value=listing),
                    ):
                        result = install_many(
                            InstallOptions(agents=["opencode"], api_key="key", model="model-a"), runtime
                        )
                self.assertEqual(result["results"][0]["error_code"], "PROVIDER_UNREACHABLE")
                self.assertTrue(result["results"][0]["retryable"])

    def test_model_discovery_is_skipped_when_every_probe_passed(self):
        probe = {"ok": True, "message": "ok", "error_code": None, "retryable": False, "protocol": "openai"}
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="linux", which=lambda _name: None)
            with (
                patch("oneagent.installer.protocol_probe", return_value=probe),
                patch("oneagent.installer.list_models", side_effect=AssertionError("discovery must not run")),
            ):
                result = install_many(
                    InstallOptions(agents=["opencode"], api_key="key", model="model-a"), runtime
                )
        self.assertTrue(result["ok"])

    def test_install_validation_platform_probe_and_windows_next_steps(self):
        runtime = Runtime.create(home=Path("/tmp"), os_id="linux", which=lambda _name: None)
        with self.assertRaises(OneAgentError):
            install_many(InstallOptions(agents=[]), runtime)
        with self.assertRaises(OneAgentError):
            install_many(InstallOptions(agents=["unknown"], configure=False), runtime)
        with self.assertRaises(OneAgentError):
            install_many(InstallOptions(agents=["codex"], profile_agents=["opencode"], configure=False), runtime)
        with self.assertRaises(OneAgentError):
            install_many(InstallOptions(agents=["codex"], timeout=0), runtime)
        with self.assertRaises(OneAgentError):
            install_many(InstallOptions(agents=["codex"], api_key="", configure=True), runtime)

        unsupported = {"agent": {"name": "Agent", "group": "auto", "command": "", "config_mode": "auto", "config_path": "x", "platforms": []}}
        with patch("oneagent.installer.agent_catalog", return_value=unsupported):
            result = install_many(InstallOptions(agents=["agent"], configure=False), runtime)
        self.assertEqual(result["results"][0]["error_code"], "PREREQUISITE_MISSING")

        with tempfile.TemporaryDirectory() as tmp, patch(
            "oneagent.installer.protocol_probe",
            return_value={"ok": False, "message": "provider failed", "error_code": "PROVIDER_UNREACHABLE", "retryable": True},
        ), patch("oneagent.installer.list_models", return_value={"ok": False, "models": []}):
            result = install_many(InstallOptions(agents=["codex"], api_key="key", model="m"), Runtime.create(home=Path(tmp), os_id="linux", which=lambda _name: None))
        self.assertFalse(result["ok"])
        self.assertIn("provider failed", result["log"])

        with tempfile.TemporaryDirectory() as tmp, patch(
            "oneagent.installer.protocol_probe",
            return_value={"ok": True, "message": "ok", "error_code": None, "retryable": False},
        ):
            successful_probe = install_many(
                InstallOptions(agents=["codex"], api_key="key", model="m"),
                Runtime.create(home=Path(tmp), os_id="linux", which=lambda _name: None),
            )
        self.assertTrue(successful_probe["ok"])
        self.assertTrue(successful_probe["probe"]["ok"])

        with tempfile.TemporaryDirectory() as tmp:
            failing_runtime = Runtime.create(home=Path(tmp), os_id="linux", which=lambda name: "/fake/npm" if name == "npm" else None)
            failing_runtime.runner = lambda args, **_kwargs: subprocess.CompletedProcess(args, 9, "", "failed")
            with patch("oneagent.installer.protocol_probe", return_value={"ok": False, "message": "provider failed", "error_code": "PROVIDER_UNREACHABLE", "retryable": True}):
                result = install_many(
                    InstallOptions(agents=["codex"], api_key="key", install_agent=True, skip_test=False),
                    failing_runtime,
                )
            self.assertEqual(result["code"], 4)

        with tempfile.TemporaryDirectory() as tmp, patch("oneagent.installer._next_step", return_value=""):
            no_next = install_many(
                InstallOptions(agents=["codex"], configure=False),
                Runtime.create(home=Path(tmp), os_id="linux", which=lambda _name: None),
            )
            self.assertEqual(no_next["next"], "")

        windows = Runtime.create(home=Path("C:/Users/Test"), os_id="windows")
        self.assertIn("env.ps1", _next_step(windows, "codex", "m"))
        self.assertIn("aider.ps1", _next_step(windows, "aider", "m"))
        self.assertEqual(_next_step(windows, "unknown", "m"), "")

        windows_status_runtime = Runtime.create(
            home=Path("C:/Users/Test"),
            os_id="windows",
            which=lambda name: f"C:/{name}.exe" if name in {"npm", "python", "git", "claude"} else None,
        )
        status = status_payload(windows_status_runtime)
        self.assertTrue(status["agents"]["claude-code"]["canInstall"])

        limited_catalog = {
            "limited": {
                "name": "Limited",
                "group": "guide",
                "command": "",
                "config_mode": "guide",
                "platforms": ["linux"],
                "guide": "guide",
            }
        }
        with patch("oneagent.installer.agent_catalog", return_value=limited_catalog), patch("oneagent.installer.public_catalog", return_value=[]):
            limited = status_payload(Runtime.create(home=Path("C:/Users/Test"), os_id="windows", which=lambda _name: None))
        self.assertEqual(limited["capabilities"]["supportedAgentIds"], [])


if __name__ == "__main__":
    unittest.main()
