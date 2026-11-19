#!/usr/bin/env python3
"""Edit native MCP declarations and submit reviewed plugin requests in Notebook."""

from contextlib import contextmanager
import argparse
import json
import os
from pathlib import Path
import re
import stat
import sys
import tempfile
import time
from urllib.parse import urlsplit
from uuid import uuid4

if sys.version_info < (3, 11):
    sys.exit("The extension script requires Python 3.11 or newer.")
import tomllib


LIMIT = 1 << 20
MCP_ID = re.compile(r"[A-Za-z0-9_-]{1,64}")
PLUGIN_TOKEN = re.compile(r"[A-Za-z0-9][A-Za-z0-9._+-]{0,127}")
REQUEST_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9_-]{0,127}")
CODEX_START = "# BEGIN Caelis Bot workspace MCP"
CODEX_END = "# END Caelis Bot workspace MCP"
CODEX_TABLE = re.compile(r"\[mcp_servers\.([A-Za-z0-9_-]+)\][ \t]*")
TOML_SPECIAL = re.compile(r"[\"'#]")
PLUGIN_REQUEST = "bot-plugin-request.json"


def read_file(path):
    try:
        info = path.lstat()
    except FileNotFoundError:
        return None
    if not stat.S_ISREG(info.st_mode) or info.st_size > LIMIT:
        raise ValueError("Expected a regular workspace file within the size limit.")
    with path.open("rb") as source:
        body = source.read(LIMIT + 1)
    if len(body) > LIMIT:
        raise ValueError("Workspace file exceeds the size limit.")
    return body


def workspace_directory(workspace, name):
    path = workspace / name
    path.mkdir(mode=0o700, exist_ok=True)
    if not path.is_dir() or path.resolve() != path:
        raise ValueError("Expected a real workspace configuration directory.")
    return path


@contextmanager
def config_lock(workspace):
    # Both native projection and script edits lock byte zero on Windows,
    # or take flock on Unix. Close releases the lock even after process death.
    path = workspace / ".bot-extension.lock"
    if path.is_symlink():
        raise ValueError("Invalid configuration lock.")
    with path.open("a+b") as lock:
        deadline = time.monotonic() + 2
        while True:
            try:
                if os.name == "nt":
                    import msvcrt
                    lock.seek(0)
                    msvcrt.locking(lock.fileno(), msvcrt.LK_NBLCK, 1)
                else:
                    import fcntl
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except (BlockingIOError, PermissionError):
                if time.monotonic() >= deadline:
                    raise ValueError("The workspace configuration is busy.")
                time.sleep(0.025)
        yield


def publish(path, body, original=None, *, exclusive=False):
    if len(body) > LIMIT:
        raise ValueError("Workspace file exceeds the size limit.")
    out = tempfile.NamedTemporaryFile(dir=path.parent, prefix=".bot-write-", delete=False)
    temporary = Path(out.name)
    try:
        with out:
            out.write(body)
            out.flush()
            os.fsync(out.fileno())
        if exclusive:
            os.link(temporary, path)
        else:
            if read_file(path) != original:
                raise ValueError("Workspace file changed while preparing the write.")
            os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def as_object(value):
    if not isinstance(value, dict):
        raise ValueError("Expected a configuration object.")
    return value


def candidate_for(workspace, name):
    source = Path(name)
    if source.is_absolute() or ".." in source.parts or not (workspace / source).resolve().is_relative_to(workspace):
        raise ValueError("Provide a Notebook-relative JSON file, such as notes/server.json.")
    body = read_file(workspace / source)
    if body is None:
        raise ValueError("The MCP candidate file is missing.")
    candidate = as_object(json.loads(body))
    if candidate.keys() - {"command", "args", "cwd", "url", "type"}:
        raise ValueError("The MCP candidate contains unsupported fields.")
    command, cwd, url, transport = (
        candidate.get(field, "") for field in ("command", "cwd", "url", "type")
    )
    args = candidate.get("args", [])
    if any(not isinstance(value, str) or any(char in value for char in "\0\r\n")
           for value in (command, cwd, url, transport)):
        raise ValueError("Expected text values for MCP configuration fields.")
    if not isinstance(args, list) or any(not isinstance(arg, str) or "\0" in arg for arg in args):
        raise ValueError("Expected a list of text MCP arguments.")
    if command and command.strip() == command and not url and transport in ("", "stdio"):
        result = {"command": command}
        if args:
            result["args"] = args
        if cwd:
            directory = (workspace / cwd).resolve()
            if not directory.is_relative_to(workspace) or not directory.is_dir():
                raise ValueError("The MCP working directory must exist inside Notebook.")
            result["cwd"] = cwd
        return result
    if url and not (command or cwd or args) and transport in ("", "http", "streamable-http", "streamable_http"):
        try:
            endpoint = urlsplit(url)
            _ = endpoint.port  # urllib validates port syntax and range on access.
            valid = endpoint.scheme in ("http", "https") and endpoint.hostname and endpoint.username is None
        except ValueError:
            valid = False
        if valid and not endpoint.fragment and not any(char.isspace() for char in url):
            return {"url": url}
    raise ValueError("Specify one stdio command or HTTP endpoint.")


