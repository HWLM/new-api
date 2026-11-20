#!/usr/bin/env python3
"""速率限制兜底拆分（§21）验收：五条规格用例 + 拒答降级回归 + 旧行为回归

对应 docs/rate-limit-fallback-split.md §21.1–§21.6。黑盒，只走 HTTP；并发用线程同时发，
上游由脚本自带（提示里写 `slow:2500` 就 sleep 2500ms），不依赖外部慢速上游。

环境变量：
  RL_BASE_URL        被测实例，默认 http://127.0.0.1:3101
  RL_ADMIN_TOKEN     管理员令牌
  RL_RELAY_KEY       普通用户 Key
  RL_USER_ID         该用户 id，默认 1
  RL_MODEL           测试模型名，默认 rl-model
  RL_UPSTREAM_PORT   假上游端口，默认 18110
  RL_SLOW_MS         慢请求延时，默认 2500
  RL_REDIS_ADDR / RL_REDIS_DB / RL_REDIS_PASSWORD
                     可选；RL5 需要往 Redis 写 refusal:demote 键来伪造降级态，
                     不给则 RL5 打 SKIP（内存模式下没法从外部造降级态）

用例：
  RL1 分组 {limit:2,fallback:true} 并发 3 → 第 3 条走全局兜底渠道，日志 group 仍是原分组
  RL2 同配置 fallback:false        → 第 3 条 429，code=rate_limit_exceeded，文案是「未启用兜底」
  RL3 不配兜底渠道（0）/ 兜底渠道不支持该模型 → 429，文案是「兜底渠道不可用」
  RL4 只超总并发、没配分组额度 → 旧行为：走**渠道自配**的 FallbackChannelId，不走全局兜底
  RL5 拒答降级回归 → 仍走渠道自配兜底、计费分组不变、429 文案与 code 与改动前一致
  RL6 429 形态并排断言：速率 429 带 code，拒答 429 不带 code（两条路径不许合并）
"""
import argparse
import json
import os
import socket
import sys
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

BASE = os.environ.get("RL_BASE_URL", "http://127.0.0.1:3101").rstrip("/")
ADMIN = os.environ.get("RL_ADMIN_TOKEN", "")
RELAY_KEY = os.environ.get("RL_RELAY_KEY", "")
USER_ID = int(os.environ.get("RL_USER_ID", "1") or 1)
MODEL = os.environ.get("RL_MODEL", "rl-model")
MODEL_OTHER = MODEL + "-other"
MODEL_HOLD = MODEL + "-hold"    # 占位请求用的非白名单模型：只占并发槽，不参与蒸馏判定
UPSTREAM_PORT = int(os.environ.get("RL_UPSTREAM_PORT", "18110"))
JEV_PORT = int(os.environ.get("RL_JEV_PORT", "18097"))
SLOW_MS = int(os.environ.get("RL_SLOW_MS", "2500"))
REDIS_ADDR = os.environ.get("RL_REDIS_ADDR", "")
REDIS_DB = int(os.environ.get("RL_REDIS_DB", "0"))
REDIS_PASSWORD = os.environ.get("RL_REDIS_PASSWORD", "")

results = []


def report(case, name, status, detail=""):
    results.append((case, name, status, detail))
    line = "[%s] %s %s" % (status, case, name)
    if detail:
        line += " — " + detail
    print(line, flush=True)


