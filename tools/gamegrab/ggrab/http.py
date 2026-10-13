# -*- coding: utf-8 -*-
"""http — 补全阶段用的轻量 HTTP GET（带 Cookie/UA，限流由调用方控制）。"""

from __future__ import annotations

import urllib.error
import urllib.request

_UA = "Mozilla/5.0 gamegrab"


def http_get(url: str, timeout: int = 20, cookies: str = ""):
    """GET 一个 URL，返回 (body|None, status|错误串)。"""
    headers = {"User-Agent": _UA}
    if cookies:
        headers["Cookie"] = cookies
    req = urllib.request.Request(url, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.read(), r.status
    except urllib.error.HTTPError as e:
        return None, e.code
    except Exception as e:
        return None, str(e)[:60]
