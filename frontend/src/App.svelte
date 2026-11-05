<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { Bell, Plus, ChevronDown } from 'lucide-svelte';
  import Interval from './lib/Interval.svelte';
  import Insights from './Insights.svelte';

  type Reminder = { kind: 'watch' | 'every'; app?: string; every: string; message: string; disabled?: boolean };
  type Config = { reminders: Reminder[]; cooldown: string; paused_until: string; quiet_start?: string; quiet_end?: string };
  type Status = { active: boolean; paused_until: string; focused_app: string; focus_known: boolean; active_rules: string[]; matching_rules_known: boolean; scheduler_ready: boolean; suppression_reason: string; next_reminder: string; next_description: string; scheduler_error: string; niri_error: string; notification_available: boolean; version: string };
  type Diagnostics = { version: string; niri_error: string; notification_available: boolean; config_error: string };
  type App = { id: string; name: string };

  let page = $state<'reminders' | 'insights'>('reminders');
  let insightsDirty = $state(false);
  let insightsBusy = $state(false);
  let config = $state<Config | null>(null);
  let status = $state<Status | null>(null);
  let diagnostics = $state<Diagnostics | null>(null);
  let apps = $state<App[]>([]);
  let appsError = $state('');
  let configError = $state('');
  let statusError = $state('');
  let diagnosticsError = $state('');
  let notice = $state('');
  let loading = $state(true);
  let saving = $state(false);
  let controlling = $state(false);
  let draft = $state<Reminder | null>(null);
  let editing = $state<number | null>(null);
  let settings = $state<Config | null>(null);
  let snoozeDuration = $state('15m');
  let revision = '';
  let originalDraft = '';
  let originalSettings = '';
  let dirty = $derived((draft !== null && JSON.stringify(draft) !== originalDraft) || (settings !== null && JSON.stringify(settings) !== originalSettings));
  let isPaused = $derived(status?.suppression_reason === 'paused' || (!!status?.paused_until && new Date(status.paused_until).getTime() > Date.now()));

  async function request<T>(path: string, options?: RequestInit): Promise<T> {
    const response = await fetch(`/api/v1/${path}`, options);
    const data = await response.json().catch(() => null);
    if (!response.ok) throw new Error(data && typeof data.error === 'string' ? data.error : `Request failed (${response.status})`);
    if (path === 'config') {
      const tag = response.headers.get('ETag');
      if (!tag) throw new Error('Configuration revision is missing. Reload before saving.');
      revision = tag;
    }
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
      config = next; configError = '';
    } catch (error) { configError = errorMessage(error); }
  }
  async function loadApps() {
    try { const result = await request<{ apps: App[] | null; error: string }>('apps'); apps = result.apps ?? []; appsError = result.error || ''; }
    catch (error) { apps = []; appsError = errorMessage(error); }
  }
  async function loadDiagnostics() {
    try { diagnostics = await request<Diagnostics>('diagnostics'); diagnosticsError = ''; }
    catch (error) { diagnostics = null; diagnosticsError = errorMessage(error); }
  }
  onMount(() => {
    void (async () => { await Promise.all([loadStatus(), loadConfig(), loadApps(), loadDiagnostics()]); loading = false; })();
    const timer = window.setInterval(() => { if (!document.hidden) void loadStatus(); }, 15000);
    const beforeUnload = (event: BeforeUnloadEvent) => { if (dirty || saving || insightsDirty || insightsBusy) { event.preventDefault(); event.returnValue = ''; } };
    window.addEventListener('beforeunload', beforeUnload);
    return () => { window.clearInterval(timer); window.removeEventListener('beforeunload', beforeUnload); };
  });
  async function saveConfig(next: Config): Promise<boolean> {
    if (!revision) { configError = 'Configuration revision is missing. Reload before saving.'; return false; }
    saving = true; configError = ''; notice = '';
    try {
      const saved = await request<Config>('config', { method: 'PUT', headers: { 'Content-Type': 'application/json', 'If-Match': revision }, body: JSON.stringify(next) });
      saved.reminders ??= [];
      config = saved; notice = 'Saved.';
      void Promise.all([loadStatus(), loadDiagnostics()]);
      return true;
    } catch (error) { configError = errorMessage(error); return false; }
    finally { saving = false; }
  }
  function openEditor(index: number | null) {
    if (!config || saving || settings) return;
    if (draft && dirty && !window.confirm('Discard your unsaved reminder changes?')) return;
    editing = index;
    draft = index === null ? { kind: 'every', every: '30m', message: '', app: '' } : { ...config.reminders[index], app: config.reminders[index].kind === 'watch' ? config.reminders[index].app : '' };
    originalDraft = JSON.stringify(draft);
    void loadApps();
    void tick().then(() => document.getElementById('message')?.focus());
  }
  function cancelEditor() {
    if (dirty && !window.confirm('Discard your unsaved reminder changes?')) return;
    draft = null; editing = null;
  }
  async function saveReminder() {
    if (!config || !draft) return;
    const reminder = { ...draft, kind: draft.app ? 'watch' as const : 'every' as const };
    if (!reminder.app) delete reminder.app;
    const reminders = [...config.reminders];
    if (editing === null) reminders.push(reminder); else reminders[editing] = reminder;
    if (await saveConfig({ ...config, reminders })) { draft = null; editing = null; }
  }
  async function toggleReminder(index: number, enabled: boolean) {
    if (!config) return;
    const reminders = config.reminders.map((rule, i) => i === index ? { ...rule, disabled: !enabled } : rule);
    await saveConfig({ ...config, reminders });
  }
  async function deleteReminder(index: number) {
    if (!config || !window.confirm(`Delete “${config.reminders[index].message}”? This cannot be undone.`)) return;
    await saveConfig({ ...config, reminders: config.reminders.filter((_, i) => i !== index) });
  }
  function openSettings() {
    if (!config || draft) return;
    settings = { ...config }; originalSettings = JSON.stringify(settings);
  }
  function cancelSettings() {
    if (dirty && !window.confirm('Discard your unsaved settings?')) return;
    settings = null;
  }
  async function saveSettings() {
    if (settings && await saveConfig(settings)) settings = null;
  }
  async function reloadConfig() {
    if (dirty && !window.confirm('Discard your unsaved changes and reload?')) return;
    draft = null; settings = null; editing = null;
    await loadConfig();
  }
  async function control(action: 'pause' | 'resume' | 'snooze') {
    controlling = true; notice = '';
    try {
      status = await request<Status>('control', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(action === 'resume' ? { action } : { action, duration: action === 'pause' ? '1h' : snoozeDuration }) });
      statusError = ''; notice = action === 'resume' ? 'Reminders resumed.' : action === 'pause' ? 'Reminders paused for 1 hour.' : `Reminders snoozed for ${snoozeDuration}.`;
    } catch (error) { statusError = errorMessage(error); }
    finally { controlling = false; }
  }
  function formatDate(value: string) {
    if (!value || value.startsWith('0001-01-01')) return 'Not known';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date);
  }
  function appName(id: string) {
    const match = apps.find(app => app.id.toLowerCase() === id.toLowerCase());
    if (match) return match.name;
    const part = id.split('.').filter(Boolean).slice(-1)[0] || id;
    return part.split(/[-_]/).filter(Boolean).map(word => word[0].toUpperCase() + word.slice(1)).join(' ') || id;
  }
  function formatInterval(value: string) {
    const match = /^(\d+(?:\.\d+)?)(s|m|h)$/.exec(value);
    if (!match) return value;
    const unit = match[2] === 's' ? 'second' : match[2] === 'm' ? 'minute' : 'hour';
    return `${match[1]} ${unit}${Number(match[1]) === 1 ? '' : 's'}`;
  }
  function navigate(next: 'reminders' | 'insights') {
    if (next === page) return;
    if (insightsBusy) return;
    if (page === 'insights' && insightsDirty && !window.confirm('Discard unsaved history settings?')) return;
    insightsDirty = false;
    page = next;
  }
