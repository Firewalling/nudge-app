<script lang="ts">
  let { value = $bindable(), id }: { value: string; id: string } = $props();
  const initial = /^(\d+(?:\.\d+)?)(s|m|h)$/.exec(value);
  let amount = $state(initial ? initial[1] : '');
  let unit = $state(initial ? initial[2] : 'custom');
</script>

<div class="interval-input">
  {#if unit === 'custom'}
    <input {id} bind:value required placeholder="e.g. 1h30m" />
  {:else}
    <input {id} type="number" min="0.001" step="any" value={amount} oninput={(event) => { amount = event.currentTarget.value; value = `${amount}${unit}`; }} required />
  {/if}
  <select value={unit} onchange={(event) => { unit = event.currentTarget.value; if (unit !== 'custom') value = `${amount}${unit}`; }} aria-label="Duration unit">
    <option value="s">Seconds</option>
    <option value="m">Minutes</option>
    <option value="h">Hours</option>
    <option value="custom">Custom duration</option>
  </select>
</div>
