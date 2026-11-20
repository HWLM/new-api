#!/usr/bin/env python3
"""蒸馏检测 Redis 模式验收：清缓存走 Redis（§20）+ 跨实例共享降级状态（上生产前的关键证据）

前置：
  实例 A、B 都配了 REDIS_CONN_STRING 指向同一个 Redis、同一个 db；
  两个实例各自有可用的检测渠道和测试模型（脚本自己建，跑完删）。

环境变量：
  DISTILL_BASE_URL_A / DISTILL_ADMIN_TOKEN_A / DISTILL_RELAY_KEY_A
  DISTILL_BASE_URL_B / DISTILL_ADMIN_TOKEN_B / DISTILL_RELAY_KEY_B
  DISTILL_USER_ID          两边同一个用户 id（默认 1）
  DISTILL_REDIS_ADDR       127.0.0.1:6380
  DISTILL_REDIS_PASSWORD   可空
  DISTILL_REDIS_DB         0
  DISTILL_JEV_URL          假 Jev（两个实例共用同一个，便于数调用）
  DISTILL_UPSTREAM_PORT    假上游端口（脚本自己起在 127.0.0.1）

用例：
  R1 Redis 模式清缓存：backend=redis、SCAN MATCH distill:* 真删干净、hits 不删、降级解除
  R2 跨实例共享：A 上降级后 B 立刻按降级处理（不调 Jev、直接 429），并反向再验一次
"""
import argparse
import json
import os
import socket
import sys
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

REDIS_ADDR = os.environ.get("DISTILL_REDIS_ADDR", "127.0.0.1:6380")
REDIS_PASSWORD = os.environ.get("DISTILL_REDIS_PASSWORD", "")
REDIS_DB = int(os.environ.get("DISTILL_REDIS_DB", "0"))
USER_ID = int(os.environ.get("DISTILL_USER_ID", "1") or 1)
JEV_URL = os.environ.get("DISTILL_JEV_URL", "http://127.0.0.1:18098/v1/systemone")
UPSTREAM_PORT = int(os.environ.get("DISTILL_UPSTREAM_PORT", "18101"))
MODEL = os.environ.get("DISTILL_MODEL", "distill-a")

results = []


def report(case, name, status, detail=""):
    results.append((case, name, status, detail))
    line = "[%s] %s %s" % (status, case, name)
    if detail:
        line += " — " + detail
    print(line, flush=True)


class Redis:
    """够用的 RESP 客户端：只做 AUTH/SELECT/SCAN/EXISTS/TTL/KEYS。"""

    def __init__(self, addr, password="", db=0):
        host, _, port = addr.partition(":")
        self.sock = socket.create_connection((host or "127.0.0.1", int(port or 6379)), timeout=10)
        self.f = self.sock.makefile("rb")
        if password:
            self.cmd("AUTH", password)
        if db:
            self.cmd("SELECT", str(db))

    def cmd(self, *args):
        buf = [b"*%d\r\n" % len(args)]
        for a in args:
            b = a if isinstance(a, bytes) else str(a).encode()
            buf.append(b"$%d\r\n%s\r\n" % (len(b), b))
        self.sock.sendall(b"".join(buf))
        return self._read()

    def _read(self):
        line = self.f.readline()
        if not line:
            raise RuntimeError("redis 连接关闭")
        t, body = line[:1], line[1:-2]
        if t == b"+":
            return body.decode()
        if t == b"-":
            raise RuntimeError("redis error: " + body.decode())
        if t == b":":
            return int(body)
        if t == b"$":
            n = int(body)
            if n < 0:
                return None
            data = self.f.read(n + 2)
            return data[:-2].decode("utf-8", "replace")
        if t == b"*":
            n = int(body)
            return [self._read() for _ in range(max(n, 0))]
        raise RuntimeError("unexpected RESP type %r" % t)

    def scan_all(self, match="distill:*"):
        out, cur = [], "0"
        while True:
            cur, keys = self.cmd("SCAN", cur, "MATCH", match, "COUNT", "200")
            out.extend(keys)
            if cur == "0":
                return out

    def ping(self):
        return self.cmd("PING")


