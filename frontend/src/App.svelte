<script lang="ts">
  import { onMount } from 'svelte';
  import { Activity, ArrowDownRight, Bell, BellOff, Check, ChevronRight, Clock3, Coffee, Focus, LoaderCircle, Moon, Pause, Play, Plus, RefreshCw, Settings2, Sparkles, Trash2, TriangleAlert, X } from 'lucide-svelte';
  import Button from './lib/components/ui/button/button.svelte';

  type Reminder = { kind: 'watch' | 'every'; app?: string; every: string; message: string; disabled?: boolean };
  type Config = { reminders: Reminder[]; cooldown: string; paused_until: string; quiet_start?: string; quiet_end?: string };
  type Status = { active: boolean; paused_until: string; focused_app: string; active_rule: string; next_reminder: string; niri_error: string; notification_available: boolean; version: string };
  type Diagnostics = { version: string; niri_error: string; notification_available: boolean; config_error: string };
  type Page = 'overview' | 'rules' | 'settings' | 'diagnostics';

  let page = $state<Page>('overview');
  let status = $state<Status | null>(null);
  let config = $state<Config | null>(null);
  let diagnostics = $state<Diagnostics | null>(null);
  let loading = $state(true);
  let statusError = $state('');
  let configError = $state('');
  let diagnosticsError = $state('');
  let notice = $state('');
  let saving = $state(false);
  let controlling = $state(false);
  let refreshing = $state(false);
  let dirty = $state(false);
  let snoozeDuration = $state('15m');
  let configRevision = '';

  async function request<T>(path: string, options?: RequestInit): Promise<T> {
    const response = await fetch(`/api/v1/${path}`, options);
    const data = await response.json().catch(() => null);
    if (!response.ok) throw new Error(data && typeof data.error === 'string' ? data.error : `Request failed (${response.status})`);
    if (path === 'config') configRevision = response.headers.get('ETag') ?? '';
    return data as T;
  }
  function message(error: unknown) { return error instanceof Error ? error.message : 'Something went wrong'; }
  async function loadStatus() {
    try { status = await request<Status>('status'); statusError = ''; }
    catch (error) { status = null; statusError = message(error); }
  }
  async function loadConfig() {
    try { config = await request<Config>('config'); config.reminders ??= []; configError = ''; dirty = false; }
    catch (error) { config = null; configError = message(error); }
  }
  async function loadDiagnostics() {
    try { diagnostics = await request<Diagnostics>('diagnostics'); diagnosticsError = ''; }
    catch (error) { diagnostics = null; diagnosticsError = message(error); }
  }
  async function refresh() {
    refreshing = true;
    await Promise.all([loadStatus(), loadDiagnostics(), ...(!dirty ? [loadConfig()] : [])]);
    refreshing = false;
  }
  onMount(() => {
    void (async () => { await refresh(); loading = false; })();
    const timer = window.setInterval(() => { if (!document.hidden) void loadStatus(); }, 15000);
    return () => window.clearInterval(timer);
  });
  async function save() {
    if (!config) return;
    saving = true; notice = '';
    try {
      config = await request<Config>('config', { method: 'PUT', headers: { 'Content-Type': 'application/json', 'If-Match': configRevision }, body: JSON.stringify(config) });
      config.reminders ??= []; dirty = false; configError = ''; notice = 'Your changes have been saved.';
      await Promise.all([loadStatus(), loadDiagnostics()]);
    } catch (error) { configError = message(error); }
    finally { saving = false; }
  }
  async function control(action: 'pause' | 'resume' | 'snooze') {
    controlling = true; notice = '';
    try {
      status = await request<Status>('control', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(action === 'resume' ? { action } : { action, duration: action === 'pause' ? '1h' : snoozeDuration }) });
      statusError = '';
      notice = action === 'resume' ? 'Reminders resumed.' : action === 'snooze' ? `Snoozed for ${snoozeDuration}.` : 'Reminders paused for 1 hour.';
    } catch (error) { statusError = message(error); }
    finally { controlling = false; }
  }
  function addRule() {
    if (!config) return;
    config.reminders = [...config.reminders, { kind: 'every', every: '1h', message: '' }];
    dirty = true;
  }
  function removeRule(index: number) {
    if (!config) return;
    config.reminders = config.reminders.filter((_, i) => i !== index);
    dirty = true;
  }
  function formatDate(value: string) {
    if (!value || value.startsWith('0001-01-01')) return 'Unavailable';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date);
  }
  let enabledRules = $derived(config?.reminders.filter((rule) => !rule.disabled).length ?? 0);
  let isPaused = $derived(!!status?.paused_until && new Date(status.paused_until).getTime() > Date.now());
