# @neko/avatars

Animated cat avatars for OpenNeko bots. Each bot is a neko.

- Three styles: `robot` (glossy shell, LED face), `flat` (soft shapes, drawn face) and `ink` (line art from the OpenNeko mark).
- Nine states: `idle`, `thinking`, `searching`, `listening`, `speaking`, `working`, `happy`, `error`, `sleeping`.
- Each bot gets a coat, a marking and an LED colour from its id. The same bot always looks the same.
- In one chat, no two bots share a coat while the 12 coats last.

The core (`@neko/avatars`) has no dependencies. It draws plain SVG shapes, so the web and a future React Native app use the same drawing. `@neko/avatars/react` holds the web component, the chat provider and the shared hook.

## Use in a chat

```tsx
import { NekoAvatar, NekoGroup, NekoProvider } from '@neko/avatars/react';

<NekoProvider nekoStyle={chat.avatarStyle} bots={chat.bots.map((b) => b.id)} onDark>
  <NekoAvatar seed={bot.id} state={bot.status} size={44} />
  <NekoGroup seeds={[a.id, b.id]} size={44} />
</NekoProvider>
```

`NekoProvider` holds the style the chat's owner picks. Every avatar inside it uses that style and the colours the provider assigns to its bot. Change `salt` to reshuffle a chat's colours.

## Avatar props

| Prop | Default | Meaning |
|---|---|---|
| `seed` | none | The bot's id. It picks the colours. Leave it out for the OpenNeko look (white coat, grey patch). |
| `nekoStyle` | the chat's style, else `robot` | `robot`, `flat` or `ink` |
| `state` | `idle` | What the bot is doing |
| `size` | `48` | Width and height in px |
| `colors` | none | Overrides for any colour |
| `animated` | `true` | Off, the still face of the state is drawn |
| `interactive` | `true` | A tap makes it hop and smile |
| `onDark` | the chat's setting | Adds a rim so the ink style and dark coats read on dark backgrounds |
| `badge` | none | Shown over the bottom-right corner |

With reduced motion on, every avatar draws its still face. All avatars on a page share one animation loop, which stops while the tab is hidden.

## Without React

```ts
import { buildNeko, nekoColors, toSvg } from '@neko/avatars';

const svg = toSvg(buildNeko({ style: 'flat', state: 'happy', colors: nekoColors('tax-bot') }), 256);
```

Use `toSvg` for server-made images, such as bot icons on chat platforms. `renderScene(scene, createElement, components)` renders a scene with any element factory. A React Native app passes the react-native-svg components to it, and uses `useNeko` from `@neko/avatars/react` for the motion.
