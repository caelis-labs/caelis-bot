"""Exercise the packaged extension script with only Python on an isolated fixture."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import tomllib
import unittest


SCRIPT = Path(__file__).resolve().parent / "skills/bot-core/scripts/extensions.py"


class ExtensionsTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="Caelis extensions ")
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        self.workspace = root / "Notebook"
        self.workspace.mkdir()
        skill = root / "app-skills" / "bot-core"
        (skill / "scripts").mkdir(parents=True)
        (skill / "assets").mkdir()
        self.script = skill / "scripts" / SCRIPT.name
        shutil.copyfile(SCRIPT, self.script)
        self.catalog = [{"id": "obsidian", "title": "Obsidian", "version": "2.0.0"}]
        (skill / "assets" / "plugin-catalog.json").write_text(json.dumps(self.catalog), encoding="utf-8")
        self.candidate = self.workspace / "candidate.json"
        self.candidate.write_text('{"url":"https://example.test/mcp"}', encoding="utf-8")

    def invoke(self, *args, expected=0, cwd=None):
        result = subprocess.run([sys.executable, str(self.script), *args],
                                cwd=cwd or self.workspace, env={**os.environ, "PATH": ""},
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, expected, result.stderr)
        return json.loads(result.stdout) if not expected else result.stderr

    def test_concurrent_edits_preserve_every_acknowledged_server(self):
        from concurrent.futures import ThreadPoolExecutor
        for runtime in ("codex", "caelis"):
            with ThreadPoolExecutor(max_workers=6) as pool:
                results = list(pool.map(lambda index: self.invoke(
                    "mcp", runtime, "set", f"server{index}", self.candidate.name
                ), range(12)))
            self.assertEqual(results, [{"state": "saved"}] * 12)
            if runtime == "codex":
                config = self.workspace / ".codex" / "config.toml"
                servers = tomllib.loads(config.read_text())["mcp_servers"]
            else:
                servers = json.loads((self.workspace / ".mcp.json").read_bytes())["mcpServers"]
            self.assertEqual(set(servers), {f"server{i}" for i in range(12)})

    def test_codex_replaces_only_target_and_validates_toml(self):
        config = self.workspace / ".codex" / "config.toml"
        config.parent.mkdir()
        unmanaged = (b'# personal\r\n[features]\r\nexample = false\r\n[mcp_servers.existing]\r\n'
                     b'url = "https://example.test/existing"\r\n')
        sibling = b'[mcp_servers.sibling]\r\ncommand = "local"\r\n'
        original = (unmanaged + b'# BEGIN Caelis Bot workspace MCP\r\n' + sibling
                    + b'[mcp_servers.fixture]\r\nurl = "https://example.test/old"\r\n'
                    + b'# END Caelis Bot workspace MCP\r\n'
                    + b'# BEGIN Caelis Bot plugins\r\n[mcp_servers.p_01234567_git]\r\n'
                    + b'command = "fixture"\r\n# END Caelis Bot plugins\r\n')
        config.write_bytes(original)
        self.assertEqual(self.invoke("mcp", "codex", "set", "fixture", self.candidate.name), {"state": "saved"})
        updated = config.read_bytes()
        self.assertTrue(updated.startswith(unmanaged))
        self.assertIn(sibling, updated)
        self.assertIn(b'[mcp_servers.p_01234567_git]\r\n', updated)
        self.assertEqual(tomllib.loads(updated.decode())["mcp_servers"]["fixture"]["url"], "https://example.test/mcp")
        self.invoke("mcp", "codex", "remove", "existing", expected=1)
        self.assertEqual(config.read_bytes(), updated)
        self.invoke("mcp", "codex", "remove", "fixture")
        self.assertNotIn("fixture", tomllib.loads(config.read_text(encoding="utf-8"))["mcp_servers"])
        broken = b'[mcp_servers.broken\nsecret-marker'
        config.write_bytes(broken)
        self.assertNotIn("secret-marker", self.invoke("mcp", "codex", "set", "fixture", self.candidate.name, expected=1))
        self.assertEqual(config.read_bytes(), broken)

    def test_codex_ignores_markers_and_table_headers_inside_strings(self):
        config = self.workspace / ".codex" / "config.toml"
        config.parent.mkdir()
        unmanaged = (b'basic = """\r\n# BEGIN Caelis Bot workspace MCP\r\nquote: \\"""\r\n'
                     b'# END Caelis Bot workspace MCP\r\n""""\r\n'
                     b'literal = \'\'\'\r\n# END Caelis Bot workspace MCP\r\n\'\'\'\'\'\r\n'
                     b'values = [nan, -0.0]\r\n')
        sibling = (b'[mcp_servers.sibling]\r\nargs = ["""\r\n[mcp_servers.fixture]\r\n'
                   b'# END Caelis Bot workspace MCP\r\n"""]\r\n')
        config.write_bytes(unmanaged + b'# BEGIN Caelis Bot workspace MCP\r\n' + sibling
                           + b'[mcp_servers.fixture]\r\ncommand = "old"\r\n'
                           + b'# END Caelis Bot workspace MCP\r\n')
        for action, candidate in (("set", [self.candidate.name]), ("remove", [])):
            self.invoke("mcp", "codex", action, "fixture", *candidate)
            updated = config.read_bytes()
            self.assertTrue(updated.startswith(unmanaged))
            self.assertIn(sibling, updated)
            servers = tomllib.loads(updated.decode())["mcp_servers"]
            if action == "set":
                self.assertEqual(servers["fixture"], {"url": "https://example.test/mcp"})
            else:
                self.assertNotIn("fixture", servers)

    def test_codex_rejects_edits_that_change_unrelated_settings(self):
        config = self.workspace / ".codex" / "config.toml"
        config.parent.mkdir()
        block = (b'# BEGIN Caelis Bot workspace MCP\n[mcp_servers.fixture]\ncommand = "old"\n'
                 b'# END Caelis Bot workspace MCP\n')
        originals = (
            block + b'# BEGIN Caelis Bot workspace MCP\n',
            b'# END Caelis Bot workspace MCP\n',
            b'[features]\nexample = false\n' + block + b'enabled = false\n',
            block.replace(b'# END', b'[features]\nexample = false\n# END'),
        )
        for original in originals:
            with self.subTest(original=original):
                config.write_bytes(original)
                self.invoke("mcp", "codex", "remove", "fixture", expected=1)
                self.assertEqual(config.read_bytes(), original)

    def test_mcp_check_and_set_validate_http_ports(self):
        for runtime, path in (("codex", ".codex/config.toml"), ("caelis", ".mcp.json")):
            config = self.workspace / path
            for port in ("text", "-1", "65536"):
                with self.subTest(runtime=runtime, port=port):
                    self.candidate.write_text(json.dumps({"url": f"https://example.test:{port}/mcp"}))
                    self.invoke("mcp", runtime, "check", "fixture", self.candidate.name, expected=1)
                    self.invoke("mcp", runtime, "set", "fixture", self.candidate.name, expected=1)
                    self.assertFalse(config.exists())
            self.candidate.write_text('{"url":"https://[::1]:65535/mcp"}')
            self.invoke("mcp", runtime, "set", "fixture", self.candidate.name)
            self.assertTrue(config.is_file())

    def test_mcp_candidate_paths_are_relative_to_notebook(self):
        for runtime in ("codex", "caelis"):
            with self.subTest(runtime=runtime):
                args = ("mcp", runtime, "check", "fixture")
                self.assertEqual(self.invoke(*args, self.candidate.name), {"state": "validated"})
                for path in (str(self.candidate), "../Notebook/candidate.json"):
                    error = self.invoke(*args, path, expected=1)
                    self.assertIn("Notebook-relative JSON file", error)
        self.assertFalse((self.workspace / ".codex").exists())
        self.assertFalse((self.workspace / ".mcp.json").exists())

    def test_caelis_checks_candidate_and_preserves_siblings(self):
        config = self.workspace / ".mcp.json"
        original = b'{"other":4,"mcpServers":{"existing":{"disabled_option":true}}}'
        config.write_bytes(original)
        self.invoke("mcp", "caelis", "check", "fixture", self.candidate.name)
        self.assertEqual(config.read_bytes(), original)
        self.candidate.write_text('{"url":"file:///invalid"}', encoding="utf-8")
        self.invoke("mcp", "caelis", "set", "fixture", self.candidate.name, expected=1)
        self.assertEqual(config.read_bytes(), original)
        self.candidate.write_text('{"command":"fixture","args":["argument"],"cwd":"."}', encoding="utf-8")
        self.invoke("mcp", "caelis", "set", "fixture", self.candidate.name)
        self.assertEqual(json.loads(config.read_bytes()), {
            "other": 4, "mcpServers": {"existing": {"disabled_option": True},
                                      "fixture": {"command": "fixture", "args": ["argument"], "workDir": "."}}})
        self.invoke("mcp", "caelis", "remove", "fixture")
        self.assertEqual(json.loads(config.read_bytes()), json.loads(original))

    def test_plugin_request_uses_a_single_slot_and_authoritative_status(self):
        self.assertEqual(self.invoke("plugin", "catalog"), self.catalog)
        self.invoke("plugin", "update", "obsidian", expected=1)
        pending = self.invoke("plugin", "update", "obsidian", "2.0.0")
        self.assertEqual(pending["state"], "pending")
        directory = self.workspace / ".agents"
        request = directory / "bot-plugin-request.json"
        submitted = json.loads(request.read_bytes())
        self.assertEqual(submitted, {"id": pending["id"], "action": "update", "pluginId": "obsidian", "version": "2.0.0"})
        self.invoke("plugin", "uninstall", "obsidian", expected=1)
        self.assertEqual(json.loads(request.read_bytes()), submitted)
        self.assertEqual(self.invoke("plugin", "status", pending["id"]), pending)
        receipt = {**submitted, "state": "unknown"}
        status = directory / f"bot-plugin-status-{pending['id']}.json"
        status.write_text(json.dumps(receipt), encoding="utf-8")
        request.unlink()
        later = self.invoke("plugin", "install", "another-package")
        self.assertEqual(self.invoke("plugin", "status", pending["id"]), receipt)
        self.assertEqual(self.invoke("plugin", "status", later["id"]), later)
        confirmed = {**receipt, "state": "saved", "revision": 4}
        status.write_text(json.dumps(confirmed), encoding="utf-8")
        self.assertEqual(self.invoke("plugin", "status", pending["id"]), confirmed)
        self.assertEqual(self.invoke("plugin", "status", "missing"), {"id": "missing", "state": "unavailable"})
        self.invoke("plugin", "status", "../outside", expected=1)

    def test_workspace_is_fixed_to_the_installed_skill(self):
        other = self.workspace.parent / "Other" / "Notebook"
        other.mkdir(parents=True)
        self.invoke("plugin", "install", "obsidian", cwd=other, expected=1)
        self.assertFalse((other / ".agents").exists())


if __name__ == "__main__":
    unittest.main()
