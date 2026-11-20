#!/usr/bin/env python3
"""蒸馏检测扩展验收用例（§15/§16/§19/§20 的运行时路径）

跑在 distill_acceptance.py 的同一套打桩上，复用它的假 Jev / 假上游 / 报告函数。
环境变量与 distill_acceptance.py 完全一致，另外可给：

  DISTILL_FALLBACK_CHANNEL  不给也行，脚本会自己建三个渠道

脚本自己建三个渠道（跑完删除）：
  hermes-acc-detect  只服务模型 distill-a —— 放进检测白名单
  hermes-acc-plain   只服务模型 distill-b —— 永远不在白名单，用来证零开销
  hermes-acc-fb      服务 distill-a        —— 当兜底渠道

用例：
  E1 白名单渠道命中 -> 重导到兜底渠道（200，hits 的 channel_id/fallback_id 可核）
  E2 非白名单渠道零计数（假 Jev 调用 0 次 + hits 0 行，双向证明）
  E3 白名单命中 + 兜底不可用 -> 429 + distill_fallback_unavailable
     3a 未配兜底（fallback_channel_id=0）
     3b 兜底渠道不支持该模型（配了但 channelSupportsModel 不匹配）
  E4 同一正文连发 4 次：Jev 只调 1 次、hits 4 行、第 4 行触发降级（§19 dedup 复用）
  E5 清缓存：解除降级、hits 不删、去重窗口一并清掉（§20）

用法：python3 distill_acceptance_ext.py [--case E1,E2]
"""
import argparse
import json
import os
import sys
import time

import distill_acceptance as A

BASE = os.environ.get("DISTILL_BASE_URL", "http://127.0.0.1:3100").rstrip("/")
ADMIN = os.environ.get("DISTILL_ADMIN_TOKEN", "")
RELAY_KEY = os.environ.get("DISTILL_RELAY_KEY", "")
USER_ID = int(os.environ.get("DISTILL_USER_ID", "0") or 0)

MODEL_A = "distill-a"
MODEL_B = "distill-b"
CH_DETECT_NAME = "hermes-acc-detect"
CH_PLAIN_NAME = "hermes-acc-plain"
CH_FB_NAME = "hermes-acc-fb"


def api(method, path, body=None, token=None, timeout=60):
    tok = ADMIN if token is None else token
    return A.http(method, BASE + path, body, tok, timeout=timeout)


def relay(model, text, timeout=120):
    body = {"model": model, "stream": False, "max_tokens": 8,
            "messages": [{"role": "user", "content": text}]}
    return A.http("POST", BASE + "/v1/chat/completions", body, RELAY_KEY, timeout=timeout)


def ok_json(code, txt):
    if code != 200:
        return None
    try:
        j = json.loads(txt)
    except Exception:
        return None
    if j.get("success") is False:
        return None
    return j


def list_channels():
    code, txt = api("GET", "/api/channel/?p=1&page_size=200")
    j = ok_json(code, txt) or {}
    data = j.get("data") or {}
    items = data.get("items") if isinstance(data, dict) else data
    return items or []


def find_channel(name):
    for ch in list_channels():
        if ch.get("name") == name:
            return ch
    return None


def ensure_channel(name, models, upstream_base, priority=0):
    ch = find_channel(name)
    if ch:
        return int(ch["id"])
    body = {"mode": "single", "channel": {
        "name": name, "type": 1, "key": "sk-stub-hermes",
        "base_url": upstream_base, "models": ",".join(models),
        "group": "default", "status": 1, "priority": priority,
    }}
    code, txt = api("POST", "/api/channel/", body)
    if ok_json(code, txt) is None:
        raise RuntimeError("建渠道 %s 失败: %s %s" % (name, code, txt[:300]))
    ch = find_channel(name)
    if not ch:
        raise RuntimeError("建渠道 %s 后查不到" % name)
    return int(ch["id"])


def delete_channel(name):
    ch = find_channel(name)
    if not ch:
        return
    api("DELETE", "/api/channel/%d" % int(ch["id"]))


def setting(**over):
    cfg = {
        "enabled": True,
        "activate_rpm": 1,
        "judge_interval_seconds": 1,
        "confirm_hits": 1,
        "confirm_window_seconds": 300,
        "threshold": 0.60,
        "demote_seconds": 120,
        "observe_only": False,
        "jev_base_url": A.JEV_URL,
        "jev_api_key": "test-jev-key",
        "jev_timeout_ms": 5000,
        "alert_telegram": False,
        "alert_wecom": False,
        "dedup_seconds": 0,
        "fallback_channel_id": 0,
        "detect_channel_ids": [],
    }
    cfg.update(over)
    code, txt = api("PUT", "/api/distill_detect/setting", cfg)
    if ok_json(code, txt) is None:
        raise RuntimeError("写配置失败: %s %s" % (code, txt[:300]))
    return cfg