def update_caelis(workspace, server_id, candidate):
    path = workspace / ".mcp.json"
    original = read_file(path)
    document = as_object(json.loads(original)) if original is not None else {}
    servers = as_object(document.setdefault("mcpServers", {}))
    if candidate is None:
        servers.pop(server_id, None)
    else:
        if "cwd" in candidate:
            candidate["workDir"] = candidate.pop("cwd")
        if "url" in candidate:
            candidate["type"] = "streamable-http"
        servers[server_id] = candidate
    publish(path, (json.dumps(document, indent=2, ensure_ascii=False, allow_nan=False) + "\n").encode(), original)


def codex_servers(document):
    return as_object(document.get("mcp_servers", {}))


def toml_lines(text):
    """Yield line spans outside strings in an already parsed TOML document."""
    quote = ""
    start = 0
    for raw in text.split("\n"):
        end = min(start + len(raw) + 1, len(text))
        line = raw.removesuffix("\r")
        if not quote:
            yield start, end, line
        index = 0
        while index < len(line):
            if not quote:
                token = TOML_SPECIAL.search(line, index)
                if token is None or token[0] == "#":
                    break
                index = token.start()
                quote = token[0] * (3 if line.startswith(token[0] * 3, index) else 1)
                index += len(quote)
            elif quote[0] == '"' and line[index] == "\\":
                index += 2
            elif line.startswith(quote, index):
                index += len(quote)
                if len(quote) == 3:
                    while index < len(line) and line[index] == quote[0]:
                        index += 1
                quote = ""
            else:
                index += 1
        start = end


def codex_block(text):
    start = content = end = None
    for offset, next_offset, line in toml_lines(text):
        if line == CODEX_START:
            if start is not None:
                raise ValueError("Invalid workspace MCP section markers.")
            start, content = offset, next_offset
        elif line == CODEX_END:
            if start is None or end is not None:
                raise ValueError("Invalid workspace MCP section markers.")
            stop, end = offset, next_offset
    if start is None:
        return text, "", ""
    if end is None:
        raise ValueError("Invalid workspace MCP section markers.")
    return text[:start], text[content:stop], text[end:]


def same_toml(left, right):
    if type(left) is not type(right):
        return False
    if isinstance(left, dict):
        return left.keys() == right.keys() and all(same_toml(value, right[key]) for key, value in left.items())
    if isinstance(left, list):
        return len(left) == len(right) and all(same_toml(x, y) for x, y in zip(left, right))
    if isinstance(left, float):
        return left.hex() == right.hex()
    return left == right


def update_codex(workspace, server_id, candidate):
    path = workspace_directory(workspace, ".codex") / "config.toml"
    original = read_file(path)
    text = original.decode() if original is not None else ""
    expected = tomllib.loads(text)
    before, owned, after = codex_block(text)
    if server_id in codex_servers(tomllib.loads(before + after)):
        raise ValueError("The server is already configured outside the managed MCP section.")
    sections = [(offset, match[1]) for offset, _, line in toml_lines(owned)
                if (match := CODEX_TABLE.fullmatch(line))]
    for index, (offset, name) in enumerate(sections):
        if name == server_id:
            end = sections[index + 1][0] if index + 1 < len(sections) else len(owned)
            owned = owned[:offset] + owned[end:]
            break
    servers = expected.setdefault("mcp_servers", {})
    if candidate is None:
        servers.pop(server_id, None)
    else:
        servers[server_id] = candidate
        if owned and not owned.endswith("\n"):
            owned += "\n"
        owned += f"[mcp_servers.{server_id}]\n"
        for name, value in candidate.items():
            literal = json.dumps(value, ensure_ascii=False).replace("\x7f", "\\u007f")
            owned += f"{name} = {literal}\n"
    if owned.strip():
        if not owned.endswith("\n"):
            owned += "\n"
        if before and not before.endswith("\n"):
            before += "\n"
        updated = before + CODEX_START + "\n" + owned + CODEX_END + "\n" + after
    else:
        updated = before + after
    actual = tomllib.loads(updated)
    for document in (expected, actual):
        if document.get("mcp_servers") == {}:
            document.pop("mcp_servers")
    if not same_toml(actual, expected):
        raise ValueError("The edit would change unrelated Codex configuration.")
    if updated != text:
        body = updated.encode()
        if updated.startswith("# Managed by Caelis Bot. Bot workspace only.\n"):
            # The old Bot rewrites this project at startup. Preserve script
            # edits even if it starts before the next native projection.
            backup = path.with_name(".caelis-bot-upgrade.toml")
            publish(backup, body, read_file(backup))
        publish(path, body, original)


