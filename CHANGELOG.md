# Changelog

## [Unreleased]

### Removed
- Removed the legacy Python harvester (chatdownloader + `fetch_chat.py`), making gnasty-chat the sole ingestion path backed by SQLite.

### Changed
- Docker builds now rely on a minimal Alpine runtime image with only the Go server and built frontend assets.
- CI/test docs now assume gnasty-chat is the exclusive harvester; Python setup steps were dropped.
