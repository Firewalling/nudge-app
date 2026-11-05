<script lang="ts">
  import { onMount } from 'svelte';
  let { onDirtyChange }: { onDirtyChange: (dirty: boolean, busy: boolean) => void } = $props();

  type Day = { date: string; attempts: number; snoozes: number };
  type Hour = { hour: number; attempts: number };
  type AppActivity = { app: string; attempts: number; foreground_seconds: number };
  type Insights = { enabled: boolean; app_activity: boolean; retention_days: number; days: Day[]; hours: Hour[]; apps: AppActivity[]; error: string };
  type Settings = { enabled: boolean; app_activity: boolean; retention_days: number };

  let data = $state<Insights | null>(null);
  let range = $state<7 | 30>(7);
  let loading = $state(true);
  let saving = $state(false);
  let error = $state('');
  let notice = $state('');
  let selected = $state<Settings>({ enabled: false, app_activity: false, retention_days: 90 });
  let originalSettings = $state('');
  let settingsDirty = $derived(originalSettings !== '' && JSON.stringify(selected) !== originalSettings);
  $effect(() => { onDirtyChange(settingsDirty, saving); });
  let requestID = 0;
  let attempts = $derived(data?.days.reduce((sum, day) => sum + day.attempts, 0) ?? 0);
  let snoozes = $derived(data?.days.reduce((sum, day) => sum + day.snoozes, 0) ?? 0);
  let dayMax = $derived(Math.max(1, ...(data?.days.map(day => Math.max(day.attempts, day.snoozes)) ?? [])));
  let hourMax = $derived(Math.max(1, ...(data?.hours.map(hour => hour.attempts) ?? [])));
  let appMax = $derived(Math.max(1, ...(data?.apps.map(app => app.attempts) ?? [])));
  let focusMax = $derived(Math.max(1, ...(data?.apps.map(app => app.foreground_seconds) ?? [])));
  let hasEvents = $derived(attempts > 0 || snoozes > 0 || (data?.apps.some(app => app.foreground_seconds > 0) ?? false));

  async function readResponse<T>(response: Response): Promise<T> {
    const body = await response.json().catch(() => null);
    if (!response.ok) throw new Error(body && typeof body.error === 'string' ? body.error : `Request failed (${response.status})`);
    return body as T;
  }
  function message(cause: unknown) { return cause instanceof Error ? cause.message : 'Please try again.'; }
  async function load() {
    const id = ++requestID;
    loading = true; error = '';
    try {
      const result = await readResponse<Insights>(await fetch(`/api/v1/insights?days=${range}`));
      if (id !== requestID) return;
      data = result;
      selected = { enabled: result.enabled, app_activity: result.app_activity, retention_days: result.retention_days };
      originalSettings = JSON.stringify(selected);
      if (result.error) error = result.error;
    } catch (cause) { if (id === requestID) { data = null; error = message(cause); } }
    finally { if (id === requestID) loading = false; }
  }
  onMount(() => { void load(); });
  async function chooseRange(next: 7 | 30) {
    if (range === next || saving) return;
    if (settingsDirty && !window.confirm('Discard unsaved history settings?')) return;
    range = next;
    await load();
  }
  async function saveSettings() {
    saving = true; error = ''; notice = '';
    try {
      await readResponse<Settings>(await fetch('/api/v1/insights/settings', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(selected) }));
      await load(); notice = 'History settings saved.';
    } catch (cause) { error = message(cause); }
    finally { saving = false; }
  }
  async function clearHistory() {
    if (!window.confirm('Delete all recorded history? This cannot be undone. Your reminders and settings will stay, and recording will continue if enabled.')) return;
    const pendingSettings = settingsDirty ? { ...selected } : null;
    saving = true; error = ''; notice = '';
    try {
      await readResponse<unknown>(await fetch('/api/v1/insights/history', { method: 'DELETE' }));
      await load();
      if (pendingSettings) selected = pendingSettings;
      notice = 'History deleted.';
    } catch (cause) { error = message(cause); }
    finally { saving = false; }
  }
  function dayLabel(value: string) {
    const date = new Date(`${value}T12:00:00`);
    return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' }).format(date);
  }
  function appName(value: string) {
    const part = value.split('.').filter(Boolean).slice(-1)[0] || value;
    return part.split(/[-_]/).filter(Boolean).map(word => word[0].toUpperCase() + word.slice(1)).join(' ') || value;
  }
  function duration(seconds: number) {
    if (seconds < 60) return `${Math.round(seconds)} sec`;
    const minutes = Math.round(seconds / 60);
    if (minutes < 60) return `${minutes} min`;
    return `${Math.floor(minutes / 60)} hr ${minutes % 60} min`;
  }
