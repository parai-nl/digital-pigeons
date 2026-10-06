import { mount } from 'svelte'
import DevShell from './DevShell.svelte'

// Standalone dev: de DevShell doet alsof hij de parai-core-shell is. De
// productie-bundle (index.ts → mountModule) kent dit bestand niet.
const el = document.getElementById('app')
if (el) mount(DevShell, { target: el })
