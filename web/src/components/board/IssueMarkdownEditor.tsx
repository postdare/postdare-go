import { useEffect, useRef } from "react";
import { EditorContent, NodeViewWrapper, ReactNodeViewRenderer, useEditor, type NodeViewProps } from "@tiptap/react";
import { Markdown } from "@tiptap/markdown";
import StarterKit from "@tiptap/starter-kit";
import Image from "@tiptap/extension-image";
import Placeholder from "@tiptap/extension-placeholder";

import { uploadAttachment } from "../../api/postdareGo";
import { useAuthStore } from "../../store/auth";
import { toast } from "../../store/toast";
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

/** What a surrounding field can ask of the editor once it exists. Uploading an
 *  image from a button rather than a paste needs a way in, and a composer that
 *  posts has to be able to empty itself without remounting and losing focus. */
export interface MarkdownEditorHandle {
  /** Resolves once every image is uploaded and placed, so the caller can show
   *  that an upload is in flight. */
  insertImages: (files: File[]) => Promise<void>;
  clear: () => void;
  focus: () => void;
}

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
  fill = false,
  onReady,
  onSubmit
}: {
  value: string;
  onChange: (next: string) => void;
  placeholder?: string;
  autoFocus?: boolean;
  /** Fills the height it is given, for the expanded composer. */
  fill?: boolean;
  /** Hands the editor's controls to the field around it, and null when the
   *  editor goes away, so a held handle cannot outlive the instance. */
  onReady?: (handle: MarkdownEditorHandle | null) => void;
  /** Cmd/Ctrl+Enter. A comment composer posts on it; a description field that
   *  passes nothing leaves the shortcut to the page. */
  onSubmit?: () => void;
}) {
  const token = useAuthStore((state) => state.token);
  // Read inside the paste/drop handlers, which are fixed when the editor is
  // created and would otherwise close over the first render's props.
  const latest = useRef({ onChange, token, onSubmit });
  latest.current = { onChange, token, onSubmit };
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
      } catch (error) {
        // Said out loud rather than swallowed: the image simply not appearing
        // reads as the editor having dropped it.
        toast(error instanceof Error ? error.message : "The image could not be uploaded", "danger");
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
      handleKeyDown: (_view, event) => {
        if (event.key !== "Enter" || !(event.metaKey || event.ctrlKey)) return false;
        const submit = latest.current.onSubmit;
        if (!submit) return false;
        event.preventDefault();
        submit();
        return true;
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

  // The handle is published once the editor exists and withdrawn when it goes,
  // so a field holding it can never reach a torn-down instance.
  useEffect(() => {
    if (!onReady) return;
    if (!editor) {
      onReady(null);
      return;
    }
    onReady({
      insertImages: insertUploadedImages,
      // Emitting the update is what tells the field its draft is empty now.
      clear: () => editor.commands.clearContent(true),
      focus: () => editor.commands.focus("end")
    });
    return () => onReady(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editor]);

  return (
    <div className={cn("flex min-h-0 flex-col", fill && "flex-1")}>
      <EditorContent editor={editor} className={cn("flex min-h-0 flex-col", fill && "flex-1")} />
    </div>
  );
}
