import ReactMarkdown, { type Components } from "react-markdown";
import rehypeSanitize from "rehype-sanitize";
import remarkGfm from "remark-gfm";

import { DescriptionImage } from "./attachmentImage";
import { cn } from "../../lib/utils";

/** Shared by both sides of the field, so a description does not change shape
 *  when you click into it: the read view renders these elements through
 *  react-markdown, the editor renders them through ProseMirror, and the rules
 *  live in one place in index.css. */
export const markdownContentClass = "issue-markdown";

/** Module scope on purpose. This object is the element type react-markdown
 *  renders for each tag, so a fresh literal per render is a new component type,
 *  which remounts every image it holds -- and a remount is another fetch and
 *  another flash. */
const markdownComponents: Components = {
  img: ({ src, alt }) => <DescriptionImage src={typeof src === "string" ? src : undefined} alt={alt} />,
  a: ({ href, children }) => (
    <a href={href} target="_blank" rel="noreferrer noopener">
      {children}
    </a>
  )
};

/** The read-only rendering of a description. */
export function IssueMarkdown({ text, className }: { text: string; className?: string }) {
  return (
    <div className={cn(markdownContentClass, className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSanitize]} components={markdownComponents}>
        {text}
      </ReactMarkdown>
    </div>
  );
}
