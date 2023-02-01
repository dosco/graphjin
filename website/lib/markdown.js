import { unified } from "unified";
import remarkGFM from "remark-gfm";
import remarkEmoji from "remark-emoji";
import remarkBreaks from "remark-breaks";
import remarkEmbedder from "@remark-embedder/core";
import remarkOEmbed from "@remark-embedder/transformer-oembed";
import remarkTOC from "remark-toc";
import remarkParse from "remark-parse";
import remarkRehype from "remark-rehype";
import rehypePrettyCode from "rehype-pretty-code";
import rehypeAutolinkHeadings from "rehype-autolink-headings";
import rehypeRaw from "rehype-raw";
import rehypeSlug from "rehype-slug";
import rehypeStringify from "rehype-stringify";
import { h } from "hastscript";

// import { FaMeteor } from 'react-icons/fa'

const handleHTML = (html, info) => {
  const { url, transformer } = info;
  if (
    transformer.name === "@remark-embedder/transformer-oembed" ||
    url.includes("youtube.com")
  ) {
    return `<div class="aspect-video overflow-hidden mb-4">${html}</div>`;
  }
  return html;
};

const oembedConfig = () => {
  return { params: { maxwidth: "780", maxheight: "780" } };
};

const prettyCodeOptions = {
  // Use one of Shiki's packaged themes
  theme: "material-palenight",

  onVisitLine(node) {
    // Prevent lines from collapsing in `display: grid` mode, and
    // allow empty lines to be copy/pasted
    if (node.children.length === 0) {
      node.children = [{ type: "text", value: " " }];
    }
  },
  // Feel free to add classNames that suit your docs
  onVisitHighlightedLine(node) {
    node.properties.className.push("highlighted");
  },
  onVisitHighlightedWord(node) {
    node.properties.className = ["word"];
  },
};

const embedderOptions = {
  transformers: [[remarkOEmbed, oembedConfig]],
  handleHTML,
};

const emojiOptions = { emoticon: true, padSpaceAfter: true };

const tocOptions = { maxDepth: 3, tight: true };

const autolinkOptions = {
  behaviour: "after",
  group: h("div", { class: "flex items-center" }),
  content: (n) =>
    h(n.tagName, { class: "material-symbols-outlined pl-3" }, "link"),
  test: ["h2", "h3"],
};

export async function markdownToHtml(markdown) {
  const result = await unified()
    .use(remarkParse)
    .use(remarkGFM)
    .use(remarkEmoji, emojiOptions)
    .use(remarkBreaks)
    .use(remarkTOC, tocOptions)
    .use(remarkEmbedder, embedderOptions)
    .use(remarkRehype, { allowDangerousHtml: true })
    .use(rehypePrettyCode, prettyCodeOptions)
    .use(rehypeSlug)
    .use(rehypeAutolinkHeadings, autolinkOptions)
    .use(rehypeRaw)
    .use(rehypeStringify)
    .process(markdown);

  return result.toString();
}