# ── HTTP ────────────────────────────────────────────────────────────────
def http(method, path, body=None, token=None, timeout=120):
    data = None if body is None else json.dumps(body).encode()
    r = urllib.request.Request(BASE + path, data=data, method=method)
    r.add_header("content-type", "application/json")
    if token:
        r.add_header("authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(r, timeout=timeout) as resp:
            return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")
    except Exception as e:
        return 0, "transport error: %s" % e


def admin(method, path, body=None):
    return http(method, path, body, ADMIN)


def must(code, txt, what):
    if code != 200:
        raise RuntimeError("%s -> HTTP %s %s" % (what, code, txt[:200]))
    try:
        j = json.loads(txt)
    except Exception:
        raise RuntimeError("%s 返回非 JSON: %s" % (what, txt[:200]))
    if j.get("success") is False:
        raise RuntimeError("%s 失败: %s" % (what, txt[:200]))
    return j


def relay(text, timeout=120, model=None):
    return http("POST", "/v1/chat/completions", {
        "model": model or MODEL, "stream": False, "max_tokens": 8,
        "messages": [{"role": "user", "content": text}],
    }, RELAY_KEY, timeout=timeout)


def burst(n, tag, slow_ms=SLOW_MS):
    """同时发 n 条，全部落在同一时刻，用来把并发槽位占满。"""
    out = []
    lock = threading.Lock()

    def one(i):
        code, txt = relay("%s slow:%d #%d %f" % (tag, slow_ms, i, time.time()))
        with lock:
            out.append((i, code, txt))

    ts = [threading.Thread(target=one, args=(i,)) for i in range(n)]
    for t in ts:
        t.start()
    for t in ts:
        t.join()
    return out


def codes(res):
    return sorted(c for _, c, _ in res)


def body_of(res, code):
    for _, c, txt in res:
        if c == code:
            return txt
    return ""


# ── 渠道 / 配置 / 用户 ──────────────────────────────────────────────────
def channels():
    j = must(*admin("GET", "/api/channel/?p=1&page_size=200"), "列渠道")
    d = j.get("data") or {}
    if isinstance(d, list):
        return d
    return d.get("items") or []


def find_channel(name):
    for ch in channels():
        if ch.get("name") == name:
            return ch
    return None


def ensure_channel(name, models, priority=0, fallback_channel_id=0):
    ch = find_channel(name)
    if ch:
        return int(ch["id"])
    must(*admin("POST", "/api/channel/", {"mode": "single", "channel": {
        "name": name, "type": 1, "key": "sk-rl-stub",
        "base_url": "http://127.0.0.1:%d" % UPSTREAM_PORT,
        "models": ",".join(models), "group": "default", "status": 1,
        "priority": priority, "fallback_channel_id": fallback_channel_id,
    }}), "建渠道 " + name)
    # 开了 Redis 的实例：abilities 缓存要重建，否则 distributor 报「无可用渠道」
    admin("POST", "/api/channel/fix")
    time.sleep(0.5)
    ch = find_channel(name)
    if not ch:
        raise RuntimeError("建渠道 %s 后查不到" % name)
    return int(ch["id"])


def patch_channel(cid, patch):
    """PUT /api/channel/ 只接受白名单字段：带 status 会直接「无效的参数」，
    从 GET 拿到的整条对象里有 status/channel_info 等只读字段，不能原样回传。"""
    cur = None
    for c in channels():
        if int(c["id"]) == int(cid):
            cur = c
    if cur is None:
        raise RuntimeError("渠道 %s 不存在" % cid)
    body = {
        "id": int(cid),
        "name": cur.get("name"),
        "type": cur.get("type"),
        "base_url": cur.get("base_url"),
        "models": cur.get("models"),
        "group": cur.get("group"),
        "priority": cur.get("priority"),
        "fallback_channel_id": int(cur.get("fallback_channel_id") or 0),
        "key": "sk-rl-stub",
    }
    body.update(patch)
    must(*admin("PUT", "/api/channel/", body), "改渠道 %s" % cid)


def drop_channel(name):
    ch = find_channel(name)
    if ch:
        admin("DELETE", "/api/channel/%d" % int(ch["id"]))
        admin("POST", "/api/channel/fix")


def set_option(key, value):
    must(*admin("PUT", "/api/option/", {"key": key, "value": str(value)}), "设置 %s" % key)


def get_option(key):
    j = must(*admin("GET", "/api/option/"), "读 options")
    for o in j.get("data") or []:
        if o.get("key") == key:
            return o.get("value")
    return None


def user_get():
    j = must(*admin("GET", "/api/user/%d" % USER_ID), "读用户")
    return j["data"]


def user_put(user):
    return admin("PUT", "/api/user/", user)


def set_user(**fields):
    u = user_get()
    u.update(fields)
    code, txt = user_put(u)
    if code != 200 or '"success":false' in txt.replace(" ", ""):
        raise RuntimeError("改用户失败: %s %s" % (code, txt[:200]))


def max_log_id():
    rows = logs_since(int(time.time()) - 3600, limit=20)
    return max([int(r["id"]) for r in rows if r.get("id") is not None] or [0])


def logs_after(base_id, ts, limit=50):
    return [r for r in logs_since(ts, limit) if int(r.get("id") or 0) > base_id]


def logs_since(ts, limit=50):
    j = must(*admin("GET", "/api/log/?p=1&page_size=%d&type=2&start_timestamp=%d&model_name=%s"
                    % (limit, ts, MODEL)), "读日志")
    d = j.get("data") or {}
    if isinstance(d, list):
        return d
    return d.get("items") or []


# ── 蒸馏配置（RL8 要让用户处于蒸馏降级态）──────────────────────────────
DISTILL_FB_NAME = "rl-distillfb"


def distill_get():
    j = must(*admin("GET", "/api/distill_detect/setting"), "读蒸馏配置")
    return j.get("data") or {}


def distill_put(patch):
    s = distill_get()
    body = {k: s[k] for k in (
        "enabled", "activate_rpm", "judge_interval_seconds", "confirm_hits",
        "confirm_window_seconds", "threshold", "demote_seconds", "observe_only",
        "jev_base_url", "alert_telegram", "alert_wecom", "alert_throttle_seconds",
        "wecom_webhook_url", "fallback_channel_id", "dedup_seconds",
        "detect_channel_ids", "jev_timeout_ms", "questions") if k in s}
    body["jev_api_key"] = ""
    body.update(patch)
    must(*admin("PUT", "/api/distill_detect/setting", body), "写蒸馏配置")


# ── Redis（RL5 造拒答降级态、RL8 造蒸馏降级态）──────────────────────────
class Redis:
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
        t, body = line[:1], line[1:-2]
        if t == b"+":
            return body.decode()
        if t == b"-":
            raise RuntimeError("redis: " + body.decode())
        if t == b":":
            return int(body)
        if t == b"$":
            n = int(body)
            if n < 0:
                return None
            return self.f.read(n + 2)[:-2].decode("utf-8", "replace")
        if t == b"*":
            return [self._read() for _ in range(int(body))]
        raise RuntimeError("RESP %r" % t)


# ── 假上游：提示里带 slow:N 就 sleep N 毫秒 ─────────────────────────────
class SlowUpstream:
    def __init__(self, port):
        outer = self
        self.calls = 0

        class H(BaseHTTPRequestHandler):
            def do_POST(self):
                n = int(self.headers.get("content-length") or 0)
                raw = self.rfile.read(n)
                try:
                    body = json.loads(raw or b"{}")
                except Exception:
                    body = {}
                text = json.dumps(body)
                delay = 0
                idx = text.find("slow:")
                if idx >= 0:
                    digits = ""
                    for ch in text[idx + 5:]:
                        if ch.isdigit():
                            digits += ch
                        else:
                            break
                    delay = int(digits or 0)
                outer.calls += 1
                if delay:
                    time.sleep(delay / 1000.0)
                payload = json.dumps({
                    "id": "chatcmpl-rl", "object": "chat.completion", "created": int(time.time()),
                    "model": body.get("model") or MODEL,
                    "choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"},
                                 "finish_reason": "stop"}],
                    "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
                }).encode()
                self.send_response(200)
                self.send_header("content-type", "application/json")
                self.send_header("content-length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, *a):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", port), H)

    def start(self):
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def stop(self):
        self.server.shutdown()


