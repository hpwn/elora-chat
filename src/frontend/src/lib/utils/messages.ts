import type { Emote } from '$lib/types/messages';
import { buildApiUrl } from './misc';

export function sanitizeMessage(message: string): string {
  // replace < and > with HTML entities to prevent XSS attacks
  return message.replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

export function addMessageEffects(message: string): { messageText: string; effects: string } {
  const colors = [
    'yellow',
    'red',
    'green',
    'cyan',
    'purple',
    'pink',
    'rainbow',
    'glow1',
    'glow2',
    'glow3',
    'flash1',
    'flash2',
    'flash3'
  ];
  const colorCommands = colors.reduce(
    (accumulator, color) => ({
      ...accumulator,
      [color]: `color-${color}`
    }),
    {}
  );

  const commands = {
    ...colorCommands,
    bold: 'text-bold',
    italic: 'text-italic',
    wave: 'effect-wave',
    shake: 'effect-shake'
  };

  const lastCommandIndex = message.indexOf(': ');
  const effectNames = lastCommandIndex >= 0 ? message.substr(0, lastCommandIndex).split(':') : [];
  let messageText = lastCommandIndex >= 0 ? message.substr(lastCommandIndex + 2) : message;
  const effects = effectNames
    .map((effect) =>
      // eslint-disable-next-line no-prototype-builtins
      commands.hasOwnProperty(effect) ? commands[effect as keyof typeof commands] : null
    )
    .filter((value) => !!value)
    .join(' ');

  // if no effects were found, set the message text back to the original message content
  if (effects.length <= 0) {
    messageText = message;
  }

  return { messageText, effects };
}

function escapeRegExp(string: string): string {
  return string.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'); // $& means the whole matched string
}

function loadImage(source: string): string {
  return buildApiUrl(`/imageproxy?url=${encodeURIComponent(source)}`);
}

export function replaceEmotes(message: string, emotes: Emote[]): string {
  let newMessage = message;

  if (emotes && emotes.length > 0) {
    emotes.forEach((emote) => {
      const emoteImg = document.createElement('img');
      emoteImg.className = 'emote-image';
      emoteImg.alt = emote.name;
      emoteImg.src = loadImage(emote.images[0].url);

      const escapedEmoteName = escapeRegExp(emote.name);
      const emoteRegex = new RegExp(escapedEmoteName, 'g');
      newMessage = newMessage.replace(emoteRegex, emoteImg.outerHTML);
    });
  }

  return newMessage;
}
