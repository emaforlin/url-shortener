# url-sortener

Just a URL shortening service

## Repo map

### Packages

```txt
// Package config loads and validates service configuration from the environment.
//
// Configuration is read once at startup by [Load]. A misconfigured process fails
// immediately with every problem reported at once, rather than surfacing the
// first issue and hiding the rest.
```

```txt
// Package logging builds the service logger and carries a request-scoped logger
// through the context, so every log line from a request can be correlated.
```

```txt
// Package links holds the URL shortening domain: the Link entity, the storage
// contract, and the service that enforces the rules.
//
// Nothing in this package imports net/http. Transport concerns stay in the api
// package, which keeps the domain testable without a server and lets the storage
// backend be swapped without touching handlers.
```