# Go native example

This is a hand-written baseline for the generated Go example in `goexample`.
It implements the same order-processing scenario and uses the same published
OpenAPI and protobuf contracts, but it does not import ServiceLib.

The baseline deliberately keeps the benchmark-relevant topology and behavior:

- separate order HTTP and inventory gRPC services;
- the same stock data and reservation rules;
- sequential dispatch of an order's items to gRPC calls, matching the generated pipeline;
- configurable gRPC connection count;
- a five-second request deadline and a one-second soft-deadline margin;
- `PROCESSING_ERROR` items when a gRPC call fails;
- `TIMED_OUT` when the soft deadline fires.

Run it with:

```sh
docker compose up --build
```

The optional `INVENTORY_SERVICE_RESPONSE_DELAY=10s` setting can be used to
exercise the soft-deadline path. This baseline has no framework telemetry or
task pools, so its benchmark difference from `go` is the end-to-end cost of
the framework-backed implementation, including those facilities.
