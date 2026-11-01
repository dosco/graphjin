import type { Scene } from './scene';
import { renderScene } from './render';

const esc = (v: unknown) => String(v).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
const kebab = (k: string) => (k === 'viewBox' || k === 'gradientUnits' ? k : k.replace(/[A-Z]/g, (m) => `-${m.toLowerCase()}`));

const TAGS = {
  Svg: 'svg', G: 'g', Path: 'path', Circle: 'circle', Ellipse: 'ellipse', Defs: 'defs',
  ClipPath: 'clipPath', RadialGradient: 'radialGradient', LinearGradient: 'linearGradient', Stop: 'stop',
};

/** The scene as a standalone SVG string: for servers, chat platform icons, or SvgXml. */
export function toSvg(scene: Scene, size = 200): string {
  return renderScene<string>(
    scene,
    (type: string, props, ...children) => {
      const attrs = Object.entries(props ?? {})
        .map(([k, v]) => ` ${kebab(k)}="${esc(v)}"`)
        .join('');
      return `<${type}${attrs}>${children.join('')}</${type}>`;
    },
    TAGS,
    { xmlns: 'http://www.w3.org/2000/svg', width: size, height: size },
  );
}
