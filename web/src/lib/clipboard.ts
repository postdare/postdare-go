/** Copy text, reporting whether it landed.
 *
 *  `navigator.clipboard` is only defined in a secure context, and a self-hosted
 *  console is routinely reached over plain http on a LAN address, so the
 *  deprecated execCommand path stays as the fallback rather than leaving copy
 *  silently broken there.
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Permissions denied or the document lost focus mid-write; try the
    // synchronous path before giving up.
  }
  try {
    const field = document.createElement("textarea");
    field.value = text;
    field.setAttribute("readonly", "");
    // Off-screen but still in the layout: a display:none or detached field
    // cannot be selected, and select() is what puts the text in the clipboard.
    field.style.position = "fixed";
    field.style.top = "-1000px";
    document.body.appendChild(field);
    field.select();
    const copied = document.execCommand("copy");
    document.body.removeChild(field);
    return copied;
  } catch {
    return false;
  }
}
