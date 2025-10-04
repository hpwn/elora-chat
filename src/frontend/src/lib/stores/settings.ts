import { browser } from '$app/environment';
import { writable } from 'svelte/store';

export type Settings = {
  showExportPanel: boolean;
};

const KEY = 'elora.settings.v1';
const DEFAULT_SETTINGS: Settings = { showExportPanel: false };

function loadSettings(): Settings {
  if (!browser) {
    return DEFAULT_SETTINGS;
  }

  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) {
      return DEFAULT_SETTINGS;
    }

    const parsed = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) {
      return DEFAULT_SETTINGS;
    }

    return {
      showExportPanel: !!(parsed as Partial<Settings>).showExportPanel
    };
  } catch (error) {
    console.warn('Failed to load settings from storage', error);
    return DEFAULT_SETTINGS;
  }
}

export const settings = writable<Settings>(DEFAULT_SETTINGS);

if (browser) {
  settings.set(loadSettings());

  settings.subscribe((value) => {
    try {
      localStorage.setItem(KEY, JSON.stringify(value));
    } catch (error) {
      console.warn('Failed to persist settings', error);
    }
  });
}
