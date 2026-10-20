#!/usr/bin/env python3
"""Isolated, account-free Linux QA. Requires three explicitly signed QA bundles.

A/B are real Bot builds. BAD is a synthetic executable that exits before Ready.
Never point this at an existing installation/profile or an experience port.
"""
import argparse
import concurrent.futures
import http.cookiejar
import http.server
import json
import os
import pathlib
import signal
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser()
    for name in ("a", "b", "bad", "public-key", "root"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--port", type=int, default=29417)
    parser.add_argument("--feed-port", type=int, default=29418)
    parser.add_argument("--pending-operation", action="store_true")
    parser.add_argument("--arch", choices=["amd64","arm64"], required=True)
    args = parser.parse_args()
    root = pathlib.Path(args.root)
    if sys.platform != "linux" or root.parent != pathlib.Path("/tmp") or not root.name.startswith("caelisbot-managed-update-qa-"):
        raise ValueError("Use a fresh /tmp/caelisbot-managed-update-qa-* directory on Linux")
    if args.port == 19317 or args.feed_port == 19317 or args.port == args.feed_port:
        raise ValueError("Reserved experience or conflicting ports")
    root.mkdir(mode=0o700)  # Existing roots are never reused or removed.
    packages = {}
    for name in ("a", "b", "bad"):
        directory = pathlib.Path(getattr(args, name)).resolve()
        manifest_file = directory / f"linux-{args.arch}-update.json"
        manifest = json.loads(manifest_file.read_text())
        assert manifest["buildKind"] == "qa" and "-qa." in manifest["version"]
        packages[name] = (directory, manifest)
    assert packages["a"][1]["runtime"] == packages["b"][1]["runtime"] == packages["bad"][1]["runtime"]
    assert packages["a"][1]["source"] == packages["b"][1]["source"] == packages["bad"][1]["source"]
    feed_origin = f"http://127.0.0.1:{args.feed_port}"
    origin = f"http://127.0.0.1:{args.port}"
    offered = []
    observations = []
    children = set()
    processes = []
    env = dict(os.environ)
    for name in ("HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"):
        directory = root / name.lower()
        directory.mkdir(mode=0o700)
        env[name] = str(directory)
    for name in list(env):
        if name.startswith("CAELIS_BOT_MANAGED_"):
            del env[name]
    trust = root / "qa-trust.json"
    trust.write_text(json.dumps({"publicKey": pathlib.Path(args.public_key).read_text().strip(), "origin": feed_origin}))
    trust.chmod(0o600)
    install, profile = root / "install", root / "profile"
    launcher = packages["a"][0] / f'caelisbot-launcher-{packages["a"][1]["version"]}-linux-{args.arch}'

    class Feed(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_GET(self):
            if self.path == "/releases":
                releases = []
                for name in offered[:]:
                    _, manifest = packages[name]
                    tag = "v" + manifest["version"]
                    assets = [f"linux-{args.arch}-update.json", f"linux-{args.arch}-update.json.sig", manifest["file"]]
                    releases.append({"tag_name": tag, "prerelease": True, "assets": [
                        {"name": asset, "browser_download_url": feed_origin + "/releases/download/" + tag + "/" + asset}
                        for asset in assets]})
                body = json.dumps(releases).encode()
                self.send_response(200)
                self.end_headers()
                self.wfile.write(body)
                return
            for directory, manifest in packages.values():
                prefix = "/releases/download/v" + manifest["version"] + "/"
                if not self.path.startswith(prefix):
                    continue
                name = self.path[len(prefix):]
                if name not in (f"linux-{args.arch}-update.json", f"linux-{args.arch}-update.json.sig", manifest["file"]):
                    break
                path = directory / name
                self.send_response(200)
                self.send_header("Content-Length", str(path.stat().st_size))
                self.end_headers()
                try:
                    with path.open("rb") as stream:
                        while chunk := stream.read(128 * 1024):
                            self.wfile.write(chunk)
                            self.wfile.flush()
                            if name.endswith(".tar.gz"):
                                time.sleep(0.012)
                except (BrokenPipeError, ConnectionResetError):
                    pass
                return
            self.send_error(404)

    feed = http.server.ThreadingHTTPServer(("127.0.0.1", args.feed_port), Feed)
    feed.daemon_threads = True
    threading.Thread(target=feed.serve_forever, daemon=True).start()
    jar = http.cookiejar.CookieJar()
    browser = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    csrf = ""

    def request(path, body=None, headers=None, authenticated=True):
        h = {"Origin": origin, "X-Caelis-Request": "1", "X-CSRF-Token": csrf}
        h.update(headers or {})
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            h["Content-Type"] = "application/json"
        opener = browser if authenticated else urllib.request.build_opener()
        req = urllib.request.Request(origin + path, data=data, headers=h)
        try:
            with opener.open(req, timeout=10) as res:
                return res.status, json.load(res)
        except urllib.error.HTTPError as err:
            return err.code, json.loads(err.read())

    def status():
        code, value = request("/api/v1/updates/status")
        assert code == 200, (code, value)
        observations.append({k: value.get(k) for k in ("state", "current", "bootId", "transactionId", "receivedBytes", "totalBytes", "reason")})
        return value

    def until(predicate, timeout=90):
        deadline = time.monotonic() + timeout
        last = None
        while time.monotonic() < deadline:
            try:
                last = status()
                if predicate(last):
                    return last
            except (OSError, ValueError, AssertionError):
                pass
            time.sleep(0.15)
        raise AssertionError(("status timeout", last))

    def command(action, **fields):
        body = {"id": uuid.uuid4().hex, "action": action, **fields}
        code, value = request("/api/v1/updates/command", body)
        assert code == 200, (code, value)
        return body, value

    def start():
        log = (root / f"launcher-{len(processes)}.log").open("wb")
        process = subprocess.Popen([str(launcher), "serve", "--root", str(install), "--qa-trust", str(trust)],
                                   env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        processes.append(process)
        return process

    def child_pid(process):
        # Go may fork from any OS thread, not necessarily the leader thread.
        pids = set()
        for task in pathlib.Path(f"/proc/{process.pid}/task").iterdir():
            try:
                pids.update(int(pid) for pid in (task / "children").read_text().split())
            except FileNotFoundError:
                pass
        assert len(pids) == 1, pids
        pid = pids.pop()
        children.add(pid)
        return pid

    def stop_child(pid):
        executable = pathlib.Path(f"/proc/{pid}/exe").resolve(strict=True)
        assert executable.is_relative_to(install)
        os.kill(pid, signal.SIGTERM)
        for _ in range(200):
            try:
                state = pathlib.Path(f"/proc/{pid}/stat").read_text().split(") ")[1].split()[0]
                if state == "Z":
                    return
            except FileNotFoundError:
                return
            time.sleep(0.1)
        raise AssertionError("QA child shutdown not confirmed")

    def record(case, **data):
        result = {"case": case, "passed": True, **data}
        evidence["cases"].append(result)
        print(json.dumps(result), flush=True)

    evidence = {"kind": "synthetic-qa-not-published", "source": packages["a"][1]["source"],
                "platform": "linux-"+args.arch, "versions": {k: v[1]["version"] for k, v in packages.items()}, "cases": []}
    try:
        directory, a = packages["a"]
        subprocess.run([str(launcher), "init", "--root", str(install), "--data-dir", str(profile),
                        "--listen", f"127.0.0.1:{args.port}", "--archive", str(directory / a["file"]),
                        "--manifest", str(directory / f"linux-{args.arch}-update.json"),
                        "--signature", str(directory / f"linux-{args.arch}-update.json.sig"), "--qa-trust", str(trust)],
                       env=env, check=True, timeout=60)
        if args.pending_operation:
            (profile / "Web").mkdir(mode=0o700, exist_ok=True)
            unknown_bytes = json.dumps({"qa-original-unknown-operation": {"digest": "qa-original-digest", "owner": "qa-original-session", "state": "pending"}}).encode()
            (profile / "Web" / "operations.json").write_bytes(unknown_bytes)
            (profile / "Web" / "operations.json").chmod(0o600)
        # Original business bytes must never be restored from a code backup.
        notebook = profile / "Notebook" / "qa-identity.txt"
        notebook.parent.mkdir(mode=0o700, exist_ok=True)
        notebook.write_text("original-profile-identity\n")
        process = start()
        deadline = time.monotonic() + 40
        while True:
            try:
                code, boot = request("/api/v1/bootstrap", {})
                if code == 200:
                    csrf = boot["csrf"]
                    break
            except (OSError, ValueError):
                pass
            assert process.poll() is None, "initial launcher stopped"
            assert time.monotonic() < deadline, "bootstrap timeout"
            time.sleep(0.2)
        first = until(lambda s: s["current"]["version"] == a["version"])
        selected = json.loads((install / "current.json").read_text())["release"]
        direct = subprocess.run([str(install / "versions" / selected / "caelisbot"), "serve", "--data-dir", str(root / "forbidden-second-profile"), "--web=false"],
                                env=env, capture_output=True, timeout=20)
        assert direct.returncode != 0 and b"requires its launcher" in direct.stderr, direct.stderr
        record("direct-second-profile-refused")
        assert first["testArtifact"]
        command("automatic", automatic=False)
        command("check")
        until(lambda s: s["state"] == "no_compatible_update")
        record("no-linux-offer")
        assert request("/api/v1/updates/status", authenticated=False)[0] == 401
        assert request("/api/v1/updates/command", {"id": uuid.uuid4().hex, "action": "check"}, {"X-CSRF-Token": "wrong"})[0] == 403
        assert request("/api/v1/updates/command", {"id": uuid.uuid4().hex, "action": "check", "url": "http://evil.invalid"})[0] == 400
        assert request("/api/v1/updates/command", {"id": uuid.uuid4().hex, "action": "check"}, {"Origin": "https://evil.invalid"})[0] == 403
        record("auth-csrf-origin-no-arbitrary-url")
        # Automatic check/download, with actual byte samples from a throttled feed.
        offered[:] = ["b"]
        command("automatic", automatic=True)
        check_body, check_receipt = command("check")
        if not args.pending_operation:
            partial = until(lambda s: s["state"] == "downloading" and 0 < s["receivedBytes"] < s["totalBytes"])
            orphan = child_pid(process)
            process.kill()
            process.wait(timeout=5)
            stop_child(orphan)
            process = start()
            interrupted = until(lambda s: s["state"] == "available" and s.get("reason") == "download_interrupted")
            assert interrupted["current"] == first["current"] and interrupted["offerId"] == partial["offerId"]
            assert request("/api/v1/updates/receipts/" + check_body["id"])[1] == check_receipt
            command("download", offerId=interrupted["offerId"])
            record("interrupted-download-original-offer", receivedBeforeCrash=partial["receivedBytes"])
        ready = until(lambda s: s["state"] == "ready", timeout=120)
        counts = sorted({s["receivedBytes"] for s in observations if s["state"] == "downloading"})
        assert len(counts) >= 2 and any(0 < n < ready["totalBytes"] for n in counts)
        record("actual-download-progress", samples=len(counts), total=ready["totalBytes"])
        if args.pending_operation:
            pending_body, pending_receipt = command("install", offerId=ready["offerId"], when="idle")
            waiting = until(lambda s: s["state"] == "waiting_idle" and s.get("reason") == "operation_unconfirmed")
            time.sleep(1.2)
            waiting = status()
            assert waiting["bootId"] == first["bootId"] and waiting["current"] == first["current"]
            command("cancel", transactionId=pending_receipt["transactionId"], revision=waiting["revision"])
            assert status()["state"] == "ready"
            assert request("/api/v1/updates/receipts/" + pending_body["id"])[1] == pending_receipt
            assert (profile / "Web" / "operations.json").read_bytes() == unknown_bytes
            record("original-unknown-operation-blocks-install-and-survives-cancel")
            evidence["success"] = True
            return
        # Two tabs can send without acquiring the composer; all join one install.
        body = {"id": uuid.uuid4().hex, "action": "install", "offerId": ready["offerId"], "when": "idle"}
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            responses = list(pool.map(lambda _: request("/api/v1/updates/command", body), range(8)))
        assert all(code == 200 for code, _ in responses), responses
        receipts = [v for _, v in responses]
        tx = receipts[0]["transactionId"]
        assert tx and all(r == receipts[0] for r in receipts)
        installed = until(lambda s: s["state"] == "installed")
        b = packages["b"][1]
        assert installed["current"] == {"version": b["version"], "source": b["source"], "kind": "qa"}
        assert installed["bootId"] != first["bootId"] and installed["transactionId"] == tx
        assert request("/api/v1/updates/receipts/" + body["id"])[1] == receipts[0]
        assert request("/api/v1/updates/command", body)[1] == receipts[0]
        assert notebook.read_text() == "original-profile-identity\n"
        record("a-to-b-original-receipt-multitab-health", transaction=tx, current=installed["current"])
        # A deliberately unhealthy executable is signed only by this QA key.
        offered[:] = ["bad"]
        command("check")
        ready = until(lambda s: s["state"] == "ready", timeout=120)
        bad_body, bad_receipt = command("install", offerId=ready["offerId"], when="now")
        rolled = until(lambda s: s["state"] == "rolled_back")
        assert rolled["current"] == installed["current"] and rolled["transactionId"] == bad_receipt["transactionId"]
        assert request("/api/v1/updates/receipts/" + bad_body["id"])[1] == bad_receipt
        assert notebook.read_text() == "original-profile-identity\n"
        record("unhealthy-candidate-rolls-back-once", current=rolled["current"])
        # Supervisor loss must not replace the still-live old Bot.
        orphan = child_pid(process)
        process.kill()
        process.wait(timeout=5)
        denied = subprocess.run([str(launcher), "serve", "--root", str(install), "--qa-trust", str(trust)],
                                env=env, capture_output=True, timeout=5)
        assert denied.returncode != 0 and b"installation_owned" in denied.stderr, denied.stderr
        assert request("/api/v1/updates/status")[1]["state"] == "unknown"
        stop_child(orphan)
        process = start()
        recovered = until(lambda s: s["current"] == installed["current"] and s["state"] == "rolled_back")
        assert request("/api/v1/updates/receipts/" + body["id"])[1] == receipts[0]
        assert request("/api/v1/updates/receipts/" + bad_body["id"])[1] == bad_receipt
        record("supervisor-crash-orphan-lock-original-recovery", current=recovered["current"])
        evidence["success"] = True
    finally:
        for process in processes:
            if process.poll() is None:
                process.send_signal(signal.SIGTERM)
                try:
                    process.wait(timeout=35)
                except subprocess.TimeoutExpired:
                    # Only the session we created; no user service or shared Runtime.
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=5)
        for pid in children:
            try:
                executable = pathlib.Path(f"/proc/{pid}/exe").resolve(strict=True)
                if executable.is_relative_to(install):
                    os.kill(pid, signal.SIGTERM)
            except (FileNotFoundError, ProcessLookupError):
                pass
        feed.shutdown()
        evidence["observations"] = observations
        (root / "evidence.json").write_text(json.dumps(evidence, indent=2) + "\n")
        print("QA evidence:", root / "evidence.json", flush=True)


if __name__ == "__main__":
    main()
