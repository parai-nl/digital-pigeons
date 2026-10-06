<script lang="ts">
  import App from './App.svelte'
  import FirstUseNotice from './lib/access/FirstUseNotice.svelte'
</script>

<div class="duif-entry"><FirstUseNotice /><div class="duif-entry-workspace"><App /></div></div>

<style>
  .duif-entry { display: flex; flex-direction: column; height: 100%; min-height: 0; }
  .duif-entry-workspace { flex: 1; min-height: 0; }
</style>
