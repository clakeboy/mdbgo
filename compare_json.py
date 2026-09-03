#!/usr/bin/env python3
"""对比两个目录中的 JSON 文件，并生成 Markdown 差异报告。"""

from __future__ import annotations

import argparse
import html
import json
import sys
from pathlib import Path
from typing import Any


MISSING = object()


def logical_name(path: Path, root: Path, strip_org_suffix: bool) -> str:
    """返回用于两边配对的相对文件名。"""
    relative = path.relative_to(root)
    name = relative.name
    if strip_org_suffix and name.endswith("_org.json"):
        name = name[: -len("_org.json")] + ".json"
    return relative.with_name(name).as_posix()


def collect_json_files(root: Path, strip_org_suffix: bool) -> dict[str, Path]:
    """收集目录下的 JSON 文件，并转换为统一的逻辑文件名。"""
    files: dict[str, Path] = {}
    for path in sorted(root.rglob("*.json")):
        key = logical_name(path, root, strip_org_suffix)
        if key in files:
            raise ValueError(f"逻辑文件名重复：{key}")
        files[key] = path
    return files


def json_path(parent: str, child: str | int) -> str:
    """拼接适合 Markdown 展示的 JSON 路径。"""
    if isinstance(child, int):
        return f"{parent}[{child}]"
    return f'{parent}[{json.dumps(child, ensure_ascii=False)}]'


def compare_json(left: Any, right: Any, path: str = "$", differences: list[tuple[str, Any, Any]] | None = None) -> list[tuple[str, Any, Any]]:
    """递归比较两个 JSON 值，并返回所有不同的路径和值。"""
    if differences is None:
        differences = []

    if isinstance(left, dict) and isinstance(right, dict):
        keys = sorted(set(left) | set(right))
        for key in keys:
            compare_json(
                left.get(key, MISSING),
                right.get(key, MISSING),
                json_path(path, key),
                differences,
            )
        return differences

    if isinstance(left, list) and isinstance(right, list):
        for index in range(max(len(left), len(right))):
            left_value = left[index] if index < len(left) else MISSING
            right_value = right[index] if index < len(right) else MISSING
            compare_json(left_value, right_value, json_path(path, index), differences)
        return differences

    if left is MISSING or right is MISSING:
        differences.append((path, left, right))
        return differences

    if isinstance(left, bool) != isinstance(right, bool) or left != right:
        differences.append((path, left, right))
    return differences


def load_json(path: Path) -> tuple[Any | None, str | None]:
    """读取并解析 JSON 文件，返回解析值或错误信息。"""
    try:
        return json.loads(path.read_text(encoding="utf-8-sig")), None
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        return None, str(exc)


def markdown_value(value: Any) -> str:
    """把 JSON 值编码为不会破坏 Markdown 表格的文本。"""
    if value is MISSING:
        return "*不存在*"
    rendered = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    return f"<code>{html.escape(rendered)}</code>"


def display_path(path: Path) -> str:
    """优先以仓库相对路径展示目录，避免报告依赖本机绝对路径。"""
    try:
        return path.relative_to(Path(__file__).resolve().parent).as_posix()
    except ValueError:
        return path.as_posix()


def append_file_list(lines: list[str], title: str, files: list[str]) -> None:
    """向报告追加只存在于一边的文件列表。"""
    if not files:
        return
    lines.extend([f"## {title}", ""])
    for name in files:
        lines.append(f"- `{name}`")
    lines.append("")


def build_report(
    left_root: Path,
    right_root: Path,
    left_files: dict[str, Path],
    right_files: dict[str, Path],
) -> tuple[str, bool]:
    """比较文件集合并生成完整的 Markdown 报告。"""
    all_names = sorted(set(left_files) | set(right_files))
    only_left = sorted(set(left_files) - set(right_files))
    only_right = sorted(set(right_files) - set(left_files))
    same_count = 0
    different_files: list[tuple[str, list[tuple[str, Any, Any]]]] = []
    parse_errors: list[tuple[str, str, str]] = []

    for name in sorted(set(left_files) & set(right_files)):
        left_value, left_error = load_json(left_files[name])
        right_value, right_error = load_json(right_files[name])
        if left_error or right_error:
            parse_errors.append((name, left_error or "", right_error or ""))
            continue

        differences = compare_json(left_value, right_value)
        if differences:
            different_files.append((name, differences))
        else:
            same_count += 1

    lines = [
        "# JSON 差异报告",
        "",
        f"- 基准目录：`{display_path(left_root)}`",
        f"- 对比目录：`{display_path(right_root)}`",
        "- 配对规则：基准目录文件名末尾的 `_org` 会被去掉后与对比目录配对。",
        "",
        "## 汇总",
        "",
        "| 项目 | 数量 |",
        "| --- | ---: |",
        f"| JSON 文件总数（去重后） | {len(all_names)} |",
        f"| 成功配对 | {len(set(left_files) & set(right_files))} |",
        f"| 内容相同 | {same_count} |",
        f"| 内容不同 | {len(different_files)} |",
        f"| 解析失败 | {len(parse_errors)} |",
        f"| 仅在基准目录 | {len(only_left)} |",
        f"| 仅在对比目录 | {len(only_right)} |",
        "",
    ]

    append_file_list(lines, "仅在基准目录", only_left)
    append_file_list(lines, "仅在对比目录", only_right)

    if parse_errors:
        lines.extend(["## JSON 解析失败", ""])
        for name, left_error, right_error in parse_errors:
            lines.append(f"### `{name}`")
            if left_error:
                lines.append(f"- 基准目录：`{left_error}`")
            if right_error:
                lines.append(f"- 对比目录：`{right_error}`")
            lines.append("")

    lines.extend(["## 内容差异", ""])
    if not different_files:
        lines.append("无内容差异。")
        lines.append("")
    else:
        for name, differences in different_files:
            lines.extend([f"### `{name}`", "", "| JSON 路径 | 基准目录 | 对比目录 |", "| --- | --- | --- |"])
            for path, left_value, right_value in differences:
                lines.append(
                    f"| `{path}` | {markdown_value(left_value)} | {markdown_value(right_value)} |"
                )
            lines.append("")

    has_difference = bool(only_left or only_right or different_files or parse_errors)
    return "\n".join(lines), has_difference


def parse_args() -> argparse.Namespace:
    """解析命令行参数。"""
    script_root = Path(__file__).resolve().parent
    dms_root = script_root / "testdb" / "dms"
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--export", type=Path, default=dms_root / "export", help="基准 JSON 目录")
    parser.add_argument("--mdbgo", type=Path, default=dms_root / "mdbgo", help="mdbgo JSON 目录")
    parser.add_argument("--output", type=Path, default=dms_root / "diff.md", help="差异报告路径")
    parser.add_argument("--check", action="store_true", help="发现差异时以状态码 1 退出")
    return parser.parse_args()


def main() -> int:
    """执行目录对比并写入 Markdown 报告。"""
    args = parse_args()
    left_root = args.export.resolve()
    right_root = args.mdbgo.resolve()
    output = args.output.resolve()

    for root in (left_root, right_root):
        if not root.is_dir():
            print(f"目录不存在：{root}", file=sys.stderr)
            return 2

    try:
        left_files = collect_json_files(left_root, strip_org_suffix=True)
        right_files = collect_json_files(right_root, strip_org_suffix=False)
        report, has_difference = build_report(left_root, right_root, left_files, right_files)
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(report, encoding="utf-8")
    except (OSError, ValueError) as exc:
        print(f"生成报告失败：{exc}", file=sys.stderr)
        return 2

    print(f"已生成：{output}")
    if args.check and has_difference:
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