class Client:
    def __init__(self, base, token, relay_key, tag):
        self.base = base.rstrip("/")
        self.token = token
        self.relay_key = relay_key
        self.tag = tag

    def api(self, method, path, body=None, timeout=60):
        data = None if body is None else json.dumps(body).encode()
        r = urllib.request.Request(self.base + path, data=data, method=method)
        r.add_header("content-type", "application/json")
        r.add_header("authorization", "Bearer " + self.token)
        try:
            with urllib.request.urlopen(r, timeout=timeout) as resp:
                return resp.status, resp.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as e:
            return e.code, e.read().decode("utf-8", "replace")
        except Exception as e:
            return 0, "transport error: %s" % e

    def ok(self, method, path, body=None):
        code, txt = self.api(method, path, body)
        if code != 200:
            raise RuntimeError("%s %s %s -> %s %s" % (self.tag, method, path, code, txt[:200]))
        j = json.loads(txt)
        if j.get("success") is False:
            raise RuntimeError("%s %s %s 失败: %s" % (self.tag, method, path, txt[:200]))
        return j

    def relay(self, text, model=None, timeout=120):
        body = {"model": model or MODEL, "stream": False, "max_tokens": 8,
                "messages": [{"role": "user", "content": text}]}
        data = json.dumps(body).encode()
        r = urllib.request.Request(self.base + "/v1/chat/completions", data=data, method="POST")
        r.add_header("content-type", "application/json")
        r.add_header("authorization", "Bearer " + self.relay_key)
        try:
            with urllib.request.urlopen(r, timeout=timeout) as resp:
                return resp.status, resp.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as e:
            return e.code, e.read().decode("utf-8", "replace")
        except Exception as e:
            return 0, "transport error: %s" % e

    def setting(self, **over):
        cfg = {
            "enabled": True, "activate_rpm": 1, "judge_interval_seconds": 1,
            "confirm_hits": 2, "confirm_window_seconds": 300, "threshold": 0.60,
            "demote_seconds": 300, "observe_only": False, "jev_base_url": JEV_URL,
            "jev_api_key": "test-jev-key", "jev_timeout_ms": 5000,
            "alert_telegram": False, "alert_wecom": False, "dedup_seconds": 0,
            "fallback_channel_id": 0,
        }
        # 白名单只指向本实例自己建的检测渠道，否则检测不启用
        det = getattr(self, "detect_id", None)
        cfg["detect_channel_ids"] = [det] if det else []
        cfg.update(over)
        self.ok("PUT", "/api/distill_detect/setting", cfg)

    def hits(self):
        j = self.ok("GET", "/api/distill_detect/hits?p=1&page_size=200")
        d = j.get("data") or {}
        return d.get("items") or d.get("records") or []

    def clear(self, scope="all"):
        return self.api("POST", "/api/distill_detect/clear_cache", {"scope": scope})

    def channels(self):
        j = self.ok("GET", "/api/channel/?p=1&page_size=200")
        d = j.get("data") or {}
        if isinstance(d, list):
            return d
        return d.get("items") or []

    def ensure_channel(self, name, upstream_base):
        for ch in self.channels():
            if ch.get("name") == name:
                return int(ch["id"])
        self.ok("POST", "/api/channel/", {"mode": "single", "channel": {
            "name": name, "type": 1, "key": "sk-stub-hermes", "base_url": upstream_base,
            "models": MODEL, "group": "default", "status": 1, "priority": 100}})
        # 开了 Redis 的实例：新渠道要等 abilities 缓存重建，否则 distributor 报「无可用渠道」
        self.api("POST", "/api/channel/fix", None)
        time.sleep(0.6)
        for ch in self.channels():
            if ch.get("name") == name:
                return int(ch["id"])
        raise RuntimeError("%s 建渠道失败" % self.tag)

    def drop_channel(self, name):
        for ch in self.channels():
            if ch.get("name") == name:
                self.api("DELETE", "/api/channel/%d" % int(ch["id"]))


class Jev:
    def __init__(self, port):
        self.calls = []
        outer = self

        class H(BaseHTTPRequestHandler):
            def do_POST(self):
                n = int(self.headers.get("content-length") or 0)
                body = json.loads(self.rfile.read(n) or b"{}")
                qs = list((body.get("questions") or {}).keys()) or ["train.corpus_farm"]
                outer.calls.append({"t": time.time(), "chars": len(body.get("state") or "")})
                payload = json.dumps({"answers": {q: {"noul": outer.score} for q in qs},
                                      "model": "fake-jev"}).encode()
                self.send_response(200)
                self.send_header("content-type", "application/json")
                self.send_header("content-length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, *a):
                pass

        self.score = 0.10
        self.server = ThreadingHTTPServer(("127.0.0.1", port), H)

    def start(self):
        import threading
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def stop(self):
        self.server.shutdown()

    def count(self):
        return len(self.calls)

    def reset(self, score):
        self.calls.clear()
        self.score = score


class Upstream:
    def __init__(self, port):
        class H(BaseHTTPRequestHandler):
            def do_POST(self):
                n = int(self.headers.get("content-length") or 0)
                body = json.loads(self.rfile.read(n) or b"{}")
                payload = json.dumps({
                    "id": "chatcmpl-stub", "object": "chat.completion", "created": int(time.time()),
                    "model": body.get("model") or MODEL,
                    "choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"},
                                 "finish_reason": "stop"}],
                    "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}}).encode()
                self.send_response(200)
                self.send_header("content-type", "application/json")
                self.send_header("content-length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, *a):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", port), H)

    def start(self):
        import threading
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def stop(self):
        self.server.shutdown()