</script>

<svelte:head><meta name="description" content="A quieter way to stay intentional with your time." /></svelte:head>

<div class="app-shell">
  <aside class="sidebar">
    <div class="brand"><div class="brand-mark"><Sparkles size={19} strokeWidth={2.2} /></div><div><strong>nudge</strong><span>A little more intention.</span></div></div>
    <div class="nav-label">YOUR SPACE</div>
    <nav aria-label="Main navigation">
      <button class:current={page === 'overview'} aria-current={page === 'overview' ? 'page' : undefined} onclick={() => page = 'overview'}><Activity size={18} /> Overview <ChevronRight class="nav-chevron" size={15} /></button>
      <button class:current={page === 'rules'} aria-current={page === 'rules' ? 'page' : undefined} onclick={() => page = 'rules'}><Bell size={18} /> Reminders <ChevronRight class="nav-chevron" size={15} /></button>
      <button class:current={page === 'settings'} aria-current={page === 'settings' ? 'page' : undefined} onclick={() => page = 'settings'}><Settings2 size={18} /> Settings <ChevronRight class="nav-chevron" size={15} /></button>
      <button class:current={page === 'diagnostics'} aria-current={page === 'diagnostics' ? 'page' : undefined} onclick={() => page = 'diagnostics'}><Activity size={18} /> Diagnostics <ChevronRight class="nav-chevron" size={15} /></button>
    </nav>
    <div class="sidebar-bottom"><div class="sidebar-note"><span class="note-icon"><Coffee size={17} /></span><p><strong>Make space for focus.</strong><br />Small reminders, on your terms.</p></div><div class="side-version"><span class:online={!!status} class="connection-dot"></span>{status ? 'Connected' : 'Not connected'} <span>{status?.version || ''}</span></div></div>
  </aside>

  <main id="main-content">
    <header class="topbar"><div><span class="top-label">YOUR PERSONAL SPACE</span><span class="top-divider">/</span><span class="top-current">{page === 'overview' ? 'Overview' : page === 'rules' ? 'Reminders' : page === 'settings' ? 'Settings' : 'Diagnostics'}</span></div><button class="refresh" onclick={refresh} disabled={refreshing} aria-label="Refresh status and diagnostics" title="Refresh"><RefreshCw size={17} class={refreshing ? 'spinning' : ''} /></button></header>
    <div class="content">
      {#if notice}<div class="feedback success" role="status"><Check size={17} /> {notice}<button aria-label="Dismiss message" onclick={() => notice = ''}><X size={16} /></button></div>{/if}
      {#if statusError}<div class="feedback error" role="alert"><TriangleAlert size={17} /><span>Nudge: {statusError}</span><button onclick={refresh}>Try again</button></div>{/if}
      {#if page === 'overview'}
        <div class="page-heading"><div><div class="eyebrow"><span></span> YOUR DASHBOARD</div><h1>A little more <em>intention.</em></h1><p>Make room for what matters, one gentle reminder at a time.</p></div><div class="heading-date">{new Intl.DateTimeFormat(undefined, { weekday: 'long', month: 'long', day: 'numeric' }).format(new Date())}</div></div>
        <section class="hero" aria-labelledby="hero-title">
          <div class="hero-copy">
            <div class="hero-kicker"><span class:live={!!status && status.active} class="pulse-dot"></span>{loading ? 'CHECKING STATUS' : !status ? 'CONNECTION UNAVAILABLE' : isPaused ? 'TAKING A BREAK' : status.active ? 'NUDGE IS ACTIVE' : 'NUDGE IS INACTIVE'}</div>
            <h2 id="hero-title">{!status ? 'Let’s get connected.' : isPaused ? 'Take your time.' : status.active ? 'A little breathing room.' : 'Ready when you are.'}</h2>
            <p>{!status ? 'Nudge is not responding right now. Check that the service is running, then try again.' : isPaused ? `Reminders are paused until ${formatDate(status.paused_until)}.` : status.active ? 'Nudge is looking out for you, quietly in the background.' : 'Nudge isn’t currently active. Check diagnostics for more details.'}</p>
            <div class="hero-actions">
              {#if status}<button class="button primary" onclick={() => control(isPaused ? 'resume' : 'pause')} disabled={controlling}>{#if isPaused}<Play size={16} fill="currentColor" /> Resume reminders{:else}<Pause size={16} fill="currentColor" /> Pause for 1 hour{/if}</button>
              {:else}<button class="button primary" onclick={refresh} disabled={refreshing}><RefreshCw size={16} /> Try again</button>{/if}
              <button class="button subtle" onclick={() => page = 'rules'}>Manage reminders <ChevronRight size={16} /></button>
            </div>
          </div>
          <div class="hero-art" aria-hidden="true"><div class="orbit orbit-one"></div><div class="orbit orbit-two"></div><div class="orbit orbit-three"></div><div class="art-center"><Bell size={34} strokeWidth={1.25} /></div><span class="art-star star-one">✳</span><span class="art-star star-two">✦</span><span class="art-dot"></span></div>
        </section>
        <div class="section-heading"><div><div class="eyebrow">THE HERE AND NOW</div><h2>At a glance</h2></div><button class="text-button" onclick={refresh} disabled={refreshing}>Refresh <RefreshCw size={14} /></button></div>
        <div class="glance-grid"><article class="glance-card"><div class="card-top"><span class="icon-tile lavender"><Focus size={19} /></span><ArrowDownRight size={17} /></div><span class="card-label">FOCUSED APP</span><strong>{status ? status.focused_app || 'No app detected' : 'Unavailable'}</strong><p>{status?.active_rule ? `Rule: ${status.active_rule}` : 'Your current foreground app'}</p></article><article class="glance-card"><div class="card-top"><span class="icon-tile peach"><Clock3 size={19} /></span><ArrowDownRight size={17} /></div><span class="card-label">NEXT REMINDER</span><strong>{status ? formatDate(status.next_reminder) : 'Unavailable'}</strong><p>When your next nudge is due</p></article><article class="glance-card"><div class="card-top"><span class="icon-tile green"><Bell size={19} /></span><ArrowDownRight size={17} /></div><span class="card-label">ACTIVE REMINDERS</span><strong>{config ? enabledRules : '—'}</strong><p>{config ? `${config.reminders.length} total ${config.reminders.length === 1 ? 'rule' : 'rules'} configured` : 'Configuration unavailable'}</p></article></div>
        <div class="overview-bottom"><section class="panel reminder-preview"><div class="panel-heading"><div><div class="eyebrow">A LITTLE CHECK-IN</div><h2>Your reminders</h2><p>A quick look at the nudges you’ve set up.</p></div><button class="text-button" onclick={() => page = 'rules'}>View all <ChevronRight size={15} /></button></div>{#if !config}<div class="empty compact">Your reminders are unavailable. {configError}</div>{:else if config.reminders.length === 0}<div class="empty compact"><BellOff size={20} /><div><strong>No reminders yet</strong><p>Create a reminder to get started.</p></div><button class="text-button" onclick={() => page = 'rules'}>Add reminder <ChevronRight size={15} /></button></div>{:else}<div class="preview-list">{#each config.reminders.slice(0, 3) as rule}<div class="preview-row"><span class="icon-tile small"><Bell size={16} /></span><div><strong>{rule.message || 'Untitled reminder'}</strong><span>{rule.kind === 'watch' ? `Watching ${rule.app || 'an app'}` : 'Everywhere'} · Every {rule.every}</span></div><span class:muted-badge={rule.disabled} class="badge">{rule.disabled ? 'Disabled' : 'Enabled'}</span></div>{/each}</div>{/if}</section><section class="panel snooze-panel"><span class="icon-tile lavender"><Moon size={19} /></span><h2>Need a moment?</h2><p>Give yourself a little uninterrupted time. Your reminders can wait.</p><form onsubmit={(event) => { event.preventDefault(); void control('snooze'); }}><label for="snooze">Snooze for</label><div class="snooze-controls"><input id="snooze" bind:value={snoozeDuration} required placeholder="15m" aria-describedby="duration-help" /><button class="button secondary" disabled={!status || controlling}>Snooze <ChevronRight size={15} /></button></div><small id="duration-help">Use a duration like 15m or 1h.</small></form></section></div>
      {:else if page === 'rules' || page === 'settings'}
        <div class="page-heading inner"><div><div class="eyebrow"><span></span> MAKE IT YOURS</div><h1>{page === 'rules' ? 'Your reminders.' : 'Your settings.'}</h1><p>{page === 'rules' ? 'Thoughtful nudges, just the way you need them.' : 'A little more control over when Nudge reaches you.'}</p></div></div>
        {#if configError}<div class="feedback error" role="alert"><TriangleAlert size={17} /><span>{configError}</span>{#if !config}<button onclick={loadConfig}>Try again</button>{/if}</div>{/if}
        {#if config}
          <form onsubmit={(event) => { event.preventDefault(); void save(); }} oninput={() => dirty = true} onchange={() => dirty = true}>
            {#if page === 'rules'}<div class="section-heading"><div><div class="eyebrow">YOUR ROUTINE</div><h2>Reminder rules <span class="count">{config.reminders.length}</span></h2><p>Changes take effect when you save.</p></div><button type="button" class="button secondary" onclick={addRule}><Plus size={16} /> Add reminder</button></div>
              {#if config.reminders.length === 0}<div class="panel empty rules-empty"><span class="icon-tile lavender"><Bell size={20} /></span><h2>No reminders yet</h2><p>Set up your first reminder for a gentle nudge when you need it.</p><button type="button" class="button secondary" onclick={addRule}><Plus size={16} /> Add reminder</button></div>{/if}
              <div class="rules-list">{#each config.reminders as rule, index}<section class="panel rule-card"><div class="rule-heading"><div><span class="rule-number">{String(index + 1).padStart(2, '0')}</span><h3>Reminder {index + 1}</h3></div><button type="button" class="icon-button danger" aria-label={`Remove reminder ${index + 1}`} onclick={() => removeRule(index)}><Trash2 size={17} /></button></div><div class="form-grid"><label>Reminder type<select bind:value={rule.kind}><option value="every">At an interval</option><option value="watch">While using an app</option></select></label>{#if rule.kind === 'watch'}<label>App name<input bind:value={rule.app} required placeholder="e.g. Firefox" /></label>{/if}<label>Every<input bind:value={rule.every} required placeholder="e.g. 30m" /><small>Duration, for example 30m or 1h.</small></label><label class="wide">Message<input bind:value={rule.message} required placeholder="Time for a little break" /></label></div><label class="check-row"><input type="checkbox" bind:checked={rule.disabled} /> Disable this reminder</label></section>{/each}</div>
            {:else}<section class="panel settings-panel"><div class="settings-title"><span class="icon-tile lavender"><Clock3 size={19} /></span><div><h2>Timing preferences</h2><p>Keep reminders useful, never overwhelming.</p></div></div><div class="form-grid"><label>Cooldown<input bind:value={config.cooldown} required placeholder="e.g. 5m" /><small>Minimum time between notifications. Use a duration like 5m.</small></label><div></div><label>Quiet hours start<input type="time" bind:value={config.quiet_start} /><small>Leave both blank to turn quiet hours off.</small></label><label>Quiet hours end<input type="time" bind:value={config.quiet_end} /></label></div></section>{/if}
            <div class="save-bar"><span>{dirty ? 'You have unsaved changes' : 'Everything is up to date'}</span><Button type="submit" disabled={saving || !dirty}>{#if saving}<LoaderCircle size={16} class="spinning" /> Saving…{:else}<Check size={16} /> Save changes{/if}</Button></div>
          </form>
        {/if}
      {:else}
        <div class="page-heading inner"><div><div class="eyebrow"><span></span> UNDER THE HOOD</div><h1>Diagnostics.</h1><p>A clear view of how Nudge is doing.</p></div><button class="button secondary" onclick={loadDiagnostics}><RefreshCw size={16} /> Refresh</button></div>
        {#if diagnosticsError}<div class="feedback error" role="alert"><TriangleAlert size={17} /> {diagnosticsError}</div>{/if}
        {#if diagnostics}<section class="panel diagnostics"><div class="section-heading"><div><div class="eyebrow">SERVICE HEALTH</div><h2>System details</h2></div></div><div class="diagnostic-row"><div><strong>Notifications</strong><span>notify-send executable (delivery not verified)</span></div><span class:muted-badge={!diagnostics.notification_available} class="badge">{diagnostics.notification_available ? 'Available' : 'Unavailable'}</span></div><div class="diagnostic-row"><div><strong>Niri connection</strong><span>{diagnostics.niri_error || 'No connection errors reported'}</span></div><span class:muted-badge={!!diagnostics.niri_error} class="badge">{diagnostics.niri_error ? 'Issue' : 'Connected'}</span></div><div class="diagnostic-row"><div><strong>Configuration</strong><span>{diagnostics.config_error || 'No configuration errors reported'}</span></div><span class:muted-badge={!!diagnostics.config_error} class="badge">{diagnostics.config_error ? 'Issue' : 'OK'}</span></div><div class="diagnostic-row"><div><strong>Version</strong><span>Running Nudge version</span></div><span>{diagnostics.version || 'Unknown'}</span></div></section>{:else if !diagnosticsError}<div class="empty">Loading diagnostics…</div>{/if}
      {/if}
      <footer><span>Made for more mindful moments <span aria-hidden="true">✳</span></span><span>NUDGE {status?.version || ''}</span></footer>
    </div>
  </main>
</div>
