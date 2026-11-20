#!/usr/bin/env python3
"""蒸馏检测验收用例（黑盒，打桩 Jev）

对应 docs/distill-detection-plan.md §3/§4/§5/§8/§10。
环境一好直接跑：python3 distill_acceptance.py [--case T1,T2,...]

需要提前准备好的东西（都通过环境变量传）：
  DISTILL_BASE_URL      被测实例地址，例如 http://127.0.0.1:3100
  DISTILL_ADMIN_TOKEN   管理员令牌（调 /api/distill_detect/*）
  DISTILL_RELAY_KEY     普通用户的 API Key（sk-...，用来打 relay 产生流量）
  DISTILL_MODEL         该 Key 能用的模型，例如 claude-sonnet-4-5
  DISTILL_USER_ID       该 Key 所属用户 id（写进 user 级三态用例）
  DISTILL_JEV_PORT      本地假 Jev 监听端口，默认 18099

用例只通过 HTTP 断言，不直连数据库；降级状态用 GET /api/distill_detect/hits 的
demote_until 字段判断，告警节流用本地假企微 webhook 计数。
"""
import argparse
import json
import os
import sys
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer as HTTPServer

BASE = os.environ.get("DISTILL_BASE_URL", "http://127.0.0.1:3100").rstrip("/")
ADMIN = os.environ.get("DISTILL_ADMIN_TOKEN", "")
RELAY_KEY = os.environ.get("DISTILL_RELAY_KEY", "")
MODEL = os.environ.get("DISTILL_MODEL", "claude-sonnet-4-5")
USER_ID = os.environ.get("DISTILL_USER_ID", "0")
JEV_PORT = int(os.environ.get("DISTILL_JEV_PORT", "18099"))
# 假 Jev 跑在本机时用 127.0.0.1；跑在测试服宿主上时，把 bind 设 0.0.0.0，
# 并把 URL 设成实例容器能访问到的地址（例如 http://184.107.149.15:18099/v1/systemone）
STUB_BIND = os.environ.get("DISTILL_STUB_BIND", "127.0.0.1")
JEV_URL = os.environ.get("DISTILL_JEV_URL", "http://127.0.0.1:%d/v1/systemone" % JEV_PORT)
WECOM_URL = os.environ.get("DISTILL_WECOM_URL", "http://127.0.0.1:%d/hook" % (JEV_PORT + 1))
# 假上游（OpenAI 兼容），让 relay 能真的返回 200；渠道 base_url 指到这里
UPSTREAM_PORT = int(os.environ.get("DISTILL_UPSTREAM_PORT", str(JEV_PORT + 2)))

results = []


def reset_state():
    """清掉上一轮留下的 Redis 状态（降级标记、确认计数、rpm），否则用例之间互相污染。
    只有设了 DISTILL_RESET_CMD 才执行，命令由环境提供（例如 redis-cli 批量 del）。"""
    cmd = os.environ.get("DISTILL_RESET_CMD", "")
    if not cmd:
        return
    import subprocess
    subprocess.run(cmd, shell=True, capture_output=True, timeout=60)


def report(case, name, status, detail=""):
    results.append((case, name, status, detail))
    line = "[%s] %s %s" % (status, case, name)
    if detail:
        line += " — " + detail
    print(line, flush=True)


def http(method, url, body=None, token=None, timeout=30, headers=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("content-type", "application/json")
    if token:
        req.add_header("authorization", "Bearer " + token)
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")
    except Exception as e:
        return 0, "transport error: %s" % e


def admin(method, path, body=None):
    return http(method, BASE + "/api/distill_detect" + path, body, ADMIN)


def get_setting():
    code, txt = admin("GET", "/setting")
    if code != 200:
        raise RuntimeError("GET setting -> %s %s" % (code, txt[:200]))
    return json.loads(txt)["data"]


def put_setting(patch):
    code, txt = admin("PUT", "/setting", patch)
    return code, txt


def hits(limit=50):
    code, txt = admin("GET", "/hits?p=1&page_size=%d" % limit)
    if code != 200:
        raise RuntimeError("GET hits -> %s %s" % (code, txt[:200]))
    j = json.loads(txt)
    data = j.get("data") or {}
    return data.get("items") or data.get("records") or []


def redis_ttl():
    cmd = os.environ.get("DISTILL_REDIS_TTL_CMD", "")
    if not cmd:
        return None
    import subprocess
    p = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=30)
    out = (p.stdout or "").strip().splitlines()
    try:
        return int(out[-1])
    except Exception:
        return None


def max_hit_id():
    rows = hits(200)
    return max([int(r["id"]) for r in rows if r.get("id") is not None] or [0])


