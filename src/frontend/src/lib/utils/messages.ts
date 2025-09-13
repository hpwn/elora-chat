import { TextEffect } from '$lib/types/effects';
import { FragmentType, type Emote, type Fragment } from '$lib/types/messages';
import { buildApiUrl } from './misc';

export const validNameColors = new Map<string, string>([
  ['red', '#df5858'],
  ['orange', '#f96708'],
  ['yellow', '#fabd40'],
  ['green', '#2ddd6a'],
  ['lightblue', '#6ad7d6'],
  ['blue', '#2bb5f3'],
  ['violet', '#ba29e0'],
  ['pink', '#e94079'],
  ['tan', '#ebb369'],
  ['olive', '#def169'],
  ['lime', '#73df5c'],
  ['sky', '#64d1fb'],
  ['purple', '#8e73ef']
]);

export function sanitizeMessage(message: string): string {
  // replace < and > with HTML entities to prevent XSS attacks
  return message.replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

export function loadImage(source: string): string {
  return buildApiUrl(`/imageproxy?url=${encodeURIComponent(source)}`);
}

export function imageFromEmote(emote: Emote): HTMLImageElement {
  const emoteImg = document.createElement('img');
  emoteImg.className = 'emote-image';
  emoteImg.alt = emote.name;
  emoteImg.title = emote.name;
  emoteImg.src = loadImage(emote.images[0].url);
  return emoteImg;
}

function* fragmentGenerator(fragments: Fragment[]): Generator<Fragment, Fragment, boolean> {
  for (const fragment of fragments) {
    yield fragment;
  }
  return { type: FragmentType.Text, text: '', emote: null };
}

// Formats message fragments into HTML and returns style classes.
// Parses chat effects with recursive descent following the OSRS wiki spec.
export function formatMessageFragments(fragments: Fragment[]): {
  messageWithHTML: string;
  effects: string;
} {
  const effectList: string[] = [];
  const messageList: string[] = [];
  const fragmentGen = fragmentGenerator(fragments);

  // Rule for handling text and emotes in the typical case
  function handleTextEmote() {
    const nextFrag = fragmentGen.next();
    if (nextFrag.done) {
      return;
    }
    const fragment = nextFrag.value;
    if (fragment.type === FragmentType.Emote && !!fragment.emote) {
      messageList.push(imageFromEmote(fragment.emote).outerHTML);
    } else {
      messageList.push(sanitizeMessage(fragment.text));
    }
    handleTextEmote();
  }

  // Rule for handling effects that require wrapping characters with span
  function handleSpanEffect() {
    const nextFrag = fragmentGen.next();
    if (nextFrag.done) {
      return;
    }
    const fragment = nextFrag.value;
    if (fragment.type === FragmentType.Emote && !!fragment.emote) {
      messageList.push(imageFromEmote(fragment.emote).outerHTML);
    } else {
      const msg = sanitizeMessage(fragment.text)
        .split('')
        .map((c) => (c === ' ' ? '&nbsp' : `<span>${c}</span>`))
        .join('');
      messageList.push(msg);
    }
    handleSpanEffect();
  }

  // Rule for handling effects
  function handleEffect(effect: string) {
    switch (effect) {
      case TextEffect.Wave:
      case TextEffect.Wave2:
      case TextEffect.Shake:
      case TextEffect.Cheddar:
        handleSpanEffect();
        break;
      default:
        handleTextEmote();
        break;
    }
  }

  // Rule for handling color fragments
  function handleColor() {
    const nextFrag = fragmentGen.next();
    if (nextFrag.done) {
      return;
    }
    const fragment = nextFrag.value;
    if (fragment.type === FragmentType.Effect) {
      effectList.push('effect-' + fragment.text);
      handleEffect(fragment.text);
    } else {
      if (fragment.type === FragmentType.Emote && !!fragment.emote) {
        messageList.push(imageFromEmote(fragment.emote).outerHTML);
      } else {
        messageList.push(sanitizeMessage(fragment.text));
      }
      handleTextEmote();
    }
  }

  // Top level entry point for recursive descent parsing
  function recursiveDescent() {
    const nextFrag = fragmentGen.next();
    if (nextFrag.done) {
      return;
    }
    const fragment = nextFrag.value;
    switch (fragment.type) {
      case FragmentType.Text:
        messageList.push(sanitizeMessage(fragment.text));
        handleTextEmote();
        break;
      case FragmentType.Emote:
        if (fragment.emote) {
          messageList.push(imageFromEmote(fragment.emote).outerHTML);
          handleTextEmote();
          break;
        }
      case FragmentType.Colour:
        effectList.push('color-' + fragment.text);
        handleColor();
        break;
      case FragmentType.Effect:
        effectList.push('effect-' + fragment.text);
        handleEffect(fragment.text);
        break;
      case FragmentType.Pattern:
        // TODO: Handle custom patterns
        handleColor();
        break;
    }
  }

  // Recursively generate the HTML message and gather styles
  recursiveDescent();

  return { messageWithHTML: messageList.join(''), effects: effectList.join(' ') };
}
