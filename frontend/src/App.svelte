<script lang="ts">
  import { onMount } from 'svelte';
  import { RefreshCw, Plus, Pencil, Trash2, Pause, Play, Bell, ChevronDown } from 'lucide-svelte';
  import Interval from './lib/Interval.svelte';

  type Reminder = { kind: 'watch' | 'every'; app?: string; every: string; message: string; disabled?: boolean };
  type Config = { reminders: Reminder[]; cooldown: string; paused_until: string; quiet_start?: string; quiet_end?: string };
  type Status = { active: boolean; paused_until: string; focused_app: string; focus_known: boolean; active_rule: string; active_rules: string[]; matching_rules_known: boolean; scheduler_ready: boolean; suppression_reason: string; next_reminder: string; next_description: string; scheduler_error: string; niri_error: string; notification_available: boolean; version: string };
  type Diagnostics = { version: string; niri_error: string; notification_available: boolean; config_error: string };

  let status = $state<Status | null>(null);
  let config = $state<Config | null>(null);
  let diagnostics = $state<Diagnostics | null>(null);
  let statusError = $state('');
  let configError = $state('');
  let diagnosticsError = $state('');
  let notice = $state('');
  let loading = $state(true);
  let refreshing = $state(false);
  let saving = $state(false);
  let controlling = $state(false);
  let editing = $state<number | null>(null);
  let snoozeDuration = $state('15m');
  let savedConfig = $state('');
  let revision = '';
  let dirty = $derived(config !== null && JSON.stringify(config) !== savedConfig);
  let isPaused = $derived(status?.suppression_reason === 'paused' || (!!status?.paused_until && new Date(status.paused_until).getTime() > Date.now()));
  let appOptions = $derived([...new Set([status?.focused_app || '', ...(config?.reminders.map(r => r.app || '') ?? [])].filter(Boolean))]);

  async function request<T>(path: string, options?: RequestInit): Promise<T> {
    const response = await fetch(`/api/v1/${path}`, options);
    const data = await response.json().catch(() => null);
    if (!response.ok) throw new Error(data && typeof data.error === 'string' ? data.error : `Request failed (${response.status})`);
    if (path === 'config') revision = response.headers.get('ETag') ?? '';
    return data as T;
  }
  function errorMessage(error: unknown) { return error instanceof Error ? error.message : 'Request failed'; }
  async function loadStatus() {
    try { status = await request<Status>('status'); statusError = ''; }
    catch (error) { status = null; statusError = errorMessage(error); }
  }
  async function loadConfig() {
    try {
      const next = await request<Config>('config');
      next.reminders ??= [];
      config = next; savedConfig = JSON.stringify(next); configError = ''; editing = null;
    } catch (error) { configError = errorMessage(error); }
  }
  async function loadDiagnostics() {
    try { diagnostics = await request<Diagnostics>('diagnostics'); diagnosticsError = ''; }
    catch (error) { diagnostics = null; diagnosticsError = errorMessage(error); }
  }
  async function refresh() {
    refreshing = true;
    await Promise.all([loadStatus(), loadDiagnostics(), ...(!dirty ? [loadConfig()] : [])]);
    refreshing = false;
  }
  onMount(() => {
    void (async () => { await refresh(); loading = false; })();
    const timer = window.setInterval(() => { if (!document.hidden) void loadStatus(); }, 15000);
    const beforeUnload = (event: BeforeUnloadEvent) => { if (dirty) { event.preventDefault(); event.returnValue = ''; } };
    window.addEventListener('beforeunload', beforeUnload);
    return () => { window.clearInterval(timer); window.removeEventListener('beforeunload', beforeUnload); };
  });
  async function save() {
    if (!config) return;
    saving = true; notice = ''; configError = '';
    try {
      const next = await request<Config>('config', { method: 'PUT', headers: { 'Content-Type': 'application/json', 'If-Match': revision }, body: JSON.stringify(config) });
      next.reminders ??= [];
      config = next; savedConfig = JSON.stringify(next); editing = null; notice = 'Changes saved.';
      await Promise.all([loadStatus(), loadDiagnostics()]);
    } catch (error) { configError = errorMessage(error); }
    finally { saving = false; }
  }
  async function discard() {
    if (dirty && !window.confirm('Discard all unsaved changes and reload the saved configuration?')) return;
    await loadConfig();
  }
  async function control(action: 'pause' | 'resume' | 'snooze') {
    controlling = true; notice = '';
    try {
      status = await request<Status>('control', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(action === 'resume' ? { action } : { action, duration: action === 'pause' ? '1h' : snoozeDuration }) });
      statusError = ''; notice = action === 'resume' ? 'Reminders resumed.' : action === 'pause' ? 'Reminders paused for 1 hour.' : `Reminders snoozed for ${snoozeDuration}.`;
      if (!dirty) await loadConfig();
    } catch (error) { statusError = errorMessage(error); }
    finally { controlling = false; }
  }
  function addRule() {
    if (!config) return;
    config.reminders.push({ kind: 'every', every: '1h', message: '' });
    editing = config.reminders.length - 1;
  }
  function removeRule(index: number) {
    if (!config) return;
    const rule = config.reminders[index];
    if (!window.confirm(`Delete “${rule.message || `Reminder ${index + 1}`}”? This removal takes effect when you save changes.`)) return;
    config.reminders.splice(index, 1); editing = null;
  }
  function formatDate(value: string) {
    if (!value || value.startsWith('0001-01-01')) return 'Not known';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date);
  }