def demote_key():
    return "distill:demote:{user:%d}" % USER_ID


def case_R1(a, r, jev):
    """清缓存走 Redis：backend=redis、SCAN 真删干净、hits 不删、降级解除"""
    case = "R1"
    a.setting(confirm_hits=2)
    code, txt = a.clear("all")
    before_keys = r.scan_all()

    jev.reset(0.90)
    a.relay("R1 redis 模式语料 %f 批量生成训练语料" % time.time())
    time.sleep(1.2)
    a.relay("R1 redis 模式语料 %f 批次二" % time.time())
    time.sleep(1.2)
    keys_after_run = r.scan_all()
    demoted = r.cmd("EXISTS", demote_key())
    report(case, "运行态落在 Redis（键出现在 redis 里）", "PASS" if len(keys_after_run) > 0 else "FAIL",
           "distill:* 键 %d 个（清完 %d 个），demote 键存在=%s" % (len(keys_after_run), len(before_keys), demoted))
    report(case, "两次命中后确实置了降级键", "PASS" if demoted == 1 else "FAIL",
           "EXISTS %s = %s" % (demote_key(), demoted))

    hits_before = len(a.hits())   # 基线必须在两次命中之后取，否则变化量含自己刚写的那两行
    code, txt = a.clear("all")
    j = json.loads(txt) if code == 200 else {}
    data = j.get("data") or {}
    report(case, "backend 是 redis（不是内存兜底）", "PASS" if data.get("backend") == "redis" else "FAIL",
           "HTTP %s backend=%s cleared=%s upgraded=%s" %
           (code, data.get("backend"), data.get("cleared"), data.get("upgraded_users")))

    left = r.scan_all()
    report(case, "SCAN MATCH distill:* 清干净", "PASS" if len(left) == 0 else "FAIL",
           "剩余键 %d 个 %s" % (len(left), left[:5]))

    hits_after = len(a.hits())
    report(case, "hits 记录不删", "PASS" if hits_after == hits_before else "FAIL",
           "清前 %d 行，清后 %d 行" % (hits_before, hits_after))

    jev.reset(0.90)
    code, txt = a.relay("R1 清后新正文 %f 语料改写" % time.time())
    time.sleep(1.2)
    report(case, "清后降级解除（重新调 Jev）", "PASS" if jev.count() >= 1 else "FAIL",
           "Jev 调用 %d 次，HTTP %s" % (jev.count(), code))


