#!/usr/bin/env python3
"""为 3101 隔离实例做一次性初始化：setup root -> 登录拿管理员令牌 -> 建 relay token。
凭据写到 env.json（0600 语义），不打印到控制台。"""
import json, os, secrets, sys, urllib.request, urllib.error

BASE = os.environ.get("BASE", "http://127.0.0.1:3101").rstrip("/")
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "env3101.json")


def http(method, path, body=None, token=None, timeout=30):
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


def main():
    env = {}
    if os.path.exists(OUT):
        env = json.load(open(OUT))

    code, txt = http("GET", "/api/setup")
    print("GET /api/setup ->", code, txt[:120])
    st = json.loads(txt).get("data") or {}
    if not st.get("status"):
        pw = env.get("admin_password") or ("Hermes-" + secrets.token_urlsafe(12))
        code, txt = http("POST", "/api/setup", {"username": "root", "password": pw,
                                                "confirmPassword": pw, "SelfUseModeEnabled": False})
        print("POST /api/setup ->", code, txt[:160])
        env["admin_password"] = pw
    else:
        print("已初始化，跳过 setup")

    pw = env.get("admin_password")
    if not pw:
        sys.exit("已有实例但本地没有密码记录，无法继续")

    code, txt = http("POST", "/api/user/login", {"username": "root", "password": pw})
    print("login ->", code, txt[:120])
    j = json.loads(txt)
    tok = (j.get("data") or {}).get("access_token") or j.get("access_token")
    if not tok:
        sys.exit("登录失败，拿不到 access_token")
    env["admin_token"] = tok

    # 建一条不限额的 relay token
    code, txt = http("GET", "/api/token/?p=1&size=50", None, tok)
    items = ((json.loads(txt).get("data") or {}).get("items")) or []
    key = None
    for it in items:
        if it.get("name") == "hermes-acceptance":
            key = "sk-" + it["key"] if not str(it.get("key", "")).startswith("sk-") else it["key"]
    if not key:
        code, txt = http("POST", "/api/token/", {
            "name": "hermes-acceptance", "remain_quota": 0, "unlimited_quota": True,
            "expired_time": -1, "model_limits_enabled": False, "group": "",
        }, tok)
        print("create token ->", code, txt[:160])
        code, txt = http("GET", "/api/token/?p=1&size=50", None, tok)
        items = ((json.loads(txt).get("data") or {}).get("items")) or []
        for it in items:
            if it.get("name") == "hermes-acceptance":
                k = it["key"]
                key = k if str(k).startswith("sk-") else "sk-" + k
    if not key:
        sys.exit("建 token 失败")

    # 列表接口只回脱敏的 key，真 key 只能从库里取（本机自有测试实例，允许直连 SQLite）
    db = os.environ.get("DISTILL_SQLITE", r"C:\Users\xiang\.dsh\bot\workspace\new-api-verify\hermes3101\data\hermes.db")
    try:
        import sqlite3
        con = sqlite3.connect(db)
        row = con.execute("select key from tokens where name='hermes-acceptance' order by id desc limit 1").fetchone()
        if row:
            key = "sk-" + row[0]
    except Exception as e:
        print("读库取 token 失败（沿用脱敏值会失败）:", e)

    code, txt = http("GET", "/api/user/self", None, tok)
    uid = ((json.loads(txt).get("data") or {}).get("id")) if code == 200 else None

    # 空库没给 distill-a/distill-b 配价格，自用模式最省事（否则 relay 回 400「价格未配置」）
    code, txt = http("PUT", "/api/option/", {"key": "SelfUseModeEnabled", "value": "true"}, tok)
    print("开启自用模式 ->", code, txt[:120])

    env.update({"base_url": BASE, "relay_key": key, "user_id": uid})
    json.dump(env, open(OUT, "w"), indent=2)
    try:
        os.chmod(OUT, 0o600)
    except Exception:
        pass
    print("env 写入", OUT, "| user_id =", uid, "| token 前缀", key[:6] + "***")


if __name__ == "__main__":
    main()