</script>

<svelte:head><title>Nudge — {page === 'reminders' ? 'Reminders' : 'Insights'}</title><meta name="description" content="Manage Nudge reminders and insights." /></svelte:head>

<header class="site-header"><div class="brand"><Bell size={20} aria-hidden="true" /><span>nudge</span><nav class="main-nav" aria-label="Main navigation"><button type="button" aria-current={page === 'reminders' ? 'page' : undefined} disabled={insightsBusy} onclick={() => navigate('reminders')}>Reminders</button><button type="button" aria-current={page === 'insights' ? 'page' : undefined} onclick={() => navigate('insights')}>Insights</button></nav></div></header>
<main>
  {#if page === 'reminders'}
  <div class="page-heading"><div><p class="eyebrow">A little help remembering</p><h1>Reminders</h1></div><button class="button primary" type="button" onclick={() => openEditor(null)} disabled={!config || saving || !!settings}><Plus size={18} aria-hidden="true" /> Add Reminder</button></div>
  {#if notice}<div class="feedback success" role="status">{notice}<button class="text-button" type="button" onclick={() => notice = ''}>Dismiss</button></div>{/if}
  {#if statusError}<div class="feedback error" role="alert">Could not check reminders: {statusError}<button class="text-button" type="button" onclick={loadStatus}>Try again</button></div>{/if}
  {#if configError}<div class="feedback error" role="alert"><span>Could not save or load reminders: {configError}</span><button class="text-button" type="button" onclick={reloadConfig}>Reload saved configuration</button></div>{/if}

  <section class="status" aria-labelledby="status-heading">
    <div class="status-copy"><span class:active={!!status && status.active && status.scheduler_ready && !status.scheduler_error && status.notification_available} class="status-indicator" aria-hidden="true"></span><div><h2 id="status-heading">{loading ? 'Checking reminders…' : !status ? 'Reminder status unavailable' : status.scheduler_error || !status.notification_available ? 'Reminders need attention' : !status.scheduler_ready ? 'Reminders are starting' : isPaused ? 'Reminders are paused' : status.suppression_reason === 'quiet_hours' ? 'Quiet hours are on' : status.active ? 'Reminders are on' : 'Reminders are off'}</h2><p>{!status ? 'Check your connection and try again.' : status.scheduler_error ? status.scheduler_error : !status.notification_available ? 'Notifications are unavailable. Check Diagnostics.' : isPaused ? `Paused until ${formatDate(status.paused_until)}.` : status.suppression_reason === 'quiet_hours' ? 'Reminders will resume after quiet hours.' : !status.scheduler_ready ? 'Waiting for the reminder service.' : 'Nudge is ready to check reminders.'}</p></div></div>
    {#if status}<button class="button secondary" type="button" onclick={() => control(isPaused ? 'resume' : 'pause')} disabled={controlling}>{isPaused ? 'Resume reminders' : 'Pause for 1 hour'}</button>{/if}
  </section>
  <details class="snooze"><summary>Snooze (pause for a different time)</summary><div class="snooze-controls"><label for="snooze">Pause for</label><select id="snooze" bind:value={snoozeDuration}><option value="5m">5 minutes</option><option value="15m">15 minutes</option><option value="30m">30 minutes</option><option value="1h">1 hour</option></select><button class="button secondary" type="button" onclick={() => control('snooze')} disabled={controlling || !status}>Snooze</button></div></details>

  <section class="reminders" aria-labelledby="reminders-heading">
    <div class="section-heading"><h2 id="reminders-heading">Your reminders</h2>{#if config}<span>{config.reminders.length} {config.reminders.length === 1 ? 'reminder' : 'reminders'}</span>{/if}</div>
    {#if !config}<div class="empty">{loading ? 'Loading reminders…' : 'Your reminders could not be loaded.'}</div>
    {:else if config.reminders.length === 0}<div class="empty"><strong>No reminders yet</strong><p>Add a reminder for something you’d like to remember.</p></div>
    {:else}<div class="reminder-list">{#each config.reminders as rule, index}
      <article class="reminder"><div class="reminder-copy"><h3>{rule.message}</h3><p>Every {formatInterval(rule.every)} · {rule.kind === 'watch' ? `While using ${appName(rule.app || '')}` : 'Any app'}{rule.disabled ? ' · Off' : ''}</p></div><div class="reminder-actions"><label class="switch-label"><input type="checkbox" role="switch" checked={!rule.disabled} onchange={(event) => { const enabled = event.currentTarget.checked; event.currentTarget.checked = !enabled; void toggleReminder(index, enabled); }} disabled={saving || !!draft || !!settings} aria-label={`Reminders for ${rule.message}`} /><span>{rule.disabled ? 'Off' : 'On'}</span></label><button class="button quiet" type="button" onclick={() => openEditor(index)} disabled={saving || !!settings}>Edit<span class="sr-only"> {rule.message}</span></button><button class="button quiet danger" type="button" onclick={() => deleteReminder(index)} disabled={saving || !!draft || !!settings}>Delete<span class="sr-only"> {rule.message}</span></button></div></article>
    {/each}</div>{/if}
  </section>

  {#if draft}<section class="editor" aria-labelledby="editor-heading"><h2 id="editor-heading">{editing === null ? 'Add Reminder' : 'Edit Reminder'}</h2><form onsubmit={(event) => { event.preventDefault(); void saveReminder(); }}>
    <label for="message">Remind me to</label><input id="message" bind:value={draft.message} required placeholder="Take a break" />
    <label for="frequency">Remind me every</label><Interval id="frequency" bind:value={draft.every} />
    <label for="app-choice">Only while using</label><select id="app-choice" bind:value={draft.app}><option value="">Any app</option>{#if draft.app && !apps.some(app => app.id === draft?.app)}<option value={draft.app}>{appName(draft.app)} (not currently open)</option>{/if}{#each apps as app}<option value={app.id}>{app.name}</option>{/each}</select>
    {#if appsError}<p class="help">Open apps are unavailable: {appsError} You can still use any app or enter an app ID below.</p>{:else if apps.length === 0}<p class="help">No open apps found. You can still use any app or enter an app ID below.</p>{/if}
    <details class="advanced"><summary>Advanced: enter an app ID</summary><label for="app-id">App ID</label><input id="app-id" bind:value={draft.app} placeholder="e.g. firefox" /><p class="help">Leave blank to remind you in any app. App IDs are saved exactly as entered.</p></details>
    <div class="form-actions"><button class="button quiet" type="button" onclick={cancelEditor} disabled={saving}>Cancel</button><button class="button primary" type="submit" disabled={saving}>{saving ? 'Saving…' : editing === null ? 'Add Reminder' : 'Save'}</button></div>
  </form></section>{/if}

  <div class="additional"><details class="disclosure"><summary><span><strong>Settings</strong><small>Quiet hours and time between notifications</small></span><ChevronDown size={18} aria-hidden="true" /></summary><div class="disclosure-content">{#if !settings}<p>Choose when reminders are quiet and how much time to leave between notifications.</p><button class="button secondary" type="button" onclick={openSettings} disabled={!config || !!draft || saving}>Edit settings</button>{:else}<form onsubmit={(event) => { event.preventDefault(); void saveSettings(); }}><label for="cooldown">Time between notifications</label><Interval id="cooldown" bind:value={settings.cooldown} /><p class="help">The minimum time between any two notifications.</p><div class="time-fields"><div><label for="quiet-start">Quiet hours start</label><input id="quiet-start" type="time" bind:value={settings.quiet_start} /></div><div><label for="quiet-end">Quiet hours end</label><input id="quiet-end" type="time" bind:value={settings.quiet_end} /></div></div><p class="help">Leave both times blank to turn off quiet hours.</p><div class="form-actions"><button class="button quiet" type="button" onclick={cancelSettings} disabled={saving}>Cancel</button><button class="button primary" type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save settings'}</button></div></form>{/if}</div></details>
  <details class="disclosure"><summary><span><strong>Diagnostics</strong><small>Connection and service details</small></span><ChevronDown size={18} aria-hidden="true" /></summary><div class="disclosure-content">{#if diagnosticsError}<p role="alert">{diagnosticsError} <button class="text-button" type="button" onclick={loadDiagnostics}>Try again</button></p>{/if}{#if diagnostics}<dl><div><dt>Notification executable <small>Availability only; delivery is not verified.</small></dt><dd>{diagnostics.notification_available ? 'Available' : 'Unavailable'}</dd></div><div><dt>Niri connection</dt><dd>{diagnostics.niri_error || 'No errors reported'}</dd></div><div><dt>Configuration</dt><dd>{diagnostics.config_error || 'No errors reported'}</dd></div><div><dt>Scheduler</dt><dd>{status?.scheduler_error || (status ? 'No errors reported' : 'Unavailable')}</dd></div><div><dt>Focused app</dt><dd>{!status?.focus_known ? 'Unavailable' : status.focused_app || 'No app focused'}</dd></div><div><dt>Next reminder</dt><dd>{status ? formatDate(status.next_reminder) : 'Unavailable'}<small>{status?.next_description}</small></dd></div><div><dt>Matching reminders</dt><dd>{status?.matching_rules_known ? status.active_rules.join(', ') || 'None' : 'Unavailable'}</dd></div><div><dt>Version</dt><dd>{diagnostics.version || 'Unknown'}</dd></div></dl>{:else if !diagnosticsError}<p>Loading diagnostics…</p>{/if}<button class="button quiet" type="button" onclick={() => { void loadStatus(); void loadDiagnostics(); void loadApps(); }}>Refresh diagnostics</button></div></details></div>
  {:else}
    <Insights onDirtyChange={(value, busy) => { insightsDirty = value; insightsBusy = busy; }} />
  {/if}
  <footer>Nudge · A local reminder service</footer>
</main>