class JevStub:
    """假 Jev：正文含 hitmarker 给 0.9（命中），否则 0.05。"""

    def __init__(self, port):
        self.port = port
        self.calls = []

        class H(BaseHTTPRequestHandler):
            def do_POST(self):
                n = int(self.headers.get("content-length") or 0)
                raw = self.rfile.read(n)
                try:
                    body = json.loads(raw or b"{}")
                except Exception:
                    body = {}
                state = body.get("state") or ""
                qs = list((body.get("questions") or {}).keys()) or ["train.corpus_farm"]
                self.server_owner.calls.append(len(state))
                score = 0.90 if "hitmarker" in state else 0.05
                payload = json.dumps({"answers": {q: {"noul": score} for q in qs},
                                      "model": "fake-jev"}).encode()
                self.send_response(200)
                self.send_header("content-type", "application/json")
                self.send_header("content-length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, *a):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", port), H)
        H.server_owner = self

    def start(self):
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def stop(self):
        self.server.shutdown()


def hits_for_request(request_id):
    j = must(*admin("GET", "/api/distill_detect/hits?p=1&page_size=50"), "读命中记录")
    d = j.get("data") or {}
    items = d.get("items") or d.get("records") or []
    return [h for h in items if h.get("request_id") == request_id]


# ── 用例 ────────────────────────────────────────────────────────────────
def prepare(ids):
    """统一把并发限制打开，模型走白名单无关的普通链路。"""
    set_option("SelfUseModeEnabled", "true")
    set_option("MaxConcurrentRequestsPerUserEnabled", "true")


