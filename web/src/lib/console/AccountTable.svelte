<script lang="ts">
  import Icon from "./Icon.svelte";
  import { duration, initials, tone, type Account } from "./model";
  let { accounts, provider = "Codex", nextId, busy = false, poolSupported = true, poolEditable = true, ontoggle }: {
    accounts: Account[];
    provider?: string;
    nextId?: string;
    busy?: boolean;
    poolSupported?: boolean;
    poolEditable?: boolean;
    ontoggle?: (account: Account) => void;
  } = $props();
</script>

<div class="account-table">
    <div class="account-columns">
      <span>ACCOUNT</span><span>AVAILABLE USAGE</span><span
        >ROUTING SCORE</span
      ><span>IN POOL</span>
    </div>
    {#each accounts as a (a.id)}<div
        class="account-row"
        class:excluded={poolSupported && !a.enrolled}
      >
        <div class="account-identity">
          <span class="account-avatar tone-{tone(a.email)}"
            >{initials(a.email)}</span
          >
          <div>
            <div class="account-name">
              {a.email ||
                "Missing local login"}{#if nextId === a.id}<span
                  class="next-badge">UP NEXT</span
                >{/if}
            </div>
            <div class="account-sub">
              <span class="plan">{a.plan || provider}</span><span
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
            {#if a.windows.filter((w) => provider !== "Codex" || w.id.startsWith("codex:")).length > 1}<div
                class="quota-details"
              >
                {#each a.windows.filter((w) => provider !== "Codex" || w.id.startsWith("codex:")) as w}<span
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
          ><small>{poolSupported ? "% / hour" : "Client login"}</small>
        </div>
        <div>
          {#if poolSupported && poolEditable}<button
            class="pool-check"
            class:checked={a.enrolled}
            aria-label={`${a.enrolled ? "Remove" : "Enroll"} ${a.email}`}
            aria-pressed={a.enrolled}
            disabled={busy}
            onclick={() => ontoggle?.(a)}
            >{#if a.enrolled}<Icon
                name="check"
                size={13}
              />{:else}<Icon name="plus" size={13} />{/if}</button
          >{:else if poolSupported}<span class="pool-check" class:checked={a.enrolled} aria-label={a.enrolled ? "In account pool" : "Not in account pool"} title={a.enrolled ? "In account pool" : "Not in account pool"}>{#if a.enrolled}<Icon name="check" size={13} />{:else}—{/if}</span>{:else}<span class="muted" aria-label="Account pool not supported" title="Claude Code uses the login selected in its local profile.">—</span>{/if}
        </div>
      </div>{/each}
  </div>
