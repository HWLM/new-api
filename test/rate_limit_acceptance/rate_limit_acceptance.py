#!/usr/bin/env python3
"""速率限制兜底（§22：改走请求所在渠道自配的 FallbackChannelId）验收

对应 docs/rate-limit-fallback-split.md §21/§22。黑盒，只走 HTTP；并发用线程同时发，
上游由脚本自带（提示里写 `slow:2500` 就 sleep 2500ms），不依赖外部慢速上游。

§22 相对 §21 的变化：全局设置项 RateLimitFallbackChannelId 删除，速率超限后改跳
**请求所在渠道自配**的 FallbackChannelId（与拒答降级共用同一字段、单跳、不递归）。
用例断言跟着改了，不是把旧的全局兜底渠道换个名字：

  RL1  分组 {limit:2,fallback:true} 并发 3 → 第 3 条跳渠道自配兜底，计费分组不变
  RL2  同配置 fallback:false → 即使渠道配了兜底也 429，一跳都不跳
  RL3  兜底未配 / 不支持该模型 / 指向自己 → 429「该渠道未配置可用的兜底渠道」
  RL4  只超总并发、没配分组额度 → 旧行为：走渠道自配兜底
  RL5  拒答降级回归 → 仍走渠道自配兜底、计费分组不变、429 文案与 code 不变
  RL6  速率 429 带 code、并发口径，不混拒答措辞
  RL7  分组条目 limit<=0 → 等于没配，只超总并发时走旧行为
  RL8  蒸馏降级态 + 并发超限 → 429，不许第二次跳转
  RL9  重导模式下被 429 的命中行不能记成已重导（fallback_id=0）
  RL10 全局设置项确实删干净：options 里没有、写入不被接受；渠道字段仍可读写

独立启动时每个用例都会自己改 main 渠道的 fallback_channel_id 并重建 abilities 缓存。
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
ADMIN_USER = os.environ.get("RL_ADMIN_USER", "root")
ADMIN_PASSWORD = os.environ.get("RL_ADMIN_PASSWORD", "")
RELAY_KEY = os.environ.get("RL_RELAY_KEY", "")
USER_ID = int(os.environ.get("RL_USER_ID", "1") or 1)
MODEL = os.environ.get("RL_MODEL", "rl-model")
MODEL_OTHER = MODEL + "-other"
MODEL_HOLD = MODEL + "-hold"    # 占位请求用的非白名单模型：只占并发槽，不参与蒸馏判定
GEMINI_MODEL = os.environ.get("RL_GEMINI_MODEL", "rl-gemini-pro")   # 跨厂商用例：只用 gemini 渠道服务
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


def relogin():
    """access token 寿命很短（十几分钟），整套用例跑到后半段会 AUTH_TOKEN_EXPIRED。
    有 RL_ADMIN_PASSWORD 就自动重登换新 token，避免整套用例被过期打断。"""
    global ADMIN
    if not ADMIN_PASSWORD:
        return False
    code, txt = http("POST", "/api/user/login",
                     {"username": ADMIN_USER, "password": ADMIN_PASSWORD})
    if code != 200:
        return False
    try:
        j = json.loads(txt)
    except Exception:
        return False
    tok = (j.get("data") or {}).get("access_token") or j.get("access_token")
    if not tok:
        return False
    ADMIN = tok
    print("      [relogin] access token 已刷新", flush=True)
    return True


def admin(method, path, body=None, _retry=True):
    code, txt = http(method, path, body, ADMIN)
    if code == 401 and _retry and relogin():
        return http(method, path, body, ADMIN)
    return code, txt


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


def burst(n, tag, slow_ms=SLOW_MS, model=None):
    """同时发 n 条，全部落在同一时刻，用来把并发槽位占满。"""
    out = []
    lock = threading.Lock()

    def one(i):
        code, txt = relay("%s slow:%d #%d %f" % (tag, slow_ms, i, time.time()), model=model)
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


def ensure_channel(name, models, priority=0, fallback_channel_id=0, group="default", status=1):
    ch = find_channel(name)
    if ch:
        return int(ch["id"])
    must(*admin("POST", "/api/channel/", {"mode": "single", "channel": {
        "name": name, "type": 1, "key": "sk-rl-stub",
        "base_url": "http://127.0.0.1:%d" % UPSTREAM_PORT,
        "models": ",".join(models), "group": group, "status": status,
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
    """改用户。SQLite 库在 60s 定时同步时会短暂锁住（database is locked (5)），
    这跟被测逻辑无关，重试几次即可，否则用例会假红/假 SKIP。"""
    last = ""
    for attempt in range(4):
        u = user_get()
        u.update(fields)
        code, txt = user_put(u)
        if code == 200 and '"success":false' not in txt.replace(" ", ""):
            return
        last = "%s %s" % (code, txt[:200])
        if "database is locked" not in txt and "SQLITE_BUSY" not in txt:
            break
        time.sleep(0.8 * (attempt + 1))
    raise RuntimeError("改用户失败: %s" % last)


def max_log_id():
    rows = logs_since(int(time.time()) - 3600, limit=20)
    return max([int(r["id"]) for r in rows if r.get("id") is not None] or [0])


def logs_after(base_id, ts, limit=50, model=None):
    return [r for r in logs_since(ts, limit, model) if int(r.get("id") or 0) > base_id]


def logs_since(ts, limit=50, model=None):
    j = must(*admin("GET", "/api/log/?p=1&page_size=%d&type=2&start_timestamp=%d&model_name=%s"
                    % (limit, ts, model or MODEL)), "读日志")
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
    """统一把并发限制打开；渠道兜底字段由各用例自己设。"""
    set_option("SelfUseModeEnabled", "true")
    set_option("MaxConcurrentRequestsPerUserEnabled", "true")


def clear_distill_state():
    """清掉蒸馏降级态与命中计数。

    RL8/RL9 会让服务端写 distill:demote:{user:N}（TTL 600~900s），如果后面再跑
    非蒸馏用例，请求会先被降级到蒸馏兜底渠道，表现为 429 文案变成「并发请求数已超限，
    请联系管理员」、日志渠道变成蒸馏兜底 —— 这就是本套件早期那次 3 条假失败的原因。
    """
    if REDIS_ADDR:
        try:
            Redis(REDIS_ADDR, REDIS_PASSWORD, REDIS_DB).cmd(
                "DEL", "distill:demote:{user:%d}" % USER_ID)
        except Exception as e:
            print("      清蒸馏降级键失败: %s" % repr(e)[:100], flush=True)
    try:
        admin("POST", "/api/distill_detect/clear_cache", {"scope": "all"})
    except Exception as e:
        print("      清蒸馏缓存失败: %s" % repr(e)[:100], flush=True)


def set_main_fallback(ids, fb):
    """改 main 渠道自配的兜底，并重建 abilities 缓存（Redis 实例必需）。"""
    patch_channel(ids["main"], {"fallback_channel_id": int(fb)})
    admin("POST", "/api/channel/fix")
    time.sleep(0.4)


def concurrent_case(case, ids, group_cfg, channel_fb, n, tag):
    """把 main 的兜底设成 channel_fb，再并发发 n 条。返回 (res, rows)。"""
    set_main_fallback(ids, channel_fb)
    set_user(max_concurrency=-1, group_rate_limits=json.dumps(group_cfg) if group_cfg else "")
    time.sleep(0.3)
    t0 = int(time.time()) - 1
    base = max_log_id()          # 用日志 id 划界：只按秒切会把上一条用例的行算进来
    res = burst(n, tag)
    time.sleep(1.2)
    return res, logs_after(base, t0)


def served_channels(rows):
    return sorted({int(r.get("channel") or 0) for r in rows})


def case_RL1(ids):
    """§22-1 分组 fallback:true → 超限那条跳**请求所在渠道自配**的兜底，计费分组不变"""
    case = "RL1"
    clear_distill_state()
    res, rows = concurrent_case(case, ids, {"default": {"limit": 2, "fallback": True}},
                                ids["old_fb"], 3, "RL1")
    served = served_channels(rows)
    fb_rows = [r for r in rows if int(r.get("channel") or 0) == ids["old_fb"]]
    report(case, "3 条并发全部 200", "PASS" if codes(res) == [200, 200, 200] else "FAIL",
           "状态码 %s" % codes(res))
    report(case, "其中 1 条落到渠道自配兜底", "PASS" if len(fb_rows) == 1 else "FAIL",
           "自配兜底=%s，实际服务渠道=%s，日志 %d 行" % (ids["old_fb"], served, len(rows)))
    group_ok = bool(fb_rows) and all(r.get("group") == "default" for r in fb_rows)
    report(case, "兜底那条的日志 group 仍是原分组", "PASS" if group_ok else "FAIL",
           "兜底行 group=%s" % (fb_rows[0].get("group") if fb_rows else "-"))
    report(case, "没有跳到任何别的渠道（全局配置已删除）",
           "PASS" if ids["decoy_fb"] not in served else "FAIL",
           "诱饵渠道=%s，实际服务渠道=%s" % (ids["decoy_fb"], served))


def case_RL2(ids):
    """§22-2 fallback:false → 渠道即使配了兜底也直接 429，一跳都不跳"""
    case = "RL2"
    clear_distill_state()
    res, rows = concurrent_case(case, ids, {"default": {"limit": 2, "fallback": False}},
                                ids["old_fb"], 3, "RL2")
    body = body_of(res, 429)
    rejected = sum(1 for _, c, _ in res if c == 429)
    report(case, "超限那条 429", "PASS" if rejected == 1 else "FAIL", "状态码 %s" % codes(res))
    report(case, "错误码 rate_limit_exceeded", "PASS" if "rate_limit_exceeded" in body else "FAIL",
           body[:150])
    report(case, "文案是「未启用兜底」", "PASS" if "未启用兜底" in body else "FAIL", body[:150])
    served = served_channels(rows)
    report(case, "渠道虽配了兜底但没偷偷跳过去", "PASS" if ids["old_fb"] not in served else "FAIL",
           "自配兜底=%s，实际服务渠道=%s" % (ids["old_fb"], served))
    report(case, "诱饵渠道同样没被用", "PASS" if ids["decoy_fb"] not in served else "FAIL",
           "诱饵=%s，实际服务渠道=%s" % (ids["decoy_fb"], served))


def case_RL3(ids):
    """§22-3 兜底不可用（未配 / 模型不支持 / 自环）→ 429 +「该渠道未配置可用的兜底渠道」"""
    case = "RL3"
    clear_distill_state()
    for label, fb in (("未配兜底渠道(0)", 0), ("兜底渠道不支持该模型", ids["bad_fb"])):
        res, rows = concurrent_case(case, ids, {"default": {"limit": 2, "fallback": True}}, fb, 3, "RL3")
        body = body_of(res, 429)
        report(case, "%s → 429" % label, "PASS" if 429 in codes(res) else "FAIL",
               "状态码 %s" % codes(res))
        report(case, "%s → 「该渠道未配置可用的兜底渠道」+ code" % label,
               "PASS" if ("未配置可用的兜底渠道" in body and "rate_limit_exceeded" in body) else "FAIL",
               body[:160])
        report(case, "%s → 没有跳到任何渠道" % label,
               "PASS" if not [r for r in rows if int(r.get("channel") or 0) != ids["main"]] else "FAIL",
               "实际服务渠道=%s" % served_channels(rows))

    # 自环：兜底指回自己。必须 429，不能递归/卡死。
    try:
        set_main_fallback(ids, ids["main"])
        set_user(max_concurrency=-1, group_rate_limits=json.dumps({"default": {"limit": 2, "fallback": True}}))
        t0 = int(time.time()) - 1
        base = max_log_id()
        tstart = time.time()
        res = burst(3, "RL3-self")
        elapsed = time.time() - tstart
        rows = logs_after(base, t0)
        body = body_of(res, 429)
        report(case, "兜底指向自己(自环) → 429", "PASS" if 429 in codes(res) else "FAIL",
               "状态码 %s" % codes(res))
        report(case, "自环 → 同一套「未配置可用的兜底渠道」文案",
               "PASS" if ("未配置可用的兜底渠道" in body and "rate_limit_exceeded" in body) else "FAIL",
               body[:160])
        report(case, "自环没有递归/卡死（3 条在 30s 内返回）", "PASS" if elapsed < 30 else "FAIL",
               "耗时 %.1fs" % elapsed)
    except Exception as e:
        report(case, "兜底指向自己(自环)", "SKIP", "接口不接受自环设置：%s" % repr(e)[:120])

    # 兜底渠道已禁用 → 429（能力表里没有它，CacheGetChannel 拿不到或 status!=1）
    try:
        res, rows = concurrent_case(case, ids, {"default": {"limit": 2, "fallback": True}},
                                    ids["dead_fb"], 3, "RL3-dead")
        body = body_of(res, 429)
        report(case, "兜底渠道已禁用 → 429", "PASS" if 429 in codes(res) else "FAIL",
               "状态码 %s" % codes(res))
        report(case, "禁用兜底 → 同一套「未配置可用的兜底渠道」文案",
               "PASS" if ("未配置可用的兜底渠道" in body and "rate_limit_exceeded" in body) else "FAIL",
               body[:160])
        report(case, "禁用兜底没有被跳过去", "PASS" if ids["dead_fb"] not in served_channels(rows) else "FAIL",
               "禁用渠道=%s，实际服务渠道=%s" % (ids["dead_fb"], served_channels(rows)))
    except Exception as e:
        report(case, "兜底渠道已禁用 → 429", "SKIP", "禁用渠道构造失败：%s" % repr(e)[:120])

    # 跨厂商：gemini 请求的渠道配了「只支持 openai 模型」的兜底 → 必须 429，不许错跳
    try:
        patch_channel(ids["gemini"], {"fallback_channel_id": ids["bad_fb"]})
        admin("POST", "/api/channel/fix")
        set_user(max_concurrency=-1, group_rate_limits=json.dumps({"default": {"limit": 2, "fallback": True}}))
        time.sleep(0.4)
        t0 = int(time.time()) - 1
        base = max_log_id()
        res = burst(3, "RL3-xvendor", model=GEMINI_MODEL)
        time.sleep(1.2)
        rows = logs_after(base, t0, model=GEMINI_MODEL)
        served = served_channels(rows)
        body = body_of(res, 429)
        report(case, "跨厂商：gemini 请求不会被送进只认 openai 的兜底",
               "PASS" if (codes(res) == [200, 200, 429] and ids["bad_fb"] not in served) else "FAIL",
               "状态码 %s，实际服务渠道=%s，兜底(只认 %s)=%s" % (codes(res), served, MODEL_OTHER, ids["bad_fb"]))
        report(case, "跨厂商 → 同一套「未配置可用的兜底渠道」文案",
               "PASS" if "未配置可用的兜底渠道" in body else "FAIL", body[:160])
    except Exception as e:
        report(case, "跨厂商：gemini 请求不会被送进只认 openai 的兜底", "SKIP",
               "gemini 渠道构造失败：%s" % repr(e)[:120])


def case_RL4(ids):
    """§22-4 只超总并发、没配分组额度 → 旧行为：走渠道自配兜底"""
    case = "RL4"
    clear_distill_state()
    set_main_fallback(ids, ids["old_fb"])
    set_user(max_concurrency=2, group_rate_limits="")
    time.sleep(0.3)
    t0 = int(time.time()) - 1
    base = max_log_id()
    res = burst(3, "RL4")
    time.sleep(1.2)
    rows = logs_after(base, t0)
    served = served_channels(rows)
    fb_rows = [r for r in rows if int(r.get("channel") or 0) == ids["old_fb"]]
    report(case, "3 条并发全部 200（旧行为不拒）", "PASS" if codes(res) == [200, 200, 200] else "FAIL",
           "状态码 %s" % codes(res))
    report(case, "超限那条走渠道自配兜底", "PASS" if fb_rows else "FAIL",
           "自配兜底=%s，实际服务渠道=%s" % (ids["old_fb"], served))
    report(case, "没有别的渠道掺进来", "PASS" if ids["decoy_fb"] not in served else "FAIL",
           "诱饵=%s，实际服务渠道=%s" % (ids["decoy_fb"], served))


def case_RL5(ids):
    """§22-5 拒答降级回归：仍走渠道自配兜底、计费分组不变、429 文案与 code 不变"""
    case = "RL5"
    clear_distill_state()
    if not REDIS_ADDR:
        report(case, "拒答降级回归", "SKIP",
               "需要 RL_REDIS_ADDR 才能写 refusal:demote:{user:N} 造降级态；内存模式无法从外部伪造")
        return
    r = Redis(REDIS_ADDR, REDIS_PASSWORD, REDIS_DB)
    set_user(max_concurrency=-1, group_rate_limits="", refusal_demote_override=1)
    set_main_fallback(ids, ids["old_fb"])
    r.cmd("SET", "refusal:demote:{user:%d}" % USER_ID, "1", "EX", "600")
    time.sleep(0.5)

    t0 = int(time.time()) - 1
    base = max_log_id()
    code, txt = relay("RL5 拒答降级回归 %f" % time.time(), timeout=60)
    time.sleep(1.2)
    rows = logs_after(base, t0)
    fb_rows = [x for x in rows if int(x.get("channel") or 0) == ids["old_fb"]]
    report(case, "降级态请求走渠道自配兜底", "PASS" if fb_rows else "FAIL",
           "HTTP %s，日志渠道=%s，期望 %s" % (code, served_channels(rows), ids["old_fb"]))
    report(case, "计费分组仍是原分组", "PASS" if fb_rows and fb_rows[0].get("group") == "default" else "FAIL",
           "group=%s" % (fb_rows[0].get("group") if fb_rows else "-"))

    # 渠道没配兜底时：429 文案必须是拒答那套，且**不带** rate_limit_exceeded
    set_main_fallback(ids, 0)
    time.sleep(0.5)
    code2, txt2 = relay("RL5 拒答降级无兜底 %f" % time.time(), timeout=60)
    report(case, "无兜底时 429", "PASS" if code2 == 429 else "FAIL", "HTTP %s" % code2)
    report(case, "拒答文案不变（风控降级口径、无 rate_limit_exceeded）",
           "PASS" if ("风控降级" in txt2 and "rate_limit_exceeded" not in txt2) else "FAIL",
           txt2[:160])

    r.cmd("DEL", "refusal:demote:{user:%d}" % USER_ID)
    set_user(refusal_demote_override=0)
    set_main_fallback(ids, ids["old_fb"])


def case_RL6(ids):
    """429 形态并排：速率 429 带 code 且是并发口径，不许混进拒答措辞"""
    case = "RL6"
    clear_distill_state()
    set_main_fallback(ids, 0)
    set_user(max_concurrency=-1, group_rate_limits=json.dumps({"default": {"limit": 1, "fallback": False}}))
    time.sleep(0.3)
    res = burst(2, "RL6")
    time.sleep(0.8)
    rate_body = body_of(res, 429)
    report(case, "速率 429 带 rate_limit_exceeded", "PASS" if "rate_limit_exceeded" in rate_body else "FAIL",
           rate_body[:150])
    report(case, "速率 429 文案是并发超限口径", "PASS" if "并发" in rate_body else "FAIL", rate_body[:120])
    report(case, "速率 429 不混拒答口径（无「风控降级」）",
           "PASS" if "风控降级" not in rate_body else "FAIL", rate_body[:120])


def case_RL7(ids):
    """分组写了条目但 limit<=0 —— 等于没配分组速率，只超总并发时走旧行为（渠道自配兜底）"""
    case = "RL7"
    clear_distill_state()
    set_main_fallback(ids, ids["old_fb"])
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
        served = served_channels(rows)
        report(case, "%s → 不 429" % label, "PASS" if codes(res) == [200, 200, 200] else "FAIL",
               "状态码 %s（429 说明 limit<=0 被当成启用了分组策略）" % codes(res))
        report(case, "%s → 超限走渠道自配兜底" % label, "PASS" if ids["old_fb"] in served else "FAIL",
               "自配兜底=%s，实际服务渠道=%s" % (ids["old_fb"], served))


def case_RL8(ids):
    """蒸馏降级态 + 并发超限 → 直接 429（不跳第二次兜底），且不超限时照常走蒸馏兜底"""
    case = "RL8"
    if not REDIS_ADDR:
        report(case, "降级态并发超限 → 429", "SKIP",
               "需要 RL_REDIS_ADDR 才能写 distill:demote:{user:N} 造假降级态")
        return
    r = Redis(REDIS_ADDR, REDIS_PASSWORD, REDIS_DB)
    demote_key = "distill:demote:{user:%d}" % USER_ID

    # 两条兜底链路指向不同渠道：渠道自配(2) / 蒸馏(5)，看落点就知道走了哪条
    set_main_fallback(ids, ids["old_fb"])
    distill_put({"enabled": True, "observe_only": False, "activate_rpm": 1,
                 "confirm_hits": 1, "confirm_window_seconds": 300, "threshold": 0.6,
                 "demote_seconds": 900, "fallback_channel_id": ids["distill_fb"],
                 "detect_channel_ids": [ids["main"]], "dedup_seconds": 0,
                 "jev_base_url": "http://127.0.0.1:%d/v1/systemone" % JEV_PORT})
    set_user(max_concurrency=1, group_rate_limits="")
    r.cmd("SET", demote_key, "1", "EX", "900")
    time.sleep(0.5)

    # 控制组：降级 + 未超限 → 仍应正常放行并落到蒸馏兜底渠道
    t0 = int(time.time()) - 1
    base = max_log_id()
    code1, _ = relay("RL8 降级未超限 %f" % time.time(), timeout=60)
    time.sleep(1.2)
    rows1 = logs_after(base, t0)
    served1 = served_channels(rows1)
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
    served = served_channels(rows)
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

    set_main_fallback(ids, ids["old_fb"])
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


def case_RL11(ids):
    """§22-7 单跳：兜底渠道自己还配了兜底 → 只跳一跳，绝不跟到第二跳"""
    clear_distill_state()
    case = "RL11"
    patch_channel(ids["old_fb"], {"fallback_channel_id": ids["decoy_fb"]})
    admin("POST", "/api/channel/fix")
    time.sleep(0.4)
    try:
        res, rows = concurrent_case(case, ids, {"default": {"limit": 2, "fallback": True}},
                                    ids["old_fb"], 3, "RL11")
        served = served_channels(rows)
        fb_rows = [r for r in rows if int(r.get("channel") or 0) == ids["old_fb"]]
        report(case, "3 条并发全部 200", "PASS" if codes(res) == [200, 200, 200] else "FAIL",
               "状态码 %s" % codes(res))
        report(case, "超限那条落在第一跳（兜底渠道 %d）" % ids["old_fb"],
               "PASS" if len(fb_rows) == 1 else "FAIL",
               "第一跳命中 %d 条，实际服务渠道=%s" % (len(fb_rows), served))
        report(case, "不会跟到第二跳（渠道 %d 零命中）" % ids["decoy_fb"],
               "PASS" if ids["decoy_fb"] not in served else "FAIL",
               "第二跳=%s，实际服务渠道=%s" % (ids["decoy_fb"], served))
    finally:
        patch_channel(ids["old_fb"], {"fallback_channel_id": 0})
        admin("POST", "/api/channel/fix")


def case_RL12(ids):
    """§22-8 兜底渠道完全没配分组 + 原分组为空：不许重导，且这条 429 分支从 HTTP 不可达

    实测（3102 / dev-5a4105c34）：把用户分组置空后，请求在**分销阶段**就被拒了
    ——`503 model_not_found「分组  下模型 rl-model 无可用渠道（distributor）」`，
    根本走不到兜底解析，所以「拿空分组去计费 → 429」这个守卫在正常 HTTP 链路上
    构造不出来（它只在内部直接调用 redirectToFallbackChannel 时才有意义，由
    middleware 的单测覆盖）。这里保留一条真正重要的断言：绝不能 200 重导到那个
    没配分组的渠道。
    """
    clear_distill_state()
    case = "RL12"
    u = user_get()
    old_group = u.get("group") or "default"
    try:
        set_user(group="", max_concurrency=2, group_rate_limits="")
        set_main_fallback(ids, ids["nogrp_fb"])
        time.sleep(0.4)
        t0 = int(time.time()) - 1
        base = max_log_id()
        res = burst(3, "RL12")
        time.sleep(1.2)
        rows = logs_after(base, t0)
        served = served_channels(rows)
        got = codes(res)
        ok200 = sum(1 for _, c, _ in res if c == 200)
        report(case, "原分组为空时绝不 200 重导到没分组的渠道",
               "PASS" if ok200 == 0 and ids["nogrp_fb"] not in served else "FAIL",
               "状态码 %s，无分组兜底=%s，实际服务渠道=%s" % (got, ids["nogrp_fb"], served))
        report(case, "429 守卫从 HTTP 不可达（分销层先 503「分组下模型无可用渠道」，由单测覆盖）",
               "SKIP", "状态码 %s；期望的 429 只在内部调用时出现" % got)
    finally:
        set_user(group=old_group, max_concurrency=-1, group_rate_limits="")
        set_main_fallback(ids, ids["old_fb"])


def case_RL10(ids):
    """§22-6 旧全局配置写进去也没有路由效果；渠道自配字段仍可读写

    说明：PUT /api/option/ 对任何未知 key 都会返回 200 success 并写进 options
    （updateOptionMap 的 switch 没有 default 分支）—— 这是全局既有行为，不是 §22
    引入的。所以这里不验 HTTP 状态码，验真正的不变量：旧 key 的值不参与路由。
    """
    case = "RL10"
    clear_distill_state()
    # 用旧 key 写一个诱饵值，再构造「分组 fallback:true + 渠道没配兜底」
    admin("PUT", "/api/option/", {"key": "RateLimitFallbackChannelId", "value": str(ids["decoy_fb"])})
    set_main_fallback(ids, 0)
    set_user(max_concurrency=-1, group_rate_limits=json.dumps({"default": {"limit": 2, "fallback": True}}))
    time.sleep(0.3)
    t0 = int(time.time()) - 1
    base = max_log_id()
    res = burst(3, "RL10")
    time.sleep(1.2)
    rows = logs_after(base, t0)
    served = served_channels(rows)
    body = body_of(res, 429)
    report(case, "旧 key 写入后不影响路由：仍 429 +「未配置可用的兜底渠道」",
           "PASS" if (429 in codes(res) and "未配置可用的兜底渠道" in body) else "FAIL",
           "状态码 %s，正文 %s" % (codes(res), body[:140]))
    report(case, "旧 key 指向的诱饵渠道没有被当成兜底",
           "PASS" if ids["decoy_fb"] not in served else "FAIL",
           "诱饵=%s，实际服务渠道=%s" % (ids["decoy_fb"], served))
    ch = find_channel("rl-main")
    report(case, "渠道自配 fallback_channel_id 仍可读写",
           "PASS" if (ch is not None and "fallback_channel_id" in ch) else "FAIL",
           "channel.fallback_channel_id=%s" % (ch.get("fallback_channel_id") if ch else "-"))


CASES = {"RL1": case_RL1, "RL2": case_RL2, "RL3": case_RL3, "RL4": case_RL4,
         "RL5": case_RL5, "RL6": case_RL6,
         "RL7": case_RL7,
         "RL8": case_RL8,
         "RL9": case_RL9,
         "RL10": case_RL10,
         "RL11": case_RL11,
         "RL12": case_RL12}


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

        for nm in ("rl-main", "rl-oldfb", "rl-decoy", "rl-badfb", DISTILL_FB_NAME, "rl-hold",
                   "rl-dead", "rl-gemini", "rl-nogrp"):
            drop_channel(nm)
        ids["main"] = ensure_channel("rl-main", [MODEL], priority=100)
        ids["old_fb"] = ensure_channel("rl-oldfb", [MODEL], priority=0)
        ids["decoy_fb"] = ensure_channel("rl-decoy", [MODEL], priority=0)
        ids["bad_fb"] = ensure_channel("rl-badfb", [MODEL_OTHER], priority=0)
        ids["distill_fb"] = ensure_channel(DISTILL_FB_NAME, [MODEL], priority=0)
        ids["hold"] = ensure_channel("rl-hold", [MODEL_HOLD], priority=0)
        ids["dead_fb"] = ensure_channel("rl-dead", [MODEL], priority=0, status=2)   # 已禁用
        ids["gemini"] = ensure_channel("rl-gemini", [GEMINI_MODEL], priority=100)
        try:
            ids["nogrp_fb"] = ensure_channel("rl-nogrp", [MODEL], priority=0, group="")
        except Exception as e:
            ids["nogrp_fb"] = 0
            print("      !! 建空分组渠道失败（RL12 会 SKIP）: %s" % repr(e)[:120], flush=True)
        patch_channel(ids["main"], {"fallback_channel_id": ids["old_fb"]})
        admin("POST", "/api/channel/fix")
        time.sleep(0.5)
        print("      渠道 main=%d old_fb=%d decoy=%d bad_fb=%d distill_fb=%d 模型=%s" %
              (ids["main"], ids["old_fb"], ids["decoy_fb"], ids["bad_fb"],
               ids["distill_fb"], MODEL), flush=True)
        # 除 RL8 外都别让蒸馏降级掺进来
        try:
            distill_put({"enabled": False, "detect_channel_ids": [], "fallback_channel_id": 0})
        except Exception as e:
            print("      !! 关闭蒸馏失败:", repr(e)[:120], flush=True)

        saved_user = user_get()
        for k in ("MaxConcurrentRequestsPerUserEnabled", "SelfUseModeEnabled"):
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
                for nm in ("rl-main", "rl-oldfb", "rl-decoy", "rl-badfb", DISTILL_FB_NAME, "rl-hold",
                           "rl-dead", "rl-gemini", "rl-nogrp"):
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
