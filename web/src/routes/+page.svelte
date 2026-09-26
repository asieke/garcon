<script lang="ts">
  import { onMount } from "svelte";
  import codexSkill from "$lib/account-skills/codex.md?raw";
  import claudeSkill from "$lib/account-skills/claude.md?raw";
  import openrouterSkill from "$lib/account-skills/openrouter.md?raw";
  const accountSkills = { codex: codexSkill, claude: claudeSkill, openrouter: openrouterSkill };
  let skillProvider = $state<keyof typeof accountSkills>("codex");
  let inventory = $state<{accounts: {id:string; provider:string; email:string; status:string}[]; openrouter_configured?: boolean}>({accounts:[]});
  let providerURLs = $state<Record<string,string>>({});
  let configReady = $state(false);
  import Icon from "$lib/console/Icon.svelte";
  import CodexLogo from "$lib/console/CodexLogo.svelte";
  import { parseCatalog, type Catalog } from "$lib/pricing";
  import {
    count,
    usd,
    tokens,
    cost,
    ago,
    duration,
    initials,
    tone,
    mergeGroups,
    type Account,
    type Routing,
    type RequestRow,
    type Session,
    type Aggregate,
    type LogPage,
  } from "$lib/console/model";
  import "$lib/console/console.css";
  type View = "accounts" | "sessions" | "analytics" | "logs";
  let view = $state<View>("accounts");
  let routing = $state<Routing>({ enabled: false, accounts: [] });
  let sessions = $state<Session[]>([]),
    aggregates = $state<Aggregate[]>([]),
    feed = $state<RequestRow[]>([]);
  let logs = $state<LogPage>({ requests: [], total: 0, offset: 0, limit: 50 });
  let eventOffset = $state(0),
    eventTotal = $state(0);
  let events = $state<{ id: number; time: number; message: string }[]>([]);
  let catalog = $state<Catalog | null>(null);
  let config = $state({
    data: "",
    version: "",
    listen: "127.0.0.1:4141",
    rows: 0,
    remote_read_only: true,
  });
  const readOnly = $derived(!configReady || config.remote_read_only !== false);
  let loaded = $state(false),
    connected = $state(false),
    busy = $state(false),
    refreshing = $state(false),
    expanded = $state(true);
  let error = $state(""),
    notice = $state(""),
    now = $state(Date.now());
  let modal = $state<"account" | "connect" | null>(null),
    copied = $state("");
  let query = $state(""),
    sessionQuery = $state(""),
    errorsOnly = $state(false),
    logMode = $state<"requests" | "system">("requests"),
    offset = $state(0),
    range = $state("7"),
    metric = $state<"usage" | "cost" | "models">("usage");
  let selected = $state<RequestRow | null>(null),
    activeOnly = $state(false),
    tickerPaused = $state(false);
  let logGeneration = 0,
    analyticsGeneration = 0,
    alive = true;
  const views: { id: View; label: string; description: string }[] = [
    {
      id: "accounts",
      label: "Accounts",
      description: "Manage Codex accounts and routing priorities.",
    },
    {
      id: "sessions",
      label: "Sessions",
      description: "Find your Codex tasks and see which account is serving them.",
    },
    {
      id: "analytics",
      label: "Analytics",
      description: "Usage, models, and estimated API cost.",
    },
    {
      id: "logs",
      label: "Logs",
      description: "Search requests and service logs.",
    },
  ];
  const title = $derived(views.find((v) => v.id === view)!);
  const enrolled = $derived(routing.accounts.filter((a) => a.enrolled));
  const active = $derived(
    routing.accounts.reduce((n, a) => n + a.active_requests, 0),
  );
  const ready = $derived(enrolled.filter((a) => a.status === "Ready"));
  const next = $derived(routing.enabled ? ready[0] : undefined);
  const groups = $derived(
    [...new Set(routing.accounts.map((a) => a.priority))].sort((a, b) => a - b),
  );
  const filteredSessions = $derived(
    sessions.filter(
      (s) =>
        (!activeOnly || s.active > 0) &&
        `${s.task?.title ?? ""} ${s.task?.cwd ?? ""} ${s.session_id} ${s.account} ${s.model} ${s.account_id}`
          .toLowerCase()
          .includes(sessionQuery.toLowerCase()),
    ),
  );
  const sessionIndex = $derived(new Map(sessions.map((s) => [s.session_id, s])));
  function taskName(id?: string) {
    return id ? sessionIndex.get(id)?.task?.title || `Session ${id.slice(0, 8)}…${id.slice(-6)}` : "Unassigned session";
  }
  function projectName(s: Session) {
    return s.task?.cwd.split(/[\\/]/).filter(Boolean).at(-1) || "Project unavailable";
  }
  function sessionActivity(s: Session) {
    if (s.active > s.active_reviews) return "Model responding";
    if (s.active > 0) return "Background review";
    if (s.last_state === "failed" || s.last_status >= 400) return "Last request failed";
    if (s.last_state === "interrupted") return "Last request interrupted";
    return "No model request";
  }
  function elapsed(t: number) {
    const seconds = Math.max(0, Math.floor((now - t) / 1000));
    return seconds < 60 ? `${seconds}s` : `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  }
  const totalTokens = $derived(aggregates.reduce((n, r) => n + tokens(r), 0));
  const totalRequests = $derived(
    aggregates.reduce((n, r) => n + r.requests, 0),
  );
  const totalErrors = $derived(aggregates.reduce((n, r) => n + r.errors, 0));
  const estimatedCost = $derived(
    aggregates.reduce((n, r) => n + (cost(r, catalog) ?? 0), 0),
  );
  const unpriced = $derived(
    aggregates
      .filter((r) => cost(r, catalog) === null)
      .reduce((n, r) => n + r.requests, 0),
  );
  const totalCache = $derived(aggregates.reduce((n, r) => n + r.cache_read, 0));
  const inputTokens = $derived(
    aggregates.reduce((n, r) => n + r.input + r.cache_read + r.cache_write, 0),
  );
  const models = $derived(mergeGroups(aggregates, "model"));
  const accountTotals = $derived(mergeGroups(aggregates, "account"));
  const bars = $derived.by(() => {
    const days =
      range === "1"
        ? 24
        : range === "7"
          ? 7
          : range === "30"
            ? 30
            : Math.min(
                90,
                Math.max(
                  7,
                  Math.ceil((now - (aggregates[0]?.time ?? now)) / 86400000) +
                    1,
                ),
              );
    const step = range === "1" ? 3600000 : 86400000;
    const end = Math.floor(now / step) * step;
    return Array.from({ length: days }, (_, i) => {
      const time = end - (days - 1 - i) * step;
      const rows = aggregates.filter(
        (r) => r.time >= time && r.time < time + step,
      );
      return {
        time,
        value: rows.reduce(
          (n, r) =>
            n +
            (metric === "cost"
              ? (cost(r, catalog) ?? 0)
              : metric === "models"
                ? r.requests
                : tokens(r)),
          0,
        ),
      };
    });
  });
  const peak = $derived(Math.max(1, ...bars.map((b) => b.value)));
  const connectConfig =
    'model_provider = "garcon"\n\n[model_providers.garcon]\nname = "Garcon"\nbase_url = "http://127.0.0.1:4141/codex/backend-api/codex"\nwire_api = "responses"\nrequires_openai_auth = true\nsupports_websockets = false';
  async function api(path: string, options?: RequestInit) {
    const response = await fetch(path, options);
    if (!response.ok)
      throw new Error(`Garcon returned ${response.status}. Try again.`);
    return response.json();
  }
  async function loadLogs() {
    const generation = ++logGeneration;
    try {
      const result = await api(
        `/api/logs?limit=50&offset=${offset}&q=${encodeURIComponent(query)}&errors=${errorsOnly}`,
      );
      if (alive && generation === logGeneration) logs = result;
    } catch (e) {
      if (alive) error = (e as Error).message;
    }
  }
  async function loadAnalytics() {
    const generation = ++analyticsGeneration;
    try {
      const since = range === "all" ? 0 : Date.now() - Number(range) * 86400000;
      const result = await api(`/api/analytics?since=${since}`);
      if (alive && generation === analyticsGeneration) aggregates = result;
    } catch (e) {
      if (alive) error = (e as Error).message;
    }
  }
  async function poll() {
    try {
      const [r, s, f, c, a, p] = await Promise.all([
        api("/api/routing/codex"),
        api("/api/sessions"),
        api("/api/usage/recent"),
        api("/api/config"), api("/api/accounts"), api("/api/providers"),
      ]);
      if (!alive) return;
      config = c; configReady = true; inventory = a; providerURLs = p;
      if (!busy) routing = r;
      sessions = s;
      feed = [...f.requests].reverse();
      connected = true;
      loaded = true;
      now = Date.now();
      if (view === "logs") await loadLogs();
      if (view === "analytics") await loadAnalytics();
    } catch {
      if (alive) {
        configReady = false;
        connected = false;
        loaded = true;
      }
    }
  }
  onMount(() => {
    alive = true;
    const q = new URLSearchParams(location.search).get("view");
    if (q === "usage" || q === "cost" || q === "models") {
      view = "analytics";
      metric = q === "usage" ? "usage" : q;
    } else if (views.some((v) => v.id === q)) view = q as View;
    void poll();
    void loadLogs();
    void loadAnalytics();
    void api("/api/prices")
      .then((v) => {
        if (alive) catalog = parseCatalog(v);
      })
      .catch(() => {});
    let pending = false;
    const timer = setInterval(async () => {
      if (pending) return;
      pending = true;
      await poll();
      pending = false;
    }, 2500);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  });
  $effect(() => {
    query;
    errorsOnly;
    offset;
    const timer = setTimeout(() => {
      if (loaded) void loadLogs();
    }, 200);
    return () => clearTimeout(timer);
  });
  $effect(() => {
    range;
    if (loaded) void loadAnalytics();
  });
  function navigate(v: View) {
    view = v;
    history.replaceState(null, "", `?view=${v}`);
    selected = null;
    error = "";
    if (v === "logs") void loadLogs();
    if (v === "analytics") void loadAnalytics();
  }
  async function configure(
    enabled: boolean,
    accounts = enrolled.map((a) => a.id),
    priorities = Object.fromEntries(
      routing.accounts.map((a) => [a.id, a.priority]),
    ),
  ) {
    if (readOnly) return;
    busy = true;
    error = "";
    try {
      routing = await api("/api/routing/codex", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled, accounts, priorities }),
      });
      notice = "Routing preferences saved";
      setTimeout(() => (notice = ""), 2800);
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busy = false;
    }
  }
  function priority(a: Account, value: number) {
    const p = Object.fromEntries(
      routing.accounts.map((a) => [a.id, a.priority]),
    );
    p[a.id] = value;
    void configure(
      routing.enabled,
      enrolled.map((a) => a.id),
      p,
    );
  }
  function moveGroup(group: number, delta: number) {
    const other = groups[groups.indexOf(group) + delta];
    if (other === undefined) return;
    const p = Object.fromEntries(
      routing.accounts.map((a) => [
        a.id,
        a.priority === group
          ? other
          : a.priority === other
            ? group
            : a.priority,
      ]),
    );
    void configure(
      routing.enabled,
      enrolled.map((a) => a.id),
      p,
    );
  }
  function toggleAccount(a: Account) {
    const ids = a.enrolled
      ? enrolled.filter((x) => x.id !== a.id).map((x) => x.id)
      : [...enrolled.map((x) => x.id), a.id];
    void configure(ids.length ? routing.enabled : false, ids);
  }
  async function refresh() {
    if (readOnly) return;
    refreshing = true;
    error = "";
    try {
      await api("/api/limits/refresh", { method: "POST" });
      await poll();
      notice = "Scanning logins and refreshing usage";
      setTimeout(() => (notice = ""), 3500);
    } catch (e) {
      error = (e as Error).message;
    } finally {
      refreshing = false;
    }
  }
  let copyError = $state("");
  async function copy(text: string, id: string) {
    copyError = "";
    try {
      try {
        await navigator.clipboard.writeText(text);
      } catch {
        // Plain HTTP remote dashboards may not have the Clipboard API.
        const previous = document.activeElement as HTMLElement | null;
        const field = document.createElement("textarea");
        field.value = text;
        field.style.cssText = "position:fixed;left:-9999px;top:0";
        document.body.appendChild(field);
        try { field.select(); if (!document.execCommand("copy")) throw new Error("copy failed"); }
        finally { field.remove(); previous?.focus(); }
      }
      copied = id;
      setTimeout(() => (copied = ""), 2000);
    } catch {
      copyError = "Clipboard unavailable. Select the text and copy it manually.";
      error = copyError;
    }
  }
  async function systemLogs() {
    logMode = "system";
    try {
      const data = await api(`/api/events?limit=100&offset=${eventOffset}`);
      events = data.events;
      eventTotal = data.total;
    } catch (e) {
      error = (e as Error).message;
    }
  }
  function sessionLogs(s: Session) {
    query = s.session_id || s.account_id;
    logMode = "requests";
    errorsOnly = false;
    offset = 0;
    navigate("logs");
  }
  function accountName(id: string) {
    return routing.accounts.find((a) => a.id === id)?.email || id;
  }
  function openDialog(node: HTMLDialogElement) {
    const previous = document.activeElement as HTMLElement;
    node.showModal();
    return {
      destroy() {
        node.close();
        previous?.focus();
      },
    };
  }
  function time(t: number) {
    return t ? new Date(t).toLocaleTimeString("en-US", { hour12: false }) : "—";
  }
  function day(t: number) {
    return new Date(t).toLocaleDateString("en-US", {
      month: "short",
      day: "numeric",
    });
  }
</script>

<svelte:head
  ><title>Garcon</title><meta
    name="description"
    content="One local home for Codex accounts, sessions, and usage."
  /></svelte:head
>
<svelte:window
  onkeydown={(e) => {
    if (e.key === "Escape") {
      modal = null;
      selected = null;
    }
  }}
/>

<div class="console">
  <aside class="rail">
    <a
      class="brand"
      href="/?view=accounts"
      onclick={(e) => {
        e.preventDefault();
        navigate("accounts");
      }}
      aria-label="Garcon home"
      ><span class="brand-mark">g<span>•</span></span><span>Garcon</span></a
    >
    <div class="workspace">
      <span class="workspace-symbol"><Icon name="terminal" size={16} /></span>
      <div><strong>Local workspace</strong><small>This computer</small></div>
      <span class="small-dot"></span>
    </div>
    <div class="nav-label">WORKSPACE</div>
    <nav aria-label="Main navigation">
      {#each views as item}<button
          class:current={view === item.id}
          aria-label={item.label}
          onclick={() => navigate(item.id)}
          aria-current={view === item.id ? "page" : undefined}
          ><Icon name={item.id} /><span>{item.label}</span
          >{#if item.id === "accounts"}<small
              >{routing.accounts.length.toString().padStart(2, "0")}</small
            >{:else if item.id === "sessions" && active > 0}<small
              class="active-count">{active}</small
            >{/if}</button
        >{/each}
    </nav>
    <div class="rail-bottom">
      <div class="local-note">
        <Icon name="database" size={17} /><span
          >Yours. On this machine.<small>One app. One local database.</small
          ></span
        >
      </div>
      <button class="connect-button" onclick={() => (modal = "connect")}
        ><Icon name="link" size={16} /> Connect Codex <Icon
          name="arrow"
          size={15}
        /></button
      >
      <div class="rail-meta">
        <span>GARCON / LOCAL</span><span>v{config.version || "dev"}</span>
      </div>
    </div>
  </aside>
  <div class="workspace-main">
    <header class="topbar">
      <div class="breadcrumb">
        Workspace <span>/</span><strong>{title.label}</strong>
      </div>
      <div class="connection">
        <span class:offline={!connected} class="status-dot"></span>{connected
          ? "Local server connected"
          : loaded
            ? "Reconnecting…"
            : "Connecting…"}<span class="port"
          >:{config.listen.split(":").at(-1)}</span
        >
      </div>
    </header>
    <main>
      {#if configReady && config.remote_read_only}<div class="remote-notice" role="status">
        <strong>Remote mode: view-only</strong>
        <span>Run changes in a terminal on the Garcon host. All dashboard writes are disabled, including from localhost.</span>
        <code>garcon accounts refresh</code>
        <code>garcon routing set --json-stdin</code>
        <code>garcon providers add NAME HTTPS_ORIGIN</code>
      </div>{/if}
      <div class="page-heading">
        <div>
          <div class="eyebrow">YOUR LOCAL ROUTING DESK</div>
          <h1>{view === "accounts" ? "Account selection" : title.label}</h1>
          <p>{title.description}</p>
        </div>
        <div class="heading-actions">
          {#if view === "accounts"}{#if !readOnly}<button
              class="button secondary"
              disabled={refreshing}
              onclick={refresh}
              ><Icon name="refresh" size={16} />{refreshing
                ? "Refreshing…"
                : "Refresh"}</button
            >{/if}<button class="button primary" onclick={() => (modal = "account")}
              ><Icon name="plus" size={17} />Add provider</button
            >{:else if view === "analytics"}<label class="range-label"
              ><Icon name="clock" size={16} /><select
                aria-label="Analytics period"
                bind:value={range}
                ><option value="1">Last 24 hours</option><option value="7"
                  >Last 7 days</option
                ><option value="30">Last 30 days</option><option value="all"
                  >All time</option
                ></select
              ></label
            >{/if}
        </div>
      </div>
      {#if error || routing.error}<div class="alert" role="alert">
          <Icon name="pulse" />{error || routing.error}<button
            onclick={() => (error = "")}
            aria-label="Dismiss error"><Icon name="close" size={16} /></button
          >
        </div>{/if}
      {#if !connected && loaded}<div class="alert" role="status">
          The local server is unavailable. Showing the last received data;
          reconnecting automatically.
        </div>{/if}
      {#if !loaded}<div class="loading-state">
          <span class="loading-line"></span>Loading…
        </div>
      {:else if view === "accounts"}
        <section class="routing-overview" aria-label="Routing overview">
          <div class="flow-intro">
            <span class="mini-label"
              ><span class="status-dot" class:offline={!routing.enabled}
              ></span>{routing.enabled
                ? "Automatic routing enabled"
                : "Automatic routing disabled"}</span
            >
            <span class="routing-next"
              >Next account <strong>{next?.email ?? "None available"}</strong
              ></span
            >
          </div>
        </section>
        <div class="section-label">
          <span>ACCOUNT POOL</span><span
            >{enrolled.length} enrolled <span class="separator">/</span>
            {routing.accounts.length} discovered</span
          >
        </div>
        <section class="provider-card">
          <div class="provider-header">
            <button
              class="provider-title"
              onclick={() => (expanded = !expanded)}
              aria-expanded={expanded}
              ><span class="provider-logo"><CodexLogo size={32} /></span><span
                ><strong>Codex</strong><small>OpenAI OAuth accounts</small
                ></span
              ><span class="provider-chevron" class:open={expanded}
                ><Icon name="chevron" size={16} /></span
              ></button
            >
            <div class="provider-actions">
              <span class="provider-summary"
                >{ready.length} ready <span>·</span> {active} in flight</span
              >{#if readOnly}<code title="Run locally: garcon routing set --json-stdin">Auto-route: {routing.enabled ? "on" : "off"}</code>{:else}<span class="switch-label">Auto-route</span><button
                class="switch"
                class:on={routing.enabled}
                role="switch"
                aria-checked={routing.enabled}
                aria-label="Automatic Codex account routing"
                disabled={busy || !routing.accounts.length}
                onclick={() =>
                  configure(
                    !routing.enabled,
                    enrolled.length
                      ? enrolled.map((a) => a.id)
                      : routing.accounts.map((a) => a.id),
                  )}><span></span></button
              >{/if}
            </div>
          </div>
          {#if expanded}<div class="provider-body">
              {#if !routing.accounts.length}<div class="empty-state">
                  <span class="empty-icon"
                    ><Icon name="accounts" size={28} /></span
                  >
                  <h3>No accounts found</h3>
                  <p>
                    Sign in to Codex with a local profile. Garcon will discover
                    it automatically.
                  </p>
                  <button
                    class="button primary"
                    onclick={() => (modal = "account")}
                    >Add a Codex account <Icon name="arrow" size={16} /></button
                  >
                </div>{/if}
              {#each groups as group, gi (group)}<div class="priority-group">
                  <div class="group-heading">
                    <div>
                      <span class="group-index"
                        >{String(gi + 1).padStart(2, "0")}</span
                      ><strong>Priority {group}</strong><span
                        class="group-description"
                        >{gi === 0 ? "First choice" : "Fallback group"}</span
                      >
                    </div>
                    <div class="group-controls">
                      <span>Highest score wins</span>{#if !readOnly}<button
                        aria-label={`Move priority ${group} up`}
                        disabled={busy || gi === 0}
                        onclick={() => moveGroup(group, -1)}
                        ><Icon name="up" size={14} /></button
                      ><button
                        aria-label={`Move priority ${group} down`}
                        disabled={busy || gi === groups.length - 1}
                        onclick={() => moveGroup(group, 1)}
                        ><Icon name="down" size={14} /></button
                      >{/if}
                    </div>
                  </div>
                  <div class="account-columns">
                    <span>ACCOUNT</span><span>AVAILABLE USAGE</span><span
                      >ROUTING SCORE</span
                    ><span>PRIORITY</span><span>IN POOL</span>
                  </div>
                  {#each routing.accounts.filter((a) => a.priority === group) as a (a.id)}<div
                      class="account-row"
                      class:excluded={!a.enrolled}
                    >
                      <div class="account-identity">
                        <span class="account-avatar tone-{tone(a.email)}"
                          >{initials(a.email)}</span
                        >
                        <div>
                          <div class="account-name">
                            {a.email ||
                              "Missing local login"}{#if next?.id === a.id}<span
                                class="next-badge">UP NEXT</span
                              >{/if}
                          </div>
                          <div class="account-sub">
                            <span class="plan">{a.plan || "Codex"}</span><span
                              >·</span
                            ><span class:healthy={a.status === "Ready"}
                              >{a.status === "Ready"
                                ? "Ready to route"
                                : a.status}</span
                            >
                          </div>
                          <span class="profile-name"
                            >{a.profile || "Profile unavailable"}</span
                          >
                        </div>
                      </div>
                      <div class="account-usage">
                        {#if a.remaining_percent !== null}<div
                            class="usage-top"
                          >
                            <strong
                              >{a.remaining_percent.toFixed(0)}<small
                                >% left</small
                              ></strong
                            ><span>{duration(a.hours_left)} to reset</span>
                          </div>
                          <div class="meter">
                            <span
                              class:low={a.remaining_percent < 15}
                              style:width={`${a.remaining_percent}%`}
                            ></span>
                          </div>
                          {#if a.windows.filter( (w) => w.id.startsWith("codex:") ).length > 1}<div
                              class="quota-details"
                            >
                              {#each a.windows.filter( (w) => w.id.startsWith("codex:") ) as w}<span
                                  title={`Resets ${new Date(w.resets_at).toLocaleString()}`}
                                  >{w.label}: {w.used_percent === null
                                    ? "—"
                                    : (100 - w.used_percent).toFixed(0)}% left</span
                                >{/each}
                            </div>{/if}{:else}<span class="muted"
                            >Usage unavailable</span
                          >
                          <div class="meter"></div>{/if}
                      </div>
                      <div class="score">
                        <strong
                          >{a.score === null ? "—" : a.score.toFixed(2)}</strong
                        ><small>% / hour</small>
                      </div>
                      <div>
                        {#if readOnly}<span title="Run locally: garcon routing set --json-stdin">P{a.priority}</span>{:else}<select
                          class="priority-select"
                          aria-label={`Priority for ${a.email}`}
                          value={a.priority}
                          disabled={busy}
                          onchange={(e) =>
                            priority(a, Number(e.currentTarget.value))}
                          >{#each [...new Set( [...Array.from({ length: Math.max(3, routing.accounts.length + 1) }, (_, i) => i + 1), ...groups] )] as n}<option
                              value={n}>P{n}</option
                            >{/each}</select
                        >{/if}
                      </div>
                      <div>
                        {#if readOnly}<span title="Run locally: garcon routing set --json-stdin">{a.enrolled ? "Enrolled" : "Excluded"}</span>{:else}<button
                          class="pool-check"
                          class:checked={a.enrolled}
                          aria-label={`${a.enrolled ? "Remove" : "Enroll"} ${a.email}`}
                          aria-pressed={a.enrolled}
                          disabled={busy}
                          onclick={() => toggleAccount(a)}
                          >{#if a.enrolled}<Icon
                              name="check"
                              size={13}
                            />{:else}<Icon name="plus" size={13} />{/if}</button
                        >{/if}
                      </div>
                    </div>{/each}
                </div>{/each}
              <button class="add-inline" onclick={() => (modal = "account")}
                ><Icon name="plus" size={16} /> Add another Codex account
                <span>Separate login. Same endpoint.</span></button
              >
            </div>{/if}
        </section>
        <section class="remote-notice" aria-label="Other providers and local commands">
          <strong>Providers</strong>
          {#each Object.entries(providerURLs) as [name, url]}<span>{name} · {url}</span>{/each}
          {#each inventory.accounts.filter(a => a.provider !== "codex") as a}<span>{a.provider} · {a.email || a.id} · {a.status}</span>{/each}
          <span>OpenRouter key: {inventory.openrouter_configured ? "saved (not verified)" : "not configured"}</span>
          <code>garcon providers add NAME HTTPS_ORIGIN</code>
          <code>garcon providers remove NAME</code>
          {#if readOnly}<code>garcon accounts refresh</code><code>garcon routing set --json-stdin</code>{/if}
          <button class="button secondary" onclick={() => (modal = "account")}>Add provider · copy setup skill</button>
        </section>
        <div class="routing-notes">
          <div>
            <span class="note-icon"><Icon name="analytics" /></span>
            <div>
              <h3>Routing order</h3>
              <p>
                Remaining quota ÷ hours until reset. Highest score wins within a
                priority group; the most constrained window sets the score.
              </p>
            </div>
          </div>
          <div>
            <span class="note-icon"><Icon name="link" /></span>
            <div>
              <h3>Session assignments</h3>
              <p>
                Priority changes apply to new sessions. Existing sessions keep
                their account, even after a restart.
              </p>
            </div>
          </div>
        </div>
      {:else if view === "sessions"}
        <div class="summary-strip">
          <div>
            <span>TRACKED SESSIONS</span><strong>{sessions.length}</strong>
          </div>
          <div>
            <span>SESSIONS IN FLIGHT</span><strong
              >{sessions.filter((s) => s.active > 0).length}<i class="tiny-live"
              ></i></strong
            >
          </div>
          <div>
            <span>ACCOUNTS IN USE</span><strong
              >{new Set(sessions.map((s) => s.account_id)).size}</strong
            >
          </div>
          <div class="summary-explanation">
            <Icon name="link" />
            <p>
              Matched to your local Codex tasks.<br />Active model requests appear first.
            </p>
          </div>
        </div>
        <section class="data-panel">
          <div class="panel-toolbar">
            <label class="search-box"
              ><Icon name="search" size={17} /><input
                placeholder="Find a task, project, account, or session ID…"
                aria-label="Search sessions"
                bind:value={sessionQuery}
              /></label
            ><button
              class="filter-button"
              class:selected={activeOnly}
              onclick={() => (activeOnly = !activeOnly)}
              ><span class="status-dot"></span>In flight only</button
            >
          </div>
          <div class="table-scroll">
            <table class="session-table">
              <thead
                ><tr
                  ><th>CODEX TASK</th><th>ACTIVITY</th><th>ROUTED ACCOUNT</th><th>MODEL</th><th
                    >REQUESTS</th
                  ><th>LAST SEEN</th><th></th></tr
                ></thead
              ><tbody
                >{#each filteredSessions as s (s.key)}<tr
                    ><td
                      ><div class="session-id">
                        <span class="session-symbol" class:live={s.active > 0}
                          ><Icon
                            name={s.active > 0 ? "pulse" : "sessions"}
                            size={16}
                          /></span
                        >
                        <div>
                          <button class="task-title" title={s.task?.title || s.session_id || s.key}
                            disabled={!s.session_id} onclick={() => sessionLogs(s)}
                            >{s.task?.title || (s.session_id ? taskName(s.session_id) : `Legacy session ${s.key.slice(0, 8)}`)}</button>
                          <small title={s.task?.cwd}>{projectName(s)}{s.task?.archived ? " · Archived" : ""}</small>
                          <small class="session-reference"><span title={s.session_id || s.key}>{s.session_id ? `${s.session_id.slice(0, 8)}…${s.session_id.slice(-6)}` : "ID available when resumed"}</span>
                            {#if s.session_id}<button class="row-action" title="Copy full session ID" aria-label={copied === s.key ? "Session ID copied" : `Copy session ID for ${taskName(s.session_id)}`} onclick={() => copy(s.session_id, s.key)}><Icon name={copied === s.key ? "check" : "copy"} size={12}/></button>{/if}
                            {#if !s.task}<span> · Title unavailable</span>{/if}
                          </small>
                        </div>
                      </div></td
                    ><td class="session-activity">
                        <span class="status-label" class:streaming={s.active > 0} class:bad={s.active === 0 && (s.last_state === "failed" || s.last_state === "interrupted" || s.last_status >= 400)} title={s.last_error || undefined}><span></span>{sessionActivity(s)}</span>
                        <small>{s.active > 0 ? `${s.active} in flight · ${elapsed(s.active_since)}` : `Last seen ${ago(s.last_seen, now)}`}</small>
                      </td
                    ><td
                      ><div class="table-account">
                        <span
                          class="avatar-dot tone-{tone(
                            s.account || accountName(s.account_id),
                          )}"
                        ></span>{s.account || accountName(s.account_id)}
                      </div></td
                    ><td><code>{s.model || "—"}</code></td><td class="numeric"
                      >{s.requests}</td
                    ><td class="muted">{ago(s.last_seen, now)}</td><td
                      ><button
                        class="row-action"
                        aria-label={`View requests for ${s.session_id || s.key}`}
                        disabled={!s.session_id}
                        onclick={() => sessionLogs(s)}
                        ><Icon name="arrow" size={17} /></button
                      ></td
                    ></tr
                  >{/each}</tbody
              >
            </table>
          </div>
          {#if !filteredSessions.length}<div class="empty-state">
              <Icon name="sessions" size={30} />
              <h3>
                {sessionQuery ? "No matching sessions." : "No sessions yet"}
              </h3>
              <p>
                {sessionQuery
                  ? "Try another task title, project, account, or session identifier."
                  : "Start a Codex session through Garcon to see its account assignment here."}
              </p>
            </div>{/if}
          <div class="panel-footer">
            <span>{filteredSessions.length} {filteredSessions.length === 1 ? "session" : "sessions"}</span>
            <span>Activity reflects model traffic. Tasks may run tools between requests.</span>
            {#if sessions.length === 1000}<span>Showing up to 1,000 assignments, in-flight first</span>{/if}
          </div>
        </section>
      {:else if view === "logs"}
        <section class="data-panel">
          <div class="panel-toolbar">
            <div class="segmented">
              <button
                class:selected={logMode === "requests"}
                onclick={() => (logMode = "requests")}>Requests</button
              ><button
                class:selected={logMode === "system"}
                onclick={systemLogs}>System</button
              >
            </div>
            {#if logMode === "requests"}<label class="search-box log-search"
                ><Icon name="search" size={17} /><input
                  placeholder="Search account, model, session, path…"
                  aria-label="Search request logs"
                  value={query}
                  oninput={(e) => {
                    query = e.currentTarget.value;
                    offset = 0;
                  }}
                /></label
              ><button
                class="filter-button"
                class:selected={errorsOnly}
                onclick={() => {
                  errorsOnly = !errorsOnly;
                  offset = 0;
                }}>Errors only</button
              >{:else}<button class="button secondary" onclick={systemLogs}
                ><Icon name="refresh" size={15} />Refresh</button
              >{/if}
          </div>
          {#if logMode === "requests"}<div class="table-scroll">
              <table class="log-table">
                <thead
                  ><tr
                    ><th>TIME</th><th>STATUS</th><th>MODEL / REQUEST</th><th
                      >ACCOUNT</th
                    ><th>TOKENS</th><th>DURATION</th><th></th></tr
                  ></thead
                ><tbody
                  >{#each logs.requests as r (r.sequence)}<tr
                      class:failed={r.status >= 400 ||
                        r.state === "interrupted" ||
                        r.state === "failed"}
                      ><td
                        ><span class="mono">{time(r.time)}</span><small
                          >{day(r.time)}</small
                        ></td
                      ><td
                        ><span
                          class="status-label"
                          class:bad={r.status >= 400 ||
                            r.state === "interrupted" ||
                            r.state === "failed"}
                          class:streaming={r.state === "streaming"}
                          ><span></span>{r.state === "streaming"
                            ? "Streaming"
                            : r.state === "interrupted"
                              ? "Interrupted"
                              : r.state === "failed" && r.status < 400
                                ? "Failed"
                                : r.status || "—"}</span
                        ></td
                      ><td
                        ><strong class="model-name"
                          >{r.model || r.path || "Unknown model"}</strong
                        ><small
                          >{r.method || "POST"} · {r.kind === "request"
                            ? "Metadata request"
                            : r.session_id
                              ? taskName(r.session_id)
                              : "Completion"}</small
                        ></td
                      ><td class="log-account">{r.account || "Unassigned"}</td
                      ><td class="numeric">{count(tokens(r))}</td><td
                        class="numeric muted"
                        >{r.state === "streaming"
                          ? "In flight"
                          : `${(r.ms / 1000).toFixed(2)}s`}</td
                      ><td
                        ><button
                          class="row-action"
                          aria-label={`Inspect request ${r.sequence}`}
                          onclick={() => (selected = r)}
                          ><Icon name="chevron" size={16} /></button
                        ></td
                      ></tr
                    >{/each}</tbody
                >
              </table>
            </div>
            {#if !logs.requests.length}<div class="empty-state">
                <Icon name="logs" size={30} />
                <h3>
                  {query || errorsOnly
                    ? "No matching requests."
                    : "No requests yet"}
                </h3>
                <p>
                  {query || errorsOnly
                    ? "Adjust your search or filters."
                    : "Requests will appear here as they pass through Garcon."}
                </p>
              </div>{/if}
            <div class="panel-footer">
              <span
                >{logs.total
                  ? `${offset + 1}–${Math.min(offset + 50, logs.total)} of ${count(logs.total)} requests`
                  : "0 requests"}</span
              >
              <div class="pagination">
                <button
                  disabled={offset === 0}
                  onclick={() => (offset = Math.max(0, offset - 50))}
                  >Previous</button
                ><button
                  disabled={offset + 50 >= logs.total}
                  onclick={() => (offset += 50)}
                  >Next <Icon name="chevron" size={13} /></button
                >
              </div>
            </div>
          {:else}<div class="system-log">
              {#each events as event}<div>
                  <time>{day(event.time)} {time(event.time)}</time><span
                    >{event.message}</span
                  >
                </div>{/each}{#if !events.length}<div class="empty-state">
                  No system events recorded yet.
                </div>{/if}
            </div>
            <div class="panel-footer">
              <span
                >{eventTotal
                  ? `${eventOffset + 1}–${Math.min(eventOffset + 100, eventTotal)} of ${eventTotal} events`
                  : "0 events"}</span
              >
              <div class="pagination">
                <button
                  disabled={eventOffset === 0}
                  onclick={() => {
                    eventOffset = Math.max(0, eventOffset - 100);
                    void systemLogs();
                  }}>Previous</button
                ><button
                  disabled={eventOffset + 100 >= eventTotal}
                  onclick={() => {
                    eventOffset += 100;
                    void systemLogs();
                  }}>Next <Icon name="chevron" size={13} /></button
                >
              </div>
            </div>{/if}
        </section>
        <p class="footnote">
          <Icon name="shield" size={14} /> Full request metadata is stored locally,
          including failures. Prompts, response bodies, and credentials are never
          saved.
        </p>
      {:else if view === "analytics"}
        <div class="metric-tabs" role="tablist" aria-label="Analytics metric">
          {#each [{ id: "usage", label: "Usage" }, { id: "cost", label: "Cost" }, { id: "models", label: "Models" }] as m}<button
              role="tab"
              aria-selected={metric === m.id}
              class:selected={metric === m.id}
              onclick={() => (metric = m.id as typeof metric)}>{m.label}</button
            >{/each}
        </div>
        <div class="analytics-stats">
          <div>
            <span>TOTAL TOKENS</span><strong>{count(totalTokens)}</strong><small
              >Input, cached input & output</small
            >
          </div>
          <div>
            <span>API EQUIVALENT COST</span><strong>{usd(estimatedCost)}</strong
            ><small
              >{unpriced
                ? `${unpriced} requests without a known rate`
                : "Estimated at model list prices"}</small
            >
          </div>
          <div>
            <span>REQUESTS</span><strong>{count(totalRequests)}</strong><small
              >{totalErrors} failed or interrupted</small
            >
          </div>
          <div>
            <span>CACHE HIT RATE</span><strong
              >{inputTokens
                ? ((100 * totalCache) / inputTokens).toFixed(1)
                : "0"}<em>%</em></strong
            ><small>Share of input served from cache</small>
          </div>
        </div>
        <section class="chart-panel">
          <div class="chart-heading">
            <div>
              <h2>
                {metric === "cost"
                  ? "Estimated cost"
                  : metric === "models"
                    ? "Model requests"
                    : "Token usage"}
              </h2>
              <p>
                {metric === "cost"
                  ? "Estimated API equivalent, not your subscription bill."
                  : metric === "models"
                    ? "Completed and failed requests across your models."
                    : "Tokens processed through Garcon."}
              </p>
            </div>
            <span class="chart-legend"
              ><i></i>{metric === "cost"
                ? "USD estimate"
                : metric === "models"
                  ? "Requests"
                  : "Tokens"}</span
            >
          </div>
          <div
            class="bar-chart"
            role="img"
            aria-label={`${metric} over selected time range`}
          >
            <div class="chart-scale">
              <span>{metric === "cost" ? usd(peak) : count(peak)}</span><span
                >{metric === "cost" ? usd(peak / 2) : count(peak / 2)}</span
              ><span>0</span>
            </div>
            <div class="chart-plot">
              <div class="chart-grid"><i></i><i></i><i></i></div>
              <div class="chart-bars">
                {#each bars as b}<div
                    class="bar-column"
                    title={`${new Date(b.time).toLocaleString()}: ${metric === "cost" ? usd(b.value) : count(b.value)}`}
                  >
                    <span
                      style:height={`${Math.max(b.value ? 1 : 0, (b.value / peak) * 100)}%`}
                    ></span>
                  </div>{/each}
              </div>
              <div class="chart-axis">
                <span
                  >{bars[0]
                    ? range === "1"
                      ? time(bars[0].time)
                      : day(bars[0].time)
                    : ""}</span
                ><span
                  >{range === "1"
                    ? "Hourly"
                    : range === "all"
                      ? "Recent 90 days · daily"
                      : "Daily"} volume</span
                ><span>Now</span>
              </div>
              {#if totalRequests === 0}<div class="chart-empty">
                  No requests in this period.
                </div>{/if}
            </div>
          </div>
        </section>
        <div class="analytics-lower">
          <section class="data-panel">
            <div class="panel-title">
              <h2>{metric === "cost" ? "Cost by model" : "Model mix"}</h2>
              <span>{models.length} models</span>
            </div>
            {#each models as m, i}<div class="rank-row">
                <span class="rank-number">{String(i + 1).padStart(2, "0")}</span
                >
                <div class="rank-main">
                  <strong>{m.model}</strong>
                  <div class="rank-track">
                    <span
                      style:width={`${metric === "cost" ? (estimatedCost ? ((cost(m, catalog) ?? 0) / estimatedCost) * 100 : 0) : totalTokens ? (tokens(m) / totalTokens) * 100 : 0}%`}
                    ></span>
                  </div>
                </div>
                <div class="rank-value">
                  <strong
                    >{metric === "cost"
                      ? cost(m, catalog) === null
                        ? "Unpriced"
                        : usd(cost(m, catalog)!)
                      : count(tokens(m))}</strong
                  ><small>{count(m.requests)} requests</small>
                </div>
              </div>{/each}{#if !models.length}<div class="small-empty">
                Model usage appears after your first request.
              </div>{/if}
          </section>
          <section class="data-panel">
            <div class="panel-title">
              <h2>Across your accounts</h2>
              <span>Token share</span>
            </div>
            {#each accountTotals as a}<div class="rank-row">
                <span class="account-avatar small tone-{tone(a.account)}"
                  >{initials(a.account)}</span
                >
                <div class="rank-main">
                  <strong>{a.account}</strong>
                  <div class="rank-track">
                    <span
                      style:width={`${totalTokens ? (tokens(a) / totalTokens) * 100 : 0}%`}
                    ></span>
                  </div>
                </div>
                <div class="rank-value">
                  <strong>{count(tokens(a))}</strong><small
                    >{totalTokens
                      ? ((tokens(a) / totalTokens) * 100).toFixed(1)
                      : 0}% of usage</small
                  >
                </div>
              </div>{/each}{#if !accountTotals.length}<div class="small-empty">
                Account usage appears after your first request.
              </div>{/if}
          </section>
        </div>
        <p class="footnote">
          <Icon name="database" size={14} /> Only requests observed by Garcon are
          counted. {metric === "cost"
            ? `Rates: ${catalog?.fetched_at ? `OpenRouter catalog, refreshed ${day(catalog.fetched_at)}` : "bundled fallback list"}. Missing prices are excluded; this is not billed subscription cost.`
            : "Historical usage is preserved in your local SQLite database."}
        </p>
      {/if}
      <div class="page-bottom">
        <span
          ><Icon name="database" size={13} /> SQLite · local persistence</span
        ><span title={config.data}
          >{config.data
            ? config.data.replace(/^.*\/garcon\//, "~/…/garcon/")
            : "All data stays on this machine"}</span
        >
      </div>
    </main>
  </div>
  <footer class="ticker" aria-label="Live request activity">
    <div class="ticker-brand">
      <span class="status-dot" class:offline={!connected}></span>LIVE ROUTES
    </div>
    <div class="ticker-window">
      <div class="ticker-track" class:paused={tickerPaused}>
        {#if feed.length}{#each [0, 1] as duplicate}<div
              class="ticker-copy"
              aria-hidden={duplicate === 1}
            >
              {#each feed.slice(0, 12) as r}<button
                  class="ticker-item"
                  title={taskName(r.session_id)}
                  tabindex={duplicate === 1 ? -1 : 0}
                  onclick={() => (selected = r)}
                  ><span
                    class="ticker-status"
                    class:bad={r.status >= 400 ||
                      r.state === "interrupted" ||
                      r.state === "failed"}
                    >{r.state === "streaming"
                      ? "●"
                      : r.status >= 400 ||
                          r.state === "interrupted" ||
                          r.state === "failed"
                        ? "↘"
                        : "↗"}</span
                  >{#if r.session_id}<span class="ticker-task">{taskName(r.session_id)}</span>{/if}<strong>{r.model || "Codex request"}</strong><Icon
                    name="arrow"
                    size={12}
                  /><span>{r.account || "Unassigned"}</span><small
                    >{r.state === "streaming"
                      ? "IN FLIGHT"
                      : `${(r.ms / 1000).toFixed(1)}s`}</small
                  ><span class="ticker-divider">/</span></button
                >{/each}
            </div>{/each}{:else}<div class="ticker-waiting">
            Listening for requests <span>·</span> Codex → Garcon → your best available
            account
          </div>{/if}
      </div>
    </div>
    <button
      class="ticker-pause"
      aria-label={tickerPaused ? "Resume ticker" : "Pause ticker"}
      onclick={() => (tickerPaused = !tickerPaused)}
      >{tickerPaused ? "▶" : "Ⅱ"}</button
    >
    <div class="ticker-count">{active}<span>IN FLIGHT</span></div>
  </footer>
</div>
{#if notice}<div class="toast" role="status">
    <Icon name="check" size={16} />{notice}
  </div>{/if}
{#if modal || selected}<div
    class="overlay"
    role="presentation"
    onclick={(e) => {
      if (e.target === e.currentTarget) {
        modal = null;
        selected = null;
      }
    }}
  >
    <dialog
      class="drawer"
      use:openDialog
      oncancel={() => {
        modal = null;
        selected = null;
      }}
      aria-modal="true"
      aria-label={selected
        ? "Request details"
        : modal === "account"
          ? "Add provider"
          : "Connect Codex"}
      tabindex="-1"
    >
      <div class="drawer-top">
        <span class="eyebrow"
          >{selected ? "REQUEST INSPECTOR" : "LOCAL SETUP"}</span
        ><button
          class="icon-button"
          aria-label="Close panel"
          onclick={() => {
            modal = null;
            selected = null;
          }}><Icon name="close" /></button
        >
      </div>
      {#if selected}<h2>Request details</h2>
        {#if selected.session_id}<p class="request-task-name">{taskName(selected.session_id)}</p>
          {#if sessionIndex.get(selected.session_id)?.task?.cwd}<p class="drawer-fine">{sessionIndex.get(selected.session_id)?.task?.cwd}</p>{/if}
        {/if}
        <div class="detail-status">
          <span
            class="status-label"
            class:bad={selected.status >= 400 ||
              selected.state === "interrupted" ||
              selected.state === "failed"}
            >{selected.state || "complete"} · {selected.status ||
              "in flight"}</span
          ><span>{day(selected.time)} · {time(selected.time)}</span>
        </div>
        {#if selected.error}<div class="alert">{selected.error}</div>{/if}
        <dl class="request-details">
          {#each [["Model", selected.model || "Unknown"], ["Account", selected.account || "Unassigned"], ["Account ID", selected.account_id || "Not recorded"], ["Session", selected.session_id || "Not recorded"], ["Request ID", selected.request_id || `Legacy record ${selected.sequence}`], ["Endpoint", `${selected.method || "POST"} ${selected.path || "Not recorded"}`], ["Duration", `${(selected.ms / 1000).toFixed(3)}s`], ["First byte", selected.first_byte_ms === undefined ? "Not measured" : `${selected.first_byte_ms}ms`], ["Input tokens", count(selected.input)], ["Cached input", count(selected.cache_read)], ["Cache write", count(selected.cache_write)], ["Output tokens", count(selected.output)]] as [label, value]}<div
            >
              <dt>{label}</dt>
              <dd>{value}</dd>
            </div>{/each}
        </dl>
        <button
          class="button secondary full"
          onclick={() => copy(JSON.stringify(selected, null, 2), "request")}
          ><Icon
            name={copied === "request" ? "check" : "copy"}
            size={16}
          />{copied === "request" ? "Copied" : "Copy request metadata"}</button
        >
      {:else if modal === "account"}
        <h2>Add provider</h2>
        <p class="drawer-description">Copy this Markdown skill into Codex to add an account on the Garcon host. It works in local and remote mode.</p>
        <label class="profile-input">Provider <select aria-label="Account provider" bind:value={skillProvider}>
          <option value="codex">Codex</option><option value="claude">Claude</option><option value="openrouter">OpenRouter</option>
        </select></label>
        {#if copyError}<p role="status">{copyError}</p>{/if}
        <div class="code-box block skill-preview"><pre>{accountSkills[skillProvider]}</pre></div>
        <button class="button primary full" onclick={() => copy(accountSkills[skillProvider], "account-skill")}>
          <Icon name={copied === "account-skill" ? "check" : "copy"} size={16}/>{copied === "account-skill" ? "Copied" : "Copy skill"}
        </button>
        <p class="drawer-fine">Run on the server, using its data path: <code>{config.data || "default database"}</code>. Credentials stay out of the skill and chat.</p>
      {:else}<span class="drawer-art"><Icon name="link" size={36} /></span>
        <h2>Connect Codex</h2>
        <p class="drawer-description">
          Add this provider to <code>~/.codex/config.toml</code>. Keep your
          existing model and other preferences. Restart Codex Desktop or start a
          new CLI session.
        </p>
        <div class="code-box block">
          <pre>{connectConfig}</pre>
          <button
            aria-label="Copy Codex configuration"
            onclick={() => copy(connectConfig, "config")}
            ><Icon
              name={copied === "config" ? "check" : "copy"}
              size={16}
            /></button
          >
        </div>
        <div class="connection-note">
          <Icon name="shield" />
          <p>
            Garcon listens on <strong>127.0.0.1:4141</strong> in development and when
            running locally in the background.
          </p>
        </div>
        <p class="drawer-fine">
          Use one model_provider setting and one [model_providers.garcon] table.
          If you already use Garcon, your endpoint stays the same.
        </p>{/if}
    </dialog>
  </div>{/if}