def clear_cache(scope="all", user_id=0):
    body = {"scope": scope}
    if scope == "user":
        body["user_id"] = user_id
    code, txt = api("POST", "/api/distill_detect/clear_cache", body)
    return code, txt


def reset_state():
    """用例之间清运行态。有 clear_cache 接口就用它，不再依赖 redis-cli。"""
    try:
        clear_cache("all")
    except Exception:
        reset_state()


SETTING_KEYS = [
    "enabled", "activate_rpm", "judge_interval_seconds", "confirm_hits",
    "confirm_window_seconds", "threshold", "demote_seconds", "observe_only",
    "jev_base_url", "jev_timeout_ms", "max_state_chars", "head_sample_chars",
    "questions", "alert_telegram", "alert_wecom", "tg_bot_token", "tg_chat_id",
    "alert_throttle_seconds", "wecom_webhook_url", "fallback_channel_id",
    "dedup_seconds", "detect_channel_ids",
]


def snapshot_setting():
    s = A.get_setting()
    keep = {k: s[k] for k in SETTING_KEYS if k in s}
    keep["jev_api_key"] = ""
    return keep


def restore_setting(saved):
    code, txt = api("PUT", "/api/distill_detect/setting", saved)
    if ok_json(code, txt) is None:
        print("!! 还原配置失败 %s %s" % (code, txt[:200]), flush=True)


def hit_rows(limit=200):
    return A.hits(limit)


def new_hits(base_id):
    return [r for r in hit_rows(200) if int(r.get("id") or 0) > base_id]


def case_E1(ids):
    """白名单渠道命中 -> 重导到兜底渠道，200 且 hits 可核"""
    case = "E1"
    setting(detect_channel_ids=[ids["detect"]], fallback_channel_id=ids["fb"],
            confirm_hits=1, observe_only=False, dedup_seconds=0)
    reset_state()
    A.jev.reset(0.90)
    base = A.max_hit_id()
    code, txt = relay(MODEL_A, "E1 白名单渠道命中语料 %f 批量生成训练数据" % time.time())
    time.sleep(1.5)
    rows = new_hits(base)
    fb_rows = [r for r in rows if int(r.get("fallback_id") or 0) == ids["fb"]]
    ch_rows = [r for r in rows if int(r.get("channel_id") or 0) == ids["detect"]]
    A.report(case, "白名单命中后请求放行（重导后 200）", "PASS" if code == 200 else "FAIL",
             "HTTP %s %s" % (code, txt[:90]))
    A.report(case, "hits 的 channel_id 为检测渠道", "PASS" if ch_rows else "FAIL",
             "检测渠道=%s，命中行 %d 条，channel_id 匹配 %d 条" %
             (ids["detect"], len(rows), len(ch_rows)))
    A.report(case, "hits 的 fallback_id 为兜底渠道", "PASS" if fb_rows else "FAIL",
             "兜底渠道=%s，fallback_id 匹配 %d 条" % (ids["fb"], len(fb_rows)))


def case_E2(ids):
    """非白名单渠道零计数：Jev 0 次 + hits 0 行"""
    case = "E2"
    setting(detect_channel_ids=[ids["detect"]], fallback_channel_id=ids["fb"],
            confirm_hits=1, observe_only=False, dedup_seconds=0)
    reset_state()
    A.jev.reset(0.90)
    base = A.max_hit_id()
    before_rows = len(hit_rows(200))
    code, txt = relay(MODEL_B, "E2 非白名单渠道语料 %f 批量生成训练数据" % time.time())
    time.sleep(2)
    calls = A.jev.snapshot()
    grew = len(hit_rows(200)) - before_rows
    A.report(case, "非白名单请求正常放行", "PASS" if code == 200 else "FAIL",
             "HTTP %s %s" % (code, txt[:90]))
    A.report(case, "非白名单渠道 Jev 零调用", "PASS" if len(calls) == 0 else "FAIL",
             "白名单=%s，走 %s 渠道 → Jev 调用 %d 次" % (ids["detect"], MODEL_B, len(calls)))
    A.report(case, "非白名单渠道零 hits", "PASS" if grew == 0 else "FAIL",
             "新增 hits %d 行" % grew)