</script>

<section class="insights" aria-labelledby="insights-title">
  <div class="insights-heading">
    <div><p class="eyebrow">Private to this computer</p><h1 id="insights-title">Your activity</h1><p>See when Nudge tried to send reminders and when you snoozed them.</p></div>
    <div class="range-switch" role="group" aria-label="Time range">
      <button type="button" class:chosen={range === 7} aria-pressed={range === 7} onclick={() => chooseRange(7)}>7 days</button>
      <button type="button" class:chosen={range === 30} aria-pressed={range === 30} onclick={() => chooseRange(30)}>30 days</button>
    </div>
  </div>
  {#if notice}<p class="insights-notice" role="status">{notice}</p>{/if}
  {#if error}<div class="insights-error" role="alert"><span>History: {error}</span><button type="button" class="text-button" onclick={() => { if (!settingsDirty || window.confirm('Discard unsaved history settings and reload?')) void load(); }}>Reload</button></div>{/if}
  {#if loading}<p class="insights-loading" role="status">Loading your activity…</p>
  {:else if data}
    {#if !data.enabled}<div class="history-state"><strong>History is off</strong><p>Nudge won’t record new activity. Turn on history below to see future reminders here.</p></div>{/if}
    {#if !hasEvents && data.enabled}<div class="history-empty"><h2>No reminder activity recorded yet</h2><p>Insights will appear as Nudge records activity.</p></div>
    {:else if hasEvents}
      <section class="insight-section" aria-labelledby="activity-title">
        <h2 id="activity-title">Reminders over time</h2>
        <p>In the last {range} days, Nudge recorded {attempts} reminder {attempts === 1 ? 'attempt' : 'attempts'} and {snoozes} {snoozes === 1 ? 'snooze' : 'snoozes'}.</p>
        <p class="chart-scale">Only recorded activity is shown; unrecorded periods are unknown. Attempts include failures and do not prove a notification appeared or was read.</p>
        <p class="chart-key"><span class="key-attempt"></span> Reminder attempts <span class="key-snooze"></span> Snoozes</p>
        <p class="chart-scale">Scale: 0 to {dayMax} per day</p>
        <!-- svelte-ignore a11y_no_noninteractive_tabindex (Scrollable chart needs keyboard access.) -->
        <div class="chart-scroll recent" role="region" aria-label="Scroll daily chart" tabindex="0"><div class="bar-chart" class:week={range === 7} role="img" aria-label={`Daily reminder attempts and snoozes over ${range} days. Exact values are in the table below.`}>
          {#each data.days as day, index (day.date)}
            <div class="day-column"><div class="bar-pair"><span class="bar attempt" class:zero={day.attempts === 0} style:height={`${(day.attempts / dayMax) * 100}%`} title={`${dayLabel(day.date)}: ${day.attempts} reminder attempts`}></span><span class="bar snooze-bar" class:zero={day.snoozes === 0} style:height={`${(day.snoozes / dayMax) * 100}%`} title={`${dayLabel(day.date)}: ${day.snoozes} snoozes`}></span></div><span class="axis-label" aria-hidden="true">{range === 7 || index === 0 || index === data.days.length - 1 ? dayLabel(day.date) : ''}</span></div>
          {/each}
        </div></div>
        {#if range === 30}<p class="chart-hint">Scroll sideways to see earlier days.</p>{/if}
        <details class="data-table"><summary>View daily numbers</summary><div class="table-scroll"><table><caption>Daily reminder attempts and snoozes</caption><thead><tr><th scope="col">Day</th><th scope="col">Reminder attempts</th><th scope="col">Snoozes</th></tr></thead><tbody>{#each data.days as day (day.date)}<tr><th scope="row">{dayLabel(day.date)}</th><td>{day.attempts}</td><td>{day.snoozes}</td></tr>{/each}</tbody></table></div></details>
      </section>
      <section class="insight-section" aria-labelledby="apps-title">
        <h2 id="apps-title">Reminders and apps</h2>
        <p>Attempts for reminders set to a specific app. App details are recorded only when app activity is on.</p>
        {#if data.apps.length === 0}<p class="chart-empty">No app activity was recorded in this period.</p>
        {:else}
          {#if data.apps.some(app => app.attempts > 0)}<div class="app-bars" role="img" aria-label="Reminder attempts by application. Exact values are in the table below.">{#each data.apps as app (app.app)}<div class="app-row"><span>{appName(app.app)}</span><span class="horizontal-track"><span style:width={`${(app.attempts / appMax) * 100}%`}></span></span><strong>{app.attempts}</strong></div>{/each}</div>{:else}<p class="chart-empty">No app-specific reminder attempts were recorded.</p>{/if}
          {#if data.apps.some(app => app.foreground_seconds > 0)}<h3>Recorded foreground time</h3><p>This is observed window focus, not verified active use. Gaps and interruptions may be missing.</p><div class="app-bars" role="img" aria-label="Recorded foreground time by application. Exact values are in the table below.">{#each data.apps as app (app.app)}<div class="app-row"><span>{appName(app.app)}</span><span class="horizontal-track focus-track"><span style:width={`${(app.foreground_seconds / focusMax) * 100}%`}></span></span><strong>{duration(app.foreground_seconds)}</strong></div>{/each}</div>{/if}
          <details class="data-table"><summary>View app numbers</summary><div class="table-scroll"><table><caption>Recorded app activity</caption><thead><tr><th scope="col">Application</th><th scope="col">Reminder attempts</th><th scope="col">Foreground time</th></tr></thead><tbody>{#each data.apps as app (app.app)}<tr><th scope="row">{appName(app.app)} ({app.app})</th><td>{app.attempts}</td><td>{duration(app.foreground_seconds)}</td></tr>{/each}</tbody></table></div></details>
        {/if}
      </section>
      <section class="insight-section" aria-labelledby="patterns-title">
        <h2 id="patterns-title">When reminders happen</h2><p>Reminder attempts by hour of the day, in your local time.</p>
        {#if attempts === 0}<p class="chart-empty">No reminder attempts were recorded in this period.</p>{:else}
        <p class="chart-scale">Scale: 0 to {hourMax} per hour</p>
        <!-- svelte-ignore a11y_no_noninteractive_tabindex (Scrollable chart needs keyboard access.) -->
        <div class="chart-scroll" role="region" aria-label="Scroll hourly chart" tabindex="0"><div class="hour-chart" role="img" aria-label="Reminder attempts by hour, from midnight to 11 pm. Exact values are in the table below.">{#each data.hours as hour (hour.hour)}<div class="hour-column"><div class="hour-plot"><span class="bar attempt" style:height={`${(hour.attempts / hourMax) * 100}%`} title={`${String(hour.hour).padStart(2, '0')}:00: ${hour.attempts} attempts`}></span></div><span class="axis-label" aria-hidden="true">{hour.hour % 6 === 0 ? `${String(hour.hour).padStart(2, '0')}:00` : ''}</span></div>{/each}</div></div>
        <details class="data-table"><summary>View hourly numbers</summary><div class="table-scroll"><table><caption>Reminder attempts by local hour</caption><thead><tr><th scope="col">Hour</th><th scope="col">Reminder attempts</th></tr></thead><tbody>{#each data.hours as hour (hour.hour)}<tr><th scope="row">{String(hour.hour).padStart(2, '0')}:00</th><td>{hour.attempts}</td></tr>{/each}</tbody></table></div></details>
        {/if}
      </section>
      <p class="coverage-note">Sleep and service interruptions can leave gaps in recorded history.</p>
    {/if}
    <section class="privacy" aria-labelledby="privacy-title"><h2 id="privacy-title">History and privacy</h2><p>History stays on this computer. Nudge never records window titles, websites or document contents.</p>
      <form onsubmit={(event) => { event.preventDefault(); void saveSettings(); }}>
        <label class="privacy-toggle"><input type="checkbox" role="switch" bind:checked={selected.enabled} disabled={saving} /><span><strong>Record reminder history</strong><small>Keep reminder attempts, failures, pauses and snoozes.</small></span></label>
        <label class="privacy-toggle"><input type="checkbox" role="switch" bind:checked={selected.app_activity} disabled={saving || !selected.enabled} /><span><strong>Record app activity</strong><small>When history is on, also record app identifiers and observed foreground time. Focus does not prove active use.</small></span></label>
        <label class="retention-label" for="retention">Keep history for</label><select id="retention" bind:value={selected.retention_days} disabled={saving}>{#if ![30, 90, 180, 365].includes(selected.retention_days)}<option value={selected.retention_days}>{selected.retention_days} days</option>{/if}<option value={30}>30 days</option><option value={90}>90 days</option><option value={180}>180 days</option><option value={365}>1 year</option></select>
        <div class="privacy-actions"><button type="submit" class="button primary" disabled={saving || !settingsDirty}>{saving ? 'Saving…' : 'Save history settings'}</button><button type="button" class="button quiet danger" disabled={saving} onclick={clearHistory}>Delete all history</button></div>
      </form>
    </section>
  {/if}
</section>
