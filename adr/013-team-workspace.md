# ADR 013: Team workspace navigation

**Status:** Accepted
**Date:** 2026-10-20
**Implementation:** pridkett/simple-llm-proxy#108

## Context

Teams, applications, and API keys have separate navigation entries that repeat
the same ownership selectors. Creating an application requires another trip to
the keys screen before it is usable. The ownership model remains useful, but
these screens make routine setup unnecessarily complicated.

## Decision

Use Teams as the single navigation entry. Its overview lists teams and exposes
Create team. Team creation asks only for a name and opens the new workspace.
The workspace contains applications and a Members tab. Selecting an application
shows its keys alongside the other applications.

Give teams, members, applications, creation forms, and key settings stable hash
routes. Show breadcrumbs below the navigation inside a workspace, but omit a
breadcrumb at the Teams overview. Redirect the former applications and keys
routes, preserving team and application identifiers from existing links.

Creating an application also creates its first key, named Default, and reveals
the full key once in a copyable dialog. All models are allowed and limits remain
unset unless explicitly specified. Keep optional limits in key settings. Keep
plaintext keys in component memory only, never URLs or browser storage.

Reuse the existing API and preserve system-admin mutation permissions. Application
and first-key creation are two existing requests. If the second request fails,
retain the created application and retry only key creation; never silently
create another application. Do not automatically retry a key request after an
uncertain network outcome, because it may already have created a credential.

## Consequences

Setup and maintenance share one workspace without changing database ownership,
budgets, or authentication. Existing membership and key administration remain
available. Overview counts may be unavailable when a secondary request fails;
show unknown counts instead of presenting them as zero.

An application can exist without its first key after a failed request. The UI
must make that outcome clear and offer recovery. A lost creation response cannot
recover plaintext from the existing API; an administrator can inspect and revoke
an unwanted key before creating a replacement.
