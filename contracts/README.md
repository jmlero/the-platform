# Work-control contract map

The shared contract has two deliberately separate owners inside the monorepo:

- AppDeck's provider-specific OpenAPI and canonical version-1 JSON fixtures live in [`deck/api/`](../deck/api/).
- Marina's provider-neutral capabilities and reusable conformance suite live in [`marina/internal/workcontrol/`](../marina/internal/workcontrol/).

The fixture manifest is [`deck/api/fixtures/v1/manifest.json`](../deck/api/fixtures/v1/manifest.json). Planned fixture schemas describe the intended additive version-1 shapes but do not imply runtime availability. AppDeck advertises only implemented capabilities through `GET /api/v1/provider`.

Run `make conformance` from the repository root to build a real AppDeck binary, start it with an isolated SQLite database, and exercise it through Marina's consumer ports.

Do not create a second copy of the OpenAPI document or fixture corpus under this directory. This file is a stable map to the authoritative producer and consumer locations.
