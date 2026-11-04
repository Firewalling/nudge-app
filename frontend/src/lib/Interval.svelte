<script lang="ts">
  let { value = $bindable(), id }: { value: string; id: string } = $props();
  const initial = /^(\d+(?:\.\d+)?)(s|m|h)$/.exec(value);
  let amount = $state(initial ? initial[1] : '');
  let unit = $state(initial ? initial[2] : 'custom');
  function update() {
    if (unit !== 'custom') value = `${amount}${unit}`;
  }
</script>

<div class="interval-input">
  {#if unit === 'custom'}
    <input {id} bind:value required placeholder="e.g. 1h30m" aria-label="Custom duration" />
  {:else}
    <input {id} type="number" min="0.001" step="any" bind:value={amount} oninput={update} required aria-label="Interval amount" />
  {/if}
  <select bind:value={unit} onchange={update} aria-label="Interval unit">
    <option value="s">Seconds</option>
    <option value="m">Minutes</option>
    <option value="h">Hours</option>
    <option value="custom">Custom duration</option>
  </select>
</div>
