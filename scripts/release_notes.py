#!/usr/bin/env python3
"""Write a release's notes, in English and in Chinese, to NOTES.md.

The app shows a release's notes in its own language: notes that have the
marker <!-- lang:zh --> are English above it and Chinese below it, and a
page in Chinese gets the Chinese (internal/update/notes.go, InLang). Owl's
releases used to have the English alone, so the app showed English in any
language.

Each line is a commit's subject. Its Chinese is the commit's Notes-zh
trailer:

    redact: mask the credentials AI coding chats leak

    Notes-zh: 脱敏：遮住 AI 写代码聊天里泄露的凭据

A commit with no Notes-zh keeps its English subject in the Chinese part.
With no Notes-zh in the release at all, the notes are English alone, as
before, and the app shows them in every language.

debt: only Chinese has a part of its own, since InLang knows the zh marker
alone; Japanese and German pages show the English. A language more is a
marker of its own here and in InLang.
"""

import argparse
import subprocess
import sys

MARKER = "<!-- lang:zh -->"


def git(*args):
    return subprocess.run(["git", *args], check=True, capture_output=True, text=True).stdout


def notes(rev_range):
    shas = git("log", "--no-merges", "--format=%H", rev_range).split()
    en, zh, any_zh = [], [], False
    for sha in shas:
        subject = git("log", "-1", "--format=%s", sha).strip()
        trailer = git("log", "-1", "--format=%(trailers:key=Notes-zh,valueonly,separator=%x1f)", sha).strip()
        chinese = trailer.split("\x1f")[0].strip() if trailer else ""
        en.append("- " + subject)
        zh.append("- " + (chinese or subject))
        any_zh = any_zh or bool(chinese)
    out = "\n".join(en) + "\n"
    if any_zh:
        out += "\n" + MARKER + "\n" + "\n".join(zh) + "\n"
    return out


def main():
    p = argparse.ArgumentParser()
    p.add_argument("range", help="PREV..TAG, or TAG alone for every commit to it")
    p.add_argument("--out", default="NOTES.md")
    a = p.parse_args()
    text = notes(a.range)
    with open(a.out, "w", encoding="utf-8") as f:
        f.write(text)
    sys.stdout.write(text)


if __name__ == "__main__":
    main()
