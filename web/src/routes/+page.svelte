<script lang="ts">
  import { onMount } from "svelte";
  import Icon from "$lib/console/Icon.svelte";
  import LiveTicker from "$lib/console/LiveTicker.svelte";
  import AccountTable from "$lib/console/AccountTable.svelte";
  import claudeLogo from "$lib/assets/providers/claude-app.png";
  import { claudeRows } from "$lib/console/claude";
  import { connectionPrompt } from "$lib/console/connection-prompts";
  import type { LimitsSnapshot } from "$lib/limits";
  import HarnessLogo from "$lib/console/HarnessLogo.svelte";
  import { harnessName, sessionKey } from "$lib/console/harness";
  import CodexLogo from "$lib/console/CodexLogo.svelte";
  import ProviderConnection from "$lib/console/ProviderConnection.svelte";
  import { parseCatalog, type Catalog } from "$lib/pricing";
  import {
    count,
    usd,
    tokens,
    cost,
    ago,
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
  let limits = $state<LimitsSnapshot>({ accounts: [], refreshing: false, updated_at: 0 });
  let limitsAvailable = $state(false);
  let claudeExpanded = $state(true);
  let routing = $state<Routing>({ enabled: false, accounts: [] });
  let claudeRouting = $state<Routing | null>(null);
  let connections = $state<{ client: string; connected: boolean | null; mode?: string; profiles?: number; remote_control_at_startup?: boolean }[]>([]);
  let connectionsAvailable = $state(false);
  let sessions = $state<Session[]>([]),
    aggregates = $state<Aggregate[]>([]),
    feed = $state<RequestRow[] | null>(null);
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
  });
  let loaded = $state(false),
    connected = $state(false),
    busy = $state(false),
    refreshing = $state(false),
    expanded = $state(true);
  let error = $state(""),
    notice = $state(""),
    now = $state(Date.now());
  let modal = $state<"account" | "connect" | "claude" | "claude-account" | "add" | null>(null),
    profile = $state("new-account"),
    copied = $state("");
  let query = $state(""),
    sessionQuery = $state(""),
    logSession = $state<{ id: string; harness: string } | null>(null),
    errorsOnly = $state(false),
    hideZeroTokens = $state(true),
    logMode = $state<"requests" | "system">("requests"),
    offset = $state(0),
    range = $state("7"),
    metric = $state<"usage" | "cost" | "models">("usage");
  let selected = $state<RequestRow | null>(null),
    activeOnly = $state(false);
  let logGeneration = 0,
    analyticsGeneration = 0,
    alive = true;
  const views: { id: View; label: string; description: string }[] = [
    {
      id: "accounts",
      label: "Accounts",
      description: "Manage the accounts available to Codex and Claude Code.",
    },
    {
      id: "sessions",
      label: "Sessions",
      description: "Sessions from Codex, Claude Code, and other clients, all in one place.",
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
  const claudeAccounts = $derived(claudeRouting?.accounts ?? claudeRows(limits.accounts, now));
  const claudeNext = $derived(claudeAccounts.find((a) => a.enrolled && a.status === "Ready"));
  const title = $derived(views.find((v) => v.id === view)!);
  const enrolled = $derived(routing.accounts.filter((a) => a.enrolled));
  const active = $derived(
    sessions.reduce((n, s) => n + s.active, 0),
  );
  const ready = $derived(enrolled.filter((a) => a.status === "Ready"));
  const next = $derived(routing.next_account === undefined ? ready[0] : routing.accounts.find(a => a.id === routing.next_account));
  const filteredSessions = $derived(
    sessions.filter(
      (s) =>
        (!activeOnly || s.active > 0) &&
        `${harnessName(s.harness)} ${s.provider ?? ""} ${s.task?.title ?? ""} ${s.task?.cwd ?? ""} ${s.session_id} ${s.account} ${s.model} ${s.account_id}`
          .toLowerCase()
          .includes(sessionQuery.toLowerCase()),
    ),
  );
  const sessionIndex = $derived(new Map(sessions.map((s) => [sessionKey(s.session_id, s.harness), s])));
  function taskName(id?: string, harness?: string) {
    return id ? sessionIndex.get(sessionKey(id, harness))?.task?.title || `Session ${id.slice(0, 8)}…${id.slice(-6)}` : "Unassigned session";
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
  const command = $derived(
    `CODEX_HOME="$HOME/.codex-${/^[a-z0-9][a-z0-9-]{0,31}$/.test(profile) ? profile : "personal"}" codex -c 'cli_auth_credentials_store="file"' login`,
  );
  const addingClaude = $derived(modal === "claude-account");
  const accountProvider = $derived(addingClaude ? "Claude Code" : "Codex");
  const claudeCommand = $derived(`CLAUDE_CONFIG_DIR="$HOME/.claude-${/^[a-z0-9][a-z0-9-]{0,31}$/.test(profile) ? profile : "personal"}" claude`);
  const claudeLaunchCommand = $derived(`garcon claude --config-dir "$HOME/.claude-${/^[a-z0-9][a-z0-9-]{0,31}$/.test(profile) ? profile : "personal"}"`);
  const setupClient = $derived(modal === "claude" ? "claude" : "codex");
  const setupPrompt = $derived(connectionPrompt(setupClient));
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
        `/api/logs?limit=50&offset=${offset}&q=${encodeURIComponent(query)}&errors=${errorsOnly}&hide_zero_tokens=${hideZeroTokens}${logSession ? `&session_id=${encodeURIComponent(logSession.id)}&harness=${encodeURIComponent(logSession.harness)}` : ""}`,
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
    // A missing status endpoint must not stop the rest of the dashboard polling.
    const limitsPoll = api("/api/limits").then((result) => {
      if (alive) { limits = result; limitsAvailable = true; }
    }).catch(() => { if (alive) limitsAvailable = false; });
    const claudeRoutingPoll = api("/api/routing/claude").then((result) => {
      if (alive) claudeRouting = result;
    }).catch(() => { if (alive) claudeRouting = null; });
    const connectionPoll = api("/api/connections").then((result) => {
      if (alive) { connections = result; connectionsAvailable = true; }
    }).catch(() => { if (alive) connectionsAvailable = false; });
    try {
      const [r, s, f] = await Promise.all([
        api("/api/routing/codex"),
        api("/api/sessions"),
        api("/api/usage/recent"),
      ]);
      if (!alive) return;
      if (!busy) routing = r;
      // Older installed services expose Codex assignments without a harness field.
      sessions = s.map((session: Session) => ({ ...session, harness: session.harness ?? "codex" }));
      feed = f.requests;
      connected = true;
      loaded = true;
      now = Date.now();
      if (view === "logs") await loadLogs();
      if (view === "analytics") await loadAnalytics();
    } catch {
      if (alive) {
        connected = false;
        loaded = true;
      }
    }
    await Promise.all([connectionPoll, limitsPoll, claudeRoutingPoll]);
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
    void api("/api/config")
      .then((v) => {
        if (alive) config = v;
      })
      .catch(() => {});
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
    logSession;
    errorsOnly;
    hideZeroTokens;
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
    accounts = enrolled.map((a) => a.id),
  ) {
    busy = true;
    error = "";
    try {
      routing = await api("/api/routing/codex", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        // Keep enrollment edits compatible with installed releases that still
        // require enabled/priorities while the dashboard is being previewed.
        body: JSON.stringify({ enabled: true, accounts,
          priorities: Object.fromEntries(routing.accounts.map((a) => [a.id, 1])) }),
      });
      notice = "Routing preferences saved";
      setTimeout(() => (notice = ""), 2800);
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busy = false;
    }
  }
  function toggleAccount(a: Account) {
    const ids = a.enrolled
      ? enrolled.filter((x) => x.id !== a.id).map((x) => x.id)
      : [...enrolled.map((x) => x.id), a.id];
    void configure(ids);
  }
  async function refresh() {
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
  async function copy(text: string, id: string) {
    try {
      await navigator.clipboard.writeText(text);
      copied = id;
      setTimeout(() => (copied = ""), 2000);
    } catch {
      error = "Clipboard unavailable. Select and copy the text.";
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
    query = "";
    logSession = { id: s.session_id, harness: s.harness };
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
    content="One local home for AI accounts, sessions, and usage."
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
              >{(routing.accounts.length + claudeAccounts.length).toString().padStart(2, "0")}</small
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
      <div class="page-heading">
        <div>
          <div class="eyebrow">YOUR LOCAL ROUTING DESK</div>
          <h1>{view === "accounts" ? "Account selection" : title.label}</h1>
          <p>{title.description}</p>
        </div>
        <div class="heading-actions">
          {#if view === "accounts"}<button
              class="button secondary"
              disabled={refreshing}
              onclick={refresh}
              ><Icon name="refresh" size={16} />{refreshing
                ? "Refreshing…"
                : "Refresh"}</button
            ><button class="button primary" onclick={() => (modal = "add")}
              ><Icon name="plus" size={17} />Add account</button
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
      {#if error || routing.error || claudeRouting?.error}<div class="alert" role="alert">
          <Icon name="pulse" />{error || routing.error || claudeRouting?.error}<button
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
            <span class="routing-next"
              >Next Codex account <strong>{next?.email ?? "None available"}</strong
              ></span
            >
          </div>
        </section>
        <div class="section-label">
          <span>ACCOUNTS</span><span
            >{routing.accounts.length + claudeAccounts.length} discovered</span
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
              >
              <ProviderConnection
                provider="Codex"
                connection={connections.find((c) => c.client === "codex")}
                available={connected && connectionsAvailable}
                onclick={() => (modal = "connect")}
              />
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
              <AccountTable accounts={routing.accounts} pinnedId={routing.pinned_account} nextId={next?.id} {busy} ontoggle={toggleAccount} />
              <button class="add-inline" onclick={() => (modal = "account")}
                ><Icon name="plus" size={16} /> Add another Codex account
                <span>Separate login. Same endpoint.</span></button
              >
            </div>{/if}
        </section>
        <section class="provider-card" aria-label="Claude Code accounts">
          <div class="provider-header">
            <button class="provider-title" onclick={() => (claudeExpanded = !claudeExpanded)} aria-expanded={claudeExpanded} aria-controls="claude-accounts">
              <span class="provider-logo"><img src={claudeLogo} width="32" height="32" alt="" /></span>
              <span><strong>Claude Code</strong><small>Anthropic OAuth accounts</small></span>
              <span class="provider-chevron" class:open={claudeExpanded}><Icon name="chevron" size={16} /></span>
            </button>
            <div class="provider-actions">
              <span class="provider-summary">{claudeAccounts.length} accounts <span>·</span> {claudeRouting?.enabled ? "Session pinned" : "Local profiles"}</span>
              <ProviderConnection launchBased provider="Claude Code" connection={connections.find((c) => c.client === "claude")} available={connected && connectionsAvailable} onclick={() => (modal = "claude")} />
            </div>
          </div>
          {#if claudeExpanded}<div class="provider-body" id="claude-accounts">
            {#if !limitsAvailable}<p class="provider-explanation" role="status">Claude account data is unavailable. Retrying automatically.</p>{/if}
            {#if limitsAvailable && !claudeAccounts.length}<div class="empty-state">
              <span class="empty-icon"><Icon name="accounts" size={28} /></span>
              <h3>No accounts found</h3><p>Sign in to Claude Code with a local profile. Garcon will discover it automatically.</p>
              <button class="button primary" onclick={() => (modal = "claude-account")}>Add a Claude account <Icon name="arrow" size={16} /></button>
            </div>{/if}
            <AccountTable accounts={claudeAccounts} provider="Claude Code" poolSupported={!!claudeRouting?.enabled} poolEditable={false} nextId={claudeNext?.id} />
            <p class="provider-explanation">{#if claudeRouting?.enabled}New gateway sessions use the eligible account with the highest quota-per-hour score. Each session keeps its account across restarts. Model-specific limits can change which account is next.{:else}Profile launchers use their selected login. Configure the optional Desktop gateway to enable automatic account selection and session pinning.{/if}</p>
            <button class="add-inline" onclick={() => (modal = "claude-account")}><Icon name="plus" size={16} /> Add another Claude account<span>Separate login. Same endpoint.</span></button>
          </div>{/if}
        </section>
        <div class="routing-notes">
          <div>
            <span class="note-icon"><Icon name="analytics" /></span>
            <div>
              <h3>Codex account selection</h3>
              <p>
                New Codex sessions use the eligible account with the highest remaining
                quota ÷ hours until reset. The most constrained window sets the score.
              </p>
            </div>
          </div>
          <div>
            <span class="note-icon"><Icon name="link" /></span>
            <div>
              <h3>Session assignments</h3>
              <p>
                Existing Codex sessions keep their assigned account, even after a restart.
                Account selection applies to new sessions.
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
              >{new Set(sessions.filter((s) => s.account_id || s.account).map((s) => `${s.provider || s.harness}:${s.account_id || s.account}`)).size}</strong
            >
          </div>
          <div class="summary-explanation">
            <Icon name="link" />
            <p>
              Codex, Claude Code, and other connected clients.<br />Active model requests appear first.
            </p>
          </div>
        </div>
        <section class="data-panel">
          <div class="panel-toolbar">
            <label class="search-box"
              ><Icon name="search" size={17} /><input
                placeholder="Find a client, task, project, account, or session ID…"
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
                  ><th>SESSION</th><th>ACTIVITY</th><th>ACCOUNT</th><th>MODEL</th><th
                    >REQUESTS</th
                  ><th>LAST SEEN</th><th></th></tr
                ></thead
              ><tbody
                >{#each filteredSessions as s (s.key)}<tr
                    ><td
                      ><div class="session-id">
                        <HarnessLogo harness={s.harness} size={28} />
                        <div>
                          <button class="task-title" title={s.task?.title || s.session_id || s.key}
                            disabled={!s.session_id} onclick={() => sessionLogs(s)}
                            >{s.task?.title || (s.session_id ? taskName(s.session_id, s.harness) : `Legacy session ${s.key.slice(0, 8)}`)}</button>
                          <small title={s.task?.cwd}>{harnessName(s.harness)}{s.task?.cwd ? ` · ${projectName(s)}` : ""}{s.task?.archived ? " · Archived" : ""}</small>
                          <small class="session-reference"><span title={s.session_id || s.key}>{s.session_id ? `${s.session_id.slice(0, 8)}…${s.session_id.slice(-6)}` : "ID available when resumed"}</span>
                            {#if s.session_id}<button class="row-action" title="Copy full session ID" aria-label={copied === s.key ? "Session ID copied" : `Copy session ID for ${taskName(s.session_id, s.harness)}`} onclick={() => copy(s.session_id, s.key)}><Icon name={copied === s.key ? "check" : "copy"} size={12}/></button>{/if}
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
                  : "Start a Codex or Claude Code session through Garcon to see its activity here."}
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
                  placeholder="Search client, account, model, session, path…"
                  aria-label="Search request logs"
                  value={query}
                  oninput={(e) => {
                    query = e.currentTarget.value;
                    logSession = null;
                    offset = 0;
                  }}
                /></label
              ><button
                class="filter-button"
                class:selected={errorsOnly}
                aria-pressed={errorsOnly}
                onclick={() => {
                  errorsOnly = !errorsOnly;
                  offset = 0;
                }}>Errors only</button
              ><button
                class="filter-button"
                class:selected={hideZeroTokens}
                aria-pressed={hideZeroTokens}
                onclick={() => {
                  hideZeroTokens = !hideZeroTokens;
                  offset = 0;
                }}>Hide 0 tokens</button
              >{:else}<button class="button secondary" onclick={systemLogs}
                ><Icon name="refresh" size={15} />Refresh</button
              >{/if}
          </div>
          {#if logMode === "requests" && logSession}<div class="session-log-filter"><HarnessLogo harness={logSession.harness} size={18} /><span>{harnessName(logSession.harness)} · {taskName(logSession.id, logSession.harness)}</span><button class="filter-button" onclick={() => { logSession = null; offset = 0; }}>Clear session filter</button></div>{/if}
          {#if logMode === "requests"}<div class="table-scroll">
              <table class="log-table">
                <thead
                  ><tr
                    ><th>TIME</th><th>CLIENT</th><th>STATUS</th><th>MODEL / REQUEST</th><th
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
                      ><td><HarnessLogo harness={r.harness} size={22} /><small>{harnessName(r.harness)}</small></td
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
                              ? taskName(r.session_id, r.harness)
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
                  {query || logSession || errorsOnly || hideZeroTokens
                    ? "No matching requests."
                    : "No requests yet"}
                </h3>
                <p>
                  {query || logSession || errorsOnly || hideZeroTokens
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
  <LiveTicker rows={feed} {connected} {active} {hideZeroTokens} {taskName} onselect={(row) => (selected = row)} />
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
        : modal === "add" ? "Add account"
        : modal === "claude" ? "Connect Claude Code"
        : modal === "account" || modal === "claude-account"
          ? `Add ${accountProvider} account`
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
        {#if selected.session_id}<p class="request-task-name">{taskName(selected.session_id, selected.harness)}</p>
          {#if sessionIndex.get(sessionKey(selected.session_id, selected.harness))?.task?.cwd}<p class="drawer-fine">{sessionIndex.get(sessionKey(selected.session_id, selected.harness))?.task?.cwd}</p>{/if}
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
          {#each [["Client", harnessName(selected.harness)], ["Provider", selected.provider || "Not recorded"], ["Model", selected.model || "Unknown"], ["Account", selected.account || "Unassigned"], ["Account ID", selected.account_id || "Not recorded"], ["Session", selected.session_id || "Not recorded"], ["Request ID", selected.request_id || `Legacy record ${selected.sequence}`], ["Endpoint", `${selected.method || "POST"} ${selected.path || "Not recorded"}`], ["Duration", `${(selected.ms / 1000).toFixed(3)}s`], ["First byte", selected.first_byte_ms === undefined ? "Not measured" : `${selected.first_byte_ms}ms`], ["Input tokens", count(selected.input)], ["Cached input", count(selected.cache_read)], ["Cache write", count(selected.cache_write)], ["Output tokens", count(selected.output)]] as [label, value]}<div
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
      {:else if modal === "add"}
        <h2>Add account</h2><p class="drawer-description">Choose the provider for your new login.</p>
        <button class="button secondary full" onclick={() => (modal = "account")}><CodexLogo size={24}/>Codex account</button>
        <button class="button secondary full" onclick={() => (modal = "claude-account")}><img src={claudeLogo} width="24" height="24" alt=""/>Claude Code account</button>
      {:else if modal === "account" || modal === "claude-account"}<span class="drawer-art">
          {#if addingClaude}<img src={claudeLogo} width="36" height="36" alt=""/>{:else}<CodexLogo size={36} />{/if}<span>+</span></span>
        <h2>Add {accountProvider} account</h2>
        <p class="drawer-description">
          Each {accountProvider} profile holds its own login. Garcon discovers
          profiles on this computer automatically.
        </p>
        <div class="setup-step">
          <span>01</span>
          <div>
            <h3>Name your profile</h3>
            <p>Use a new name to keep your existing login intact.</p>
            <label class="profile-input"
              ><span>~/.{addingClaude ? "claude" : "codex"}-</span><input
                aria-label={`${accountProvider} profile name`}
                bind:value={profile}
                pattern={"[a-z0-9][a-z0-9-]{0,31}"}
                placeholder="personal"
              /></label
            >{#if !/^[a-z0-9][a-z0-9-]{0,31}$/.test(profile)}<small
                class="validation"
                >Use lowercase letters, numbers, and hyphens (1–32 characters).</small
              >{/if}
          </div>
        </div>
        <div class="setup-step">
          <span>02</span>
          <div>
            <h3>Sign in from your terminal</h3>
            <p>
              Run this command, then sign in to your {accountProvider} account.
            </p>
            <div class="code-box">
              <code>{addingClaude ? claudeCommand : command}</code><button
                aria-label="Copy login command"
                onclick={() => copy(addingClaude ? claudeCommand : command, "login")}
                ><Icon
                  name={copied === "login" ? "check" : "copy"}
                  size={16}
                /></button
              >
            </div>
          </div>
        </div>
        <div class="setup-step">
          <span>03</span>
          <div>
            <h3>{addingClaude ? "Discover the account" : "Add it to your pool"}</h3>
            <p>
              {#if addingClaude}Refresh accounts to show the new login and its quota. Use this launcher to keep Remote Control available, then enable “Remote Control for all sessions” in Claude’s /config.
              {:else}Refresh accounts and add the new login to your pool. Garcon selects an eligible account automatically for each new session.{/if}
            </p>
            {#if addingClaude}<div class="code-box"><code>{claudeLaunchCommand}</code><button aria-label="Copy Claude launch command" onclick={() => copy(claudeLaunchCommand, "claude-launch")}><Icon name={copied === "claude-launch" ? "check" : "copy"} size={16}/></button></div>{/if}
          </div>
        </div>
        <button
          class="button primary full"
          disabled={refreshing}
          onclick={async () => {
            await refresh();
            modal = null;
          }}
          ><Icon name="refresh" size={16} />I've signed in · discover accounts</button
        >
        <p class="drawer-fine">
          {#if addingClaude}Scans ~/.claude, ~/.claude-*, and CLAUDE_CONFIG_DIR. Credentials remain in Claude Code's own files or Keychain.
          {:else}Scans ~/.codex, ~/.codex-*, and CODEX_HOME. Duplicate logins appear once. Credentials remain in Codex's own files.{/if}
        </p>
      {:else}
        <span class="drawer-art">
          {#if setupClient === "claude"}<img src={claudeLogo} width="36" height="36" alt="" />
          {:else}<Icon name="link" size={36} />{/if}
        </span>
        <h2>Connect {setupClient === "claude" ? "Claude Code" : "Codex"}</h2>
        <p class="drawer-description">
          {#if setupClient === "claude"}Make your profile launcher use Garcon with Remote Control on by default. Paste this prompt into a coding agent on this computer to set it up.
          {:else}Paste this prompt into a coding agent on this computer. It will back up your config, update the connection settings, and verify the result.{/if}
        </p>
        <button class="button primary full" onclick={() => copy(setupPrompt, `${setupClient}-prompt`)}>
          <Icon name={copied === `${setupClient}-prompt` ? "check" : "copy"} size={16} />
          <span aria-live="polite">{copied === `${setupClient}-prompt` ? "Prompt copied" : "Copy setup prompt"}</span>
        </button>
        <div class="code-box block setup-prompt">
          <textarea readonly rows="14" aria-label="Coding agent setup prompt" value={setupPrompt}></textarea>
        </div>
        <div class="connection-note">
          <Icon name="shield" />
          <p>
            Garcon listens on <strong>127.0.0.1:4141</strong> in development and when
            running locally in the background.
          </p>
        </div>
        <p class="drawer-fine">
          Copying the prompt does not change your configuration.
          {#if setupClient === "claude"}The launcher connects each session. Confirm Remote Control in Claude Code and model traffic in Garcon's Logs.
          {:else}The connection indicator checks saved settings; confirm live traffic in Logs after restarting the client.{/if}
        </p>{/if}
    </dialog>
  </div>{/if}
