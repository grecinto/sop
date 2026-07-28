---
sidebar_position: 5
title: AI Window
description: Use the AI window as the primary interaction surface while the system streams command output and execution state back to you.
---

# AI Window

AI Studio centers the experience around the AI window. The AI window is the primary interaction surface, and the terminal is the supporting mechanism the AI uses to show you the running commands and what is happening in real time.

## What this experience gives you

The terminal experience helps you:

- See when a command is actually running
- Observe live output instead of waiting for a final response
- Distinguish between background execution and visible terminal execution
- Understand whether a command completed, failed, or was cancelled

## The AI Window and command visibility

The AI chat window is the primary surface for interaction. When a command is launched, the AI can surface command progress, terminal output, and completion status directly in that window so the user can stay focused on the conversation.

The terminal is a supporting capability underneath that experience. It is used when the AI needs to run a command, observe its behavior, or interact with a real shell, while the AI window remains the main place where the user sees progress and responds.

## Floating AI Window for desktop use

AI Studio can also be surfaced through a floating AI chat window that stays above other applications. This makes the AI window the primary desktop surface, while the terminal remains a sub-feature that the AI can use behind the scenes when it needs to run commands.

The floating window can be placed anywhere on the screen and used as a lightweight companion for:

- monitoring running commands while you work elsewhere
- asking for help without leaving the current app
- keeping an AI assistant visible while you switch contexts
- governing the desktop experience from a persistent AI chat surface

In this mode, the AI chat window remains the main interaction layer, and the terminal is simply one of the tools the AI uses to carry out the task.

## When to use terminal mode

Use terminal mode when a command needs interaction or real-time visibility, such as:

- `sudo` commands that require a password
- `ssh` or `scp` flows
- editors like `vim` or `nano`
- monitoring commands like `top`, `htop`, or `tail -f`
- interactive shells and REPLs

## When background mode is better

Use background mode for non-interactive tasks such as:

- `git status`
- `ls` or `pwd`
- simple builds or tests
- short scripts that only need a final result

Background mode is faster and keeps the workflow focused on the result rather than the live terminal session.

## Observing running commands

If a command is long-running, the system shows progress and output in the AI window so you can tell whether it is still active or stuck. This helps with:

- debugging a command that is hanging
- watching a server or build process
- confirming that a command started correctly
- understanding whether a command was stopped by the user

## Best practices

- Use terminal mode for interactive or sensitive commands.
- Use background mode for short, non-interactive work.
- If something looks stuck, inspect the live output before assuming it failed.
- Treat the AI window as the observability surface for active command execution.
- Use the floating window when you want persistent desktop-level access to AI assistance while working across apps.

## Summary

The terminal experience and AI window together make command execution more transparent. They are especially valuable when you want to observe ongoing work, interact with commands directly, and understand what the system is doing in real time.