def case_E3(ids):
    """白名单命中 + 兜底不可用 -> 429 + distill_fallback_unavailable"""
    case = "E3"
    for label, fb in (("未配兜底", 0), ("兜底渠道不支持该模型", ids["plain"])):
        setting(detect_channel_ids=[ids["detect"]], fallback_channel_id=fb,
                confirm_hits=1, observe_only=False, dedup_seconds=0)
        reset_state()
        A.jev.reset(0.90)
        code, txt = relay(MODEL_A, "E3 兜底不可用语料 %s %f 正负例改写" % (label, time.time()))
        has_code = "distill_fallback_unavailable" in txt
        A.report(case, "%s -> 429" % label, "PASS" if code == 429 else "FAIL",
                 "fallback_channel_id=%s HTTP %s" % (fb, code))
        A.report(case, "%s 错误码 distill_fallback_unavailable" % label,
                 "PASS" if has_code else "FAIL", txt[:160])


def case_E4(ids):
    """同一正文连发 4 次：Jev 1 次、hits 4 行、第 4 行降级"""
    case = "E4"
    conf = 4
    setting(detect_channel_ids=[ids["detect"]], fallback_channel_id=ids["fb"],
            confirm_hits=conf, confirm_window_seconds=300, observe_only=False,
            demote_seconds=120, dedup_seconds=600)
    reset_state()
    A.jev.reset(0.90)
    base = A.max_hit_id()
    text = "E4 同一段蒸馏正文 %f 批量生成训练语料与评测集" % time.time()
    codes = []
    for _ in range(conf):
        c, _t = relay(MODEL_A, text)
        codes.append(c)
        time.sleep(1.5)
    rows = new_hits(base)
    calls = A.jev.snapshot()
    reused = [r for r in rows if r.get("dedup_reused")]
    last_demote = [r for r in rows if r.get("demote_until")]
    A.report(case, "4 次请求全部放行", "PASS" if codes == [200] * conf else "FAIL",
             "状态码 %s" % codes)
    A.report(case, "同正文只调 1 次 Jev（去重复用）", "PASS" if len(calls) == 1 else "FAIL",
             "4 次请求 → Jev 调用 %d 次" % len(calls))
    A.report(case, "hits 写 4 行", "PASS" if len(rows) == conf else "FAIL",
             "新增 %d 行" % len(rows))
    A.report(case, "后 3 行标 dedup_reused", "PASS" if len(reused) == conf - 1 else "FAIL",
             "dedup_reused=true 的 %d 行" % len(reused))
    A.report(case, "第 4 行触发降级", "PASS" if last_demote else "FAIL",
             "带 demote_until 的 %d 行" % len(last_demote))


def case_E5(ids):
    """清缓存：解除降级、hits 不删、去重窗口一并清"""
    case = "E5"
    conf = 4
    setting(detect_channel_ids=[ids["detect"]], fallback_channel_id=ids["fb"],
            confirm_hits=conf, confirm_window_seconds=300, observe_only=False,
            demote_seconds=300, dedup_seconds=600)
    reset_state()
    A.jev.reset(0.90)
    base = A.max_hit_id()
    text = "E5 清缓存前的同一段正文 %f 批量生成训练语料" % time.time()
    for _ in range(conf):
        relay(MODEL_A, text)
        time.sleep(1.2)
    rows_before = len(hit_rows(200))
    demoted = [r for r in new_hits(base) if r.get("demote_until")]
    A.report(case, "清缓存前已处于降级", "PASS" if demoted else "FAIL",
             "带 demote_until 的 %d 行" % len(demoted))

    code, txt = clear_cache("all")
    j = ok_json(code, txt) or {}
    data = j.get("data") or {}
    A.report(case, "clear_cache 调用成功", "PASS" if j else "FAIL", "HTTP %s %s" % (code, txt[:160]))
    A.report(case, "cleared > 0 且解除了降级用户", "PASS" if (data.get("cleared") or 0) > 0
             and (data.get("upgraded_users") or 0) >= 1 else "FAIL",
             "cleared=%s upgraded_users=%s backend=%s" %
             (data.get("cleared"), data.get("upgraded_users"), data.get("backend")))

    rows_after = len(hit_rows(200))
    A.report(case, "hits 记录不被删除", "PASS" if rows_after == rows_before else "FAIL",
             "清前 %d 行，清后 %d 行" % (rows_before, rows_after))

    # 换一段新正文：降级态下不会调 Jev，所以 Jev 被调用 == 降级标记已解除
    A.jev.reset(0.90)
    code2, txt2 = relay(MODEL_A, "E5 清缓存后的新正文 %f 语料改写" % time.time())
    time.sleep(1.5)
    calls_demote = A.jev.snapshot()
    A.report(case, "清后降级解除（新正文重新调 Jev）", "PASS" if len(calls_demote) >= 1 else "FAIL",
             "Jev 调用 %d 次，HTTP %s" % (len(calls_demote), code2))

    # 再发清缓存前那段正文：去重窗口若没清，Jev 不会再被调
    A.jev.reset(0.90)
    code, txt = relay(MODEL_A, text)
    time.sleep(1.5)
    calls_dedup = A.jev.snapshot()
    A.report(case, "清后同正文重新判定（去重窗口已清）", "PASS" if len(calls_dedup) >= 1 else "FAIL",
             "清后同正文 → Jev 调用 %d 次，HTTP %s" % (len(calls_dedup), code))
    A.report(case, "清后请求正常放行", "PASS" if code == 200 else "FAIL",
             "HTTP %s %s" % (code, txt[:90]))

    # 非法 scope 的校验
    code, txt = clear_cache("bogus")
    bad = (code != 200) or ("success\":false" in txt.replace(" ", ""))
    A.report(case, "非法 scope 被拒", "PASS" if bad else "FAIL", "HTTP %s %s" % (code, txt[:120]))