def concurrent_case(case, ids, group_cfg, global_fb, n, expect_fb, tag):
    """并发 n 条，断言落到哪个渠道。返回 (res, rows)。"""
    set_option("RateLimitFallbackChannelId", global_fb)
    set_user(max_concurrency=-1, group_rate_limits=json.dumps(group_cfg) if group_cfg else "")
    time.sleep(0.3)
    t0 = int(time.time()) - 1
    base = max_log_id()          # 用日志 id 划界：只按秒切会把上一条用例的行算进来
    res = burst(n, tag)
    time.sleep(1.2)
    return res, logs_after(base, t0)


def case_RL1(ids):
    """§21.6-1 分组 fallback:true → 第 3 条走全局兜底渠道，计费分组不变"""
    case = "RL1"
    res, rows = concurrent_case(case, ids, {"default": {"limit": 2, "fallback": True}},
                                ids["global_fb"], 3, ids["global_fb"], "RL1")
    served = {int(r.get("channel") or 0) for r in rows}
    fb_rows = [r for r in rows if int(r.get("channel") or 0) == ids["global_fb"]]
    report(case, "3 条并发全部 200", "PASS" if codes(res) == [200, 200, 200] else "FAIL",
           "状态码 %s" % codes(res))
    report(case, "其中 1 条落到全局兜底渠道", "PASS" if len(fb_rows) == 1 else "FAIL",
           "兜底渠道=%s，实际服务渠道集合=%s，日志 %d 行" %
           (ids["global_fb"], sorted(served), len(rows)))
    group_ok = bool(fb_rows) and all(r.get("group") == "default" for r in fb_rows)
    report(case, "兜底那条的日志 group 仍是原分组", "PASS" if group_ok else "FAIL",
           "兜底行 group=%s" % (fb_rows[0].get("group") if fb_rows else "-"))


def case_RL2(ids):
    """§21.6-2 fallback:false → 第 3 条 429 + rate_limit_exceeded + 「未启用兜底」"""
    case = "RL2"
    res, rows = concurrent_case(case, ids, {"default": {"limit": 2, "fallback": False}},
                                ids["global_fb"], 3, None, "RL2")
    body = body_of(res, 429)
    rejected = sum(1 for _, c, _ in res if c == 429)
    report(case, "超限那条 429", "PASS" if rejected == 1 else "FAIL", "状态码 %s" % codes(res))
    report(case, "错误码 rate_limit_exceeded", "PASS" if "rate_limit_exceeded" in body else "FAIL",
           body[:150])
    report(case, "文案是「未启用兜底」", "PASS" if "未启用兜底" in body else "FAIL", body[:150])
    went_fb = [r for r in rows if int(r.get("channel") or 0) == ids["global_fb"]]
    report(case, "没有偷偷走兜底渠道", "PASS" if not went_fb else "FAIL",
           "兜底渠道命中 %d 条" % len(went_fb))


def case_RL3(ids):
    """§21.6-3 兜底渠道不可用 → 429 + rate_limit_exceeded + 「兜底不可用」"""
    case = "RL3"
    for label, fb in (("未配兜底渠道(0)", 0), ("兜底渠道不支持该模型", ids["bad_fb"])):
        res, rows = concurrent_case(case, ids, {"default": {"limit": 2, "fallback": True}}, fb,
                                    3, None, "RL3")
        body = body_of(res, 429)
        report(case, "%s → 429" % label, "PASS" if 429 in codes(res) else "FAIL",
               "状态码 %s" % codes(res))
        report(case, "%s → 兜底不可用文案" % label,
               "PASS" if ("兜底渠道不可用" in body and "rate_limit_exceeded" in body) else "FAIL",
               body[:150])


