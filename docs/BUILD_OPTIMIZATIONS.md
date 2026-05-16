Build optimizations added in this branch

- CI: added actions/cache for Go modules and Go build cache to speed up dependency download and build.
- Local verification: ran 'go test ./...' successfully. Measure builds by timing 'go test' or CI job timings before/after.

To revert: remove the cache steps from .github/workflows/ci.yml and push a fix.
