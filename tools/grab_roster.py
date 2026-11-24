#!/usr/bin/env python3
"""
读取 Gemini 网页端当前账号的模型清单。

只做两件事：
  1. GET  https://gemini.google.com/app   —— 拿 SNlM0e（XSRF）和 cfb2h（bl 构建号）
  2. POST batchexecute?rpcids=otAQ7b      —— 尝试取模型表

只读，不发送对话、不修改账号状态。Cookie 从文件读取，不打印。

注意：2026-11-18 这一版网页端已不再接受 otAQ7b（服务端回 400 error 帧）。
那时本脚本会打印错误帧并正常退出——此时改用浏览器抓 L5adhe：
选中模型后，它把模型 hex 放在 req0___data__ 的第 101 槽。

用法：
    GEMINI_COOKIE_FILE=~/.gemini_cookie python3 -I grab_roster.py
"""
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

UA = ("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/158.0.0.0 Safari/537.36")
BASE = "https://gemini.google.com"


def load_cookie() -> str:
    path = os.environ.get("GEMINI_COOKIE_FILE", os.path.expanduser("~/.gemini_cookie"))
    try:
        raw = open(path, encoding="utf-8").read().strip()
    except OSError as e:
        sys.exit(f"读不到 cookie 文件 {path}: {e}")
    line = raw.splitlines()[0].strip()
    if line.lower().startswith("cookie:"):
        line = line[7:].strip()
    if len(line) < 100:
        sys.exit("cookie 太短，像是没贴全")
    return line


def http(url, cookie, data=None):
    """返回 (状态码, 响应文本)。4xx/5xx 不抛异常，好把服务端错误帧打出来。"""
    headers = {
        "User-Agent": UA,
        "Accept": "*/*",
        "Accept-Language": "zh-CN,zh;q=0.9",
        "Cookie": cookie,
        "Origin": BASE,
        "Referer": BASE + "/",
    }
    if data is not None:
        headers["Content-Type"] = "application/x-www-form-urlencoded;charset=utf-8"
        headers["X-Same-Domain"] = "1"
    req = urllib.request.Request(url, data=data, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=40) as r:
            return r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")


def walk(node, out, depth=0):
    if depth > 8:
        return
    if isinstance(node, list):
        if any(isinstance(x, str) and re.fullmatch(r"[0-9a-f]{16}", x) for x in node):
            out.append([x if not isinstance(x, list) else "[…]" for x in node])
        for x in node:
            walk(x, out, depth + 1)
    elif isinstance(node, dict):
        for x in node.values():
            walk(x, out, depth + 1)


def main():
    cookie = load_cookie()

    code, page = http(BASE + "/app", cookie)
    if code != 200:
        sys.exit(f"GET /app -> {code}，cookie 可能已失效")
    at = re.search(r'"SNlM0e":"(.*?)"', page)
    if not at:
        sys.exit("页面里没有 SNlM0e —— cookie 已失效或被跳到登录页")
    bl = re.search(r'"cfb2h":"(.*?)"', page) or re.search(r"(boq_gemini-web-[A-Za-z0-9._]+)", page)
    bl = bl.group(1) if bl else "boq_gemini-web-uiserver"
    print(f"cookie 有效，页面 {len(page)} 字节，bl = {bl}")

    body = urllib.parse.urlencode({
        "f.req": '[["otAQ7b","[]",null,"generic"]]',
        "at": at.group(1),
    }).encode()
    url = (f"{BASE}/_/BardChatUi/data/batchexecute?rpcids=otAQ7b"
           f"&source-path=%2Fapp&bl={urllib.parse.quote(bl)}&hl=zh-CN&_reqid=100001&rt=c")
    code, resp = http(url, cookie, body)

    if code != 200:
        print(f"\nPOST otAQ7b -> {code}")
        print(resp[:400].replace("\n", "\\n"))
        print("\n这一版网页端已不再接受该 RPC（属于上游改版，不是 cookie 或参数问题）。")
        print("取模型 hex 请改用浏览器抓 L5adhe：选中模型后，它的 req0___data__ 第 101 槽就是 hex。")
        return

    payloads = []
    for line in resp.splitlines():
        line = line.strip()
        if not line.startswith("[["):
            continue
        try:
            chunk = json.loads(line)
        except json.JSONDecodeError:
            continue
        for item in chunk:
            if isinstance(item, list) and len(item) > 2 and item[0] == "wrb.fr" and item[2]:
                try:
                    payloads.append(json.loads(item[2]))
                except json.JSONDecodeError:
                    pass

    if not payloads:
        print("\n没有解出 wrb.fr 负载，原始响应前 400 字符：")
        print(resp[:400])
        return

    rows = []
    for p in payloads:
        walk(p, rows)
    print(f"\n共 {len(rows)} 条含 16 位 hex 的结构：\n")
    for r in rows:
        print(json.dumps(r[:25], ensure_ascii=False))


if __name__ == "__main__":
    main()