def case_RL4(ids):
    """§21.6-4 只超总并发、没配分组额度 → 旧行为：走渠道自配兜底，不走全局兜底"""
    case = "RL4"
    set_option("RateLimitFallbackChannelId", ids["global_fb"])
    patch_channel(ids["main"], {"fallback_channel_id": ids["old_fb"]})
    set_user(max_concurrency=2, group_rate_limits="")
    time.sleep(0.3)
    t0 = int(time.time()) - 1
    base = max_log_id()
    res = burst(3, "RL4")
    time.sleep(1.2)
    rows = logs_after(base, t0)
    served = sorted({int(r.get("channel") or 0) for r in rows})
    report(case, "3 条并发全部 200（旧行为不拒）", "PASS" if codes(res) == [200, 200, 200] else "FAIL",
           "状态码 %s" % codes(res))
    report(case, "超限那条走渠道自配兜底", "PASS" if ids["old_fb"] in served else "FAIL",
           "渠道自配兜底=%s，实际服务渠道=%s" % (ids["old_fb"], served))
    report(case, "没有误用新的全局速率兜底", "PASS" if ids["global_fb"] not in served else "FAIL",
           "全局兜底=%s，实际服务渠道=%s" % (ids["global_fb"], served))


def case_RL5(ids):
    """§21.6-5 拒答降级回归：仍走渠道自配兜底，计费分组不变"""
    case = "RL5"
    if not REDIS_ADDR:
        report(case, "拒答降级回归", "SKIP",
               "需要 RL_REDIS_ADDR 才能写 refusal:demote:{user:N} 造降级态；内存模式无法从外部伪造")
        return
    r = Redis(REDIS_ADDR, REDIS_PASSWORD, REDIS_DB)
    set_option("RateLimitFallbackChannelId", ids["global_fb"])
    set_user(max_concurrency=-1, group_rate_limits="", refusal_demote_override=1)
    patch_channel(ids["main"], {"fallback_channel_id": ids["old_fb"]})
    r.cmd("SET", "refusal:demote:{user:%d}" % USER_ID, "1", "EX", "600")
    time.sleep(0.5)

    t0 = int(time.time()) - 1
    base = max_log_id()
    code, txt = relay("RL5 拒答降级回归 %f" % time.time(), timeout=60)
    time.sleep(1.2)
    rows = logs_after(base, t0)
    fb_rows = [x for x in rows if int(x.get("channel") or 0) == ids["old_fb"]]
    report(case, "降级态请求走渠道自配兜底", "PASS" if fb_rows else "FAIL",
           "HTTP %s，日志渠道=%s，期望 %s" %
           (code, sorted({int(x.get("channel") or 0) for x in rows}), ids["old_fb"]))
    report(case, "没有串到全局速率兜底", "PASS"
           if not [x for x in rows if int(x.get("channel") or 0) == ids["global_fb"]] else "FAIL",
           "全局兜底=%s" % ids["global_fb"])
    report(case, "计费分组仍是原分组", "PASS" if fb_rows and fb_rows[0].get("group") == "default" else "FAIL",
           "group=%s" % (fb_rows[0].get("group") if fb_rows else "-"))

    # 渠道没配兜底时：429 文案必须是拒答那套，且**不带** rate_limit_exceeded
    patch_channel(ids["main"], {"fallback_channel_id": 0})
    admin("POST", "/api/channel/fix")
    time.sleep(0.5)
    code2, txt2 = relay("RL5 拒答降级无兜底 %f" % time.time(), timeout=60)
    report(case, "无兜底时 429", "PASS" if code2 == 429 else "FAIL", "HTTP %s" % code2)
    report(case, "拒答文案不变（风控降级口径）",
           "PASS" if ("风控降级" in txt2 and "rate_limit_exceeded" not in txt2) else "FAIL",
           txt2[:160])

    r.cmd("DEL", "refusal:demote:{user:%d}" % USER_ID)
    set_user(refusal_demote_override=0)
    patch_channel(ids["main"], {"fallback_channel_id": ids["old_fb"]})


