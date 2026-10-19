# Optional telemetry blocking

Garcon can block selected hosts for clients routed through it. Blocking is **off by default**. It is a host filter, not a machine-wide firewall: tools that ignore proxy settings or connect directly remain outside its reach.

## Enable

Create `~/.config/garcon/telemetry.json`:

```json
{
  "enabled": true,
  "listen": "127.0.0.1:4142",
  "block_hosts": [],
  "allow_hosts": []
}
```

Restart Garcon with `garcon service restart`. The service reads this file on startup, including after updates. `-telemetry-config FILE` selects another file for a foreground instance. Invalid settings stop startup with an explanation instead of silently disabling protection. Remove the file or set `enabled` to `false`, then restart, to disable blocking and close the extra listener.

The policy applies to all provider requests routed through Garcon. Ordinary provider routes have fixed upstream hosts: configuring a model's base URL alone does **not** route that tool's separate analytics requests through Garcon. For tools that support standard proxy variables, launch the tool with:

```sh
HTTP_PROXY=http://claude@127.0.0.1:4142 \
HTTPS_PROXY=http://claude@127.0.0.1:4142 \
NO_PROXY=localhost,127.0.0.1,::1 claude
```

The proxy username (`claude` here) is an optional client label for blocked-attempt logs; use the tool's name, never a secret. With no label, events say `proxy client`. Client support varies; verify an attempt in **Logs → System events**. The listener accepts only loopback connections, HTTP destinations on port 80, and HTTPS CONNECT destinations on port 443. It rejects browser-origin requests and cannot be made remotely accessible with `-allow-remote`.

## Hosts and overrides

The bundled list covers Datadog log intake hosts:

- `http-intake.logs.*.datadoghq.com`
- `http-intake.logs.datadoghq.com`
- `http-intake.logs.datadoghq.eu`

Garcon maintainers update this small list through normal releases. `block_hosts` adds owner-chosen hosts; `allow_hosts` takes precedence over both the bundled list and custom entries, so an incorrectly classified host can be restored without disabling the rest of the policy. Entries are case-insensitive DNS names. `*` matches exactly one complete label, so a rule never also matches an unrelated suffix such as `datadoghq.com.example.org`.

For example:

```json
{
  "enabled": true,
  "block_hosts": ["analytics.example.org"],
  "allow_hosts": ["http-intake.logs.us5.datadoghq.com"]
}
```

## What is recorded

A blocked request receives HTTP **403** and `X-Garcon-Telemetry-Blocked: true`. The existing system events view records its timestamp, client label, destination host, and HTTP path. Query strings, credentials, headers, and bodies are never logged by this feature.

HTTPS stays encrypted end to end. A blocked CONNECT attempt records the host and `(encrypted HTTPS path)` because its path is not visible; Garcon installs no certificate and performs no TLS interception. Allowed tunnels and HTTP requests are forwarded without content inspection. Host filtering cannot identify telemetry sent to a different host or an IP address, or distinguish telemetry from functional traffic sharing a host.

`GET /api/config` includes the effective `telemetry` configuration for checking whether the feature is enabled. The widget and provider allowance reads are unaffected; only client traffic passes through this policy.
