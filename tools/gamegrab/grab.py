# -*- coding: utf-8 -*-
"""gamegrab — 通用 H5 游戏抓取器（人工点击 + 全量抓取 + 自动补全 + 一键打包）。

用途：
    打开一个游戏原站（有头浏览器），你手动点击操作（含点进关卡），
    工具在后台把「静态资源 + 所有服务端请求/响应（含 WebSocket）」全量抓取落盘，
    结束后自动：归位资源 → 引擎识别 + 补全缺失 → 打包为离线可玩 zip。

流水线（一条命令跑完）：
    1. 抓取   capture : 有头浏览器 + 全量 hook，静态资源与接口全量落盘
    2. 归位   relocate: 把误分类的静态资源按原 URL 归位
    3. 补全   complete: 引擎自动识别 + 清单补全 + 通用回退（reqlog 重放/扫描）
    4. 打包   pack    : 注入 XHR/fetch/WS 三合一离线补丁 + config.json + zip

用法：
    python grab.py <游戏URL> [--out 输出目录] [--id 游戏ID] [--wait 秒]
                   [--proxy 代理] [--mobile] [--engine cocos] [--no-pack] [--name 名]

    python grab.py diagnose <抓取目录>            # 可玩性诊断（结构化报告）
    python grab.py query <抓取目录> [--list-packs|--list-apis|--grep kw|--missing|--show rel]

结束：
    操作完成后【关闭浏览器窗口】即自动结束，随后自动补全并打包。
"""

import argparse
import os
import sys
from datetime import datetime

from ggrab.capture import Capture
from ggrab.complete import Completer
from ggrab.diagnose import diagnose, print_report
from ggrab.pack import pack_game
from ggrab.query import dispatch as query_dispatch, print_result
from ggrab.relocate import relocate
from ggrab.store import Store


def run_pipeline(args) -> str:
    """执行 抓取 -> 归位 -> 补全 -> 打包 全流水线。返回 zip 路径（可能为空）。"""
    if args.out:
        out_dir = args.out
    else:
        ts = datetime.now().strftime("%Y%m%d_%H%M%S")
        tag = f"game{args.id}_" if args.id else ""
        out_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                               "grab_out", f"{tag}{ts}")

    store = Store(out_dir)
    cap = Capture(args.url, store, args.id)
    cap.run(wait_sec=args.wait, proxy=args.proxy, mobile=args.mobile)

    relocate(out_dir)

    comp = Completer(out_dir, cookies=cap.cookies_jar)
    comp.run(force_engine=args.engine or "", use_fallback=not args.no_fallback)

    if args.no_pack:
        return ""
    return pack_game(out_dir, name=args.name, game_id=args.id)


def build_parser() -> argparse.ArgumentParser:
    ap = argparse.ArgumentParser(
        description="gamegrab — 通用 H5 游戏抓取器（抓取→归位→补全→打包）")
    ap.add_argument("command", nargs="?", default="",
                    help="子命令：留空=抓取；diagnose=诊断；query=查询")
    ap.add_argument("target", nargs="?", default="",
                    help="游戏 URL（抓取）或 抓取目录（diagnose/query）")

    # 抓取参数
    ap.add_argument("--out", default=None, help="输出目录（默认 ./grab_out/<时间戳>）")
    ap.add_argument("--id", default="", help="游戏 ID（用于输出目录命名）")
    ap.add_argument("--wait", type=int, default=0, help="自动等待秒数后结束（0=手动关窗结束）")
    ap.add_argument("--proxy", default="", help="浏览器代理，如 127.0.0.1:7890（默认直连）")
    ap.add_argument("--mobile", action="store_true", help="用手机 UA + 触屏 + 移动视口打开")
    ap.add_argument("--engine", default="", help="强制指定引擎适配器（默认自动识别）")
    ap.add_argument("--no-fallback", action="store_true", help="禁用通用回退补全")
    ap.add_argument("--no-pack", action="store_true", help="只抓取，不打包 zip")
    ap.add_argument("--name", default="", help="打包后的游戏名（默认 game<ID>）")

    # diagnose 参数
    ap.add_argument("--json", action="store_true", help="diagnose 时输出 JSON 报告")

    # query 参数
    ap.add_argument("--list-packs", action="store_true", help="query：列出引擎与 pack 清单")
    ap.add_argument("--list-apis", action="store_true", help="query：列出接口端点")
    ap.add_argument("--grep", default=None, help="query：在 reqlog/JS/JSON 中检索关键字")
    ap.add_argument("--missing", action="store_true", help="query：列出缺失资源")
    ap.add_argument("--show", default=None, help="query：查看落盘文件内容摘要")
    return ap


def main(argv=None):
    ap = build_parser()
    args = ap.parse_args(argv)

    if args.command == "diagnose":
        if not args.target:
            ap.error("diagnose 需要指定抓取目录")
        rep = diagnose(args.target)
        if args.json:
            print_result(rep, pretty=True)
        else:
            print_report(rep)
            # 同时落盘 report.json 便于 AI 读取
            import json
            with open(os.path.join(os.path.abspath(args.target), "report.json"),
                      "w", encoding="utf-8") as f:
                json.dump(rep, f, ensure_ascii=False, indent=2)
        return 0

    if args.command == "query":
        if not args.target:
            ap.error("query 需要指定抓取目录")
        print_result(query_dispatch(args.target, args), pretty=True)
        return 0

    # 默认：抓取流水线（command 即 URL）
    url = args.command or args.target
    if not url:
        ap.error("需要提供游戏 URL")
    args.url = url
    zip_path = run_pipeline(args)
    if zip_path:
        print(f"\n完成：{zip_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
