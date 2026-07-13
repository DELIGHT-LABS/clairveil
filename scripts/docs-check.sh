#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

python3 - "$repo_root" <<'PY'
import os
import json
import re
import subprocess
import sys
from datetime import date
from pathlib import Path, PurePosixPath
from urllib.parse import unquote, urlsplit


repo = Path(sys.argv[1]).resolve()
excluded_dapp = PurePosixPath("examples/clairveil-dapp")
generated_release_files = {"RELEASE-MANIFEST.txt", "SHA256SUMS.txt"}
errors = []


def add_error(message):
    errors.append(message)


def git_output(*args, text=True):
    result = subprocess.run(
        ["git", "-C", os.fspath(repo), *args],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=text,
    )
    return result.stdout


def git_paths(*patterns):
    raw = git_output("ls-files", "-z", "--", *patterns, text=False)
    return [
        Path(os.fsdecode(item))
        for item in raw.split(b"\0")
        if item
    ]


def is_excluded_dapp(path):
    parts = PurePosixPath(path.as_posix()).parts
    excluded_parts = excluded_dapp.parts
    return parts[: len(excluded_parts)] == excluded_parts


markdown_documents = {}


def parse_markdown_paths(paths):
    pending = sorted({path.resolve() for path in paths if path.resolve() not in markdown_documents})
    if not pending:
        return
    try:
        result = subprocess.run(
            [
                "go",
                "run",
                "-mod=readonly",
                os.fspath(repo / "scripts/markdown-ast.go"),
                *(os.fspath(path) for path in pending),
            ],
            cwd=repo,
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        parsed = json.loads(result.stdout)
        for document in parsed.get("documents", []):
            markdown_documents[Path(document["path"]).resolve()] = document
    except (subprocess.CalledProcessError, json.JSONDecodeError, KeyError) as error:
        detail = getattr(error, "stderr", "") or str(error)
        add_error(f"CommonMark parser를 실행하지 못했습니다: {detail.strip()}")
    for path in pending:
        markdown_documents.setdefault(path, {"links": [], "headings": []})


def markdown_destinations(path):
    resolved = path.resolve()
    parse_markdown_paths([resolved])
    return [
        (int(link["line"]), str(link["destination"]))
        for link in markdown_documents[resolved]["links"]
    ]


def markdown_headings(path):
    resolved = path.resolve()
    parse_markdown_paths([resolved])
    return markdown_documents[resolved]["headings"]


def local_link_target(source, destination):
    destination = destination.strip()
    if destination.startswith("<") and destination.endswith(">"):
        destination = destination[1:-1]
    destination = destination.replace("\\ ", " ")
    if not destination or destination.startswith(("/", "//")):
        return None, None, None

    parsed = urlsplit(destination)
    if parsed.scheme or parsed.netloc:
        return None, None, None
    target_text = unquote(parsed.path)
    fragment = unquote(parsed.fragment)
    candidate = (
        (source.parent / target_text).resolve(strict=False)
        if target_text
        else source.resolve()
    )
    try:
        relative = candidate.relative_to(repo)
    except ValueError:
        return None, "repository 밖을 가리키는 상대 링크", fragment
    if is_excluded_dapp(relative):
        return None, None, None
    return candidate, None, fragment


def markdown_fragment_error(candidate, fragment):
    if not fragment or not candidate.is_file() or candidate.suffix.lower() != ".md":
        return None
    heading_ids = {str(heading["id"]) for heading in markdown_headings(candidate)}
    if fragment not in heading_ids:
        return f"존재하지 않는 Markdown fragment: #{fragment}"
    return None


working_markdown = []
try:
    markdown_paths = set(git_paths("*.md"))
    untracked_raw = git_output(
        "ls-files", "--others", "--exclude-standard", "-z", "--", "*.md", text=False
    )
    markdown_paths.update(
        Path(os.fsdecode(item)) for item in untracked_raw.split(b"\0") if item
    )
    for relative in sorted(markdown_paths):
        if is_excluded_dapp(relative):
            continue
        absolute = repo / relative
        # A tracked file intentionally moved in the current worktree is no longer
        # part of the documentation set being checked.
        if absolute.is_file():
            working_markdown.append((relative, absolute))
except subprocess.CalledProcessError as error:
    add_error(f"working-tree Markdown 목록을 읽지 못했습니다: {error.stderr.strip()}")

parse_markdown_paths([absolute for _, absolute in working_markdown])

for relative, source in working_markdown:
    for line_number, destination in markdown_destinations(source):
        candidate, link_error, fragment = local_link_target(source, destination)
        if link_error:
            add_error(f"{relative}:{line_number}: {link_error}: {destination}")
        elif candidate is not None and not candidate.exists():
            add_error(f"{relative}:{line_number}: 존재하지 않는 상대 링크: {destination}")
        elif candidate is not None:
            fragment_error = markdown_fragment_error(candidate, fragment)
            if fragment_error:
                add_error(f"{relative}:{line_number}: {fragment_error}: {destination}")


docs_dir = repo / "docs"
for document in sorted(docs_dir.glob("*.md")):
    if document.stem.endswith("-kr"):
        counterpart = document.with_name(document.stem[:-3] + ".md")
    else:
        counterpart = document.with_name(document.stem + "-kr.md")
    if not counterpart.is_file():
        add_error(
            f"{document.relative_to(repo)}: EN/KR 대응 문서가 없습니다: "
            f"{counterpart.relative_to(repo)}"
        )

docs_indexes = [docs_dir / "README.md", docs_dir / "README-kr.md"]
knowledge_documents = [
    document.resolve()
    for document in sorted(docs_dir.glob("*.md"))
    if document.name not in {"README.md", "README-kr.md"}
]
for index in docs_indexes:
    if not index.is_file():
        add_error(f"필수 docs index가 없습니다: {index.relative_to(repo)}")
        continue
    indexed_targets = set()
    for line_number, destination in markdown_destinations(index):
        candidate, link_error, _ = local_link_target(index, destination)
        if link_error:
            add_error(f"{index.relative_to(repo)}:{line_number}: {link_error}: {destination}")
        elif candidate is not None:
            indexed_targets.add(candidate)
    for document in knowledge_documents:
        if document not in indexed_targets:
            add_error(
                f"{index.relative_to(repo)}: 최상위 knowledge 문서를 열거하지 않았습니다: "
                f"{document.relative_to(repo)}"
            )


plans_dir = repo / "plans"
plan_indexes = [plans_dir / "README.md", plans_dir / "README-kr.md"]
tracked_plans = []
try:
    plan_paths = set(git_paths("plans/*.md"))
    untracked_plan_raw = git_output(
        "ls-files", "--others", "--exclude-standard", "-z", "--", "plans/*.md", text=False
    )
    plan_paths.update(
        Path(os.fsdecode(item)) for item in untracked_plan_raw.split(b"\0") if item
    )
    for relative in sorted(plan_paths):
        absolute = repo / relative
        if absolute.parent == plans_dir and absolute.name not in {"README.md", "README-kr.md"} and absolute.is_file():
            tracked_plans.append(absolute.resolve())
except subprocess.CalledProcessError as error:
    add_error(f"tracked plan 목록을 읽지 못했습니다: {error.stderr.strip()}")

for index in plan_indexes:
    if not index.is_file():
        add_error(f"필수 plan index가 없습니다: {index.relative_to(repo)}")
        continue
    indexed_targets = set()
    for line_number, destination in markdown_destinations(index):
        candidate, link_error, _ = local_link_target(index, destination)
        if link_error:
            add_error(f"{index.relative_to(repo)}:{line_number}: {link_error}: {destination}")
        elif candidate is not None:
            indexed_targets.add(candidate)
    for plan in tracked_plans:
        if plan not in indexed_targets:
            add_error(
                f"{index.relative_to(repo)}: tracked 최상위 plan을 열거하지 않았습니다: "
                f"{plan.relative_to(repo)}"
            )


try:
    tags = [line for line in git_output("tag", "--list").splitlines() if line]
except subprocess.CalledProcessError as error:
    tags = []
    add_error(f"Git tag 목록을 읽지 못했습니다: {error.stderr.strip()}")

semver_prerelease_identifier = (
    r"(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
)
semver_tag = re.compile(
    r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
    rf"(?:-{semver_prerelease_identifier}"
    rf"(?:\.{semver_prerelease_identifier})*)?"
    r"(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"
)
for tag in tags:
    if not semver_tag.fullmatch(tag):
        add_error(f"Git tag가 exact v-prefixed SemVer가 아닙니다: {tag}")
        continue
    try:
        tag_type = git_output("cat-file", "-t", f"refs/tags/{tag}").strip()
    except subprocess.CalledProcessError as error:
        add_error(f"Git tag type을 읽지 못했습니다: {tag}: {error.stderr.strip()}")
        continue
    if tag_type != "tag":
        add_error(f"Git tag가 annotated tag가 아닙니다: {tag}")
        continue
    try:
        tag_object = git_output("cat-file", "-p", f"refs/tags/{tag}")
    except subprocess.CalledProcessError as error:
        add_error(f"Git tag target을 읽지 못했습니다: {tag}: {error.stderr.strip()}")
        continue
    target_type = next(
        (line.removeprefix("type ") for line in tag_object.splitlines() if line.startswith("type ")),
        "",
    )
    if target_type != "commit":
        add_error(f"Git tag가 commit을 직접 가리키지 않습니다: {tag}")

changelog_dates = {}
for changelog_name in ("CHANGELOG.md", "CHANGELOG-kr.md"):
    changelog = repo / changelog_name
    headings = markdown_headings(changelog)
    for tag in tags:
        escaped_tag = re.escape(tag)
        heading = re.compile(
            rf"^{escaped_tag}[ \t]+-[ \t]+"
            rf"([0-9]{{4}}-[0-9]{{2}}-[0-9]{{2}})[ \t]*$"
        )
        matches = [
            match.group(1)
            for value in headings
            if int(value["level"]) == 2
            for match in [heading.fullmatch(str(value["text"]))]
            if match is not None
        ]
        if not matches:
            add_error(f"{changelog_name}: 날짜가 있는 tag heading이 없습니다: {tag}")
        elif len(matches) != 1:
            add_error(f"{changelog_name}: tag heading이 정확히 하나가 아닙니다: {tag}")
        else:
            try:
                date.fromisoformat(matches[0])
            except ValueError:
                add_error(f"{changelog_name}: tag heading 날짜가 유효하지 않습니다: {tag}: {matches[0]}")
            changelog_dates[(changelog_name, tag)] = matches[0]

for tag in tags:
    english_date = changelog_dates.get(("CHANGELOG.md", tag))
    korean_date = changelog_dates.get(("CHANGELOG-kr.md", tag))
    if english_date and korean_date and english_date != korean_date:
        add_error(
            f"CHANGELOG.md와 CHANGELOG-kr.md의 tag 날짜가 다릅니다: "
            f"{tag}: {english_date} != {korean_date}"
        )


tmp_dir = repo / "tmp"
if tmp_dir.is_dir():
    tmp_markdown = sorted(tmp_dir.rglob("*.md"))
    if tmp_markdown:
        preview = ", ".join(str(path.relative_to(repo)) for path in tmp_markdown[:5])
        suffix = "" if len(tmp_markdown) <= 5 else f" 외 {len(tmp_markdown) - 5}개"
        add_error(
            f"runtime tmp 아래에 Markdown 문서 {len(tmp_markdown)}개가 남아 있습니다: "
            f"{preview}{suffix}"
        )

commit_notes_dir = docs_dir / "commit-notes"
if commit_notes_dir.is_dir():
    commit_notes = sorted(path for path in commit_notes_dir.rglob("*") if path.is_file())
    if commit_notes:
        preview = ", ".join(str(path.relative_to(repo)) for path in commit_notes[:5])
        suffix = "" if len(commit_notes) <= 5 else f" 외 {len(commit_notes) - 5}개"
        add_error(
            f"docs/commit-notes 아래에 문서 {len(commit_notes)}개가 남아 있습니다: "
            f"{preview}{suffix}"
        )


def validate_manifest_path(value, manifest_name, line_number):
    if not value:
        add_error(f"{manifest_name}:{line_number}: 빈 path는 허용되지 않습니다")
        return None
    if any(ord(character) < 32 or ord(character) == 127 for character in value):
        add_error(f"{manifest_name}:{line_number}: control character가 포함되어 있습니다")
        return None
    if "\\" in value:
        add_error(f"{manifest_name}:{line_number}: POSIX path가 아닙니다: {value}")
        return None
    path = PurePosixPath(value)
    if path.is_absolute() or str(path) != value or any(part in ("", ".", "..") for part in path.parts):
        add_error(f"{manifest_name}:{line_number}: 비정규 path입니다: {value}")
        return None
    return value


def read_path_manifest(relative_name):
    manifest = repo / relative_name
    try:
        text = manifest.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as error:
        add_error(f"{relative_name}: manifest를 읽을 수 없습니다: {error}")
        return []
    if text and not text.endswith("\n"):
        add_error(f"{relative_name}: 마지막 줄은 newline으로 끝나야 합니다")
    entries = []
    seen = set()
    for line_number, value in enumerate(text.splitlines(), 1):
        entry = validate_manifest_path(value, relative_name, line_number)
        if entry is None:
            continue
        if entry in seen:
            add_error(f"{relative_name}:{line_number}: 중복 path입니다: {entry}")
        else:
            seen.add(entry)
            entries.append(entry)
    if not entries:
        add_error(f"{relative_name}: manifest가 비어 있습니다")
    return entries


selected_manifest_name = "scripts/release-pack-paths.txt"
required_manifest_name = "scripts/release-pack-required-files.txt"
selected_paths = read_path_manifest(selected_manifest_name)
required_files = read_path_manifest(required_manifest_name)


def selects_excluded_dapp(entry):
    entry_parts = PurePosixPath(entry).parts
    excluded_parts = excluded_dapp.parts
    return (
        entry_parts[: len(excluded_parts)] == excluded_parts
        or excluded_parts[: len(entry_parts)] == entry_parts
    )


for selected in selected_paths:
    selected_path = repo / selected
    if selects_excluded_dapp(selected):
        add_error(f"{selected_manifest_name}: 제외 대상 dapp path를 선택합니다: {selected}")
    elif not selected_path.exists():
        add_error(f"{selected_manifest_name}: 존재하지 않는 선택 path입니다: {selected}")

required_set = set(required_files)
for required_generated in sorted(generated_release_files):
    if required_generated not in required_set:
        add_error(f"{required_manifest_name}: generated 필수 파일이 없습니다: {required_generated}")
if required_manifest_name not in required_set:
    add_error(f"{required_manifest_name}: manifest 자신을 required로 열거해야 합니다")
if selected_manifest_name not in required_set:
    add_error(f"{required_manifest_name}: release path manifest를 required로 열거해야 합니다")

selected_with_types = [
    (selected, (repo / selected).is_dir())
    for selected in selected_paths
    if (repo / selected).exists()
]
for required in required_files:
    if selects_excluded_dapp(required):
        add_error(f"{required_manifest_name}: 제외 대상 dapp path가 포함되어 있습니다: {required}")
        continue
    if required in generated_release_files:
        continue
    required_path = repo / required
    if not required_path.is_file():
        add_error(f"{required_manifest_name}: 존재하지 않는 필수 파일입니다: {required}")
    covered = any(
        required == selected
        or (is_directory and required.startswith(selected + "/"))
        for selected, is_directory in selected_with_types
    )
    if not covered:
        add_error(f"{required_manifest_name}: release path manifest가 포함하지 않습니다: {required}")


release_selected_files = set()
for selected, is_directory in selected_with_types:
    selected_path = repo / selected
    if is_directory:
        release_selected_files.update(
            path.resolve()
            for path in selected_path.rglob("*")
            if path.is_file() and not is_excluded_dapp(path.relative_to(repo))
        )
    elif selected_path.is_file():
        release_selected_files.add(selected_path.resolve())


def release_target_is_covered(target):
    target = target.resolve()
    if target in release_selected_files:
        return True
    if target.is_dir():
        for selected_file in release_selected_files:
            try:
                selected_file.relative_to(target)
                return True
            except ValueError:
                pass
    return False


for source in sorted(
    path for path in release_selected_files if path.suffix.lower() == ".md"
):
    for line_number, destination in markdown_destinations(source):
        candidate, link_error, _ = local_link_target(source, destination)
        if link_error or candidate is None or not candidate.exists():
            continue
        if not release_target_is_covered(candidate):
            add_error(
                f"{source.relative_to(repo)}:{line_number}: release pack 밖을 가리키는 "
                f"상대 링크: {destination}"
            )


unique_errors = list(dict.fromkeys(errors))
if unique_errors:
    print(f"docs check failed ({len(unique_errors)} issue(s)):", file=sys.stderr)
    for error in unique_errors:
        print(f"- {error}", file=sys.stderr)
    raise SystemExit(1)

print(f"working-tree Markdown links verified: {len(working_markdown)} file(s)")
print(f"docs EN/KR pairs and indexes verified: {len(list(docs_dir.glob('*.md')))} file(s)")
print(f"plan indexes verified: {len(tracked_plans)} tracked plan(s)")
print(f"changelog tag headings verified: {len(tags)} tag(s), 2 file(s)")
print(f"release manifests verified: {len(selected_paths)} selected path(s), {len(required_files)} required file(s)")
print(f"release Markdown link closure verified: {len(release_selected_files)} packed source file(s)")
PY