def relay(payload_text, stream=False):
    """打一条文本类 relay 请求，产生判定机会。"""
    body = {
        "model": MODEL,
        "stream": stream,
        "max_tokens": 8,
        "messages": [{"role": "user", "content": payload_text}],
    }
    return http("POST", BASE + "/v1/chat/completions", body, RELAY_KEY, timeout=120)


class FakeJev:
    """假 Jev：/v1/systemone 返回固定四题分数，并记录每次调用的时间与正文长度。"""

    def __init__(self, port):
        self.port = port
        self.calls = []
        self.score = 0.10
        self.mode = "ok"          # ok | error(500) | timeout(超时)
        self.lock = threading.Lock()
        outer = self

        class H(BaseHTTPRequestHandler):
            def do_POST(self):
                n = int(self.headers.get("content-length") or 0)
                raw = self.rfile.read(n)
                try:
                    body = json.loads(raw or b"{}")
                except Exception:
                    body = {}
                state = body.get("state") or ""
                qs = list((body.get("questions") or {}).keys())
                with outer.lock:
                    outer.calls.append({"t": time.time(), "chars": len(state), "questions": qs})
                    score = outer.score
                    mode = outer.mode
                if mode == "error":
                    payload = json.dumps({"error": {"message": "stub 500"}}).encode()
                    self.send_response(500)
                    self.send_header("content-type", "application/json")
                    self.send_header("content-length", str(len(payload)))
                    self.end_headers()
                    self.wfile.write(payload)
                    return
                if mode == "timeout":
                    time.sleep(6)
                answers = {q: {"noul": score} for q in qs} or {"train.corpus_farm": {"noul": score}}
                payload = json.dumps({"answers": answers, "model": "fake-jev"}).encode()
                self.send_response(200)
                self.send_header("content-type", "application/json")
                self.send_header("content-length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, *a):
                pass

        self.server = HTTPServer((STUB_BIND, port), H)

    def start(self):
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def stop(self):
        try:
            self.server.shutdown()
        except Exception:
            pass

    def reset(self, score, mode="ok"):
        with self.lock:
            self.calls = []
            self.score = score
            self.mode = mode

    def snapshot(self):
        with self.lock:
            return list(self.calls)


def snapshot_setting():
    s = get_setting()
    keep = {k: s[k] for k in (
        "enabled", "activate_rpm", "judge_interval_seconds", "confirm_hits",
        "confirm_window_seconds", "threshold", "demote_seconds", "observe_only",
        "jev_base_url", "alert_telegram", "alert_wecom", "alert_throttle_seconds",
        "wecom_webhook_url", "fallback_channel_id", "dedup_seconds")}
    keep["jev_api_key"] = ""
    return keep


def restore_setting(saved):
    code, txt = put_setting(saved)
    if code != 200:
        print("!! 还原配置失败 %s %s" % (code, txt[:200]), flush=True)


def base_case_settings(jev_url, **over):
    cfg = {
        "enabled": True,
        "activate_rpm": 1,
        "judge_interval_seconds": 1,
        "confirm_hits": 4,
        "confirm_window_seconds": 300,
        "threshold": 0.60,
        "demote_seconds": 60,
        "observe_only": True,
        "jev_base_url": jev_url,
        "jev_api_key": "test-jev-key",
        "jev_timeout_ms": 5000,
        "alert_telegram": False,
        "alert_wecom": False,
        "fallback_channel_id": 0,
        "dedup_seconds": 0,
    }
    cfg.update(over)
    code, txt = put_setting(cfg)
    ok = code == 200 and '"success":false' not in txt.replace(" ", "")
    if not ok:
        raise RuntimeError("写入用例配置失败: %s %s" % (code, txt[:300]))
    return cfg


def case_T1():
    """配置读写 + 越界校验"""
    case = "T1"
    s = get_setting()
    need = ["enabled", "activate_rpm", "judge_interval_seconds", "confirm_hits",
            "confirm_window_seconds", "threshold", "observe_only", "fallback_channel_id",
            "alert_throttle_seconds", "jev_base_url", "jev_api_key_set"]
    missing = [k for k in need if k not in s]
    report(case, "GET /setting 字段齐全", "PASS" if not missing else "FAIL",
           "缺 " + ",".join(missing) if missing else "11 个关键字段都在")
    orig = s["activate_rpm"]
    code, _ = put_setting({"activate_rpm": 7})
    back = get_setting()["activate_rpm"] if code == 200 else None
    report(case, "PUT 后回读一致", "PASS" if back == 7 else "FAIL",
           "PUT=%s 回读=%s" % (code, back))
    code, txt = put_setting({"activate_rpm": -1})
    # new-api 的约定是 HTTP 200 + success:false，不能用状态码判断
    rejected = (code != 200) or ("success" in txt and '"success":false' in txt.replace(" ", ""))
    report(case, "越界值被拒", "PASS" if rejected else "FAIL",
           "HTTP %s %s" % (code, txt[:120]))
    put_setting({"activate_rpm": orig})


def case_T2(jev):
    """判定间隔：30 秒桶内每分钟最多 2 次"""
    case = "T2"
    base_case_settings(JEV_URL,
                       judge_interval_seconds=30, observe_only=True, confirm_hits=4)
    jev.reset(0.10)
    t0 = time.time()
    sent = 0
    for i in range(8):
        relay("T2 采样文本 %d %f" % (i, time.time()))
        sent += 1
        time.sleep(2)
    burst = jev.snapshot()
    elapsed = time.time() - t0
    report(case, "20 秒内最多判 2 次", "PASS" if len(burst) <= 2 and len(burst) >= 1 else "FAIL",
           "发 %d 条请求/%.0fs → Jev 调用 %d 次" % (sent, elapsed, len(burst)))
    report(case, "送模正文头部截断", "PASS" if all(c["chars"] <= 4000 for c in burst) else "FAIL",
           "最大 %d 字符" % max([c["chars"] for c in burst] or [0]))
    time.sleep(31)
    jev.reset(0.10)
    relay("T2 桶过期后文本 %f" % time.time())
    time.sleep(2)
    after = jev.snapshot()
    report(case, "30 秒桶过期后再判", "PASS" if len(after) >= 1 else "FAIL",
           "过期后新增 %d 次调用" % len(after))


def case_T3(jev):
    """前 N-1 次命中只写 hits，第 N 次才降级"""
    case = "T3"
    conf = 4
    base_case_settings(JEV_URL,
                       judge_interval_seconds=1, confirm_hits=conf,
                       confirm_window_seconds=300, observe_only=False,
                       fallback_channel_id=int(os.environ.get("DISTILL_FALLBACK_CHANNEL", "1")),
                       dedup_seconds=0)
    jev.reset(0.90)
    base_id = max_hit_id()
    marks = []
    for i in range(conf):
        relay("T3 语料样本 %d %f 这是用于训练语料生产的请求" % (i, time.time()))
        time.sleep(2)
        fresh = [r for r in hits(200) if int(r["id"]) > base_id]
        marks.append((len(fresh), len([r for r in fresh if r.get("demote_until")])))
    grew = marks[-1][0]
    report(case, "命中写入 hits 行", "PASS" if grew >= conf else "FAIL",
           "新增 %d 行（期望 ≥%d）" % (grew, conf))
    pre = all(m[1] == 0 for m in marks[:conf - 1])
    post = marks[-1][1] >= 1
    report(case, "前 %d 次不降级" % (conf - 1), "PASS" if pre else "FAIL",
           "第 1-%d 次 demote 行数 %s" % (conf - 1, [m[1] for m in marks[:conf - 1]]))
    report(case, "第 %d 次降级" % conf, "PASS" if post else "FAIL",
           "第 %d 次后 demote 行数 %d" % (conf, marks[-1][1]))


class WebhookStub:
    """假企微 webhook，只数收到几条。"""

    def __init__(self, port):
        self.port = port
        self.hits = []
        outer = self

        class H(BaseHTTPRequestHandler):
            def do_POST(self):
                n = int(self.headers.get("content-length") or 0)
                raw = self.rfile.read(n)
                with threading.Lock():
                    outer.hits.append(raw[:400].decode("utf-8", "replace"))
                self.send_response(200)
                self.send_header("content-type", "application/json")
                self.end_headers()
                self.wfile.write(b'{"errcode":0,"errmsg":"ok"}')

            def log_message(self, *a):
                pass

        self.server = HTTPServer((STUB_BIND, port), H)

    def start(self):
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def stop(self):
        try:
            self.server.shutdown()
        except Exception:
            pass


class UpstreamStub:
    """假上游：合法 OpenAI chat.completion，让 relay 能返回 200。"""

    def __init__(self, port):
        self.port = port
        self.calls = 0
        outer = self

        class H(BaseHTTPRequestHandler):
            def do_POST(self):
                n = int(self.headers.get("content-length") or 0)
                raw = self.rfile.read(n)
                try:
                    body = json.loads(raw or b"{}")
                except Exception:
                    body = {}
                outer.calls += 1
                payload = json.dumps({
                    "id": "chatcmpl-stub",
                    "object": "chat.completion",
                    "created": int(time.time()),
                    "model": body.get("model") or "stub-model",
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

        self.server = HTTPServer((STUB_BIND, port), H)

    def start(self):
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def stop(self):
        try:
            self.server.shutdown()
        except Exception:
            pass


def case_T4(jev):
    """observe_only=true 时绝不重导"""
    case = "T4"
    base_case_settings(JEV_URL,
                       judge_interval_seconds=1, confirm_hits=1, observe_only=True, dedup_seconds=0)
    jev.reset(0.90)
    base_id = max_hit_id()
    code, txt = relay("T4 观察模式语料样本 %f 批量生成训练语料" % time.time())
    time.sleep(1.5)
    rows = [r for r in hits(50) if int(r["id"]) > base_id]
    redirected = [r for r in rows if (r.get("fallback_id") or 0) != 0]
    report(case, "观察模式下不重导", "PASS" if not redirected else "FAIL",
           "responses=%s hits 中 fallback_id!=0 的 %d 条" % (code, len(redirected)))
    report(case, "观察模式下请求正常放行", "PASS" if code == 200 else "FAIL", "HTTP %s %s" % (code, txt[:80]))


def case_T5(jev):
    """observe_only=false 且未配兜底：429 + distill_fallback_unavailable"""
    case = "T5"
    base_case_settings(JEV_URL,
                       judge_interval_seconds=1, confirm_hits=1, observe_only=False,
                       fallback_channel_id=0, dedup_seconds=0)
    jev.reset(0.90)
    code, txt = relay("T5 蒸馏兜底缺失语料样本 %f 正负例改写" % time.time())
    ok_code = False
    try:
        j = json.loads(txt)
        marker = json.dumps(j)
        ok_code = "distill_fallback_unavailable" in marker
    except Exception:
        ok_code = "distill_fallback_unavailable" in txt
    report(case, "命中且无兜底 -> 429", "PASS" if code == 429 else "FAIL", "HTTP %s" % code)
    report(case, "错误码 distill_fallback_unavailable", "PASS" if ok_code else "FAIL", txt[:200])


def case_T6(jev, hook):
    """告警按用户 600 秒节流"""
    case = "T6"
    hook.hits.clear()
    base_case_settings(JEV_URL,
                       judge_interval_seconds=1, confirm_hits=1, observe_only=False,
                       fallback_channel_id=int(os.environ.get("DISTILL_FALLBACK_CHANNEL", "1")),
                       demote_seconds=60, alert_wecom=True,
                       wecom_webhook_url=os.environ.get(
                           "DISTILL_WECOM_URL",
                           "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=hermes-acceptance"),
                       alert_throttle_seconds=600, dedup_seconds=0)
    jev.reset(0.90)
    relay("T6 节流语料样本 A %f" % time.time())
    time.sleep(3)
    first = len(hook.hits)
    print("      第 1 次降级后 webhook=%d，等降级过期 62s 再命中一次" % first, flush=True)
    time.sleep(62)
    relay("T6 节流语料样本 B %f" % time.time())
    time.sleep(3)
    ttl = redis_ttl()
    report(case, "降级后写入 600 秒告警节流键", "PASS" if ttl and 0 < ttl <= 620 else "FAIL",
           "distill:alert TTL=%s 秒" % ttl)
    report(case, "同窗口第二次降级不再告警", "SKIP",
           "后端要求企微 webhook 必须是 qyapi.weixin.qq.com，本地 stub 收不到；"
           "需要 DNS/TLS 劫持或抓包才能实测，节流键 TTL 已证明窗口生效")


def set_user_override(value):
    code, txt = http("GET", BASE + "/api/user/%s" % USER_ID, None, ADMIN)
    if code != 200:
        return code, txt
    user = json.loads(txt)["data"]
    user["distill_detect_override"] = value
    return http("PUT", BASE + "/api/user/", user, ADMIN)


def case_T7(jev):
    """用户三态关闭后不再判定"""
    case = "T7"
    base_case_settings(JEV_URL,
                       judge_interval_seconds=1, confirm_hits=4, observe_only=True, dedup_seconds=0)
    code, txt = set_user_override(-1)
    if code != 200:
        report(case, "用户三态关闭后不再判定", "SKIP",
               "设置 distill_detect_override=-1 失败（HTTP %s），需手工在用户编辑里点「关闭」后重跑" % code)
        return
    jev.reset(0.90)
    for i in range(3):
        relay("T7 关闭后样本 %d %f" % (i, time.time()))
        time.sleep(1.5)
    calls = jev.snapshot()
    report(case, "关闭后零判定", "PASS" if not calls else "FAIL", "Jev 调用 %d 次" % len(calls))
    set_user_override(0)


def case_T8(jev):
    """判定失败一律按未命中放行，且不写 dedup 键（规格 §8 硬要求）"""
    case = "T8"
    base_case_settings(JEV_URL,
                       judge_interval_seconds=1, confirm_hits=1, observe_only=False,
                       fallback_channel_id=0, dedup_seconds=600, jev_timeout_ms=2000)
    text = "T8 判定失败样本 %f 语料生产" % time.time()
    for mode, label in (("error", "Jev 500"), ("timeout", "Jev 超时")):
        jev.reset(0.90, mode)
        before = len(hits(200))
        c1, t1 = relay(text)
        time.sleep(2)
        c2, t2 = relay(text)
        time.sleep(2)
        calls = jev.snapshot()
        grew = len(hits(200)) - before
        report(case, label + " 时请求放行（不 429）", "PASS" if (c1 == 200 and c2 == 200) else "FAIL",
               "HTTP %s/%s %s" % (c1, c2, t1[:80]))
        report(case, label + " 时不写 hits", "PASS" if grew == 0 else "FAIL", "新增 hits %d 行" % grew)
        report(case, label + " 时不写 dedup 键", "PASS" if len(calls) >= 2 else "FAIL",
               "同正文连发 2 次 → Jev 被调用 %d 次（=1 说明 dedup 被污染）" % len(calls))
    jev.reset(0.10)


CASES = {"T1": None, "T2": None, "T3": None, "T4": None, "T5": None, "T6": None, "T7": None, "T8": None}


def main():
    global BASE, ADMIN, RELAY_KEY, USER_ID
    ap = argparse.ArgumentParser()
    ap.add_argument("--case", default="", help="逗号分隔，默认全跑")
    ap.add_argument("--keep-setting", action="store_true", help="跑完不还原配置")
    ap.add_argument("--jev-error", action="store_true", help="只跑判定失败用例（T8）")
    a = ap.parse_args()
    if not ADMIN or not RELAY_KEY:
        sys.exit("需要 DISTILL_ADMIN_TOKEN 与 DISTILL_RELAY_KEY 两个环境变量")
    BASE = os.environ.get("DISTILL_BASE_URL", BASE).rstrip("/")
    want = [c.strip().upper() for c in a.case.split(",") if c.strip()] or list(CASES)
    if a.jev_error:
        want = ["T8"]

    jev = FakeJev(JEV_PORT)
    jev.start()
    hook = WebhookStub(JEV_PORT + 1)
    hook.start()
    upstream = None
    if os.environ.get("DISTILL_UPSTREAM_EXTERNAL") == "1":
        print("      上游 stub 由外部进程提供（DISTILL_UPSTREAM_EXTERNAL=1）", flush=True)
    else:
        upstream = UpstreamStub(UPSTREAM_PORT)
        upstream.start()
    saved = None
    try:
        for name, url, body in (("upstream", "http://127.0.0.1:%d/v1/chat/completions" % UPSTREAM_PORT, {}),
                                ("jev", JEV_URL, {"state": "preflight", "questions": {"train.corpus_farm": {}}})):
            code, txt = http("POST", url, body, None, timeout=10)
            print("      自检 %s -> %s %s" % (name, code, txt[:60]), flush=True)
        saved = snapshot_setting()
        if "T1" in want:
            case_T1()
        if "T2" in want:
            reset_state()
            case_T2(jev)
        if "T3" in want:
            reset_state()
            case_T3(jev)
        if "T4" in want:
            reset_state()
            case_T4(jev)
        if "T5" in want:
            reset_state()
            case_T5(jev)
        if "T6" in want:
            reset_state()
            case_T6(jev, hook)
        if "T7" in want:
            reset_state()
            case_T7(jev)
        if "T8" in want:
            reset_state()
            case_T8(jev)
    finally:
        if saved and not a.keep_setting:
            restore_setting(saved)
        jev.stop()
        hook.stop()
        if upstream is not None:
            upstream.stop()

    print("\n== 汇总 ==", flush=True)
    fail = 0
    for case, name, status, _ in results:
        if status == "FAIL":
            fail += 1
        print("  %-4s %-4s %s" % (status, case, name))
    judged = len([r for r in results if r[2] in ("PASS", "FAIL")])
    print("\n共 %d 条判定，失败 %d 条。" % (judged, fail))
    sys.exit(1 if fail else 0)


if __name__ == "__main__":
    main()