def case_RL6(ids):
    """429 形态并排：速率 429 带 code，拒答 429 不带 code —— 两条路径不许合并"""
    case = "RL6"
    set_option("RateLimitFallbackChannelId", 0)
    set_user(max_concurrency=-1, group_rate_limits=json.dumps({"default": {"limit": 1, "fallback": False}}))
    time.sleep(0.3)
    res = burst(2, "RL6")
    time.sleep(0.8)
    rate_body = body_of(res, 429)
    report(case, "速率 429 带 rate_limit_exceeded", "PASS" if "rate_limit_exceeded" in rate_body else "FAIL",
           rate_body[:150])
    report(case, "速率 429 文案是并发超限口径", "PASS" if "并发" in rate_body else "FAIL", rate_body[:120])


def case_RL7(ids):
    """分组写了条目但 limit<=0 —— 等于没配分组速率，只超总并发必须走旧行为（渠道自配兜底）"""
    case = "RL7"
    set_option("RateLimitFallbackChannelId", ids["global_fb"])
    patch_channel(ids["main"], {"fallback_channel_id": ids["old_fb"]})
    for label, cfg in (("limit=0 + fallback:false", {"default": {"limit": 0, "fallback": False}}),
                       ("limit=-1 + fallback:false", {"default": {"limit": -1, "fallback": False}}),
                       ("limit=0 + fallback:true", {"default": {"limit": 0, "fallback": True}})):
        set_user(max_concurrency=2, group_rate_limits=json.dumps(cfg))
        time.sleep(0.3)
        t0 = int(time.time()) - 1
        base = max_log_id()
        res = burst(3, "RL7")
        time.sleep(1.2)
        rows = logs_after(base, t0)
        served = sorted({int(r.get("channel") or 0) for r in rows})
        report(case, "%s → 不 429" % label, "PASS" if codes(res) == [200, 200, 200] else "FAIL",
               "状态码 %s（429 说明 limit<=0 被当成启用了分组策略）" % codes(res))
        report(case, "%s → 超限走渠道自配兜底" % label, "PASS" if ids["old_fb"] in served else "FAIL",
               "自配兜底=%s，实际服务渠道=%s" % (ids["old_fb"], served))
        report(case, "%s → 不走全局速率兜底" % label, "PASS" if ids["global_fb"] not in served else "FAIL",
               "全局兜底=%s，实际服务渠道=%s" % (ids["global_fb"], served))


def case_RL8(ids):
    """蒸馏降级态 + 并发超限 → 直接 429（不跳第二次兜底），且不超限时照常走蒸馏兜底"""
    case = "RL8"
    if not REDIS_ADDR:
        report(case, "降级态并发超限 → 429", "SKIP",
               "需要 RL_REDIS_ADDR 才能写 distill:demote:{user:N} 造假降级态")
        return
    r = Redis(REDIS_ADDR, REDIS_PASSWORD, REDIS_DB)
    demote_key = "distill:demote:{user:%d}" % USER_ID

    # 三条兜底链路给三个不同渠道：自配(2) / 全局速率(3) / 蒸馏(5)，看落点就知道走了哪条
    patch_channel(ids["main"], {"fallback_channel_id": ids["old_fb"]})
    set_option("RateLimitFallbackChannelId", ids["global_fb"])
    distill_put({"enabled": True, "observe_only": False, "activate_rpm": 1,
                 "confirm_hits": 1, "confirm_window_seconds": 300, "threshold": 0.6,
                 "demote_seconds": 900, "fallback_channel_id": ids["distill_fb"],
                 "detect_channel_ids": [ids["main"]], "dedup_seconds": 0,
                 "jev_base_url": "http://127.0.0.1:18098/v1/systemone"})
    set_user(max_concurrency=1, group_rate_limits="")
    r.cmd("SET", demote_key, "1", "EX", "900")
    time.sleep(0.5)

    # 控制组：降级 + 未超限 → 仍应正常放行并落到蒸馏兜底渠道
    t0 = int(time.time()) - 1
    base = max_log_id()
    code1, _ = relay("RL8 降级未超限 %f" % time.time(), timeout=60)
    time.sleep(1.2)
    rows1 = logs_after(base, t0)
    served1 = sorted({int(x.get("channel") or 0) for x in rows1})
    report(case, "降级+未超限 → 200 且走蒸馏兜底",
           "PASS" if (code1 == 200 and ids["distill_fb"] in served1) else "FAIL",
           "HTTP %s，服务渠道=%s，蒸馏兜底=%s" % (code1, served1, ids["distill_fb"]))

    # 降级 + limit=1 并发 3 → 只该过 1 条，另外 2 条 429，且不许再跳第二跳
    t0 = int(time.time()) - 1
    base = max_log_id()
    res = burst(3, "RL8")
    time.sleep(1.2)
    got = codes(res)
    bodies = [b for _, c, b in res if c == 429]
    rows = logs_after(base, t0)
    served = sorted({int(x.get("channel") or 0) for x in rows})
    report(case, "降级+超限 → [200,429,429]", "PASS" if got == [200, 429, 429] else "FAIL",
           "状态码 %s（全 200 说明并发限制又被降级绕过）" % got)
    report(case, "两条 429 都是 rate_limit_exceeded",
           "PASS" if bodies and all("rate_limit_exceeded" in b for b in bodies) else "FAIL",
           (bodies[0][:140] if bodies else "没有 429"))
    b0 = bodies[0] if bodies else ""
    report(case, "429 文案是并发超限、不混蒸馏兜底措辞",
           "PASS" if ("并发请求数已超限" in b0 and "兜底渠道不可用" not in b0) else "FAIL",
           b0[:140])
    report(case, "429 没有产生第二次跳转（日志只有通过的那条）",
           "PASS" if (len(rows) == 1 and served == [ids["distill_fb"]]) else "FAIL",
           "日志 %d 行，渠道=%s，期望 [%s]" % (len(rows), served, ids["distill_fb"]))

    r.cmd("DEL", demote_key)
    distill_put({"enabled": False, "fallback_channel_id": 0, "detect_channel_ids": []})


