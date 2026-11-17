<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { slide } from 'svelte/transition';
  import { cubicOut } from 'svelte/easing';
  import { prefersReducedMotion } from 'svelte/motion';
  import { Bell, Plus, ChevronDown, Timer, AppWindow, Pause, Play, X } from 'lucide-svelte';
  import Interval from './lib/Interval.svelte';
  import Insights from './Insights.svelte';
  import { apiFetch } from './transport';

  type Reminder = { kind: 'watch' | 'every'; app?: string; every: string; message: string; disabled?: boolean };
  type ElapsedReminder = { app: string; after: string; repeat?: string; message: string; disabled?: boolean };
  type Draft = { kind: 'watch' | 'every' | 'elapsed'; app: string; every: string; repeat: string; message: string; disabled?: boolean };
  type Config = { reminders: Reminder[]; elapsed_reminders?: ElapsedReminder[]; cooldown: string; paused_until: string; quiet_start?: string; quiet_end?: string };
  type Status = { active: boolean; paused_until: string; focused_app: string; focus_known: boolean; active_rules: string[]; matching_rules_known: boolean; scheduler_ready: boolean; suppression_reason: string; next_reminder: string; next_description: string; scheduler_error: string; niri_error: string; notification_available: boolean; version: string };
  type Diagnostics = { version: string; niri_error: string; notification_available: boolean; config_error: string };
  type App = { id: string; name: string };
  type ReminderToast = { id: number; title: string; body: string; accepted: boolean };

  let page = $state<'reminders' | 'settings' | 'insights' | 'diagnostics'>('reminders');
  type Theme = 'forest' | 'mocha' | 'latte' | 'nord' | 'rose' | 'tokyo' | 'gruvbox';
  const themes: { id: Theme; label: string }[] = [
    { id: 'forest', label: 'Nudge Forest' }, { id: 'mocha', label: 'Catppuccin Mocha Lavender' },
    { id: 'latte', label: 'Catppuccin Latte' }, { id: 'nord', label: 'Nord' },
    { id: 'rose', label: 'Rosé Pine' }, { id: 'tokyo', label: 'Tokyo Night' },
    { id: 'gruvbox', label: 'Gruvbox Dark' },
  ];
  let theme = $state<Theme>('forest');
  let themeError = $state('');
  function changeTheme(value: Theme) {
    try {
      localStorage.setItem('nudge-theme', value);
      theme = value;
      document.documentElement.dataset.theme = value;
      themeError = '';
    } catch {
      themeError = 'Appearance could not be saved on this device.';
      // Keep the current theme when persistence is unavailable.
    }
  }
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
  let toast = $state<ReminderToast | null>(null);
  let toastError = $state('');
  let toastBusy = $state(false);
  let toastSnoozed = false;
  let toastPolling = false;
  let toastEpoch = 0;
  let loading = $state(true);
  let saving = $state(false);
  let controlling = $state(false);
  let draft = $state<Draft | null>(null);
  let editing = $state<number | null>(null);
  let editingSource = $state<'reminders' | 'elapsed_reminders'>('reminders');
  let repeatEnabled = $state(false);
  let usageInitialized = false;
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
  let dirty = $derived((editorActive && draft !== null && JSON.stringify({ draft, repeatEnabled }) !== originalDraft) || (settings !== null && JSON.stringify(settings) !== originalSettings));
  let isPaused = $derived(status?.suppression_reason === 'paused' || (!!status?.paused_until && new Date(status.paused_until).getTime() > Date.now()));
  let focusProblem = $derived(!!status?.active && !!status.scheduler_ready && !!status.niri_error && (!!config?.reminders.some(rule => rule.kind === 'watch' && !rule.disabled) || !!config?.elapsed_reminders?.some(rule => !rule.disabled)));

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
  function isReminderToast(value: unknown): value is ReminderToast {
    return typeof value === 'object' && value !== null && 'id' in value && Number.isSafeInteger(value.id) &&
      'title' in value && typeof value.title === 'string' && 'body' in value && typeof value.body === 'string' &&
      'accepted' in value && typeof value.accepted === 'boolean';
  }
  async function pollToast() {
    if (toastPolling || editorActive || document.hidden || !document.hasFocus()) return;
    toastPolling = true;
    const epoch = toastEpoch;
    try {
      const result = await request<{ toast: unknown }>('toast', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: 'poll' }) });
      if (epoch !== toastEpoch || editorActive || document.hidden || !document.hasFocus()) return;
      if (!isReminderToast(result.toast)) { toast = null; return; }
      if (toast?.id === result.toast.id) return;
      if (toast) toast = null;
      if (!result.toast.accepted) {
        if (editorActive || document.hidden || !document.hasFocus()) return;
        await request('toast', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: 'claim', id: result.toast.id }) });
      }
      if (epoch === toastEpoch && !editorActive && !document.hidden && document.hasFocus()) {
        toast = result.toast;
        toastSnoozed = false;
        toastError = '';
      }
    } catch {
      toast = null;
      // The daemon falls back to a native notification if an offer is not claimed.
    } finally {
      toastPolling = false;
    }
  }
  async function actOnToast(action: 'snooze' | 'dismiss') {
    if (!toast || toastBusy) return;
    toastBusy = true;
    toastError = '';
    try {
      if (action === 'snooze' && !toastSnoozed) {
        status = await request<Status>('control', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: 'snooze', duration: '5m' }) });
        toastSnoozed = true;
      }
      const response = await apiFetch('/api/v1/toast', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: 'dismiss', id: toast.id }) });
      if (!response.ok && response.status !== 404) throw new Error(`Dismiss failed (${response.status})`);
      toastEpoch++;
      toast = null;
    } catch (error) {
      toastError = errorMessage(error);
    } finally {
      toastBusy = false;
    }
  }
  onMount(() => {
    try {
      const saved = themes.find(option => option.id === localStorage.getItem('nudge-theme'));
      if (saved) theme = saved.id;
      document.documentElement.dataset.theme = theme;
    } catch {
      themeError = 'Saved appearance is unavailable on this device.';
    }
    void (async () => { await Promise.all([loadStatus(), loadConfig(), loadApps(), loadDiagnostics()]); loading = false; })();
    const timer = window.setInterval(() => { if (!document.hidden) void loadStatus(); }, 15000);
    const toastTimer = import.meta.env.VITE_NUDGE_DESKTOP === '1' ? window.setInterval(() => { void pollToast(); }, 750) : undefined;
    const releaseToast = () => { toastEpoch++; toast = null; };
    window.addEventListener('blur', releaseToast);
    const beforeUnload = (event: BeforeUnloadEvent) => { if (dirty || saving || insightsDirty || insightsBusy) { event.preventDefault(); event.returnValue = ''; } };
    window.addEventListener('beforeunload', beforeUnload);
    return () => { window.clearInterval(timer); if (toastTimer !== undefined) window.clearInterval(toastTimer); window.removeEventListener('beforeunload', beforeUnload); window.removeEventListener('blur', releaseToast); };
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
  function openEditor(index: number | null, source: 'reminders' | 'elapsed_reminders' = 'reminders') {
    if (!config || saving || settings || editorActive) return;
    const legacy = index === null || source !== 'reminders' ? null : config.reminders[index];
    const elapsed = index === null || source !== 'elapsed_reminders' ? null : config.elapsed_reminders?.[index];
    if (index !== null && !legacy && !elapsed) return;
    const generation = ++editorGeneration;
    ++editorSession;
    editorActive = true;
    toastEpoch++;
    toast = null;
    editing = index;
    editingSource = source;
    usageInitialized = !!elapsed;
    repeatEnabled = !!elapsed?.repeat;
    draft = elapsed
      ? { kind: 'elapsed', app: elapsed.app, every: elapsed.after, repeat: elapsed.repeat || '30m', message: elapsed.message, disabled: elapsed.disabled }
      : legacy
        ? { kind: legacy.kind, app: legacy.app || '', every: legacy.every, repeat: '30m', message: legacy.message, disabled: legacy.disabled }
        : { kind: 'every', app: '', every: '30m', repeat: '30m', message: '' };
    originalDraft = JSON.stringify({ draft, repeatEnabled });
    void loadApps();
    void tick().then(() => {
      if (!editorActive || generation !== editorGeneration || !editorDialog) return;
      editorDialog.classList.remove('instant-close');
      editorDialog.showModal();
      document.getElementById('message')?.focus();
    });
  }
  function selectReminderKind(kind: Draft['kind']) {
    if (!draft) return;
    if (kind === 'elapsed' && !usageInitialized) {
      usageInitialized = true;
      if (editing === null) draft.every = '10m';
      if (!draft.message.trim()) draft.message = 'Still doing what you intended?';
    }
    draft.kind = kind;
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
    const reminders = [...config.reminders];
    const elapsed = [...(config.elapsed_reminders || [])];
    const keys = [...reminderKeys];
    let oldKey: number | undefined;
    if (editing !== null) {
      if (editingSource === 'reminders') {
        reminders.splice(editing, 1);
        [oldKey] = keys.splice(editing, 1);
      } else {
        elapsed.splice(editing, 1);
      }
    }
    if (draft.kind === 'elapsed') {
      const rule: ElapsedReminder = { app: draft.app, after: draft.every, message: draft.message, disabled: draft.disabled };
      if (repeatEnabled) rule.repeat = draft.repeat;
      elapsed.splice(editing !== null && editingSource === 'elapsed_reminders' ? editing : elapsed.length, 0, rule);
    } else {
      const rule: Reminder = { kind: draft.kind, every: draft.every, message: draft.message, disabled: draft.disabled };
      if (draft.kind === 'watch') rule.app = draft.app;
      const index = editing !== null && editingSource === 'reminders' ? editing : reminders.length;
      reminders.splice(index, 0, rule);
      keys.splice(index, 0, oldKey ?? ++nextReminderKey);
    }
    if (await saveConfig({ ...config, reminders, elapsed_reminders: elapsed }, keys)) {
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
      (document.querySelector<HTMLElement>(`[data-reminder-key="${nextKey}"] .rule-menu summary`) ?? document.querySelector<HTMLButtonElement>('.page-heading .primary'))?.focus();
    }
  }
  async function toggleElapsed(index: number, enabled: boolean) {
    if (!config || saving) return;
    const previous = config;
    const elapsed_reminders = (config.elapsed_reminders || []).map((rule, i) => i === index ? { ...rule, disabled: !enabled } : rule);
    config = { ...config, elapsed_reminders };
    if (await saveConfig(config)) notice = enabled ? 'Reminder turned on.' : 'Reminder turned off.';
    else config = previous;
  }
  async function deleteElapsed(index: number) {
    if (!config) return;
    const rules = config.elapsed_reminders || [];
    if (!window.confirm(`Delete “${rules[index].message}”? This cannot be undone.`)) return;
    if (await saveConfig({ ...config, elapsed_reminders: rules.filter((_, i) => i !== index) })) {
      notice = 'Reminder deleted.';
      await tick();
      document.querySelector<HTMLButtonElement>('.page-heading .primary')?.focus();
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
    if (insightsDirty && !window.confirm('Discard unsaved history settings?')) return;
    if ((page === 'reminders' || page === 'settings') && (saving || (dirty && !window.confirm('Discard your unsaved changes?')))) return;
    if (page === 'reminders' || page === 'settings') { ++editorGeneration; editorActive = false; draft = null; settings = null; editing = null; }
    insightsDirty = false;
    page = next;
  }
</script>

<svelte:head><title>Nudge — {page[0].toUpperCase() + page.slice(1)}</title><meta name="description" content="Manage Nudge reminders and insights." /></svelte:head>

<header class="site-header"><div class="brand"><Bell size={20} aria-hidden="true" /><span>nudge</span><nav class="main-nav" aria-label="Main navigation"><button type="button" aria-current={page === 'reminders' ? 'page' : undefined} disabled={insightsBusy} onclick={() => navigate('reminders')}>Reminders</button><button type="button" aria-current={page === 'insights' ? 'page' : undefined} disabled={insightsBusy} onclick={() => navigate('insights')}>Insights</button><button type="button" aria-current={page === 'settings' || page === 'diagnostics' ? 'page' : undefined} disabled={insightsBusy} onclick={() => navigate('settings')}>Settings</button></nav></div></header>
<a class="skip-link" href="#main-content">Skip to content</a>
<main id="main-content" data-page={page}>
  {#if page !== 'insights'}
  {#if page === 'settings'}<div class="page-heading"><div><h1>Settings</h1><p class="page-intro">Choose when reminders can reach you.</p></div></div>{:else if page === 'diagnostics'}<button class="text-button" type="button" onclick={() => navigate('settings')}>Back to Settings</button><div class="page-heading"><div><h1>Diagnostics</h1><p class="page-intro">Check the reminder service and its connections.</p></div></div>{/if}
  {#if page === 'reminders'}
  <div class="page-heading"><div><p class="eyebrow">A moment of intention</p><h1>Reminders</h1></div><button class="button primary" type="button" onclick={() => openEditor(null)} disabled={!config || saving || !!settings}><Plus size={18} aria-hidden="true" /> Add reminder</button></div>
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
  {#if page === 'settings'}
    <section class="appearance" aria-labelledby="appearance-title">
      <h2 id="appearance-title">Appearance</h2>
      <h3>Color theme</h3>
      <div class="theme-gallery" role="group" aria-label="Theme">
        {#each themes as option}
          <button type="button" class="theme-choice" aria-pressed={theme === option.id} onclick={() => changeTheme(option.id)}>
            <span class={`theme-swatch ${option.id}`} aria-hidden="true"></span><span>{option.label}</span>
          </button>
        {/each}
      </div>
      <p class="help">Saved on this device for this interface.</p>
      {#if themeError}<p class="feedback error" role="alert">{themeError}</p>{/if}
    </section>
  {/if}
  <div class="workspace">
  {#if page === 'reminders'}
  <div class="workspace-main">


  <section class="reminders" aria-labelledby="reminders-heading">
    <div class="section-heading"><h2 id="reminders-heading">Your reminders</h2>{#if config}<span>{config.reminders.filter(rule => !rule.disabled).length + (config.elapsed_reminders || []).filter(rule => !rule.disabled).length} enabled · {config.reminders.length + (config.elapsed_reminders || []).length} total</span>{/if}</div>
    {#if !config}<div class="empty">{loading ? 'Loading reminders…' : 'Your reminders could not be loaded.'}</div>
    {:else}<div class="reminder-list">{#each config.reminders as rule, index (reminderKeys[index])}
      <article class="reminder" class:disabled={rule.disabled} data-reminder-key={reminderKeys[index]} transition:slide={{ duration: prefersReducedMotion.current ? 0 : 170, easing: cubicOut }} onfocusin={(event) => { event.currentTarget.getAnimations().forEach((animation) => animation.finish()); }} onoutrostart={(event) => { event.currentTarget.inert = true; event.currentTarget.classList.add('leaving'); }}>
        <div class="reminder-info"><span class="reminder-icon" aria-hidden="true">{#if rule.kind === 'watch'}<AppWindow size={19} />{:else}<Timer size={19} />{/if}</span><div class="reminder-copy"><h3>{rule.message}</h3><p>{rule.kind === 'watch' ? `On focus, then every ${formatInterval(rule.every)} · ${appName(rule.app || '')}` : `Every ${formatInterval(rule.every)} · Any app`}</p></div></div>
        <div class="reminder-actions">
          <label class="switch-label"><input type="checkbox" role="switch" checked={!rule.disabled} onchange={(event) => { if (saving) event.currentTarget.checked = !event.currentTarget.checked; else void toggleReminder(index, event.currentTarget.checked); }} disabled={editorActive || !!settings} aria-disabled={saving} aria-label={`Reminders for ${rule.message}`} /><span>{rule.disabled ? 'Off' : 'On'}</span></label>
          <details class="rule-menu"><summary aria-label={`Actions for ${rule.message}`} onkeydown={pauseMenuKeydown}>···</summary><div><button class="button quiet" type="button" onkeydown={pauseMenuKeydown} onclick={() => openEditor(index)} disabled={saving || !!settings}>Edit<span class="sr-only"> {rule.message}</span></button><button class="button quiet danger" type="button" onkeydown={pauseMenuKeydown} onclick={() => deleteReminder(index)} disabled={saving || editorActive || !!settings}>Delete<span class="sr-only"> {rule.message}</span></button></div></details>
        </div>
      </article>
    {/each}
    {#each config.elapsed_reminders || [] as rule, index}
      <article class="reminder" class:disabled={rule.disabled}>
        <div class="reminder-info"><span class="reminder-icon" aria-hidden="true"><Timer size={19} /></span><div class="reminder-copy"><h3>{rule.message}</h3><p>After {formatInterval(rule.after)} of observed focus · {appName(rule.app)}{rule.repeat ? ` · Repeats every ${formatInterval(rule.repeat)}` : ''}</p></div></div>
        <div class="reminder-actions">
          <label class="switch-label"><input type="checkbox" role="switch" checked={!rule.disabled} onchange={(event) => { if (saving) event.currentTarget.checked = !event.currentTarget.checked; else void toggleElapsed(index, event.currentTarget.checked); }} disabled={editorActive || !!settings} aria-disabled={saving} aria-label={`Reminders for ${rule.message}`} /><span>{rule.disabled ? 'Off' : 'On'}</span></label>
          <details class="rule-menu"><summary aria-label={`Actions for ${rule.message}`} onkeydown={pauseMenuKeydown}>···</summary><div><button class="button quiet" type="button" onkeydown={pauseMenuKeydown} onclick={() => openEditor(index, 'elapsed_reminders')} disabled={saving || !!settings}>Edit<span class="sr-only"> {rule.message}</span></button><button class="button quiet danger" type="button" onkeydown={pauseMenuKeydown} onclick={() => deleteElapsed(index)} disabled={saving || editorActive || !!settings}>Delete<span class="sr-only"> {rule.message}</span></button></div></details>
        </div>
      </article>
    {/each}</div>{#if config.reminders.length === 0 && !(config.elapsed_reminders || []).length}<div class="empty"><Bell size={22} aria-hidden="true" /><div><strong>No reminders yet</strong><p>Choose what to remember and how often. You can even choose a particular app.</p></div></div>{/if}{/if}
  </section>
  </div>
  {/if}
  <aside class="workspace-side" aria-label={page === 'settings' ? 'Settings' : 'Diagnostics'}>
  <div class="additional"><details class="disclosure"><summary onclick={closeSettingsDisclosure}><span><strong>Settings</strong><small>Quiet hours and time between notifications</small></span><ChevronDown size={18} aria-hidden="true" /></summary><div class="disclosure-content">{#if !settings}<p>Choose when reminders are quiet and how much time to leave between notifications.</p><button class="button secondary" type="button" onclick={openSettings} disabled={!config || editorActive || saving}>Edit settings</button>{#if notice === 'Settings saved.'}<p class="help">Settings saved.</p>{/if}{:else}<form onsubmit={(event) => { event.preventDefault(); void saveSettings(); }}><label for="cooldown">Time between notifications</label><Interval id="cooldown" bind:value={settings.cooldown} /><p class="help">The minimum time between any two notifications.</p><div class="time-fields"><div><label for="quiet-start">Quiet hours start</label><input id="quiet-start" type="time" bind:value={settings.quiet_start} /></div><div><label for="quiet-end">Quiet hours end</label><input id="quiet-end" type="time" bind:value={settings.quiet_end} /></div></div><p class="help">Leave both times blank to turn off quiet hours.</p><div class="form-actions"><button class="button quiet" type="button" onclick={cancelSettings} disabled={saving}>Cancel</button><button class="button primary" type="submit" disabled={saving}>{saving ? 'Saving…' : 'Save settings'}</button></div></form>{/if}</div></details>
  <details class="disclosure"><summary><span><strong>Diagnostics</strong><small>Connection and service details</small></span><ChevronDown size={18} aria-hidden="true" /></summary><div class="disclosure-content">{#if diagnosticsError}<p role="alert">{diagnosticsError} <button class="text-button" type="button" onclick={loadDiagnostics}>Try again</button></p>{/if}{#if diagnostics}<dl><div><dt>Notification executable <small>Availability only; delivery is not verified.</small></dt><dd>{diagnostics.notification_available ? 'Available' : 'Unavailable'}</dd></div><div><dt>Niri connection</dt><dd>{diagnostics.niri_error || 'No errors reported'}</dd></div><div><dt>Configuration</dt><dd>{diagnostics.config_error || 'No errors reported'}</dd></div><div><dt>Scheduler</dt><dd>{status?.scheduler_error || (status ? 'No errors reported' : 'Unavailable')}</dd></div><div><dt>Focused app</dt><dd>{!status?.focus_known ? 'Unavailable' : status.focused_app || 'No app focused'}</dd></div><div><dt>Next reminder</dt><dd>{status ? formatDate(status.next_reminder) : 'Unavailable'}<small>{status?.next_description}</small></dd></div><div><dt>Matching reminders</dt><dd>{status?.matching_rules_known ? status.active_rules.join(', ') || 'None' : 'Unavailable'}</dd></div><div><dt>Version</dt><dd>{diagnostics.version || 'Unknown'}</dd></div></dl>{:else if !diagnosticsError}<p>Loading diagnostics…</p>{/if}<button class="button quiet" type="button" onclick={() => { void loadStatus(); void loadDiagnostics(); void loadApps(); }}>Refresh diagnostics</button></div></details></div>
  </aside>
  </div>
  {#if page === 'settings'}
    <Insights mode="settings" onOpenSettings={() => navigate('settings')} onDirtyChange={(value, busy) => { insightsDirty = value; insightsBusy = busy; }} />
    <section class="support"><h2>Support</h2><button class="button secondary" type="button" onclick={() => navigate('diagnostics')} disabled={insightsBusy}>Open diagnostics</button></section>
  {/if}

  {#if draft}<dialog class="editor" class:usage-editor={draft.kind === 'elapsed'} bind:this={editorDialog} inert={!editorActive} aria-labelledby="editor-heading" oncancel={(event) => { event.preventDefault(); cancelEditor(); }} onclick={(event) => { if (editorDialog && event.target === editorDialog) { const rect = editorDialog.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) cancelEditor(); } }}><div class="editor-heading"><h2 id="editor-heading">{editing === null ? 'Add reminder' : 'Edit reminder'}</h2><button class="button quiet editor-close" type="button" onclick={cancelEditor} disabled={saving} aria-label="Close editor"><X size={18} aria-hidden="true" /></button></div>{#key editorSession}<form onsubmit={(event) => { event.preventDefault(); void saveReminder(); }}>
    {#if configError}<div class="feedback error" role="alert"><span>Could not save reminder: {configError}</span><button class="text-button" type="button" onclick={reloadConfig} disabled={saving}>Reload saved configuration</button></div>{/if}
    {#if draft.kind !== 'elapsed'}<div class="editor-section"><span class="editor-number" aria-hidden="true">01</span><div><label for="message">What would you like to remember?</label><textarea id="message" bind:value={draft.message} rows="3" required placeholder="Take a break"></textarea></div></div>{/if}
    <fieldset class="editor-types"><legend>{draft.kind === 'elapsed' ? '01  Reminder type' : 'When should it remind you?'}</legend>
      <label><input type="radio" name="kind" value="every" checked={draft.kind === 'every'} onchange={() => selectReminderKind('every')} /> <span>Every so often<small>At a recurring interval, regardless of the app.</small></span></label>
      <label><input type="radio" name="kind" value="watch" checked={draft.kind === 'watch'} onchange={() => selectReminderKind('watch')} /> <span>While using an app<small>On focus, then at intervals while it remains focused.</small></span></label>
      <label><input type="radio" name="kind" value="elapsed" checked={draft.kind === 'elapsed'} onchange={() => selectReminderKind('elapsed')} /> <span>Usage Breaker<small>After observed foreground time reaches a threshold.</small></span></label>
    </fieldset>
    {#if draft.kind === 'elapsed'}
      <div class="usage-layout">
        <div class="usage-fields">
          <div class="editor-section"><span class="editor-number" aria-hidden="true">02</span><div><label for="app-id">Application</label><input id="app-id" bind:value={draft.app} list="open-apps" required placeholder="e.g. brave" /><datalist id="open-apps">{#each apps as app}<option value={app.id}>{app.name}</option>{/each}</datalist><p class="help">{appsError ? 'Open apps are unavailable. Enter an application ID.' : draft.app ? appName(draft.app) : 'Enter a Niri application ID.'}</p></div></div>
          <div class="editor-section"><span class="editor-number" aria-hidden="true">03</span><div><label for="frequency">Time threshold</label><Interval id="frequency" bind:value={draft.every} /><p class="help">Observed foreground time, not verified active input. Switching apps or uncertain focus resets the session.</p></div></div>
          <div class="editor-section"><span class="editor-number" aria-hidden="true">04</span><div><label><input type="checkbox" bind:checked={repeatEnabled} /> Repeat after the first reminder</label><p class="help">One reminder per foreground session by default.</p>{#if repeatEnabled}<label for="repeat-frequency">Repeat interval</label><Interval id="repeat-frequency" bind:value={draft.repeat} />{/if}</div></div>
          <div class="editor-section"><span class="editor-number" aria-hidden="true">05</span><div><label for="message">Message</label><textarea id="message" bind:value={draft.message} rows="3" required></textarea></div></div>
        </div>
        <aside class="usage-preview" aria-label="Notification preview">
          <div><span class="editor-number">PREVIEW</span><h3>A gentle moment to check in</h3><p>Minimal notification · example at the threshold</p></div>
          <div class="usage-preview-card"><span class="usage-preview-icon" aria-hidden="true"><Bell size={18} /></span><div><strong>{formatInterval(draft.every) || '10 minutes'} in {draft.app ? appName(draft.app) : 'your app'}</strong><p>{draft.message || 'Still doing what you intended?'}</p></div><X size={16} aria-hidden="true" /></div>
          <p class="usage-preview-note">The delivered notification shows the actual observed duration. Native appearance depends on your desktop.</p>
        </aside>
      </div>
    {:else}
      {#if draft.kind === 'watch'}<div class="editor-section"><span class="editor-number" aria-hidden="true">02</span><div><label for="app-id">Application ID</label><input id="app-id" bind:value={draft.app} list="open-apps" required placeholder="e.g. firefox" /><datalist id="open-apps">{#each apps as app}<option value={app.id}>{app.name}</option>{/each}</datalist>{#if appsError}<p class="help">Open apps are unavailable. Enter an application ID.</p>{/if}</div></div>{/if}
      <div class="editor-section"><span class="editor-number" aria-hidden="true">03</span><div><label for="frequency">How often?</label><Interval id="frequency" bind:value={draft.every} /><p class="help">The shared cooldown and quiet hours can delay reminders.</p></div></div>
    {/if}
    <div class="form-actions"><button class="button quiet" type="button" onclick={cancelEditor} disabled={saving}>Cancel</button><button class="button primary" type="submit" disabled={saving}>{saving ? 'Saving…' : editing === null ? 'Add reminder' : 'Save changes'}</button></div>
  </form>{/key}</dialog>{/if}

  {:else}
    <Insights onOpenSettings={() => navigate('settings')} onDirtyChange={(value, busy) => { insightsDirty = value; insightsBusy = busy; }} />
  {/if}
</main>
<div class="toast-region" aria-live="polite" aria-atomic="true">
  {#if toast}
    <aside class="reminder-toast" aria-label="Reminder">
      <div class="toast-content"><span class="toast-icon" aria-hidden="true"><Bell size={17} /></span><div><p class="toast-kicker">Nudge · reminder</p><strong>{toast.title}</strong><p>{toast.body}</p></div></div>
      {#if toastError}<p class="toast-error" role="alert">{toastError}</p>{/if}
      <div class="toast-actions"><button type="button" onclick={() => actOnToast('snooze')} disabled={toastBusy} aria-label="Snooze all reminders for 5 minutes">Snooze 5 min</button><button type="button" onclick={() => actOnToast('dismiss')} disabled={toastBusy}>Dismiss</button></div>
    </aside>
  {/if}
</div>
