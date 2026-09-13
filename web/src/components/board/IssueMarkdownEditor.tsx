import { useRef } from "react";
import { EditorContent, NodeViewWrapper, ReactNodeViewRenderer, useEditor, type NodeViewProps } from "@tiptap/react";
import { Markdown } from "@tiptap/markdown";
import StarterKit from "@tiptap/starter-kit";
import Image from "@tiptap/extension-image";
import Placeholder from "@tiptap/extension-placeholder";

import { uploadAttachment } from "../../api/postdareGo";
import { useAuthStore } from "../../store/auth";
import { DescriptionImage } from "./attachmentImage";
import { markdownContentClass } from "./IssueMarkdown";
import { cn } from "../../lib/utils";

/** An image inside the editor, rendered through the authenticated path like
 *  everywhere else. The node keeps the API URL in its attrs -- that is what
 *  reaches the markdown -- and only the view swaps in an object URL. */
function ImageNodeView({ node }: NodeViewProps) {
  return (
    <NodeViewWrapper className="my-1" data-drag-handle>
      <DescriptionImage src={String(node.attrs.src ?? "")} alt={node.attrs.alt ?? undefined} />
    </NodeViewWrapper>
  );
}

/** The image extension, taught to survive a markdown round trip.
 *
 *  The markdown package serializes the nodes its extensions describe, and the
 *  image extension describes none, so a pasted screenshot would come back as
 *  nothing at all the next time the description was read. */
const MarkdownImage = Image.extend({
  markdownTokenName: "image",
  parseMarkdown: (token, helpers) =>
    helpers.createNode("image", {
      src: token.href ?? "",
      alt: token.text ?? "",
      title: token.title ?? null
    }),
  renderMarkdown: (node) => {
    const alt = node.attrs?.alt ?? "";
    const src = node.attrs?.src ?? "";
    const title = node.attrs?.title ? ` "${node.attrs.title}"` : "";
    return `![${alt}](${src}${title})`;
  },
  addNodeView() {
    return ReactNodeViewRenderer(ImageNodeView);
  }
});

/** The description editor: markdown you type becomes markdown you see, with no
 *  second pane. `**bold**` turns bold as the closing asterisk lands, `# ` makes
 *  a heading, and a pasted screenshot is uploaded and placed at the caret.
 *
 *  It is deliberately uncontrolled. The value flows out on every update and is
 *  only read back when the editor is remounted (switching to the source view
 *  and back, or the read view). Feeding the serialized markdown back in on each
 *  keystroke would rewrite the document under the caret. */
export function IssueMarkdownEditor({
  value,
  onChange,
  placeholder,
  autoFocus = false,
  fill = false
}: {
  value: string;
  onChange: (next: string) => void;
  placeholder?: string;
  autoFocus?: boolean;
  /** Fills the height it is given, for the expanded composer. */
  fill?: boolean;
}) {
  const token = useAuthStore((state) => state.token);
  // Read inside the paste/drop handlers, which are fixed when the editor is
  // created and would otherwise close over the first render's props.
  const latest = useRef({ onChange, token });
  latest.current = { onChange, token };
  const editorRef = useRef<ReturnType<typeof useEditor> | null>(null);

  /** Uploads the images and drops them in at the caret, in the order they were
   *  pasted. Only the files are taken: a paste that also carries text keeps its
   *  text, because the caller only suppresses the default when it holds an
   *  image. */
  async function insertUploadedImages(images: File[]) {
    for (const file of images) {
      try {
        const attachment = await uploadAttachment(file, latest.current.token);
        editorRef.current?.chain().focus().setImage({ src: attachment.url, alt: attachment.filename }).run();
      } catch {
        // The field shows upload failures on its own error line; an inline
        // placeholder here would be replaced by the same message.
      }
    }
  }

  function imagesFrom(files: FileList | null | undefined) {
    const images = Array.from(files ?? []).filter((file) => file.type.startsWith("image/"));
    return images.length > 0 ? images : null;
  }

  const editor = useEditor({
    extensions: [
      StarterKit.configure({ heading: { levels: [1, 2, 3] }, link: { openOnClick: false } }),
      Markdown,
      MarkdownImage,
      ...(placeholder ? [Placeholder.configure({ placeholder })] : [])
    ],
    content: value,
    contentType: "markdown",
    autofocus: autoFocus ? "end" : false,
    editorProps: {
      attributes: {
        class: cn(markdownContentClass, "focus:outline-none", fill && "scrollbar-subtle min-h-0 flex-1 overflow-y-auto")
      },
      handlePaste: (_view, event) => {
        const images = imagesFrom(event.clipboardData?.files);
        if (!images) return false;
        event.preventDefault();
        void insertUploadedImages(images);
        return true;
      },
      handleDrop: (_view, event, _slice, moved) => {
        // A move is a drag inside the document, not a file from outside.
        if (moved) return false;
        const images = imagesFrom(event.dataTransfer?.files);
        if (!images) return false;
        event.preventDefault();
        void insertUploadedImages(images);
        return true;
      }
    },
    onUpdate: ({ editor: instance }) => latest.current.onChange(instance.getMarkdown())
  });
  editorRef.current = editor;

  return (
    <div className={cn("flex min-h-0 flex-col", fill && "flex-1")}>
      <EditorContent editor={editor} className={cn("flex min-h-0 flex-col", fill && "flex-1")} />
    </div>
  );
}
