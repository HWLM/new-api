#!/usr/bin/env python3
"""渠道缓存定时同步的 panic 验收（fix/channel-cache-sync-error）

复现原理：`InitChannelCache` 每次定时同步都 `SELECT abilities`，旧代码忽略错误，
查询失败拿到 0 行后 `groups` 为空，随后按 `channel.Group` 往 nil map 里写就 panic。
所以只要让 `SELECT abilities` 真的失败即可——最稳的办法是把表改名（测完改回来）。

⚠️ 为什么不用 SQLite 排他锁：rollback journal 模式下普通 `BEGIN EXCLUSIVE` 实测只拦写，
读照样通过（同步仍打出 `channels synced from database`），根本复现不出来。

⚠️ 定时同步只在 `MEMORY_CACHE_ENABLED=true` 时才启动（main.go），只设 SYNC_FREQUENCY 不生效。

环境变量：
  CC_BASE_URL        被测实例，默认 http://127.0.0.1:3005
  CC_ADMIN_TOKEN     管理员令牌
  CC_RELAY_KEY       普通用户 Key
  CC_MODEL           能用的模型名，默认 stub-model
  CC_SQLITE          该实例的 SQLite 文件路径
  CC_SYNC_SECONDS    该实例的 SYNC_FREQUENCY，默认 5（脚本按它算等几轮）
  CC_WAIT_SECONDS    破坏后要等的秒数，默认 3×SYNC+5

断言：
  C1 基线：relay 可用、渠道集合可读
  C2 `abilities` 缺失时，定时同步不再把进程打挂（进程仍活着）
  C3 期间 relay 仍 200（旧渠道缓存保留，没被 0 行结果覆盖）
  C4 渠道集合与破坏前一致
  C5 恢复表名后一切正常
失败特征：旧构建在破坏后的下一轮同步直接退出（/api/status 连接被拒）。
"""
import argparse
import json
import os
import sqlite3
import sys
import time
import urllib.error
import urllib.request

BASE = os.environ.get("CC_BASE_URL", "http://127.0.0.1:3005").rstrip("/")
ADMIN = os.environ.get("CC_ADMIN_TOKEN", "")
RELAY_KEY = os.environ.get("CC_RELAY_KEY", "")
MODEL = os.environ.get("CC_MODEL", "stub-model")
SQLITE = os.environ.get("CC_SQLITE", "")
SYNC = int(os.environ.get("CC_SYNC_SECONDS", "5"))
WAIT = int(os.environ.get("CC_WAIT_SECONDS", str(SYNC * 3 + 5)))

results = []


def report(name, status, detail=""):
    results.append((name, status, detail))
    print("[%s] %s%s" % (status, name, (" — " + detail) if detail else ""), flush=True)


def http(method, path, body=None, token=None, timeout=20):
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
        return 0, repr(e)


def alive():
    return http("GET", "/api/status", timeout=3)[0] == 200


def relay():
    return http("POST", "/v1/chat/completions", {
        "model": MODEL, "stream": False, "max_tokens": 4,
        "messages": [{"role": "user", "content": "cache probe %f" % time.time()}],
    }, RELAY_KEY, timeout=40)[0]


def channel_ids():
    if not ADMIN:
        return []
    code, txt = http("GET", "/api/channel/?p=1&page_size=100", token=ADMIN)
    if code != 200:
        return None
    try:
        d = json.loads(txt).get("data") or {}
    except Exception:
        return None
    items = d if isinstance(d, list) else (d.get("items") or [])
    return sorted(int(c["id"]) for c in items)


def sql(sql_text, tries=20):
    """对被测库执行一条 DDL；实例随时可能在写，锁住就重试。"""
    for i in range(tries):
        try:
            con = sqlite3.connect(SQLITE, timeout=10, isolation_level=None)
            con.execute(sql_text)
            con.close()
            return True
        except sqlite3.OperationalError:
            if i == tries - 1:
                raise
            time.sleep(0.5)
    return False


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--wait", type=int, default=WAIT)
    a = ap.parse_args()
    if not SQLITE or not RELAY_KEY:
        sys.exit("需要 CC_SQLITE / CC_RELAY_KEY")
    if not ADMIN:
        print("      ⚠️ 没给 CC_ADMIN_TOKEN：跳过「渠道集合」类断言，只看进程存活与 relay",
              flush=True)
    if not alive():
        sys.exit("实例当前不可达，先把服务起来")

    before_ids = channel_ids()
    c0 = relay()
    report("基线：relay 可用", "PASS" if c0 == 200 else "FAIL", "HTTP %s" % c0)
    report("基线：渠道集合可读", "PASS" if before_ids else ("SKIP" if not ADMIN else "FAIL"),
           "渠道 %s" % (before_ids or "（未提供管理员令牌）"))

    try:
        sql("ALTER TABLE abilities RENAME TO abilities_hidden")
    except Exception as e:
        sys.exit("改表名失败（库被占？）: %r" % e)
    print("      已把 abilities 改名为 abilities_hidden，等 %ds（≈%d 轮同步）"
          % (a.wait, a.wait // max(SYNC, 1)), flush=True)

    dead_at = None
    relay_codes = []
    t0 = time.time()
    while time.time() - t0 < a.wait:
        time.sleep(min(SYNC, 3))
        if not alive():
            dead_at = time.time() - t0
            break
        relay_codes.append(relay())

    report("abilities 查询失败时进程未 panic", "PASS" if dead_at is None else "FAIL",
           "存活 %ds" % (int(dead_at) if dead_at is not None else a.wait))

    if dead_at is not None:
        try:
            sql("ALTER TABLE abilities_hidden RENAME TO abilities")
        except Exception:
            pass
        report("旧渠道缓存保留（relay 仍 200）", "FAIL", "进程已退出，无从验证")
        report("渠道集合不变", "FAIL", "进程已退出")
        summarize()
        return

    report("旧渠道缓存保留（relay 仍 200）",
           "PASS" if relay_codes and all(c == 200 for c in relay_codes) else "FAIL",
           "状态码 %s" % relay_codes)

    after_ids = channel_ids()
    report("渠道集合与破坏前一致",
           "PASS" if after_ids == before_ids and before_ids else ("SKIP" if not ADMIN else "FAIL"),
           "前 %s 后 %s" % (before_ids or "-", after_ids or "-"))

    try:
        sql("ALTER TABLE abilities_hidden RENAME TO abilities")
    except Exception as e:
        report("恢复 abilities 表", "FAIL", repr(e))
        summarize()
        return
    time.sleep(SYNC + 2)
    report("恢复表名后进程仍活着", "PASS" if alive() else "FAIL")
    report("恢复表名后 relay 可用", "PASS" if relay() == 200 else "FAIL")
    summarize()


def summarize():
    print("\n== 汇总 ==", flush=True)
    fail = 0
    for n, s, _ in results:
        if s == "FAIL":
            fail += 1
        print("  %-4s %s" % (s, n))
    judged = len([r for r in results if r[1] in ("PASS", "FAIL")])
    print("\n共 %d 条判定，失败 %d 条。" % (judged, fail))
    sys.exit(1 if fail else 0)


if __name__ == "__main__":
    main()
