<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { slide } from 'svelte/transition';
  import { cubicOut } from 'svelte/easing';
  import { prefersReducedMotion } from 'svelte/motion';
  import { Bell, Plus, ChevronDown, Timer, AppWindow, Pause, Play } from 'lucide-svelte';
  import Interval from './lib/Interval.svelte';
  import Insights from './Insights.svelte';
  import { apiFetch } from './transport';

  type Reminder = { kind: 'watch' | 'every'; app?: string; every: string; message: string; disabled?: boolean };
  type Config = { reminders: Reminder[]; cooldown: string; paused_until: string; quiet_start?: string; quiet_end?: string };
  type Status = { active: boolean; paused_until: string; focused_app: string; focus_known: boolean; active_rules: string[]; matching_rules_known: boolean; scheduler_ready: boolean; suppression_reason: string; next_reminder: string; next_description: string; scheduler_error: string; niri_error: string; notification_available: boolean; version: string };
  type Diagnostics = { version: string; niri_error: string; notification_available: boolean; config_error: string };
  type App = { id: string; name: string };

  let page = $state<'reminders' | 'settings' | 'insights' | 'diagnostics'>('reminders');
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
  let snoozeDuration = $state('1h');
  let editorDialog = $state<HTMLDialogElement>();
  let editorActive = $state(false);
  let editorGeneration = 0;
  let editorSession = $state(0);
  // UI-only identity keeps duplicate reminders distinct during list transitions.
  let reminderKeys = $state<number[]>([]);
  let nextReminderKey = 0;
  let revision = '';
  let originalDraft = '';
  let originalSettings = '';
  let dirty = $derived((editorActive && draft !== null && JSON.stringify(draft) !== originalDraft) || (settings !== null && JSON.stringify(settings) !== originalSettings));
  let isPaused = $derived(status?.suppression_reason === 'paused' || (!!status?.paused_until && new Date(status.paused_until).getTime() > Date.now()));
  let focusProblem = $derived(!!status?.active && !!status.scheduler_ready && !!status.niri_error && !!config?.reminders.some(rule => rule.kind === 'watch' && !rule.disabled));

  async function request<T>(path: string, options?: RequestInit): Promise<T> {
    const response = await apiFetch(`/api/v1/${path}`, options);
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
      reminderKeys = next.reminders.map(() => ++nextReminderKey);
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
  async function saveConfig(next: Config, keys = reminderKeys): Promise<boolean> {
    if (!revision) { configError = 'Configuration revision is missing. Reload before saving.'; return false; }
    saving = true; configError = ''; notice = '';
    try {
      const saved = await request<Config>('config', { method: 'PUT', headers: { 'Content-Type': 'application/json', 'If-Match': revision }, body: JSON.stringify(next) });
      saved.reminders ??= [];
      reminderKeys = keys;
      config = saved;
      void Promise.all([loadStatus(), loadDiagnostics()]);
      return true;
    } catch (error) { configError = errorMessage(error); return false; }
    finally { saving = false; }
  }
  function openEditor(index: number | null) {
    if (!config || saving || settings || editorActive) return;
    const generation = ++editorGeneration;
    ++editorSession;
    editorActive = true;
    editing = index;
    draft = index === null ? { kind: 'every', every: '30m', message: '', app: '' } : { ...config.reminders[index], app: config.reminders[index].kind === 'watch' ? config.reminders[index].app : '' };
    originalDraft = JSON.stringify(draft);
    void loadApps();
    void tick().then(() => {
      if (!editorActive || generation !== editorGeneration || !editorDialog) return;
      editorDialog.classList.remove('instant-close');
      editorDialog.showModal();
      document.getElementById('message')?.focus();
    });
  }
  function closeEditor() {
    const generation = ++editorGeneration;
    const keyboardFocus = !!document.activeElement?.matches(':focus-visible');
    editorDialog?.classList.toggle('instant-close', keyboardFocus);
    editorActive = false;
    editorDialog?.close();
    const animated = !keyboardFocus && !prefersReducedMotion.current && CSS.supports('transition-behavior', 'allow-discrete') && CSS.supports('overlay', 'auto');
    window.setTimeout(() => {
      if (generation === editorGeneration && !editorActive) { draft = null; editing = null; }
    }, animated ? 180 : 0);
  }
  function cancelEditor() {
    if (saving || (dirty && !window.confirm('Discard your unsaved reminder changes?'))) return;
    closeEditor();
  }
  async function saveReminder() {
    if (!config || !draft) return;
    const reminder = { ...draft, kind: draft.app ? 'watch' as const : 'every' as const };
    if (!reminder.app) delete reminder.app;
    const reminders = [...config.reminders];
    if (editing === null) reminders.push(reminder); else reminders[editing] = reminder;
    const keys = editing === null ? [...reminderKeys, ++nextReminderKey] : reminderKeys;
    if (await saveConfig({ ...config, reminders }, keys)) {
      notice = editing === null ? 'Reminder added.' : 'Reminder updated.';
      closeEditor();
    }
  }
  async function toggleReminder(index: number, enabled: boolean) {
    if (!config || saving) return;
    const previous = config;
    const reminders = config.reminders.map((rule, i) => i === index ? { ...rule, disabled: !enabled } : rule);
    const next = { ...config, reminders };
    config = next;
    if (await saveConfig(next)) notice = enabled ? 'Reminder turned on.' : 'Reminder turned off.';
    else config = previous;
  }
  async function deleteReminder(index: number) {
    if (!config || !window.confirm(`Delete “${config.reminders[index].message}”? This cannot be undone.`)) return;
    const keys = reminderKeys.filter((_, i) => i !== index);
    if (await saveConfig({ ...config, reminders: config.reminders.filter((_, i) => i !== index) }, keys)) {
      notice = 'Reminder deleted.';
      await tick();
      const nextKey = keys[Math.min(index, keys.length - 1)];
      (document.querySelector<HTMLButtonElement>(`[data-reminder-key="${nextKey}"] .reminder-actions button:not(.danger)`) ?? document.querySelector<HTMLButtonElement>('.page-heading .primary'))?.focus();
    }
  }
  function openSettings() {
    if (!config || editorActive) return;
    settings = { ...config }; originalSettings = JSON.stringify(settings);
    void tick().then(() => document.getElementById('cooldown')?.focus());
  }
  function cancelSettings() {
    if (dirty && !window.confirm('Discard your unsaved settings?')) return;
    settings = null;
    void tick().then(() => document.querySelector<HTMLButtonElement>('.additional .disclosure .secondary')?.focus());
  }
  function closeSettingsDisclosure(event: MouseEvent) {
    if (!settings) return;
    event.preventDefault();
    if (saving || (dirty && !window.confirm('Discard your unsaved settings?'))) return;
    settings = null;
    if (event.currentTarget instanceof HTMLElement) event.currentTarget.parentElement?.removeAttribute('open');
  }
  async function saveSettings() {
    if (settings && await saveConfig(settings)) {
      settings = null; notice = 'Settings saved.';
      await tick();
      document.querySelector<HTMLButtonElement>('.additional .disclosure .secondary')?.focus();
    }
  }
  async function reloadConfig() {
    if (dirty && !window.confirm('Discard your unsaved changes and reload?')) return;
    ++editorGeneration; editorActive = false; editorDialog?.close();
    draft = null; settings = null; editing = null;
    await loadConfig();
  }
  async function control(action: 'pause' | 'resume' | 'snooze') {
    if (controlling) return;
    controlling = true; notice = '';
    try {
      const next = await request<Status>('control', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(action === 'resume' ? { action } : { action, duration: action === 'pause' ? '1h' : snoozeDuration }) });
      const restoreFocus = !!document.activeElement?.closest('.status');
      status = next;
      statusError = ''; notice = action === 'resume' ? 'Reminders resumed.' : action === 'pause' ? 'Reminders paused for 1 hour.' : `Reminders paused for ${formatInterval(snoozeDuration)}.`;
      if (restoreFocus) {
        await tick();
        document.querySelector<HTMLElement>('.status-action')?.focus();
      }
    } catch (error) { statusError = errorMessage(error); }
    finally { controlling = false; }
  }
  function pauseMenuKeydown(event: KeyboardEvent) {
    const menu = event.currentTarget instanceof Element ? event.currentTarget.closest('details') : null;
    if (event.key !== 'Escape' || !menu?.open) return;
    event.preventDefault();
    menu.open = false;
    menu.querySelector<HTMLElement>('summary')?.focus();
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
  function navigate(next: 'reminders' | 'settings' | 'insights' | 'diagnostics') {
    if (next === page) return;
    if (insightsBusy) return;
    if (page === 'insights' && insightsDirty && !window.confirm('Discard unsaved history settings?')) return;
    if ((page === 'reminders' || page === 'settings') && (saving || (dirty && !window.confirm('Discard your unsaved changes?')))) return;
    if (page === 'reminders' || page === 'settings') { ++editorGeneration; editorActive = false; draft = null; settings = null; editing = null; }
    insightsDirty = false;
    page = next;
  }
</script>

<svelte:head><title>Nudge — {page[0].toUpperCase() + page.slice(1)}</title><meta name="description" content="Manage Nudge reminders and insights." /></svelte:head>

<header class="site-header"><div class="brand"><Bell size={20} aria-hidden="true" /><span>nudge</span><nav class="main-nav" aria-label="Main navigation"><button type="button" aria-current={page === 'reminders' ? 'page' : undefined} disabled={insightsBusy} onclick={() => navigate('reminders')}>Reminders</button><button type="button" aria-current={page === 'settings' ? 'page' : undefined} disabled={insightsBusy} onclick={() => navigate('settings')}>Settings</button><button type="button" aria-current={page === 'insights' ? 'page' : undefined} onclick={() => navigate('insights')}>Insights</button><button type="button" aria-current={page === 'diagnostics' ? 'page' : undefined} disabled={insightsBusy} onclick={() => navigate('diagnostics')}>Diagnostics</button></nav></div></header>
<a class="skip-link" href="#main-content">Skip to content</a>
<main id="main-content" data-page={page}>
  {#if page !== 'insights'}
  {#if page === 'settings'}<div class="page-heading"><div><h1>Settings</h1><p class="page-intro">Choose when reminders can reach you.</p></div></div>{:else if page === 'diagnostics'}<div class="page-heading"><div><h1>Diagnostics</h1><p class="page-intro">Check the reminder service and its connections.</p></div></div>{/if}
  {#if page === 'reminders'}
  <div class="page-heading"><div><p class="eyebrow">A little space for what matters</p><h1>Your reminders</h1><p class="page-intro">Set reminders for any time, or while using a particular app.</p></div><button class="button primary" type="button" onclick={() => openEditor(null)} disabled={!config || saving || !!settings}><Plus size={18} aria-hidden="true" /> Add reminder</button></div>
  <div class="sr-only" role="status" aria-atomic="true">{notice}</div>
  {#if configError}<div class="feedback error" role="alert"><span>Could not save or load reminders: {configError}</span><button class="text-button" type="button" onclick={reloadConfig}>Reload saved configuration</button></div>{/if}

  <section class="status" aria-labelledby="status-heading">
    <div class="status-copy"><span class:active={!!status && status.active && status.scheduler_ready && !status.scheduler_error && status.notification_available && !focusProblem && !statusError} class="status-indicator" aria-hidden="true"></span><div><h2 id="status-heading">{loading ? 'Checking reminders…' : !status ? 'Reminder status unavailable' : statusError ? 'Could not change reminders' : status.scheduler_error || !status.notification_available || focusProblem ? 'Reminders need attention' : !status.scheduler_ready ? 'Reminders are starting' : isPaused ? 'Reminders are paused' : status.suppression_reason === 'quiet_hours' ? 'Quiet hours are on' : status.active ? 'Reminders are on' : 'Reminders are off'}</h2>{#if statusError}<p role="alert">Could not {status ? 'change' : 'check'} reminders: {statusError}. {#if status}Try the control again.{:else}<button class="text-button" type="button" onclick={loadStatus}>Try again</button>{/if}</p>{:else}<p>{!status ? 'Check your connection and try again.' : status.scheduler_error ? 'The reminder service encountered an error. See Diagnostics for details.' : !status.notification_available ? 'Notifications are unavailable. Check Diagnostics.' : focusProblem ? 'App-specific reminders cannot detect which app is in use. Check Diagnostics.' : isPaused ? `Paused until ${formatDate(status.paused_until)}.` : status.suppression_reason === 'quiet_hours' ? 'Reminders will resume after quiet hours.' : !status.scheduler_ready ? 'Waiting for the reminder service.' : 'Nudge is ready to check reminders.'}</p>{/if}</div></div>
    {#if status}
      {#if isPaused}<button class="button secondary status-action" type="button" onclick={() => control('resume')} aria-disabled={controlling}><Play size={16} aria-hidden="true" /> {controlling ? 'Resuming…' : 'Resume reminders'}</button>
      {:else}<details class="pause-menu"><summary class="status-action" onkeydown={pauseMenuKeydown}><Pause size={16} aria-hidden="true" /> Pause reminders <ChevronDown size={16} aria-hidden="true" /></summary><div class="pause-controls"><label for="snooze">Pause for</label><select id="snooze" bind:value={snoozeDuration} onkeydown={pauseMenuKeydown}><option value="5m">5 minutes</option><option value="15m">15 minutes</option><option value="30m">30 minutes</option><option value="1h">1 hour</option></select><button class="button secondary" type="button" onclick={() => control(snoozeDuration === '1h' ? 'pause' : 'snooze')} onkeydown={pauseMenuKeydown} aria-disabled={controlling}>{controlling ? 'Pausing…' : 'Pause'}</button></div></details>{/if}
    {/if}
  </section>
  {/if}
  {#if page === 'settings' && configError}<div class="feedback error" role="alert"><span>Could not save or load settings: {configError}</span><button class="text-button" type="button" onclick={reloadConfig}>Reload saved configuration</button></div>{/if}
  <div class="workspace">
  {#if page === 'reminders'}
  <div class="workspace-main">


  <section class="reminders" aria-labelledby="reminders-heading">
    <div class="section-heading"><h2 id="reminders-heading">All reminders</h2>{#if config}<span>{config.reminders.filter(rule => !rule.disabled).length} enabled · {config.reminders.length} total</span>{/if}</div>
    {#if !config}<div class="empty">{loading ? 'Loading reminders…' : 'Your reminders could not be loaded.'}</div>
    {:else}<div class="reminder-list">{#each config.reminders as rule, index (reminderKeys[index])}
      <article class="reminder" class:disabled={rule.disabled} data-reminder-key={reminderKeys[index]} transition:slide={{ duration: prefersReducedMotion.current ? 0 : 170, easing: cubicOut }} onfocusin={(event) => { event.currentTarget.getAnimations().forEach((animation) => animation.finish()); }} onoutrostart={(event) => { event.currentTarget.inert = true; event.currentTarget.classList.add('leaving'); }}><div class="reminder-info"><span class="reminder-icon" aria-hidden="true">{#if rule.kind === 'watch'}<AppWindow size={19} />{:else}<Timer size={19} />{/if}</span><div class="reminder-copy"><h3>{rule.message}</h3><p>Every {formatInterval(rule.every)} · {rule.kind === 'watch' ? `While using ${appName(rule.app || '')}` : 'Any app'}</p></div></div><div class="reminder-actions"><label class="switch-label"><input type="checkbox" role="switch" checked={!rule.disabled} onchange={(event) => { if (saving) event.currentTarget.checked = !event.currentTarget.checked; else void toggleReminder(index, event.currentTarget.checked); }} disabled={editorActive || !!settings} aria-disabled={saving} aria-label={`Reminders for ${rule.message}`} /><span>{rule.disabled ? 'Off' : 'On'}</span></label><button class="button quiet" type="button" onclick={() => openEditor(index)} disabled={saving || !!settings}>Edit<span class="sr-only"> {rule.message}</span></button><button class="button quiet danger" type="button" onclick={() => deleteReminder(index)} disabled={saving || editorActive || !!settings}>Delete<span class="sr-only"> {rule.message}</span></button></div></article>
    {/each}</div>{#if config.reminders.length === 0}<div class="empty"><Bell size={22} aria-hidden="true" /><div><strong>No reminders yet</strong><p>Choose what to remember and how often. You can even choose a particular app.</p></div></div>{/if}{/if}
  </section>
  <details class="why-missing"><summary>Why might I not see a reminder?</summary><p>Check that the reminder is on and that Nudge isn’t paused or in quiet hours. App-specific reminders only run while that app is focused. The time between notifications can also delay a reminder.</p><p>Your desktop’s notification settings, including Do Not Disturb, can hide notifications even when Nudge sends them.</p></details>
  </div>
  {/if}
  <aside class="workspace-side" aria-label={page === 'settings' ? 'Settings' : 'Diagnostics'}>
  <div class="additional"><details class="disclosure"><summary onclick={closeSettingsDisclosure}><span><strong>Settings</strong><small>Quiet hours and time between notifications</small></span><ChevronDown size={18} aria-hidden="true" /></summary><div class="disclosure-content">{#if !settings}<p>Choose when reminders are quiet and how much time to leave between notifications.</p><button class="button secondary" type="button" onclick={openSettings} disabled={!config || editorActive || saving}>Edit settings</button>{#if notice === 'Settings saved.'}<p class="help">Settings saved.</p>{/if}{:else}<form onsubmit={(event) => { event.preventDefault(); void saveSettings(); }}><label for="cooldown">Time between notifications</label><Interval id="cooldown" bind:value={settings.cooldown} /><p class="help">The minimum time between any two notifications.</p><div class="time-fields"><div><label for="quiet-start">Quiet hours start</label><input id="quiet-start" type="time" bind:value={settings.quiet_start} /></div><div><label for="quiet-end">Quiet hours end</label><input id="quiet-end" type="time" bind:value={settings.quiet_end} /></div></div><p class="help">Leave both times blank to turn off quiet hours.</p><div class="form-actions"><button class="button quiet" type="button" onclick={cancelSettings} disabled={saving}>Cancel</button><button class="button primary" type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save settings'}</button></div></form>{/if}</div></details>
  <details class="disclosure"><summary><span><strong>Diagnostics</strong><small>Connection and service details</small></span><ChevronDown size={18} aria-hidden="true" /></summary><div class="disclosure-content">{#if diagnosticsError}<p role="alert">{diagnosticsError} <button class="text-button" type="button" onclick={loadDiagnostics}>Try again</button></p>{/if}{#if diagnostics}<dl><div><dt>Notification executable <small>Availability only; delivery is not verified.</small></dt><dd>{diagnostics.notification_available ? 'Available' : 'Unavailable'}</dd></div><div><dt>Niri connection</dt><dd>{diagnostics.niri_error || 'No errors reported'}</dd></div><div><dt>Configuration</dt><dd>{diagnostics.config_error || 'No errors reported'}</dd></div><div><dt>Scheduler</dt><dd>{status?.scheduler_error || (status ? 'No errors reported' : 'Unavailable')}</dd></div><div><dt>Focused app</dt><dd>{!status?.focus_known ? 'Unavailable' : status.focused_app || 'No app focused'}</dd></div><div><dt>Next reminder</dt><dd>{status ? formatDate(status.next_reminder) : 'Unavailable'}<small>{status?.next_description}</small></dd></div><div><dt>Matching reminders</dt><dd>{status?.matching_rules_known ? status.active_rules.join(', ') || 'None' : 'Unavailable'}</dd></div><div><dt>Version</dt><dd>{diagnostics.version || 'Unknown'}</dd></div></dl>{:else if !diagnosticsError}<p>Loading diagnostics…</p>{/if}<button class="button quiet" type="button" onclick={() => { void loadStatus(); void loadDiagnostics(); void loadApps(); }}>Refresh diagnostics</button></div></details></div>
  </aside>
  </div>

  {#if draft}<dialog class="editor" bind:this={editorDialog} inert={!editorActive} aria-labelledby="editor-heading" oncancel={(event) => { event.preventDefault(); cancelEditor(); }} onclick={(event) => { if (editorDialog && event.target === editorDialog) { const rect = editorDialog.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) cancelEditor(); } }}><div class="editor-heading"><h2 id="editor-heading">{editing === null ? 'New reminder' : 'Edit reminder'}</h2></div>{#key editorSession}<form onsubmit={(event) => { event.preventDefault(); void saveReminder(); }}>
    {#if configError}<div class="feedback error" role="alert"><span>Could not save reminder: {configError}</span><button class="text-button" type="button" onclick={reloadConfig} disabled={saving}>Reload saved configuration</button></div>{/if}
    <div class="editor-section"><span class="editor-number" aria-hidden="true">01</span><div><label for="message">What would you like to remember?</label><input id="message" bind:value={draft.message} required placeholder="Take a break" /></div></div>
    <div class="editor-section"><span class="editor-number" aria-hidden="true">02</span><div><label for="frequency">How often?</label><Interval id="frequency" bind:value={draft.every} /><p class="help">The shared cooldown and quiet hours can delay reminders.</p></div></div>
    <div class="editor-section"><span class="editor-number" aria-hidden="true">03</span><div><label for="app-choice">Where?</label><select id="app-choice" bind:value={draft.app}><option value="">Any app</option>{#if draft.app && !apps.some(app => app.id === draft?.app)}<option value={draft.app}>{appName(draft.app)} (not currently open)</option>{/if}{#each apps as app}<option value={app.id}>{app.name}</option>{/each}</select>
    {#if appsError}<p class="help">Open apps are unavailable. You can enter an app ID below.</p>{:else if apps.length === 0}<p class="help">No open apps found. You can enter an app ID below.</p>{/if}
    <details class="advanced"><summary>Enter an app ID</summary><label for="app-id">App ID</label><input id="app-id" bind:value={draft.app} placeholder="e.g. firefox" /><p class="help">Leave blank to remind you in any app. App IDs are saved exactly as entered.</p></details></div></div>
    <div class="form-actions"><button class="button quiet" type="button" onclick={cancelEditor} disabled={saving}>Cancel</button><button class="button primary" type="submit" disabled={saving}>{saving ? 'Saving…' : editing === null ? 'Add reminder' : 'Save changes'}</button></div>
  </form>{/key}</dialog>{/if}

  {:else}
    <Insights onDirtyChange={(value, busy) => { insightsDirty = value; insightsBusy = busy; }} />
  {/if}
  <footer>Nudge · A local reminder service</footer>
</main>