def case_RL9(ids):
    """重导模式下「命中即第 N 次确认、同时并发超限被 429」的命中行不能记成已重导"""
    case = "RL9"
    if not REDIS_ADDR:
        report(case, "被 429 的命中行 fallback_id 为空", "SKIP", "需要 RL_REDIS_ADDR 清降级态")
        return
    r = Redis(REDIS_ADDR, REDIS_PASSWORD, REDIS_DB)
    r.cmd("DEL", "distill:demote:{user:%d}" % USER_ID)

    patch_channel(ids["main"], {"fallback_channel_id": ids["old_fb"]})
    set_option("RateLimitFallbackChannelId", ids["global_fb"])
    distill_put({"enabled": True, "observe_only": False, "activate_rpm": 1,
                 "confirm_hits": 1, "confirm_window_seconds": 300, "threshold": 0.6,
                 "demote_seconds": 600, "fallback_channel_id": ids["distill_fb"],
                 "detect_channel_ids": [ids["main"]], "dedup_seconds": 0,
                 "judge_interval_seconds": 30,
                 "jev_base_url": "http://127.0.0.1:%d/v1/systemone" % JEV_PORT})
    set_user(max_concurrency=1, group_rate_limits="")
    time.sleep(0.5)

    # A 先占住唯一槽位：走**非白名单**模型，这样它不会消耗本次判定的时间桶
    # （judge_interval 是按用户分桶的，白名单被 A 占掉的话 B 会被桶跳过，根本不会命中）
    holder = {}
    def hold():
        holder["res"] = relay("RL9 占位请求 slow:%d" % SLOW_MS, timeout=60, model=MODEL_HOLD)
    t = threading.Thread(target=hold)
    t.start()
    time.sleep(0.4)
    t_hit = int(time.time())
    code, txt = relay("RL9 hitmarker 命中并发超限 %f" % time.time(), timeout=60)
    t.join()

    report(case, "命中且超限的那条被 429", "PASS" if code == 429 else "FAIL",
           "B HTTP %s；占位请求 HTTP %s" % (code, (holder.get("res") or [""])[0]))
    report(case, "429 带 rate_limit_exceeded", "PASS" if "rate_limit_exceeded" in txt else "FAIL",
           txt[:140])

    rid = ""
    import re as _re
    m = _re.search(r"request id: ([0-9a-zA-Z]+)", txt)
    if m:
        rid = m.group(1)
    rows = hits_for_request(rid) if rid else []
    report(case, "命中记录已落库（场景确实发生）", "PASS" if rows else "FAIL",
           "request_id=%s 命中行 %d 条" % (rid or "-", len(rows)))
    if rows:
        fb = max(int(h.get("fallback_id") or 0) for h in rows)
        report(case, "被 429 的命中行 fallback_id 为空", "PASS" if fb == 0 else "FAIL",
               "fallback_id=%d（非 0 说明记成已重导了）" % fb)


