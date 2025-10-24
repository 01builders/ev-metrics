### Adding new monitors

- when adding a new monitor to monitor.go, try to keep the structure consistent with existing monitors, this means:
  - define a new package that exports a `Monitor` function, or create a struct that exposes some `monitorX` method.
  - add any new metrics to the `Metrics` struct in metrics.go, and initialize them in the `metrics.New` constructor.
  - ensure tests are present for the new metrics added.

### README File

- when new metrics are added, ensure to update the README file to include descriptions of the new metrics.
- when new cli flags are added, ensure to update the README file to include descriptions of the new flags.

### Lint code
- run `make lint-fix` to ensure code is formatted and linted.