</script>

<svelte:head><title>Nudge — reminders</title><meta name="description" content="Manage Nudge reminders and settings." /></svelte:head>

<div class="shell">
  <header class="site-header">
    <div class="brand"><span class="brand-icon"><Bell size={17} /></span><span>nudge</span><span class="brand-divider"></span><span class="brand-context">Reminder service</span></div>
    <button class="button quiet refresh" type="button" onclick={refresh} disabled={refreshing}><RefreshCw size={15} /> {refreshing ? 'Refreshing…' : 'Refresh'}</button>
  </header>
  <main>
    <div class="page-title"><div><h1>Reminders</h1><p>Service status and reminder rules</p></div><a class="button secondary" href="#rules"><Plus size={16} /> Manage rules</a></div>
    {#if notice}<div class="feedback success" role="status">{notice}<button type="button" class="text-button" onclick={() => notice = ''}>Dismiss</button></div>{/if}
    {#if statusError}<div class="feedback error" role="alert">Status: {statusError} <button type="button" class="text-button" onclick={loadStatus}>Retry</button></div>{/if}
    <section class="overview" aria-labelledby="status-heading">
      <div class="section-top"><div><h2 id="status-heading">Service status</h2></div><span class:good={!!status && status.scheduler_ready && status.active && !status.suppression_reason && !status.scheduler_error} class:warning={!!status && (!!status.suppression_reason || !!status.scheduler_error)} class="status-badge"><span class="status-dot"></span>{loading ? 'Checking' : !status ? 'Unavailable' : status.scheduler_error ? 'Scheduling error' : !status.scheduler_ready ? 'Starting' : isPaused ? 'Paused' : status.suppression_reason === 'quiet_hours' ? 'Quiet hours' : status.active ? 'Scheduling active' : 'Inactive'}</span></div>
      <div class="status-grid">
        <div><span class="data-label">Focused app</span><strong>{!status || !status.focus_known ? 'Unavailable' : status.focused_app || 'No app focused'}</strong>{#if status?.niri_error}<small>Niri: {status.niri_error}</small>{/if}</div>
        <div><span class="data-label">Next eligible reminder</span><strong>{status ? formatDate(status.next_reminder) : 'Unavailable'}</strong><small>{status?.next_description || 'Timing is not available.'}</small></div>
        <div><span class="data-label">Matching rules</span><strong>{status?.matching_rules_known ? status.active_rules.length : 'Unavailable'}</strong>{#if status?.matching_rules_known && status.active_rules.length}<small>{status.active_rules.join(', ')}</small>{/if}</div>
      </div>
      {#if status?.scheduler_error}<p class="inline-error" role="alert">Scheduler: {status.scheduler_error}</p>{/if}
      {#if status && isPaused}<p class="status-note">Paused until {formatDate(status.paused_until)}</p>{:else if status?.suppression_reason === 'quiet_hours'}<p class="status-note">Reminders are suppressed during quiet hours.</p>{/if}
      <div class="control-row">
        {#if status}<button class="button primary" type="button" onclick={() => control(isPaused ? 'resume' : 'pause')} disabled={controlling}>{#if isPaused}<Play size={15} /> Resume{:else}<Pause size={15} /> Pause for 1 hour{/if}</button>{/if}
        <div class="snooze"><label for="snooze">Snooze</label><select id="snooze" bind:value={snoozeDuration}><option value="5m">5 minutes</option><option value="15m">15 minutes</option><option value="30m">30 minutes</option><option value="1h">1 hour</option></select><button class="button secondary" type="button" onclick={() => control('snooze')} disabled={controlling || !status}>Snooze</button></div>
      </div>
    </section>
    <form onsubmit={(event) => { event.preventDefault(); void save(); }}>
      <section id="rules" class="rules-section" aria-labelledby="rules-heading">
        <div class="section-top"><div><p class="section-label">Configuration</p><h2 id="rules-heading">Reminder rules {#if config}<span class="count">{config.reminders.length}</span>{/if}</h2><p class="section-description">Changes apply after saving.</p></div><button class="button secondary" type="button" onclick={addRule} disabled={!config || saving}><Plus size={16} /> Add rule</button></div>
        {#if configError}<div class="feedback error" role="alert"><span>Configuration: {configError}</span><button class="text-button" type="button" onclick={discard}>{config ? 'Reload saved configuration' : 'Retry'}</button></div>{/if}
        {#if !config}<p class="empty">{loading ? 'Loading configuration…' : 'Configuration unavailable.'}</p>
        {:else if config.reminders.length === 0}<div class="empty"><strong>No reminder rules</strong><p>Add a rule to schedule a reminder or watch an app.</p></div>
        {:else}<div class="rule-list">
          {#each config.reminders as rule, index}
            <article class="rule" class:rule-disabled={rule.disabled}>
              <div class="rule-summary"><div class="rule-copy"><div class="rule-title"><strong>{rule.message || 'New reminder'}</strong><span class="rule-state">{rule.disabled ? 'Disabled' : 'Enabled'}</span></div><p>{rule.kind === 'watch' ? `While using ${rule.app || 'an app'}` : 'At an interval'} <span aria-hidden="true">·</span> Every {rule.every || '—'}</p></div><div class="rule-actions"><label class="toggle-label"><input type="checkbox" checked={!rule.disabled} onchange={(event) => rule.disabled = !event.currentTarget.checked} aria-label={`Enable ${rule.message || `reminder ${index + 1}`}`} /> Enabled</label><button class="button quiet" type="button" aria-expanded={editing === index} onclick={() => editing = editing === index ? null : index}><Pencil size={15} /> {editing === index ? 'Close' : 'Edit'}</button><button class="icon-button danger" type="button" aria-label={`Delete ${rule.message || `reminder ${index + 1}`}`} onclick={() => removeRule(index)}><Trash2 size={16} /></button></div></div>
              {#if editing === index}<div class="editor"><div class="form-grid"><label>Reminder type<select bind:value={rule.kind}><option value="every">At an interval</option><option value="watch">While using an app</option></select></label>{#if rule.kind === 'watch'}<label>App ID<input bind:value={rule.app} list="known-apps" required placeholder="e.g. firefox" /><small>Choose a detected app ID or enter one manually.</small></label>{/if}<div class="field"><label for={`interval-${index}`}>Repeat every</label><Interval id={`interval-${index}`} bind:value={rule.every} /><small>For complex durations, choose custom.</small></div><label class="wide">Message<input bind:value={rule.message} required placeholder="Reminder message" /></label></div></div>{/if}
            </article>
          {/each}
        </div>{/if}
        <datalist id="known-apps">{#each appOptions as app}<option value={app}></option>{/each}</datalist>
      </section>
      {#if config}<details class="disclosure settings"><summary><span><strong>Settings</strong><small>Cooldown and quiet hours</small></span><ChevronDown size={17} /></summary><div class="disclosure-content"><div class="form-grid"><div class="field"><label for="cooldown">Shared cooldown</label><Interval id="cooldown" bind:value={config.cooldown} /><small>Minimum time between notifications.</small></div><div></div><label>Quiet hours start<input type="time" bind:value={config.quiet_start} /><small>Leave both times blank to turn off quiet hours.</small></label><label>Quiet hours end<input type="time" bind:value={config.quiet_end} /></label></div></div></details>
        <div class="save-bar"><span>{dirty ? 'Unsaved changes' : 'No unsaved changes'}</span><div><button class="button quiet" type="button" onclick={discard} disabled={!dirty || saving}>Discard</button><button class="button primary" type="submit" disabled={!dirty || saving}>{saving ? 'Saving…' : 'Save changes'}</button></div></div>
      {/if}
    </form>
    <details class="disclosure diagnostics"><summary><span><strong>Diagnostics</strong><small>Connection, notification executable and version</small></span><ChevronDown size={17} /></summary><div class="disclosure-content">{#if diagnosticsError}<div class="feedback error" role="alert">{diagnosticsError} <button class="text-button" type="button" onclick={loadDiagnostics}>Retry</button></div>{/if}{#if diagnostics}<dl class="diagnostic-list"><div><dt>Notification executable<small>Availability only; delivery is not verified.</small></dt><dd>{diagnostics.notification_available ? 'Available' : 'Unavailable'}</dd></div><div><dt>Niri connection</dt><dd>{diagnostics.niri_error || 'No errors reported'}</dd></div><div><dt>Configuration</dt><dd>{diagnostics.config_error || 'No errors reported'}</dd></div><div><dt>Scheduler</dt><dd>{status?.scheduler_error || (status ? 'No errors reported' : 'Unavailable')}</dd></div><div><dt>Version</dt><dd>{diagnostics.version || 'Unknown'}</dd></div></dl>{:else if !diagnosticsError}<p>Loading diagnostics…</p>{/if}</div></details>
    <footer>Nudge {status?.version || ''} <span>Local reminder service</span></footer>
  </main>
</div>