def case_R2(a, b, r, jev):
    """跨实例共享降级：A 降级后 B 立刻不调 Jev、直接 429"""
    case = "R2"
    # confirm_hits=2：单次命中不降级，于是「B 有没有调 Jev」成为状态是否共享的判别点
    for c in (a, b):
        c.setting(confirm_hits=2, fallback_channel_id=0, observe_only=False, dedup_seconds=0)
    a.clear("all")
    time.sleep(0.5)

    # 控制组：B 正常工作时必须调 Jev，且单次命中不降级
    jev.reset(0.90)
    code_b, _ = b.relay("R2 控制组 %f 语料" % time.time())
    time.sleep(1.2)
    report(case, "控制组：B 正常调 Jev 且不降级", "PASS" if (jev.count() == 1 and code_b == 200) else "FAIL",
           "B HTTP %s，Jev 调用 %d 次" % (code_b, jev.count()))

    a.clear("all")
    time.sleep(0.5)

    # A 上凑够 2 次命中 -> 降级 -> 第二次请求因为没配兜底直接 429
    jev.reset(0.90)
    a.relay("R2 A 实例命中一 %f" % time.time())
    time.sleep(1.2)
    code_a2, _ = a.relay("R2 A 实例命中二 %f" % time.time())
    time.sleep(1.2)
    report(case, "A 第 2 次命中触发降级（429，无兜底）", "PASS" if code_a2 == 429 else "FAIL",
           "A 第二次 HTTP %s，Jev 调用 %d 次" % (code_a2, jev.count()))
    report(case, "A 降级键落在共享 Redis", "PASS" if r.cmd("EXISTS", demote_key()) == 1 else "FAIL",
           "EXISTS %s = %s" % (demote_key(), r.cmd("EXISTS", demote_key())))

    # B 换一段新正文：若状态共享，B 不会调 Jev，直接 429
    jev.reset(0.90)
    code_b2, txt_b2 = b.relay("R2 B 实例新正文 %f 语料改写" % time.time())
    time.sleep(1.2)
    report(case, "B 立刻看到 A 的降级（不调 Jev）", "PASS" if jev.count() == 0 else "FAIL",
           "B HTTP %s，期间 Jev 调用 %d 次（>0 说明各实例各存一份）" % (code_b2, jev.count()))
    report(case, "B 按降级处理返回 429", "PASS" if code_b2 == 429 else "FAIL",
           "B HTTP %s %s" % (code_b2, txt_b2[:120]))

    # 反向：B 触发降级，A 立刻看到
    a.clear("all")
    time.sleep(0.5)
    b.relay("R2 B 反向命中一 %f" % time.time())
    time.sleep(1.2)
    code_b3, _ = b.relay("R2 B 反向命中二 %f" % time.time())
    time.sleep(1.2)
    jev.reset(0.90)
    code_a3, txt_a3 = a.relay("R2 A 实例新正文反向 %f" % time.time())
    time.sleep(1.2)
    report(case, "反向：A 立刻看到 B 的降级", "PASS" if (code_b3 == 429 and jev.count() == 0 and code_a3 == 429) else "FAIL",
           "B 第二次 HTTP %s，A 之后 HTTP %s，Jev 调用 %d 次" % (code_b3, code_a3, jev.count()))

    for c in (a, b):
        try:
            c.clear("all")
        except Exception:
            pass


CASES = {"R1": case_R1, "R2": case_R2}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--case", default="", help="R1,R2，默认全跑")
    a = ap.parse_args()
    A = Client(os.environ.get("DISTILL_BASE_URL_A", "http://127.0.0.1:3101"),
               os.environ.get("DISTILL_ADMIN_TOKEN_A", ""),
               os.environ.get("DISTILL_RELAY_KEY_A", ""), "A")
    B = Client(os.environ.get("DISTILL_BASE_URL_B", "http://127.0.0.1:3102"),
               os.environ.get("DISTILL_ADMIN_TOKEN_B", ""),
               os.environ.get("DISTILL_RELAY_KEY_B", ""), "B")
    if not (A.token and A.relay_key and B.token and B.relay_key):
        sys.exit("需要 A/B 两套 DISTILL_*_A / *_B 环境变量")

    r = Redis(REDIS_ADDR, REDIS_PASSWORD, REDIS_DB)
    print("      redis PING -> %s | 键前缀 distill:* 当前 %d 个" %
          (r.ping(), len(r.scan_all())), flush=True)

    jev = Jev(18098)
    jev.start()
    upstream = Upstream(UPSTREAM_PORT)
    upstream.start()
    up_base = "http://127.0.0.1:%d" % UPSTREAM_PORT
    ids = {}
    try:
        for tag, c in (("A", A), ("B", B)):
            c.drop_channel("hermes-acc-%s" % tag.lower())
            ids[tag] = c.ensure_channel("hermes-acc-%s" % tag.lower(), up_base)
            c.detect_id = ids[tag]
        print("      渠道 A=%s B=%s 模型=%s" % (ids["A"], ids["B"], MODEL), flush=True)

        want = [x.strip().upper() for x in a.case.split(",") if x.strip()] or list(CASES)
        if "R1" in want:
            case_R1(A, r, jev)
        if "R2" in want:
            case_R2(A, B, r, jev)
    finally:
        for tag, c in (("A", A), ("B", B)):
            try:
                c.drop_channel("hermes-acc-%s" % tag.lower())
            except Exception:
                pass
        jev.stop()
        upstream.stop()

    print("\n== 汇总 ==", flush=True)
    fail = 0
    for c, n, s, _ in results:
        if s == "FAIL":
            fail += 1
        print("  %-4s %-3s %s" % (s, c, n))
    judged = len([x for x in results if x[2] in ("PASS", "FAIL")])
    print("\n共 %d 条判定，失败 %d 条。" % (judged, fail))
    sys.exit(1 if fail else 0)


if __name__ == "__main__":
    main()