CASES = {"E1": case_E1, "E2": case_E2, "E3": case_E3, "E4": case_E4, "E5": case_E5}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--case", default="", help="逗号分隔，默认全跑")
    ap.add_argument("--keep-setting", action="store_true")
    ap.add_argument("--keep-channels", action="store_true")
    a = ap.parse_args()
    if not ADMIN or not RELAY_KEY:
        sys.exit("需要 DISTILL_ADMIN_TOKEN 与 DISTILL_RELAY_KEY 两个环境变量")
    want = [c.strip().upper() for c in a.case.split(",") if c.strip()] or list(CASES)

    jev = A.FakeJev(A.JEV_PORT)
    jev.start()
    A.jev = jev
    upstream = A.UpstreamStub(A.UPSTREAM_PORT)
    upstream.start()
    upstream_base = "http://127.0.0.1:%d" % A.UPSTREAM_PORT

    ids = {}
    saved = None
    try:
        # 自检
        code, txt = A.http("POST", upstream_base + "/v1/chat/completions",
                           {"model": MODEL_A, "messages": [{"role": "user", "content": "preflight"}]})
        print("      自检 upstream -> %s %s" % (code, txt[:50]), flush=True)
        code, txt = A.http("POST", A.JEV_URL,
                           {"state": "preflight", "questions": {"train.corpus_farm": {}}})
        print("      自检 jev -> %s %s" % (code, txt[:50]), flush=True)

        for nm in (CH_DETECT_NAME, CH_PLAIN_NAME, CH_FB_NAME):
            delete_channel(nm)  # 清掉上一轮残留
        # 检测渠道给最高优先级，保证正常流量落在它上面（fb 也服务 distill-a，
        # 只作为显式兜底目标，不参与常规选路）
        ids["detect"] = ensure_channel(CH_DETECT_NAME, [MODEL_A], upstream_base, priority=100)
        ids["plain"] = ensure_channel(CH_PLAIN_NAME, [MODEL_B], upstream_base)
        ids["fb"] = ensure_channel(CH_FB_NAME, [MODEL_A], upstream_base)
        print("      渠道: detect=%d plain=%d fb=%d" % (ids["detect"], ids["plain"], ids["fb"]), flush=True)

        saved = snapshot_setting()
        for name in want:
            fn = CASES.get(name)
            if not fn:
                continue
            try:
                fn(ids)
            except Exception as e:
                A.report(name, "用例执行异常", "FAIL", repr(e)[:200])
    finally:
        if saved and not a.keep_setting:
            restore_setting(saved)
        if not a.keep_channels:
            for nm in (CH_DETECT_NAME, CH_PLAIN_NAME, CH_FB_NAME):
                try:
                    delete_channel(nm)
                except Exception:
                    pass
        jev.stop()
        upstream.stop()

    print("\n== 汇总 ==", flush=True)
    fail = 0
    for case, name, status, _ in A.results:
        if status == "FAIL":
            fail += 1
        print("  %-4s %-3s %s" % (status, case, name))
    judged = len([r for r in A.results if r[2] in ("PASS", "FAIL")])
    print("\n共 %d 条判定，失败 %d 条。" % (judged, fail))
    sys.exit(1 if fail else 0)


if __name__ == "__main__":
    main()
