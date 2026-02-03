#!/usr/bin/env python3
"""Analyze git changes and suggest commit message."""

import subprocess
import sys

def get_staged_files():
    """Get list of staged files."""
    result = subprocess.run(['git', 'diff', '--cached', '--name-only'],
                          capture_output=True, text=True)
    return result.stdout.strip().split('\n') if result.stdout.strip() else []

def get_diff():
    """Get git diff of staged changes."""
    result = subprocess.run(['git', 'diff', '--cached'],
                          capture_output=True, text=True)
    return result.stdout

def analyze_changes():
    """Analyze changes and suggest commit type."""
    files = get_staged_files()
    diff = get_diff()

    if not files or not any(files):
        return "No staged changes found."

    analysis = {
        'files': files,
        'count': len([f for f in files if f]),
        'has_tests': any('test' in f for f in files),
        'has_docs': any(f.endswith('.md') for f in files),
        'has_go': any(f.endswith('.go') for f in files),
    }

    # Suggest commit type
    if analysis['has_tests']:
        commit_type = 'test'
    elif analysis['has_docs']:
        commit_type = 'docs'
    else:
        commit_type = 'feat'

    return f"Type: {commit_type}, Files: {analysis['count']}"

if __name__ == '__main__':
    print(analyze_changes())