CASES = {"RL1": case_RL1, "RL2": case_RL2, "RL3": case_RL3, "RL4": case_RL4,
         "RL5": case_RL5, "RL6": case_RL6,
         "RL7": case_RL7,
         "RL8": case_RL8,
         "RL9": case_RL9}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--case", default="")
    ap.add_argument("--keep", action="store_true", help="跑完不删渠道、不还原配置")
    a = ap.parse_args()
    if not ADMIN or not RELAY_KEY:
        sys.exit("需要 RL_ADMIN_TOKEN 与 RL_RELAY_KEY")

    up = SlowUpstream(UPSTREAM_PORT)
    up.start()
    jev = JevStub(JEV_PORT)
    jev.start()
    time.sleep(0.5)
    saved_user = None
    saved_opts = {}
    ids = {}
    try:
        code, txt = http("POST", "http://127.0.0.1:%d/v1/chat/completions" % UPSTREAM_PORT,
                         {"model": MODEL, "messages": [{"role": "user", "content": "preflight"}]})
        print("      自检 upstream -> %s %s" % (code, txt[:40]), flush=True)

        for nm in ("rl-main", "rl-oldfb", "rl-globalfb", "rl-badfb", DISTILL_FB_NAME, "rl-hold"):
            drop_channel(nm)
        ids["main"] = ensure_channel("rl-main", [MODEL], priority=100)
        ids["old_fb"] = ensure_channel("rl-oldfb", [MODEL], priority=0)
        ids["global_fb"] = ensure_channel("rl-globalfb", [MODEL], priority=0)
        ids["bad_fb"] = ensure_channel("rl-badfb", [MODEL_OTHER], priority=0)
        ids["distill_fb"] = ensure_channel(DISTILL_FB_NAME, [MODEL], priority=0)
        ids["hold"] = ensure_channel("rl-hold", [MODEL_HOLD], priority=0)
        patch_channel(ids["main"], {"fallback_channel_id": ids["old_fb"]})
        admin("POST", "/api/channel/fix")
        time.sleep(0.5)
        print("      渠道 main=%d old_fb=%d global_fb=%d bad_fb=%d distill_fb=%d 模型=%s" %
              (ids["main"], ids["old_fb"], ids["global_fb"], ids["bad_fb"],
               ids["distill_fb"], MODEL), flush=True)
        # 除 RL8 外都别让蒸馏降级掺进来
        try:
            distill_put({"enabled": False, "detect_channel_ids": [], "fallback_channel_id": 0})
        except Exception as e:
            print("      !! 关闭蒸馏失败:", repr(e)[:120], flush=True)

        saved_user = user_get()
        for k in ("MaxConcurrentRequestsPerUserEnabled", "RateLimitFallbackChannelId", "SelfUseModeEnabled"):
            saved_opts[k] = get_option(k)
        prepare(ids)

        want = [x.strip().upper() for x in a.case.split(",") if x.strip()] or list(CASES)
        for name in want:
            fn = CASES.get(name)
            if not fn:
                continue
            try:
                fn(ids)
            except Exception as e:
                report(name, "用例执行异常", "FAIL", repr(e)[:200])
    finally:
        if not a.keep:
            try:
                if saved_user:
                    user_put(saved_user)
                for k, v in saved_opts.items():
                    if v is not None:
                        set_option(k, v)
                patch_channel(ids["main"], {"fallback_channel_id": 0}) if ids.get("main") else None
                for nm in ("rl-main", "rl-oldfb", "rl-globalfb", "rl-badfb", DISTILL_FB_NAME, "rl-hold"):
                    drop_channel(nm)
            except Exception as e:
                print("!! 收尾失败（需手工清理）:", repr(e)[:200], flush=True)
        up.stop()
        jev.stop()

    print("\n== 汇总 ==", flush=True)
    fail = 0
    for c, n, s, _ in results:
        if s == "FAIL":
            fail += 1
        print("  %-4s %-4s %s" % (s, c, n))
    judged = len([x for x in results if x[2] in ("PASS", "FAIL")])
    print("\n共 %d 条判定，失败 %d 条。" % (judged, fail))
    sys.exit(1 if fail else 0)


if __name__ == "__main__":
    main()