def mcp(workspace, args):
    if (not MCP_ID.fullmatch(args.id) or re.match(r"p_[a-f0-9]{8}_", args.id)
            or args.id.lower().startswith("caelis_")):
        raise ValueError("Invalid or reserved MCP server name.")
    if args.action == "remove":
        if args.file is not None:
            raise ValueError("Remove takes only a server ID.")
        candidate = None
    else:
        if args.file is None:
            raise ValueError("Provide a Notebook-relative JSON file, such as notes/server.json.")
        candidate = candidate_for(workspace, args.file)
        if args.action == "check":
            return {"state": "validated"}
    updater = update_codex if args.runtime == "codex" else update_caelis
    with config_lock(workspace):
        updater(workspace, args.id, candidate)
    return {"state": "saved"}


def plugin_status(directory, request_id):
    def read_record(name):
        body = read_file(directory / name)
        return as_object(json.loads(body)) if body is not None else {}

    request = read_record(PLUGIN_REQUEST)
    status = read_record(f"bot-plugin-status-{request_id}.json")
    state = status.get("state")
    if status.get("id") == request_id and state in ("saved", "rejected", "unknown"):
        result = {"id": request_id, "state": state}
        for field in ("action", "pluginId", "version"):
            value = status.get(field)
            if isinstance(value, str) and PLUGIN_TOKEN.fullmatch(value):
                result[field] = value
        revision = status.get("revision")
        if type(revision) is int and revision > 0:
            result["revision"] = revision
        return result
    return {"id": request_id, "state": "pending" if request.get("id") == request_id else "unavailable"}


def plugin(workspace, skill, args):
    if args.action == "catalog":
        if args.id is not None or args.version is not None:
            raise ValueError("Catalog takes no package ID or version.")
        return json.loads((skill / "assets" / "plugin-catalog.json").read_bytes())
    if args.action == "status":
        if not args.id or not REQUEST_ID.fullmatch(args.id) or args.version is not None:
            raise ValueError("Provide the original plugin request ID.")
        directory = workspace / ".agents"
        if directory.resolve() != directory:
            raise ValueError("Expected a real workspace configuration directory.")
        return plugin_status(directory, args.id)
    if not args.id or not PLUGIN_TOKEN.fullmatch(args.id):
        raise ValueError("Provide a valid reviewed package ID.")
    if args.action == "update" and args.version is None:
        raise ValueError("Provide the exact package version to update.")
    request = {"id": uuid4().hex, "action": args.action, "pluginId": args.id}
    if args.version is not None:
        if args.action not in ("install", "update") or not PLUGIN_TOKEN.fullmatch(args.version):
            raise ValueError("Invalid version for this plugin action.")
        request["version"] = args.version
    directory = workspace_directory(workspace, ".agents")
    try:
        publish(directory / PLUGIN_REQUEST, (json.dumps(request) + "\n").encode(), exclusive=True)
    except FileExistsError:
        raise ValueError("A plugin request is still pending. Check its original request ID.") from None
    return {"id": request["id"], "state": "pending"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    mcp_parser = commands.add_parser("mcp")
    mcp_parser.add_argument("runtime", choices=("codex", "caelis"))
    mcp_parser.add_argument("action", choices=("check", "set", "remove"))
    mcp_parser.add_argument("id")
    mcp_parser.add_argument("file", nargs="?")
    plugin_parser = commands.add_parser("plugin")
    plugin_parser.add_argument("action", choices=("catalog", "status", "install", "update", "enable", "disable", "uninstall"))
    plugin_parser.add_argument("id", nargs="?")
    plugin_parser.add_argument("version", nargs="?")
    args = parser.parse_args()
    skill = Path(__file__).resolve().parent.parent
    notebook = skill.parent.parent / "Notebook"
    workspace = Path.cwd().resolve()
    if workspace != notebook:
        parser.exit(1, "Run the installed extension script from the Bot Notebook.\n")
    try:
        result = mcp(workspace, args) if args.command == "mcp" else plugin(workspace, skill, args)
    except (json.JSONDecodeError, tomllib.TOMLDecodeError, UnicodeError):
        parser.exit(1, "Invalid configuration format. No change was saved.\n")
    except PermissionError:
        parser.exit(1, "Workspace access was denied. Check file permissions or use the Runtime's native approval flow.\n")
    except OSError:
        parser.exit(1, "The workspace file could not be accessed. Check the Notebook path and file access.\n")
    except ValueError as error:
        parser.exit(1, f"{error}\n")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
